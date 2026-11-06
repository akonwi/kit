import Foundation
import Synchronization
import Testing
@testable import Kit

/// A session whose turn has just started. Its live user message has only
/// its text, as the server sends it; the stored message, read beside the
/// stream, also has its attachments or comments. Each port is a scenario.
private final class LiveUserMessageResponse: URLProtocol, @unchecked Sendable {
    struct Scenario: Sendable {
        /// The live event's text.
        var text: String
        /// The stored message's content, or nil when reading it fails.
        var stored: String?
        /// The turn completes on the stream, and the read answers only after
        /// the refresh that follows, with content the refresh doesn't have.
        var lateAfterRefresh = false
        /// The turn's message carries comments.
        var annotations = false
    }

    struct State {
        var snapshots: [Int: Int] = [:]
        /// The `limit` of each message read, by port.
        var reads: [Int: [String]] = [:]
        /// Message reads answered, by port.
        var answered: [Int: Int] = [:]
    }
    static let state = Mutex(State())
    static func reads(_ port: Int) -> [String]? { state.withLock { $0.reads[port] } }

    static let image = #"{"kind":"image","attachmentId":"attachment_0123456789abcdef0123456789abcdef","filename":"shot.png","mediaType":"image/png"}"#
    static let note = #"{"kind":"annotations","annotations":[{"originalAnnotationId":5,"anchor":{"kind":"workspace_file","workspaceFile":{"workspaceId":"workspace_test","path":"README.md","fileRevision":"file_test","startLine":7,"endLine":7}},"body":"testing","preview":{"startLine":7,"endLine":7,"text":"Frozen source"}}]}"#
    static let scenarios: [Int: Scenario] = [
        19261: Scenario(text: "Attachment", stored: "[\(image)]"),
        19262: Scenario(text: "Look at this", stored: #"[{"kind":"text","text":"Look at this"},"# + image + "]"),
        19263: Scenario(text: "Attachment", stored: nil),
        19264: Scenario(text: "Attachment", stored: "[\(image.replacingOccurrences(of: "shot.png", with: "late.png"))]",
                        lateAfterRefresh: true),
        19265: Scenario(text: "Annotations", stored: "[\(note)]", annotations: true),
    ]
    static let messageID = "message_0123456789abcdef0123456789abcdef"

    static func message(_ content: String) -> String {
        #"{"id":"\#(messageID)","turnId":"t","sequence":1,"role":"user","createdAt":"2026-11-05T00:00:00Z","content":\#(content)}"#
    }

    static func event(_ sequence: Int, _ fields: String) -> String {
        #"{"streamId":"stream","sequence":\#(sequence),"sessionId":"s",\#(fields)}"#
    }

    static func frame(_ events: [String], first: Int, last: Int) -> String {
        "event: session.events\ndata: {\"streamId\":\"stream\",\"firstSequence\":\(first),\"lastSequence\":\(last),\"events\":[\(events.joined(separator: ","))]}\n\n"
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let url = request.url!
        let port = url.port!
        let scenario = Self.scenarios[port]!
        if url.path.hasSuffix("/events/stream") {
            stream(url, scenario)
        } else if url.path.hasSuffix("/messages") {
            read(url, scenario)
        } else {
            let count = Self.state.withLock { state in
                state.snapshots[port, default: 0] += 1
                return state.snapshots[port]!
            }
            // The refresh at the turn's end holds the stored message.
            let refreshed = count > 1
            let messages = refreshed ? "[\(Self.message("[\(Self.image)]"))]" : "[]"
            let body = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0},"messages":\#(messages),"eventStreamId":"stream","eventCursor":\#(refreshed ? 3 : 0)}"#
            respond(url, status: 200, type: "application/json", body: body)
        }
    }

    override func stopLoading() {}

    /// Starts the turn and adds its message, and stays open. Once the read
    /// is answered, a page request follows, marking that the stream went on.
    private func stream(_ url: URL, _ scenario: Scenario) {
        var frames = [
            Self.frame([Self.event(1, #""turnId":"t","kind":"turn.started","status":"running""#)], first: 1, last: 1),
        ]
        var last = 2
        if scenario.annotations {
            frames.append(Self.frame([
                Self.event(2, #""turnId":"t","kind":"user.message.added","text":"\#(scenario.text)""#),
                Self.event(3, #""turnId":"","kind":"annotation.submitted","annotationIds":[5],"acceptedMessageId":"\#(Self.messageID)""#),
            ], first: 2, last: 3))
            last = 3
        } else {
            frames.append(Self.frame([Self.event(2, #""turnId":"t","kind":"user.message.added","text":"\#(scenario.text)""#)], first: 2, last: 2))
        }
        if scenario.lateAfterRefresh {
            frames.append(Self.frame([Self.event(3, #""turnId":"t","kind":"turn.completed","status":"completed""#)], first: 3, last: 3))
            last = 3
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil,
            headerFields: ["Content-Type": "text/event-stream"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(frames.joined().utf8))
        let port = url.port!
        let later = Self.frame([Self.event(last + 1, #""kind":"url.open.requested","url":"https://example.com/after""#)],
                               first: last + 1, last: last + 1)
        DispatchQueue.global().async { [self] in
            while Self.state.withLock({ ($0.answered[port] ?? 0) == 0 }) { usleep(5_000) }
            client?.urlProtocol(self, didLoad: Data(later.utf8))
        }
    }

    private func read(_ url: URL, _ scenario: Scenario) {
        let port = url.port!
        let limit = URLComponents(url: url, resolvingAgainstBaseURL: false)!.queryItems?
            .first { $0.name == "limit" }?.value ?? ""
        Self.state.withLock { $0.reads[port, default: []].append(limit) }
        DispatchQueue.global().async { [self] in
            if scenario.lateAfterRefresh {
                while Self.state.withLock({ ($0.snapshots[port] ?? 0) < 2 }) { usleep(5_000) }
            }
            if let stored = scenario.stored {
                respond(url, status: 200, type: "application/json",
                        body: #"{"SessionID":"s","HasMore":false,"Messages":[\#(Self.message(stored))]}"#)
            } else {
                respond(url, status: 500, type: "application/json",
                        body: #"{"error":{"code":"internal","message":"no"}}"#)
            }
            Self.state.withLock { $0.answered[port, default: 0] += 1 }
        }
    }

    private func respond(_ url: URL, status: Int, type: String, body: String) {
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil,
            headerFields: ["Content-Type": type])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
}

/// Every session the watch delivered.
private actor Deliveries {
    private(set) var all: [SessionExcerpt] = []
    func record(_ session: SessionExcerpt) { all.append(session) }
    var user: TranscriptMessage? { all.last?.messages.last { $0.role == "user" } }
    /// Whether the page request sent after the read was answered arrived.
    var wentOn: Bool { all.last?.requestedPages?.isEmpty == false }
}

struct LiveUserMessageTransportTests {
    private func watch(_ port: Int) throws -> (Task<Void, any Error>, Deliveries) {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [LiveUserMessageResponse.self]
        let client = try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test",
                                    instance: "test", serverID: "test", configuration: configuration)
        let deliveries = Deliveries()
        return (Task { try await client.watch("s") { await deliveries.record($0) } }, deliveries)
    }

    @Test func anImageAloneShowsBeforeTheTurnEnds() async throws {
        let (watch, deliveries) = try watch(19261)
        defer { watch.cancel() }
        try await eventually { await deliveries.user?.attachments?.isEmpty == false }
        let user = try #require(await deliveries.user)
        #expect(user.id == LiveUserMessageResponse.messageID)
        #expect(user.text == "")
        #expect(user.attachments == [TranscriptAttachment(id: "attachment_0123456789abcdef0123456789abcdef",
                                                         filename: "shot.png", mediaType: "image/png", isImage: true)])
        #expect(await deliveries.all.last?.activity != nil)
        #expect(LiveUserMessageResponse.reads(19261) == ["20"])
    }

    @Test func textWithAnImageKeepsItsTextAndGainsTheImage() async throws {
        let (watch, deliveries) = try watch(19262)
        defer { watch.cancel() }
        try await eventually { await deliveries.user?.attachments?.isEmpty == false }
        let user = try #require(await deliveries.user)
        #expect(user.text == "Look at this")
        #expect(user.attachments?.map(\.filename) == ["shot.png"])
    }

    @Test func aFailedReadLeavesTheMessageAsItCame() async throws {
        let (watch, deliveries) = try watch(19263)
        defer { watch.cancel() }
        try await eventually { await deliveries.wentOn }
        let user = try #require(await deliveries.user)
        #expect(user.id == "live-user-t")
        #expect(user.text == "Attachment")
        #expect(user.attachments == [])
        #expect(LiveUserMessageResponse.reads(19263) == ["20"])
    }

    @Test func aReadAnsweredAfterTheTurnEndRefreshChangesNothing() async throws {
        let (watch, deliveries) = try watch(19264)
        defer { watch.cancel() }
        try await eventually { await deliveries.wentOn }
        let user = try #require(await deliveries.user)
        #expect(user.id == LiveUserMessageResponse.messageID)
        #expect(user.attachments?.map(\.filename) == ["shot.png"])
        #expect(await deliveries.all.allSatisfy { session in
            session.messages.allSatisfy { $0.attachments?.contains { $0.filename == "late.png" } != true }
        })
    }

    @Test func aMessageWithCommentsIsReadOnlyAsASubmission() async throws {
        let (watch, deliveries) = try watch(19265)
        defer { watch.cancel() }
        try await eventually { await deliveries.wentOn }
        let user = try #require(await deliveries.user)
        #expect(user.annotations?.map(\.body) == ["testing"])
        #expect(LiveUserMessageResponse.reads(19265) == ["100"])
    }
}
