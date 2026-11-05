import Foundation
import Testing
@testable import Kit

/// Answers each request with the reply its port's script gives, and keeps
/// what was asked so a test can check retries and fallbacks.
private final class StatusResponse: URLProtocol, @unchecked Sendable {
    struct Reply {
        let status: Int
        let body: String
        var type = "application/json"
    }
    struct Request {
        let line: String
        let body: Data
    }

    private static let lock = NSLock()
    nonisolated(unsafe) private static var scripts: [Int: @Sendable (String) -> Reply] = [:]
    nonisolated(unsafe) private static var seen: [Int: [Request]] = [:]

    /// Serves `script`, which answers a "METHOD /path?query" line, on `port`.
    static func serve(_ port: Int, _ script: @escaping @Sendable (String) -> Reply) {
        lock.lock(); defer { lock.unlock() }
        scripts[port] = script; seen[port] = []
    }

    static func requests(_ port: Int) -> [Request] {
        lock.lock(); defer { lock.unlock() }
        return seen[port] ?? []
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let line = (request.httpMethod ?? "GET") + " " + url.path + (url.query.map { "?" + $0 } ?? "")
        let body = request.httpBody ?? request.httpBodyStream.map(Self.read) ?? Data()
        Self.lock.lock()
        let script = Self.scripts[url.port!]
        Self.seen[url.port!, default: []].append(Request(line: line, body: body))
        Self.lock.unlock()
        let reply = script?(line) ?? Reply(status: 500, body: #"{"error":{"code":"internal","message":"No script."}}"#)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: reply.status, httpVersion: nil,
            headerFields: ["Content-Type": reply.type])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(reply.body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}

    private static func read(_ stream: InputStream) -> Data {
        stream.open(); defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            guard count > 0 else { break }
            data.append(buffer, count: count)
        }
        return data
    }
}

/// The HTTP status `operation` failed with, if it failed with one.
private func status(_ operation: () async throws -> Void) async -> Int? {
    do { try await operation(); return nil }
    catch ClientError.http(let status) { return status }
    catch { return nil }
}

private func refusal(_ code: String) -> String { #"{"error":{"code":"\#(code)","message":"Refused."}}"# }

private func client(_ port: Int, _ script: @escaping @Sendable (String) -> StatusResponse.Reply) throws -> HTTPClient {
    StatusResponse.serve(port, script)
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [StatusResponse.self]
    return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "test",
                          serverID: "test", configuration: configuration)
}

private let workspace = #"{"sessionId":"s","cwd":"/tmp/repo","workspaceId":"workspace_test","state":"ready","limits":{"maxPathBytes":4096,"maxPathComponents":64,"defaultDirectoryPageSize":100,"maxDirectoryPageSize":200,"maxDirectoryEntries":10000,"maxDirectoryResponseBytes":524288,"maxDirectoryObservationBytes":8388608,"maxPreviewBytes":1000000,"maxPreviewLines":5000,"maxActiveRequests":4,"maxPendingRequests":16}}"#

private let snapshot = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0},"messages":[],"eventStreamId":"stream","eventCursor":0}"#

private func fileRead(revision: String) -> String {
    #"{"sessionId":"s","workspace":\#(workspace),"path":"main.swift","revision":"\#(revision)","size":5,"encoding":"utf-8","content":"hello","returnedBytes":5,"returnedLines":1,"truncated":false}"#
}

/// Errors the server documents arrive as their status, so callers can tell a
/// refusal from a broken answer.
struct ResponseStatusTests {
    private func readStatus(_ port: Int, _ reply: StatusResponse.Reply) async throws -> Int? {
        let client = try client(port) { line in
            line.hasSuffix("/workspace") ? .init(status: 200, body: workspace) : reply
        }
        do { _ = try await client.readWorkspaceFile("s", path: "main.swift"); return nil }
        catch WorkspaceFileError.response(let status) { return status }
    }

    @Test func aRefusedReadSaysWhy() async throws {
        #expect(try await readStatus(19801, .init(status: 415, body: refusal("binary_file"))) == 415)
        #expect(WorkspaceFileError.response(415).errorDescription == "Binary files cannot be previewed.")
        let stale = #"{"error":{"code":"stale_file","message":"Changed.","details":{}}}"#
        #expect(try await readStatus(19802, .init(status: 409, body: stale)) == 409)
        #expect(try await readStatus(19803, .init(status: 418, body: "")) == 418)
    }

    @MainActor @Test func aReadOfAChangedFileTriesOnceMoreFresh() async throws {
        let client = try client(19804) { line in
            if line.hasSuffix("/workspace") { return .init(status: 200, body: workspace) }
            return .init(status: 200, body: fileRead(revision: "rev_1"))
        }
        let cache = FilePreviewCache()
        await cache.load("main.swift", session: "s", cwd: "/tmp/repo", client: client)
        #expect(cache.entries["main.swift"]?.preview?.revision == "rev_1")
        let stale = #"{"error":{"code":"stale_file","message":"Changed.","details":{}}}"#
        StatusResponse.serve(19804) { line in
            if line.hasSuffix("/workspace") { return .init(status: 200, body: workspace) }
            return StatusResponse.requests(19804).count == 2 ? .init(status: 409, body: stale) : .init(status: 200, body: fileRead(revision: "rev_2"))
        }
        await cache.load("main.swift", session: "s", cwd: "/tmp/repo", client: client, force: true)
        #expect(cache.entries["main.swift"]?.preview?.revision == "rev_2")
        #expect(cache.entries["main.swift"]?.error == nil)
        let reads = StatusResponse.requests(19804).filter { $0.line.hasSuffix("/files/read") }
        let expected = try reads.map { try JSONSerialization.jsonObject(with: $0.body) as? [String: Any] }
            .map { $0?["expectedFileRevision"] as? String }
        #expect(expected == ["rev_1", nil])
    }

    @Test func aHistoryPageTheServerRefusesKeepsItsStatus() async throws {
        let stale = try client(19805) { _ in .init(status: 409, body: refusal("conflict")) }
        let conflict = await status { _ = try await stale.history("s", before: "5") }
        #expect(conflict == 409)
        let gone = try client(19806) { _ in .init(status: 404, body: refusal("not_found")) }
        let missing = await status { _ = try await gone.history("s", before: "5") }
        #expect(missing == 404)
    }

    @Test func aSessionListTheServerRefusesKeepsItsStatus() async throws {
        let client = try client(19807) { _ in .init(status: 401, body: refusal("unauthorized")) }
        let sessions = await status { _ = try await client.sessions() }
        let activity = await status { _ = try await client.sessionActivity() }
        #expect(sessions == 401)
        #expect(activity == 401)
    }
}

@MainActor struct RefusedMutationTests {
    private let anchor = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
        path: "main.swift", fileRevision: "file_test", startLine: 1, endLine: 1), workingTreeDiff: nil)

    private func annotations(_ port: Int, update: StatusResponse.Reply) throws -> HTTPClient {
        try client(port) { line in
            line.hasPrefix("PATCH") ? update : .init(status: 200, body: #"{"annotations":[]}"#)
        }
    }

    @Test func aRefusedAnnotationEditIsCertain() async throws {
        let state = AnnotationState()
        state.draft = "Edited comment"
        let client = try annotations(19811, update: .init(status: 409, body: refusal("conflict")))
        #expect(await state.save(client: client, session: "s", anchor: anchor, editing: 7) == false)
        #expect(state.uncertain == false)
        #expect(state.draft == "Edited comment")
    }

    @Test func aStaleAnnotationEditAsksForTheRangeAgain() async throws {
        let state = AnnotationState()
        state.draft = "Edited comment"
        let client = try annotations(19812, update: .init(status: 409, body: refusal("stale_file")))
        #expect(await state.save(client: client, session: "s", anchor: anchor, editing: 7) == false)
        #expect(state.evidenceConflict == .staleFile)
        #expect(state.uncertain == false)
    }

    @Test func anAnnotationEditTheServerFailedStaysUncertain() async throws {
        let state = AnnotationState()
        state.draft = "Edited comment"
        let client = try annotations(19813, update: .init(status: 503, body: refusal("unavailable")))
        #expect(await state.save(client: client, session: "s", anchor: anchor, editing: 7) == false)
        #expect(state.uncertain)
    }

    @Test func aRefusedAnnotationIsCertain() async throws {
        let state = AnnotationState()
        state.draft = "New comment"
        let client = try client(19814) { line in
            line.hasPrefix("POST") ? .init(status: 404, body: refusal("not_found")) : .init(status: 200, body: #"{"annotations":[]}"#)
        }
        #expect(await state.save(client: client, session: "s", anchor: anchor) == false)
        #expect(state.uncertain == false)
    }

    private func mutationIDs(_ port: Int) throws -> [String?] {
        try StatusResponse.requests(port).filter { $0.line.hasSuffix("/cwd") }
            .map { try JSONSerialization.jsonObject(with: $0.body) as? [String: Any] }
            .map { $0?["mutationId"] as? String }
    }

    @Test func aDirectoryChangeRefusedForActiveWorkSaysSoAndIsSettled() async throws {
        let client = try client(19815) { _ in .init(status: 409, body: refusal("conflict")) }
        let operation = DirectoryChangeOperation()
        #expect(await operation.perform(path: "..", session: "s", client: client, refresh: {}) == false)
        #expect(operation.error == "The session has active work. Wait for it to finish before changing directories.")
        #expect(operation.request == nil)
    }

    @Test func aDirectoryChangeTheServerFailedRetriesTheSameMutation() async throws {
        let client = try client(19816) { _ in .init(status: 503, body: refusal("unavailable")) }
        let operation = DirectoryChangeOperation()
        #expect(await operation.perform(path: "..", session: "s", client: client, refresh: {}) == false)
        let original = try #require(operation.request?.mutationId)
        #expect(await operation.perform(path: "..", session: "s", client: client, refresh: {}) == false)
        #expect(try mutationIDs(19816) == [original, original])
    }

    @Test func aDismissalOfAChangedConversationSaysSo() async throws {
        let client = try client(19817) { _ in .init(status: 409, body: refusal("conflict")) }
        let operation = SubagentDismissalOperation()
        let conversation = "subagent_" + String(repeating: "a", count: 32)
        #expect(await operation.dismiss(session: "s", conversation: conversation, generation: 1, client: client, refresh: {}) == false)
        #expect(operation.error == "This conversation changed. Review its current state and try again.")
    }
}

/// The live session's event stream, refused at different points.
@MainActor struct RefusedEventStreamTests {
    private let session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "Test", model: "m", thinking: "high",
                                         workspace: "tmp", date: "", messages: [])

    private func replica(_ port: Int, stream: StatusResponse.Reply) throws -> SessionReplica {
        let client = try client(port) { line in
            line.contains("/events/stream") ? stream : .init(status: 200, body: snapshot)
        }
        return SessionReplica(sessions: [session], client: client)
    }

    @Test func aStreamForASessionThatIsGoneMarksItUnavailable() async throws {
        let replica = try replica(19821, stream: .init(status: 404, body: refusal("not_found")))
        replica.attach()
        defer { replica.detach() }
        try await eventually { replica.connectionState == .unavailable }
    }

    @Test func aStreamRefusedForCredentialsFailsAuthentication() async throws {
        for (port, status, code) in [(19822, 401, "unauthorized"), (19823, 403, "forbidden")] {
            let replica = try replica(port, stream: .init(status: status, body: refusal(code)))
            replica.attach()
            try await eventually { replica.connectionState == .authenticationFailed }
            replica.detach()
        }
    }

    @Test func aStreamTheServerCannotServeYetIsRetried() async throws {
        let replica = try replica(19824, stream: .init(status: 503, body: refusal("unavailable")))
        replica.attach()
        defer { replica.detach() }
        try await eventually {
            if case .retrying = replica.connectionState { return true }
            return false
        }
    }

    @Test func aStreamRefusedForAClientPluginOpensWithoutIt() async throws {
        let client = try client(19825) { line in
            if line.hasPrefix("PUT") { return .init(status: 204, body: "") }
            if line.contains("/events/stream") {
                return line.contains("clientPlugin=") ? .init(status: 409, body: refusal("conflict"))
                    : .init(status: 200, body: "", type: "text/event-stream")
            }
            return .init(status: 200, body: snapshot)
        }
        let refusals = Refusals()
        let hosting = ClientPluginHosting(instanceID: ClientPluginHosting.newInstanceID(), declaration: BrowserAgentPlugin.declaration,
                                          receive: { _ in }, refused: { error in Task { await refusals.record(error) } })
        let watch = Task { try await client.watch("s", hosting: hosting) { _ in } }
        defer { watch.cancel() }
        try await eventually { await !refusals.errors.isEmpty }
        try await eventually { StatusResponse.requests(19825).contains { $0.line.contains("/events/stream") && !$0.line.contains("clientPlugin=") } }
        guard case .http(let refused)? = await refusals.errors.first as? ClientError else {
            Issue.record("Expected an HTTP refusal"); return
        }
        #expect(refused == 409)
        let streams = StatusResponse.requests(19825).map(\.line).filter { $0.contains("/events/stream") }.prefix(2)
        #expect(streams.map { $0.contains("clientPlugin=") } == [true, false])
    }
}

private actor Refusals {
    private(set) var errors: [any Error] = []
    func record(_ error: any Error) { errors.append(error) }
}
