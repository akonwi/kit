import AppKit
import SwiftUI
import Testing
@testable import Kit

@MainActor private func id(_ session: String) -> SessionIdentity {
    SessionIdentity(server: SessionWindowModel.defaultServer, session: session)
}

private func temporaryURL() -> URL {
    FileManager.default.temporaryDirectory.appendingPathComponent("kit-sidebar-\(UUID().uuidString)/windows.json")
}

/// Preferences of their own, so tests don't share the last sidebar set.
private func scratchDefaults() -> SessionSidebarDefaults {
    SessionSidebarDefaults(defaults: TestDefaults.make())
}

/// The sidebar's width limits, and how a drag of its edge ends.
@MainActor struct SessionSidebarWidthTests {
    @Test func widthsStayWithinTheLimits() {
        #expect(SessionSidebarLayout.clamped(150) == 200)
        #expect(SessionSidebarLayout.clamped(200) == 200)
        #expect(SessionSidebarLayout.clamped(321) == 321)
        #expect(SessionSidebarLayout.clamped(400) == 400)
        #expect(SessionSidebarLayout.clamped(900) == 400)
        #expect(SessionSidebarLayout.clamped(.nan) == 248)
        #expect(SessionSidebarLayout.clamped(.infinity) == 248)
        #expect(SessionSidebarLayout() == SessionSidebarLayout(width: 248, hidden: false))
        #expect(SessionSidebarLayout(width: 1000).width == 400)
    }

    @Test func draggingTheEdgeFollowsThePointerWithinTheLimitsAndIsRememberedOnRelease() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"))
        var remembered: [SessionSidebarLayout] = []
        var changes = 0
        model.onSidebarChange = { remembered.append($0) }
        model.onChange = { _ in changes += 1 }
        // The edge moves in whole points.
        model.dragSidebar(to: 300.4)
        #expect(model.sidebar == SessionSidebarLayout(width: 300, hidden: false))
        model.dragSidebar(to: 520)
        #expect(model.sidebar.width == 400)
        model.dragSidebar(to: 140)
        #expect(model.sidebar.width == 200)
        // Steps of a drag aren't remembered; letting go is, once.
        #expect(remembered.isEmpty)
        #expect(changes == 0)
        // Letting go short of the minimum, but not of the hiding width, stops at the minimum.
        model.endSidebarDrag(at: 140, from: 248)
        #expect(model.sidebar == SessionSidebarLayout(width: 200, hidden: false))
        #expect(remembered == [SessionSidebarLayout(width: 200, hidden: false)])
        #expect(changes == 1)
    }

    @Test func lettingGoUnderTheHidingWidthHidesTheSidebarAtItsEarlierWidth() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"),
                                       sidebar: SessionSidebarLayout(width: 320))
        var remembered: [SessionSidebarLayout] = []
        model.onSidebarChange = { remembered.append($0) }
        model.dragSidebar(to: 100)
        #expect(model.sidebar.width == 200)
        model.endSidebarDrag(at: 119, from: 320)
        #expect(model.sidebar == SessionSidebarLayout(width: 320, hidden: true))
        #expect(remembered == [SessionSidebarLayout(width: 320, hidden: true)])
        #expect(model.sidebarFocusHandoffs == 1)
        // Showing it again brings back that width.
        model.toggleSidebar()
        #expect(model.sidebar == SessionSidebarLayout(width: 320, hidden: false))
    }

    @Test func doubleClickAndAssistiveAdjustmentsSetTheWidthWithinTheLimits() {
        let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"),
                                       sidebar: SessionSidebarLayout(width: 390))
        model.setSidebarWidth(model.sidebar.width + SessionSidebarLayout.accessibilityStep)
        #expect(model.sidebar.width == 400)
        model.setSidebarWidth(SessionSidebarLayout.defaultWidth)
        #expect(model.sidebar.width == 248)
        model.setSidebarWidth(model.sidebar.width - 3 * SessionSidebarLayout.accessibilityStep)
        #expect(model.sidebar.width == 200)
        #expect(SessionSidebarResizeHandle.accessibilityValue(248) == "248 points")
        #expect(SessionSidebarResizeHandle.accessibilityValue(200.4) == "200 points")
    }
}

/// Each window keeps its own sidebar; a new window starts with the last one set.
@MainActor struct SessionSidebarRestorationTests {
    @Test func aWindowsSidebarIsRestoredWithIt() throws {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let defaults = scratchDefaults()
        let request = SessionWindowRequest(windowID: UUID())
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url), sidebarDefaults: defaults)
        let model = registry.model(for: request)
        model.show(id("a"), in: model.tabs[0])
        #expect(model.sidebar == SessionSidebarLayout())
        model.setSidebarWidth(332)
        model.setSidebarHidden(true)
        #expect(registry.restoration.window(request.windowID)?.sidebar == SessionSidebarLayout(width: 332, hidden: true))

        let saved = try String(contentsOf: url, encoding: .utf8)
        #expect(saved.contains(#""sidebar":{"hidden":true,"width":332}"#) || saved.contains(#""sidebar":{"width":332,"hidden":true}"#))

        // Another launch restores the window's own sidebar, not the last one set.
        defaults.save(SessionSidebarLayout(width: 210, hidden: false))
        let next = SessionWindowRegistry(restoration: WindowRestorationStore(url: url), sidebarDefaults: defaults)
        #expect(next.model(for: request).sidebar == SessionSidebarLayout(width: 332, hidden: true))
    }

    @Test func aNewWindowStartsWithTheSidebarLastSetInAnyWindow() {
        let defaults = scratchDefaults()
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: temporaryURL()), sidebarDefaults: defaults)
        #expect(registry.model(for: SessionWindowRequest(windowID: UUID())).sidebar == SessionSidebarLayout())
        let first = registry.model(for: SessionWindowRequest(windowID: UUID()))
        let second = registry.model(for: SessionWindowRequest(windowID: UUID()))
        first.setSidebarWidth(360)
        second.setSidebarHidden(true)
        // Each window keeps its own.
        #expect(first.sidebar == SessionSidebarLayout(width: 360, hidden: false))
        #expect(second.sidebar == SessionSidebarLayout(width: 248, hidden: true))
        // A new window takes the layout set last, in the second window.
        #expect(defaults.load() == SessionSidebarLayout(width: 248, hidden: true))
        #expect(registry.model(for: SessionWindowRequest(windowID: UUID())).sidebar == SessionSidebarLayout(width: 248, hidden: true))
        // A drag still under way isn't a layout set.
        first.dragSidebar(to: 280)
        #expect(defaults.load() == SessionSidebarLayout(width: 248, hidden: true))
        first.endSidebarDrag(at: 280, from: 360)
        #expect(defaults.load() == SessionSidebarLayout(width: 280, hidden: false))
    }

    @Test func windowsSavedBeforeTheSidebarCouldChangeStartLikeNewOnes() throws {
        let url = temporaryURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let window = UUID()
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(#"{"version":2,"windows":[{"id":"\#(window.uuidString)","sessions":[{"server":"local","session":"a"}]}]}"#.utf8)
            .write(to: url)
        let defaults = scratchDefaults()
        defaults.save(SessionSidebarLayout(width: 300, hidden: true))
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url), sidebarDefaults: defaults)
        #expect(registry.restoration.window(window)?.sidebar == nil)
        #expect(registry.model(for: SessionWindowRequest(windowID: window)).sidebar == SessionSidebarLayout(width: 300, hidden: true))
    }

    @Test func savedWidthsOutsideTheLimitsAreClampedAndUnreadableOnesDropped() throws {
        let decoder = JSONDecoder()
        let wide = try decoder.decode(WindowRestorationStore.Window.self, from: Data(
            #"{"id":"\#(UUID().uuidString)","sessions":[],"sidebar":{"width":9000,"hidden":false}}"#.utf8))
        #expect(wide.sidebar == SessionSidebarLayout(width: 400, hidden: false))
        let broken = try decoder.decode(WindowRestorationStore.Window.self, from: Data(
            #"{"id":"\#(UUID().uuidString)","sessions":[],"sidebar":"wide"}"#.utf8))
        #expect(broken.sidebar == nil)
        let defaults = scratchDefaults()
        defaults.defaults.set(12.0, forKey: SessionSidebarDefaults.widthKey)
        #expect(defaults.load() == SessionSidebarLayout(width: 200, hidden: false))
    }
}

/// View › Hide Sidebar and the toggle beside the traffic lights.
@MainActor struct SessionSidebarToggleTests {
    @Test func theMenuItemHidesAndShowsTheFocusedWindowsSidebarWithCommandS() {
        #expect(SessionSidebarCommand.shortcut.key == KeyEquivalent("s"))
        #expect(SessionSidebarCommand.shortcut.modifiers == .command)
        #expect(SessionSidebarCommand.title(hidden: false) == "Hide Sidebar")
        #expect(SessionSidebarCommand.title(hidden: true) == "Show Sidebar")
        let model = SessionWindowModel(id: UUID(), sessions: [id("a")], selected: id("a"))
        #expect(SessionSidebarCommand.enabled(window: model, placement: .side))
        // On Top has no sidebar to hide; without a window there's nothing to act on.
        #expect(!SessionSidebarCommand.enabled(window: model, placement: .top))
        #expect(!SessionSidebarCommand.enabled(window: nil, placement: .side))

        var remembered: [SessionSidebarLayout] = []
        model.onSidebarChange = { remembered.append($0) }
        model.toggleSidebar()
        #expect(model.sidebar.hidden)
        #expect(model.sidebarFocusHandoffs == 1)
        model.toggleSidebar()
        #expect(!model.sidebar.hidden)
        // Showing it leaves the keyboard where it is.
        #expect(model.sidebarFocusHandoffs == 1)
        // Asking for the state it's already in changes nothing.
        model.setSidebarHidden(false)
        #expect(remembered == [SessionSidebarLayout(width: 248, hidden: true), SessionSidebarLayout(width: 248, hidden: false)])
    }

    @Test func theHiddenToggleMarksATabWaitingForAnAnswerOtherThanTheSelectedOne() {
        let selected = UUID(), other = UUID()
        #expect(SessionWindowModel.needsAnswer([(selected, .idle), (other, .awaitingResponse)], selected: selected))
        #expect(!SessionWindowModel.needsAnswer([(selected, .awaitingResponse), (other, .running)], selected: selected))
        #expect(!SessionWindowModel.needsAnswer([(selected, .idle), (other, .idle)], selected: selected))
    }
}

/// The hidden sidebar shows over the tiles while its toggle is hovered.
@MainActor struct SessionSidebarPeekTests {
    @Test func restingOnTheToggleOpensThePeekWhichStaysWhileThePointerIsOnTheSidebar() async throws {
        let delay = ManualSleeper()
        let peek = SessionSidebarPeek(sleep: delay.sleep)
        peek.pointer(onToggle: true)
        try await delay.waitForSleeps(1)
        #expect(!peek.showing)
        delay.elapse()
        try await eventually { peek.showing }
        // From the toggle onto the sidebar, which covers it.
        peek.pointer(onToggle: false)
        try await delay.waitForSleeps(2)
        peek.pointer(onSidebar: true)
        #expect(!peek.isPending)
        #expect(peek.showing)
        // Off the sidebar, it closes once the delay passes.
        peek.pointer(onSidebar: false)
        try await delay.waitForSleeps(3)
        #expect(peek.showing)
        delay.elapse()
        try await eventually { !peek.showing }
        #expect(delay.durations == Array(repeating: SessionSidebarPeek.delay, count: 3))
    }

    @Test func passingOverTheToggleDoesntOpenIt() async throws {
        let delay = ManualSleeper()
        let peek = SessionSidebarPeek(sleep: delay.sleep)
        peek.pointer(onToggle: true)
        try await delay.waitForSleeps(1)
        peek.pointer(onToggle: false)
        #expect(!peek.isPending)
        delay.elapse()
        // The sidebar alone doesn't open it.
        peek.pointer(onSidebar: true)
        #expect(!peek.isPending)
        #expect(!peek.showing)
        #expect(delay.cancelled == 1)
    }

    @Test func aCommandHoldOpensAndClosesThePeekAtOnce() async throws {
        let delay = ManualSleeper()
        let peek = SessionSidebarPeek(sleep: delay.sleep)
        peek.command(held: true)
        #expect(peek.showing)
        #expect(!peek.isPending)
        peek.command(held: false)
        #expect(!peek.showing)
        // A hold that ends with the pointer on the sidebar leaves it to the pointer.
        peek.command(held: true)
        peek.pointer(onSidebar: true)
        peek.command(held: false)
        #expect(peek.showing)
        peek.pointer(onSidebar: false)
        try await delay.waitForSleeps(1)
        delay.elapse()
        try await eventually { !peek.showing }
        // Closing forgets the hold.
        peek.command(held: true)
        peek.close()
        peek.pointer(onToggle: true)
        peek.pointer(onToggle: false)
        #expect(!peek.showing)
    }

    @Test func choosingATabDockingOrEscClosesThePeek() async throws {
        let delay = ManualSleeper()
        let model = SessionWindowModel(id: UUID(), sessions: [id("a"), id("b")], selected: id("a"),
                                       sidebar: SessionSidebarLayout(hidden: true), sleep: delay.sleep)
        func open(_ sleeps: Int) async throws {
            model.sidebarPeek.pointer(onToggle: true)
            try await delay.waitForSleeps(sleeps)
            delay.elapse()
            try await eventually { model.sidebarPeek.showing }
            model.sidebarPeek.pointer(onToggle: false)
            model.sidebarPeek.pointer(onSidebar: true)
        }
        try await open(1)
        model.select(model.tabs[1])
        #expect(!model.sidebarPeek.showing)
        try await open(3)
        model.sidebarPeek.close()
        #expect(!model.sidebarPeek.showing)
        try await open(5)
        // Clicking the peeking sidebar's toggle docks it.
        model.toggleSidebar()
        #expect(!model.sidebarPeek.showing)
        #expect(!model.sidebar.hidden)
    }
}

/// Where the sidebar's controls and rows sit: rows by the frames the
/// sidebar records for dragging, controls by where their glyphs draw.
@MainActor struct SessionSidebarPresentationTests {
    private let theme = ThemeConfiguration.decode("").theme(dark: true)

    private func render(_ view: some View, size: NSSize) async throws -> (NSBitmapImageRep, CGFloat) {
        _ = NSApplication.shared
        let host = NSHostingView(rootView: view.frame(width: size.width, height: size.height, alignment: .topLeading)
            .environment(\.mica, theme).environment(\.colorScheme, .dark)
            .foregroundStyle(theme.text).background(theme.frame))
        host.sizingOptions = []
        let window = NSWindow(testContentRect: NSRect(origin: .zero, size: size))
        window.colorSpace = .sRGB
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        for _ in 0..<10 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }
        try #require(host.bounds.size == size)
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        return (bitmap, CGFloat(bitmap.pixelsWide) / size.width)
    }

    /// The horizontal spans, in points, of columns between `minX` and `maxX`
    /// where anything other than the frame colour draws in the band from
    /// `top` to `bottom`. Spans closer than 3 pt join.
    private func ink(_ bitmap: NSBitmapImageRep, scale: CGFloat, x minX: CGFloat, _ maxX: CGFloat,
                     y top: CGFloat, _ bottom: CGFloat) throws -> [ClosedRange<CGFloat>] {
        let frame = try #require(NSColor(theme.frame).usingColorSpace(.sRGB))
        let background = [frame.redComponent, frame.greenComponent, frame.blueComponent]
        var columns: [CGFloat] = []
        for px in Int(minX * scale)..<Int(maxX * scale) {
            let inked = (Int(top * scale)..<Int(bottom * scale)).contains { py in
                var pixel = [Int](repeating: 0, count: bitmap.samplesPerPixel)
                bitmap.getPixel(&pixel, atX: px, y: py)
                return zip(pixel.prefix(3), background).contains { abs(CGFloat($0) / 255 - $1) > 0.08 }
            }
            if inked { columns.append(CGFloat(px) / scale) }
        }
        var spans: [ClosedRange<CGFloat>] = []
        for x in columns {
            if let last = spans.last, x - last.upperBound <= 3 {
                spans[spans.count - 1] = last.lowerBound...x
            } else {
                spans.append(x...x)
            }
        }
        return spans
    }

    private func center(_ span: ClosedRange<CGFloat>?) -> CGFloat? {
        span.map { (($0.lowerBound + $0.upperBound) / 2).rounded() }
    }

    /// A family three levels deep, a lone session, and long titles.
    private func model() -> SessionWindowModel {
        let titles = ["a": "Tiling the native shell for every window", "a1": "Review the tiling diff before merging",
                      "a2": "Ghostty build flags for release", "b": "Release notes"]
        let parents = ["a1": "a", "a2": "a1"]
        let sessions = ["a", "a1", "a2", "b"]
        let model = SessionWindowModel(id: UUID(), sessions: sessions.map { id($0) }, selected: id("b"),
                                       sidebar: SessionSidebarLayout(width: SessionSidebarLayout.minimumWidth))
        for session in sessions.reversed() {
            model.servers.local.catalog.recordCreated(SessionExcerpt(id: session, title: titles[session]!, parentSessionID: parents[session],
                                                                     sourceTitle: "", model: "m", thinking: "high", workspace: "",
                                                                     date: "", messages: []))
        }
        return model
    }

    @Test func atTheMinimumWidthEveryRowKeepsItsPlaceAndControlsAndTitlesShorten() async throws {
        let model = model()
        let drag = SessionTreeDrag()
        let (bitmap, scale) = try await render(SessionSidebar(model: model, placement: .constant(.side), drag: drag)
            .frame(width: model.sidebar.width), size: NSSize(width: 200, height: 400))
        // Rows fill the 184 pt between the 8 pt insets, 32 pt in per level:
        // 32 pt rows, 2 pt apart within a family and 8 pt between families.
        let rows = model.rows
        #expect(rows.map(\.depth) == [0, 1, 2, 0])
        #expect(rows.map { drag.frames[$0.id] } == [
            CGRect(x: 0, y: 0, width: 184, height: 32),
            CGRect(x: 32, y: 34, width: 152, height: 32),
            CGRect(x: 64, y: 68, width: 120, height: 32),
            CGRect(x: 0, y: 108, width: 184, height: 32),
        ])
        // The rows start 8 pt below the 44 pt header and 8 pt in. The first
        // row's title shortens to leave its caret's 18 pt slot 4 pt from the
        // row's end, and the 10 pt between them, clear.
        let top: CGFloat = 52
        let first = try ink(bitmap, scale: scale, x: 8, 192, y: top + 9, top + 23)
        #expect(first.count >= 3)
        #expect(try #require(first.last).upperBound <= 188)
        #expect(center(first.last) == 179)
        #expect(try #require(first.dropLast().last).upperBound < 160)
        // The third-level row's title starts 32 + 32 + 8 + 14 + 10 pt in
        // and shortens before the row's 4 pt trailing padding.
        let deep = try ink(bitmap, scale: scale, x: 72, 192, y: top + 68 + 9, top + 68 + 23)
        #expect(try #require(deep.first).lowerBound >= 104)
        #expect(try #require(deep.last).upperBound <= 188)
    }

    /// The side layout with a 10 pt marker where the title row's content
    /// starts and blue tiles.
    private func layout(_ model: SessionWindowModel) -> some View {
        SessionSideLayout(model: model, placement: .constant(.side)) {
            Rectangle().fill(.red).frame(width: 10, height: 10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .frame(height: SessionChrome.rowHeight)
        } tiles: {
            Rectangle().fill(.blue)
        }
    }

    @Test func theToggleKeepsItsPlaceAndNewTabTheTitleAndTilesFollowTheSidebar() async throws {
        let model = model()
        let size = NSSize(width: 600, height: 400)
        let (shown, scale) = try await render(layout(model), size: size)
        // The toggle's glyph centred in its 26 pt button 90 pt in, after the
        // traffic lights; New tab 16 pt in from the sidebar's 200 pt edge;
        // the title row and tiles from that edge.
        #expect(try ink(shown, scale: scale, x: 80, 260, y: 9, 35).map(center) == [103, 171, 205])
        #expect(try ink(shown, scale: scale, x: 200, 260, y: 9, 35).first?.lowerBound == 200)
        #expect(try ink(shown, scale: scale, x: 0, 600, y: 300, 310) == [200...(600 - 1 / scale)])

        // Hidden: New tab follows the toggle 6 pt on, the title row starts
        // after it, and the tiles keep a 6 pt gap from the window's edge.
        model.setSidebarHidden(true)
        let (hidden, _) = try await render(layout(model), size: size)
        #expect(try ink(hidden, scale: scale, x: 80, 260, y: 9, 35).map(center) == [103, 135, 153])
        #expect(try ink(hidden, scale: scale, x: 140, 260, y: 9, 35).last?.lowerBound == 148)
        #expect(try ink(hidden, scale: scale, x: 0, 600, y: 300, 310) == [6...(600 - 1 / scale)])
    }
}
