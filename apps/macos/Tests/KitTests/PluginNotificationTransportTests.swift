import Foundation
import Testing
@testable import Kit

private final class PluginNotificationResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(request.httpMethod == "GET")
        #expect(url.path == "/v1/sessions/session_one/plugin-toasts")
        #expect(url.query == nil)
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Instance-ID") == "server")
        let status = url.port == 19202 ? 401 : 200
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil,
            headerFields: ["Content-Type": "application/x-ndjson"])!, cacheStoragePolicy: .notAllowed)
        let body = #"{"pluginId":"demo","instance":"host:1","title":"Notice","variant":"info"}"#
        let frame = url.port == 19203 ? body : "\n\r\n" + body + "\n"
        // Deliberately fragment a frame across transport callbacks.
        for chunk in [String(frame.prefix(20)), String(frame.dropFirst(20))] { client?.urlProtocol(self, didLoad: Data(chunk.utf8)) }
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}
private actor NotificationCollector {
    var values: [String] = []
    func append(_ notification: PluginNotification) { values.append(notification.title) }
}
struct PluginNotificationTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let config = URLSessionConfiguration.ephemeral; config.protocolClasses = [PluginNotificationResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "server", serverID: "test", configuration: config)
    }
    @Test func streamsFramesAcrossChunksAndIgnoresHeartbeats() async throws {
        let collector = NotificationCollector()
        do {
            try await client(19201).watchPluginNotifications("session_one") { await collector.append($0) }
            Issue.record("EOF should end the subscription")
        } catch ClientError.disconnected { }
        #expect(await collector.values == ["Notice"])
    }
    @Test func rejectsUnauthenticatedResponsesAndTruncatedFrames() async throws {
        for port in [19202, 19203] {
            let collector = NotificationCollector()
            await #expect(throws: ClientError.self) {
                try await client(port).watchPluginNotifications("session_one") { await collector.append($0) }
            }
            #expect(await collector.values.isEmpty)
        }
    }
}
