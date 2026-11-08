import Foundation
import Testing
@testable import Kit

private final class SessionAttentionResponse: URLProtocol, @unchecked Sendable {
    static func session(_ id: String, updated: String, activity: String? = nil) -> String {
        #"{"id":"\#(id)","cwd":"/work","model":"test/echo","thinkingLevel":"medium","configurationRevision":1,"createdAt":"2026-03-23T12:00:00Z","updatedAt":"\#(updated)""#
            + (activity.map { #","activity":\#($0)"# } ?? "") + "}"
    }

    static let waiting = #"{"status":"awaitingResponse","since":"2026-03-23T12:10:00.123456789Z"}"#
    static let running = #"{"status":"running"}"#

    /// The body each port answers with.
    static let bodies: [Int: String] = [
        19911: #"{"sessions":["# + [
            session("session_b", updated: "2026-03-23T12:20:00Z", activity: waiting),
            session("session_c", updated: "2026-03-23T12:25:00Z", activity: running),
            session("session_a", updated: "2026-03-23T12:30:00Z"),
        ].joined(separator: ",") + #"],"order":"attention"}"#,
        // A server that predates the order lists its sessions without one.
        19912: #"{"sessions":["# + session("session_a", updated: "2026-03-23T12:30:00Z") + "]}",
        // Idle before running.
        19913: #"{"sessions":["# + [
            session("session_a", updated: "2026-03-23T12:30:00Z"),
            session("session_c", updated: "2026-03-23T12:25:00Z", activity: running),
        ].joined(separator: ",") + #"],"order":"attention"}"#,
        // Waiting without since.
        19914: #"{"sessions":["# + session("session_b", updated: "2026-03-23T12:20:00Z", activity: #"{"status":"awaitingResponse"}"#) + #"],"order":"attention"}"#,
        // Activity without the order.
        19916: #"{"sessions":["# + session("session_c", updated: "2026-03-23T12:20:00Z", activity: running) + "]}",
        // Running with since.
        19915: #"{"sessions":["# + session("session_c", updated: "2026-03-23T12:20:00Z", activity: #"{"status":"running","since":"2026-03-23T12:10:00Z"}"#) + #"],"order":"attention"}"#,
    ]

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(url.path == "/v1/sessions")
        #expect(url.query == "order=attention")
        #expect(request.httpMethod == "GET")
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200,
            httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data((Self.bodies[url.port ?? 0] ?? "").utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct SessionAttentionTransportTests {
    private func client(port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [SessionAttentionResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test",
            instance: "test", serverID: "test", configuration: configuration)
    }

    @Test func sessionsComeInTheServersOrderWithWhatTheyAreDoing() async throws {
        guard case let .ordered(listed) = try await client(port: 19911).sessionsByAttention() else {
            Issue.record("expected an ordered list"); return
        }
        #expect(listed.map(\.session.id) == ["session_b", "session_c", "session_a"])
        #expect(listed.map(\.status) == [.awaitingResponse, .running, .idle])
        let since = try #require(listed[0].waitingSince)
        #expect(abs(since.timeIntervalSince1970 - 1_774_267_800.123) < 0.001)
        #expect(listed[1].waitingSince == nil && listed[2].waitingSince == nil)
    }

    /// A server that predates the order ignores it, and its list comes back
    /// as it sent it, so the client needs only its activity besides.
    @Test func anOlderServersListComesBackUnordered() async throws {
        guard case let .unordered(sessions) = try await client(port: 19912).sessionsByAttention() else {
            Issue.record("expected an unordered list"); return
        }
        #expect(sessions.map(\.id) == ["session_a"])
    }

    @Test(arguments: [19913, 19914, 19915, 19916])
    func malformedListsAreRejected(port: Int) async throws {
        let error = await #expect(throws: ClientError.self) { _ = try await client(port: port).sessionsByAttention() }
        guard case .invalidPayload = error else { Issue.record("error = \(String(describing: error)), want invalid payload"); return }
    }

    @Test func theOrderPutsWaitingOldestFirstThenRunningThenIdleNewestFirst() {
        let date = { (minute: Int) in Date(timeIntervalSince1970: 1_774_267_200 + Double(minute) * 60) }
        let keys: [AttentionOrder.Key] = [
            .init(status: .idle, updated: date(40), id: "g"),
            .init(status: .running, updated: date(20), id: "d"),
            .init(status: .awaitingResponse, waitingSince: date(10), updated: date(31), id: "b"),
            .init(status: .idle, updated: date(5), id: "h"),
            .init(status: .awaitingResponse, waitingSince: date(4), updated: date(30), id: "c"),
            .init(status: .running, updated: date(29), id: "e"),
            .init(status: .awaitingResponse, waitingSince: date(10), updated: date(31), id: "a"),
            .init(status: .idle, updated: date(5), id: "f"),
        ]
        #expect(keys.sorted(by: AttentionOrder.precedes).map(\.id) == ["c", "a", "b", "e", "d", "g", "f", "h"])
    }

    /// An older server doesn't say when a session started waiting, so it's
    /// placed by its last change.
    @Test func aWaitingSessionWithoutSinceIsPlacedByItsLastChange() {
        let date = { (minute: Int) in Date(timeIntervalSince1970: 1_774_267_200 + Double(minute) * 60) }
        let keys: [AttentionOrder.Key] = [
            .init(status: .awaitingResponse, updated: date(30), id: "a"),
            .init(status: .awaitingResponse, waitingSince: date(20), updated: date(31), id: "b"),
        ]
        #expect(keys.sorted(by: AttentionOrder.precedes).map(\.id) == ["b", "a"])
    }
}
