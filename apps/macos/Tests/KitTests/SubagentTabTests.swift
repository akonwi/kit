import Foundation
import Testing
@testable import Kit

/// A session whose roster the test changes, delivered as the event stream
/// would. Each watch starts with the latest excerpt, as a reattached stream
/// starts with a snapshot; `drop` ends the stream as a lost connection does.
private actor RosterClient: SessionClient {
    nonisolated let serverID = "subagent-tabs-test"
    nonisolated let isDemo = false
    private var latest: SessionExcerpt
    private var stream: AsyncThrowingStream<SessionExcerpt, any Error>.Continuation?
    private(set) var watches = 0
    init(_ first: SessionExcerpt) { latest = first }
    func sessions() async throws -> [SessionExcerpt] { [latest] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { latest }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        let (updates, continuation) = AsyncThrowingStream<SessionExcerpt, any Error>.makeStream()
        stream = continuation
        watches += 1
        await receive(latest)
        for try await update in updates { await receive(update) }
    }
    func deliver(_ excerpt: SessionExcerpt) {
        latest = excerpt
        stream?.yield(excerpt)
    }
    /// Loses the connection; the session is `excerpt` when it comes back.
    func drop(returning excerpt: SessionExcerpt) {
        latest = excerpt
        stream?.finish(throwing: ClientError.disconnected)
        stream = nil
    }
}

@MainActor struct SubagentTabTests {
    /// An agent's state: its conversation, if started, and its status.
    private typealias Agent = (name: String, conversation: String?, status: String)

    /// A session whose main transcript started `started`, in that order.
    private func excerpt(_ title: String, _ agents: [Agent], started: [String] = []) -> SessionExcerpt {
        let starts = started.isEmpty ? [] : [TranscriptMessage(id: "tools", role: "tools", text: "", tools: started.map {
            ToolActivity(id: "call-" + $0, name: "subagent", summary: "", output: "",
                         arguments: #"{"action":"start","agent":"\#($0)"}"#, failed: false)
        })]
        var value = SessionExcerpt(id: "s", title: title, sourceTitle: "test", model: "m", thinking: "off", workspace: "tmp", date: "", messages: starts)
        value.subagents = SubagentRoster(items: agents.map {
            SubagentRoster.Item(name: $0.name, description: "", model: "m", status: $0.status,
                                conversationID: $0.conversation, updatedAt: nil, generation: $0.conversation == nil ? nil : 1)
        })
        return value
    }

    private func attached(_ first: SessionExcerpt, arrangement: TileArrangement? = nil) async throws -> (SessionStore, RosterClient) {
        let client = RosterClient(first)
        let store = SessionStore(fixture: Fixture(sessions: [first]), client: client)
        if let arrangement { store.ui.workspace.restore(arrangement) }
        store.attach()
        // The session's opening snapshot has arrived and its stream is open.
        try await eventually { store.connectionState == .connected }
        return (store, client)
    }

    private func deliver(_ excerpt: SessionExcerpt, to client: RosterClient, _ store: SessionStore) async throws {
        await client.deliver(excerpt)
        try await eventually { store.selected?.title == excerpt.title }
    }

    private func panes(_ store: SessionStore, tileOf pane: WorkspacePane = .conversation) -> [WorkspacePane] {
        let workspace = store.ui.workspace
        return workspace.tile(containing: pane).flatMap { workspace.tiles[$0]?.panes } ?? []
    }

    @Test func agentsStartedWhileAttachedOpenBehindTheAgentTabInStartOrder() async throws {
        let (store, client) = try await attached(excerpt("Attached", [
            ("reviewer", "c1", "idle"), ("helper", nil, "inactive"), ("builder", nil, "inactive"),
        ]))
        let workspace = store.ui.workspace
        // The reader is in the Diff, in a tile of its own.
        workspace.open(.review, beside: .conversation)
        let diffTile = try #require(workspace.tile(containing: .review))
        let agentTile = try #require(workspace.tile(containing: .conversation))
        #expect(workspace.root.tiles == [agentTile, diffTile])
        #expect(workspace.focusedTile == diffTile)

        try await deliver(excerpt("Helper started", [
            ("reviewer", "c1", "idle"), ("helper", "c2", "running"), ("builder", nil, "inactive"),
        ]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("helper")])
        #expect(workspace.tiles[agentTile]?.selected == .conversation)
        #expect(workspace.tiles[diffTile]?.panes == [.review])
        #expect(workspace.tiles[diffTile]?.selected == .review)
        #expect(workspace.focusedTile == diffTile)

        try await deliver(excerpt("Builder started", [
            ("reviewer", "c1", "idle"), ("helper", "c2", "running"), ("builder", "c3", "running"),
        ]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("helper"), .agent("builder")])

        // Status changes don't reorder the tabs.
        try await deliver(excerpt("Statuses changed", [
            ("reviewer", "c1", "running"), ("helper", "c2", "idle"), ("builder", "c3", "failed"),
        ]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("helper"), .agent("builder")])
        #expect(workspace.tiles[agentTile]?.selected == .conversation)
        #expect(workspace.focusedTile == diffTile)
        #expect(workspace.selected == .review)
        store.detach()
    }

    @Test func aZoomedTileStaysZoomedWhenAnAgentStarts() async throws {
        let (store, client) = try await attached(excerpt("Attached", [("helper", nil, "inactive")]))
        let workspace = store.ui.workspace
        workspace.open(.scratchpad, beside: .conversation)
        workspace.toggleZoom()
        let zoomed = try #require(workspace.tile(containing: .scratchpad))
        #expect(workspace.visibleTiles == [zoomed])

        try await deliver(excerpt("Helper started", [("helper", "c1", "running")]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("helper")])
        #expect(workspace.visibleTiles == [zoomed])
        #expect(workspace.selected == .scratchpad)
        store.detach()
    }

    @Test func aClosedTabStaysClosedUntilItsAgentStartsAgain() async throws {
        let (store, client) = try await attached(excerpt("Attached", [("helper", nil, "inactive")]))
        try await deliver(excerpt("Started", [("helper", "c1", "running")]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("helper")])

        store.ui.workspace.close(.agent("helper"))
        try await deliver(excerpt("Finished", [("helper", "c1", "idle")]), to: client, store)
        try await deliver(excerpt("Working again", [("helper", "c1", "running")]), to: client, store)
        #expect(panes(store) == [.conversation])

        try await deliver(excerpt("Dismissed", [("helper", nil, "inactive")]), to: client, store)
        #expect(panes(store) == [.conversation])
        try await deliver(excerpt("Started again", [("helper", "c2", "running")]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("helper")])
        #expect(store.ui.workspace.selected == .conversation)
        store.detach()
    }

    @Test func anEndedConversationClosesItsTabAndAReplacementKeepsIt() async throws {
        let (store, client) = try await attached(excerpt("Attached", [
            ("reviewer", "c1", "idle"), ("helper", "c2", "idle"),
        ]))
        let workspace = store.ui.workspace
        // Agents started before the app attached open only when asked.
        #expect(panes(store) == [.conversation])
        workspace.open(.agent("reviewer"))
        workspace.open(.agent("helper"))
        #expect(panes(store) == [.conversation, .agent("reviewer"), .agent("helper")])
        #expect(workspace.selected == .agent("helper"))

        // Dismissed elsewhere: the tab the user opened closes.
        try await deliver(excerpt("Helper dismissed", [("reviewer", "c1", "idle"), ("helper", nil, "inactive")]),
                          to: client, store)
        #expect(panes(store) == [.conversation, .agent("reviewer")])
        #expect(workspace.selected == .agent("reviewer"))

        // Dismissed and started again between updates: one tab, kept.
        try await deliver(excerpt("Reviewer replaced", [("reviewer", "c3", "running"), ("helper", nil, "inactive")]),
                          to: client, store)
        #expect(panes(store) == [.conversation, .agent("reviewer")])
        #expect(workspace.selected == .agent("reviewer"))
        store.detach()
    }

    @Test func reconnectingOpensAgentsStartedMeanwhileOnce() async throws {
        let (store, client) = try await attached(excerpt("Attached", [
            ("reviewer", nil, "inactive"), ("helper", nil, "inactive"), ("builder", nil, "inactive"),
        ]))
        try await deliver(excerpt("Started", [
            ("reviewer", "c1", "running"), ("helper", nil, "inactive"), ("builder", nil, "inactive"),
        ], started: ["reviewer"]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("reviewer")])
        store.ui.workspace.close(.agent("reviewer"))
        #expect(panes(store) == [.conversation])

        // While the connection is down, builder starts, then helper.
        await client.drop(returning: excerpt("Reconnected", [
            ("reviewer", "c1", "idle"), ("helper", "c2", "running"), ("builder", "c3", "running"),
        ], started: ["reviewer", "builder", "helper"]))
        try await eventually { store.selected?.title == "Reconnected" }
        #expect(await client.watches == 2)
        #expect(panes(store) == [.conversation, .agent("builder"), .agent("helper")])
        #expect(store.ui.workspace.selected == .conversation)

        // While it's down again, helper is dismissed.
        await client.drop(returning: excerpt("Reconnected again", [
            ("reviewer", "c1", "idle"), ("helper", nil, "inactive"), ("builder", "c3", "idle"),
        ], started: ["reviewer", "builder", "helper"]))
        try await eventually { store.selected?.title == "Reconnected again" }
        #expect(panes(store) == [.conversation, .agent("builder")])
        store.detach()
    }

    @Test func agentsStartedTogetherOpenInStartOrderThenByName() async throws {
        let (store, client) = try await attached(excerpt("Attached", [
            ("alpha", nil, "inactive"), ("beta", nil, "inactive"), ("gamma", nil, "inactive"), ("delta", nil, "inactive"),
        ]))
        // gamma and alpha were started by the main agent; delta and beta by
        // other means, so the transcript doesn't order them.
        try await deliver(excerpt("All started", [
            ("alpha", "c1", "running"), ("beta", "c2", "running"), ("gamma", "c3", "running"), ("delta", "c4", "running"),
        ], started: ["gamma", "alpha"]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("gamma"), .agent("alpha"), .agent("beta"), .agent("delta")])
        #expect(store.ui.workspace.selected == .conversation)
        store.detach()
    }

    @Test func anAgentStartedAgainIsPlacedByItsNewStart() async throws {
        let (store, client) = try await attached(excerpt("Attached", [
            ("reviewer", "c1", "idle"), ("helper", nil, "inactive"),
        ], started: ["reviewer"]))
        try await deliver(excerpt("Reviewer dismissed", [("reviewer", nil, "inactive"), ("helper", nil, "inactive")],
                                  started: ["reviewer"]), to: client, store)
        // Helper starts, then reviewer starts again, in one update.
        try await deliver(excerpt("Both started", [("reviewer", "c3", "running"), ("helper", "c2", "running")],
                                  started: ["reviewer", "helper", "reviewer"]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("helper"), .agent("reviewer")])
        #expect(store.ui.workspace.selected == .conversation)
        store.detach()
    }

    @Test func aRestoredTabWhoseConversationEndedMeanwhileCloses() async throws {
        // Last time, reviewer's and helper's tabs were open beside Agent's,
        // with helper's selected; helper was dismissed while the app was quit.
        let saved = TileArrangement(root: .split(.init(axis: .horizontal, ratio: 0.5,
            first: .tile(.init(panes: ["conversation"], selected: "conversation")),
            second: .tile(.init(panes: ["agent:reviewer", "agent:helper"], selected: "agent:helper")))),
            focused: 1, zoomed: false)
        let (store, _) = try await attached(excerpt("Attached", [
            ("reviewer", "c1", "idle"), ("helper", nil, "inactive"),
        ]), arrangement: saved)
        let workspace = store.ui.workspace
        #expect(panes(store) == [.conversation])
        #expect(panes(store, tileOf: .agent("reviewer")) == [.agent("reviewer")])
        #expect(workspace.selected == .agent("reviewer"))
        store.detach()
    }

    @Test func aRestoredLayoutKeepsItsTabsAndLaterAgentsJoinTheAgentTile() async throws {
        // Last time, reviewer's tab was in a tile beside Agent's and helper's
        // tab was closed.
        let saved = TileArrangement(root: .split(.init(axis: .horizontal, ratio: 0.5,
            first: .tile(.init(panes: ["conversation"], selected: "conversation")),
            second: .tile(.init(panes: ["agent:reviewer"], selected: "agent:reviewer")))),
            focused: 1, zoomed: false)
        let (store, client) = try await attached(excerpt("Attached", [
            ("reviewer", "c1", "running"), ("helper", "c2", "idle"), ("builder", nil, "inactive"),
        ]), arrangement: saved)
        let workspace = store.ui.workspace
        #expect(workspace.arrangement == saved)

        try await deliver(excerpt("Builder started", [
            ("reviewer", "c1", "idle"), ("helper", "c2", "idle"), ("builder", "c3", "running"),
        ]), to: client, store)
        #expect(panes(store) == [.conversation, .agent("builder")])
        #expect(panes(store, tileOf: .agent("reviewer")) == [.agent("reviewer")])
        #expect(workspace.selected == .agent("reviewer"))
        store.detach()
    }
}
