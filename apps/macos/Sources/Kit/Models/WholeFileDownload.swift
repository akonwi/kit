import Foundation
import Observation

/// Loads what a file pane shows with one download of the file (ADR 0074),
/// through the shared `FileContent` loader, and reports its progress. The
/// editor gets at most `maxEditorBytes`, cut at a line's end, because opening
/// very large text in it is slow.
@MainActor @Observable
final class WholeFileDownload {
    /// The text a pane shows.
    typealias Text = FileContent.Text

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
        /// Text with a line longer than `maxEditorLineBytes`.
        case lineTooLong
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
    /// The longest line the editor is given, in UTF-8 bytes. A longer one
    /// makes it too slow to use.
    nonisolated static let maxEditorLineBytes = 1 << 20
    /// What a pane says about a file with a line longer than that.
    static let lineTooLongMessage = "This file has a line too long to show."

    private(set) var state = State.idle
    private var task: Task<Outcome, Never>?
    private var generation = 0

    /// Downloads `target` and returns what it is, or that the download
    /// stopped. `images` asks for supported images as images rather than as
    /// binary files.
    func load(_ target: FileDownloadTarget, session: String, client: any FileDownloadClient, images: Bool = false,
              store: FileDownloadStore = .shared) async -> Outcome {
        task?.cancel()
        generation += 1
        let generation = generation
        state = .downloading(written: 0, size: nil)
        let progress: @Sendable (Int64, Int64) -> Void = { [weak self] written, size in
            Task { @MainActor in
                guard let self, self.generation == generation, case .downloading(let shown, _) = self.state, written > shown else { return }
                self.state = .downloading(written: written, size: size)
            }
        }
        let decode: (@Sendable (Data) -> DecodedImage?)? = images ? Self.decodeImage : nil
        let task = Task<Outcome, Never> { [weak self] in
            do {
                let outcome = try await FileContent.load(target, session: session, textLimit: Self.maxEditorBytes, decodeImage: decode,
                                                         client: client, store: store, progress: progress)
                let result: Outcome
                switch outcome {
                case .text(let text):
                    let scan = Task.detached(priority: .userInitiated) { Self.hasLine(longerThan: Self.maxEditorLineBytes, in: text.content) }
                    let long = await withTaskCancellationHandler { await scan.value } onCancel: { scan.cancel() }
                    try Task.checkCancellation()
                    result = long ? .lineTooLong : .text(text)
                case .image(let file, let image, let revision, let changed):
                    result = .image(Image(file: file, loaded: image.loaded), revision: revision, changed: changed)
                case .binary: result = .binary
                }
                self?.finish(generation, .idle)
                return result
            } catch {
                self?.finish(generation, isCancellation(error) ? .cancelled : .failed(error.localizedDescription))
                return .stopped
            }
        }
        self.task = task
        return await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
    }

    /// A decoded image as it crosses from the loader.
    private struct DecodedImage: @unchecked Sendable {
        let loaded: LoadedImage
    }

    private nonisolated static func decodeImage(_ data: Data) -> DecodedImage? {
        LoadedImage(data: data).map(DecodedImage.init)
    }

    /// Whether a line of `content` is longer than `limit` UTF-8 bytes, not
    /// counting its line ending. A cancelled scan stops early.
    nonisolated static func hasLine(longerThan limit: Int, in content: String) -> Bool {
        var length = 0, scanned = 0
        for byte in content.utf8 {
            // CR ends a line too, alone or before LF.
            length = byte == 0x0A || byte == 0x0D ? 0 : length + 1
            if length > limit { return true }
            scanned += 1
            if scanned & 0xFFFF == 0, Task.isCancelled { return false }
        }
        return false
    }

    /// Stops a download under way.
    func cancel() { task?.cancel() }

    private func finish(_ generation: Int, _ state: State) {
        guard self.generation == generation else { return }
        self.state = state
        task = nil
    }

    /// The UTF-8 text of a downloaded file, as `FileContent.decode` gives it.
    nonisolated static func decode(_ data: Data, limited: Bool) -> String? {
        FileContent.decode(data, limited: limited)
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
