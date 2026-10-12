import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor struct WorkspaceNavigationTests {
    @Test func panesOpenInTheFocusedTileAndReopeningFocusesThem() {
        let workspace = WorkspaceState(demo: false)
        let first = workspace.focusedTile
        workspace.open(.file("main.swift"))
        workspace.open(.agent("reviewer"))
        #expect(workspace.tiles[first] == WorkspaceTile(panes: [.conversation, .file("main.swift"), .agent("reviewer")], selected: .agent("reviewer")))

        workspace.split(.file("main.swift"), .right)
        let second = workspace.focusedTile
        #expect(workspace.root.tiles == [first, second])
        #expect(workspace.tiles[first] == WorkspaceTile(panes: [.conversation, .agent("reviewer")], selected: .agent("reviewer")))
        #expect(workspace.tiles[second] == WorkspaceTile(panes: [.file("main.swift")], selected: .file("main.swift")))
        #expect(workspace.isVisible(.agent("reviewer")))
        #expect(workspace.isVisible(.file("main.swift")))

        workspace.open(.scratchpad)
        #expect(workspace.tiles[second]?.panes == [.file("main.swift"), .scratchpad])
        workspace.select(.conversation)
        #expect(workspace.focusedTile == first)
        workspace.open(.scratchpad)
        #expect(workspace.focusedTile == second)
        #expect(workspace.selected == .scratchpad)
        #expect(workspace.panes == [.conversation, .file("main.swift"), .agent("reviewer"), .scratchpad])
    }

    @Test func closingATilesLastTabRemovesTheTile() {
        let workspace = WorkspaceState(demo: false)
        let first = workspace.focusedTile
        workspace.open(.file("main.swift"))
        workspace.open(.scratchpad)
        workspace.split(.scratchpad, .bottom)
        workspace.close(.scratchpad)
        #expect(workspace.root == .tile(first))
        #expect(workspace.tiles.keys.sorted() == [first])
        #expect(workspace.focusedTile == first)
        #expect(workspace.selected == .file("main.swift"))
        workspace.close(.file("main.swift"))
        workspace.close(.conversation)
        #expect(workspace.tiles[first] == WorkspaceTile(panes: [.conversation], selected: .conversation))
    }

    @Test func movingIntoATileOrOntoItsEdge() {
        let workspace = WorkspaceState(demo: false)
        let first = workspace.focusedTile
        workspace.open(.review)
        workspace.open(.scratchpad)
        workspace.split(.review, .right)
        let right = workspace.focusedTile
        workspace.move(.scratchpad, to: right, edge: .bottom)
        let bottom = workspace.focusedTile
        // Agent | (Diff / Scratchpad)
        guard case .split(let outer) = workspace.root, case .split(let inner) = outer.second else {
            Issue.record("expected nested splits"); return
        }
        #expect(outer.axis == .horizontal && outer.first == .tile(first))
        #expect(inner.axis == .vertical && inner.first == .tile(right) && inner.second == .tile(bottom))

        workspace.move(.scratchpad, to: first, edge: .center)
        #expect(workspace.tiles[bottom] == nil)
        #expect(workspace.root.tiles == [first, right])
        #expect(workspace.tiles[first] == WorkspaceTile(panes: [.conversation, .scratchpad], selected: .scratchpad))
        // A tile's only tab can't split away from itself.
        workspace.split(.review, .left)
        #expect(workspace.root.tiles == [first, right])
        #expect(!workspace.canSplit(.review))
    }

    @Test func zoomShowsOneTileUntilPanesOpenOrMove() {
        let workspace = WorkspaceState(demo: false)
        workspace.open(.review)
        workspace.split(.review, .right)
        let right = workspace.focusedTile
        workspace.toggleZoom()
        #expect(workspace.zoomedTile == right)
        #expect(workspace.visibleTiles == [right])
        #expect(!workspace.isVisible(.conversation))
        workspace.select(.conversation)
        #expect(workspace.zoomedTile == nil)

        workspace.toggleZoom()
        workspace.open(.scratchpad)
        #expect(workspace.zoomedTile == nil)
        // Zooming a lone tile does nothing.
        workspace.joinTiles(selecting: .review)
        workspace.toggleZoom()
        #expect(workspace.zoomedTile == nil)
    }

    @Test func joiningPreservesDraftsAndPutsAgentFirst() {
        let workspace = WorkspaceState(demo: false)
        workspace.open(.scratchpad)
        workspace.scratchpad = "Retained draft"
        workspace.open(.review)
        workspace.select(.conversation)
        workspace.split(.conversation, .right)
        workspace.joinTiles(selecting: .scratchpad)
        #expect(workspace.root.tiles.count == 1)
        #expect(workspace.tiles[workspace.focusedTile] == WorkspaceTile(panes: [.conversation, .scratchpad, .review], selected: .scratchpad))
        #expect(workspace.scratchpad == "Retained draft")
    }

    @Test func tilesRenderWithGapsAndKeepEditorsMountedAcrossMoves() async throws {
        let store = SessionStore(fixture: try Fixture.load())
        let workspace = store.ui.workspace
        workspace.open(.scratchpad)
        workspace.split(.scratchpad, .right)
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        let host = NSHostingView(rootView: WorkspaceLayout(state: store).environment(\.mica, theme).environment(\.colorScheme, .dark))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1100, height: 750), styleMask: [.titled], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<15 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(30)) }

        func descendants(_ view: NSView) -> [NSView] { [view] + view.subviews.flatMap(descendants) }
        let handles = descendants(host).compactMap { $0 as? TileDivider.Handle }
        #expect(handles.count == 1)
        let handle = try #require(handles.first)
        let gap = handle.convert(handle.bounds, to: host)
        #expect(handle.axis == .horizontal)
        // The gap divides the width evenly: (1100 - 6) / 2 on each side.
        #expect(gap.minX == 547)
        #expect(gap.width == tileGap)

        let editors = descendants(host).filter { String(describing: type(of: $0)) == "ForwardingScrollView" }
        #expect(!editors.isEmpty)
        workspace.move(.scratchpad, to: workspace.root.tiles[0], edge: .center)
        workspace.split(.scratchpad, .bottom)
        workspace.toggleZoom()
        for _ in 0..<5 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(30)) }
        let retained = Set(descendants(host).map(ObjectIdentifier.init))
        #expect(editors.allSatisfy { retained.contains(ObjectIdentifier($0)) })

        workspace.toggleZoom()
        for _ in 0..<5 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(30)) }
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        try #require(bitmap.representation(using: .png, properties: [:])).write(to: URL(fileURLWithPath: "/tmp/kit-workspace-tiles.png"))
    }
}
