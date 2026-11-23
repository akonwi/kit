import Foundation

/// Loads what a file pane shows with one download of the file (ADR 0074),
/// for every Apple client. The copy is kept in a download store, which
/// revalidates it with the server instead of downloading it again.
///
/// A download whose file has a revision is checked afterwards; a file that
/// changed meanwhile is downloaded once more, and a copy that changed again
/// is kept without a revision, so it can't be annotated or revalidated.
///
/// The download is classified here: UTF-8 text, a supported image where the
/// pane shows images, or a binary file. Text stops at the caller's byte
/// limit, cut at a line's end, or at the last whole character when the first
/// line doesn't fit.
public enum FileContent {
    /// The text a pane shows.
    public struct Text: Equatable, Sendable {
        public let content: String
        /// The file's path as the server resolved it.
        public var path: String
        /// The revision the download had, or nil for a file without one or
        /// one that changed while it downloaded.
        public let revision: String?
        /// The file's size, when the server reported it.
        public let size: Int64?
        public var modifiedAt: String?
        /// The text stops at the byte limit, before the file's end.
        public let limited: Bool
        /// The file changed while it downloaded, so the text may not match
        /// any one revision of it.
        public var changed: Bool

        public init(content: String, path: String = "", revision: String?, size: Int64?, modifiedAt: String? = nil,
                    limited: Bool, changed: Bool = false) {
            self.content = content; self.path = path; self.revision = revision; self.size = size
            self.modifiedAt = modifiedAt; self.limited = limited; self.changed = changed
        }
    }

    /// What a download is.
    public enum Outcome<Image: Sendable>: Sendable {
        case text(Text)
        /// A supported image, decoded for display, when the pane asked for
        /// images, its revision, and whether it changed while it downloaded.
        case image(ViewedFile, Image, revision: String?, changed: Bool)
        /// The file isn't text, or an image the pane shows.
        case binary
    }

    /// The extensions of the images panes show, which are downloaded up to
    /// `ViewedFile.maxImageBytes` rather than the text limit.
    static let imageExtensions: Set<String> = ["png", "jpg", "jpeg", "gif", "webp"]

    /// Whether `path` names an image.
    static func namesImage(_ path: String) -> Bool {
        imageExtensions.contains((path as NSString).pathExtension.lowercased())
    }

    /// Downloads `target` and classifies it. Text stops at `textLimit`
    /// bytes. `progress` reports the bytes written and the bytes the current
    /// request expects: the file's size, up to the request's limit. `decodeImage`, when given, asks for supported images as images:
    /// it decodes one for display, inside the retry, so a copy that can't
    /// show fails rather than replacing one that can. Throws why the file
    /// couldn't be downloaded or an image can't show, and on cancellation.
    public static func load<Image: Sendable>(
        _ target: FileDownloadTarget, session: String, textLimit: Int64,
        decodeImage: (@Sendable (Data) -> Image?)?, client: any FileDownloadClient, store: FileDownloadStore,
        progress: @escaping @Sendable (Int64, Int64) -> Void
    ) async throws -> Outcome<Image> {
        let path: String
        switch target {
        case .workspace(_, let name), .host(let name): path = name
        }
        let imageLimit = Int64(ViewedFile.maxImageBytes)
        let limit = decodeImage != nil && namesImage(path) ? imageLimit : textLimit
        if let outcome = try await fetch(target, session: session, limit: limit, decodeImage: decodeImage, client: client,
                                         store: store, progress: progress) {
            return outcome
        }
        // An image named as something else, past the text limit.
        return try await fetch(target, session: session, limit: imageLimit, decodeImage: decodeImage, client: client,
                               store: store, progress: progress) ?? .binary
    }

    /// Downloads and classifies a file at `limit`, checking afterwards that
    /// it didn't change meanwhile, or returns nil for an image cut at a limit
    /// smaller than the one images have.
    private static func fetch<Image: Sendable>(
        _ target: FileDownloadTarget, session: String, limit: Int64, decodeImage: (@Sendable (Data) -> Image?)?,
        client: any FileDownloadClient, store: FileDownloadStore, progress: @escaping @Sendable (Int64, Int64) -> Void
    ) async throws -> Outcome<Image>? {
        let key = FileDownloadStore.Key(server: client.serverID, session: session, target: target, maxBytes: limit)
        let progress: @Sendable (Int64, Int64?) -> Void = { written, size in progress(written, min(size ?? limit, limit)) }
        var kept: Outcome<Image>?
        for attempt in 0..<2 {
            let file: FileDownloadStore.File
            do {
                file = try await store.file(key, client: client, progress: progress)
            } catch {
                // A retry that fails keeps the copy that asked for it.
                if attempt > 0, !Task.isCancelled, !isCancellation(error), let kept { return kept }
                throw error
            }
            if decodeImage != nil, ViewedFile.imageType(file.data.prefix(16)) != nil, file.truncated {
                // A retry keeps the copy that asked for it.
                if let kept { return kept }
                guard limit >= ViewedFile.maxImageBytes else { return nil }
                throw ViewedFileError.imageTooLarge
            }
            let classified: Outcome<Image>
            do {
                classified = try await Task.detached(priority: .userInitiated) { try classify(file, decodeImage: decodeImage) }.value
            } catch {
                if let kept { return kept }
                throw error
            }
            try Task.checkCancellation()
            guard case .binary = classified else {
                guard let revision = file.info.revision, try await changed(key, revision: revision, client: client) else { return classified }
                switch classified {
                case .text(let text):
                    kept = .text(Text(content: text.content, path: text.path, revision: nil, size: text.size,
                                      modifiedAt: text.modifiedAt, limited: text.limited, changed: true))
                case .image(let file, let image, _, _):
                    kept = .image(file, image, revision: nil, changed: true)
                case .binary:
                    break
                }
                continue
            }
            // A retry that isn't text keeps the copy that asked for it.
            return kept ?? .binary
        }
        // The loop ends only after a copy that changed while it downloaded.
        return kept ?? .binary
    }

    /// What a whole downloaded file is: text, a supported image when
    /// `decodeImage` is given, or binary. An image that can't show throws
    /// why.
    static func classify<Image: Sendable>(_ file: FileDownloadStore.File,
                                          decodeImage: (@Sendable (Data) -> Image?)?) throws -> Outcome<Image> {
        let path = file.info.path
        if let decodeImage, let mediaType = ViewedFile.imageType(file.data.prefix(16)) {
            let size = file.info.size ?? Int64(file.data.count)
            let viewed = try ViewedFile.image(file.data, mediaType: mediaType, path: path, size: size)
            guard let image = decodeImage(file.data) else { throw ViewedFileError.unsupported }
            return .image(viewed, image, revision: file.info.revision, changed: false)
        }
        guard let content = decode(file.data, limited: file.truncated) else { return .binary }
        return .text(Text(content: content, path: path, revision: file.info.revision, size: file.info.size,
                          modifiedAt: file.info.modifiedAt, limited: file.truncated))
    }

    /// Whether the file no longer has `revision`: its revision differs, or
    /// it or its workspace is gone. A check that fails otherwise says
    /// nothing, unless the load was cancelled.
    private static func changed(_ key: FileDownloadStore.Key, revision: String, client: any FileDownloadClient) async throws -> Bool {
        do {
            return try await client.checkFile(key.session, target: key.target).revision != revision
        } catch {
            if Task.isCancelled || isCancellation(error) { throw CancellationError() }
            return error as? FileDownloadError == .response(404) || error as? FileDownloadError == .response(409)
        }
    }

    /// The UTF-8 text of a downloaded file, or nil when it isn't text. A copy
    /// cut at the byte limit ends at its last complete line, or, when its
    /// first line doesn't fit, at its last whole character.
    public static func decode(_ data: Data, limited: Bool) -> String? {
        var data = data
        if limited {
            if let newline = data.lastIndex(of: 0x0A) {
                data = data.prefix(through: newline)
            } else {
                // A cut may end inside a character, at most three bytes in.
                for _ in 0..<3 where !data.isEmpty && String(data: data, encoding: .utf8) == nil {
                    data = data.dropLast()
                }
            }
        }
        guard !data.contains(0) else { return nil }
        return String(data: data, encoding: .utf8)
    }
}
