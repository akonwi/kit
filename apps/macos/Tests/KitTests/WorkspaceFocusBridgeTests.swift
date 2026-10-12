import AppKit
import SwiftUI
import Testing
@testable import Kit

/// An editor stand-in that can hold the keyboard.
private final class ProbeTextView: NSTextView {
    var name = ""
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
/// tabs at zero opacity without hit testing, the bridge behind the tiles, and
/// a composer below them. Agent has no focus target, as in the app.
private struct FocusHarness: View {
    let workspace: WorkspaceState
    let overlay: OverlayFlag

    var body: some View {
        VStack(spacing: 0) {
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
            .frame(height: 300)
            .overlay {
                // A palette field over the tiles, as the command palette has.
                if overlay.open { ProbeEditor(name: "palette").frame(width: 200, height: 40).offset(x: -100) }
            }
            ProbeEditor(name: "composer").workspaceFocusTarget(.composer).frame(height: 60)
        }
        .frame(width: 600, height: 360)
    }

    @ViewBuilder private func editor(_ pane: WorkspacePane) -> some View {
        if pane == .conversation {
            ProbeEditor(name: pane.id)
        } else {
            ProbeEditor(name: pane.id).workspaceFocusTarget(.pane(pane))
        }
    }
}

@MainActor private final class Fixture {
    let workspace = WorkspaceState(demo: false)
    let overlay = OverlayFlag()
    let window: NSWindow
    let host: NSHostingView<FocusHarness>

    /// Agent and Scratchpad share the left tile; the file is alone on the right.
    init() async throws {
        workspace.open(.scratchpad)
        workspace.open(.file("a.swift"))
        workspace.split(.file("a.swift"), .right)
        host = NSHostingView(rootView: FocusHarness(workspace: workspace, overlay: overlay))
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 600, height: 360),
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

    @Test func aPaneWithoutTargetTakesTheKeyboardFromItsHiddenNeighbor() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.scratchpad)
        try await fixture.settle()
        #expect(fixture.responder == "scratchpad")

        fixture.workspace.select(.conversation)
        try await fixture.settle()
        #expect(fixture.workspace.focusedTile == fixture.left)
        #expect(fixture.window.firstResponder === fixture.window)
    }

    @Test func aHiddenTabCannotKeepTheKeyboard() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.conversation)
        try await fixture.settle()

        fixture.window.makeFirstResponder(fixture.editor("scratchpad"))
        try await fixture.settle()
        #expect(fixture.workspace.selected == .conversation)
        #expect(fixture.window.firstResponder === fixture.window)
    }

    @Test func hiddenTabsAreOutOfHitTesting() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.conversation)
        try await fixture.settle()
        let agent = fixture.editor("conversation")
        let rect = agent.convert(agent.visibleRect, to: nil)
        let frame = try #require(fixture.window.contentView?.superview)

        let hit = frame.hitTest(frame.convert(NSPoint(x: rect.midX, y: rect.midY), from: nil))
        #expect((hit as? ProbeTextView)?.name == "conversation")
    }

    @Test func theComposerTakingTheKeyboardUnfocusesTheTiles() async throws {
        let fixture = try await Fixture()
        fixture.window.makeFirstResponder(fixture.editor("composer"))
        try await fixture.settle()
        #expect(fixture.workspace.contentFocused == false)

        fixture.workspace.select(.scratchpad)
        try await fixture.settle()
        #expect(fixture.workspace.contentFocused)
        #expect(fixture.responder == "scratchpad")
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

    @Test func anOpenedPaneReceivesTheKeyboard() async throws {
        let fixture = try await Fixture()
        fixture.workspace.select(.conversation)
        try await fixture.settle()

        fixture.workspace.open(.file("b.swift"))
        try await fixture.settle()
        #expect(fixture.responder == "file:b.swift")
    }
}
