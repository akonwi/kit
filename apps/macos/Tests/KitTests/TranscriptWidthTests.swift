import AppKit
import SwiftUI
import Testing
@testable import Kit

/// The Full-width transcript preference: transcript rows sit in a centred
/// 780 pt column, or fill the pane less a 32 pt gutter each side, while the
/// composer keeps its 844 pt column either way. Panes narrower than the
/// column look the same at both settings.
@MainActor struct TranscriptWidthTests {
    @Test func rowWidthsAtNarrowAndWidePanes() {
        // Narrow: the gutters bind before the column does.
        #expect(TranscriptLayout.rowWidth(pane: 700, fullWidth: false) == 636)
        #expect(TranscriptLayout.rowWidth(pane: 700, fullWidth: true) == 636)
        // Wide: the column caps, unless the transcript fills the pane.
        #expect(TranscriptLayout.rowWidth(pane: 1440, fullWidth: false) == 780)
        #expect(TranscriptLayout.rowWidth(pane: 1440, fullWidth: true) == 1376)
    }

    /// The user message card spans its row, so its painted extent is the row's.
    @Test(arguments: [
        (pane: 700.0, fullWidth: false, card: 32.0...668.0),
        (pane: 700.0, fullWidth: true, card: 32.0...668.0),
        (pane: 1440.0, fullWidth: false, card: 330.0...1110.0),
        (pane: 1440.0, fullWidth: true, card: 32.0...1408.0),
    ])
    func transcriptRowsSpanTheColumnOrThePane(pane: Double, fullWidth: Bool, card: ClosedRange<Double>) async throws {
        let theme = MicaTheme(dark: false)
        let input = NativeTranscript(messages: [
            TranscriptMessage(id: "message", role: "user", text: "Show me each width.", tools: [])
        ], hasHistory: false, historyLoading: false, historyError: nil, active: false,
           presentation: TranscriptPresentationState(), workspace: WorkspaceState(demo: false),
           resumeRequest: 0, latestOutOfView: .constant(false), loadHistory: {}, theme: theme, fullWidth: fullWidth)
        let adapter = NativeTranscriptCoordinator(input)
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: pane, height: 400))
        window.contentView = adapter.scroll
        window.orderFront(nil)
        defer { adapter.stop(); window.close() }
        adapter.receive(input)
        // The card's top edge is 18 pt down (12 row padding, 6 card margin).
        try await eventually { Self.ink(adapter.scroll, y: 24, background: theme.surface) != nil }
        let extent = try #require(Self.ink(adapter.scroll, y: 24, background: theme.surface))
        #expect(abs(extent.lowerBound - card.lowerBound) <= 1, "card \(extent)")
        #expect(abs(extent.upperBound - card.upperBound) <= 1, "card \(extent)")
    }

    /// Switching the preference re-measures rows in place: the reader keeps
    /// the row at the top of the viewport.
    @Test func switchingKeepsTheReadingPosition() async throws {
        let theme = MicaTheme(dark: false)
        let prose = String(repeating: "A long paragraph that wraps differently at each width. ", count: 30)
        func input(fullWidth: Bool) -> NativeTranscript {
            NativeTranscript(messages: (0..<12).map { TranscriptMessage(id: "m\($0)", role: "assistant", text: prose, tools: []) },
                hasHistory: false, historyLoading: false, historyError: nil, active: false,
                presentation: TranscriptPresentationState(), workspace: WorkspaceState(demo: false),
                resumeRequest: 0, latestOutOfView: .constant(false), loadHistory: {}, theme: theme, fullWidth: fullWidth)
        }
        let adapter = NativeTranscriptCoordinator(input(fullWidth: false))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 1440, height: 500))
        window.contentView = adapter.scroll
        window.orderFront(nil)
        defer { adapter.stop(); window.close() }
        adapter.receive(input(fullWidth: false))
        try await eventually { adapter.table.bounds.height > 2_000 }
        try await Task.sleep(for: .milliseconds(100))
        adapter.restore(.init(id: "message:m5", inset: 10))
        adapter.scroll.userScrolled?()
        let before = try #require(adapter.anchor())
        #expect(before.id == "message:m5")

        adapter.receive(input(fullWidth: true))
        try await Task.sleep(for: .milliseconds(200))
        let after = try #require(adapter.anchor())
        #expect(after.id == "message:m5")
        #expect(abs(after.inset - before.inset) <= 1)
    }

    /// A pane that stays wider than the reading column, as while the session
    /// sidebar is dragged, keeps its rows as they are: they only recentre. A
    /// pane that narrows the column builds and measures its rows again.
    @Test func resizingAPaneAroundTheColumnRebuildsRowsOnlyWhenTheColumnChanges() async throws {
        let theme = MicaTheme(dark: false)
        let prose = String(repeating: "A long paragraph that wraps differently at each width. ", count: 30)
        let input = NativeTranscript(messages: (0..<12).map { TranscriptMessage(id: "m\($0)", role: "assistant", text: prose, tools: []) },
            hasHistory: false, historyLoading: false, historyError: nil, active: false,
            presentation: TranscriptPresentationState(), workspace: WorkspaceState(demo: false),
            resumeRequest: 0, latestOutOfView: .constant(false), loadHistory: {}, theme: theme)
        let adapter = NativeTranscriptCoordinator(input)
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 1200, height: 500))
        window.contentView = adapter.scroll
        window.orderFront(nil)
        defer { adapter.stop(); window.close() }
        adapter.receive(input)
        try await eventually { adapter.table.bounds.height > 2_000 }
        try await Task.sleep(for: .milliseconds(100))
        func visible() -> [Int: (configuration: UUID, height: CGFloat)] {
            let range = adapter.table.rows(in: adapter.scroll.contentView.bounds)
            var rows: [Int: (configuration: UUID, height: CGFloat)] = [:]
            for row in range.location..<NSMaxRange(range) {
                if let cell = adapter.table.view(atColumn: 0, row: row, makeIfNecessary: false) as? NativeTranscriptCell {
                    rows[row] = (cell.configuration, adapter.table.rect(ofRow: row).height)
                }
            }
            return rows
        }
        let before = visible()
        try #require(!before.isEmpty)

        window.setContentSize(NSSize(width: 960, height: 500))
        adapter.scroll.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(100))
        #expect(adapter.table.bounds.width == 960)
        let wide = visible()
        #expect(wide.keys.sorted() == before.keys.sorted())
        for (row, value) in before {
            #expect(wide[row]?.configuration == value.configuration, "row \(row)")
            #expect(wide[row]?.height == value.height, "row \(row)")
        }

        // At 700 pt the column narrows to 636 pt, and the prose wraps taller.
        window.setContentSize(NSSize(width: 700, height: 500))
        let row = try #require(before.keys.min())
        try await eventually { (visible()[row]?.height ?? 0) > before[row]!.height + 20 }
        #expect(visible()[row]?.configuration != before[row]?.configuration)
    }

    /// The composer keeps its 844 pt column in a full-width transcript.
    @Test(arguments: [false, true])
    func theComposerKeepsItsColumn(fullWidth: Bool) async throws {
        let store = SessionStore(fixture: try Fixture.load())
        store.ui.workspace = WorkspaceState(demo: false)
        let theme = MicaTheme(dark: false)
        let host = NSHostingView(rootView: SessionView(state: store)
            .environment(\.mica, theme).environment(\.transcriptFullWidth, fullWidth)
            .environment(\.colorScheme, .light))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 1440, height: 700))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        func editor(_ view: NSView) -> NSTextView? {
            (view as? NSTextView).flatMap { $0.isEditable ? $0 : nil } ?? view.subviews.lazy.compactMap(editor).first
        }
        try await eventually { host.layoutSubtreeIfNeeded(); return editor(host) != nil }
        let text = try #require(editor(host))
        let row = host.convert(text.bounds, from: text).midY
        let extent = try #require(Self.ink(host, y: host.isFlipped ? row : host.bounds.height - row, background: theme.surface))
        // (1440 − 844) / 2 on each side.
        #expect(abs(extent.lowerBound - 298) <= 1, "composer \(extent)")
        #expect(abs(extent.upperBound - 1142) <= 1, "composer \(extent)")
    }

    /// The reading strip's controls end where the transcript's rows end.
    @Test(arguments: [(fullWidth: false, trailing: 1110.0), (fullWidth: true, trailing: 1408.0)])
    func theReadingStripEndsWithTheRows(fullWidth: Bool, trailing: Double) async throws {
        let theme = MicaTheme(dark: false)
        let reading = TranscriptReadingState()
        reading.location = .init(message: "m", sections: [
            .init(id: 0, title: "Overview", offset: 0), .init(id: 1, title: "Widths", offset: 200),
        ], selected: 0)
        let host = NSHostingView(rootView: TranscriptReadingNavigation(state: reading)
            .environment(\.mica, theme).environment(\.transcriptFullWidth, fullWidth))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 1440, height: 32))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        try await eventually { host.layoutSubtreeIfNeeded(); return Self.ink(host, y: 16, background: theme.surface) != nil }
        let extent = try #require(Self.ink(host, y: 16, background: theme.surface))
        // The last control is a 12 pt arrow glyph, inset slightly within its frame.
        #expect(extent.upperBound <= trailing && extent.upperBound >= trailing - 3, "strip \(extent)")
    }

    /// A Markdown preview's column follows the transcript's.
    @Test(arguments: [(fullWidth: false, block: 310.0...1130.0), (fullWidth: true, block: 24.0...1416.0)])
    func markdownPreviewsFollowTheTranscript(fullWidth: Bool, block: ClosedRange<Double>) async throws {
        let theme = MicaTheme(dark: false)
        let host = NSHostingView(rootView: MarkdownDocumentView(source: "```swift\nlet width = 1440\n```\n")
            .environment(\.mica, theme).environment(\.transcriptFullWidth, fullWidth).background(theme.surface))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 1440, height: 300))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        // The code block's card starts 20 pt down.
        try await eventually { host.layoutSubtreeIfNeeded(); return Self.ink(host, y: 30, background: theme.surface) != nil }
        let extent = try #require(Self.ink(host, y: 30, background: theme.surface))
        #expect(abs(extent.lowerBound - block.lowerBound) <= 1, "code block \(extent)")
        #expect(abs(extent.upperBound - block.upperBound) <= 1, "code block \(extent)")
    }

    /// Settings › Workspace's switch writes the app preference, and every
    /// view below `transcriptWidthPreference()` follows it at once.
    @Test func theWorkspaceSwitchSetsThePreference() async throws {
        let defaults = TestDefaults.make()
        #expect(defaults.object(forKey: TranscriptLayout.fullWidthKey) == nil)
        let probe = Probe()
        let theme = MicaTheme(dark: false)
        let host = NSHostingView(rootView: VStack(alignment: .leading, spacing: 0) {
            WorkspaceSettingsView()
            ProbeView(probe: probe)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .transcriptWidthPreference()
        .defaultAppStorage(defaults)
        .environment(\.mica, theme)
        .background(theme.surface))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 500), styleMask: [.titled])
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        try await eventually { host.layoutSubtreeIfNeeded(); return probe.values == [false] }

        // The switch is the second bordered row: click inside it, between
        // its top and bottom rules.
        let rules = Self.rules(host, x: 8, background: theme.surface)
        try #require(rules.count >= 4, "rules \(rules)")
        let point = host.convert(NSPoint(x: host.bounds.width / 2, y: (rules[2] + rules[3]) / 2), to: nil)
        for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
            window.sendEvent(try #require(NSEvent.mouseEvent(with: type, location: point, modifierFlags: [],
                timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber,
                context: nil, eventNumber: 0, clickCount: 1, pressure: type == .leftMouseDown ? 1 : 0)))
        }
        try await eventually { probe.values.last == true }
        #expect(defaults.bool(forKey: TranscriptLayout.fullWidthKey))
        #expect(probe.values == [false, true])

        // A change made elsewhere, such as the View menu, reaches it too.
        defaults.set(false, forKey: TranscriptLayout.fullWidthKey)
        try await eventually { probe.values.last == false }
    }

    @MainActor private final class Probe {
        var values: [Bool] = []
    }

    private struct ProbeView: View {
        let probe: Probe
        @Environment(\.transcriptFullWidth) private var fullWidth
        var body: some View {
            Color.clear.frame(width: 1, height: 1)
                .onChange(of: fullWidth, initial: true) { probe.values.append(fullWidth) }
        }
    }

    /// The rules crossing the vertical line `x` points from the leading edge
    /// of `view`: painted runs no taller than a hairline, in points from the
    /// top, top first.
    static func rules(_ view: NSView, x: CGFloat, background: Color) -> [CGFloat] {
        view.layoutSubtreeIfNeeded()
        guard let bitmap = view.bitmapImageRepForCachingDisplay(in: view.bounds),
              let surface = NSColor(background).usingColorSpace(.sRGB) else { return [] }
        view.cacheDisplay(in: view.bounds, to: bitmap)
        let scale = CGFloat(bitmap.pixelsWide) / view.bounds.width
        let column = Int(x * scale)
        var rules: [CGFloat] = [], start: Int?
        for y in 0...bitmap.pixelsHigh {
            let painted = y < bitmap.pixelsHigh && Self.differs(bitmap.colorAt(x: column, y: y), from: surface)
            if painted, start == nil { start = y }
            if !painted, let top = start {
                if CGFloat(y - top) <= 1.5 * scale { rules.append(CGFloat(top) / scale) }
                start = nil
            }
        }
        return rules
    }

    private static func differs(_ color: NSColor?, from surface: NSColor) -> Bool {
        guard let color = color?.usingColorSpace(.sRGB) else { return false }
        return abs(color.redComponent - surface.redComponent) > 0.02
            || abs(color.greenComponent - surface.greenComponent) > 0.02
            || abs(color.blueComponent - surface.blueComponent) > 0.02
    }

    /// The horizontal extent, in points from the leading edge, of what is
    /// painted on the line `y` points from the top of `view`.
    static func ink(_ view: NSView, y: CGFloat, background: Color) -> ClosedRange<CGFloat>? {
        view.layoutSubtreeIfNeeded()
        guard let bitmap = view.bitmapImageRepForCachingDisplay(in: view.bounds) else { return nil }
        view.cacheDisplay(in: view.bounds, to: bitmap)
        let scale = CGFloat(bitmap.pixelsWide) / view.bounds.width
        guard let surface = NSColor(background).usingColorSpace(.sRGB) else { return nil }
        let row = Int(y * scale)
        func painted(_ x: Int) -> Bool { differs(bitmap.colorAt(x: x, y: row), from: surface) }
        guard let first = (0..<bitmap.pixelsWide).first(where: painted),
              let last = (0..<bitmap.pixelsWide).last(where: painted) else { return nil }
        return CGFloat(first) / scale...CGFloat(last + 1) / scale
    }
}
