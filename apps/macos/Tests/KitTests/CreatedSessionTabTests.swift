import Foundation
import Testing
@testable import Kit

/// Sessions whose updates the test delivers as their event streams would.
/// Each watch starts with the session's latest excerpt, as a stream starts
/// with a snapshot.
private actor CreatingClient: SessionClient {
    nonisolated let serverID = "local"
    nonisolated let isDemo = false
    private var latest: [String: SessionExcerpt]
    private var streams: [String: AsyncThrowingStream<SessionExcerpt, any Error>.Continuation] = [:]

    init(_ sessions: [SessionExcerpt]) { latest = Dictionary(uniqueKeysWithValues: sessions.map { ($0.id, $0) }) }

    func sessions() async throws -> [SessionExcerpt] { Array(latest.values) }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        guard let session = latest[id] else { throw ClientError.http(404) }
        return session
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        let (updates, continuation) = AsyncThrowingStream<SessionExcerpt, any Error>.makeStream()
        streams[id] = continuation
        await receive(try await snapshot(id))
        for try await update in updates { await receive(update) }
    }
    func add(_ session: SessionExcerpt) { latest[session.id] = session }
    func deliver(_ session: SessionExcerpt) {
        latest[session.id] = session
        streams[session.id]?.yield(session)
    }
}

/// A session ID as the server makes them.
private func sid(_ n: Int) -> String { "session_" + String(format: "%032x", n) }

private func identity(_ session: String) -> SessionIdentity { SessionIdentity(server: "local", session: session) }

/// A create_session call that made `created`, or failed when it's nil.
private func create(_ call: String, _ created: String?, name: String = "Docs", live: Bool? = true) -> ToolActivity {
    let output = created.map { #"{"session":{"id":"\#($0)","cwd":"/work","name":"\#(name)"}}"# } ?? #"{"error":"cwd is missing"}"#
    return ToolActivity(id: call, name: "create_session", summary: "", output: output,
                        arguments: #"{"cwd":"/work","name":"\#(name)"}"#, failed: created == nil, status: "Completed",
                        completedLive: live)
}

private func session(_ id: String, title: String, parent: String? = nil, tools: [ToolActivity] = []) -> SessionExcerpt {
    SessionExcerpt(id: id, title: title, parentSessionID: parent, sourceTitle: "", model: "m", thinking: "high", workspace: "/work",
                   date: "", messages: tools.isEmpty ? [] : [TranscriptMessage(id: id + "-tools", role: "tools", text: "", tools: tools)])
}

/// A session created with the model's create_session tool opens as a tab
/// behind its creator, in the creator's window, once.
@MainActor struct CreatedSessionTabTests {
    private let a = sid(0xa), b = sid(0xb), c = sid(0xc), x = sid(0x1), y = sid(0x2), z = sid(0x3)

    private func attach(_ model: SessionWindowModel, to client: CreatingClient) async throws {
        for tab in model.tabs where !tab.isLauncher { model.store(for: tab, client: client) }
        try await eventually { model.tabs.allSatisfy { $0.store?.connectionState == .connected } }
    }

    private func sessions(_ model: SessionWindowModel) -> [String] { model.tabs.map(\.identity.session) }

    private func titles(_ model: SessionWindowModel) -> [String] { model.tabs.map(\.title) }

    @Test func aSessionCreatedInABackgroundTabOpensBehindItsCreatorAndTheSelectionStays() async throws {
        let client = CreatingClient([session(a, title: "Planner"), session(b, title: "Notes"), session(c, title: "Shown")])
        let model = SessionWindowModel(id: UUID(), sessions: [identity(a), identity(b), identity(c)], selected: identity(c))
        defer { model.closeAll() }
        try await attach(model, to: client)
        let shown = model.selectedTab
        let workspace = try #require(shown.store?.ui.workspace)
        workspace.open(.scratchpad)
        let focusedTile = workspace.focusedTile
        var changes = 0
        model.onChange = { _ in changes += 1 }

        // The planner, in a tab the user isn't looking at, creates a session.
        await client.add(session(x, title: "Docs", parent: a))
        await client.deliver(session(a, title: "Planner", tools: [create("call-1", x, name: "Docs")]))
        try await eventually { model.tabs.count == 4 }

        #expect(sessions(model) == [a, x, b, c])
        #expect(titles(model) == ["Planner", "Docs", "Notes", "Shown"])
        #expect(model.selectedTab === shown)
        #expect(workspace.focusedTile == focusedTile)
        #expect(workspace.selected == .scratchpad)
        #expect(changes == 1)
        #expect(model.sessions == [identity(a), identity(x), identity(b), identity(c)])
        // The sidebar shows it under its creator.
        #expect(model.rows.map(\.id) == [a, x, b, c])
        #expect(model.rows.map(\.depth) == [0, 1, 0, 0])
        // It attaches like any background tab.
        try await attach(model, to: client)
        #expect(model.tabs[1].store?.selected?.parentSessionID == a)
    }

    @Test func sessionsCreatedTogetherOpenInCallOrderAfterTheCreatorsFamily() async throws {
        let client = CreatingClient([session(a, title: "Planner"), session(y, title: "Earlier", parent: a), session(b, title: "Notes")])
        let model = SessionWindowModel(id: UUID(), sessions: [identity(a), identity(y), identity(b)], selected: identity(b))
        defer { model.closeAll() }
        try await attach(model, to: client)

        await client.deliver(session(a, title: "Planner", tools: [create("call-1", x, name: "API"), create("call-2", z, name: "Site")]))
        try await eventually { model.tabs.count == 5 }

        #expect(sessions(model) == [a, y, x, z, b])
        #expect(titles(model) == ["Planner", "Earlier", "API", "Site", "Notes"])
        #expect(model.selectedTab.identity == identity(b))
    }

    @Test func theCreatorsFamilyReachesThroughAClosedParent() async throws {
        // c came from b, whose tab is closed, and b from the planner.
        let client = CreatingClient([session(a, title: "Planner"), session(c, title: "Grandchild", parent: b), session(z, title: "Notes")])
        let model = SessionWindowModel(id: UUID(), sessions: [identity(a), identity(c), identity(z)], selected: identity(z))
        defer { model.closeAll() }
        model.servers.local.catalog.recordCreated(session(b, title: "Child", parent: a))
        try await attach(model, to: client)

        await client.deliver(session(a, title: "Planner", tools: [create("call-1", x, name: "Docs")]))
        try await eventually { model.tabs.count == 4 }

        #expect(sessions(model) == [a, c, x, z])
    }

    @Test func aCollapsedCreatorStaysCollapsed() async throws {
        let client = CreatingClient([session(a, title: "Planner"), session(y, title: "Earlier", parent: a), session(b, title: "Notes")])
        let model = SessionWindowModel(id: UUID(), sessions: [identity(a), identity(y), identity(b)], selected: identity(b),
                                       collapsed: [a])
        defer { model.closeAll() }
        try await attach(model, to: client)

        await client.deliver(session(a, title: "Planner", tools: [create("call-1", x, name: "Docs")]))
        try await eventually { model.tabs.count == 4 }

        #expect(sessions(model) == [a, y, x, b])
        #expect(model.collapsed == [a])
        #expect(model.rows.map(\.id) == [a, b])
        #expect(model.rows[0].hiddenTabs.map(\.identity.session) == [y, x])
        #expect(model.selectedTab.identity == identity(b))
    }

    @Test func onlySuccessfulLiveCallsOpenAndOnlyOnce() async throws {
        // Calls in the session's opening snapshot came before it attached.
        let client = CreatingClient([session(a, title: "Planner", tools: [create("old", y, name: "Old", live: nil)]),
                                     session(b, title: "Shown")])
        let model = SessionWindowModel(id: UUID(), sessions: [identity(a), identity(b)], selected: identity(b))
        defer { model.closeAll() }
        try await attach(model, to: client)
        #expect(sessions(model) == [a, b])

        let calls = [create("old", y, name: "Old", live: nil), create("failed", nil), create("call-1", x, name: "Docs")]
        await client.deliver(session(a, title: "Planner", tools: calls))
        try await eventually { model.tabs.count == 3 }
        #expect(sessions(model) == [a, x, b])

        // A tab the user closes stays closed as the session goes on.
        model.close(model.tabs[1], from: .top)
        #expect(sessions(model) == [a, b])
        await client.deliver(session(a, title: "Planner, later", tools: calls))
        try await eventually { model.tabs[0].title == "Planner, later" }
        #expect(sessions(model) == [a, b])
        #expect(model.selectedTab.identity == identity(b))
    }

    @Test func aSessionAlreadyOpenKeepsItsTab() async throws {
        let client = CreatingClient([session(a, title: "Planner"), session(b, title: "Shown"), session(x, title: "Docs", parent: a)])
        let model = SessionWindowModel(id: UUID(), sessions: [identity(a), identity(b), identity(x)], selected: identity(b))
        defer { model.closeAll() }
        try await attach(model, to: client)

        await client.deliver(session(a, title: "Planner, later", tools: [create("call-1", x)]))
        try await eventually { model.tabs[0].title == "Planner, later" }
        #expect(sessions(model) == [a, b, x])
        #expect(model.selectedTab.identity == identity(b))
    }

    // MARK: Windows

    private func registry(_ windows: [(id: UUID, sessions: [String])], restored: Int? = nil) -> (SessionWindowRegistry, [SessionWindowModel]) {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        let store = WindowRestorationStore(url: url)
        for window in windows {
            store.update(.init(id: window.id, sessions: window.sessions.map(identity), selected: identity(window.sessions[0])))
        }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url),
                                             sidebarDefaults: SessionSidebarDefaults(defaults: TestDefaults.make()))
        return (registry, windows.prefix(restored ?? windows.count).map { registry.model(for: SessionWindowRequest(windowID: $0.id)) })
    }

    @Test func aCreatedSessionOpensInItsCreatorsWindow() async throws {
        let client = CreatingClient([session(a, title: "Front"), session(b, title: "Planner"), session(c, title: "Shown")])
        let (registry, models) = registry([(UUID(), [a]), (UUID(), [c, b])])
        let (front, back) = (models[0], models[1])
        defer { for model in models { model.closeAll() } }
        _ = registry
        try await attach(front, to: client)
        try await attach(back, to: client)

        await client.deliver(session(b, title: "Planner", tools: [create("call-1", x)]))
        try await eventually { back.tabs.count == 3 }

        #expect(sessions(back) == [c, b, x])
        #expect(back.selectedTab.identity == identity(c))
        #expect(sessions(front) == [a])
    }

    @Test func aSessionOpenInAnotherWindowStaysThere() async throws {
        let client = CreatingClient([session(a, title: "Planner"), session(x, title: "Docs", parent: a)])
        let (registry, models) = registry([(UUID(), [a]), (UUID(), [x])])
        defer { for model in models { model.closeAll() } }
        _ = registry
        try await attach(models[0], to: client)
        try await attach(models[1], to: client)

        await client.deliver(session(a, title: "Planner, later", tools: [create("call-1", x)]))
        try await eventually { models[0].tabs[0].title == "Planner, later" }

        #expect(sessions(models[0]) == [a])
        #expect(sessions(models[1]) == [x])
    }

    @Test func aSessionSavedInAWindowNotYetRestoredStaysThere() async throws {
        let client = CreatingClient([session(a, title: "Planner"), session(x, title: "Docs", parent: a)])
        let (registry, models) = registry([(UUID(), [a]), (UUID(), [x])], restored: 1)
        defer { for model in models { model.closeAll() } }
        _ = registry
        try await attach(models[0], to: client)

        await client.deliver(session(a, title: "Planner, later", tools: [create("call-1", x)]))
        try await eventually { models[0].tabs[0].title == "Planner, later" }

        #expect(sessions(models[0]) == [a])
    }
}

struct CreatedSessionPendingTests {
    private func rows(_ tools: [ToolActivity]) -> [TranscriptMessage] {
        [TranscriptMessage(id: "tools", role: "tools", text: "", tools: tools)]
    }

    @Test func returnsEachLiveCreatedSessionOnceInOrder() {
        var handled: Set<String> = []
        let messages = rows([create("a", sid(1), name: "API"), create("b", sid(2), name: "Site")])
        #expect(CreatedSessionTabs.pending(in: messages, handled: &handled)
            == [.init(id: sid(1), name: "API"), .init(id: sid(2), name: "Site")])
        #expect(CreatedSessionTabs.pending(in: messages, handled: &handled) == [])
    }

    @Test func forgetsOnlyCallsNoLongerShownWhenTheBoundIsExceeded() {
        var handled = Set((0..<CreatedSessionTabs.handledLimit).map { "old\($0)" })
        let messages = rows([create("shown", sid(1))])
        #expect(CreatedSessionTabs.pending(in: messages, handled: &handled) == [.init(id: sid(1), name: "Docs")])
        #expect(handled == ["shown"])
        #expect(CreatedSessionTabs.pending(in: messages, handled: &handled) == [])
    }
}
