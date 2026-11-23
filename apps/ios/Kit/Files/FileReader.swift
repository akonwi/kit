import Foundation
import ImageIO
import KitClient
import UIKit

/// One file as last downloaded from the server (ADR 0074): its text or
/// image, or why it can't be shown. A file in the session's folder is a
/// workspace file; one elsewhere on the server, view-only, a host file (ADR
/// 0050), which may be a supported image. Downloads again on refresh,
/// keeping what it has until the new copy lands; the server answers with
/// nothing new while the file is unchanged.
@MainActor @Observable
final class FileReader {
    /// The text that shows.
    struct Text: Equatable {
        let content: String
        /// Changes when the content does.
        let revision: String
        let truncated: Bool
    }

    /// The most text a file shows. Larger files show their start, cut at a
    /// line's end.
    nonisolated static let maxTextBytes: Int64 = 8 << 20
    /// The most of a Markdown file that shows. Its preview is parsed whole on
    /// the main actor, at about 0.8 s per MiB, so this keeps it under 2 s.
    nonisolated static let maxMarkdownBytes: Int64 = 2 << 20

    /// The most of the file at `path` that shows.
    nonisolated static func textLimit(_ path: String) -> Int64 {
        MarkdownFilePaths.isMarkdown(path) ? maxMarkdownBytes : maxTextBytes
    }

    let path: String
    /// Whether the file is outside the session's folder.
    let onServer: Bool
    /// The workspace file as downloaded, which comments anchor to; nil for
    /// a host file, or a copy that changed while it downloaded.
    private(set) var file: WorkspaceFile?
    private(set) var text: Text?
    /// An image elsewhere on the server.
    private(set) var image: UIImage?
    /// The path the server read, absolute, for a file elsewhere.
    private(set) var readPath: String?
    private(set) var loading = false
    /// How much of the file has downloaded, while it downloads.
    private(set) var progress: Double?
    private(set) var error: String?
    /// The file's size, when the server reported it.
    private var size: Int64?
    /// The download shown, and its revision.
    private var shown: FileContent.Text?
    private var shownRevision: String?
    /// What shows changed while it downloaded.
    private var changed = false
    private var token = UUID()
    private let store: FileDownloadStore

    init(path: String, onServer: Bool = false, store: FileDownloadStore = .shared) {
        self.path = path
        self.onServer = onServer
        self.store = store
    }

    /// The file's lines as they show, tabs as four spaces. A trailing
    /// newline adds no line.
    private(set) var lines: [String] = []
    /// The length of the longest of `lines`.
    private(set) var longest = 0

    /// What the text doesn't say about the file: that it stops short, or
    /// changed while it downloaded.
    var notice: String? {
        var notes: [String] = []
        if let text, text.truncated {
            let shown = ByteCountFormatter.string(fromByteCount: Int64(text.content.utf8.count), countStyle: .file)
            if let size {
                notes.append("Showing the first \(shown) of \(ByteCountFormatter.string(fromByteCount: size, countStyle: .file)).")
            } else {
                notes.append("Showing the first \(shown).")
            }
        }
        if changed {
            // Only the session's own text files take comments.
            notes.append(text != nil && !onServer
                ? "The file changed while it loaded, so it can’t take comments. Refresh to load it again."
                : "The file changed while it loaded. Refresh to load it again.")
        }
        return notes.isEmpty ? nil : notes.joined(separator: " ")
    }

    /// Downloads the file in `session`, whose working directory is `cwd`.
    func load(client: any FileDownloadClient, session: String, cwd: String?) async {
        let token = UUID()
        self.token = token
        loading = true
        error = nil
        defer {
            if self.token == token {
                loading = false
                progress = nil
            }
        }
        let report: @Sendable (Int64, Int64) -> Void = { [weak self] written, expected in
            Task { @MainActor in
                guard let self, self.token == token else { return }
                self.progress = expected > 0 ? min(Double(written) / Double(expected), 1) : nil
            }
        }
        do {
            var workspace: WireWorkspaceRef?
            let target: FileDownloadTarget
            if onServer {
                target = .host(path: path)
            } else {
                let ref = try await client.workspace(session)
                guard cwd == nil || ref.cwd == cwd else { throw WorkspaceFileError.unavailable }
                workspace = ref
                target = .workspace(id: ref.workspaceId, path: path)
            }
            try Task.checkCancellation()
            guard self.token == token else { return }
            let decode: (@Sendable (Data) -> DecodedImage?)? = onServer ? Self.decodeImage : nil
            let outcome = try await FileContent.load(target, session: session, textLimit: Self.textLimit(path), decodeImage: decode,
                                                     client: client, store: store, progress: report)
            try Task.checkCancellation()
            guard self.token == token else { return }
            switch outcome {
            case .binary:
                throw onServer ? ViewedFileError.unsupported : WorkspaceFileError.response(415)
            case .image(let viewed, let decoded, _, let changed):
                readPath = viewed.path
                size = viewed.size
                image = decoded.image
                self.changed = changed
                text = nil; file = nil; shown = nil; shownRevision = nil
                lines = []
                longest = 0
            case .text(let downloaded):
                // A copy the server says is current stays as it shows.
                if let revision = downloaded.revision, revision == shownRevision, downloaded.limited == shown?.limited, text != nil { break }
                // Splitting megabytes of text takes a while.
                let (lines, longest) = await Task.detached(priority: .userInitiated) {
                    let lines = Self.lines(downloaded.content)
                    return (lines, FileLineMetrics.longest(lines))
                }.value
                try Task.checkCancellation()
                guard self.token == token else { return }
                self.longest = longest
                show(downloaded, lines: lines, workspace: workspace)
            }
        } catch is CancellationError {
        } catch {
            guard self.token == token, !Task.isCancelled else { return }
            self.error = (error as? any LocalizedError)?.errorDescription ?? "Couldn’t read this file."
        }
    }

    private func show(_ downloaded: FileContent.Text, lines: [String], workspace: WireWorkspaceRef?) {
        shown = downloaded
        shownRevision = downloaded.revision
        size = downloaded.size
        changed = downloaded.changed
        image = nil
        self.lines = lines
        if onServer { readPath = downloaded.path.isEmpty ? path : downloaded.path }
        // A copy without a revision, such as one that changed while it
        // downloaded, can't anchor comments.
        if let workspace, let revision = downloaded.revision {
            file = WorkspaceFile(
                workspace: workspace, path: path, revision: revision, content: downloaded.content,
                size: size ?? Int64(downloaded.content.utf8.count), modifiedAt: downloaded.modifiedAt, truncated: downloaded.limited)
        } else {
            file = nil
        }
        text = Text(content: downloaded.content, revision: downloaded.revision ?? "\(downloaded.content.utf8.count):\(downloaded.content.hashValue)",
                    truncated: downloaded.limited)
    }

    /// A decoded image as it crosses from the loader.
    private struct DecodedImage: @unchecked Sendable {
        let image: UIImage
    }

    private nonisolated static func decodeImage(_ data: Data) -> DecodedImage? {
        image(data).map(DecodedImage.init)
    }

    /// The longest side an image from the server is kept at, enough to zoom
    /// into on a phone without holding every pixel of a large photo.
    nonisolated static let maxImagePixels = 3072

    nonisolated static func image(_ data: Data) -> UIImage? {
        autoreleasepool {
            guard let source = CGImageSourceCreateWithData(data as CFData, nil),
                  let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                      kCGImageSourceCreateThumbnailFromImageAlways: true,
                      kCGImageSourceCreateThumbnailWithTransform: true,
                      kCGImageSourceShouldCacheImmediately: true,
                      kCGImageSourceThumbnailMaxPixelSize: maxImagePixels
                  ] as CFDictionary) else { return nil }
            return UIImage(cgImage: image)
        }
    }

    nonisolated static func lines(_ content: String) -> [String] {
        guard !content.isEmpty else { return [] }
        var lines = content.components(separatedBy: "\n")
        if lines.last == "" { lines.removeLast() }
        return lines.map { line in
            (line.hasSuffix("\r") ? String(line.dropLast()) : line).replacingOccurrences(of: "\t", with: "    ")
        }
    }
}
