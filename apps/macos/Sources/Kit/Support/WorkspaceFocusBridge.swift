import AppKit
import SwiftUI

/// What a focus target marker stands for: a pane's keyboard target, or the
/// session composer below the tiles.
enum WorkspaceFocusTarget: Hashable {
    case pane(WorkspacePane)
    case composer
}

/// Keeps AppKit's first responder and the workspace's focused tile in step.
///
/// - Focusing a tile (a tab click, a command, a drop) moves the first
///   responder to the focused pane's marked target view. A pane without one
///   only takes the keyboard away from views in other tiles or hidden tabs.
/// - A view in a tile becoming first responder (a click into an editor or
///   transcript) focuses that tile; the composer becoming first responder
///   hands attention to it.
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
            let contentFocused: Bool
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
            let key = Key(tile: workspace.focusedTile, pane: workspace.selected,
                          contentFocused: workspace.contentFocused, tiles: workspace.visibleTiles)
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
            guard let window, let workspace, !overlayOpen(), workspace.contentFocused,
                  Self.isVisible(self) else { return }
            let pane = workspace.selected
            let responder = window.firstResponder as? NSView
            if let responder, region(of: responder) == .tile(workspace.focusedTile), Self.isVisible(responder) {
                pendingPane = nil
                return
            }
            if let target = targetView(for: .pane(pane)) {
                pendingPane = nil
                move(window, to: target)
                return
            }
            pendingPane = pane
            if let responder, case .tile = region(of: responder) { move(window, to: nil) }
        }

        /// A marker for `pane` appeared or moved.
        func targetChanged(_ target: WorkspaceFocusTarget) {
            guard case .pane(let pane) = target, pane == pendingPane else { return }
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
                } else if workspace.focusedTile != id || !workspace.contentFocused {
                    workspace.focus(tile: id)
                }
            case .composer:
                workspace.contentFocused = false
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

        enum Region: Equatable { case tile(UUID), composer, outside }

        /// Where a view sits: in a visible tile, in the composer, or elsewhere.
        func region(of view: NSView) -> Region {
            let center = Self.windowCenter(of: view)
            let point = convert(center, from: nil)
            if bounds.contains(point) {
                let tiles = workspace?.visibleTiles ?? []
                if let id = tiles.first(where: { frames[$0]?.contains(point) == true }) { return .tile(id) }
            }
            if let composer = markers(for: .composer).first,
               composer.convert(composer.bounds, to: nil).contains(center) { return .composer }
            return .outside
        }

        /// The view that should hold the keyboard for a target: the first
        /// view accepting first responder under its visible marker's center.
        func targetView(for target: WorkspaceFocusTarget) -> NSView? {
            guard let window, let root = window.contentView,
                  let marker = markers(for: target).first(where: { Self.isVisible($0) }) else { return nil }
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

        private func markers(for target: WorkspaceFocusTarget) -> [FocusTargetMarker.MarkerView] {
            guard let root = window?.contentView else { return [] }
            var found: [FocusTargetMarker.MarkerView] = []
            var stack = [root]
            while let view = stack.popLast() {
                if let marker = view as? FocusTargetMarker.MarkerView, marker.target == target { found.append(marker) }
                stack.append(contentsOf: view.subviews)
            }
            return found
        }

        /// The center of the part of a view that is on screen, so a long
        /// scrolling document is placed by what the user sees.
        private static func windowCenter(of view: NSView) -> NSPoint {
            let visible = view.visibleRect.isEmpty ? view.bounds : view.visibleRect
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

/// Marks the view whose AppKit content should take the keyboard for a
/// focus target. Attach with `.workspaceFocusTarget(_:)`.
struct FocusTargetMarker: NSViewRepresentable {
    let target: WorkspaceFocusTarget

    func makeNSView(context: Context) -> MarkerView { MarkerView() }

    func updateNSView(_ view: MarkerView, context: Context) {
        view.target = target
        view.report()
    }

    final class MarkerView: NSView {
        var target: WorkspaceFocusTarget?
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            report()
        }
        func report() {
            guard let target, let window else { return }
            for bridge in WorkspaceFocusBridge.bridges.allObjects where bridge.window === window {
                bridge.targetChanged(target)
            }
        }
    }
}

extension View {
    /// Marks this view as where the keyboard goes when `target` is focused.
    func workspaceFocusTarget(_ target: WorkspaceFocusTarget) -> some View {
        background(FocusTargetMarker(target: target))
    }
}
