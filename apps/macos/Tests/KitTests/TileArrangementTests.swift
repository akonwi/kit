import Foundation
import Testing
@testable import Kit

/// Saving and restoring session tile layouts across relaunch.
@MainActor
struct TileArrangementTests {
    private func id(_ session: String) -> SessionIdentity { SessionIdentity(server: "demo", session: session) }

    /// Agent | (Scratchpad, Diff over reviewer), with Diff focused.
    private func arranged() -> WorkspaceState {
        let workspace = WorkspaceState()
        workspace.open(.scratchpad)
        workspace.split(.scratchpad, .right)
        workspace.open(.review)
        workspace.open(.agent("reviewer"))
        workspace.split(.agent("reviewer"), .bottom)
        workspace.select(.review)
        return workspace
    }

    private let expected = TileArrangement(
        root: .split(.init(axis: .horizontal, ratio: 0.5,
            first: .tile(.init(panes: ["conversation"], selected: "conversation")),
            second: .split(.init(axis: .vertical, ratio: 0.5,
                first: .tile(.init(panes: ["scratchpad", "review"], selected: "review")),
                second: .tile(.init(panes: ["agent:reviewer"], selected: "agent:reviewer")))))),
        focused: 1, zoomed: false)

    @Test func layoutSurvivesEncodingAndRestore() throws {
        let saved = arranged().arrangement
        #expect(saved == expected)
        let data = try JSONEncoder().encode(saved)
        let restored = WorkspaceState()
        restored.restore(try JSONDecoder().decode(TileArrangement.self, from: data))
        #expect(restored.arrangement == expected)
        #expect(restored.panes == [.conversation, .scratchpad, .review, .agent("reviewer")])
        #expect(restored.selected == .review)
    }

    @Test func tilesEncodeAsTabsAndSplitsAsTheirChildren() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        let json = String(decoding: try encoder.encode(TileArrangement(
            root: .split(.init(axis: .horizontal, ratio: 0.5,
                first: .tile(.init(panes: ["conversation"], selected: "conversation")),
                second: .tile(.init(panes: ["review"], selected: "review")))),
            focused: 1, zoomed: false)), as: UTF8.self)
        #expect(json == #"{"focused":1,"root":{"axis":"horizontal","first":{"panes":["conversation"],"selected":"conversation"},"#
            + #""ratio":0.5,"second":{"panes":["review"],"selected":"review"}},"zoomed":false}"#)
    }

    @Test func restoreRepairsLayoutsItCanNotShowAsSaved() {
        let workspace = WorkspaceState()
        workspace.restore(TileArrangement(
            root: .split(.init(axis: .vertical, ratio: 4,
                first: .split(.init(axis: .horizontal, ratio: 0.3,
                    first: .tile(.init(panes: ["terminal:1", ""], selected: "terminal:1")),
                    second: .tile(.init(panes: ["review", "file:main.swift", "review"], selected: "gone")))),
                second: .tile(.init(panes: ["scratchpad", "agent:"], selected: "scratchpad")))),
            focused: 9, zoomed: true))
        // The tile of unknown panes collapses, the Agent tab returns to the
        // first tile, the ratio is clamped, and focus lands on the last tile.
        #expect(workspace.arrangement == TileArrangement(
            root: .split(.init(axis: .vertical, ratio: 0.9,
                first: .tile(.init(panes: ["conversation", "review", "file:main.swift"], selected: "review")),
                second: .tile(.init(panes: ["scratchpad"], selected: "scratchpad")))),
            focused: 1, zoomed: true))
    }

    @Test func aLayoutWithNoKnownPanesLeavesTheWorkspaceAlone() {
        let workspace = arranged()
        workspace.restore(TileArrangement(root: .tile(.init(panes: ["terminal:1"], selected: "terminal:1")),
                                          focused: 0, zoomed: false))
        #expect(workspace.arrangement == expected)
    }

    @Test func zoomNeedsMoreThanOneTile() {
        let workspace = WorkspaceState()
        workspace.restore(TileArrangement(root: .tile(.init(panes: ["conversation", "review"], selected: "review")),
                                          focused: 0, zoomed: true))
        #expect(workspace.zoomedTile == nil)
        #expect(workspace.selected == .review)
    }

    @Test func storeSavesLayoutsOfOpenTabsOnly() throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let window = UUID()
        let store = WindowRestorationStore(url: url)
        let single = TileArrangement(root: .tile(.init(panes: ["conversation"], selected: "conversation")),
                                     focused: 0, zoomed: false)
        store.update(.init(id: window, sessions: [id("a"), id("b")], selected: id("a"),
                           layouts: [id("a"): expected, id("b"): single, id("closed"): expected]))
        let reloaded = WindowRestorationStore(url: url)
        #expect(reloaded.windows == [.init(id: window, sessions: [id("a"), id("b")], selected: id("a"),
                                           layouts: [id("a"): expected])])
        // Earlier builds read version 2 and ignore the layouts field.
        let manifest = try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any]
        #expect(manifest?["version"] as? Int == 2)
    }

    @Test func windowsSavedBeforeLayoutsStillRestore() throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let window = UUID()
        try Data(#"{"version":2,"windows":[{"id":"\#(window.uuidString)","sessions":[{"server":"demo","session":"a"}]}]}"#.utf8)
            .write(to: url)
        #expect(WindowRestorationStore(url: url).windows == [.init(id: window, sessions: [id("a")], selected: id("a"))])
    }

    @Test func tabsApplySavedLayoutsWhenTheirSessionOpens() throws {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a"), id("b")], selected: id("a"),
                                       layouts: [id("a"): expected])
        #expect(model.layouts == [id("a"): expected])
        let client = FixtureClient(fixture: Fixture(sessions: []))
        let store = try #require(model.store(for: model.tabs[0], client: client))
        defer { store.detach() }
        #expect(store.ui.workspace.arrangement == expected)
        store.ui.workspace.joinTiles(selecting: .review)
        #expect(model.layouts == [id("a"): TileArrangement(
            root: .tile(.init(panes: ["conversation", "scratchpad", "review", "agent:reviewer"], selected: "review")),
            focused: 0, zoomed: false)])
        // A tab pointed at another session starts that session's layout fresh.
        model.show(id("c"), in: model.tabs[1])
        #expect(model.layouts.keys.sorted { $0.session < $1.session } == [id("a")])
    }

    @Test func layoutChangesAreCapturedAfterADelay() async throws {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"))
        var captures = 0
        model.onChange = { _ in captures += 1 }
        let store = try #require(model.store(for: model.tabs[0], client: FixtureClient(fixture: Fixture(sessions: []))))
        defer { store.detach() }
        store.ui.workspace.open(.scratchpad)
        store.ui.workspace.split(.scratchpad, .right)
        store.ui.workspace.setRatio(0.6, for: try #require(store.ui.workspace.root.split))
        #expect(captures == 0)
        try await wait { captures > 0 }
        #expect(captures == 1)
        // Quitting records a change still waiting out the delay, once.
        store.ui.workspace.equalize()
        try await wait { model.hasPendingLayoutCapture }
        model.captureLayouts()
        #expect(captures == 2)
        try await Task.sleep(for: SessionWindowModel.layoutCaptureDelay + .milliseconds(100))
        #expect(captures == 2)
    }

    private func wait(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(5)
        while !condition(), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(20)) }
        #expect(condition())
    }
}

private extension TileTree {
    var split: UUID? { if case .split(let split) = self { split.id } else { nil } }
}
