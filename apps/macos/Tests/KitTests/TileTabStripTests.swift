import AppKit
import SwiftUI
import Testing
@testable import Kit

struct TileTabWidthsTests {
    @Test func tabsThatFitKeepTheirNaturalWidths() {
        let tabs = TileTabWidths(natural: [60, 150, 222], selected: 0, available: 500)
        #expect(tabs.widths == [60, 150, 222])
    }

    @Test func theWidestTabsShrinkFirst() {
        // 654 pt of tabs in 570: the two widest give way to 180 pt.
        #expect(TileTabWidths(natural: [60, 150, 222, 222], selected: 0, available: 570).widths == [60, 150, 180, 180])
        // In 510, every tab past 60 shrinks to 150.
        #expect(TileTabWidths(natural: [60, 150, 222, 222], selected: 0, available: 510).widths == [60, 150, 150, 150])
    }

    @Test func theSelectedTabStaysReadable() {
        let tabs = TileTabWidths(natural: [222, 222, 222], selected: 2, available: 400)
        #expect(tabs.widths == [120, 120, 160])
    }

    @Test func tabsThatDoNotFitAtTheirMinimumWidthsStayThere() {
        let tabs = TileTabWidths(natural: [222, 60, 222, 222], selected: 3, available: 300)
        #expect(tabs.widths == [88, 60, 88, 160])
    }

    @Test func aSelectedTabNarrowerThanTheReadableWidthKeepsItsOwn() {
        #expect(TileTabWidths(natural: [222, 100, 222], selected: 1, available: 300).widths == [100, 100, 100])
    }

    @Test func withoutASelectionEveryTabShrinksAlike() {
        #expect(TileTabWidths(natural: [222, 222], selected: nil, available: 300).widths == [150, 150])
    }

    @Test func anUnmeasuredStripKeepsNaturalWidths() {
        #expect(TileTabWidths(natural: [222, 222], selected: 0, available: nil).widths == [222, 222])
    }
}

struct TabStripRevealTests {
    private func reveal(_ span: ClosedRange<CGFloat>, offset: CGFloat) -> CGFloat? {
        TabStripOverflow<Int>.revealOffset(span, offset: offset, viewport: 400, content: 1000, leading: 24, trailing: 32)
    }

    @Test func aTabInViewStaysPut() {
        #expect(reveal(100...200, offset: 0) == nil)
        #expect(reveal(330...368, offset: 0) == nil)
    }

    @Test func aTabPastTheTrailingEdgeScrollsClearOfTheFade() {
        // The tab ends 32 pt before the strip's trailing edge.
        #expect(reveal(500...600, offset: 0) == 232)
    }

    @Test func aTabBeforeTheLeadingEdgeScrollsClearOfTheFade() {
        #expect(reveal(300...400, offset: 500) == 276)
    }

    @Test func revealingTheEndsStopsAtTheContent() {
        #expect(reveal(840...1000, offset: 0) == 600)
        #expect(reveal(0...88, offset: 600) == 0)
    }

    @Test func hiddenTabsAreThoseNotWhollyInView() {
        // Nine 88 pt tabs and the 160 pt selected one, scrolled to the end
        // of a 433 pt strip.
        let widths = TileTabWidths(natural: Array(repeating: 222, count: 10), selected: 9, available: 465).widths
        var x: CGFloat = 0
        var spans: [Int: ClosedRange<CGFloat>] = [:]
        for (index, width) in widths.enumerated() { spans[index] = x...(x + width); x += width }
        let strip = TabStripOverflow(frames: spans, offset: 952 - 433, viewport: 433)
        #expect((0..<10).filter(strip.hides) == [0, 1, 2, 3, 4, 5])
        #expect(strip.hiddenBefore)
        #expect(!strip.hiddenAfter)
    }
}

@MainActor struct HorizontalWheelScrollingTests {
    private func wheel(vertical: Int32, horizontal: Int32 = 0, continuous: Bool = false) throws -> NSEvent {
        let event = try #require(CGEvent(scrollWheelEvent2Source: nil, units: continuous ? .pixel : .line,
                                         wheelCount: 2, wheel1: vertical, wheel2: horizontal, wheel3: 0))
        if continuous { event.setIntegerValueField(.scrollWheelEventIsContinuous, value: 1) }
        return try #require(NSEvent(cgEvent: event))
    }

    @Test func aMouseWheelScrollsAcross() throws {
        let original = try wheel(vertical: -3)
        let converted = try #require(HorizontalWheelScrolling.horizontal(original))
        #expect(converted.scrollingDeltaY == 0)
        #expect(converted.scrollingDeltaX == original.scrollingDeltaY)
        #expect(converted.deltaX == original.deltaY)
    }

    /// A strip with a panel over part of it, as the command palette or a
    /// toast can cover a tile's tabs.
    private struct CoveredStrip: View {
        var body: some View {
            ZStack(alignment: .topLeading) {
                ScrollView(.horizontal) {
                    HStack(spacing: 0) {
                        ForEach(0..<8) { Text("Tab \($0) with a long title").padding(.horizontal, 10) }
                    }.frame(maxHeight: .infinity)
                }
                .scrollIndicators(.never)
                .background(HorizontalWheelScrolling())
                .frame(width: 360, height: 34)
                VStack { Text("Palette"); TextField("Search", text: .constant("")) }
                    .frame(width: 200, height: 60).background(Color.gray).offset(x: 100, y: 10)
            }
            .frame(width: 400, height: 100, alignment: .topLeading)
        }
    }

    @Test func aPanelOverTheStripKeepsItsWheel() async throws {
        let host = NSHostingView(rootView: CoveredStrip())
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 400, height: 100), styleMask: [.borderless],
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<5 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(30)) }
        func descendants(_ view: NSView) -> [NSView] { [view] + view.subviews.flatMap(descendants) }
        let region = try #require(descendants(host).compactMap { $0 as? HorizontalWheelScrolling.Region }.first)
        let screen = try #require(NSScreen.screens.first).frame.height
        func wheel(at point: CGPoint) throws -> NSEvent {
            let event = try #require(CGEvent(scrollWheelEvent2Source: nil, units: .line, wheelCount: 1,
                                             wheel1: -3, wheel2: 0, wheel3: 0))
            event.location = CGPoint(x: point.x, y: screen - (window.frame.maxY - point.y))
            return try #require(NSEvent(cgEvent: event))
        }
        #expect(region.redirect(try wheel(at: CGPoint(x: 50, y: 17))) == nil)
        let overPanel = try wheel(at: CGPoint(x: 200, y: 22))
        #expect(region.redirect(overPanel) === overPanel)
    }

    @Test func trackpadsAndHorizontalWheelsPassThrough() throws {
        #expect(HorizontalWheelScrolling.horizontal(try wheel(vertical: -30, continuous: true)) == nil)
        #expect(HorizontalWheelScrolling.horizontal(try wheel(vertical: 0, horizontal: -3)) == nil)
    }
}

/// A 1200 pt workspace split in two: the Agent on the left, and browser
/// tabs in the focused right tile, 597 pt wide. Its strip has 499 pt for
/// tabs and the add button beside the tile actions; 465 pt for the tabs.
@MainActor private final class TileStripHarness {
    let workspace: WorkspaceState
    let panes: [WorkspacePane]
    private let store: SessionStore
    private let engines = FakeBrowserEngines()
    private let host: NSView
    private let window: NSWindow

    init(titles: [String]) async throws {
        store = SessionStore(fixture: try Fixture.load())
        workspace = store.ui.workspace
        workspace.makeBrowserEngine = engines.make
        let first = workspace.openBrowser(URL(string: "https://example.com/0"))
        workspace.split(.browser(first.id), .right)
        var panes = [WorkspacePane.browser(first.id)]
        for index in titles.indices.dropFirst() {
            panes.append(.browser(workspace.openBrowser(URL(string: "https://example.com/\(index)")).id))
        }
        self.panes = panes
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        host = NSHostingView(rootView: WorkspaceLayout(state: store).environment(\.mica, theme).environment(\.colorScheme, .dark))
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1200, height: 300), styleMask: [.borderless],
                          backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFront(nil)
        try await settle()
        for engine in engines.made {
            guard let index = engine.state.url.flatMap({ Int($0.lastPathComponent) }) else { continue }
            engine.change { $0.title = titles[index] }
        }
        try await settle()
    }

    func close() { window.close() }

    /// Gives the tab at `index` a new page title.
    func retitle(_ index: Int, _ title: String) {
        engines.made.first { $0.state.url?.lastPathComponent == "\(index)" }?.change { $0.title = title }
    }

    func resize(width: CGFloat) { window.setContentSize(NSSize(width: width, height: 300)) }

    func settle(passes: Int = 15) async throws {
        for _ in 0..<passes { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(30)) }
    }

    private var views: [NSView] {
        func descendants(_ view: NSView) -> [NSView] { [view] + view.subviews.flatMap(descendants) }
        return descendants(host)
    }

    private func frame(_ view: NSView) -> CGRect { view.convert(view.bounds, to: host) }

    /// The right tile's tab strip.
    var strip: NSScrollView {
        get throws {
            try #require(views.compactMap { $0 as? NSScrollView }.first { view in
                let frame = frame(view)
                return frame.minX > host.bounds.midX && frame.minY == 0 && frame.height == 34
            })
        }
    }
    var viewport: CGRect { get throws { frame(try strip) } }
    var contentWidth: CGFloat { get throws { try #require(try strip.documentView).frame.width } }
    var offset: CGFloat { get throws { try strip.contentView.bounds.minX } }

    /// The right tile's strip's wheel redirection.
    var wheelRegion: HorizontalWheelScrolling.Region {
        get throws {
            try #require(views.compactMap { $0 as? HorizontalWheelScrolling.Region }.first { frame($0).minX > host.bounds.midX })
        }
    }

    /// A downward mouse-wheel event at `point` in the workspace. The window
    /// sits at the screen's origin, so its coordinates are the screen's.
    func wheel(at point: CGPoint) throws -> NSEvent {
        let event = try #require(CGEvent(scrollWheelEvent2Source: nil, units: .line, wheelCount: 1,
                                         wheel1: -3, wheel2: 0, wheel3: 0))
        let screen = try #require(NSScreen.screens.first).frame.height
        event.location = CGPoint(x: point.x, y: screen - (window.frame.maxY - point.y))
        return try #require(NSEvent(cgEvent: event))
    }

    /// The right tile's add button.
    var addButton: CGRect {
        get throws {
            try #require(views.first { $0 is ComposerPickerAnchorView.AnchorView && frame($0).minX > host.bounds.midX }
                .map(frame))
        }
    }
}

@MainActor @Suite(.serialized)
struct TileTabStripPresentationTests {
    private let long = (0..<10).map { "CCA-24\($0)0: Intake discharge summary for the clinic" }

    @Test func shortTabsKeepTheirWidthsAndTheAddButtonFollowsThem() async throws {
        let strip = try await TileStripHarness(titles: ["Issues", "Research", "Docs"])
        defer { strip.close() }
        let viewport = try strip.viewport
        #expect(viewport.minX == 609)
        #expect(try strip.contentWidth == viewport.width)
        #expect(try strip.offset == 0)
        #expect(try strip.addButton.minX == viewport.maxX + 6)
    }

    @Test func tabsShrinkToShareTheStrip() async throws {
        // Three tabs at 101.67 pt and the selected one at 160 fill 465 pt.
        let strip = try await TileStripHarness(titles: Array(long.prefix(4)))
        defer { strip.close() }
        let viewport = try strip.viewport
        #expect(viewport == CGRect(x: 609, y: 0, width: 465, height: 34))
        #expect(try strip.contentWidth == 465)
        #expect(try strip.addButton.minX == 1080)

        // The tile actions keep their slot when they hide, so the tabs
        // don't reflow as focus or the pointer moves.
        let left = try #require(strip.workspace.visibleTiles.first { strip.workspace.tiles[$0]?.panes == [.conversation] })
        strip.workspace.focus(tile: left)
        try await strip.settle()
        #expect(try strip.viewport == CGRect(x: 609, y: 0, width: 465, height: 34))
        #expect(try strip.addButton.minX == 1080)
    }

    @Test func crowdedTabsScrollWithoutAScrollerAndKeepTheSelectedTabInView() async throws {
        let strip = try await TileStripHarness(titles: long)
        defer { strip.close() }
        // A legacy scroller, as when the system always shows scroll bars,
        // must not appear or squeeze the tabs.
        try strip.strip.scrollerStyle = .legacy
        NotificationCenter.default.post(name: NSScroller.preferredScrollerStyleDidChangeNotification, object: nil)
        try await strip.settle()
        #expect(try strip.strip.scrollerStyle == .legacy)
        #expect(try !strip.strip.hasHorizontalScroller)
        #expect(try strip.strip.contentView.frame.height == 34)

        // Nine tabs at 88 pt and the selected last one at 160 scroll to
        // its end. The strip leaves room for the count of six hidden tabs
        // and the add button, which holds the end of the tab area.
        let count = NSHostingView(rootView: TabOverflowCount(count: 6)
            .environment(\.mica, ThemeConfiguration.decode("").theme(dark: true))).fittingSize.width
        let viewport = try strip.viewport
        #expect(viewport == CGRect(x: 609, y: 0, width: 465 - 4 - count, height: 34))
        #expect(try strip.contentWidth == 952)
        #expect(try strip.offset == 952 - viewport.width)
        #expect(try strip.addButton == CGRect(x: 1080, y: 6, width: 22, height: 22))

        // Selecting the first tab scrolls back to the start. The sixth,
        // once selected 440–600 pt in, scrolls until it clears the
        // trailing fade.
        strip.workspace.select(strip.panes[0])
        try await strip.settle()
        #expect(try strip.offset == 0)
        strip.workspace.select(strip.panes[5])
        try await strip.settle()
        #expect(try strip.offset == 600 + TileTabFade.trailing - viewport.width)

        // A mouse wheel over the strip scrolls it across; elsewhere the
        // event scrolls as it is.
        let before = try strip.offset
        let region = try strip.wheelRegion
        let over = try strip.wheel(at: CGPoint(x: 700, y: 17))
        #expect(region.redirect(over) == nil)
        let below = try strip.wheel(at: CGPoint(x: 700, y: 150))
        #expect(region.redirect(below) === below)
        try await strip.settle()
        #expect(try strip.offset > before)

        // Once the selected tab is scrolled out of view by hand, the strip
        // stays put as it narrows, until another tab is selected.
        for _ in 0..<40 where try strip.offset <= 442 {
            _ = region.redirect(try strip.wheel(at: CGPoint(x: 700, y: 17)))
            try await strip.settle(passes: 2)
        }
        let scrolled = try strip.offset
        #expect(scrolled > 442)
        strip.resize(width: 1150)
        try await strip.settle()
        #expect(try strip.viewport.width == viewport.width - 25)
        #expect(try strip.offset == scrolled)
        let narrowed = try strip.viewport
        strip.workspace.select(strip.panes[9])
        try await strip.settle()
        #expect(try strip.offset == 952 - narrowed.width)
    }
}
