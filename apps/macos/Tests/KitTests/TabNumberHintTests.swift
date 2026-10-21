import AppKit
import SwiftUI
import Testing
@testable import Kit

struct TabNumberHintTriggerTests {
    /// Presses ⌘ alone, returning the show it schedules.
    private func arm(_ trigger: inout TabNumberHintTrigger) throws -> Int {
        let show = trigger.modifiersChanged(.command)
        return try #require(show)
    }

    @Test func holdingCommandAloneShowsHintsAfterTheDelayUntilItIsReleased() throws {
        var trigger = TabNumberHintTrigger()
        let show = try arm(&trigger)
        #expect(!trigger.shown)
        trigger.delayElapsed(show)
        #expect(trigger.shown)
        #expect(trigger.modifiersChanged([]) == nil)
        #expect(!trigger.shown)
    }

    @Test func aKeyBeforeTheDelayIsAShortcutAndShowsNothing() throws {
        var trigger = TabNumberHintTrigger()
        let show = try arm(&trigger)
        trigger.keyPressed()
        trigger.delayElapsed(show)
        #expect(!trigger.shown)
        #expect(trigger.pending == nil)
    }

    @Test func shownHintsStayThroughNumberedShortcuts() throws {
        var trigger = TabNumberHintTrigger()
        let shown = try arm(&trigger)
        trigger.delayElapsed(shown)
        trigger.keyPressed()
        trigger.keyPressed()
        #expect(trigger.shown)
    }

    @Test func anotherModifierHidesHintsAndCommandAloneRestartsTheDelay() throws {
        var trigger = TabNumberHintTrigger()
        let first = try arm(&trigger)
        trigger.delayElapsed(first)
        #expect(trigger.modifiersChanged([.command, .shift]) == nil)
        #expect(!trigger.shown)
        let second = try arm(&trigger)
        // The first delay no longer counts.
        trigger.delayElapsed(first)
        #expect(!trigger.shown)
        trigger.delayElapsed(second)
        #expect(trigger.shown)
        for other: NSEvent.ModifierFlags in [.option, .control] {
            #expect(trigger.modifiersChanged([.command, other]) == nil)
            #expect(!trigger.shown)
        }
    }

    @Test func commandAddedToAnotherModifierShowsNothing() {
        var trigger = TabNumberHintTrigger()
        #expect(trigger.modifiersChanged(.shift) == nil)
        #expect(trigger.modifiersChanged([.shift, .command]) == nil)
        #expect(trigger.pending == nil)
    }

    @Test func capsLockAndFunctionUnderCommandLeaveHintsAsTheyAre() throws {
        var trigger = TabNumberHintTrigger()
        let show = try arm(&trigger)
        #expect(trigger.modifiersChanged([.command, .capsLock]) == nil)
        #expect(trigger.pending == show)
        trigger.delayElapsed(show)
        #expect(trigger.modifiersChanged([.command, .function]) == nil)
        #expect(trigger.shown)
    }

    @Test func leavingTheWindowHidesHintsUntilCommandIsPressedAgain() throws {
        var trigger = TabNumberHintTrigger()
        let shown = try arm(&trigger)
        trigger.delayElapsed(shown)
        trigger.windowResignedKey()
        #expect(!trigger.shown)
        // Back in the window with ⌘ still down, releasing it shows nothing.
        #expect(trigger.modifiersChanged([]) == nil)
        let again = try arm(&trigger)
        trigger.delayElapsed(again)
        #expect(trigger.shown)
    }

    @Test func commandReleasedWhereTheWindowCouldNotSeeEndsHints() throws {
        var trigger = TabNumberHintTrigger()
        let shown = try arm(&trigger)
        trigger.delayElapsed(shown)
        // A menu or drag tracked its own events while ⌘ was released.
        trigger.modifiersSeen([])
        #expect(!trigger.shown)
        let pending = try arm(&trigger)
        trigger.modifiersSeen([.command, .shift])
        trigger.delayElapsed(pending)
        #expect(!trigger.shown)
    }

    @Test func commandSeenOnAnotherEventShowsNothing() throws {
        var trigger = TabNumberHintTrigger()
        let shown = try arm(&trigger)
        trigger.windowResignedKey()
        // Clicking back into the window with ⌘ still down.
        trigger.modifiersSeen(.command)
        #expect(trigger.pending == nil)
        trigger.delayElapsed(shown)
        #expect(!trigger.shown)
        // Seeing ⌘ while hints show keeps them.
        let again = try arm(&trigger)
        trigger.delayElapsed(again)
        trigger.modifiersSeen(.command)
        #expect(trigger.shown)
    }

    @Test func aPendingShowEndsWhenTheWindowStopsBeingKey() throws {
        var trigger = TabNumberHintTrigger()
        let show = try arm(&trigger)
        trigger.windowResignedKey()
        trigger.delayElapsed(show)
        #expect(!trigger.shown)
    }
}

/// A server for the window's own sessions whose watch waits until cancelled.
private actor IdleClient: SessionClient {
    nonisolated let serverID = ServerClient.localID
    nonisolated let isDemo = false
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        while !Task.isCancelled { try await Task.sleep(for: .seconds(60)) }
    }
}

@MainActor struct TabNumberHintTests {
    private func id(_ session: String) -> SessionIdentity {
        SessionIdentity(server: SessionWindowModel.defaultServer, session: session)
    }

    /// A window whose catalog knows each session's parent.
    private func model(_ sessions: [String], parents: [String: String] = [:], selected: String,
                       closed: [String] = [], collapsed: Set<String> = []) -> SessionWindowModel {
        let model = SessionWindowModel(id: UUID(), sessions: sessions.map(id), selected: id(selected), collapsed: collapsed)
        for session in (sessions + closed).reversed() {
            model.servers.local.catalog.recordCreated(SessionExcerpt(id: session, title: session.uppercased(), parentSessionID: parents[session],
                                                                     sourceTitle: "", model: "m", thinking: "high", workspace: "",
                                                                     date: "", messages: []))
        }
        return model
    }

    /// Each numbered row's title and number, in order.
    private func numbered(_ model: SessionWindowModel, _ placement: SessionTabPlacement,
                          hints: Bool = false) -> [String] {
        let numbers = hints ? model.tabNumberHints(placement) : model.shortcutNumbers(placement)
        return model.numberedRows(placement).compactMap { content in
            guard let number = numbers.number(for: content) else { return nil }
            return switch content {
            case .tab(let tab): "\(number) \(tab.title)"
            case .closedParent(_, let title): "\(number) (\(title))"
            }
        }
    }

    /// Holds ⌘ alone until the window's hints show.
    private func showHints(_ model: SessionWindowModel, delay: Duration = .zero) async throws {
        model.tabHints.delay = delay
        model.tabHints.liveModifiers = { .command }
        model.tabHints.modifiersChanged(.command)
        for _ in 0..<100 where !model.tabHints.shown { try await Task.sleep(for: .milliseconds(20)) }
        try #require(model.tabHints.shown)
    }

    @Test func sidebarNumbersFollowVisibleRowsIncludingClosedParents() {
        let model = model(["b", "p1", "p2", "c", "d", "d1", "d2"], parents: ["p1": "p", "p2": "p", "d1": "d", "d2": "d"],
                          selected: "b", closed: ["p"], collapsed: ["d"])
        #expect(numbered(model, .side) == ["1 B", "2 (P)", "3 P1", "4 P2", "5 C", "6 D"])
    }

    @Test func topNumbersFollowFamilyOrderWithoutClosedParentsOrCollapsing() {
        let model = model(["b", "p1", "p2", "c", "d", "d1", "d2"], parents: ["p1": "p", "p2": "p", "d1": "d", "d2": "d"],
                          selected: "b", closed: ["p"], collapsed: ["d"])
        #expect(numbered(model, .top) == ["1 B", "2 P1", "3 P2", "4 C", "5 D", "6 D1", "7 D2"])
    }

    @Test func onlyTheFirstNineRowsAreNumbered() {
        let model = model((0..<11).map { "s\($0)" }, selected: "s0")
        #expect(numbered(model, .side) == (0..<9).map { "\($0 + 1) S\($0)" })
        #expect(numbered(model, .top) == (0..<9).map { "\($0 + 1) S\($0)" })
    }

    @Test func numbersMatchWhatTheShortcutsSelect() throws {
        let model = model(["b", "p1", "c"], parents: ["p1": "p"], selected: "b", closed: ["p"])
        let numbers = model.shortcutNumbers(.side)
        let p1 = try #require(model.tab(for: id("p1")))
        #expect(numbers.tabs[p1.id] == 3)
        #expect(model.select(index: 2, in: .side) == nil)
        #expect(model.selectedSession == id("p1"))
        #expect(numbers.closedParents[SessionWindowModel.treeKey(id("p"))] == 2)
        #expect(model.select(index: 1, in: .side) == id("p"))
    }

    @Test func topNumbersMatchWhatTheShortcutsSelect() throws {
        let model = model(["b", "p1", "d", "d1"], parents: ["p1": "p", "d1": "d"], selected: "b", closed: ["p"], collapsed: ["d"])
        let numbers = model.shortcutNumbers(.top)
        #expect(numbers.closedParents.isEmpty)
        for (index, session) in ["b", "p1", "d", "d1"].enumerated() {
            let tab = try #require(model.tab(for: id(session)))
            #expect(numbers.tabs[tab.id] == index + 1)
            #expect(model.select(index: index, in: .top) == nil)
            #expect(model.selectedSession == id(session))
        }
    }

    @Test func hintsShowWhileCommandIsHeldAndNotUnderADialog() async throws {
        let model = model(["a", "b"], selected: "a")
        let store = try #require(model.store(for: model.selectedTab, client: IdleClient()))
        defer { model.closeAll() }
        #expect(numbered(model, .side, hints: true) == [])
        try await showHints(model)
        #expect(numbered(model, .side, hints: true) == ["1 A", "2 B"])
        model.creatingIn = model.tabs[0]
        #expect(numbered(model, .side, hints: true) == [])
        model.creatingIn = nil
        store.ui.palette = true
        #expect(numbered(model, .side, hints: true) == [])
        store.ui.palette = false
        store.ui.filePicker = true
        #expect(numbered(model, .side, hints: true) == [])
        store.ui.filePicker = false
        #expect(numbered(model, .side, hints: true) == ["1 A", "2 B"])
        model.tabHints.modifiersChanged([])
        #expect(numbered(model, .side, hints: true) == [])
    }

    @Test func hintsWaitForTheDelayAndForCommandToStillBeHeld() async throws {
        let model = model(["a", "b"], selected: "a")
        try await showHints(model, delay: .milliseconds(50))
        // ⌘ was released inside a menu before the next delay ran out.
        model.tabHints.modifiersChanged([])
        model.tabHints.modifiersChanged(.command)
        model.tabHints.liveModifiers = { [] }
        try await Task.sleep(for: .milliseconds(200))
        #expect(numbered(model, .side, hints: true) == [])
    }

    // MARK: Presentation

    /// Renders a view and returns its bitmap and backing scale.
    private func render(_ view: some View, size: NSSize, theme: MicaTheme) async throws -> (NSBitmapImageRep, CGFloat) {
        _ = NSApplication.shared
        let host = NSHostingView(rootView: view.frame(width: size.width, height: size.height, alignment: .topLeading)
            .environment(\.mica, theme).environment(\.colorScheme, theme.dark ? .dark : .light)
            .foregroundStyle(theme.text).background(theme.frame))
        host.sizingOptions = []
        let window = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        // Render in sRGB, whatever the display, so theme colors compare exactly.
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

    /// A pixel's sRGB components. The window renders in sRGB, so the
    /// bitmap's samples are sRGB whatever colorspace it is tagged with.
    private func color(_ bitmap: NSBitmapImageRep, scale: CGFloat, at point: CGPoint) throws -> [CGFloat] {
        try #require(bitmap.bitsPerSample == 8 && bitmap.samplesPerPixel >= 3)
        var pixel = [Int](repeating: 0, count: bitmap.samplesPerPixel)
        bitmap.getPixel(&pixel, atX: Int(point.x * scale), y: Int(point.y * scale))
        return pixel.prefix(3).map { CGFloat($0) / 255 }
    }

    private func expectColor(_ actual: [CGFloat], _ expected: Color, _ comment: Comment) throws {
        let color = try #require(NSColor(expected).usingColorSpace(.sRGB))
        let expected = [color.redComponent, color.greenComponent, color.blueComponent]
        let close = zip(actual, expected).allSatisfy { abs($0 - $1) < 0.02 }
        #expect(close, "\(comment): \(actual) vs \(expected)")
    }

    /// Keycaps 1 to 9 side by side, to compare the tabs' against.
    private func keycaps(_ theme: MicaTheme) async throws -> (NSBitmapImageRep, CGFloat) {
        let strip = HStack(spacing: 0) { ForEach(1...9, id: \.self) { TabNumberHint(number: $0) } }
        return try await render(strip, size: NSSize(width: TabNumberHint.size * 9, height: TabNumberHint.size), theme: theme)
    }

    /// A keycap's stroke on its left edge and surface fill beside it, at
    /// mid-height, and its face, digit included, matching keycap `number`.
    private func expectKeycap(_ bitmap: NSBitmapImageRep, scale: CGFloat, at origin: CGPoint, number: Int,
                              keycaps: (NSBitmapImageRep, CGFloat), theme: MicaTheme, _ label: String) throws {
        let mid = origin.y + TabNumberHint.size / 2
        try expectColor(color(bitmap, scale: scale, at: CGPoint(x: origin.x + 0.5, y: mid)), theme.strongBorder, "\(label) stroke")
        try expectColor(color(bitmap, scale: scale, at: CGPoint(x: origin.x + 2.5, y: mid)), theme.surface, "\(label) fill")
        let reference = CGPoint(x: CGFloat(number - 1) * TabNumberHint.size, y: 0)
        let face = Int(2 * scale)..<Int((TabNumberHint.size - 2) * scale)
        var differing = 0
        for dy in face {
            for dx in face {
                let offset = CGPoint(x: CGFloat(dx) / scale, y: CGFloat(dy) / scale)
                let actual = try color(bitmap, scale: scale, at: CGPoint(x: origin.x + offset.x, y: origin.y + offset.y))
                let expected = try color(keycaps.0, scale: keycaps.1, at: CGPoint(x: reference.x + offset.x, y: reference.y + offset.y))
                if zip(actual, expected).contains(where: { abs($0 - $1) > 0.05 }) { differing += 1 }
            }
        }
        #expect(differing == 0, "\(label) shows \(number): \(differing) pixels differ")
    }

    @Test func sidebarHintsLineUpInTheTrailingActionSlot() async throws {
        for dark in [false, true] {
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let model = model((0..<10).map { "s\($0)" }, selected: "s0")
            try await showHints(model)
            let size = NSSize(width: 248, height: 560)
            let (bitmap, scale) = try await render(SessionSidebar(model: model, placement: .constant(.side)), size: size, theme: theme)
            let keycaps = try await keycaps(theme)
            // Lone tabs are 32 pt rows 8 pt apart, below the 44 pt title row
            // and 8 pt of padding. The 16 pt keycap is centred in the 18 pt
            // slot at the row's 4 pt trailing inset, inside 8 pt of sidebar
            // padding: x 219–235, and y 8 pt into each row.
            for row in 0..<9 {
                let origin = CGPoint(x: 219, y: 52 + CGFloat(row) * 40 + 8)
                try expectKeycap(bitmap, scale: scale, at: origin, number: row + 1, keycaps: keycaps, theme: theme, "row \(row + 1)")
            }
            // The tenth row has no shortcut; its slot shows the sidebar.
            try expectColor(color(bitmap, scale: scale, at: CGPoint(x: 219.5, y: 52 + 9 * 40 + 16)), theme.frame, "row 10 slot")
        }
    }

    @Test func topTabHintsTakeTheCloseButtonsPlace() async throws {
        for dark in [false, true] {
            let theme = ThemeConfiguration.decode("").theme(dark: dark)
            let model = model(["a", "b", "c", "d"], selected: "a")
            try await showHints(model)
            let size = NSSize(width: 1200, height: SessionChrome.rowHeight)
            let (bitmap, scale) = try await render(SessionTopTabs(model: model, placement: .constant(.top)), size: size, theme: theme)
            let keycaps = try await keycaps(theme)
            // Tabs are 220 pt wide and 2 pt apart, after 16 pt of padding, the
            // 68 pt traffic-light space and 6 pt. Each keycap ends at the tab's
            // 10 pt trailing padding and is centred in the 44 pt row.
            for index in 0..<4 {
                let origin = CGPoint(x: 90 + CGFloat(index) * 222 + 194, y: 14)
                try expectKeycap(bitmap, scale: scale, at: origin, number: index + 1, keycaps: keycaps, theme: theme, "tab \(index + 1)")
            }
        }
    }
}
