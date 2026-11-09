import AppKit
import SwiftUI
import Testing
@testable import Kit

/// Window glass (ADR 0064): the frame is Liquid Glass only on macOS 26,
/// with the preference on and Reduce Transparency off, and the theme's
/// colour tints it only when Tint with theme is on. Otherwise it is the
/// theme colour, opaque, in an opaque window.
@MainActor struct WindowGlassTests {
    @Test func glassIsOnAndTintedByDefault() {
        #expect(WindowGlass.storageKey == "windowGlass")
        #expect(WindowGlass.defaultValue)
        #expect(WindowGlass.themeTintKey == "windowGlassThemeTint")
        #expect(WindowGlass.themeTintDefault)
    }

    /// Tint with theme lays the theme colour over the glass at 50%; off,
    /// the glass is the system's own.
    @Test func tintWithThemeCoversTheGlassAtHalf() {
        #expect(WindowGlass.tint(themeTint: true) == 0.5)
        #expect(WindowGlass.tint(themeTint: false) == 0)
    }

    @Test(arguments: [
        (available: true, enabled: true, reduce: false, glass: true),
        (available: true, enabled: false, reduce: false, glass: false),
        (available: true, enabled: true, reduce: true, glass: false),
        (available: false, enabled: true, reduce: false, glass: false),
        (available: false, enabled: false, reduce: true, glass: false),
    ])
    func glassNeedsTheSystemThePreferenceAndTransparency(available: Bool, enabled: Bool, reduce: Bool, glass: Bool) {
        #expect(WindowGlass.active(enabled: enabled, reduceTransparency: reduce, available: available) == glass)
    }

    /// Without glass the frame draws exactly what the theme colour alone
    /// draws, and the window keeps its own opaque background.
    @Test(arguments: [false, true])
    func anOpaqueFrameIsTheThemeColour(dark: Bool) async throws {
        let theme = MicaTheme(dark: dark)
        let host = NSHostingView(rootView: HStack(spacing: 0) {
            WindowFrameFill(color: theme.frame, glass: false)
            theme.frame
        }.frame(width: 80, height: 40))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 80, height: 40))
        let background = window.backgroundColor
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        host.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(50))

        #expect(window.isOpaque)
        #expect(window.backgroundColor == background)
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        let row = bitmap.pixelsHigh / 2
        let frame = try #require(bitmap.colorAt(x: bitmap.pixelsWide / 4, y: row)?.usingColorSpace(.sRGB))
        let swatch = try #require(bitmap.colorAt(x: bitmap.pixelsWide * 3 / 4, y: row)?.usingColorSpace(.sRGB))
        #expect(frame == swatch, "\(frame) vs \(swatch)")
        #expect(frame.alphaComponent == 1)
    }

    /// Glass clears the window so the desktop shows through, and turning
    /// it off, or taking the frame out of the window, gives the window
    /// back its own background.
    @Test(.enabled(if: WindowGlass.available))
    func glassClearsTheWindowAndGivesItBack() async throws {
        let color = MicaTheme(dark: false).frame
        func fill(_ glass: Bool) -> AnyView { AnyView(WindowFrameFill(color: color, glass: glass).frame(width: 40, height: 40)) }
        let host = NSHostingView(rootView: fill(true))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 40, height: 40))
        let background = window.backgroundColor
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }

        try await eventually { !window.isOpaque }
        #expect(window.backgroundColor == .clear)

        host.rootView = fill(false)
        try await eventually { window.isOpaque }
        #expect(window.backgroundColor == background)

        host.rootView = fill(true)
        try await eventually { !window.isOpaque }
        window.contentView = NSView()
        try await eventually { window.isOpaque }
        #expect(window.backgroundColor == background)
    }
}
