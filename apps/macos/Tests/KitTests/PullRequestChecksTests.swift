import Foundation
import OpenAPIRuntime
import Testing
@testable import Kit

private let session = "session_one"

private let failingFrame = #"{"sessionId":"session_one","cwd":"/repo","branch":"main","pullRequest":{"number":42,"url":"https://github.com/a/b/pull/42","state":"open","draft":false,"checks":{"state":"failing","required":"unsatisfied","counts":{"passed":1,"failed":1,"pending":1,"skipped":0,"canceled":0,"unknown":0},"items":[{"name":"test","workflow":"CI","state":"failed","required":true},{"name":"lint","workflow":"CI","state":"pending","required":false},{"name":"docs","state":"passed","required":false}],"omitted":0}}}"#

private func checksExcerpt(number: Int? = 42, status: PullRequestStatus?) -> SessionExcerpt {
    var value = SessionExcerpt(id: session, title: "Repository", sourceTitle: "Test", model: "p/m",
        thinking: "off", workspace: "repo", cwd: "/repo", date: "", messages: [])
    value.gitHead = "main"
    value.gitHeadKind = "branch"
    value.pullRequestNumber = number
    value.pullRequestURL = number.map { "https://github.com/a/b/pull/\($0)" }
    value.pullRequestStatus = status
    return value
}

private func status(_ state: PullRequestChecks.State, counts: PullRequestChecks.Counts, items: [PullRequestChecks.Check] = [],
                    required: PullRequestChecks.Requirement = .unknown, omitted: Int = 0, number: Int = 42) -> PullRequestStatus {
    PullRequestStatus(number: number, url: URL(string: "https://github.com/a/b/pull/\(number)")!, state: .open, draft: false,
                      checks: PullRequestChecks(state: state, required: required, counts: counts, items: items, omitted: omitted))
}

struct PullRequestChecksPresentationTests {
    @Test func failingChecksShowTheFailureGlyphCountSummaryAndRows() throws {
        let value = checksExcerpt(status: status(.failing, counts: .init(passed: 9, failed: 2, pending: 1, skipped: 1), items: [
            .init(name: "test", workflow: "Go tests", state: .failed, required: true),
            .init(name: "macOS tests", workflow: "macOS", state: .failed),
            .init(name: "OpenAPI compatibility", workflow: "Go tests", state: .pending, required: true),
            .init(name: "build", workflow: "Go tests", state: .passed, required: true),
            .init(name: "docs", state: .skipped),
        ], required: .unsatisfied, omitted: 8))
        let presentation = try #require(PullRequestChecksPresentation.make(value))
        #expect(presentation == PullRequestChecksPresentation(
            icon: .xCircle, tone: .danger, count: "9/12",
            summary: "2 failing, 1 running, 9 passed, 1 skipped", requirement: "Required checks aren’t met",
            rows: [
                .init(name: "test", workflow: "Go tests", icon: .xCircle, tone: .danger, required: true),
                .init(name: "macOS tests", workflow: "macOS", icon: .xCircle, tone: .danger, required: false),
                .init(name: "OpenAPI compatibility", workflow: "Go tests", icon: .clock, tone: .warning, required: true),
                .init(name: "build", workflow: "Go tests", icon: .checkCircle, tone: .success, required: true),
                .init(name: "docs", workflow: nil, icon: .minusCircle, tone: .muted, required: false),
            ],
            omitted: 8, checksURL: URL(string: "https://github.com/a/b/pull/42/checks")!))
        #expect(presentation.spokenSummary == "Checks: 2 failing, 1 running, 9 passed, 1 skipped. Required checks aren’t met")
    }

    @Test func passingAndRunningChecksUseTheirOwnGlyphAndTone() throws {
        let passing = try #require(PullRequestChecksPresentation.make(checksExcerpt(status: status(.passing, counts: .init(passed: 12), required: .satisfied))))
        #expect(passing.icon == .checkCircle && passing.tone == .success && passing.count == "12/12")
        #expect(passing.summary == "12 passed" && passing.requirement == "Required checks passed")
        let running = try #require(PullRequestChecksPresentation.make(checksExcerpt(status: status(.pending, counts: .init(passed: 9, pending: 3), required: .pending))))
        #expect(running.icon == .clock && running.tone == .warning && running.count == "9/12")
        #expect(running.summary == "3 running, 9 passed" && running.requirement == "Required checks are running")
        let canceled = try #require(PullRequestChecksPresentation.make(checksExcerpt(status: status(.failing, counts: .init(passed: 1, canceled: 1)))))
        #expect(canceled.icon == .xCircle && canceled.tone == .danger && canceled.summary == "1 canceled, 1 passed" && canceled.requirement == nil)
    }

    @Test func theChipHidesWithoutChecksOrForAnotherPullRequest() {
        #expect(PullRequestChecksPresentation.make(checksExcerpt(status: status(.none, counts: .init()))) == nil)
        #expect(PullRequestChecksPresentation.make(checksExcerpt(status: status(.none, counts: .init(skipped: 2)))) == nil)
        #expect(PullRequestChecksPresentation.make(checksExcerpt(status: nil)) == nil)
        #expect(PullRequestChecksPresentation.make(checksExcerpt(status: status(.passing, counts: .init(passed: 1), number: 7))) == nil)
        #expect(PullRequestChecksPresentation.make(checksExcerpt(number: nil, status: status(.passing, counts: .init(passed: 1)))) == nil)
        var detached = checksExcerpt(status: status(.passing, counts: .init(passed: 1)))
        detached.gitHeadKind = "detached"
        #expect(PullRequestChecksPresentation.make(detached) == nil)
    }
}

struct PullRequestStatusRecordsTests {
    @Test func decodesAValidatedStatus() throws {
        let update = try #require(try PullRequestStatusRecords.decode(ServerSentEvent(event: "pullRequest.status", data: failingFrame), session: session))
        #expect(update == SessionPullRequestUpdate(sessionID: session, cwd: "/repo", branch: "main", pullRequest: PullRequestStatus(
            number: 42, url: URL(string: "https://github.com/a/b/pull/42")!, state: .open, draft: false,
            checks: PullRequestChecks(state: .failing, required: .unsatisfied, counts: .init(passed: 1, failed: 1, pending: 1), items: [
                .init(name: "test", workflow: "CI", state: .failed, required: true),
                .init(name: "lint", workflow: "CI", state: .pending),
                .init(name: "docs", state: .passed),
            ]))))
        let none = #"{"sessionId":"session_one","cwd":"/repo"}"#
        #expect(try PullRequestStatusRecords.decode(ServerSentEvent(event: "pullRequest.status", data: none), session: session)
                == SessionPullRequestUpdate(sessionID: session, cwd: "/repo", branch: nil, pullRequest: nil))
        #expect(try PullRequestStatusRecords.decode(ServerSentEvent(), session: session) == nil)
    }

    @Test func rejectsPayloadsOutsideTheContract() {
        let invalid = [
            failingFrame.replacingOccurrences(of: "session_one", with: "session_other"),
            failingFrame.replacingOccurrences(of: "\"cwd\":\"/repo\"", with: "\"cwd\":\"repo\""),
            failingFrame.replacingOccurrences(of: "\"branch\":\"main\",", with: ""),
            failingFrame.replacingOccurrences(of: "https://github.com/a/b/pull/42", with: "javascript:alert(1)"),
            failingFrame.replacingOccurrences(of: "\"state\":\"open\"", with: "\"state\":\"OPEN\""),
            failingFrame.replacingOccurrences(of: "\"state\":\"failing\"", with: "\"state\":\"passing\""),
            failingFrame.replacingOccurrences(of: "\"state\":\"failed\"", with: "\"state\":\"failure\""),
            failingFrame.replacingOccurrences(of: "\"passed\":1", with: "\"passed\":2"),
            failingFrame.replacingOccurrences(of: "\"omitted\":0", with: "\"omitted\":1"),
            failingFrame.replacingOccurrences(of: "\"name\":\"test\"", with: "\"name\":\"\""),
            failingFrame.replacingOccurrences(of: "\"name\":\"test\"", with: "\"name\":\"te\\u202est\""),
            failingFrame.replacingOccurrences(of: "\"name\":\"test\"", with: "\"name\":\"" + String(repeating: "n", count: 257) + "\""),
            failingFrame.replacingOccurrences(of: "\"draft\":false", with: "\"draft\":false,\"extra\":true"),
            failingFrame.replacingOccurrences(of: "\"omitted\":0", with: "\"omitted\":0,\"omitted\":0"),
            "{", "[]",
        ]
        for data in invalid {
            #expect(throws: (any Error).self, "accepted \(data.prefix(120))") {
                try PullRequestStatusRecords.decode(ServerSentEvent(event: "pullRequest.status", data: data), session: session)
            }
        }
        for event in [ServerSentEvent(event: "vcs.status", data: failingFrame), ServerSentEvent(event: "pullRequest.status"),
                      ServerSentEvent(id: "1", event: "pullRequest.status", data: failingFrame)] {
            #expect(throws: Kit.ClientError.self) { try PullRequestStatusRecords.decode(event, session: session) }
        }
    }
}

private final class PullRequestStreamResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(url.path == "/v1/sessions/session_one/pull-request/events")
        let (status, type, body): (Int, String, String) = switch url.port! {
        case 19701: (200, "text/event-stream; charset=utf-8", ": connected\n\nevent: pullRequest.status\ndata: \(failingFrame)\n\n")
        case 19702: (404, "text/plain; charset=utf-8", "404 page not found\n")
        default: (404, "application/json", #"{"error":{"code":"not_found","message":"session not found"}}"#)
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil,
            headerFields: ["Content-Type": type])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

private actor UpdateCollector {
    var updates: [SessionPullRequestUpdate] = []
    func append(_ value: SessionPullRequestUpdate) { updates.append(value) }
}

@Suite(.serialized)
struct PullRequestStreamTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [PullRequestStreamResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "server", serverID: "test", configuration: configuration)
    }

    @Test func deliversValidatedRecordsUntilTheStreamEnds() async throws {
        let collector = UpdateCollector()
        await #expect(throws: Kit.ClientError.self) { try await client(19701).watchPullRequest(session) { await collector.append($0) } }
        #expect(await collector.updates.map { $0.pullRequest?.checks.state } == [.failing])
    }

    @Test func anOlderServersUnroutedStreamIsNotServed() async throws {
        await #expect(throws: PullRequestStreamNotServed.self) { try await client(19702).watchPullRequest(session) { _ in } }
    }

    @Test func aDeclaredNotFoundStaysAnHTTPFailure() async throws {
        do {
            try await client(19703).watchPullRequest(session) { _ in }
            Issue.record("watchPullRequest returned")
        } catch Kit.ClientError.http(404) {}
    }
}

/// Repository and pull request streams that tests drive by hand.
private actor ChecksClient: SessionVCSClient, SessionPullRequestClient {
    nonisolated let serverID = "checks-test"
    nonisolated let isDemo = false
    static let value = SessionExcerpt(id: "session", title: "Repository", sourceTitle: "Test", model: "p/m",
        thinking: "off", workspace: "repo", cwd: "/tmp/repo", date: "", messages: [])
    static let pullRequest = WireGitHubPullRequest(number: 42, url: "https://github.com/a/b/pull/42")

    var notServed = false
    var pullRequestOpens = 0
    var pullRequestActive = 0
    private var vcs: WireVCSStatus? = .init(pullRequest: nil, root: "/tmp/repo", head: .init(kind: .value0, name: "main", oid: nil), dirty: false)
    private var vcsSubscribers: [UUID: AsyncThrowingStream<WireSessionVCSStatus, Error>.Continuation] = [:]
    private var latest: SessionPullRequestUpdate?
    private var prSubscribers: [UUID: AsyncThrowingStream<SessionPullRequestUpdate, Error>.Continuation] = [:]

    func sessions() async throws -> [SessionExcerpt] { [Self.value] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { Self.value }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(Self.value)
        try await Task.sleep(for: .seconds(60))
    }
    func vcs(_ id: String) async throws -> WireSessionVCSStatus { .init(sessionId: id, cwd: "/tmp/repo", status: vcs) }
    func watchVCS(_ id: String, receive: @escaping @Sendable (WireSessionVCSStatus) async -> Void) async throws {
        let token = UUID()
        let (stream, continuation) = AsyncThrowingStream<WireSessionVCSStatus, Error>.makeStream()
        vcsSubscribers[token] = continuation
        defer { vcsSubscribers.removeValue(forKey: token); continuation.finish() }
        await receive(.init(sessionId: id, cwd: "/tmp/repo", status: vcs))
        for try await update in stream { await receive(update) }
    }
    func watchPullRequest(_ id: String, receive: @escaping @Sendable (SessionPullRequestUpdate) async -> Void) async throws {
        pullRequestOpens += 1
        if notServed { throw PullRequestStreamNotServed() }
        pullRequestActive += 1
        let token = UUID()
        let (stream, continuation) = AsyncThrowingStream<SessionPullRequestUpdate, Error>.makeStream()
        prSubscribers[token] = continuation
        defer { prSubscribers.removeValue(forKey: token); continuation.finish(); pullRequestActive -= 1 }
        if let latest { await receive(latest) }
        for try await update in stream { await receive(update) }
    }

    func setNotServed() { notServed = true }
    func showPullRequest(_ pull: WireGitHubPullRequest?, branch: String = "main") {
        vcs = .init(pullRequest: pull, root: "/tmp/repo", head: .init(kind: .value0, name: branch, oid: nil), dirty: false)
        for continuation in vcsSubscribers.values { continuation.yield(.init(sessionId: "session", cwd: "/tmp/repo", status: vcs)) }
    }
    func report(_ checks: PullRequestChecks, branch: String = "main", number: Int = 42) {
        let update = SessionPullRequestUpdate(sessionID: "session", cwd: "/tmp/repo", branch: branch, pullRequest: PullRequestStatus(
            number: number, url: URL(string: "https://github.com/a/b/pull/\(number)")!, state: .open, draft: false, checks: checks))
        latest = update
        for continuation in prSubscribers.values { continuation.yield(update) }
    }
}

@MainActor struct PullRequestChecksReplicaTests {
    private let failing = PullRequestChecks(state: .failing, required: .unsatisfied, counts: .init(passed: 1, failed: 1),
        items: [.init(name: "test", state: .failed, required: true), .init(name: "lint", state: .passed)])
    private let passing = PullRequestChecks(state: .passing, required: .satisfied, counts: .init(passed: 2),
        items: [.init(name: "test", state: .passed, required: true), .init(name: "lint", state: .passed)])

    @Test func theChipFollowsTheServersChecksForTheShownPullRequest() async throws {
        let client = ChecksClient()
        await client.report(failing)
        let replica = SessionReplica(sessions: [ChecksClient.value], client: client)
        replica.vcsReconnectDelay = .milliseconds(10)
        replica.attach()
        defer { replica.detach() }
        try await eventually { replica.snapshot?.gitHead == "main" }
        // No pull request, no stream and no chip.
        #expect(await client.pullRequestOpens == 0)
        #expect(PullRequestChecksPresentation.make(replica.snapshot) == nil)

        await client.showPullRequest(ChecksClient.pullRequest)
        try await eventually { PullRequestChecksPresentation.make(replica.snapshot)?.tone == .danger }
        let shown = try #require(PullRequestChecksPresentation.make(replica.snapshot))
        #expect(shown.icon == .xCircle && shown.count == "1/2" && shown.summary == "1 failing, 1 passed")
        #expect(WorkspaceLocation.pullRequestDestination(replica.snapshot)?.absoluteString == "https://github.com/a/b/pull/42")

        await client.report(passing)
        try await eventually { PullRequestChecksPresentation.make(replica.snapshot)?.tone == .success }
        #expect(PullRequestChecksPresentation.make(replica.snapshot)?.count == "2/2")
        #expect(await client.pullRequestOpens == 1)

        // The pull request closing removes the chip and closes the stream.
        await client.showPullRequest(nil)
        try await eventually { replica.snapshot?.pullRequestNumber == nil }
        #expect(replica.snapshot?.pullRequestStatus == nil)
        try await eventually { await client.pullRequestActive == 0 }
    }

    @Test func checksForAnotherBranchOrPullRequestAreNotShown() async throws {
        let client = ChecksClient()
        await client.report(failing, branch: "other")
        let replica = SessionReplica(sessions: [ChecksClient.value], client: client)
        replica.attach()
        defer { replica.detach() }
        await client.showPullRequest(ChecksClient.pullRequest)
        try await eventually { await client.pullRequestOpens == 1 }
        try await Task.sleep(for: .milliseconds(100))
        #expect(replica.snapshot?.pullRequestNumber == 42)
        #expect(PullRequestChecksPresentation.make(replica.snapshot) == nil)
        await client.report(failing, number: 7)
        try await Task.sleep(for: .milliseconds(100))
        #expect(PullRequestChecksPresentation.make(replica.snapshot) == nil)
        await client.report(passing)
        try await eventually { PullRequestChecksPresentation.make(replica.snapshot)?.tone == .success }
        // A switch to another branch can reach the pull request stream first;
        // its checks show once the repository stream names that branch's PR.
        await client.report(failing, branch: "feature", number: 50)
        try await eventually { PullRequestChecksPresentation.make(replica.snapshot) == nil }
        await client.showPullRequest(WireGitHubPullRequest(number: 50, url: "https://github.com/a/b/pull/50"), branch: "feature")
        try await eventually { PullRequestChecksPresentation.make(replica.snapshot)?.tone == .danger }
        #expect(WorkspaceLocation.pullRequestNumber(replica.snapshot) == 50)
    }

    @Test func anOlderServerKeepsThePullRequestChipWithoutChecks() async throws {
        let client = ChecksClient()
        await client.setNotServed()
        let replica = SessionReplica(sessions: [ChecksClient.value], client: client)
        replica.vcsReconnectDelay = .milliseconds(10)
        replica.attach()
        defer { replica.detach() }
        await client.showPullRequest(ChecksClient.pullRequest)
        try await eventually { replica.snapshot?.pullRequestNumber == 42 }
        try await eventually { await client.pullRequestOpens == 1 }
        // Further repository updates don't retry the stream this attachment.
        await client.showPullRequest(WireGitHubPullRequest(number: 43, url: "https://github.com/a/b/pull/43"))
        try await eventually { replica.snapshot?.pullRequestNumber == 43 }
        try await Task.sleep(for: .milliseconds(100))
        #expect(await client.pullRequestOpens == 1)
        #expect(PullRequestChecksPresentation.make(replica.snapshot) == nil)
        #expect(WorkspaceLocation.pullRequestNumber(replica.snapshot) == 43)
    }
}
