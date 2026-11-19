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

/// A server whose file is longer than its preview, and which may predate
/// downloads.
private actor LongFileClient: WorkspaceFileClient, FileDownloadClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var whole: String
    let olderServer: Bool
    /// Whether downloads carry a revision; a file of unknown length doesn't.
    let revisioned: Bool
    var downloads = 0
    init(whole: String, olderServer: Bool = false, revisioned: Bool = true) {
        self.whole = whole; self.olderServer = olderServer; self.revisioned = revisioned
    }
    func change(to whole: String) { self.whole = whole }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func readWorkspaceFile(_ session: String, path: String, expectedRevision: String?) async throws -> WireWorkspaceFileRead {
        try JSONDecoder().decode(WireWorkspaceFileRead.self, from: Data("""
        {"sessionId":"\(session)","workspace":{"sessionId":"\(session)","cwd":"/tmp","workspaceId":"workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","state":"ready","limits":{"maxPathBytes":4096,"maxPathComponents":64,"defaultDirectoryPageSize":100,"maxDirectoryPageSize":200,"maxDirectoryEntries":10000,"maxDirectoryResponseBytes":524288,"maxDirectoryObservationBytes":8388608,"maxPreviewBytes":1000000,"maxPreviewLines":5000,"maxActiveRequests":1,"maxPendingRequests":0}},"path":"\(path)","revision":"file_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB","size":\(whole.utf8.count),"encoding":"utf-8","content":"one\\n","returnedBytes":4,"returnedLines":1,"truncated":true,"truncationReason":"line_limit"}
        """.utf8))
    }
    func downloadFile(_ session: String, target: FileDownloadTarget, cachedRevision: String?, maxBytes: Int64?,
                      to destination: URL, progress: (@Sendable (Int64, Int64?) -> Void)?) async throws -> FileDownloadResult {
        downloads += 1
        if olderServer { throw FileDownloadError.serverNeedsUpdate }
        guard case .workspace("workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", let path) = target else { throw ClientError.invalidPayload }
        try Data(whole.utf8).write(to: destination)
        let info = FileDownloadInfo(session: session, workspace: "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", path: path, revision: revisioned ? "file_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB" : nil,
                                    size: revisioned ? Int64(whole.utf8.count) : nil, modifiedAt: "2026-11-18T12:00:00Z")
        return FileDownloadResult(info: info, notModified: false, bytes: Int64(whole.utf8.count), truncated: false)
    }
}

@MainActor struct FilePreviewCacheTests {
    static func store() -> FileDownloadStore {
        FileDownloadStore(directory: FileManager.default.temporaryDirectory.appendingPathComponent("kit-preview-test-" + UUID().uuidString),
                          maxTotalBytes: 1 << 20)
    }

    @Test func aCutOffPreviewIsReplacedByTheWholeFile() async {
        let client = LongFileClient(whole: "one\ntwo\nthree\n"), cache = FilePreviewCache(downloadStore: Self.store())
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        let preview = cache.entries["a"]?.preview
        #expect(preview?.content == "one\ntwo\nthree\n")
        #expect(preview?.truncated == false && preview?.returnedLines == 3 && preview?.returnedBytes == 14)
        #expect(cache.entries["a"]?.whole == WholeFileDownload.Text(content: "one\ntwo\nthree\n", revision: "file_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", size: 14, limited: false))
        #expect(cache.downloads["a"]?.state == .idle)

        // Rereading the same revision keeps the whole file on screen.
        await cache.load("a", session: "s", cwd: "/tmp", client: client, force: true)
        #expect(cache.entries["a"]?.preview?.content == "one\ntwo\nthree\n")
        #expect(await client.downloads == 1)
    }

    @Test func aWholeFileWithoutARevisionIsDownloadedAgainOnReread() async {
        let client = LongFileClient(whole: "one\ntwo\n", revisioned: false), cache = FilePreviewCache(downloadStore: Self.store())
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        #expect(cache.entries["a"]?.preview?.content == "one\ntwo\n")
        await client.change(to: "one\ntwo\nthree\n")
        await cache.load("a", session: "s", cwd: "/tmp", client: client, force: true)
        #expect(cache.entries["a"]?.preview?.content == "one\ntwo\nthree\n")
        #expect(cache.entries["a"]?.whole == WholeFileDownload.Text(content: "one\ntwo\nthree\n", revision: nil, size: nil, limited: false))
        #expect(await client.downloads == 2)
    }

    @Test func anOlderServerKeepsThePreview() async {
        let client = LongFileClient(whole: "one\ntwo\n", olderServer: true), cache = FilePreviewCache(downloadStore: Self.store())
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        #expect(cache.entries["a"]?.preview?.content == "one\n")
        #expect(cache.entries["a"]?.preview?.truncated == true)
        #expect(cache.entries["a"]?.whole == nil)
        #expect(cache.downloads["a"]?.state == .idle)
        #expect(await client.downloads == 1)
    }

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
