import Foundation
import Observation

/// Downloads the whole of a file whose preview was cut off (ADR 0074), so a
/// pane can show all of it. The copy is kept in the shared download store,
/// which revalidates it with the server instead of downloading it again. The
/// editor gets at most `maxEditorBytes`, cut at a line's end, because opening
/// very large text in it is slow.
@MainActor @Observable
final class WholeFileDownload {
    /// The text a pane shows instead of its preview.
    struct Text: Equatable, Sendable {
        let content: String
        /// The revision the download had, or nil for a file without one.
        let revision: String?
        /// The file's size, when the server reported it.
        let size: Int64?
        /// The text stops at `maxEditorBytes`, before the file's end.
        let limited: Bool
    }

    enum State: Equatable {
        case idle
        case downloading(written: Int64, size: Int64?)
        case cancelled
        case failed(String)
    }

    /// The most text the editor is given.
    static let maxEditorBytes: Int64 = 8 << 20

    private(set) var state = State.idle
    private var task: Task<Text?, Never>?
    private var generation = 0

    /// Downloads `target` and returns its text, or nil when the preview should
    /// stay: the server predates downloads, the file isn't UTF-8 text, the
    /// download was cancelled, or it failed, which `state` then says.
    func load(_ target: FileDownloadTarget, session: String, client: any FileDownloadClient,
              store: FileDownloadStore = .shared) async -> Text? {
        task?.cancel()
        generation += 1
        let generation = generation
        state = .downloading(written: 0, size: nil)
        let key = FileDownloadStore.Key(server: client.serverID, session: session, target: target, maxBytes: Self.maxEditorBytes)
        let progress: @Sendable (Int64, Int64?) -> Void = { [weak self] written, size in
            Task { @MainActor in
                guard let self, self.generation == generation, case .downloading(let shown, _) = self.state, written > shown else { return }
                self.state = .downloading(written: written, size: size)
            }
        }
        let task = Task<Text?, Never> { [weak self] in
            do {
                let file = try await store.file(key, client: client, progress: progress)
                let content = await Task.detached(priority: .userInitiated) {
                    Self.decode(file.data, limited: file.truncated)
                }.value
                guard let content else { self?.finish(generation, .idle); return nil }
                self?.finish(generation, .idle)
                return Text(content: content, revision: file.info.revision, size: file.info.size, limited: file.truncated)
            } catch FileDownloadError.serverNeedsUpdate {
                self?.finish(generation, .idle)
            } catch {
                self?.finish(generation, isCancellation(error) ? .cancelled : .failed(error.localizedDescription))
            }
            return nil
        }
        self.task = task
        return await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
    }

    /// Stops a download under way; the preview stays.
    func cancel() { task?.cancel() }

    private func finish(_ generation: Int, _ state: State) {
        guard self.generation == generation else { return }
        self.state = state
        task = nil
    }

    /// The UTF-8 text of a downloaded file, or nil when it isn't text. A copy
    /// cut at the byte limit ends at its last complete line, and is nil
    /// without one: its only line doesn't fit.
    nonisolated static func decode(_ data: Data, limited: Bool) -> String? {
        var data = data
        if limited {
            guard let newline = data.lastIndex(of: 0x0A) else { return nil }
            data = data.prefix(through: newline)
        }
        guard !data.contains(0) else { return nil }
        return String(data: data, encoding: .utf8)
    }

    /// The footer's note on the download, or nil when there's nothing to say.
    static func note(_ state: State, text: Text?) -> String? {
        switch state {
        case .downloading(let written, let size):
            let total = min(size ?? maxEditorBytes, maxEditorBytes)
            guard total > 0 else { return "Loading the whole file…" }
            return "Loading the whole file… \(Int(min(Double(written) / Double(total), 1) * 100))%"
        case .cancelled: return "Preview truncated"
        case .failed(let message): return "Preview truncated · " + message
        case .idle:
            guard let text, text.limited else { return nil }
            return "Showing the first " + ByteCountFormatter.string(fromByteCount: Int64(text.content.utf8.count), countStyle: .file)
        }
    }
}
