import Foundation
import Observation

/// Loads a file pane's text with one download of the file (ADR 0074). The
/// copy is kept in the shared download store, which revalidates it with the
/// server instead of downloading it again. The editor gets at most
/// `maxEditorBytes`, cut at a line's end, because opening very large text in
/// it is slow.
///
/// A download whose file has a revision is checked afterwards; a file that
/// changed meanwhile is downloaded once more, and a copy that changed again
/// is kept without a revision, so it can't be annotated or revalidated.
///
/// The download is classified here: UTF-8 text, a supported image where the
/// pane shows images, or a binary file. A text file whose first line doesn't
/// fit is cut at its last whole character.
@MainActor @Observable
final class WholeFileDownload {
    /// The text a pane shows.
    struct Text: Equatable, Sendable {
        let content: String
        /// The file's path as the server resolved it.
        var path: String = ""
        /// The revision the download had, or nil for a file without one or
        /// one that changed while it downloaded.
        let revision: String?
        /// The file's size, when the server reported it.
        let size: Int64?
        var modifiedAt: String?
        /// The text stops at `maxEditorBytes`, before the file's end.
        let limited: Bool
        /// The file changed while it downloaded, so the text may not match
        /// any one revision of it.
        var changed = false
    }

    enum State: Equatable {
        case idle
        case downloading(written: Int64, size: Int64?)
        case cancelled
        case failed(String)
    }

    /// What a load ended with.
    enum Outcome: Equatable {
        case text(Text)
        /// A supported image, when the pane asked for images, and whether it
        /// changed while it downloaded.
        case image(Image, revision: String?, changed: Bool = false)
        /// The file isn't text, or an image the pane shows.
        case binary
        /// The download was cancelled or failed, which `state` says.
        case stopped
    }

    /// A downloaded image, decoded for display.
    struct Image: Equatable, @unchecked Sendable {
        let file: ViewedFile
        let loaded: LoadedImage

        static func == (lhs: Self, rhs: Self) -> Bool { lhs.file == rhs.file }
    }

    /// The most text the editor is given.
    static let maxEditorBytes: Int64 = 8 << 20

    /// The extensions of the images panes show, which are downloaded up to
    /// `ViewedFile.maxImageBytes` rather than the text limit.
    static let imageExtensions: Set<String> = ["png", "jpg", "jpeg", "gif", "webp"]

    private(set) var state = State.idle
    private var task: Task<Outcome, Never>?
    private var generation = 0

    /// Whether `path` names an image, which is never shown as text.
    static func namesImage(_ path: String) -> Bool {
        imageExtensions.contains((path as NSString).pathExtension.lowercased())
    }

    /// Downloads `target` and returns what it is, or that the download
    /// stopped. `images` asks for supported images as images rather than as
    /// binary files.
    func load(_ target: FileDownloadTarget, session: String, client: any FileDownloadClient, images: Bool = false,
              store: FileDownloadStore = .shared) async -> Outcome {
        task?.cancel()
        generation += 1
        let generation = generation
        let path: String
        switch target {
        case .workspace(_, let name), .host(let name): path = name
        }
        state = .downloading(written: 0, size: nil)
        let limit = images && Self.namesImage(path) ? Int64(ViewedFile.maxImageBytes) : Self.maxEditorBytes
        let progress: @Sendable (Int64, Int64?) -> Void = { [weak self] written, size in
            Task { @MainActor in
                guard let self, self.generation == generation, case .downloading(let shown, _) = self.state, written > shown else { return }
                self.state = .downloading(written: written, size: size)
            }
        }
        let task = Task<Outcome, Never> { [weak self] in
            do {
                var outcome = try await Self.fetch(target, session: session, limit: limit, images: images, client: client,
                                                   store: store, progress: progress)
                if case .tooSmall = outcome {
                    // An image named as something else, past the text limit.
                    outcome = try await Self.fetch(target, session: session, limit: Int64(ViewedFile.maxImageBytes), images: images,
                                                   client: client, store: store, progress: progress)
                }
                self?.finish(generation, .idle)
                return outcome.public
            } catch {
                self?.finish(generation, isCancellation(error) ? .cancelled : .failed(error.localizedDescription))
                return .stopped
            }
        }
        self.task = task
        return await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
    }

    /// What a download is, before the pane sees it.
    private enum Fetched {
        case outcome(Outcome)
        /// An image cut at a limit smaller than the one images have.
        case tooSmall

        var `public`: Outcome {
            switch self {
            case .outcome(let outcome): outcome
            case .tooSmall: .stopped
            }
        }
    }

    /// Downloads and classifies a file, checking afterwards that it didn't
    /// change meanwhile.
    private nonisolated static func fetch(_ target: FileDownloadTarget, session: String, limit: Int64, images: Bool,
                                          client: any FileDownloadClient, store: FileDownloadStore,
                                          progress: @escaping @Sendable (Int64, Int64?) -> Void) async throws -> Fetched {
        let key = FileDownloadStore.Key(server: client.serverID, session: session, target: target, maxBytes: limit)
        var kept: Outcome?
        for attempt in 0..<2 {
            let file: FileDownloadStore.File
            do {
                file = try await store.file(key, client: client, progress: progress)
            } catch {
                // A retry that fails keeps the copy that asked for it.
                if attempt > 0, !Task.isCancelled, !isCancellation(error), let kept { return .outcome(kept) }
                throw error
            }
            let image = images && ViewedFile.imageType(file.data.prefix(16)) != nil
            if image, file.truncated {
                // A retry keeps the copy that asked for it.
                if let kept { return .outcome(kept) }
                guard limit >= ViewedFile.maxImageBytes else { return .tooSmall }
                throw ViewedFileError.imageTooLarge
            }
            let classified: Outcome?
            do {
                classified = try await Task.detached(priority: .userInitiated) { try classify(file, images: images) }.value
            } catch {
                if let kept { return .outcome(kept) }
                throw error
            }
            try Task.checkCancellation()
            guard var outcome = classified else {
                // A retry that isn't text keeps the copy that asked for it.
                return .outcome(kept ?? .binary)
            }
            guard let revision = file.info.revision, try await changed(key, revision: revision, client: client) else { return .outcome(outcome) }
            switch outcome {
            case .text(let text):
                outcome = .text(Text(content: text.content, path: text.path, revision: nil, size: text.size, modifiedAt: text.modifiedAt,
                                     limited: text.limited, changed: true))
            case .image(let image, _, _):
                outcome = .image(image, revision: nil, changed: true)
            case .binary, .stopped:
                break
            }
            kept = outcome
        }
        return kept.map(Fetched.outcome) ?? .outcome(.stopped)
    }

    /// What a whole downloaded file is: text, a supported image when
    /// `images` is set, or nil for a binary file. An image Kit can't show
    /// throws why.
    nonisolated static func classify(_ file: FileDownloadStore.File, images: Bool) throws -> Outcome? {
        let path = file.info.path
        if images, let mediaType = ViewedFile.imageType(file.data.prefix(16)) {
            let size = file.info.size ?? Int64(file.data.count)
            let image = try ViewedFile.image(file.data, mediaType: mediaType, path: path, size: size)
            // The image is decoded here, so a copy that can't show fails
            // rather than replacing one that can.
            guard let loaded = LoadedImage(data: file.data) else { throw ViewedFileError.unsupported }
            return .image(Image(file: image, loaded: loaded), revision: file.info.revision)
        }
        guard let content = decode(file.data, limited: file.truncated) else { return nil }
        return .text(Text(content: content, path: path, revision: file.info.revision, size: file.info.size,
                          modifiedAt: file.info.modifiedAt, limited: file.truncated))
    }

    /// Whether the file no longer has `revision`: its revision differs, or
    /// it or its workspace is gone. A check that fails otherwise says
    /// nothing, unless the load was cancelled.
    private nonisolated static func changed(_ key: FileDownloadStore.Key, revision: String, client: any FileDownloadClient) async throws -> Bool {
        do {
            return try await client.checkFile(key.session, target: key.target).revision != revision
        } catch {
            if Task.isCancelled || isCancellation(error) { throw CancellationError() }
            return error as? FileDownloadError == .response(404) || error as? FileDownloadError == .response(409)
        }
    }

    /// Stops a download under way.
    func cancel() { task?.cancel() }

    private func finish(_ generation: Int, _ state: State) {
        guard self.generation == generation else { return }
        self.state = state
        task = nil
    }

    /// The UTF-8 text of a downloaded file, or nil when it isn't text. A copy
    /// cut at the byte limit ends at its last complete line, or, when its
    /// first line doesn't fit, at its last whole character.
    nonisolated static func decode(_ data: Data, limited: Bool) -> String? {
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

    /// The footer's note on the download, or nil when there's nothing to say.
    static func note(_ state: State, text: Text?) -> String? {
        switch state {
        case .downloading(let written, let size):
            let total = min(size ?? maxEditorBytes, maxEditorBytes)
            guard total > 0 else { return "Loading file…" }
            return "Loading file… \(Int(min(Double(written) / Double(total), 1) * 100))%"
        case .idle, .cancelled, .failed:
            // The pane says why a load stopped.
            guard let text else { return nil }
            var notes: [String] = []
            if text.limited {
                notes.append("Showing the first " + ByteCountFormatter.string(fromByteCount: Int64(text.content.utf8.count), countStyle: .file))
            }
            if text.changed { notes.append("Changed while loading") }
            return notes.isEmpty ? nil : notes.joined(separator: " · ")
        }
    }
}
