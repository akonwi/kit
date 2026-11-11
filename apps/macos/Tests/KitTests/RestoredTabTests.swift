import Foundation
import Testing
@testable import Kit

/// A server that has some sessions and answers for the rest as told: by
/// default it has no such session, as after one was deleted.
private actor RestoringClient: SessionClient {
    nonisolated let serverID: String
    nonisolated let isDemo = false
    private var known: [String: SessionExcerpt]
    /// The error a session the server doesn't have answers with.
    private var failure: any Error
    /// Sessions whose watch waits until released, to observe them loading.
    private var held: Set<String>

    init(serverID: String = "local", sessions: [SessionExcerpt], failure: any Error = ClientError.http(404),
         held: Set<String> = []) {
        self.serverID = serverID
        known = Dictionary(uniqueKeysWithValues: sessions.map { ($0.id, $0) })
        self.failure = failure
        self.held = held
    }

    func release(_ id: String) { held.remove(id) }
    func delete(_ id: String) { known[id] = nil }

    func sessions() async throws -> [SessionExcerpt] { Array(known.values) }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        guard let session = known[id] else { throw failure }
        return session
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        while held.contains(id) { try await Task.sleep(for: .milliseconds(10)) }
        await receive(try await snapshot(id))
        try await Task.sleep(for: .seconds(60))
    }
}

private func session(_ id: String, title: String, parent: String? = nil, parentName: String? = nil) -> SessionExcerpt {
    SessionExcerpt(id: id, title: title, parentSessionID: parent, parentSessionName: parentName, sourceTitle: "", model: "m",
                   thinking: "high", workspace: "/work", date: "",
                   messages: [TranscriptMessage(id: id + "-m", role: "assistant", text: "Transcript of " + title, tools: [])])
}

/// Restored tabs show a session's outline while it opens, close themselves
/// when their server no longer has the session, and keep waiting when the
/// server can't answer.
@MainActor struct RestoredTabTests {
    private func id(_ session: String, server: String = "local") -> SessionIdentity {
        SessionIdentity(server: server, session: session)
    }
    private func temporaryURL() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
    }

    /// A registry restoring one window with `sessions`, and that window's model.
    private func restore(_ sessions: [String], selected: String, titles: [String: String] = [:],
                         url: URL) -> (SessionWindowRegistry, SessionWindowModel) {
        let window = UUID()
        WindowRestorationStore(url: url).update(.init(
            id: window, sessions: sessions.map { id($0) }, selected: id(selected),
            titles: Dictionary(uniqueKeysWithValues: titles.map { (id($0.key), $0.value) })))
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url),
                                             sidebarDefaults: SessionSidebarDefaults(defaults: TestDefaults.make()))
        return (registry, registry.model(for: SessionWindowRequest(windowID: window)))
    }

    private func attach(_ model: SessionWindowModel, to client: RestoringClient) {
        for tab in model.tabs where !tab.isLauncher { model.store(for: tab, client: client) }
    }

    // MARK: What the tab shows

    @Test func aRestoredTabShowsItsOutlineUntilTheSessionArrives() async throws {
        let client = RestoringClient(sessions: [session("a", title: "Release notes")], held: ["a"])
        let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"), titles: [id("a"): "Release notes"])
        // The catalog may describe the session before the server sends it.
        model.servers.local.catalog.recordCreated(session("a", title: "Release notes"))
        let tab = model.tabs[0]
        #expect(SessionTabLoading.label(status: .connected(Self.identity), server: nil, launcher: false,
                                        opening: tab.store?.opening) == "Opening session")
        let store = try #require(model.store(for: tab, client: client))
        defer { model.closeAll() }
        #expect(store.opening == .loading)
        #expect(SessionTabLoading.label(status: .connected(Self.identity), server: nil, launcher: false,
                                        opening: store.opening) == "Opening session")
        #expect(tab.title == "Release notes")
        await client.release("a")
        try await eventually { store.opening == .open }
        #expect(SessionTabLoading.label(status: .connected(Self.identity), server: nil, launcher: false,
                                        opening: store.opening) == nil)
        #expect(store.messages.map(\.text) == ["Transcript of Release notes"])
    }

    @Test func oneLoadingLabelCoversConnectingAndOpening() {
        let retry = Date.now
        let cases: [(ServerStatus, String?, Bool, SessionOpening?, String?)] = [
            (.connecting, nil, false, nil, "Connecting to Kit"),
            (.connecting, "devbox", false, nil, "Connecting to devbox"),
            (.reconnecting(Self.identity, retry: retry), nil, false, nil, "Reconnecting to Kit"),
            (.connected(Self.identity), nil, false, nil, "Opening session"),
            (.connected(Self.identity), "devbox", false, .loading, "Opening session"),
            (.connecting, nil, false, .loading, "Connecting to Kit"),
            // An opened session stays readable while its server is checked again.
            (.reconnecting(Self.identity, retry: retry), nil, false, .open, nil),
            (.reconnecting(Self.identity, retry: retry), "devbox", false, .loading, "Reconnecting to devbox"),
            (.connected(Self.identity), nil, false, .open, nil),
            (.connected(Self.identity), nil, false, .failed("Server request failed (HTTP 500)."), nil),
            (.connected(Self.identity), nil, false, .missing, nil),
            // The server's own state, not an outline, while it can't serve the tab.
            (.unreachable(reason: "Connection refused", retry: retry), nil, false, .loading, nil),
            (.connecting, nil, true, nil, "Connecting to Kit"),
            (.connected(Self.identity), nil, true, nil, nil),
        ]
        for (status, server, launcher, opening, label) in cases {
            #expect(SessionTabLoading.label(status: status, server: server, launcher: launcher, opening: opening) == label,
                    "\(status) \(server ?? "local") launcher: \(launcher) \(String(describing: opening))")
        }
    }

    // MARK: A session that no longer exists

    @Test func aRestoredTabWhoseSessionIsGoneClosesToTheLauncherAndIsForgotten() async throws {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url) }
        let (registry, model) = restore(["gone"], selected: "gone",
                                        titles: ["gone": "Styling Pro plan card as visual default"], url: url)
        #expect(model.tabs.map(\.title) == ["Styling Pro plan card as visual default"])
        let client = RestoringClient(sessions: [])
        attach(model, to: client)
        defer { model.closeAll() }
        try await eventually { model.tabs.allSatisfy(\.isLauncher) }
        #expect(model.tabs.count == 1)
        #expect(model.selectedTab.isLauncher)
        #expect(model.selectedTab.title == "New session")
        #expect(registry.restoration.windows == [])
        #expect(WindowRestorationStore(url: url).windows == [])
    }

    @Test func onlyTheGoneTabClosesAndTheWindowKeepsTheRest() async throws {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url) }
        let (registry, model) = restore(["a", "gone", "b"], selected: "gone",
                                        titles: ["a": "Release notes", "gone": "Deleted plan", "b": "Bug bash"], url: url)
        let client = RestoringClient(sessions: [session("a", title: "Release notes"), session("b", title: "Bug bash")])
        attach(model, to: client)
        defer { model.closeAll() }
        try await eventually { model.sessions == [id("a"), id("b")] }
        #expect(model.tabs.map(\.title) == ["Release notes", "Bug bash"])
        #expect(model.selectedSession == id("b"))
        let saved = WindowRestorationStore(url: url).window(model.id)
        #expect(saved?.sessions == [id("a"), id("b")])
        #expect(saved?.selected == id("b"))
        #expect(saved?.titles == [id("a"): "Release notes", id("b"): "Bug bash"])
        #expect(registry.restoration.sessions == [id("a"), id("b")])
    }

    @Test func eachRestoredWindowClosesItsOwnGoneTabs() async throws {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url) }
        let first = UUID(), second = UUID()
        let saved = WindowRestorationStore(url: url)
        saved.update(.init(id: first, sessions: [id("gone"), id("a")], selected: id("gone")))
        saved.update(.init(id: second, sessions: [id("also-gone")], selected: id("also-gone")))
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url),
                                             sidebarDefaults: SessionSidebarDefaults(defaults: TestDefaults.make()))
        let firstModel = registry.model(for: SessionWindowRequest(windowID: first))
        let secondModel = registry.model(for: SessionWindowRequest(windowID: second))
        let client = RestoringClient(sessions: [session("a", title: "Release notes")])
        attach(firstModel, to: client)
        attach(secondModel, to: client)
        defer { firstModel.closeAll(); secondModel.closeAll() }
        try await eventually { firstModel.sessions == [id("a")] && secondModel.tabs.allSatisfy(\.isLauncher) }
        #expect(firstModel.selectedSession == id("a"))
        #expect(secondModel.selectedTab.isLauncher)
        #expect(WindowRestorationStore(url: url).windows == [.init(id: first, sessions: [id("a")], selected: id("a"),
                                                                    sidebar: SessionSidebarLayout())])
    }

    @Test func aRemoteServersGoneSessionClosesItsTab() async throws {
        let model = SessionWindowModel(id: UUID(), sessions: [id("gone", server: "devbox"), id("a")], selected: id("gone", server: "devbox"))
        let devbox = RestoringClient(serverID: "devbox", sessions: [])
        let local = RestoringClient(sessions: [session("a", title: "Release notes")])
        model.store(for: model.tabs[0], client: devbox)
        model.store(for: model.tabs[1], client: local)
        defer { model.closeAll() }
        try await eventually { model.sessions == [id("a")] }
        #expect(model.selectedSession == id("a"))
    }

    @Test func aChildWhoseSessionIsGoneClosesAndADeletedParentLeavesNoRow() async throws {
        // A child restored beside its sibling; the parent was deleted elsewhere,
        // so the server names no parent for the sibling.
        let model = SessionWindowModel(id: UUID(), sessions: [id("child"), id("sibling")], selected: id("child"))
        let client = RestoringClient(sessions: [session("sibling", title: "Sibling", parent: "parent")])
        attach(model, to: client)
        defer { model.closeAll() }
        try await eventually { model.sessions == [id("sibling")] && model.tabs[0].store?.opening == .open }
        #expect(model.rows.map(\.id) == [SessionWindowModel.treeKey(id("sibling"))])
        #expect(model.rows.map(\.depth) == [0])
        // Opening the parent anyway, as from a link, closes its tab again.
        let parent = model.open(id("parent"))
        #expect(model.selectedSession == id("parent"))
        model.store(for: parent, client: client)
        try await eventually { model.sessions == [id("sibling")] }
        #expect(model.selectedSession == id("sibling"))
    }

    @Test func aShownSessionDeletedElsewhereKeepsItsTabAndTranscript() async throws {
        let client = RestoringClient(sessions: [session("a", title: "Release notes")])
        let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"))
        let store = try #require(model.store(for: model.tabs[0], client: client))
        defer { model.closeAll() }
        try await eventually { store.opening == .open }
        await client.delete("a")
        store.attach()
        try await eventually { store.unavailable }
        #expect(model.sessions == [id("a")])
        #expect(store.opening == .open)
        #expect(store.messages.map(\.text) == ["Transcript of Release notes"])
    }

    // MARK: A server that can't answer

    @Test func aServerThatFailsKeepsTheTabAndOffersRetry() async throws {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url) }
        let (registry, model) = restore(["a"], selected: "a", titles: ["a": "Release notes"], url: url)
        let client = RestoringClient(sessions: [], failure: ClientError.http(500))
        attach(model, to: client)
        defer { model.closeAll() }
        let store = try #require(model.tabs[0].store)
        try await eventually { if case .failed = store.opening { true } else { false } }
        #expect(store.opening == .failed("Server request failed (HTTP 500). · Retrying in 1s"))
        #expect(model.sessions == [id("a")])
        #expect(model.tabs[0].title == "Release notes")
        #expect(registry.restoration.sessions == [id("a")])
    }

    @Test func aDroppedConnectionOrRefusedCredentialKeepsTheTab() async throws {
        for (failure, reason) in [(ClientError.disconnected, "The server connection closed. · Retrying in 1s"),
                                  (ClientError.http(401), "Authentication failed · Reconnect to refresh credentials")] {
            let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"))
            let store = try #require(model.store(for: model.tabs[0], client: RestoringClient(sessions: [], failure: failure)))
            try await eventually { store.opening == .failed(reason) }
            #expect(model.sessions == [id("a")])
            #expect(store.unavailable == false)
            model.closeAll()
        }
    }

    nonisolated static let identity = ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: "i",
                                                     kitVersion: "0.48.0", protocolVersion: kitWireVersion,
                                                     machine: String(repeating: "b", count: 64), home: "/Users/kit")
}
