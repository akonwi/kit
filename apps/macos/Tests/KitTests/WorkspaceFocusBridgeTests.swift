import AppKit
import SwiftUI
import Testing
@testable import Kit

/// An editor stand-in that can hold the keyboard.
private final class ProbeTextView: NSTextView {
    var name = ""
    /// Report a visible rect reaching far past the view's left edge, as a
    /// web view's does.
    var overreaches = false
    override var visibleRect: NSRect {
        let rect = super.visibleRect
        return overreaches ? NSRect(x: rect.minX - 600, y: rect.minY, width: rect.width + 600, height: rect.height) : rect
    }
}

/// A scrolling text view, shaped like the app's editors.
private struct ProbeEditor: NSViewRepresentable {
    let name: String
    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSScrollView()
        let text = ProbeTextView()
        text.name = name
        text.isVerticallyResizable = false
        text.autoresizingMask = [.width, .height]
        scroll.documentView = text
        return scroll
    }
    func updateNSView(_ view: NSScrollView, context: Context) {}
}

@MainActor @Observable private final class OverlayFlag { var open = false }

/// The workspace's arrangement in miniature: every pane stays mounted, hidden
/// tabs at zero opacity without hit testing, and the bridge behind the tiles.
/// Diff has no focus target, as in the app; Agent's target is its composer.
private struct FocusHarness: View {
    let workspace: WorkspaceState
    let overlay: OverlayFlag

    var body: some View {
        GeometryReader { geometry in
            let frames = workspace.root.layout(in: CGRect(origin: .zero, size: geometry.size), gap: tileGap).tiles
            ZStack(alignment: .topLeading) {
                ForEach(workspace.panes) { pane in
                    let frame = workspace.tile(containing: pane).flatMap { frames[$0] } ?? .zero
                    let visible = workspace.isVisible(pane)
                    editor(pane)
                        .frame(width: frame.width, height: frame.height)
                        .offset(x: frame.minX, y: frame.minY)
                        .opacity(visible ? 1 : 0)
                        .allowsHitTesting(visible)
                }
            }
            .frame(width: geometry.size.width, height: geometry.size.height, alignment: .topLeading)
            .background(WorkspaceFocusBridge(workspace: workspace, frames: frames, overlayOpen: { overlay.open }))
        }
        .overlay {
            // A palette field over the tiles, as the command palette has.
            if overlay.open { ProbeEditor(name: "palette").frame(width: 200, height: 40).offset(x: -100) }
        }
        .frame(width: 600, height: 300)
    }

    @ViewBuilder private func editor(_ pane: WorkspacePane) -> some View {
        if pane == .review {
            ProbeEditor(name: pane.id)
        } else {
            ProbeEditor(name: pane.id).workspaceFocusTarget(pane)
        }
    }
}

@MainActor private final class Fixture {
    let workspace = WorkspaceState(demo: false)
    let overlay = OverlayFlag()
    let window: NSWindow
    let host: NSHostingView<FocusHarness>

    /// Agent, Scratchpad, and Diff share the left tile; the file is alone on the right.
    init() async throws {
        workspace.open(.review)
        workspace.open(.scratchpad)
        workspace.open(.file("a.swift"))
        workspace.split(.file("a.swift"), .right)
        host = NSHostingView(rootView: FocusHarness(workspace: workspace, overlay: overlay))
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 600, height: 300),
                          styleMask: [.titled], backing: .buffered, defer: false)
        window.contentView = host
        try await settle()
    }

    var left: UUID { workspace.tile(containing: .conversation)! }
    var right: UUID { workspace.tile(containing: .file("a.swift"))! }

    func settle() async throws {
        for _ in 0..<3 {
            host.layoutSubtreeIfNeeded()
            try await Task.sleep(for: .milliseconds(30))
        }
    }

    func editor(_ name: String) -> ProbeTextView {
        func all(_ view: NSView) -> [NSView] { [view] + view.subviews.flatMap(all) }
        return all(host).compactMap { $0 as? ProbeTextView }.first { $0.name == name }!
    }

    var responder: String? { (window.firstResponder as? ProbeTextView)?.name }

    var bridge: WorkspaceFocusBridge.BridgeView {
        func all(_ view: NSView) -> [NSView] { [view] + view.subviews.flatMap(all) }
        return all(host).compactMap { $0 as? WorkspaceFocusBridge.BridgeView }.first!
    }

    /// Moves the pointer to the center of `tile`, as the bridge's tracking reports it.
    func movePointer(into tile: UUID) {
        let frame = workspace.root.layout(in: bridge.bounds, gap: tileGap).tiles[tile]!
        bridge.pointerMoved(to: NSPoint(x: frame.midX, y: frame.midY))
    }
}

@MainActor struct WorkspaceFocusBridgeTests {
    @Test func focusingATileMovesTheKeyboardToItsSelectedPane() async throws {
        let fixture = try await Fixture()
        #expect(fixture.workspace.focusedTile == fixture.right)
        #expect(fixture.responder == "file:a.swift")

        fixture.workspace.select(.scratchpad)
        try await fixture.settle()
        #expect(fixture.workspace.focusedTile == fixture.left)
        #expect(fixture.responder == "scratchpad")
    }

    @Test func clickingIntoATilesViewFocusesThatTile() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.scratchpad)
        try await fixture.settle()

        fixture.window.makeFirstResponder(fixture.editor("file:a.swift"))
        try await fixture.settle()
        #expect(fixture.workspace.focusedTile == fixture.right)
        #expect(fixture.responder == "file:a.swift")
    }

    @Test func aViewWhoseVisibleRectOverreachesFocusesItsOwnTile() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.scratchpad)
        try await fixture.settle()

        let page = fixture.editor("file:a.swift")
        page.overreaches = true
        fixture.window.makeFirstResponder(page)
        try await fixture.settle()
        #expect(fixture.workspace.focusedTile == fixture.right)
    }

    @Test func aPaneWithoutTargetTakesTheKeyboardFromItsHiddenNeighbor() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.scratchpad)
        try await fixture.settle()
        #expect(fixture.responder == "scratchpad")

        fixture.workspace.select(.review)
        try await fixture.settle()
        #expect(fixture.workspace.focusedTile == fixture.left)
        #expect(fixture.window.firstResponder === fixture.window)
    }

    @Test func aHiddenTabCannotKeepTheKeyboard() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.review)
        try await fixture.settle()

        fixture.window.makeFirstResponder(fixture.editor("scratchpad"))
        try await fixture.settle()
        #expect(fixture.workspace.selected == .review)
        #expect(fixture.window.firstResponder === fixture.window)
    }

    @Test func hiddenTabsAreOutOfHitTesting() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.review)
        try await fixture.settle()
        let diff = fixture.editor("review")
        let rect = diff.convert(diff.visibleRect, to: nil)
        let frame = try #require(fixture.window.contentView?.superview)

        let hit = frame.hitTest(frame.convert(NSPoint(x: rect.midX, y: rect.midY), from: nil))
        #expect((hit as? ProbeTextView)?.name == "review")
    }

    @Test func theAgentPaneTakesTheKeyboardInItsComposer() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.conversation)
        try await fixture.settle()
        #expect(fixture.workspace.focusedTile == fixture.left)
        #expect(fixture.responder == "conversation")
    }

    @Test func closingAnOverlayReturnsTheKeyboard() async throws {
        let fixture = try await Fixture()
        #expect(fixture.responder == "file:a.swift")

        fixture.overlay.open = true
        try await fixture.settle()
        fixture.window.makeFirstResponder(fixture.editor("palette"))
        try await fixture.settle()
        #expect(fixture.workspace.focusedTile == fixture.right)
        fixture.overlay.open = false
        try await fixture.settle()
        #expect(fixture.responder == "file:a.swift")
    }

    @Test func anOverlayThatMovesFocusLeavesItThere() async throws {
        let fixture = try await Fixture()
        fixture.overlay.open = true
        try await fixture.settle()
        fixture.window.makeFirstResponder(fixture.editor("palette"))
        fixture.workspace.select(.scratchpad)
        fixture.overlay.open = false
        try await fixture.settle()
        #expect(fixture.responder == "scratchpad")
    }

    @Test func movingThePointerIntoATileFocusesItAndMovesTheKeyboard() async throws {
        let fixture = try await Fixture()
        fixture.window.makeKeyAndOrderFront(nil)
        fixture.workspace.select(.scratchpad)
        try await fixture.settle()
        #expect(fixture.responder == "scratchpad")

        fixture.movePointer(into: fixture.right)
        try await fixture.settle()
        #expect(fixture.workspace.focusedTile == fixture.right)
        #expect(fixture.responder == "file:a.swift")
        fixture.window.orderOut(nil)
    }

    @Test func thePointerDoesNotFocusUnderAnOverlayOrDuringATabDrag() async throws {
        let fixture = try await Fixture()
        fixture.window.makeKeyAndOrderFront(nil)
        fixture.workspace.select(.scratchpad)
        try await fixture.settle()

        fixture.overlay.open = true
        try await fixture.settle()
        fixture.movePointer(into: fixture.right)
        #expect(fixture.workspace.focusedTile == fixture.left)
        fixture.overlay.open = false
        try await fixture.settle()

        fixture.workspace.tabDrag = .init(pane: .scratchpad, location: .zero, target: nil)
        fixture.movePointer(into: fixture.right)
        #expect(fixture.workspace.focusedTile == fixture.left)
        fixture.window.orderOut(nil)
    }

    @Test func anOpenedPaneReceivesTheKeyboard() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.review)
        try await fixture.settle()

        fixture.workspace.open(.file("b.swift"))
        try await fixture.settle()
        #expect(fixture.responder == "file:b.swift")
    }

    /// A removed view that held the keyboard, such as a closed terminal,
    /// leaves the window holding it.
    @Test func theWindowTakingTheKeyboardReturnsItToTheFocusedPane() async throws {
        let fixture = try await Fixture()
        #expect(fixture.responder == "file:a.swift")

        fixture.window.makeFirstResponder(nil)
        try await fixture.settle()
        #expect(fixture.responder == "file:a.swift")
    }
}
