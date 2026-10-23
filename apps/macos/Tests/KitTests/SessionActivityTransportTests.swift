import Foundation
import Testing
@testable import Kit

private final class SessionActivityResponse: URLProtocol, @unchecked Sendable {
    /// The body each port answers with.
    static let bodies: [Int: String] = [
        19311: #"{"sessions":[{"sessionId":"session_a","status":"running"},{"sessionId":"session_b","status":"awaitingResponse"}]}"#,
        19312: #"{"sessions":[{"sessionId":"session_a","status":"idle"}]}"#,
        19313: #"{"sessions":[{"sessionId":"session_a","status":"running"},{"sessionId":"session_a","status":"running"}]}"#,
        19314: #"{"sessions":[{"sessionId":"../a","status":"running"}]}"#,
    ]
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(url.path == "/v1/sessions/activity")
        #expect(request.httpMethod == "GET")
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200,
            httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data((Self.bodies[url.port ?? 0] ?? "").utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct SessionActivityTransportTests {
    private func client(port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [SessionActivityResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test",
            instance: "test", serverID: "test", configuration: configuration)
    }

    @Test func reportedSessionsCarryTheirStatus() async throws {
        let activity = try await client(port: 19311).sessionActivity()
        #expect(activity == ["session_a": .running, "session_b": .awaitingResponse])
    }

    @Test(arguments: [19312, 19313, 19314])
    func malformedReportsAreRejected(port: Int) async throws {
        let error = await #expect(throws: ClientError.self) { _ = try await client(port: port).sessionActivity() }
        guard case .invalidPayload = error else { Issue.record("error = \(String(describing: error)), want invalid payload"); return }
    }

    /// A server that can't report its activity leaves its sessions without
    /// status, and the others keep theirs.
    @Test func eachServersStatusesAreKeyedByTreeKey() async throws {
        let statuses = await SessionPickerView.activity([
            (server: ServerClient.localID, client: try client(port: 19311)),
            (server: "devbox", client: try client(port: 19311)),
            (server: "broken", client: try client(port: 19312)),
        ])
        #expect(statuses == ["session_a": .running, "session_b": .awaitingResponse,
                             "devbox/session_a": .running, "devbox/session_b": .awaitingResponse])
    }
}
