import AppKit
import Foundation
import Testing
@testable import Kit

/// A session server that records interaction responses.
private actor AuthorizingClient: ComposerClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    let catalog: [SessionExcerpt]
    private(set) var responses: [(session: String, value: String?)] = []
    private var receivers: [String: @Sendable (SessionExcerpt) async -> Void] = [:]
    init(_ catalog: [SessionExcerpt]) { self.catalog = catalog }
    func sessions() async throws -> [SessionExcerpt] { catalog }
    func snapshot(_ id: String) async throws -> SessionExcerpt { catalog.first { $0.id == id }! }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        receivers[id] = receive
        while !Task.isCancelled { try await Task.sleep(for: .seconds(1)) }
    }
    func ready(_ id: String) -> Bool { receivers[id] != nil }
    func emit(_ value: SessionExcerpt) async { await receivers[value.id]?(value) }
    func models() async throws -> [WireModelCapability] { [] }
    func files(_ session: String) async throws -> [String] { [] }
    func configure(_ session: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult { throw ClientError.http(409) }
    func upload(_ session: String, filename: String, data: Data) async throws -> WireAttachmentInfo { throw ClientError.http(503) }
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution { .init(attachments: [], missingAttachmentIds: ids) }
    func respond(_ session: String, input: WireInteractionResponse) async throws { responses.append((session, input.value)) }
}

@MainActor struct AuthorizationStoreTests {
    private let page = URL(string: "https://auth.example.com/authorize?state=s1")!
    private let callback = URL(string: "http://127.0.0.1:53173/mcp/callback?code=c&state=s1")!

    private func session(_ id: String, authorizing: Bool = false) -> SessionExcerpt {
        var session = SessionExcerpt(id: id, title: id, sourceTitle: "Test", model: "model", thinking: "high", workspace: "Test", date: "",
                                     messages: [TranscriptMessage(id: "message", role: "assistant", text: "Connecting", tools: [])])
        if authorizing {
            session.pendingInteractions = [WireInteractionRequest(id: "interaction_authorize", sessionId: id, turnId: "t", toolCallId: "c",
                kind: .value4, title: "Authorize the docs MCP server", url: page.absoluteString,
                redirectUri: "http://127.0.0.1:53173/mcp/callback", createdAt: "2026-10-12T00:00:00Z")]
        }
        return session
    }

    /// A store attached to the first session, which asks for authorization.
    private func authorizing(_ ids: [String] = ["a"]) async throws -> (SessionStore, AuthorizingClient, FakeBrowserEngines) {
        // A new request asks for the Dock's attention through the app.
        _ = NSApplication.shared
        let catalog = ids.map { session($0) }
        let client = AuthorizingClient(catalog)
        let store = SessionStore(fixture: Fixture(sessions: catalog), client: client)
        let engines = FakeBrowserEngines()
        store.ui.workspace.makeBrowserEngine = engines.make
        store.attach()
        try await until { await client.ready("a") }
        await client.emit(session("a", authorizing: true))
        return (store, client, engines)
    }

    private func until(_ condition: () async -> Bool) async throws {
        for _ in 0..<100 where await !condition() { try await Task.sleep(for: .milliseconds(10)) }
        #expect(await condition())
    }

    @Test func anAuthorizationRequestOpensItsPageOnce() async throws {
        let (store, _, engines) = try await authorizing()
        defer { store.detach() }
        let workspace = store.ui.workspace
        let flow = try #require(store.dockedInteraction)

        store.openAuthorizationOnce(flow)
        let pane = try #require(workspace.selected)
        #expect(workspace.browser(pane)?.url == page)
        #expect(workspace.root.tiles.count == 2)
        #expect(engines.made.count == 1)

        // Once closed, the page opens again only from the dock.
        workspace.close(pane)
        store.openAuthorizationOnce(flow)
        #expect(workspace.root.tiles.count == 1)
        #expect(engines.made.count == 1)
    }

    @Test func thePagesRedirectAnswersTheRequestAndClosesThePage() async throws {
        let (store, client, engines) = try await authorizing()
        defer { store.detach() }
        let workspace = store.ui.workspace
        store.openAuthorizationOnce(try #require(store.dockedInteraction))

        #expect(!engines.made[0].navigate(callback))
        try await until { await client.responses.count == 1 }
        #expect(await client.responses.map(\.session) == ["a"])
        #expect(await client.responses.map(\.value) == [callback.absoluteString])
        try await until { workspace.root.tiles.count == 1 }
        #expect(workspace.panes == [.conversation])
    }

    @Test func aRedirectAnswersItsOwnSessionWhileAnotherIsShown() async throws {
        let (store, client, engines) = try await authorizing(["a", "b"])
        defer { store.detach() }
        store.openAuthorizationOnce(try #require(store.dockedInteraction))
        store.select("b")
        #expect(store.selectedID == "b")

        #expect(!engines.made[0].navigate(callback))
        try await until { await client.responses.count == 1 }
        #expect(await client.responses.map(\.session) == ["a"])
        #expect(await client.responses.map(\.value) == [callback.absoluteString])
    }

    @Test func aRequestResolvedElsewhereLetsThePageLoadItsRedirect() async throws {
        let (store, client, engines) = try await authorizing()
        defer { store.detach() }
        let workspace = store.ui.workspace
        store.openAuthorizationOnce(try #require(store.dockedInteraction))
        let pane = try #require(workspace.selected)
        await client.emit(session("a"))
        #expect(store.interactions.isEmpty)

        #expect(engines.made[0].navigate(callback))
        #expect(workspace.browser(pane)?.url == callback)
        #expect(await client.responses.isEmpty)
    }
}
