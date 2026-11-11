import Foundation
import KitClient
import Testing
@testable import Kit

/// The repository strip: what it says for each repository state, and how
/// the session keeps it current from the server's streams.
@MainActor
struct SessionRepositoryTests {
    static let cwd = "/Users/me/kit"
    static let pullURL = "https://github.com/akonwi/kit/pull/312"

    static func record(_ kind: WireVCSHeadKind = .value0, name: String? = "fix-sse-resume", oid: String? = nil,
                       dirty: Bool = false, pull: Int? = nil, cwd: String = cwd, session: String = "s") -> WireSessionVCSStatus {
        let head = WireVCSHead(kind: kind, name: name, oid: oid)
        let request = pull.map { WireGitHubPullRequest(number: $0, url: pullURL) }
        return WireSessionVCSStatus(sessionId: session, cwd: cwd,
                                    status: WireVCSStatus(pullRequest: request, root: cwd, head: head, dirty: dirty))
    }

    static func checks(passed: Int = 0, failed: Int = 0, pending: Int = 0, skipped: Int = 0,
                       required: PullRequestChecks.Requirement = .unknown,
                       items: [PullRequestChecks.Check] = [], omitted: Int = 0) -> PullRequestChecks {
        let counts = PullRequestChecks.Counts(passed: passed, failed: failed, pending: pending, skipped: skipped)
        let state: PullRequestChecks.State = failed > 0 ? .failing : pending > 0 ? .pending : passed > 0 ? .passing : .none
        return PullRequestChecks(state: state, required: required, counts: counts, items: items, omitted: omitted)
    }

    static func update(_ checks: PullRequestChecks, number: Int = 312, branch: String? = "fix-sse-resume",
                       cwd: String = cwd) -> SessionPullRequestUpdate {
        SessionPullRequestUpdate(sessionID: "s", cwd: cwd, branch: branch, pullRequest: PullRequestStatus(
            number: number, url: URL(string: pullURL)!, state: .open, draft: false, checks: checks))
    }

    static func strip(_ record: WireSessionVCSStatus, _ update: SessionPullRequestUpdate? = nil) -> RepositoryStatus? {
        RepositoryHead(record).map { RepositoryStatus($0, pullRequest: update) }
    }

    // MARK: What the strip shows

    @Test func aCleanBranchShowsItsName() {
        #expect(Self.strip(Self.record(name: "main"))?.branch == "main")
        #expect(Self.strip(Self.record(name: "main"))?.pullRequest == nil)
    }

    @Test func uncommittedChangesMarkTheBranch() {
        #expect(Self.strip(Self.record(dirty: true))?.branch == "fix-sse-resume*")
    }

    @Test func aDetachedHeadShowsItsShortCommit() {
        let oid = "4f2a91c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6"
        #expect(Self.strip(Self.record(.value1, name: nil, oid: oid))?.branch == "detached 4f2a91c0")
    }

    @Test func anUnbornBranchShowsItsName() {
        #expect(Self.strip(Self.record(.value2, name: "main"))?.branch == "main")
    }

    @Test func aFolderOutsideARepositoryShowsNoStrip() {
        #expect(RepositoryHead(WireSessionVCSStatus(sessionId: "s", cwd: Self.cwd, status: nil)) == nil)
    }

    @Test func aPullRequestShowsWithoutChecksUntilTheyArrive() {
        let status = Self.strip(Self.record(pull: 312))
        #expect(status?.pullRequest == RepositoryStatus.PullRequest(number: 312, url: URL(string: Self.pullURL)!, checks: nil))
    }

    @Test func aPullRequestWithAnUnsafeAddressIsLeftOut() {
        let record = WireSessionVCSStatus(sessionId: "s", cwd: Self.cwd, status: WireVCSStatus(
            pullRequest: WireGitHubPullRequest(number: 312, url: "javascript:alert(1)"), root: Self.cwd,
            head: WireVCSHead(kind: .value0, name: "fix", oid: nil), dirty: false))
        #expect(Self.strip(record)?.pullRequest == nil)
    }

    @Test func checksShowWhenTheyAreForTheBranchAndPullRequestShown() {
        let update = Self.update(Self.checks(passed: 10, failed: 2))
        let checks = Self.strip(Self.record(pull: 312), update)?.pullRequest?.checks
        #expect(checks?.count == "10/12")
        #expect(checks?.tone == .danger)
        #expect(checks?.symbol == "xmark.circle")
        #expect(checks?.summary == "2 failing, 10 passed")
        #expect(checks?.url == URL(string: Self.pullURL + "/checks"))
    }

    @Test func checksForAnotherBranchPullRequestOrFolderAreLeftOut() {
        let checks = Self.checks(passed: 1)
        #expect(Self.strip(Self.record(pull: 312), Self.update(checks, branch: "other"))?.pullRequest?.checks == nil)
        #expect(Self.strip(Self.record(pull: 312), Self.update(checks, number: 7))?.pullRequest?.checks == nil)
        #expect(Self.strip(Self.record(pull: 312), Self.update(checks, cwd: "/elsewhere"))?.pullRequest?.checks == nil)
    }

    @Test func aPullRequestWithNoChecksShowsNoChecksChip() {
        #expect(Self.strip(Self.record(pull: 312), Self.update(Self.checks()))?.pullRequest?.checks == nil)
    }

    @Test func runningChecksCountWhatPassedOfThoseThatWillRun() {
        let checks = ChecksPresentation(Self.update(Self.checks(passed: 7, pending: 5, skipped: 2, required: .pending)).pullRequest!)
        #expect(checks?.count == "7/12")
        #expect(checks?.tone == .warning)
        #expect(checks?.symbol == "clock")
        #expect(checks?.summary == "5 running, 7 passed, 2 skipped")
        #expect(checks?.requirement == "Required checks are running")
    }

    @Test func passingChecksAreGreen() {
        let checks = ChecksPresentation(Self.update(Self.checks(passed: 12, required: .satisfied)).pullRequest!)
        #expect(checks?.count == "12/12")
        #expect(checks?.tone == .success)
        #expect(checks?.requirement == "Required checks passed")
    }

    @Test func eachCheckShowsItsStateWorkflowAndWhetherItIsRequired() {
        let items = [
            PullRequestChecks.Check(name: "test (ubuntu-latest)", workflow: "CI", state: .failed, required: true),
            PullRequestChecks.Check(name: "lint", state: .skipped),
        ]
        let checks = ChecksPresentation(Self.update(Self.checks(failed: 1, skipped: 1, items: items)).pullRequest!)
        #expect(checks?.rows == [
            .init(name: "test (ubuntu-latest)", workflow: "CI", symbol: "xmark.circle", tone: .danger, required: true),
            .init(name: "lint", workflow: nil, symbol: "minus.circle", tone: .muted, required: false),
        ])
    }

    // MARK: Following the streams

    @Test func theStripFollowsTheRepositoryAndPullRequestStreams() async throws {
        let client = FakeRepositoryClient()
        let repository = SessionRepository(session: "s", client: client)
        let watching = Task { await repository.watch() }
        defer { watching.cancel() }

        client.sendVCS(Self.record(name: "main"))
        try await until { repository.status?.branch == "main" }
        #expect(client.pullRequestWatches == 0)

        client.sendVCS(Self.record(pull: 312))
        try await until { client.pullRequestWatches == 1 }
        client.sendPullRequest(Self.update(Self.checks(passed: 3)))
        try await until { repository.status?.pullRequest?.checks?.count == "3/3" }

        // The branch loses its pull request: the checks go with it.
        client.sendVCS(Self.record(name: "fix-sse-resume"))
        try await until { repository.status?.pullRequest == nil }
        #expect(repository.pullRequest == nil)
    }

    @Test func aRecordForAnotherSessionIsIgnored() {
        let repository = SessionRepository(session: "s", client: FakeRepositoryClient())
        repository.apply(Self.record(name: "main", session: "other"))
        #expect(repository.status == nil)
    }

    @Test func theLastStatusStaysWhileTheStreamReconnects() async throws {
        let client = FakeRepositoryClient()
        let repository = SessionRepository(session: "s", client: client)
        repository.reconnectDelays = [.milliseconds(10)]
        let watching = Task { await repository.watch() }
        defer { watching.cancel() }

        client.sendVCS(Self.record(name: "main"))
        try await until { repository.status?.branch == "main" }
        client.dropVCS()
        try await until { client.vcsWatches == 2 }
        #expect(repository.status?.branch == "main")
    }

    @Test func aServerWithoutThePullRequestStreamShowsNoChecks() async throws {
        let client = FakeRepositoryClient()
        client.servesPullRequests = false
        let repository = SessionRepository(session: "s", client: client)
        repository.reconnectDelays = [.milliseconds(10)]
        let watching = Task { await repository.watch() }
        defer { watching.cancel() }

        client.sendVCS(Self.record(pull: 312))
        try await until { client.pullRequestWatches == 1 }
        // The pull request goes and comes back: the server isn't asked again.
        client.sendVCS(Self.record(name: "fix-sse-resume"))
        try await until { repository.status?.pullRequest == nil }
        client.sendVCS(Self.record(pull: 312))
        try await until { repository.status?.pullRequest?.number == 312 }
        try await Task.sleep(for: .milliseconds(50))
        #expect(client.pullRequestWatches == 1)
        #expect(repository.status?.pullRequest?.checks == nil)
    }

    @Test func leavingTheScreenClosesBothStreamsAndKeepsWhatItShowed() async throws {
        let client = FakeRepositoryClient()
        let repository = SessionRepository(session: "s", client: client)
        let watching = Task { await repository.watch() }

        client.sendVCS(Self.record(pull: 312))
        try await until { client.openPullRequestStreams == 1 }
        client.sendPullRequest(Self.update(Self.checks(passed: 3)))
        try await until { repository.status?.pullRequest?.checks?.count == "3/3" }

        watching.cancel()
        await watching.value
        try await until { client.openPullRequestStreams == 0 && client.openVCSStreams == 0 }
        #expect(repository.status?.pullRequest?.checks?.count == "3/3")

        // Coming back asks the server again.
        let again = Task { await repository.watch() }
        defer { again.cancel() }
        try await until { client.vcsWatches == 2 }
    }

    private func until(_ condition: @MainActor () -> Bool) async throws {
        for _ in 0..<200 where !condition() { try await Task.sleep(for: .milliseconds(10)) }
        #expect(condition())
    }
}

/// Streams repository and pull request records the test sends.
final class FakeRepositoryClient: SessionVCSClient, SessionPullRequestClient, @unchecked Sendable {
    let serverID = "studio"
    let isDemo = false
    private let lock = NSLock()
    private var vcs: AsyncThrowingStream<WireSessionVCSStatus, any Error>.Continuation?
    private var pulls: AsyncThrowingStream<SessionPullRequestUpdate, any Error>.Continuation?
    private var _vcsWatches = 0
    private var _pullRequestWatches = 0
    private var _servesPullRequests = true
    private var _openVCSStreams = 0
    private var _openPullRequestStreams = 0
    private var pendingVCS: [WireSessionVCSStatus] = []

    var vcsWatches: Int { lock.withLock { _vcsWatches } }
    var pullRequestWatches: Int { lock.withLock { _pullRequestWatches } }
    var openVCSStreams: Int { lock.withLock { _openVCSStreams } }
    var openPullRequestStreams: Int { lock.withLock { _openPullRequestStreams } }
    var servesPullRequests: Bool {
        get { lock.withLock { _servesPullRequests } }
        set { lock.withLock { _servesPullRequests = newValue } }
    }

    func sendVCS(_ record: WireSessionVCSStatus) {
        lock.withLock {
            if let vcs { vcs.yield(record) } else { pendingVCS.append(record) }
        }
    }
    /// Ends the repository stream as a dropped connection would.
    func dropVCS() {
        let stream = lock.withLock {
            defer { vcs = nil }
            return vcs
        }
        stream?.finish(throwing: ClientError.disconnected)
    }
    func sendPullRequest(_ update: SessionPullRequestUpdate) { _ = lock.withLock { pulls?.yield(update) } }

    /// Counts a stream closing. Streams close outside the lock.
    private func closed(_ count: ReferenceWritableKeyPath<FakeRepositoryClient, Int>) {
        lock.withLock { self[keyPath: count] -= 1 }
    }

    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func vcs(_ session: String) async throws -> WireSessionVCSStatus { throw ClientError.missingSession }

    func watchVCS(_ session: String, receive: @escaping @Sendable (WireSessionVCSStatus) async -> Void) async throws {
        let (stream, continuation) = AsyncThrowingStream<WireSessionVCSStatus, any Error>.makeStream()
        continuation.onTermination = { [weak self] _ in self?.closed(\._openVCSStreams) }
        lock.withLock {
            _vcsWatches += 1
            _openVCSStreams += 1
            vcs = continuation
            for record in pendingVCS { continuation.yield(record) }
            pendingVCS = []
        }
        for try await record in stream { await receive(record) }
        try Task.checkCancellation()
        throw ClientError.disconnected
    }

    func watchPullRequest(_ session: String, receive: @escaping @Sendable (SessionPullRequestUpdate) async -> Void) async throws {
        let serves = lock.withLock {
            _pullRequestWatches += 1
            return _servesPullRequests
        }
        guard serves else { throw PullRequestStreamNotServed() }
        let (stream, continuation) = AsyncThrowingStream<SessionPullRequestUpdate, any Error>.makeStream()
        continuation.onTermination = { [weak self] _ in self?.closed(\._openPullRequestStreams) }
        lock.withLock {
            _openPullRequestStreams += 1
            pulls = continuation
        }
        for try await update in stream { await receive(update) }
        try Task.checkCancellation()
        throw ClientError.disconnected
    }
}
