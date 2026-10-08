import Foundation
import Testing
@testable import Kit

private final class ForkResultResponse: URLProtocol, @unchecked Sendable {
    static let source = "session_0123456789abcdef0123456789abcdef"
    /// Port 19302 answers with a renderer-unsafe first-turn message.
    static let unsafePort = 19302
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let child = #""session":{"id":"session_cccccccccccccccccccccccccccccccc","cwd":"/work","model":"test/echo","thinkingLevel":"high","configurationRevision":1,"parentSessionId":"session_0123456789abcdef0123456789abcdef","createdAt":"2026-03-23T12:34:56Z","updatedAt":"2026-03-23T12:34:56Z"}"#
        let message = url.port == Self.unsafePort ? #"session\u001bis unavailable"# : "session is unavailable"
        let body = "{" + child + #","firstTurnError":{"code":"unavailable","message":""# + message + #""}}"#
        #expect(url.path == "/v1/sessions/\(Self.source)/forks")
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 201,
            httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@MainActor struct ForkTransportTests {
    private func client(port: Int = 19301) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ForkResultResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test",
            instance: "test", serverID: "test", configuration: configuration)
    }

    @Test func firstTurnErrorArrivesWithThePublishedChild() async throws {
        let prompt = WirePromptInput(text: "Explore", attachmentIds: nil, annotationIds: nil)
        let forked = try await client().forkSession(ForkResultResponse.source, input: .init(name: nil, prompt: prompt))
        #expect(forked.session.id == "session_cccccccccccccccccccccccccccccccc")
        #expect(forked.session.parentSessionID == ForkResultResponse.source)
        #expect(forked.firstTurnError == "session is unavailable")
    }

    @Test func firstTurnErrorWithoutAPromptIsRejected() async throws {
        let error = await #expect(throws: ClientError.self) {
            _ = try await client().forkSession(ForkResultResponse.source, input: .init(name: nil, prompt: nil))
        }
        guard case .invalidPayload = error else { Issue.record("error = \(String(describing: error)), want invalid payload"); return }
    }

    @Test func unsafeFirstTurnErrorMessageIsRejected() async throws {
        let prompt = WirePromptInput(text: "Explore", attachmentIds: nil, annotationIds: nil)
        let error = await #expect(throws: ClientError.self) {
            _ = try await client(port: ForkResultResponse.unsafePort).forkSession(ForkResultResponse.source, input: .init(name: nil, prompt: prompt))
        }
        guard case .invalidPayload = error else { Issue.record("error = \(String(describing: error)), want invalid payload"); return }
    }
}
