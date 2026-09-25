import Foundation
import Testing
@testable import Kit

private let vcsFrame = #"{"sessionId":"session_one","cwd":"/repo","status":{"root":"/repo","head":{"kind":"branch","name":"main"},"dirty":false,"pullRequest":{"number":42,"url":"https://github.com/a/b/pull/42"}}}"#

private final class VCSResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(url.path == "/v1/sessions/session_one/vcs/events")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        #expect(request.value(forHTTPHeaderField: "X-Kit-Instance-ID") == "server")
        let code = url.port == 19602 ? 401 : 200
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: code, httpVersion: nil,
            headerFields: ["Content-Type": "application/x-ndjson"])!, cacheStoragePolicy: .notAllowed)
        let frame = url.port == 19603 ? vcsFrame : "\n\r\n" + vcsFrame + "\n"
        for chunk in [String(frame.prefix(20)), String(frame.dropFirst(20))] { client?.urlProtocol(self, didLoad: Data(chunk.utf8)) }
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}
private actor VCSCollector {
    var numbers: [Int] = []
    func append(_ value: WireSessionVCSStatus) { if let number = value.status?.pullRequest?.number { numbers.append(number) } }
}
struct VCSStreamTests {
    @Test func validatesCombinedStateAndRejectsMalformedNestedFrames() throws {
        let state = try VCSStreamFrame.decode(Data(vcsFrame.utf8), session: "session_one")
        #expect(state.status?.head.name == "main")
        #expect(state.status?.pullRequest?.number == 42)
        let invalid = [
            vcsFrame.replacingOccurrences(of: "\"number\":42", with: "\"number\":42,\"nu\\u006dber\":43"),
            vcsFrame.replacingOccurrences(of: "\"name\":\"main\"", with: "\"name\":\"\""),
            vcsFrame.replacingOccurrences(of: "\"dirty\":false", with: "\"dirty\":false,\"extra\":true"),
            vcsFrame.replacingOccurrences(of: "https://github.com/a/b/pull/42", with: "file:///tmp/a"),
            vcsFrame.replacingOccurrences(of: "session_one", with: "session_other"),
            "{", "[]", vcsFrame + vcsFrame,
        ]
        for frame in invalid {
            #expect(throws: (any Error).self) { try VCSStreamFrame.decode(Data(frame.utf8), session: "session_one") }
        }
        #expect(throws: (any Error).self) { try VCSStreamFrame.decode(Data(repeating: 32, count: 64 * 1024), session: "session_one") }
    }
    private func client(_ port: Int) throws -> HTTPClient {
        let config = URLSessionConfiguration.ephemeral; config.protocolClasses = [VCSResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "server", serverID: "test", configuration: config)
    }
    @Test func streamsFragmentedFramesAndIgnoresHeartbeats() async throws {
        let collector = VCSCollector()
        do { try await client(19601).watchVCS("session_one") { await collector.append($0) }; Issue.record("expected EOF") }
        catch ClientError.disconnected {}
        #expect(await collector.numbers == [42])
    }
    @Test func rejectsAuthenticationAndPartialFrames() async throws {
        for port in [19602, 19603] {
            let collector = VCSCollector()
            await #expect(throws: ClientError.self) { try await client(port).watchVCS("session_one") { await collector.append($0) } }
            #expect(await collector.numbers == [])
        }
    }
}
