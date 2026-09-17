import Foundation
import Testing
@testable import Kit

private final class MutationResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let rejected = url.port == 19102
        let wrong = url.port == 19103
        let body = rejected ? #"{"error":"invalid input"}"# : "{\"reservation\":{\"sessionId\":\"\(wrong ? "wrong" : "s")\",\"turnId\":\"turn\",\"runId\":\"run\"},\"queued\":false,\"queue\":{\"count\":0}}"
        #expect(request.httpMethod == "POST")
        #expect(url.path == "/v1/sessions/s/submissions")
        #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: rejected ? 400 : 202,
            httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct MutationTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MutationResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "test", serverID: "test", configuration: configuration)
    }
    @Test func acceptedSubmissionReturnsCanonicalReservation() async throws {
        let result = try await client(19101).submit("s", input: WirePromptInput(text: "Hello", attachmentIds: [], annotationIds: nil))
        #expect(result.reservation?.turnId == "turn")
        #expect(result.reservation?.runId == "run")
        #expect(result.queued == false)
    }
    @Test func rejectionAndWrongIdentityAreRejected() async throws {
        await #expect(throws: ClientError.self) { try await client(19102).submit("s", input: WirePromptInput(text: "Hello", attachmentIds: [], annotationIds: nil)) }
        await #expect(throws: ClientError.self) { try await client(19103).submit("s", input: WirePromptInput(text: "Hello", attachmentIds: [], annotationIds: nil)) }
    }
}
