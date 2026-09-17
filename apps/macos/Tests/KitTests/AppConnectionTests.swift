import Foundation
import Testing
@testable import Kit

@MainActor struct AppConnectionTests {
    private struct Client: SessionClient {
        let serverID = "local-v2"
        let isDemo = false
        var fails = false
        func sessions() async throws -> [SessionExcerpt] {
            if fails { throw ClientError.noDaemon }
            return []
        }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    }

    private actor CatalogClient: SessionClient {
        nonisolated let serverID = "local-v2"
        nonisolated let isDemo = false
        var titles = ["First"]
        func changeCatalog() { titles = ["Renamed", "New session"] }
        func sessions() async throws -> [SessionExcerpt] {
            titles.enumerated().map { index, title in
                SessionExcerpt(id: String(index), title: title, sourceTitle: "", model: "",
                               thinking: "", workspace: "", date: "", messages: [])
            }
        }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    }

    @Test func presentationRefreshUsesEstablishedClientAndLoadsNewCatalog() async {
        let client = CatalogClient()
        let app = AppModel()
        await app.connectLocal(using: client)
        #expect(app.sessions.map(\.title) == ["First"])
        await client.changeCatalog()
        await app.refreshSessions()
        #expect(app.sessions.map(\.title) == ["Renamed", "New session"])
        #expect(app.client?.serverID == "local-v2")
        #expect(app.connecting == false)
    }

    private actor SlowCatalogClient: SessionClient {
        nonisolated let serverID = "local-v2"
        nonisolated let isDemo = false
        private(set) var calls = 0
        private(set) var cancelled = false
        func sessions() async throws -> [SessionExcerpt] {
            calls += 1
            if calls == 1 { return [] }
            do { try await Task.sleep(for: .seconds(60)) }
            catch { cancelled = true; throw error }
            return []
        }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    }

    @Test func cancellingCatalogRequestReleasesConnectionState() async throws {
        let client = SlowCatalogClient()
        let app = AppModel()
        await app.connectLocal(using: client)
        let request = Task { await app.refreshSessions() }
        for _ in 0..<100 {
            if await client.calls == 2 { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(await client.calls == 2)
        request.cancel()
        await request.value
        for _ in 0..<100 {
            if await client.cancelled { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(await client.cancelled)
        #expect(app.connecting == false)
        #expect(app.error == nil)
    }

    @Test(arguments: ["", "local-v2"])
    func launchConnectsWithoutAChooser(serverID: String) async {
        let app = AppModel()
        await app.connectIfNeeded(to: serverID, using: Client())
        #expect(app.client?.serverID == "local-v2")
        #expect(app.sessions.count == 0)
        #expect(app.connecting == false)
        #expect(app.error == nil)
        // A request binding update after connection should retain that connection.
        await app.connectIfNeeded(to: "local-v2", using: Client(fails: true))
        #expect(app.client?.serverID == "local-v2")
        #expect(app.error == nil)
    }

    @Test func failedAutomaticConnectionCanBeRetried() async {
        let app = AppModel()
        await app.connectIfNeeded(to: "", using: Client(fails: true))
        #expect(app.error == ClientError.noDaemon.localizedDescription)
        #expect(app.connecting == false)
        await app.connectLocal(using: Client())
        #expect(app.client?.serverID == "local-v2")
        #expect(app.error == nil)
    }
}
