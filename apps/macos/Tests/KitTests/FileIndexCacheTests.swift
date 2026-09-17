import Foundation
import Testing
@testable import Kit

private actor IndexLoader {
    var calls = 0
    var refreshes: [Bool] = []
    func read(_ refresh: Bool) async throws -> FileIndex {
        calls += 1; refreshes.append(refresh)
        try await Task.sleep(for: .milliseconds(80))
        return FileIndex(paths: ["src/main.swift"], cwd: "/tmp", truncated: true)
    }
}

struct FileIndexCacheTests {
    let key = FileIndexCache.Key(server: "server", session: "session", cwd: "/tmp")

    @Test func olderIndexResponsesDefaultToComplete() throws {
        let data = Data(#"{"sessionId":"session_test","cwd":"/tmp","entries":[]}"#.utf8)
        let index = try JSONDecoder().decode(WireSessionFileIndex.self, from: data)
        #expect(index.truncated == false)
        #expect(index.cwd == "/tmp")
    }

    @Test func consumersShareRequestsAndRefreshReplacesCachedResults() async throws {
        let cache = FileIndexCache(), loader = IndexLoader()
        async let picker = cache.load(key: key, loader: loader.read)
        async let mention = cache.load(key: key, loader: loader.read)
        let results = try await [picker, mention]
        #expect(results.map(\.paths) == [["src/main.swift"], ["src/main.swift"]])
        #expect(results.map(\.truncated) == [true, true])
        #expect(await loader.calls == 1)
        _ = try await cache.load(key: key, loader: loader.read)
        #expect(await loader.calls == 1)
        _ = try await cache.load(key: key, refresh: true, loader: loader.read)
        #expect(await loader.refreshes == [false, true])
    }

    @Test func closingOneConsumerDoesNotCancelTheOther() async throws {
        let cache = FileIndexCache(), loader = IndexLoader()
        let first = Task { try await cache.load(key: key, loader: loader.read) }
        let second = Task { try await cache.load(key: key, loader: loader.read) }
        try await Task.sleep(for: .milliseconds(20))
        first.cancel()
        await #expect(throws: CancellationError.self) { try await first.value }
        #expect(try await second.value.paths == ["src/main.swift"])
        #expect(await loader.calls == 1)
    }

    @Test func changedIdentityRejectsLateResultsAndWrongDirectories() async throws {
        let cache = FileIndexCache(), loader = IndexLoader()
        let old = Task { try await cache.load(key: key) { _ in
            try? await Task.sleep(for: .milliseconds(60))
            return FileIndex(paths: ["old"])
        } }
        try await Task.sleep(for: .milliseconds(20))
        let other = FileIndexCache.Key(server: "other", session: "session", cwd: "/tmp")
        #expect(try await cache.load(key: other, loader: loader.read).paths == ["src/main.swift"])
        await #expect(throws: CancellationError.self) { try await old.value }
        let wrong = FileIndexCache.Key(server: "other", session: "session", cwd: "/changed")
        await #expect(throws: ClientError.self) { try await cache.load(key: wrong, loader: loader.read) }
    }

    @MainActor @Test func mentionShowsIncompleteIndexNotice() async throws {
        let mentions = ComposerMentionState()
        mentions.observe(previous: "", next: "@", pasted: false)
        mentions.load { _ in FileIndex(paths: ["README.md"], truncated: true) }
        for _ in 0..<100 where mentions.loading { try await Task.sleep(for: .milliseconds(5)) }
        #expect(mentions.matches == ["README.md"])
        #expect(mentions.indexNotice == "File index is incomplete; some paths may be missing.")
    }
}
