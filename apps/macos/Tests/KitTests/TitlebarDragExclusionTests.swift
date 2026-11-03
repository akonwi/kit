import AppKit
import SwiftUI
import Testing
@testable import Kit

/// The top tabs sit in the window's title bar, where AppKit moves the window
/// for a drag before the tab's own drag gesture sees it. The strip carries a
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

    /// The strip's viewport, and nothing else in the row, is cut out of the
    /// drag area. AppKit doesn't work the drag area out again when the strip
    /// scrolls, so a cut-out that moved with each tab was left behind, and a
    /// press on a tab where a divider had been moved the window. The cut-out
    /// stays on every visible tab as the strip scrolls and follows the
    /// viewport when the window is resized.
    @Test func theStripsViewportIsCutOutOfTheDragAreaWhereverItsTabsScroll() async throws {
        _ = NSApplication.shared
        let sessions = (0..<10).map { SessionIdentity(server: SessionWindowModel.defaultServer, session: "s\($0)") }
        let model = SessionWindowModel(id: UUID(), sessions: sessions, selected: sessions[0])
        let size = NSSize(width: 1200, height: SessionChrome.rowHeight)
        let host = NSHostingView(rootView: SessionTopTabs(model: model, placement: .constant(.top))
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading))
        host.sizingOptions = []
        let window = NSWindow(testContentRect: NSRect(origin: .zero, size: size))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        func settle() async throws {
            for _ in 0..<10 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }
        }
        func views<View: NSView>(_ type: View.Type, in view: NSView) -> [View] {
            view.subviews.flatMap { (($0 as? View).map { [$0] } ?? []) + views(type, in: $0) }
        }
        try await settle()
        let strip = try #require(views(NSScrollView.self, in: host).only)
        let pitch = SessionTabStrip.tabWidth + SessionTabStrip.separatorWidth
        let y = (SessionChrome.rowHeight - SessionTabStrip.tabHeight) / 2

        /// The cut-out, which must be the strip's viewport, and the part of
        /// each tab that shows in it, in the window's coordinates.
        func layout() throws -> (cutOut: NSRect, viewport: NSRect, tabs: [NSRect]) {
            let region = try #require(views(TitlebarDragExclusion.Region.self, in: host).only)
            let viewport = strip.convert(strip.bounds, to: nil)
            let offset = strip.contentView.bounds.minX
            let tabs = (0..<sessions.count).map {
                NSRect(x: viewport.minX - offset + CGFloat($0) * pitch, y: y,
                       width: SessionTabStrip.tabWidth, height: SessionTabStrip.tabHeight).intersection(viewport)
            }
            return (region.convert(region.bounds, to: nil), viewport, tabs.filter { !$0.isEmpty })
        }

        // Tabs start at 90 pt: 16 pt padding, the 68 pt traffic-light space,
        // and 6 pt. Ten tabs overflow the row, so the strip scrolls.
        let before = try layout()
        #expect(before.cutOut == before.viewport)
        #expect(before.cutOut.minX == 90)
        #expect(before.cutOut.minY == 0 && before.cutOut.height == SessionChrome.rowHeight)
        #expect(before.tabs.allSatisfy { before.cutOut.contains($0) })

        // Scrolled by part of a tab, a divider is where a tab was.
        strip.contentView.scroll(to: NSPoint(x: pitch * 2 + 100, y: 0))
        strip.reflectScrolledClipView(strip.contentView)
        try await settle()
        let scrolled = try layout()
        #expect(strip.contentView.bounds.minX == pitch * 2 + 100)
        #expect(scrolled.cutOut == before.cutOut)
        #expect(scrolled.tabs.allSatisfy { scrolled.cutOut.contains($0) })

        // A narrower window narrows the viewport, and the cut-out with it.
        window.setContentSize(NSSize(width: 900, height: SessionChrome.rowHeight))
        try await settle()
        let narrowed = try layout()
        #expect(narrowed.cutOut == narrowed.viewport)
        #expect(narrowed.cutOut.width < before.cutOut.width)
        #expect(narrowed.tabs.allSatisfy { narrowed.cutOut.contains($0) })
    }
}

private extension Array {
    /// The only element, or nil when there are none or several.
    var only: Element? { count == 1 ? first : nil }
}
