import Foundation
import Observation

/// Owned by one session workspace; evicts content, never tabs or editor positions.
@MainActor @Observable
final class FilePreviewCache {
    struct Entry {
        var preview: WireWorkspaceFileRead?
        var error: String?
        var loading = false
        var stale = true
        var token = UUID()
        /// The whole file, downloaded because the preview was cut off; it
        /// replaces the preview's content (ADR 0074).
        var whole: WholeFileDownload.Text?
    }
    private(set) var entries: [String: Entry] = [:]
    private var recent: [String] = []
    private(set) var revision = 0
    private var activity: [String]?
    /// Downloads of whole files, by path.
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

    func load(_ path: String, session: String, cwd: String, client: any WorkspaceFileClient, force: Bool = false) async {
        recent.removeAll { $0 == path }; recent.append(path)
        if !force, entries[path]?.preview != nil, entries[path]?.stale == false { return }
        let token = UUID()
        var entry = entries[path] ?? Entry()
        let expected = entry.preview?.revision
        entry.token = token; entry.loading = true; entry.error = nil
        entries[path] = entry
        defer { if entries[path]?.token == token { entries[path]?.loading = false } }
        do {
            let value: WireWorkspaceFileRead
            do {
                value = try await client.readWorkspaceFile(session, path: path, expectedRevision: expected)
            } catch WorkspaceFileError.response(409) where expected != nil {
                // The guarded observation changed. Fetch one fresh bounded snapshot;
                // another concurrent change is surfaced rather than retried forever.
                try Task.checkCancellation()
                value = try await client.readWorkspaceFile(session, path: path, expectedRevision: nil)
            }
            try Task.checkCancellation()
            guard entries[path]?.token == token else { return }
            guard value.sessionId == session, value.path == path, value.workspace.cwd == cwd else { throw ClientError.invalidPayload }
            // A reread of a file whose whole text already shows keeps it
            // while the revision is unchanged, rather than flashing back to
            // the preview. A download without a revision can't be validated
            // that way, so it's fetched again.
            if value.truncated, let shown = entries[path]?.preview, shown.revision == value.revision,
               shown.workspace.workspaceId == value.workspace.workspaceId, let whole = entries[path]?.whole {
                entries[path]?.stale = false
                if whole.revision == nil, let downloads = client as? any FileDownloadClient {
                    await loadWhole(path, preview: value, session: session, token: token, client: downloads)
                }
                return
            }
            entries[path]?.preview = value
            entries[path]?.whole = nil
            entries[path]?.stale = false
            trim(keeping: path)
            if value.truncated, let downloads = client as? any FileDownloadClient {
                await loadWhole(path, preview: value, session: session, token: token, client: downloads)
            }
        } catch is CancellationError {} catch {
            guard !Task.isCancelled, entries[path]?.token == token else { return }
            entries[path]?.error = error.localizedDescription
            entries[path]?.stale = true
        }
    }
    /// Downloads the whole of a cut-off file and shows it in place of the
    /// preview. The preview stays when the download can't replace it.
    private func loadWhole(_ path: String, preview: WireWorkspaceFileRead, session: String, token: UUID, client: any FileDownloadClient) async {
        let download = downloads[path] ?? WholeFileDownload()
        downloads[path] = download
        guard let whole = await download.load(.workspace(id: preview.workspace.workspaceId, path: path), session: session, client: client,
                                               store: downloadStore),
              entries[path]?.token == token else { return }
        var value = preview
        value.content = whole.content
        value.returnedBytes = whole.content.utf8.count
        value.returnedLines = FileFind.searchedLines(whole.content, truncated: true)
        value.truncated = whole.limited
        value.truncationReason = whole.limited ? "byte_limit" : nil
        value.highlight = nil
        if let revision = whole.revision { value.revision = revision }
        if let size = whole.size { value.size = size }
        entries[path]?.preview = value
        entries[path]?.whole = whole
        trim(keeping: path)
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
