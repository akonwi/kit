import Foundation
import Testing
@testable import Kit

private actor TemporaryCatalogClient: SessionClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var missing = false
    func remove() { missing = true }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        if missing { throw ClientError.http(404) }
        return SessionExcerpt(id: id, title: "Temporary work", sourceTitle: "Server", model: "provider/model",
                              thinking: "medium", workspace: "tmp", date: "", messages: [])
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
}

/// A server that has restarted: it lists no sessions and no longer has the
/// remembered temporary session.
private final class RestartedServerResponse: URLProtocol, @unchecked Sendable {
    static let temporary = "session_0123456789abcdef0123456789abcdef"
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let (status, body) = url.path == "/v1/sessions"
            ? (200, #"{"sessions":[]}"#)
            : (404, #"{"error":{"code":"not_found","message":"session not found"}}"#)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status,
            httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

/// Presents the HTTP client as the catalog's session client.
private struct RestartedServerClient: SessionClient, @unchecked Sendable {
    let http: HTTPClient
    var serverID: String { http.serverID }
    let isDemo = false
    func sessions() async throws -> [SessionExcerpt] { try await http.sessions() }
    func snapshot(_ id: String) async throws -> SessionExcerpt { try await http.snapshot(id) }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
}

@MainActor struct TemporarySessionTests {
    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_TEMPORARY_LIVE_TEST"] == "1"))
    func liveTemporaryLifecycle() async throws {
        let client = try await HTTPClient.local()
        let model = try #require(try await client.models().first)
        let id = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        do {
            let created = try await client.createSession(WireCreateSessionInput(id: id, cwd: "/private/tmp",
                name: "macOS temporary lifecycle verification", model: model.id,
                thinkingLevel: model.thinkingLevels?.first?.rawValue ?? "off", temporary: true))
            #expect(created.id == id)
            #expect(try await client.snapshot(id).title == "macOS temporary lifecycle verification")
            #expect(try await client.sessions().contains(where: { $0.id == id }) == false)
            try await client.disposeSession(id)
            try await client.disposeSession(id)
            do {
                _ = try await client.snapshot(id)
                Issue.record("Disposed session must be unavailable")
            } catch ClientError.http(404) {}
        } catch {
            try await client.disposeSession(id)
            throw error
        }
    }

    @Test func rememberedIdentitySurvivesAppRelaunchAndExpiresWithDaemonSession() async throws {
        let suite = "TemporarySessionTests-" + UUID().uuidString
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let references = TemporarySessions(defaults: defaults)
        references.remember(server: "test", session: "temporary")
        references.remember(server: "other", session: "elsewhere")
        let relaunched = TemporarySessions(defaults: defaults)
        let client = TemporaryCatalogClient()
        let catalog = try await relaunched.catalog(client: client)
        #expect(catalog.map(\.id) == ["temporary"])
        #expect(catalog.first?.isTemporary == true)
        #expect(catalog.first?.title == "Temporary work")
        await client.remove()
        #expect(try await relaunched.catalog(client: client).isEmpty)
        #expect(relaunched.identities == [SessionIdentity(server: "other", session: "elsewhere")])
        #expect(TemporarySessions(defaults: defaults).identities == relaunched.identities)
    }

    // Regression: the HTTP client reported a missing session as an invalid
    // payload, so a temporary session lost in a server restart failed every
    // catalog load with "The server identity or response does not match local
    // discovery" instead of being forgotten.
    @Test func temporarySessionLostInAServerRestartIsForgotten() async throws {
        let suite = "TemporarySessionTests-" + UUID().uuidString
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RestartedServerResponse.self]
        let http = try HTTPClient(endpoint: URL(string: "http://127.0.0.1:19311")!, token: "test",
            instance: "test", serverID: "local-v2", configuration: configuration)
        do {
            _ = try await http.snapshot(RestartedServerResponse.temporary)
            Issue.record("A session the server no longer has must be unavailable")
        } catch ClientError.http(404) {}
        let references = TemporarySessions(defaults: defaults)
        references.remember(server: "local-v2", session: RestartedServerResponse.temporary)
        let catalog = try await references.catalog(client: RestartedServerClient(http: http))
        #expect(catalog.map(\.id) == [])
        #expect(references.identities == [])
        #expect(TemporarySessions(defaults: defaults).identities == [])
    }
}
