import Foundation
import Testing
@testable import Kit

private actor PreviewClient: WorkspaceFileClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var revision = "one"
    var expected: [String?] = []
    var fail = false
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func change(_ revision: String, fail: Bool = false) { self.revision = revision; self.fail = fail }
    func readWorkspaceFile(_ session: String, path: String, expectedRevision: String?) async throws -> WireWorkspaceFileRead {
        expected.append(expectedRevision)
        if fail { throw WorkspaceFileError.response(403) }
        if let expectedRevision, expectedRevision != revision { throw WorkspaceFileError.response(409) }
        return try JSONDecoder().decode(WireWorkspaceFileRead.self, from: Data("""
        {"sessionId":"\(session)","workspace":{"sessionId":"\(session)","cwd":"/tmp","workspaceId":"workspace_test","state":"ready","limits":{"maxPathBytes":4096,"maxPathComponents":64,"defaultDirectoryPageSize":100,"maxDirectoryPageSize":200,"maxDirectoryEntries":10000,"maxDirectoryResponseBytes":524288,"maxDirectoryObservationBytes":8388608,"maxPreviewBytes":1000000,"maxPreviewLines":5000,"maxActiveRequests":1,"maxPendingRequests":0}},"path":"\(path)","revision":"\(revision)","size":5,"encoding":"utf-8","content":"hello","returnedBytes":5,"returnedLines":1,"truncated":false}
        """.utf8))
    }
}

@MainActor struct FilePreviewCacheTests {
    @Test func guardedRefreshRecoversOnceAndRetainsOldContentOnFailure() async {
        let client = PreviewClient(), cache = FilePreviewCache()
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        await client.change("two")
        await cache.load("a", session: "s", cwd: "/tmp", client: client, force: true)
        #expect(await client.expected == [nil, "one", nil])
        #expect(cache.entries["a"]?.preview?.revision == "two")
        #expect(cache.entries["a"]?.stale == false)
        await client.change("three", fail: true)
        await cache.load("a", session: "s", cwd: "/tmp", client: client, force: true)
        #expect(cache.entries["a"]?.preview?.revision == "two")
        #expect(cache.entries["a"]?.stale == true)
        #expect(cache.entries["a"]?.error == "This file cannot be read from this workspace.")
    }
    @Test func cacheEvictsContentAndActivityInvalidatesWithoutProseChurn() async {
        let client = PreviewClient(), cache = FilePreviewCache(byteLimit: 8, entryLimit: 2)
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        await cache.load("b", session: "s", cwd: "/tmp", client: client)
        #expect(cache.entries.values.compactMap(\.preview).map(\.path) == ["b"])
        var snapshot = SessionExcerpt(id: "s", title: "", sourceTitle: "", model: "", thinking: "", workspace: "", date: "", messages: [])
        cache.observe(snapshot)
        snapshot.activeRunID = "run"
        cache.observe(snapshot)
        #expect(cache.entries["b"]?.stale == true)
        #expect(cache.revision == 1)
        cache.observe(snapshot)
        #expect(cache.revision == 1)
        cache.remove("b")
        #expect(cache.entries["b"] == nil)
        cache.clear()
        #expect(cache.entries.isEmpty)
    }
    @Test func gateSerializesReads() async throws {
        actor Counter {
            var active = 0, peak = 0
            func begin() { active += 1; peak = max(peak, active) }
            func end() { active -= 1 }
        }
        let gate = WorkspaceReadGate(), counter = Counter()
        try await withThrowingTaskGroup(of: Void.self) { group in
            for _ in 0..<4 {
                group.addTask {
                    try await gate.read {
                        await counter.begin()
                        try await Task.sleep(for: .milliseconds(30))
                        await counter.end()
                    }
                }
            }
            try await group.waitForAll()
        }
        #expect(await counter.peak == 1)
    }
}
