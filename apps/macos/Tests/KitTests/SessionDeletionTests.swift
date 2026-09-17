import Foundation
import Testing
@testable import Kit

private actor DeletionClient: SessionDeletionClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var present = true
    let loseAcknowledgement: Bool
    let busy: Bool
    var attempts = 0
    init(loseAcknowledgement: Bool = false, busy: Bool = false) {
        self.loseAcknowledgement = loseAcknowledgement; self.busy = busy
    }
    func sessions() async throws -> [SessionExcerpt] { present ? [Self.value] : [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        guard present else { throw ClientError.http(404) }
        return Self.value
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func deleteSession(_ id: String) async throws {
        attempts += 1
        if busy { throw ClientError.http(409) }
        present = false
        if loseAcknowledgement { throw ClientError.disconnected }
    }
    static let value = SessionExcerpt(id: "a", title: "Session", sourceTitle: "Test", model: "m",
        thinking: "high", workspace: "Test", date: "", messages: [])
}

@MainActor struct SessionDeletionTests {
    @Test func lostAcknowledgementReconcilesWithoutRepeatingDelete() async throws {
        let client = DeletionClient(loseAcknowledgement: true)
        try await SessionDeletion.perform("a", client: client)
        #expect(await client.attempts == 1)
        #expect(try await client.sessions().isEmpty)
    }

    @Test func busySessionRemainsAvailable() async throws {
        let client = DeletionClient(busy: true)
        do {
            try await SessionDeletion.perform("a", client: client)
            Issue.record("Expected busy rejection")
        } catch ClientError.http(409) {}
        #expect(try await client.sessions().map(\.id) == ["a"])
    }

    @Test func deletionOnlyAffectsMatchingServerAndRetainsLocalWork() {
        let store = SessionStore(fixture: Fixture(sessions: [DeletionClient.value]), client: DeletionClient())
        store.ui.draft = "Unsent draft"
        store.ui.workspace.scratchpad = "Notes"
        store.sessionDeleted(SessionIdentity(server: "other", session: "a"))
        #expect(store.sessions.map(\.id) == ["a"])
        store.sessionDeleted(SessionIdentity(server: "test", session: "a"))
        #expect(store.unavailable)
        #expect(store.selected?.title == "Session")
        #expect(store.ui.draft == "Unsent draft")
        #expect(store.ui.workspace.scratchpad == "Notes")
        #expect(store.sessions.isEmpty)
    }
}

private final class DeleteResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let valid = ((request.httpMethod == "DELETE" && request.url?.path == "/v1/sessions/session_test")
            || (request.httpMethod == "POST" && request.url?.path == "/v1/sessions/session_test/dispose"))
            && request.value(forHTTPHeaderField: "Authorization") == "Bearer test"
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!,
            statusCode: valid ? 204 : 400, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct SessionDeletionTransportTests {
    @Test func authenticatedDeleteAcceptsEmpty204Response() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [DeleteResponse.self]
        let client = try HTTPClient(endpoint: URL(string: "http://127.0.0.1:18200")!,
            token: "test", instance: "test", serverID: "test", configuration: configuration)
        try await client.deleteSession("session_test")
        try await client.disposeSession("session_test")
    }
}
