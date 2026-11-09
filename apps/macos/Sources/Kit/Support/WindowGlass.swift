import AppKit
import SwiftUI

/// Window glass (ADR 0064): on macOS 26 and later the frame around the
/// tiles (the session sidebar or top tabs, the session bar, and the gaps
/// between tiles) and the Settings section list are Liquid Glass, plain or
/// under the theme's own colour. Tiles stay opaque. Earlier systems, the
/// preference off, and Reduce Transparency all draw the opaque frame.
enum WindowGlass {
    /// The app preference: whether the frame is glass where it can be. On by default.
    static let storageKey = "windowGlass"
    static let defaultValue = true
    /// The app preference: whether the theme colour tints the glass. On by
    /// default; off, the frame is the system's own glass.
    static let themeTintKey = "windowGlassThemeTint"
    static let themeTintDefault = true
    /// How much of the theme colour covers the glass when it is tinted.
    static let themeTint = 0.5

    /// Whether this Mac can draw Liquid Glass.
    static var available: Bool {
        if #available(macOS 26, *) { true } else { false }
    }

    /// Whether the frame is glass.
    static func active(enabled: Bool, reduceTransparency: Bool, available: Bool = available) -> Bool {
        available && enabled && !reduceTransparency
    }

    /// How much of the theme colour covers glass: none for the system's
    /// own glass.
    static func tint(themeTint: Bool) -> Double {
        themeTint ? Self.themeTint : 0
    }
}

/// A window's frame: `color`, or glass while Window glass is on, under
/// `color` while Tint with theme is on too.
struct WindowBackdrop: View {
    let color: Color
    @AppStorage(WindowGlass.storageKey) private var enabled = WindowGlass.defaultValue
    @AppStorage(WindowGlass.themeTintKey) private var themeTint = WindowGlass.themeTintDefault
    @Environment(\.accessibilityReduceTransparency) private var reduceTransparency

    var body: some View {
        WindowFrameFill(
            color: color,
            glass: WindowGlass.active(enabled: enabled, reduceTransparency: reduceTransparency),
            tint: WindowGlass.tint(themeTint: themeTint))
    }
}

/// The frame's fill. Glass needs a window that lets the desktop through,
/// so the window is made clear while the fill is glass and given back its
/// own background when it isn't. A window that never shows glass is left
/// untouched.
struct WindowFrameFill: View {
    let color: Color
    let glass: Bool
    /// How much of `color` covers the glass.
    var tint: Double = 0

    var body: some View {
        if #available(macOS 26, *) {
            ZStack {
                if glass {
                    Rectangle().fill(.clear).glassEffect(.regular, in: .rect)
                    if tint > 0 { color.opacity(tint) }
                } else {
                    color
                }
            }
            .background { WindowClearing(clear: glass).frame(width: 0, height: 0) }
        } else {
            color
        }
    }
}

private struct WindowClearing: NSViewRepresentable {
    let clear: Bool

    func makeNSView(context: Context) -> Probe { Probe() }
    func updateNSView(_ view: Probe, context: Context) { view.clear = clear }

    final class Probe: NSView {
        var clear = false {
            didSet { if clear != oldValue { apply() } }
        }
        /// The window and what it had before it was cleared.
        private var cleared: (window: NSWindow, opaque: Bool, background: NSColor)?

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            apply()
        }

        private func apply() {
            if let cleared, !clear || cleared.window !== window {
                cleared.window.isOpaque = cleared.opaque
                cleared.window.backgroundColor = cleared.background
                self.cleared = nil
            }
            guard clear, cleared == nil, let window else { return }
            cleared = (window, window.isOpaque, window.backgroundColor)
            window.isOpaque = false
            window.backgroundColor = .clear
        }
    }
}
