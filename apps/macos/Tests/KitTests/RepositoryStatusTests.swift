import Foundation
import Testing
@testable import Kit

private actor RepositoryClient: SessionVCSClient {
    nonisolated let serverID = "vcs-test"
    nonisolated let isDemo = false
    var receiver: (@Sendable (SessionExcerpt) async -> Void)?
    var calls = 0
    var status: WireVCSStatus? = .init(pullRequest: nil, root: "/tmp/repo", head: .init(kind: .value0, name: "main", oid: nil), dirty: false)
    var fail = false
    var delay = false
    func sessions() async throws -> [SessionExcerpt] { [Self.value] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { Self.value }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        receiver = receive
        await receive(Self.value)
        try await Task.sleep(for: .seconds(60))
    }
    private var subscribers: [UUID: AsyncThrowingStream<WireSessionVCSStatus, Error>.Continuation] = [:]
    var snapshotReads = 0
    func vcs(_ id: String) async throws -> WireSessionVCSStatus {
        snapshotReads += 1
        return .init(sessionId: id, cwd: resultCWD, status: status)
    }
    func watchVCS(_ id: String, receive: @escaping @Sendable (WireSessionVCSStatus) async -> Void) async throws {
        calls += 1
        let token = UUID()
        let (stream, continuation) = AsyncThrowingStream<WireSessionVCSStatus, Error>.makeStream()
        subscribers[token] = continuation
        defer { subscribers.removeValue(forKey: token); continuation.finish() }
        let initial = WireSessionVCSStatus(sessionId: id, cwd: resultCWD, status: status)
        if slowFirst && calls == 1 { try? await Task.sleep(for: .milliseconds(600)) }
        else if delay { try? await Task.sleep(for: .milliseconds(400)) }
        if fail { throw ClientError.http(500) }
        await receive(initial)
        for try await update in stream { await receive(update) }
    }
    var resultCWD = "/tmp/repo"
    var slowFirst = false
    func delayResponses() { delay = true }
    func delayFirstResponse() { slowFirst = true }
    func change(_ status: WireVCSStatus?, fail: Bool = false) {
        self.status = status; self.fail = fail
        for continuation in subscribers.values {
            if fail { continuation.finish(throwing: ClientError.http(500)) }
            else { continuation.yield(.init(sessionId: "session", cwd: resultCWD, status: status)) }
        }
    }
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

    @Test func initialStatusPushUpdatesAndReconnectWithoutPolling() async throws {
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
        await client.change(.init(pullRequest: nil, root: "/tmp/repo", head: .init(kind: .value1, name: nil, oid: "abcdef0123456789"), dirty: true))
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
        #expect(await client.calls == count)
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

    @Test func staleResponseCannotRestorePullRequestAfterDirectoryChange() async throws {
        let client = RepositoryClient()
        await client.change(.init(
            pullRequest: .init(number: 55, url: "https://github.com/akonwi/kit/pull/55"),
            root: "/tmp/repo", head: .init(kind: .value0, name: "main", oid: nil), dirty: false))
        await client.delayResponses()
        let replica = SessionReplica(sessions: [RepositoryClient.value], client: client)
        replica.attach()
        defer { replica.detach() }
        try await eventually { replica.connectionState == .connected }
        // The session changes directory while a pull-request-bearing response
        // for the old cwd is still in flight.
        var moved = RepositoryClient.value
        moved.cwd = "/tmp/other"
        await client.emit(moved)
        try await eventually { replica.snapshot?.cwd == "/tmp/other" }
        try await Task.sleep(for: .milliseconds(900))
        #expect(replica.snapshot?.cwd == "/tmp/other")
        #expect(replica.snapshot?.pullRequestNumber == nil)
        #expect(replica.snapshot?.pullRequestURL == nil)
        #expect(WorkspaceLocation.repositoryLabel(replica.snapshot) == nil)
    }

    @Test func sameDirectorySnapshotDeliveriesPreservePullRequest() async throws {
        let client = RepositoryClient()
        await client.change(.init(
            pullRequest: .init(number: 7, url: "https://github.com/akonwi/kit/pull/7"),
            root: "/tmp/repo", head: .init(kind: .value0, name: "main", oid: nil), dirty: false))
        let replica = SessionReplica(sessions: [RepositoryClient.value], client: client)
        replica.attach()
        defer { replica.detach() }
        try await eventually { replica.snapshot?.pullRequestNumber == 7 }
        // Later responses stall so a merge bug cannot be hidden by a fast refresh.
        await client.delayResponses()
        var update = RepositoryClient.value
        update.messages = [.init(id: "note", role: "assistant", text: "Unrelated snapshot delivery", tools: [])]
        await client.emit(update)
        try await Task.sleep(for: .milliseconds(150))
        #expect(replica.snapshot?.pullRequestNumber == 7)
        #expect(replica.snapshot?.pullRequestURL == "https://github.com/akonwi/kit/pull/7")
        #expect(WorkspaceLocation.repositoryLabel(replica.snapshot) == "main · PR #7")
    }

    @Test func pushSubscriptionStopsWithDetachmentWithoutPolling() async throws {
        let client = RepositoryClient()
        let replica = SessionReplica(sessions: [RepositoryClient.value], client: client)
        replica.attach()
        try await eventually { await client.calls == 1 }
        try await eventually { replica.snapshot?.gitHead == "main" }
        replica.detach()
        await client.change(.init(pullRequest: nil, root: "/tmp/repo", head: .init(kind: .value0, name: "late", oid: nil), dirty: true))
        try await Task.sleep(for: .milliseconds(200))
        #expect(await client.calls == 1)
        #expect(await client.snapshotReads == 0)
        #expect(replica.snapshot?.gitHead == "main")
    }

    @Test func heldResponseAfterCwdRoundTripCannotResurrectOldPullRequest() async throws {
        let client = RepositoryClient()
        // The old-A read is computed while the pull request is still open, but
        // its delivery is held until after fresher reads have completed.
        await client.change(.init(
            pullRequest: .init(number: 55, url: "https://github.com/akonwi/kit/pull/55"),
            root: "/tmp/repo", head: .init(kind: .value0, name: "main", oid: nil), dirty: false))
        await client.delayFirstResponse()
        await client.delayResponses()
        let replica = SessionReplica(sessions: [RepositoryClient.value], client: client)
        replica.attach()
        defer { replica.detach() }
        try await eventually { replica.connectionState == .connected }
        try await eventually { await client.calls == 1 }
        // The pull request closes on the server, then the session's cwd
        // transitions away and back (A→B→A) without detaching.
        await client.change(.init(
            pullRequest: nil, root: "/tmp/repo", head: .init(kind: .value0, name: "main", oid: nil), dirty: false))
        var moved = RepositoryClient.value
        moved.cwd = "/tmp/other"
        await client.emit(moved)
        try await eventually { replica.snapshot?.cwd == "/tmp/other" }
        await client.emit(RepositoryClient.value)
        try await eventually { replica.snapshot?.cwd == "/tmp/repo" }
        // Sample inside the window after the held old-A response would land but
        // before any follow-up read could mask a transient resurrect.
        try await Task.sleep(for: .milliseconds(700))
        #expect(replica.snapshot?.pullRequestNumber == nil)
        #expect(replica.snapshot?.pullRequestURL == nil)
        // And the settled state stays clean.
        try await Task.sleep(for: .milliseconds(900))
        #expect(replica.snapshot?.cwd == "/tmp/repo")
        #expect(replica.snapshot?.pullRequestNumber == nil)
        #expect(replica.snapshot?.pullRequestURL == nil)
        #expect(WorkspaceLocation.repositoryLabel(replica.snapshot) == "main")
        #expect(WorkspaceLocation.pullRequestDestination(replica.snapshot) == nil)
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

    @Test func pullRequestFollowsBranchHeadAndClearsWithIt() async throws {
        let client = RepositoryClient()
        let replica = SessionReplica(sessions: [RepositoryClient.value], client: client)
        replica.attach()
        defer { replica.detach() }
        try await eventually { WorkspaceLocation.repositoryLabel(replica.snapshot) == "main" }
        // A branch head with a pull request reaches the footer through the idle
        // poll alone — no model or activity event is emitted.
        await client.change(.init(
            pullRequest: .init(number: 123, url: "https://github.com/akonwi/kit/pull/123"),
            root: "/tmp/repo", head: .init(kind: .value0, name: "main", oid: nil), dirty: true))
        try await eventually { WorkspaceLocation.repositoryLabel(replica.snapshot) == "main* · PR #123" }
        #expect(WorkspaceLocation.repositoryLabel(replica.snapshot) == "main* · PR #123")
        #expect(WorkspaceLocation.pullRequestDestination(replica.snapshot)?.absoluteString == "https://github.com/akonwi/kit/pull/123")
        // Detaching the head clears the pull request affordance even if the
        // server still reports cached metadata.
        await client.change(.init(
            pullRequest: .init(number: 123, url: "https://github.com/akonwi/kit/pull/123"),
            root: "/tmp/repo", head: .init(kind: .value1, name: nil, oid: "abcdef0123456789"), dirty: false))
        try await eventually { WorkspaceLocation.repositoryLabel(replica.snapshot) == "detached abcdef01" }
        #expect(WorkspaceLocation.repositoryLabel(replica.snapshot) == "detached abcdef01")
        #expect(WorkspaceLocation.pullRequestDestination(replica.snapshot) == nil)
        #expect(replica.snapshot?.pullRequestNumber == nil)
        // Losing the repository clears everything.
        await client.change(nil)
        try await eventually { replica.snapshot?.gitHead == nil }
        #expect(replica.snapshot?.pullRequestNumber == nil)
        #expect(replica.snapshot?.pullRequestURL == nil)
    }
}
