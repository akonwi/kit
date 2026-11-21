import Foundation
import Observation

/// Owned by one session workspace; evicts content, never tabs or editor positions.
@MainActor @Observable
final class FilePreviewCache {
    struct Entry {
        var preview: WireWorkspaceFileRead?
        var error: String?
        /// The file is binary, so it has no preview to retry for.
        var binary = false
        var loading = false
        var stale = true
        var token = UUID()
        /// The downloaded text that `preview` shows (ADR 0074).
        var whole: WholeFileDownload.Text?
        /// The Refresh count at which its download was cancelled, so it
        /// isn't loaded again until the next Refresh.
        var cancelled: Int?
    }
    private(set) var entries: [String: Entry] = [:]
    private var recent: [String] = []
    private(set) var revision = 0
    private var activity: [String]?
    /// Downloads of files, by path.
    private(set) var downloads: [String: WholeFileDownload] = [:]
    let byteLimit: Int
    let entryLimit: Int
    private let downloadStore: FileDownloadStore
    init(byteLimit: Int = 8_000_000, entryLimit: Int = 8, downloadStore: FileDownloadStore = .shared) {
        self.byteLimit = byteLimit; self.entryLimit = entryLimit; self.downloadStore = downloadStore
    }
    func observe(_ snapshot: SessionExcerpt) {
        let next = [snapshot.watchGeneration ?? "", snapshot.activeRunID ?? "", snapshot.activeBashID ?? "", snapshot.messages.last?.id ?? ""] +
            snapshot.messages.suffix(2).flatMap(\.tools).map { $0.id + ":" + ($0.status ?? "") } +
            (snapshot.subagents?.items ?? []).map { $0.name + ":" + $0.status + ":" + ($0.updatedAt ?? "") }
        defer { activity = next }
        guard let activity, activity != next else { return }
        for path in entries.keys { entries[path]?.stale = true }
        revision += 1
    }
    func invalidate(_ path: String) { entries[path]?.stale = true; revision += 1 }
    func clear() {
        downloads.values.forEach { $0.cancel() }
        entries = [:]; recent = []; activity = nil; downloads = [:]; revision += 1
    }
    func remove(_ path: String) {
        downloads.removeValue(forKey: path)?.cancel()
        entries.removeValue(forKey: path); recent.removeAll { $0 == path }
    }

    /// Loads a file with one download (ADR 0074). `refresh` counts the
    /// pane's Refreshes.
    func load(_ path: String, session: String, cwd: String, client: any FileDownloadClient, force: Bool = false,
              refresh: Int = 0) async {
        recent.removeAll { $0 == path }; recent.append(path)
        if !force, entries[path]?.preview != nil, entries[path]?.stale == false { return }
        if let cancelled = entries[path]?.cancelled, cancelled == refresh { return }
        let token = UUID()
        var entry = entries[path] ?? Entry()
        entry.token = token; entry.loading = true; entry.error = nil; entry.binary = false; entry.cancelled = nil
        entries[path] = entry
        defer { if entries[path]?.token == token { entries[path]?.loading = false } }
        do {
            let workspace = try await client.workspace(session)
            try Task.checkCancellation()
            guard entries[path]?.token == token else { return }
            guard workspace.cwd == cwd else { throw ClientError.invalidPayload }
            let download = downloads[path] ?? WholeFileDownload()
            downloads[path] = download
            let outcome = await download.load(.workspace(id: workspace.workspaceId, path: path), session: session, client: client,
                                              store: downloadStore)
            guard !Task.isCancelled, entries[path]?.token == token else { return }
            switch outcome {
            case .binary, .image:
                entries[path]?.error = WorkspaceFileError.response(415).localizedDescription
                entries[path]?.binary = true
                entries[path]?.stale = true
            case .stopped:
                if case .failed(let message) = download.state {
                    entries[path]?.error = message
                } else {
                    entries[path]?.error = "Loading canceled."
                    entries[path]?.cancelled = refresh
                }
                entries[path]?.stale = true
            case .text(let text):
                let shown = entries[path]?.preview
                // A copy the server says is current stays as it shows.
                if let revision = text.revision, let shown, shown.revision == revision,
                   shown.workspace.workspaceId == workspace.workspaceId, entries[path]?.whole?.limited == text.limited {
                    entries[path]?.stale = false
                    return
                }
                entries[path]?.preview = WireWorkspaceFileRead(
                    content: text.content, encoding: "utf-8", modifiedAt: text.modifiedAt, path: path,
                    returnedBytes: text.content.utf8.count, returnedLines: FileFind.searchedLines(text.content, truncated: true),
                    revision: text.revision ?? "", sessionId: session, size: text.size ?? Int64(text.content.utf8.count),
                    truncated: text.limited, truncationReason: text.limited ? "byte_limit" : nil, workspace: workspace)
                entries[path]?.whole = text
                entries[path]?.stale = false
                trim(keeping: path)
            }
        } catch is CancellationError {} catch {
            guard !Task.isCancelled, entries[path]?.token == token else { return }
            entries[path]?.error = error.localizedDescription
            entries[path]?.stale = true
        }
    }

    private func trim(keeping path: String) {
        while entries.values.compactMap(\.preview).count > entryLimit ||
                entries.values.compactMap(\.preview).reduce(0, { $0 + $1.content.utf8.count }) > byteLimit {
            guard let victim = recent.first(where: { $0 != path && entries[$0]?.preview != nil }) else { break }
            entries[victim]?.preview = nil
            entries[victim]?.whole = nil
            entries[victim]?.stale = true
            recent.removeAll { $0 == victim }
        }
    }
}
