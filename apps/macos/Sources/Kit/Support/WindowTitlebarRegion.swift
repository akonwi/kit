import AppKit
import SwiftUI

/// Gives empty window chrome titlebar behavior: dragging moves the window
/// and a double-click applies the system's title-bar double-click action.
/// SwiftUI content drawn above it, such as tabs and buttons, keeps its clicks.
struct WindowTitlebarRegion: NSViewRepresentable {
    func makeNSView(context: Context) -> RegionView { RegionView() }
    func updateNSView(_ view: RegionView, context: Context) {}

    final class RegionView: NSView {
        override var mouseDownCanMoveWindow: Bool { false }
        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

        override func mouseDown(with event: NSEvent) {
            guard let window else { return }
            if event.clickCount == 2 {
                WindowTitlebarRegion.performDoubleClickAction(in: window)
            } else {
                window.performDrag(with: event)
            }
        }
    }

    /// The action chosen in System Settings > Desktop & Dock.
    enum DoubleClickAction: Equatable { case zoom, fill, minimize, none }

    /// Maps the global `AppleActionOnDoubleClick` setting, falling back to
    /// the older `AppleMiniaturizeOnDoubleClick` flag when it is unset.
    static func doubleClickAction(setting: String?, miniaturize: Bool) -> DoubleClickAction {
        switch setting {
        case "Minimize": .minimize
        case "None": .none
        case "Fill": .fill
        case "Maximize": .zoom
        default: miniaturize ? .minimize : .zoom
        }
    }

    @MainActor static func performDoubleClickAction(in window: NSWindow) {
        let defaults = UserDefaults.standard
        switch doubleClickAction(setting: defaults.string(forKey: "AppleActionOnDoubleClick"),
                                 miniaturize: defaults.bool(forKey: "AppleMiniaturizeOnDoubleClick")) {
        case .zoom: window.performZoom(nil)
        case .minimize: window.performMiniaturize(nil)
        case .none: break
        case .fill:
            // AppKit exposes Fill only through the Window menu's own item.
            if let item = NSApp.windowsMenu?.items.first(where: { $0.title == "Fill" }), let action = item.action {
                NSApp.sendAction(action, to: item.target, from: item)
            } else {
                window.performZoom(nil)
            }
        }
    }
}
