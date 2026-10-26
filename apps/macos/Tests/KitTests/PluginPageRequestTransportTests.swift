import Foundation
import Testing
@testable import Kit

/// Serves a session whose turn completes, and then a page request the
/// turn-end refresh snapshot's cursor already covers.
private final class PageRequestResponse: URLProtocol, @unchecked Sendable {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var snapshots = 0
    nonisolated(unsafe) private static var streams = 0
    static func next(_ counter: ReferenceWritableKeyPath<PageRequestResponse.Type, Int>) -> Int {
        lock.lock(); defer { lock.unlock() }
        self[keyPath: counter] += 1
        return self[keyPath: counter]
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let body: String
        if url.path.hasSuffix("/events/stream") {
            let frame = { (events: String) in "event: session.events\ndata: {\"streamId\":\"stream\",\"firstSequence\":1,\"lastSequence\":3,\"events\":[\(events)]}\n\n" }
            body = Self.next(\.streams) > 1 ? "" : [
                frame(#"{"streamId":"stream","sequence":1,"sessionId":"s","turnId":"t","kind":"turn.started","status":"running"}"#),
                frame(#"{"streamId":"stream","sequence":2,"sessionId":"s","turnId":"t","kind":"turn.completed","status":"completed"}"#),
                frame(#"{"streamId":"stream","sequence":3,"sessionId":"s","kind":"url.open.requested","url":"https://example.com/preview"}"#),
            ].joined()
        } else {
            // The refresh after the turn completes already covers sequence 3.
            let cursor = Self.next(\.snapshots) == 1 ? 0 : 3
            body = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0},"messages":[],"eventStreamId":"stream","eventCursor":\#(cursor)}"#
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil,
            headerFields: ["Content-Type": url.path.hasSuffix("/events/stream") ? "text/event-stream" : "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

private actor Deliveries {
    private(set) var pages: [RequestedPage]?
    func record(_ session: SessionExcerpt) { if let pages = session.requestedPages { self.pages = pages } }
}

struct PluginPageRequestTransportTests {
    @Test func aLiveRequestTheTurnEndRefreshAlreadyCoversStillOpens() async throws {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [PageRequestResponse.self]
        let client = try HTTPClient(endpoint: URL(string: "http://127.0.0.1:19251")!, token: "test", instance: "test", serverID: "test", configuration: config)
        let deliveries = Deliveries()
        let watch = Task { try await client.watch("s") { await deliveries.record($0) } }
        defer { watch.cancel() }
        try await eventually { await deliveries.pages != nil }
        #expect(await deliveries.pages == [RequestedPage(id: "stream:3", url: URL(string: "https://example.com/preview")!)])
    }
}
