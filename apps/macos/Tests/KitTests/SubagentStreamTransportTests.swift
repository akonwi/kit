import Foundation
import Testing
@testable import Kit

private final class ChildFeed: @unchecked Sendable {
    private let lock = NSLock()
    private var settled = false
    private(set) var requests = 0
    func finish() { lock.lock(); settled = true; lock.unlock() }
    func response(_ request: URLRequest) -> String {
        lock.lock(); defer { lock.unlock() }
        requests += 1
        if request.url!.path.hasSuffix("transcript") { return #"{"conversationId":"child","messages":[]}"# }
        let after = Int(URLComponents(url: request.url!, resolvingAgainstBaseURL: false)!.queryItems!.first { $0.name == "after" }!.value!)!
        let all: [[String: Any]] = [
            ["sequence": 1, "kind": "turn.started", "turnId": "t"],
            ["sequence": 2, "kind": "message.thinking.delta", "turnId": "t", "messageId": "m", "delta": "Inspecting resources"],
            ["sequence": 3, "kind": "tool.started", "turnId": "t", "toolCallId": "call", "toolName": "bash"],
            ["sequence": 4, "kind": "tool.updated", "turnId": "t", "toolCallId": "call", "toolName": "bash", "text": "first"],
            ["sequence": 5, "kind": "tool.updated", "turnId": "t", "toolCallId": "call", "toolName": "bash", "text": " second"],
            ["sequence": 6, "kind": "tool.completed", "turnId": "t", "toolCallId": "call", "toolName": "bash", "text": "final"],
            ["sequence": 7, "kind": "turn.settled", "turnId": "t"]]
        let last = settled ? 7 : after == 0 ? 4 : 5
        let events = all.filter { ($0["sequence"] as! Int) > after && ($0["sequence"] as! Int) <= last }
        return String(data: try! JSONSerialization.data(withJSONObject: ["streamId": "stream", "firstSequence": 1, "lastSequence": last, "events": events]), encoding: .utf8)!
    }
}
private final class ChildFeedProtocol: URLProtocol, @unchecked Sendable {
    static let feed = ChildFeed()
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: 200,
            httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(Self.feed.response(request).utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

private actor ChildUpdates {
    var values: [SubagentTranscriptUpdate] = []
    func receive(_ value: SubagentTranscriptUpdate) { values.append(value) }
    func hasOutput(_ text: String) -> Bool { values.contains { $0.messages.flatMap(\.tools).contains { $0.output == text } } }
}

struct SubagentStreamTransportTests {
    @Test func httpFeedPublishesTwoLiveUpdatesBeforeCompletion() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ChildFeedProtocol.self]
        let client = try HTTPClient(endpoint: URL(string: "http://127.0.0.1:19031")!, token: "test",
            instance: "test", serverID: "test", configuration: configuration)
        let updates = ChildUpdates()
        let task = Task { try await client.watchSubagent(session: "s", conversation: "child") { await updates.receive($0) } }
        defer { task.cancel() }
        for _ in 0..<150 {
            if await updates.hasOutput("first second") { break }
            try await Task.sleep(for: .milliseconds(20))
        }
        let running = await updates.values
        #expect(running.contains { $0.messages.first?.tools.first?.output == "first" })
        let latest = try #require(running.last)
        #expect(latest.messages.first?.tools.first?.output == "first second")
        #expect(latest.messages.first?.tools.first?.status == "Running…")
        #expect(latest.messages.first?.tools.first?.thinking == "Inspecting resources")
        #expect(latest.activity == "Working…")
        ChildFeedProtocol.feed.finish()
        for _ in 0..<150 {
            if await updates.hasOutput("final") { break }
            try await Task.sleep(for: .milliseconds(20))
        }
        let final = try #require(await updates.values.last)
        #expect(final.messages.first?.tools.first?.output == "final")
        #expect(final.activity == nil)
        task.cancel()
        _ = await task.result
        let count = await updates.values.count
        try await Task.sleep(for: .milliseconds(300))
        #expect(await updates.values.count == count)
    }

    @MainActor @Test func reconnectRetainsMessagesAndCancellationStopsWatcher() async throws {
        let state = SubagentConversationState()
        let client = ReconnectingChild()
        let task = Task { await state.watch(client: client, session: "s", conversation: "child") }
        defer { task.cancel() }
        for _ in 0..<150 {
            if await client.calls >= 2 && state.error == nil { break }
            try await Task.sleep(for: .milliseconds(20))
        }
        #expect(await client.calls == 2)
        #expect(state.messages.first?.text == "Retained response")
        #expect(state.activity == "Thinking…")
        #expect(state.error == nil)
        task.cancel()
        await task.value
        #expect(await client.active == 0)
        #expect(state.messages.first?.text == "Retained response")
        #expect(state.activity == nil)
    }

    private actor ReconnectingChild: SubagentStreamingClient {
        nonisolated let serverID = "test"
        nonisolated let isDemo = false
        var calls = 0
        var active = 0
        func sessions() async throws -> [SessionExcerpt] { [] }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
        func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] { [] }
        func watchSubagent(session: String, conversation: String,
                           receive: @escaping @Sendable (SubagentTranscriptUpdate) async -> Void) async throws {
            calls += 1; active += 1
            defer { active -= 1 }
            await receive(SubagentTranscriptUpdate(messages: [TranscriptMessage(id: "m", role: "assistant", text: "Retained response", tools: [])], activity: "Thinking…"))
            if calls == 1 { throw ClientError.disconnected }
            try await Task.sleep(for: .seconds(30))
        }
    }
}
