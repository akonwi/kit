import Foundation
import Testing
@testable import Kit

private final class AcceptedAnnotationResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)!.queryItems ?? []
        #expect(url.path == "/v1/sessions/session_test/messages")
        #expect(query.contains { $0.name == "role" && $0.value == "user" })
        #expect(query.contains { $0.name == "limit" && $0.value == "100" })
        let noteID = url.port == 19212 ? 6 : 5
        let response = """
        {"SessionID":"session_test","HasMore":false,"Messages":[
          {"id":"message_0123456789abcdef0123456789abcdef","turnId":"turn_test","sequence":1,
           "role":"user","createdAt":"2026-09-17T00:00:00Z","content":[
             {"kind":"annotations","annotations":[
               {"originalAnnotationId":\(noteID),"anchor":{"kind":"workspace_file","workspaceFile":{
                 "workspaceId":"workspace_test","path":"README.md","fileRevision":"file_test","startLine":7,"endLine":7}},
                "body":"testing","preview":{"startLine":7,"endLine":7,"text":"Frozen source"}}]}]}]}
        """
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200,
            httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(response.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct AnnotationLiveTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [AcceptedAnnotationResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test",
            instance: "test", serverID: "test", configuration: configuration)
    }
    private func acceptedEvent() throws -> WireSessionEvent {
        let json = #"{"streamId":"stream","sequence":3,"sessionId":"session_test","turnId":"","kind":"annotation.submitted","annotationIds":[5],"acceptedMessageId":"message_0123456789abcdef0123456789abcdef"}"#
        return try JSONDecoder().decode(WireSessionEvent.self, from: Data(json.utf8))
    }

    @Test func acceptedMessageIsAvailableDuringActiveTurn() async throws {
        let message = try await client(19211).acceptedAnnotationMessage(session: "session_test", event: acceptedEvent())
        #expect(message.id == "message_0123456789abcdef0123456789abcdef")
        let row = try #require(SessionProjection.transcript([message]).first)
        #expect(row.annotations?.map(\.body) == ["testing"])
        #expect(row.annotations?.map(\.source) == ["Frozen source"])
    }

    @Test func mismatchedAcceptedEvidenceIsRejected() async throws {
        await #expect(throws: ClientError.self) {
            try await client(19212).acceptedAnnotationMessage(session: "session_test", event: acceptedEvent())
        }
    }
}
