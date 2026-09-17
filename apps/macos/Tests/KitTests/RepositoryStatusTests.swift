import Foundation
import Testing
@testable import Kit

private actor RepositoryClient: SessionVCSClient {
    nonisolated let serverID = "vcs-test"
    nonisolated let isDemo = false
    var receiver: (@Sendable (SessionExcerpt) async -> Void)?
    var calls = 0
    var status: WireVCSStatus? = .init(root: "/tmp/repo", head: .init(kind: .value0, name: "main", oid: nil), dirty: false)
    var fail = false
    var delay = false
    func sessions() async throws -> [SessionExcerpt] { [Self.value] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { Self.value }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        receiver = receive
        await receive(Self.value)
        try await Task.sleep(for: .seconds(60))
    }
    func vcs(_ id: String) async throws -> WireSessionVCSStatus {
        calls += 1
        if delay { try? await Task.sleep(for: .milliseconds(400)) }
        if fail { throw ClientError.http(500) }
        return .init(sessionId: id, cwd: "/tmp/repo", status: status)
    }
    func delayResponses() { delay = true }
    func change(_ status: WireVCSStatus?, fail: Bool = false) { self.status = status; self.fail = fail }
    func emit(_ value: SessionExcerpt) async { await receiver?(value) }
    static let value = SessionExcerpt(id: "session", title: "Repository", sourceTitle: "Test", model: "p/m",
        thinking: "off", workspace: "repo", cwd: "/tmp/repo", date: "", messages: [])
}

@MainActor struct RepositoryStatusTests {
    private func settle() async throws { try await Task.sleep(for: .milliseconds(350)) }

    private func eventually(_ condition: () async -> Bool) async throws {
        for _ in 0..<100 {
            if await condition() { return }
            try await Task.sleep(for: .milliseconds(50))
        }
        #expect(await condition())
    }

    @Test func initialStatusToolCompletionReconnectAndQuietFallback() async throws {
        let client = RepositoryClient()
        let replica = SessionReplica(sessions: [RepositoryClient.value], client: client)
        replica.attach()
        defer { replica.detach() }
        try await eventually { WorkspaceLocation.repositoryLabel(replica.snapshot) == "main" }
        #expect(WorkspaceLocation.repositoryLabel(replica.snapshot) == "main")
        var update = RepositoryClient.value
        update.messages = [.init(id: "tool", role: "assistant", text: "", tools: [
            .init(id: "edit", name: "edit", summary: "file", output: "", failed: false, status: "Running…")])]
        await client.emit(update)
        try await settle()
        await client.change(.init(root: "/tmp/repo", head: .init(kind: .value1, name: nil, oid: "abcdef0123456789"), dirty: true))
        update.messages[0].tools = [.init(id: "edit", name: "edit", summary: "file", output: "done", failed: false, status: "Completed")]
        await client.emit(update)
        try await settle()
        #expect(WorkspaceLocation.repositoryLabel(replica.snapshot) == "detached abcdef01*")
        let count = await client.calls
        update.messages[0].text = "Streaming prose does not invalidate Git state"
        await client.emit(update)
        try await settle()
        #expect(await client.calls == count)
        await client.change(nil)
        update.watchGeneration = "resynchronized"
        await client.emit(update)
        try await settle()
        #expect(replica.snapshot?.gitHead == nil)
        #expect(await client.calls == count + 1)
        replica.attach()
        try await settle()
        #expect(replica.snapshot?.gitHead == nil)
        #expect(replica.connectionState == .connected)
        await client.change(nil, fail: true)
        replica.attach()
        try await settle()
        #expect(replica.snapshot?.gitDirty == nil)
        #expect(replica.connectionState == .connected)
    }

    @Test func lateRepositoryResponseCannotPopulateAnotherSession() async throws {
        let client = RepositoryClient()
        await client.delayResponses()
        let replica = SessionReplica(sessions: [RepositoryClient.value], client: client)
        replica.attach()
        try await eventually { await client.calls == 1 }
        #expect(await client.calls == 1)
        replica.select("other-session")
        try await Task.sleep(for: .milliseconds(450))
        #expect(replica.selectedID == "other-session")
        #expect(replica.snapshot == nil)
        #expect(replica.connectionState == .disconnected)
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_VCS_LIVE_TEST"] == "1"))
    func liveFooterUsesServerRepository() async throws {
        let http = try await HTTPClient.local()
        let sessions = try await http.sessions()
        let session = try #require(sessions.first { $0.cwd?.contains("kit-v2") == true })
        let expected = try await http.vcs(session.id)
        _ = try #require(expected.status)
        let replica = SessionReplica(sessions: [session], client: LocalClient())
        replica.attach()
        defer { replica.detach() }
        for _ in 0..<50 {
            if replica.snapshot?.gitHead != nil { break }
            try await Task.sleep(for: .milliseconds(100))
        }
        #expect(replica.snapshot?.gitHead == expected.status?.head.name ?? expected.status?.head.oid.map { String($0.prefix(8)) })
        #expect(replica.snapshot?.gitDirty == expected.status?.dirty)
        #expect(replica.snapshot?.gitHeadKind == expected.status?.head.kind.rawValue)
    }

    @Test func detachedAndUnbornLabels() {
        var session = RepositoryClient.value
        session.gitHead = "new-branch"; session.gitHeadKind = "unborn"; session.gitDirty = true
        #expect(WorkspaceLocation.repositoryLabel(session) == "new-branch*")
        session.gitHead = "01234567"; session.gitHeadKind = "detached"; session.gitDirty = false
        #expect(WorkspaceLocation.repositoryLabel(session) == "detached 01234567")
    }
}
