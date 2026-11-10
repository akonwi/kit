import AppKit
import SwiftUI

/// The In Sidebar workspace: the session sidebar, then the selected
/// session's bar over its tiles. The sidebar hides by narrowing its column
/// to the gap before the tiles, so the sidebar, its toggle's neighbour, the
/// title, and the tiles move as one; hidden, it peeks over the tiles while
/// its toggle is hovered.
struct SessionSideLayout<Bar: View, Tiles: View>: View {
    @Environment(\.mica) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement
    @ViewBuilder let bar: Bar
    @ViewBuilder let tiles: Tiles

    var body: some View {
        let hidden = model.sidebarHidden
        let peeking = model.sidebarPeek.showing
        HStack(spacing: 0) {
            SessionSidebarColumn(model: model, placement: $placement)
            VStack(spacing: 0) {
                HStack(spacing: 0) {
                    // Hidden, the title row starts after the sidebar's controls.
                    Color.clear.frame(width: hidden ? SessionSidebarChrome.hiddenTitleLeading - tileGap : 0, height: 1)
                    if hidden {
                        AppUpdateChip().padding(.leading, 6).transition(.opacity)
                    }
                    bar
                }
                .background(WindowTitlebarRegion())
                tiles
            }
        }
        .overlay(alignment: .topLeading) {
            if hidden && peeking {
                SessionSidebarPeekView(model: model, placement: $placement)
                    .transition(.move(edge: .leading))
            }
        }
        .overlay(alignment: .topLeading) { SessionSidebarChrome(model: model) }
        .animation(reduceMotion ? nil : SessionSidebarMotion.animation, value: hidden)
        .animation(reduceMotion ? nil : SessionSidebarMotion.animation, value: peeking)
        .background(DragCancellation(active: hidden && peeking) {
            model.sidebarPeek.close()
            return true
        })
        // Hiding the sidebar takes its rows away; a row holding the
        // keyboard gives it to the selected tile.
        .onChange(of: model.sidebarFocusHandoffs) {
            if let window = model.window { WorkspaceFocusBridge.reclaimKeyboard(in: window) }
        }
    }
}

/// How the sidebar hides, shows, and peeks: one steady movement.
enum SessionSidebarMotion {
    static let duration = 0.25
    static let animation = Animation.linear(duration: duration)
}

/// The sidebar's column. Hiding narrows it to the gap before the tiles,
/// with the sidebar sliding out past its leading edge; the rows leave once
/// they are out of sight. Its own width is read here, so a drag lays out
/// the window again without rebuilding it.
struct SessionSidebarColumn: View {
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement
    /// Whether the rows are still in the column, sliding out after hiding.
    @State private var rows: Bool

    init(model: SessionWindowModel, placement: Binding<SessionTabPlacement>) {
        self.model = model
        _placement = placement
        _rows = State(initialValue: !model.sidebarHidden)
    }

    var body: some View {
        let hidden = model.sidebarHidden
        let width = model.sidebarWidth
        ZStack(alignment: .trailing) {
            if !hidden || rows {
                SessionSidebar(model: model, placement: $placement)
                    .frame(width: width)
                    .allowsHitTesting(!hidden)
                    .accessibilityHidden(hidden)
                    // Coming back, the rows slide in with the column's edge.
                    .transition(.move(edge: .leading))
            }
        }
        .frame(width: hidden ? tileGap : width, alignment: .trailing)
        .frame(maxHeight: .infinity)
        .clipped()
        .overlay(alignment: .trailing) {
            SessionSidebarResizeHandle(model: model, enabled: !hidden).padding(.top, SessionChrome.rowHeight)
        }
        .task(id: hidden) {
            guard hidden else { rows = true; return }
            try? await Task.sleep(for: .seconds(SessionSidebarMotion.duration))
            if !Task.isCancelled { rows = false }
        }
    }
}

/// The hidden sidebar over the tiles, while its toggle is hovered. The
/// tiles keep their size.
struct SessionSidebarPeekView: View {
    @Environment(\.mica) private var theme
    let model: SessionWindowModel
    @Binding var placement: SessionTabPlacement

    var body: some View {
        SessionSidebar(model: model, placement: $placement)
            .frame(width: model.sidebarWidth)
            .frame(maxHeight: .infinity)
            .background(WindowBackdrop(color: theme.frame))
            .overlay(alignment: .trailing) { Rectangle().fill(theme.border).frame(width: 1) }
            .compositingGroup()
            .shadow(color: .black.opacity(theme.dark ? 0.45 : 0.16), radius: 16, y: 8)
            .onHover { model.sidebarPeek.pointer(onSidebar: $0) }
    }
}

/// The sidebar's toggle and New tab, over the title row. The toggle stays
/// just after the traffic lights; New tab sits at the sidebar's trailing
/// edge, and beside the toggle while the sidebar is hidden, moving between
/// them as the sidebar hides and shows.
struct SessionSidebarChrome: View {
    let model: SessionWindowModel

    var body: some View {
        let hidden = model.sidebarHidden
        ZStack(alignment: .leading) {
            SessionSidebarToggle(model: model)
                .padding(.leading, SessionChrome.sidebarToggleLeading)
            ChromeIconButton(icon: .plus, help: "New session tab (⌘T)") { model.openLauncher() }
                .padding(.leading, Self.newTabLeading(hidden: hidden, width: model.sidebarWidth))
        }
        .frame(height: SessionChrome.rowHeight)
    }

    /// The buttons' width.
    static let button: CGFloat = 26
    /// The space between the buttons while the sidebar is hidden.
    static let spacing: CGFloat = 6

    /// Where New tab starts: 16 pt in from the sidebar's trailing edge, or
    /// after the toggle while the sidebar is hidden.
    static func newTabLeading(hidden: Bool, width: CGFloat) -> CGFloat {
        hidden ? SessionChrome.sidebarToggleLeading + button + spacing : width - 2 * SessionSidebar.inset - button
    }

    /// Where the title row's content starts while the sidebar is hidden.
    static let hiddenTitleLeading = newTabLeading(hidden: true, width: 0) + button
}

/// Hides the sidebar, or shows it again. It stays put either way: just
/// after the traffic lights. While the sidebar is hidden, resting on it
/// peeks the sidebar over the tiles, and a dot marks a tab waiting for an
/// answer.
struct SessionSidebarToggle: View {
    @Environment(\.mica) private var theme
    let model: SessionWindowModel

    var body: some View {
        let hidden = model.sidebarHidden
        let attention = hidden && model.hiddenTabNeedsAnswer
        ChromeIconButton(icon: .sidebarSimple, help: SessionSidebarCommand.title(hidden: hidden) + " (⌘S)") {
            model.toggleSidebar()
        }
        .overlay(alignment: .topTrailing) {
            if attention {
                Circle().fill(theme.warning)
                    .frame(width: SessionSidebarToggle.dot, height: SessionSidebarToggle.dot)
                    .padding(3)
                    .allowsHitTesting(false)
            }
        }
        .onHover { inside in
            if hidden { model.sidebarPeek.pointer(onToggle: inside) }
        }
        .accessibilityValue(attention ? "A tab is waiting for your answer" : "")
    }

    /// The attention dot's diameter.
    static let dot: CGFloat = 6
}

/// The sidebar's trailing edge: the gap before the tiles. Dragging it
/// resizes the sidebar; letting go short of `hidingWidth` hides it, and
/// double-clicking resets its width. A line shows in the gap while the
/// pointer is over it. AppKit tracks the pointer and owns the cursor, so
/// the drag follows each mouse event and the cursor goes when the edge does.
struct SessionSidebarResizeHandle: View {
    @Environment(\.mica) private var theme
    let model: SessionWindowModel
    /// Off while the sidebar is hidden: the edge takes no clicks and shows
    /// no cursor.
    var enabled = true
    @State private var hovering = false
    /// The width when the drag began.
    @State private var start: CGFloat?

    var body: some View {
        SessionSidebarEdge(enabled: enabled, hover: { hovering = $0 }, edge: edge)
            .frame(width: SessionSidebar.inset)
            .overlay {
                if enabled && (hovering || start != nil) {
                    RoundedRectangle(cornerRadius: 1).fill(theme.strongBorder).frame(width: Self.line)
                        .allowsHitTesting(false)
                }
            }
            .accessibilityElement()
            .accessibilityLabel("Sidebar width")
            .accessibilityValue(Self.accessibilityValue(model.sidebarWidth))
            .accessibilityAdjustableAction { direction in
                switch direction {
                case .increment: model.setSidebarWidth(model.sidebarWidth + SessionSidebarLayout.accessibilityStep)
                case .decrement: model.setSidebarWidth(model.sidebarWidth - SessionSidebarLayout.accessibilityStep)
                @unknown default: break
                }
            }
            .accessibilityAction(named: "Reset width") { model.setSidebarWidth(SessionSidebarLayout.defaultWidth) }
            .accessibilityAction(named: "Hide sidebar") { model.setSidebarHidden(true) }
            .accessibilityHidden(!enabled)
    }

    /// The hover line's width.
    static let line: CGFloat = 2

    static func accessibilityValue(_ width: CGFloat) -> String { "\(Int(width.rounded())) points" }

    private func edge(_ event: SessionSidebarEdge.Event) {
        switch event {
        case .began:
            start = model.sidebarWidth
        case let .moved(distance):
            if let start { model.dragSidebar(to: start + distance) }
        case let .ended(distance):
            if let start { model.endSidebarDrag(at: start + distance, from: start) }
            start = nil
        case .cancelled:
            start = nil
        case .doubleClicked:
            model.setSidebarWidth(SessionSidebarLayout.defaultWidth)
        }
    }
}

/// The AppKit edge under `SessionSidebarResizeHandle`.
struct SessionSidebarEdge: NSViewRepresentable {
    /// What the pointer did to the edge. Distances are horizontal, in
    /// points from where the drag began.
    enum Event: Equatable {
        case began
        case moved(CGFloat)
        /// The drag moved and was let go.
        case ended(CGFloat)
        /// The edge was clicked without moving.
        case cancelled
        case doubleClicked
    }

    let enabled: Bool
    let hover: (Bool) -> Void
    let edge: (Event) -> Void

    func makeNSView(context: Context) -> EdgeView { EdgeView() }

    func updateNSView(_ view: EdgeView, context: Context) {
        view.hover = hover
        view.edge = edge
        view.enabled = enabled
    }

    final class EdgeView: NSView {
        var hover: (Bool) -> Void = { _ in }
        var edge: (Event) -> Void = { _ in }
        var enabled = true {
            didSet {
                guard enabled != oldValue else { return }
                if !enabled { setHovering(false) }
                window?.invalidateCursorRects(for: self)
            }
        }
        /// Where the drag began, in window coordinates, which stay put as
        /// the edge moves.
        private var origin: CGFloat?
        private var moved = false
        private var hovering = false
        private var tracking: NSTrackingArea?

        static var cursor: NSCursor { .columnResize }

        override var mouseDownCanMoveWindow: Bool { false }
        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
        override func hitTest(_ point: NSPoint) -> NSView? { enabled ? super.hitTest(point) : nil }

        override func resetCursorRects() {
            if enabled { addCursorRect(bounds, cursor: Self.cursor) }
        }

        override func updateTrackingAreas() {
            super.updateTrackingAreas()
            if let tracking { removeTrackingArea(tracking) }
            let area = NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .activeInActiveApp, .inVisibleRect],
                                      owner: self)
            addTrackingArea(area)
            tracking = area
        }

        override func mouseEntered(with event: NSEvent) { if enabled { setHovering(true) } }
        override func mouseExited(with event: NSEvent) { if origin == nil { setHovering(false) } }

        override func mouseDown(with event: NSEvent) {
            guard enabled else { return }
            if event.clickCount == 2 {
                edge(.doubleClicked)
                return
            }
            origin = event.locationInWindow.x
            moved = false
            // Keep the resize cursor wherever the pointer goes while it drags.
            window?.disableCursorRects()
            Self.cursor.set()
            edge(.began)
        }

        override func mouseDragged(with event: NSEvent) {
            guard let origin else { return }
            moved = true
            edge(.moved(event.locationInWindow.x - origin))
        }

        override func mouseUp(with event: NSEvent) {
            guard let origin else { return }
            finish(event.locationInWindow.x - origin)
        }

        override func viewWillMove(toWindow newWindow: NSWindow?) {
            if origin != nil { finish(nil) }
            if newWindow == nil, hovering {
                setHovering(false)
                NSCursor.arrow.set()
            }
            super.viewWillMove(toWindow: newWindow)
        }

        private func finish(_ distance: CGFloat?) {
            origin = nil
            window?.enableCursorRects()
            window?.invalidateCursorRects(for: self)
            if let distance, moved { edge(.ended(distance)) } else { edge(.cancelled) }
            moved = false
            let inside = window.map { bounds.contains(convert($0.mouseLocationOutsideOfEventStream, from: nil)) } ?? false
            setHovering(enabled && inside)
            if !inside { NSCursor.arrow.set() }
        }

        private func setHovering(_ value: Bool) {
            guard value != hovering else { return }
            hovering = value
            hover(value)
        }
    }
}

/// View › Hide Sidebar, which also shows it. It applies to the sidebar
/// placement only.
enum SessionSidebarCommand {
    static let shortcut = KeyboardShortcut("s", modifiers: .command)

    static func title(hidden: Bool) -> String { hidden ? "Show Sidebar" : "Hide Sidebar" }

    @MainActor static func enabled(window: SessionWindowModel?, placement: SessionTabPlacement) -> Bool {
        window != nil && placement == .side
    }
}
