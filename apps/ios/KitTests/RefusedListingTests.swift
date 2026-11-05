import Foundation
import KitClient
import Testing
@testable import Kit

/// Refuses every request the way a server does once this iPhone's pairing is
/// revoked.
private final class RevokedPairing: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let body = #"{"error":{"code":"unauthorized","message":"Pair again."}}"#
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: 401, httpVersion: nil,
            headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

/// Lists sessions through a real client whose server refuses it.
private struct RevokedNetwork: SessionsNetwork {
    let http: HTTPClient

    init() throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RevokedPairing.self]
        http = try HTTPClient(endpoint: URL(string: "https://studio.example.ts.net")!, token: "test", instance: "test",
                              serverID: "studio", remote: true, configuration: configuration)
    }

    func sessions(on server: SavedServer) async throws -> [SessionExcerpt] { try await http.sessions() }
    func activity(on server: SavedServer) async throws -> [String: SessionTabStatus] { try await http.sessionActivity() }
    func snapshot(_ id: String, on server: SavedServer) async throws -> SessionExcerpt { try await http.snapshot(id) }
    func client(for server: SavedServer) -> ServerClient {
        ServerClient(serverID: server.name, connector: .url(server.address, credential: DeviceCredential(home: "test", name: server.name)),
                     tokens: MemoryTokens())
    }
}

@MainActor
struct RefusedListingTests {
    @Test func aServerThatNoLongerKnowsThisIPhoneAsksToPairAgain() async throws {
        let studio = SavedServer(name: "studio", kind: .url, address: "https://studio.example.ts.net")
        let store = SessionsStore(network: try RevokedNetwork(), seen: SeenSessions(defaults: UserDefaults(suiteName: UUID().uuidString)!))
        await store.refresh([studio])
        #expect(store.problems.map(\.server) == ["studio"])
        #expect(store.problems.first?.state == .needsPairing(.refused))
    }
}
