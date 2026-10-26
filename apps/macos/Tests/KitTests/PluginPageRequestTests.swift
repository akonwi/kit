import Foundation
import Testing
@testable import Kit

/// Delivers a fixed sequence of session excerpts, as the event stream would.
private actor PageRequestClient: SessionClient {
    nonisolated let serverID = "page-request-test"
    nonisolated let isDemo = false
    let deliveries: [SessionExcerpt]
    init(deliveries: [SessionExcerpt]) { self.deliveries = deliveries }
    func sessions() async throws -> [SessionExcerpt] { [deliveries[0]] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { deliveries[0] }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        for delivery in deliveries { await receive(delivery) }
        try await Task.sleep(for: .seconds(60))
    }
}

@MainActor struct PluginPageRequestTests {
    private func excerpt(_ title: String, pages: [RequestedPage]? = nil) -> SessionExcerpt {
        var value = SessionExcerpt(id: "s", title: title, sourceTitle: "test", model: "m", thinking: "off", workspace: "tmp", date: "", messages: [])
        value.requestedPages = pages
        return value
    }

    @Test func eachLivePageRequestOpensOnceAsATabBesideTheConversation() async throws {
        let docs = RequestedPage(id: "stream:4", url: URL(string: "https://example.com/docs")!)
        let local = RequestedPage(id: "stream:7", url: URL(string: "http://localhost:3000/")!)
        let client = PageRequestClient(deliveries: [
            excerpt("Attached"),
            excerpt("First request", pages: [docs]),
            excerpt("Second request", pages: [docs, local]),
            excerpt("Redelivered", pages: [docs, local]),
        ])
        let store = SessionStore(fixture: Fixture(sessions: [excerpt("Attached")]), client: client)
        let engines = FakeBrowserEngines()
        store.ui.workspace.makeBrowserEngine = engines.make
        store.attach()
        try await eventually { store.selected?.title == "Redelivered" }

        let workspace = store.ui.workspace
        let home = try #require(workspace.tile(containing: .conversation))
        #expect(workspace.root.tiles.count == 2)
        #expect(workspace.tiles[home]?.panes == [.conversation])
        let browser = try #require(workspace.root.tiles.first { $0 != home })
        let panes = try #require(workspace.tiles[browser]?.panes)
        #expect(panes.count == 2)
        #expect(workspace.tiles[browser]?.selected == panes.last)
        #expect(engines.made.map(\.loads) == [[docs.url], [local.url]])
        store.detach()
    }
}
