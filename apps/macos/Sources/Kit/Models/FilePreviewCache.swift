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
    }
    private(set) var entries: [String: Entry] = [:]
    private var recent: [String] = []
    private(set) var revision = 0
    private var activity: [String]?
    let byteLimit: Int
    let entryLimit: Int
    init(byteLimit: Int = 8_000_000, entryLimit: Int = 8) {
        self.byteLimit = byteLimit; self.entryLimit = entryLimit
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
    func clear() { entries = [:]; recent = []; activity = nil; revision += 1 }
    func remove(_ path: String) { entries.removeValue(forKey: path); recent.removeAll { $0 == path } }

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
            entries[path]?.preview = value
            entries[path]?.stale = false
            trim(keeping: path)
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
            entries[victim]?.stale = true
            recent.removeAll { $0 == victim }
        }
    }
}
