import AppKit
@preconcurrency import CodeEditSourceEditor
import CodeEditTextView
import SwiftUI
import Testing
@testable import Kit

/// Kit's scrollers. Under legacy scroll bars, as with a mouse attached,
/// the gutter is the scroll view's own background and the knob is a pill
/// in the theme's muted text colour: 6 pt thick, 3 pt from the gutter's
/// far edge, widening to 9 pt under the pointer.
@MainActor struct KitScrollerTests {
    /// The transcript's gutter is its background from top to bottom, but
    /// for the knob, which rests in the theme's colours.
    @Test(arguments: [false, true])
    func theTranscriptsLegacyGutterIsItsBackgroundWithAThemedKnob(dark: Bool) async throws {
        let theme = MicaTheme(dark: dark)
        let (adapter, window) = try await Self.transcript(theme)
        defer { adapter.stop(); window.close() }
        let scroll = adapter.scroll
        let scroller = try #require(scroll.verticalScroller as? KitScroller)
        #expect(scroller.scrollerStyle == .legacy)
        let width = scroll.bounds.width
        let gutter = width - scroller.frame.width...width

        let image = Self.snapshot(scroll, holding: scroll)
        let knob = try #require(image.run(x: width - 6, background: theme.surface), "knob")
        let expected = Self.knobSpan(scroller, in: scroll)
        #expect(abs(knob.lowerBound - expected.lowerBound) <= 1 && abs(knob.upperBound - expected.upperBound) <= 1,
                "knob \(knob), expected \(expected)")
        // Across the knob's middle: the pill against the far edge, on the
        // background; above it, the background all the way across.
        let middle = (knob.lowerBound + knob.upperBound) / 2
        #expect(image.row(y: middle, in: gutter, background: theme.surface) == width - 9...width - 3)
        #expect(Self.same(image.color(x: width - 6, y: middle), theme.muted.mix(with: theme.surface, by: 0.45)))
        #expect(image.row(y: knob.lowerBound - 20, in: gutter, background: theme.surface) == nil)
        #expect(Self.same(image.color(x: gutter.lowerBound + 0.5, y: knob.lowerBound - 20), theme.surface))

        // Scrolled to the top, the knob starts 3 pt below the gutter's top.
        adapter.restore(.init(id: "message:m0", inset: 0))
        scroll.userScrolled?()
        try await eventually { abs((Self.snapshot(scroll, holding: scroll).run(x: width - 6, background: theme.surface)?.lowerBound ?? 0) - KitScroller.edgeInset) <= 0.5 }
        let top = try #require(Self.snapshot(scroll, holding: scroll).run(x: width - 6, background: theme.surface))
        let span = Self.knobSpan(scroller, in: scroll)
        #expect(abs(top.upperBound - span.upperBound) <= 1, "knob \(top), expected \(span)")
    }

    /// Comment marks on words stay in the reading column: the gutter beside
    /// the knob is the background from top to bottom.
    @Test(arguments: [false, true])
    func commentMarksStayOutOfTheGutter(dark: Bool) async throws {
        let theme = MicaTheme(dark: dark)
        let workspace = WorkspaceState(demo: false)
        let prose = String(repeating: "A long paragraph that wraps differently at each width. ", count: 30)
        for id in 0..<12 {
            workspace.textComments.add(comment: "Wide.", source: "Transcript",
                                       quote: "A long paragraph that wraps differently at each width.", scope: "message:m\(id)")
        }
        let (adapter, window) = try await Self.transcript(theme, workspace: workspace)
        defer { adapter.stop(); window.close() }
        let scroll = adapter.scroll
        let scroller = try #require(scroll.verticalScroller as? KitScroller)
        let width = scroll.bounds.width
        // The marks' accent wash is drawn on the rows, to the column's end.
        let end = (width + TranscriptLayout.rowWidth(pane: width, fullWidth: false)) / 2
        try await eventually {
            let image = Self.snapshot(scroll, holding: scroll)
            return stride(from: 10.0, to: scroll.bounds.height - 10, by: 2).contains { y in
                guard let color = image.color(x: end - 4, y: y)?.usingColorSpace(.sRGB) else { return false }
                return color.blueComponent - color.redComponent > 0.04
            }
        }
        let image = Self.snapshot(scroll, holding: scroll)
        // Down the gutter, from the scroller's leading edge to the knob.
        for x in stride(from: width - scroller.frame.width + 0.5, to: width - 9, by: 1) {
            #expect(image.run(x: x, background: theme.surface) == nil, "gutter at \(x)")
        }
        #expect(image.run(x: width - 6, background: theme.surface) != nil)
    }

    /// The knob widens towards the content under the pointer and takes the
    /// muted colour at full strength, then rests again when it leaves.
    @Test(arguments: [false, true])
    func theKnobWidensUnderThePointer(dark: Bool) async throws {
        let theme = MicaTheme(dark: dark)
        let (adapter, window) = try await Self.transcript(theme)
        defer { adapter.stop(); window.close() }
        let scroll = adapter.scroll
        let scroller = try #require(scroll.verticalScroller as? KitScroller)
        let width = scroll.bounds.width
        let gutter = width - scroller.frame.width...width
        let middle = { () -> CGFloat in let span = Self.knobSpan(scroller, in: scroll); return (span.lowerBound + span.upperBound) / 2 }

        scroller.mouseEntered(with: try Self.crossing(.mouseEntered, window))
        try await eventually { Self.snapshot(scroll, holding: scroll).row(y: middle(), in: gutter, background: theme.surface) == width - 12...width - 3 }
        #expect(Self.same(Self.snapshot(scroll, holding: scroll).color(x: width - 6, y: middle()), theme.muted))

        scroller.mouseExited(with: try Self.crossing(.mouseExited, window))
        try await eventually { Self.snapshot(scroll, holding: scroll).row(y: middle(), in: gutter, background: theme.surface) == width - 9...width - 3 }
        #expect(Self.same(Self.snapshot(scroll, holding: scroll).color(x: width - 6, y: middle()), theme.muted.mix(with: theme.surface, by: 0.45)))
    }

    /// With Increase Contrast the resting knob is the muted colour at full
    /// strength, and the primary text colour under the pointer.
    @Test(arguments: [false, true])
    func increaseContrastStrengthensTheKnob(dark: Bool) async throws {
        let theme = MicaTheme(dark: dark)
        let (adapter, window) = try await Self.transcript(theme)
        defer { adapter.stop(); window.close() }
        let scroll = adapter.scroll
        let scroller = try #require(scroll.verticalScroller as? KitScroller)
        scroller.increasesContrast = { true }
        let width = scroll.bounds.width
        let middle = { () -> CGFloat in let span = Self.knobSpan(scroller, in: scroll); return (span.lowerBound + span.upperBound) / 2 }
        try await eventually { Self.same(Self.snapshot(scroll, holding: scroll).color(x: width - 6, y: middle()), theme.muted) }

        scroller.mouseEntered(with: try Self.crossing(.mouseEntered, window))
        try await eventually { Self.same(Self.snapshot(scroll, holding: scroll).color(x: width - 6, y: middle()), theme.text) }
    }

    /// A user theme's tokens colour the knob and the gutter.
    @Test func aUserThemesTokensColourTheKnob() async throws {
        let theme = MicaTheme(dark: true, overrides: ["bg": "#1d2433", "textMuted": "#8fa1c7"])
        let (adapter, window) = try await Self.transcript(theme)
        defer { adapter.stop(); window.close() }
        let scroll = adapter.scroll
        let scroller = try #require(scroll.verticalScroller as? KitScroller)
        let width = scroll.bounds.width
        let image = Self.snapshot(scroll, holding: scroll)
        let knob = try #require(image.run(x: width - 6, background: theme.surface), "knob")
        let middle = (knob.lowerBound + knob.upperBound) / 2
        #expect(Self.same(image.color(x: width - 6, y: middle), theme.muted.mix(with: theme.surface, by: 0.45)))
        #expect(Self.same(image.color(x: width - scroller.frame.width + 0.5, y: middle), theme.surface))
    }

    /// A transcript that changes theme recolours its knob.
    @Test func changingThemeRecoloursTheKnob() async throws {
        let light = MicaTheme(dark: false)
        let (adapter, window) = try await Self.transcript(light)
        defer { adapter.stop(); window.close() }
        let dark = MicaTheme(dark: true)
        adapter.receive(Self.input(dark))
        let scroll = adapter.scroll
        let scroller = try #require(scroll.verticalScroller as? KitScroller)
        #expect(scroller.theme == dark)
        let width = scroll.bounds.width
        let middle = { () -> CGFloat in let span = Self.knobSpan(scroller, in: scroll); return (span.lowerBound + span.upperBound) / 2 }
        try await eventually { Self.same(Self.snapshot(scroll, holding: scroll).color(x: width - 6, y: middle()), dark.muted.mix(with: dark.surface, by: 0.45)) }
    }

    /// Overlay scrollers stay the system's own: the scroller takes no width
    /// from the transcript, and it draws through the system's overlay style.
    @Test func overlayScrollersStayTheSystems() async throws {
        let theme = MicaTheme(dark: true)
        let (adapter, window) = try await Self.transcript(theme, style: .overlay)
        defer { adapter.stop(); window.close() }
        let scroller = try #require(adapter.scroll.verticalScroller as? KitScroller)
        #expect(scroller.scrollerStyle == .overlay)
        #expect(adapter.scroll.contentSize.width == adapter.scroll.frame.width)
        #expect(KitScroller.isCompatibleWithOverlayScrollers)
        // Shown, the system's overlay knob is drawn in its layers: a 6 pt
        // pill 3 pt from the edge. The pointer leaves it as the system has it.
        scroller.mouseEntered(with: try Self.crossing(.mouseEntered, window))
        #expect(scroller.expansion == 0)
        adapter.scroll.flashScrollers()
        try await eventually {
            guard let layer = scroller.layer else { return false }
            layer.sublayers?.forEach { $0.opacity = 1 }
            let scale: CGFloat = 2
            let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(scroller.bounds.width * scale),
                pixelsHigh: Int(scroller.bounds.height * scale), bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true,
                isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
            let context = NSGraphicsContext(bitmapImageRep: bitmap)!.cgContext
            context.scaleBy(x: scale, y: scale)
            layer.render(in: context)
            let knob = Self.knobSpan(scroller, in: scroller)
            let row = Int((knob.lowerBound + knob.upperBound) / 2 * scale)
            let y = bitmap.pixelsHigh - 1 - row
            let painted = (0..<bitmap.pixelsWide).filter { (bitmap.colorAt(x: $0, y: y)?.alphaComponent ?? 0) > 0.5 }
            guard let first = painted.first, let last = painted.last else { return false }
            return abs(CGFloat(first) / scale - (scroller.bounds.width - 9)) <= 1 && abs(CGFloat(last + 1) / scale - (scroller.bounds.width - 3)) <= 1
        }
    }

    /// A scroll view given Kit's scrollers after it has laid out shows the
    /// knob at once, where it is, without waiting to be scrolled.
    @Test func replacedScrollersShowTheKnobAtOnce() async throws {
        let theme = MicaTheme(dark: true)
        let scroll = NSScrollView(frame: NSRect(x: 0, y: 0, width: 300, height: 300))
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = true
        scroll.backgroundColor = NSColor(theme.surface)
        scroll.documentView = NSView(frame: NSRect(x: 0, y: 0, width: 283, height: 2000))
        let window = NSWindow(testContentRect: scroll.frame)
        window.contentView = scroll
        window.orderFront(nil)
        defer { window.close() }
        scroll.scrollerStyle = .legacy
        scroll.layoutSubtreeIfNeeded()
        scroll.contentView.scroll(to: NSPoint(x: 0, y: 850))
        scroll.reflectScrolledClipView(scroll.contentView)
        scroll.useKitScrollers(theme)
        let scroller = try #require(scroll.verticalScroller as? KitScroller)
        let image = Self.snapshot(scroll)
        let knob = try #require(image.run(x: 294, background: theme.surface))
        let expected = Self.knobSpan(scroller, in: scroll)
        #expect(abs(knob.lowerBound - expected.lowerBound) <= 1 && abs(knob.upperBound - expected.upperBound) <= 1,
                "knob \(knob), expected \(expected)")
        // Half way down 1700 pt of travel, in a 294 pt slot: 3 + (294 − 44) / 2.
        #expect(abs(knob.lowerBound - 128) <= 2, "knob \(knob)")
    }

    /// Scroll views at the same place, as in panes stacked in one tile, each
    /// get Kit's scrollers.
    @Test func stackedScrollViewsEachGetKitScrollers() async throws {
        let theme = MicaTheme(dark: true)
        let host = NSHostingView(rootView: ZStack {
            ForEach(0..<3) { pane in
                KitScrollView { VStack { ForEach(0..<100) { Text("Pane \(pane) row \($0)") } }.frame(maxWidth: .infinity) }
                    .opacity(pane == 0 ? 1 : 0)
            }
        }
        .environment(\.mica, theme))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 300, height: 300))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        try await eventually {
            host.layoutSubtreeIfNeeded()
            let scrolls = Self.scrollViews(host)
            return scrolls.count == 3 && scrolls.allSatisfy { $0.verticalScroller is KitScroller }
        }
    }

    /// A SwiftUI `KitScrollView` gets Kit's scrollers in the environment's
    /// theme, and its gutter is what lies behind it.
    @Test(arguments: [false, true])
    func swiftUIScrollViewsUseKitScrollers(dark: Bool) async throws {
        let theme = MicaTheme(dark: dark)
        let host = NSHostingView(rootView: KitScrollView {
            VStack(spacing: 0) { ForEach(0..<100) { Text("Row \($0)").frame(height: 20) } }.frame(maxWidth: .infinity)
        }
        .environment(\.mica, theme).background(theme.surface))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 300, height: 300))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        try await eventually { host.layoutSubtreeIfNeeded(); return Self.scrollViews(host).first?.verticalScroller is KitScroller }
        let scroll = try #require(Self.scrollViews(host).first)
        let scroller = try #require(scroll.verticalScroller as? KitScroller)
        #expect(scroller.theme == theme)
        scroll.scrollerStyle = .legacy
        let width = host.bounds.width
        let gutter = width - scroller.frame.width...width
        try await eventually { Self.snapshot(host, holding: scroll).run(x: width - 6, background: theme.surface) != nil }
        let image = Self.snapshot(host, holding: scroll)
        let knob = try #require(image.run(x: width - 6, background: theme.surface))
        let middle = (knob.lowerBound + knob.upperBound) / 2
        #expect(image.row(y: middle, in: gutter, background: theme.surface) == width - 9...width - 3)
        #expect(Self.same(image.color(x: width - 6, y: middle), theme.muted.mix(with: theme.surface, by: 0.45)))
        #expect(image.row(y: knob.upperBound + 20, in: gutter, background: theme.surface) == nil)
    }

    /// A horizontal scroller's knob lies along the gutter's bottom edge.
    @Test func horizontalKnobsRestAlongTheBottomEdge() async throws {
        let theme = MicaTheme(dark: true)
        let host = NSHostingView(rootView: KitScrollView(.horizontal) {
            Text(String(repeating: "A long line that scrolls sideways. ", count: 40)).fixedSize().frame(height: 60)
        }
        .frame(height: 100).environment(\.mica, theme).background(theme.surface))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 300, height: 100))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        try await eventually { host.layoutSubtreeIfNeeded(); return Self.scrollViews(host).first?.horizontalScroller is KitScroller }
        let scroll = try #require(Self.scrollViews(host).first)
        scroll.scrollerStyle = .legacy
        // Down the knob's column, 20 pt in, from the scroll view's bottom
        // edge: the pill 3 pt above it.
        func bottom() -> CGFloat {
            host.layoutSubtreeIfNeeded()
            let frame = host.convert(scroll.bounds, from: scroll)
            return host.isFlipped ? frame.maxY : host.bounds.height - frame.minY
        }
        try await eventually { Self.snapshot(host, holding: scroll).run(x: 20, from: bottom() - 17, background: theme.surface) == bottom() - 9...bottom() - 3 }
        let height = bottom()
        #expect(Self.same(Self.snapshot(host, holding: scroll).color(x: 20, y: height - 6), theme.muted.mix(with: theme.surface, by: 0.45)))
    }

    /// The file and scratchpad editors' scroll views use Kit's scrollers.
    @Test func sourceEditorsUseKitScrollers() async throws {
        let theme = MicaTheme(dark: true)
        let host = NSHostingView(rootView: HStack(spacing: 0) {
            ReadOnlyFileEditor(path: "test.swift", source: String(repeating: "let value = 1\n", count: 200),
                               position: .constant(SourceEditorState()))
            ScratchpadEditor(text: .constant(String(repeating: "Note\n", count: 200)), editorState: .constant(SourceEditorState()))
        }
        .environment(\.mica, theme))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 300))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        func editors() -> [NSScrollView] {
            Self.scrollViews(host).filter { $0.documentView is TextView }
        }
        try await eventually {
            host.layoutSubtreeIfNeeded()
            let found = editors()
            return found.count == 2 && found.allSatisfy { ($0.verticalScroller as? KitScroller)?.theme == theme }
        }
    }

    // MARK: Helpers

    static func input(_ theme: MicaTheme, workspace: WorkspaceState = WorkspaceState(demo: false)) -> NativeTranscript {
        let prose = String(repeating: "A long paragraph that wraps differently at each width. ", count: 30)
        return NativeTranscript(messages: (0..<12).map { TranscriptMessage(id: "m\($0)", role: "assistant", text: prose, tools: []) },
            hasHistory: false, historyLoading: false, historyError: nil, active: false,
            presentation: TranscriptPresentationState(), workspace: workspace,
            resumeRequest: 0, latestOutOfView: .constant(false), loadHistory: {}, theme: theme)
    }

    /// A long transcript, scrolled to its end, under the given scroller style.
    static func transcript(_ theme: MicaTheme, style: NSScroller.Style = .legacy,
                           workspace: WorkspaceState = WorkspaceState(demo: false)) async throws
        -> (NativeTranscriptCoordinator, NSWindow) {
        let input = input(theme, workspace: workspace)
        let adapter = NativeTranscriptCoordinator(input)
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 700, height: 400))
        window.contentView = adapter.scroll
        window.orderFront(nil)
        adapter.receive(input)
        try await eventually { adapter.table.bounds.height > 2_000 }
        // The system sets every scroll view's style as it learns its own.
        adapter.scroll.scrollerStyle = style
        adapter.scroll.layoutSubtreeIfNeeded()
        let scroller = try #require(adapter.scroll.verticalScroller as? KitScroller)
        scroller.increasesContrast = { false }
        try await eventually { scroller.scrollerStyle == style && !scroller.rect(for: .knob).isEmpty }
        return (adapter, window)
    }

    /// The knob's painted extent along the scroller, in points from the top
    /// of `view`: the system's knob span.
    static func knobSpan(_ scroller: NSScroller, in view: NSView) -> ClosedRange<CGFloat> {
        let knob = view.convert(scroller.rect(for: .knob), from: scroller)
        let top = view.isFlipped ? knob.minY : view.bounds.height - knob.maxY
        return top...top + knob.height
    }

    static func crossing(_ type: NSEvent.EventType, _ window: NSWindow) throws -> NSEvent {
        try #require(NSEvent.enterExitEvent(with: type, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: window.windowNumber, context: nil, eventNumber: 0, trackingNumber: 0, userData: nil))
    }

    static func scrollViews(_ view: NSView) -> [NSScrollView] {
        ((view as? NSScrollView).map { [$0] } ?? []) + view.subviews.flatMap(scrollViews)
    }

    /// Whether `color`, read from a snapshot, is `expected` as a snapshot
    /// shows it. A snapshot of a window's view is in the display's colour
    /// space, so a swatch of `expected` is drawn in a window and read back.
    nonisolated static func same(_ color: NSColor?, _ expected: Color) -> Bool {
        MainActor.assumeIsolated {
            guard let color = color?.usingColorSpace(.sRGB), let expected = swatch(expected) else { return false }
            return close(color, expected)
        }
    }

    static func swatch(_ color: Color) -> NSColor? {
        let view = Swatch(frame: NSRect(x: 0, y: 0, width: 4, height: 4))
        view.color = NSColor(color)
        let window = NSWindow(testContentRect: view.frame)
        window.contentView = view
        defer { window.close() }
        return snapshot(view).color(x: 2, y: 2)?.usingColorSpace(.sRGB)
    }

    private final class Swatch: NSView {
        var color = NSColor.clear
        override func draw(_ dirtyRect: NSRect) { color.setFill(); bounds.fill() }
    }

    nonisolated static func close(_ color: NSColor, _ expected: NSColor) -> Bool {
        return abs(color.redComponent - expected.redComponent) <= 0.02
            && abs(color.greenComponent - expected.greenComponent) <= 0.02
            && abs(color.blueComponent - expected.blueComponent) <= 0.02
    }

    /// A snapshot of `view`, with `scroll` held to `style`: the system sets
    /// every scroll view's style as it learns its own, at whatever point
    /// that reaches the main thread during a run.
    static func snapshot(_ view: NSView, holding scroll: NSScrollView? = nil, style: NSScroller.Style = .legacy) -> Snapshot {
        if let scroll, scroll.scrollerStyle != style { scroll.scrollerStyle = style }
        view.layoutSubtreeIfNeeded()
        let bitmap = view.bitmapImageRepForCachingDisplay(in: view.bounds)!
        view.cacheDisplay(in: view.bounds, to: bitmap)
        return Snapshot(bitmap: bitmap, scale: CGFloat(bitmap.pixelsWide) / view.bounds.width)
    }

    /// A view's pixels, addressed in points from its top-left corner.
    @MainActor struct Snapshot {
        let bitmap: NSBitmapImageRep
        let scale: CGFloat

        func color(x: CGFloat, y: CGFloat) -> NSColor? {
            bitmap.colorAt(x: Int(x * scale), y: Int(y * scale))
        }

        private func painted(_ x: Int, _ y: Int, _ background: NSColor?) -> Bool {
            guard let background, let color = bitmap.colorAt(x: x, y: y)?.usingColorSpace(.sRGB) else { return true }
            return !KitScrollerTests.close(color, background)
        }

        /// The first run painted off the background down the column `x`,
        /// from `from` points down.
        func run(x: CGFloat, from: CGFloat = 0, background: Color) -> ClosedRange<CGFloat>? {
            let column = Int(x * scale)
            let background = KitScrollerTests.swatch(background)
            guard let first = (Int(from * scale)..<bitmap.pixelsHigh).first(where: { painted(column, $0, background) }) else { return nil }
            let end = (first..<bitmap.pixelsHigh).first(where: { !painted(column, $0, background) }) ?? bitmap.pixelsHigh
            return CGFloat(first) / scale...CGFloat(end) / scale
        }

        /// What is painted off the background along the row `y`, within `span`.
        func row(y: CGFloat, in span: ClosedRange<CGFloat>, background: Color) -> ClosedRange<CGFloat>? {
            let line = Int(y * scale)
            let background = KitScrollerTests.swatch(background)
            let xs = Int(span.lowerBound * scale)..<min(bitmap.pixelsWide, Int(span.upperBound * scale))
            guard let first = xs.first(where: { painted($0, line, background) }),
                  let last = xs.last(where: { painted($0, line, background) }) else { return nil }
            return CGFloat(first) / scale...CGFloat(last + 1) / scale
        }
    }
}
