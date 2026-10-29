import AppKit
import SwiftUI
import Testing
@testable import Kit

/// The top tabs sit in the window's title bar, where AppKit moves the window
/// for a drag before the tab's own drag gesture sees it. Each tab carries a
/// region that AppKit leaves out of the title bar's drag area.
@MainActor struct TitlebarDragExclusionTests {
    /// AppKit leaves a title-bar view out of the drag area only when it
    /// can't move the window, accepts first responder, and overrides
    /// `mouseDown(with:)`. Without any one of these a tab drag moved the
    /// window. The region still never takes focus or a click.
    @Test func aRegionMeetsEachConditionForLeavingTheDragAreaAndTakesNoInput() {
        let region = TitlebarDragExclusion.Region(frame: NSRect(x: 0, y: 0, width: 220, height: 30))
        #expect(!region.mouseDownCanMoveWindow)
        #expect(region.acceptsFirstResponder)
        #expect(TitlebarDragExclusion.Region.instanceMethod(for: #selector(NSView.mouseDown(with:)))
            != NSView.instanceMethod(for: #selector(NSView.mouseDown(with:))))
        #expect(!region.becomeFirstResponder())
        #expect(!region.canBecomeKeyView)
        #expect(region.hitTest(NSPoint(x: 110, y: 15)) == nil)
    }

    /// Each tab is cut out of the drag area, and only the tabs: the space
    /// around them still moves the window.
    @Test func eachTopTabAndNothingElseIsCutOutOfTheDragArea() async throws {
        _ = NSApplication.shared
        let model = SessionWindowModel(id: UUID(), sessions: ["a", "b", "c"].map {
            SessionIdentity(server: SessionWindowModel.defaultServer, session: $0)
        }, selected: SessionIdentity(server: SessionWindowModel.defaultServer, session: "b"))
        let size = NSSize(width: 1200, height: SessionChrome.rowHeight)
        let host = NSHostingView(rootView: SessionTopTabs(model: model, placement: .constant(.top))
            .frame(width: size.width, height: size.height, alignment: .topLeading))
        host.sizingOptions = []
        let window = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<10 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }

        func regions(in view: NSView) -> [NSView] {
            view.subviews.flatMap { ($0 is TitlebarDragExclusion.Region ? [$0] : []) + regions(in: $0) }
        }
        // Tabs start at 90 pt (16 pt padding, the 68 pt traffic-light space,
        // and 6 pt) and repeat every 229 pt, 220 by 30 pt and centered in
        // the 44 pt row. Frames are in the window's bottom-up coordinates.
        let pitch = SessionTabStrip.tabWidth + SessionTabStrip.separatorWidth
        let y = (SessionChrome.rowHeight - SessionTabStrip.tabHeight) / 2
        let expected = (0..<3).map {
            NSRect(x: 90 + CGFloat($0) * pitch, y: y, width: SessionTabStrip.tabWidth, height: SessionTabStrip.tabHeight)
        }
        let frames = regions(in: host).map { $0.convert($0.bounds, to: nil) }.sorted { $0.minX < $1.minX }
        #expect(frames == expected)
    }
}
