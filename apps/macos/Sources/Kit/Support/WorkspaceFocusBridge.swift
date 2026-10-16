import AppKit
import SwiftUI

/// Keeps AppKit's first responder and the workspace's focused tile in step.
///
/// - Focusing a tile (a tab click, a command, a drop) moves the first
///   responder to the focused pane's marked target view: its composer or
///   editor. A pane without one only takes the keyboard away from views in
///   other tiles or hidden tabs.
/// - A view in a tile becoming first responder (a click into a composer,
///   editor, or transcript) focuses that tile.
/// - Opening an overlay remembers the first responder, and closing it
///   returns focus there unless the overlay moved focus itself.
///
/// Hidden tabs stay mounted at zero opacity with hit testing off. SwiftUI
/// keeps their AppKit views out of hit testing but not out of the view tree,
/// so a hidden view could still hold the keyboard; the bridge moves it away.
struct WorkspaceFocusBridge: NSViewRepresentable {
    let workspace: WorkspaceState
    /// Visible tile frames in the bridge's (the tiles') coordinate space.
    let frames: [UUID: CGRect]
    /// Whether a picker or palette is above the workspace, read live: an
    /// overlay's field can take the keyboard before this view updates.
    let overlayOpen: () -> Bool

    func makeNSView(context: Context) -> BridgeView { BridgeView() }

    func updateNSView(_ view: BridgeView, context: Context) {
        view.update(workspace: workspace, frames: frames, overlayOpen: overlayOpen)
    }

    static func dismantleNSView(_ view: BridgeView, coordinator: ()) { view.stopObserving() }

    final class BridgeView: NSView {
        private(set) var workspace: WorkspaceState?
        private var frames: [UUID: CGRect] = [:]
        private var overlayOpen: () -> Bool = { false }
        private var overlayShown = false
        private var lastKey: Key?
        private var keyAtOverlayOpen: Key?
        /// The last workspace view to hold the keyboard outside an overlay,
        /// where closing an overlay returns it.
        private weak var lastResponder: NSView?
        private var observation: NSKeyValueObservation?
        /// A pane whose target view wasn't there yet when its tile was
        /// focused. Its marker appearing completes the move, unless the user
        /// put the keyboard somewhere else in the meantime.
        private var pendingPane: WorkspacePane?
        private var moving = false

        /// What the focus depends on. A change runs a sync.
        private struct Key: Equatable {
            let tile: UUID
            let pane: WorkspacePane
            let tiles: [UUID]
        }

        override var isFlipped: Bool { true }
        override func hitTest(_ point: NSPoint) -> NSView? { nil }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            stopObserving()
            guard let window else { return }
            observation = window.observe(\.firstResponder, options: [.new]) { [weak self] _, _ in
                MainActor.assumeIsolated { self?.firstResponderChanged() }
            }
            WorkspaceFocusBridge.bridges.add(self)
            scheduleSync()
        }

        func stopObserving() {
            observation?.invalidate()
            observation = nil
            WorkspaceFocusBridge.bridges.remove(self)
        }

        func update(workspace: WorkspaceState, frames: [UUID: CGRect], overlayOpen: @escaping () -> Bool) {
            self.workspace = workspace
            self.frames = frames
            self.overlayOpen = overlayOpen
            let key = Key(tile: workspace.focusedTile, pane: workspace.selected, tiles: workspace.visibleTiles)
            let open = overlayOpen()
            if open != overlayShown {
                overlayShown = open
                if open {
                    keyAtOverlayOpen = key
                } else {
                    let moved = keyAtOverlayOpen != key
                    DispatchQueue.main.async { [weak self] in self?.overlayClosed(focusMoved: moved) }
                }
            }
            if key != lastKey {
                lastKey = key
                scheduleSync()
            }
        }

        private func scheduleSync() {
            // Let SwiftUI apply the change (visibility, a new pane) first.
            DispatchQueue.main.async { [weak self] in self?.sync() }
        }

        /// Moves the first responder to the focused pane's target.
        func sync() {
            guard let window, let workspace, !overlayOpen(), Self.isVisible(self) else { return }
            let pane = workspace.selected
            let responder = window.firstResponder as? NSView
            if let responder, region(of: responder) == .tile(workspace.focusedTile), Self.isVisible(responder) {
                pendingPane = nil
                return
            }
            if let target = targetView(for: pane) {
                pendingPane = nil
                move(window, to: target)
                return
            }
            pendingPane = pane
            if let responder, case .tile = region(of: responder) { move(window, to: nil) }
        }

        /// A marker for `pane` appeared or moved.
        func targetChanged(_ pane: WorkspacePane) {
            guard pane == pendingPane else { return }
            scheduleSync()
        }

        private func overlayClosed(focusMoved: Bool) {
            guard let window, !overlayOpen() else { return }
            // A command that focused something (the composer, a new pane) wins.
            if let responder = window.firstResponder as? NSView, responder.window === window,
               responder !== window.contentView, !isDescendant(of: responder) { return }
            if !focusMoved, let saved = lastResponder, saved.window === window, Self.isVisible(saved) {
                move(window, to: saved)
            } else {
                sync()
            }
        }

        private func firstResponderChanged() {
            // An overlay's own fields sit above the tiles; they don't focus one.
            guard !moving, !overlayOpen(), let window, let workspace, Self.isVisible(self) else { return }
            pendingPane = nil
            guard let responder = window.firstResponder as? NSView, responder.window === window else { return }
            let region = region(of: responder)
            if region != .outside, Self.isVisible(responder) { lastResponder = responder }
            switch region {
            case .tile(let id):
                if !Self.isVisible(responder) {
                    // A hidden tab's view took the keyboard; give it back.
                    scheduleSync()
                } else if workspace.focusedTile != id {
                    workspace.focus(tile: id)
                }
            case .outside:
                break
            }
        }

        private func move(_ window: NSWindow, to view: NSView?) {
            moving = true
            window.makeFirstResponder(view)
            moving = false
            lastResponder = view
        }

        enum Region: Equatable { case tile(UUID), outside }

        /// Where a view sits: in a visible tile, or elsewhere.
        func region(of view: NSView) -> Region {
            let point = convert(Self.windowCenter(of: view), from: nil)
            guard bounds.contains(point) else { return .outside }
            let tiles = workspace?.visibleTiles ?? []
            return tiles.first(where: { frames[$0]?.contains(point) == true }).map(Region.tile) ?? .outside
        }

        /// The view that should hold the keyboard for a target: the first
        /// view accepting first responder under its visible marker's center.
        func targetView(for pane: WorkspacePane) -> NSView? {
            guard let window, let root = window.contentView,
                  let marker = markers(for: pane).first(where: { Self.isVisible($0) }) else { return nil }
            let rect = marker.convert(marker.bounds, to: nil)
            guard rect.width > 0, rect.height > 0 else { return nil }
            let center = NSPoint(x: rect.midX, y: rect.midY)
            var view = root.hitTest(root.superview?.convert(center, from: nil) ?? center)
            while let candidate = view, !marker.isDescendant(of: candidate) {
                if candidate.acceptsFirstResponder, Self.isVisible(candidate) { return candidate }
                view = candidate.superview
            }
            return nil
        }

        private func markers(for pane: WorkspacePane) -> [FocusTargetMarker.MarkerView] {
            guard let root = window?.contentView else { return [] }
            var found: [FocusTargetMarker.MarkerView] = []
            var stack = [root]
            while let view = stack.popLast() {
                if let marker = view as? FocusTargetMarker.MarkerView, marker.pane == pane { found.append(marker) }
                stack.append(contentsOf: view.subviews)
            }
            return found
        }

        /// The center of the part of a view that is on screen, so a long
        /// scrolling document is placed by what the user sees.
        private static func windowCenter(of view: NSView) -> NSPoint {
            // Clamped to the bounds: a web view's visible rect extends past them.
            let clipped = view.visibleRect.intersection(view.bounds)
            let visible = clipped.isEmpty ? view.bounds : clipped
            return view.convert(NSPoint(x: visible.midX, y: visible.midY), to: nil)
        }

        /// Whether the view and its ancestors are shown. SwiftUI's zero
        /// opacity puts a hosted view in a container with zero alpha.
        static func isVisible(_ view: NSView) -> Bool {
            var current: NSView? = view
            while let v = current {
                if v.isHidden || v.alphaValue == 0 { return false }
                current = v.superview
            }
            return view.window != nil
        }
    }

    /// Live bridges, so markers can report themselves without a reference.
    @MainActor static let bridges = NSHashTable<BridgeView>.weakObjects()
}

/// Marks the view whose AppKit content should take the keyboard when its
/// pane's tile is focused. Attach with `.workspaceFocusTarget(_:)`.
struct FocusTargetMarker: NSViewRepresentable {
    let pane: WorkspacePane

    func makeNSView(context: Context) -> MarkerView { MarkerView() }

    func updateNSView(_ view: MarkerView, context: Context) {
        view.pane = pane
        view.report()
    }

    final class MarkerView: NSView {
        var pane: WorkspacePane?
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            report()
        }
        func report() {
            guard let pane, let window else { return }
            for bridge in WorkspaceFocusBridge.bridges.allObjects where bridge.window === window {
                bridge.targetChanged(pane)
            }
        }
    }
}

extension View {
    /// Marks this view as where the keyboard goes when `pane`'s tile is focused.
    func workspaceFocusTarget(_ pane: WorkspacePane) -> some View {
        background(FocusTargetMarker(pane: pane))
    }

    /// Marks this view as `pane`'s keyboard target while `active`, keeping
    /// the view's identity when the target moves elsewhere.
    func workspaceFocusTarget(_ pane: WorkspacePane, when active: Bool) -> some View {
        background { if active { FocusTargetMarker(pane: pane) } }
    }
}
