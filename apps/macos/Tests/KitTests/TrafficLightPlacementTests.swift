import AppKit
import Testing
@testable import Kit

@MainActor struct TrafficLightPlacementTests {
    @Test func lightsCenterInTheChromeRowAndFollowResize() throws {
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1000, height: 700),
                              styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        defer { window.close() }
        let placement = TrafficLightPlacement(window: window)
        let buttons = try [NSWindow.ButtonType.closeButton, .miniaturizeButton, .zoomButton].map {
            try #require(window.standardWindowButton($0))
        }

        func frames() -> [NSRect] { buttons.map { $0.convert($0.bounds, to: nil) } }
        let spacing = buttons[1].frame.minX - buttons[0].frame.minX
        let height = buttons[0].frame.height
        let expected = (0..<3).map { index in
            NSRect(x: 16 + CGFloat(index) * spacing,
                   y: window.frame.height - 44 + ((44 - height) / 2).rounded(),
                   width: buttons[index].frame.width, height: height)
        }
        #expect(frames() == expected)

        window.setContentSize(NSSize(width: 800, height: 500))
        placement.place()
        #expect(frames().map(\.minX) == expected.map(\.minX))
        #expect(frames().map(\.minY) == expected.map { $0.minY - 200 })
    }

    @Test func doubleClickFollowsTheSystemTitleBarSetting() {
        typealias Action = WindowTitlebarRegion.DoubleClickAction
        #expect(WindowTitlebarRegion.doubleClickAction(setting: nil, miniaturize: false) == .zoom)
        #expect(WindowTitlebarRegion.doubleClickAction(setting: nil, miniaturize: true) == .minimize)
        let cases: [(String, Action)] = [("Maximize", .zoom), ("Fill", .fill), ("Minimize", .minimize), ("None", .none)]
        for (setting, action) in cases {
            #expect(WindowTitlebarRegion.doubleClickAction(setting: setting, miniaturize: true) == action)
        }
    }
}
