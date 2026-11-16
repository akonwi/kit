import Foundation
import ImageIO
import KitClient
import UIKit

/// One file as last read from the server: its text or image, or why it
/// can't be shown. A file in the session's folder is read as a workspace
/// file; one elsewhere on the server, view-only, as a host file (ADR 0050).
/// Reads again on refresh, keeping what it has until the new read lands.
@MainActor @Observable
final class FileReader {
    /// The text that shows, from either kind of read.
    struct Text: Equatable {
        let content: String
        /// Changes when the content does.
        let revision: String
        let truncated: Bool
    }

    let path: String
    /// Whether the file is outside the session's folder.
    let onServer: Bool
    /// The workspace read, which comments anchor to.
    private(set) var file: WireWorkspaceFileRead?
    private(set) var text: Text?
    /// An image elsewhere on the server.
    private(set) var image: UIImage?
    /// The path the server read, absolute, for a file elsewhere.
    private(set) var readPath: String?
    private(set) var loading = false
    private(set) var error: String?
    private var size: Int64 = 0
    private var token = UUID()

    init(path: String, onServer: Bool = false) {
        self.path = path
        self.onServer = onServer
    }

    /// The file's lines as they show, tabs as four spaces. A trailing
    /// newline adds no line.
    private(set) var lines: [String] = []

    /// Why the text stops short, when it does.
    var truncationNotice: String? {
        guard let text, text.truncated else { return nil }
        let lines = file?.returnedLines ?? lines.count
        return "Showing the first \(lines.formatted()) lines of \(ByteCountFormatter.string(fromByteCount: size, countStyle: .file))."
    }

    /// Reads the file in `session`, whose working directory is `cwd`.
    func load(client: any FileBrowsingClient, session: String, cwd: String?) async {
        let token = UUID()
        self.token = token
        loading = true
        error = nil
        defer { if self.token == token { loading = false } }
        do {
            if onServer {
                let read = try await client.readHostFile(session, path: path)
                try Task.checkCancellation()
                guard self.token == token else { return }
                let shown = try await Self.shown(read)
                // A newer read may have landed while the image was decoded.
                try Task.checkCancellation()
                guard self.token == token else { return }
                show(read, shown)
            } else {
                let read = try await readWorkspace(client: client, session: session, cwd: cwd)
                try Task.checkCancellation()
                guard self.token == token else { return }
                file = read
                size = read.size
                text = Text(content: read.content, revision: read.revision, truncated: read.truncated)
                lines = Self.lines(read.content)
            }
        } catch is CancellationError {
        } catch {
            guard self.token == token, !Task.isCancelled else { return }
            self.error = (error as? WorkspaceFileError)?.errorDescription ?? (error as? ViewedFileError)?.errorDescription
                ?? "Couldn’t read this file."
        }
    }

    private func readWorkspace(client: any WorkspaceFileClient, session: String, cwd: String?) async throws -> WireWorkspaceFileRead {
        let expected = file?.revision
        let read: WireWorkspaceFileRead
        do {
            read = try await client.readWorkspaceFile(session, path: path, expectedRevision: expected)
        } catch WorkspaceFileError.response(409) where expected != nil {
            // The file changed since it was read: read it fresh.
            try Task.checkCancellation()
            read = try await client.readWorkspaceFile(session, path: path, expectedRevision: nil)
        }
        guard read.sessionId == session, read.path == path, cwd == nil || read.workspace.cwd == cwd else {
            throw WorkspaceFileError.response(0)
        }
        return read
    }

    /// What a read from the server shows: its text, or its image decoded
    /// at most `maxImagePixels` on its longer side.
    private enum Shown {
        case text(String, truncated: Bool)
        case image(UIImage)
    }

    /// The longest side an image from the server is kept at, enough to zoom
    /// into on a phone without holding every pixel of a large photo.
    nonisolated static let maxImagePixels = 3072

    private static func shown(_ read: ViewedFile) async throws -> Shown {
        switch read.content {
        case .text(let content, let truncated):
            return .text(content, truncated: truncated)
        case .image(let data, _, _, _):
            let decoded = await Task.detached(priority: .userInitiated) { Self.image(data) }.value
            guard let decoded else { throw ViewedFileError.unsupported }
            return .image(decoded)
        }
    }

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

    private func show(_ read: ViewedFile, _ shown: Shown) {
        readPath = read.path
        size = read.size
        switch shown {
        case .text(let content, let truncated):
            image = nil
            text = Text(content: content, revision: "\(read.size):\(content.hashValue)", truncated: truncated)
            lines = Self.lines(content)
        case .image(let decoded):
            image = decoded
            text = nil
            lines = []
        }
    }

    static func lines(_ content: String) -> [String] {
        guard !content.isEmpty else { return [] }
        var lines = content.components(separatedBy: "\n")
        if lines.last == "" { lines.removeLast() }
        return lines.map { line in
            (line.hasSuffix("\r") ? String(line.dropLast()) : line).replacingOccurrences(of: "\t", with: "    ")
        }
    }
}
