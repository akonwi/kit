import Foundation
import Testing
@testable import Kit

private struct Counts: Equatable {
    var downloads = 0, checks = 0
}

/// A server with one file, which serves downloads unless it predates them.
private actor LongFileClient: FileDownloadClient {
    static let workspace = "workspace_" + String(repeating: "A", count: 43)
    static func revision(_ letter: Character) -> String { "file_" + String(repeating: letter, count: 43) }
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var whole: String
    var revision = LongFileClient.revision("B")
    let olderServer: Bool
    /// Whether downloads carry a revision; a file of unknown length doesn't.
    let revisioned: Bool
    /// Revisions the file takes as each download finishes, which checks
    /// then report.
    var changes: [String] = []
    /// Whether downloads wait until they are cancelled, and whether one is.
    var holding = false
    var held = false
    var downloads = 0
    var checks = 0
    /// Whether downloads fail as the server refuses them.
    var refusing = false
    func refuse() { refusing = true }
    /// Downloads and checks so far.
    var counts: Counts { Counts(downloads: downloads, checks: checks) }
    init(whole: String, olderServer: Bool = false, revisioned: Bool = true) {
        self.whole = whole; self.olderServer = olderServer; self.revisioned = revisioned
    }
    func change(to whole: String, revision: String? = nil) {
        self.whole = whole
        if let revision { self.revision = revision }
    }
    func changeWhileDownloading(_ revisions: [String]) { changes = revisions }
    /// Content the file takes once its first download finishes.
    var next: String?
    func becomeAfterDownload(_ content: String) { next = content }
    func hold() { holding = true }
    func waitForHeld() async { while !held { await Task.yield() } }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func workspace(_ session: String) async throws -> WireWorkspaceRef {
        try JSONDecoder().decode(WireWorkspaceRef.self, from: Data("""
        {"sessionId":"\(session)","cwd":"/tmp","workspaceId":"\(Self.workspace)","state":"ready","limits":{"maxPathBytes":4096,"maxPathComponents":64,"defaultDirectoryPageSize":100,"maxDirectoryPageSize":200,"maxDirectoryEntries":10000,"maxDirectoryResponseBytes":524288,"maxDirectoryObservationBytes":8388608,"maxPreviewBytes":1000000,"maxPreviewLines":5000,"maxActiveRequests":1,"maxPendingRequests":0}}
        """.utf8))
    }
    func downloadFile(_ session: String, target: FileDownloadTarget, cachedRevision: String?, maxBytes: Int64?,
                      to destination: URL, progress: (@Sendable (Int64, Int64?) -> Void)?) async throws -> FileDownloadResult {
        downloads += 1
        if olderServer { throw FileDownloadError.serverNeedsUpdate }
        if refusing { throw FileDownloadError.response(403) }
        if holding {
            held = true
            while true { try await Task.sleep(for: .milliseconds(5)) }
        }
        guard case .workspace(Self.workspace, let path) = target else { throw ClientError.invalidPayload }
        var served = Data(whole.utf8)
        let truncated = maxBytes.map { served.count > $0 } ?? false
        if truncated, let maxBytes { served = served.prefix(Int(maxBytes)) }
        try served.write(to: destination)
        let info = FileDownloadInfo(session: session, workspace: Self.workspace, path: path, revision: revisioned ? revision : nil,
                                    size: revisioned ? Int64(whole.utf8.count) : nil, modifiedAt: "2026-11-18T12:00:00Z")
        if !changes.isEmpty { revision = changes.removeFirst() }
        if let next { whole = next; self.next = nil }
        return FileDownloadResult(info: info, notModified: false, bytes: Int64(served.count), truncated: truncated)
    }
    func checkFile(_ session: String, target: FileDownloadTarget) async throws -> FileDownloadInfo {
        checks += 1
        guard case .workspace(_, let path) = target else { throw ClientError.invalidPayload }
        return FileDownloadInfo(session: session, workspace: Self.workspace, path: path, revision: revision, size: nil,
                                modifiedAt: "2026-11-18T12:00:00Z")
    }
}

@MainActor struct FilePreviewCacheTests {
    static func store() -> FileDownloadStore {
        FileDownloadStore(directory: FileManager.default.temporaryDirectory.appendingPathComponent("kit-preview-test-" + UUID().uuidString),
                          maxTotalBytes: 1 << 20)
    }

    @Test func aFileLoadsWithOneDownload() async {
        let client = LongFileClient(whole: "one\ntwo\nthree\n"), cache = FilePreviewCache(downloadStore: Self.store())
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        let preview = cache.entries["a"]?.preview
        #expect(preview?.content == "one\ntwo\nthree\n")
        #expect(preview?.truncated == false && preview?.returnedBytes == 14 && preview?.revision == LongFileClient.revision("B"))
        #expect(preview?.workspace.workspaceId == LongFileClient.workspace && preview?.path == "a")
        #expect(cache.entries["a"]?.whole?.content == "one\ntwo\nthree\n")
        #expect(cache.downloads["a"]?.state == .idle)
        #expect(await client.counts == Counts(downloads: 1, checks: 1))

        // Rereading an unchanged file keeps it on screen.
        await cache.load("a", session: "s", cwd: "/tmp", client: client, force: true)
        #expect(cache.entries["a"]?.preview?.content == "one\ntwo\nthree\n")
        #expect(await client.counts == Counts(downloads: 2, checks: 2))

        // A changed file replaces it.
        await client.change(to: "four\n", revision: LongFileClient.revision("C"))
        await cache.load("a", session: "s", cwd: "/tmp", client: client, force: true)
        #expect(cache.entries["a"]?.preview?.content == "four\n")
        #expect(cache.entries["a"]?.preview?.revision == LongFileClient.revision("C"))
    }

    @Test func aWholeFileWithoutARevisionIsDownloadedAgainOnReread() async {
        let client = LongFileClient(whole: "one\ntwo\n", revisioned: false), cache = FilePreviewCache(downloadStore: Self.store())
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        #expect(cache.entries["a"]?.preview?.content == "one\ntwo\n")
        #expect(cache.entries["a"]?.preview?.revision == "")
        await client.change(to: "one\ntwo\nthree\n")
        await cache.load("a", session: "s", cwd: "/tmp", client: client, force: true)
        #expect(cache.entries["a"]?.preview?.content == "one\ntwo\nthree\n")
        #expect(cache.entries["a"]?.whole?.revision == nil)
        #expect(await client.counts == Counts(downloads: 2, checks: 0))
    }

    @Test func anOlderServerSaysItNeedsAnUpdate() async {
        let client = LongFileClient(whole: "one\ntwo\n", olderServer: true), cache = FilePreviewCache(downloadStore: Self.store())
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        #expect(cache.entries["a"]?.preview == nil)
        #expect(cache.entries["a"]?.error == "This server’s Kit needs an update to open files.")
        #expect(cache.entries["a"]?.binary == false)
    }

    @Test func aBinaryFileIsMarkedSoThePaneOffersNoRetry() async {
        let client = LongFileClient(whole: "a\u{0}b"), cache = FilePreviewCache(downloadStore: Self.store())
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        #expect(cache.entries["a"]?.preview == nil)
        #expect(cache.entries["a"]?.error == "Binary files cannot be previewed.")
        #expect(cache.entries["a"]?.binary == true)
        // A file that becomes text shows once it is read again.
        await client.change(to: "one\n", revision: LongFileClient.revision("C"))
        await cache.load("a", session: "s", cwd: "/tmp", client: client, force: true)
        #expect(cache.entries["a"]?.preview?.content == "one\n")
        #expect(cache.entries["a"]?.error == nil && cache.entries["a"]?.binary == false)
    }

    @Test func aFirstLinePastTheLimitIsCutAtACharacter() async {
        let limit = Int(WholeFileDownload.maxEditorBytes)
        let client = LongFileClient(whole: String(repeating: "x", count: limit - 1) + "é" + "yy")
        let cache = FilePreviewCache(byteLimit: 16 << 20, downloadStore: Self.store())
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        let preview = cache.entries["a"]?.preview
        #expect(preview?.content.utf8.count == limit - 1)
        #expect(preview?.truncated == true && preview?.returnedLines == 1)
        #expect(cache.entries["a"]?.whole?.limited == true)
    }

    @Test func aFileThatChangesWhileItDownloadsDownloadsAgain() async {
        let client = LongFileClient(whole: "one\n"), cache = FilePreviewCache(downloadStore: Self.store())
        await client.changeWhileDownloading([LongFileClient.revision("C")])
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        #expect(cache.entries["a"]?.preview?.revision == LongFileClient.revision("C"))
        #expect(cache.entries["a"]?.whole?.changed == false)
        #expect(await client.counts == Counts(downloads: 2, checks: 2))
    }

    @Test func aFileThatKeepsChangingIsKeptWithoutARevision() async {
        let client = LongFileClient(whole: "one\n"), cache = FilePreviewCache(downloadStore: Self.store())
        await client.changeWhileDownloading([LongFileClient.revision("C"), LongFileClient.revision("D")])
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        #expect(cache.entries["a"]?.preview?.content == "one\n")
        #expect(cache.entries["a"]?.preview?.revision == "")
        #expect(cache.entries["a"]?.whole?.changed == true)
        #expect(FileFooter.truncation(previewTruncated: false, whole: cache.entries["a"]?.whole, download: cache.downloads["a"])
                == " · Changed while loading")
        #expect(await client.downloads == 2)
    }

    @Test func aRetryThatIsntTextKeepsTheChangedCopy() async {
        let client = LongFileClient(whole: "one\n"), cache = FilePreviewCache(downloadStore: Self.store())
        await client.changeWhileDownloading([LongFileClient.revision("C")])
        await client.becomeAfterDownload("a\u{0}b")
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        #expect(cache.entries["a"]?.preview?.content == "one\n")
        #expect(cache.entries["a"]?.preview?.revision == "")
        #expect(cache.entries["a"]?.whole?.changed == true)
        #expect(await client.counts == Counts(downloads: 2, checks: 1))
    }

    @Test func aCancelledDownloadWaitsForARefresh() async throws {
        let client = LongFileClient(whole: "one\n"), cache = FilePreviewCache(downloadStore: Self.store())
        await client.hold()
        let loading = Task { await cache.load("a", session: "s", cwd: "/tmp", client: client) }
        await client.waitForHeld()
        cache.downloads["a"]?.cancel()
        await loading.value
        #expect(cache.entries["a"]?.error == "Loading canceled.")
        #expect(cache.entries["a"]?.preview == nil)
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        #expect(await client.counts == Counts(downloads: 1, checks: 0))
    }

    @Test func aFailedReloadKeepsWhatShows() async {
        let client = LongFileClient(whole: "one\n"), cache = FilePreviewCache(downloadStore: Self.store())
        await cache.load("a", session: "s", cwd: "/tmp", client: client)
        await client.refuse()
        await cache.load("a", session: "s", cwd: "/tmp", client: client, force: true)
        #expect(cache.entries["a"]?.preview?.content == "one\n")
        #expect(cache.entries["a"]?.stale == true)
        #expect(cache.entries["a"]?.error == "Kit doesn’t have permission to read this file.")
    }

    @Test func cacheEvictsContentAndActivityInvalidatesWithoutProseChurn() async {
        let client = LongFileClient(whole: "hello"), cache = FilePreviewCache(byteLimit: 8, entryLimit: 2, downloadStore: Self.store())
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
}
