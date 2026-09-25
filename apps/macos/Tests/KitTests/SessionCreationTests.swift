import Foundation
import Testing
@testable import Kit

private final class CreationResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        let body: String
        let status: Int
        if url.path == "/v1/sessions/resync" {
            status = 200
            body = #"{"session":{"id":"resync","cwd":"/tmp","model":"provider/model","thinkingLevel":"medium","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0},"eventStreamId":"stream","eventCursor":0}"#
        } else if url.path.hasSuffix("/events/stream") {
            status = 200
            body = url.port == 19204 ? "" : url.port == 19203
                ? "event: session.events\ndata: {\"streamId\":\"next-stream\",\"events\":[]}\n\n"
                : "event: session.resync\ndata: {}\n\n"
        } else if url.path == "/v1/models" {
            #expect(request.httpMethod == "GET")
            status = 200
            body = #"{"models":[{"id":"provider/model","name":"Test model","provider":"provider","api":"test","contextWindow":10000,"thinkingLevels":["off","medium"],"inputs":["text"],"available":true},{"id":"other/model","name":"Unavailable","provider":"other","api":"test","contextWindow":10000,"thinkingLevels":["off"],"inputs":["text"],"available":false}]}"#
        } else {
            #expect(url.path == "/v1/sessions")
            #expect(request.httpMethod == "POST")
            #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
            var data = request.httpBody ?? Data()
            if let stream = request.httpBodyStream {
                stream.open(); defer { stream.close() }
                var buffer = [UInt8](repeating: 0, count: 1024)
                while stream.hasBytesAvailable {
                    let count = stream.read(&buffer, maxLength: buffer.count)
                    if count <= 0 { break }; data.append(contentsOf: buffer.prefix(count))
                }
            }
            let input = try? JSONDecoder().decode(WireCreateSessionInput.self, from: data)
            #expect(input?.cwd == "/tmp/test")
            #expect(input?.name == "New workspace")
            #expect(input?.model == "provider/model")
            #expect(input?.thinkingLevel == "medium")
            #expect(input?.temporary == (url.port == 19205))
            if url.port == 19205 { #expect(input?.id == "session_test") }
            status = url.port == 19202 ? 400 : 201
            body = #"{"id":"session_test","cwd":"/tmp/test","name":"New workspace","model":"provider/model","thinkingLevel":"medium","configurationRevision":1,"createdAt":"2026-09-13T00:00:00Z","updatedAt":"2026-09-13T00:00:00Z"}"#
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil,
            headerFields: ["Content-Type":url.path.hasSuffix("/events/stream") ? "text/event-stream" : "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct SessionCreationTests {
    private func client(_ port: Int = 19201) throws -> HTTPClient {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [CreationResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "test", serverID: "test", configuration: config)
    }
    private var input: WireCreateSessionInput {
        WireCreateSessionInput(id: nil, cwd: "/tmp/test", name: "New workspace", model: "provider/model", thinkingLevel: "medium", temporary: false)
    }
    @Test func availableModelsAndCreatedSessionUseCanonicalContract() async throws {
        let client = try client()
        #expect(try await client.models().map(\.id) == ["provider/model"])
        let created = try await client.createSession(input)
        #expect(created.id == "session_test")
        #expect(created.title == "New workspace")
        #expect(created.cwd == "/tmp/test")
        #expect(created.messages.isEmpty)
    }
    @Test func temporaryCreationUsesClientSelectedIdentity() async throws {
        let created = try await client(19205).createSession(WireCreateSessionInput(id: "session_test",
            cwd: "/tmp/test", name: "New workspace", model: "provider/model", thinkingLevel: "medium", temporary: true))
        #expect(created.id == "session_test")
    }

    @Test func failedCreationDoesNotReturnAnInventedSession() async throws {
        await #expect(throws: ClientError.self) { try await client(19202).createSession(input) }
    }
    @Test(arguments: [19201, 19203, 19204]) func serverResyncReattachesImmediatelyWithABoundedRetryCount(port: Int) async throws {
        actor Received {
            var ids: [String] = []
            func append(_ session: SessionExcerpt) { ids.append(session.id) }
        }
        let received = Received()
        do {
            try await client(port).watch("resync") { await received.append($0) }
            Issue.record("Repeated resyncs must eventually report a connection failure")
        } catch {
            guard case ClientError.disconnected = error else { throw error }
        }
        #expect(await received.ids == ["resync", "resync", "resync", "resync"])
    }
    @MainActor @Test func createdSessionIsImmediatelyAvailableInCatalog() {
        let app = AppModel()
        let created = SessionExcerpt(id: "s", title: "New workspace", sourceTitle: "Kit server", model: "provider/model", thinking: "medium", workspace: "test", cwd: "/tmp/test", date: "2026-09-13", messages: [])
        app.recordCreated(created); app.recordCreated(created)
        #expect(app.sessions.map(\.id) == ["s"])
        #expect(app.sessions.first?.title == "New workspace")
    }
}
