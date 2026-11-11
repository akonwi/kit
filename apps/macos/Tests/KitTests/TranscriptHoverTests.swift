import AppKit
import SwiftUI
import Testing
@testable import Kit

/// How transcript content shows it's clickable: the pointing hand and an
/// accent underline over links, the hand over elements that open something
/// as a link does, and a `bgMuted` fill under small buttons. Hovering only
/// paints; it never changes a size.
@MainActor struct TranscriptHoverTests {
    static let width: CGFloat = 320

    /// Rendered Markdown in a window of its own, tracked as the transcript
    /// tracks its rows.
    @MainActor struct Rendered {
        let window: NSWindow
        let host: NSView
        let hover: TextLinkHover
        let theme: MicaTheme

        /// `message` marks the text as a transcript message, which copies;
        /// `sessionLink` links its session mentions.
        init(_ source: String, theme: MicaTheme = MicaTheme(dark: false), message: Bool = false,
             sessionLink: TranscriptSessionLink? = nil) {
            // An accent swatch below the text, painted as the text is.
            let host = NSHostingView(rootView: VStack(alignment: .leading, spacing: 0) {
                MarkdownView(source: source)
                theme.accent.frame(width: 8, height: 8)
            }
                .background { if message { MessageCopyRegion(markdown: source) } }
                .environment(\.transcriptSessionLink, sessionLink)
                .frame(width: TranscriptHoverTests.width, alignment: .topLeading)
                .environment(\.mica, theme).foregroundStyle(theme.text).background(theme.surface))
            // The window fits the text, so its size stays put as it lays out.
            let size = host.fittingSize
            host.sizingOptions = []
            let window = NSWindow(testContentRect: NSRect(origin: .zero, size: size))
            window.contentView = host
            host.frame = NSRect(origin: .zero, size: size)
            window.makeKeyAndOrderFront(nil)
            host.layoutSubtreeIfNeeded()
            let hover = TextLinkHover(color: NSColor(theme.accent))
            hover.attach(to: host)
            self.window = window
            self.host = host
            self.hover = hover
            self.theme = theme
        }

        var field: NSTextField? { Self.field(in: host) }

        func close() {
            hover.detach()
            window.close()
        }

        private static func field(in view: NSView) -> NSTextField? {
            if let field = view as? NSTextField { return field }
            for child in view.subviews { if let field = field(in: child) { return field } }
            return nil
        }

        /// Where the field's editor, which takes its clicks, lays out the
        /// characters in `range`, in window coordinates.
        func editorRects(_ range: NSRange) throws -> [NSRect] {
            let field = try #require(field)
            let editor = try #require(window.fieldEditor(true, for: field) as? NSTextView)
            field.cell?.select(withFrame: field.bounds, in: field, editor: editor, delegate: nil, start: 0, length: 0)
            defer { field.cell?.endEditing(editor) }
            let manager = try #require(editor.layoutManager), container = try #require(editor.textContainer)
            let glyphs = manager.glyphRange(forCharacterRange: range, actualCharacterRange: nil)
            var rects: [NSRect] = []
            manager.enumerateEnclosingRects(forGlyphRange: glyphs, withinSelectedGlyphRange: NSRange(location: NSNotFound, length: 0),
                                            in: container) { rect, _ in
                rects.append(editor.convert(rect.offsetBy(dx: editor.textContainerOrigin.x, dy: editor.textContainerOrigin.y), to: nil))
            }
            return rects
        }

        func range(of text: String) throws -> NSRange {
            let range = (try #require(field).attributedStringValue.string as NSString).range(of: text)
            try #require(range.location != NSNotFound)
            return range
        }

        func bitmap() throws -> NSBitmapImageRep {
            host.layoutSubtreeIfNeeded()
            host.displayIfNeeded()
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            return bitmap
        }
    }

    private static func center(_ rect: NSRect) -> NSPoint { NSPoint(x: rect.midX, y: rect.midY) }

    @Test func linksAndLinkLikeElementsTakeTheHandAndButtonsKeepTheArrow() {
        let hand = TranscriptClickable.allCases.filter { $0.pointer == .hand }
        #expect(hand == [.markdownLink, .toolChip, .wellFilePath, .forkedSession, .imagePreview])
        let arrow = TranscriptClickable.allCases.filter { $0.pointer == .arrow }
        #expect(arrow == [.toolRow, .toolGroup, .compaction, .pluginMessage, .fenceAction, .attachmentAction])
        #expect(hand.allSatisfy { $0.pointer.cursor === NSCursor.pointingHand && $0.pointerStyle != nil })
        #expect(arrow.allSatisfy { $0.pointer.cursor === NSCursor.arrow && $0.pointerStyle == nil })
    }

    @Test(arguments: [false, true])
    func aHoveredLinkShowsTheHandAndAnAccentUnderlineUnderEachOfItsLines(dark: Bool) throws {
        let rendered = Rendered("A paragraph that runs on for a while, then [a link whose label is long enough to wrap onto the next line](https://example.com/page) and plain text after it.",
                                theme: MicaTheme(dark: dark))
        defer { rendered.close() }
        let field = try #require(rendered.field)
        let link = try rendered.range(of: "a link whose label is long enough to wrap onto the next line")
        let lines = try rendered.editorRects(link)
        try #require(lines.count >= 2)
        let fieldFrame = field.frame, size = rendered.host.fittingSize

        NSCursor.arrow.set()
        rendered.hover.update(at: Self.center(lines[0]))
        let hovered = try #require(rendered.hover.hovered)
        #expect(hovered.field === field)
        #expect(hovered.range == link)
        #expect(NSCursor.current == .pointingHand)

        // One underline per line, spanning the link's text on that line,
        // just under its glyphs, in the theme's accent.
        let underlines = hovered.underlines.map { field.convert($0, to: nil) }
        #expect(underlines.count == lines.count)
        for (underline, line) in zip(underlines, lines) {
            #expect(abs(underline.minX - line.minX) < 1)
            #expect(underline.maxX <= line.maxX + 0.5)
            #expect(underline.height >= 1 && underline.height <= 2)
            // Window coordinates rise upward: the underline sits in the line's lower part.
            #expect(underline.midY > line.minY && underline.midY < line.midY)
        }
        let underlineView = try #require(field.subviews.compactMap { $0 as? TextLinkUnderlineView }.first)
        #expect(underlineView.color == NSColor(rendered.theme.accent))
        // The last line's part ends where the link does.
        #expect(abs(try #require(underlines.last).maxX - (try #require(lines.last)).maxX) < 1)
        let bitmap = try rendered.bitmap()
        let scale = CGFloat(bitmap.pixelsWide) / rendered.host.bounds.width
        let accent = try #require(bitmap.colorAt(x: Int(4 * scale), y: Int((rendered.host.bounds.height - 4) * scale))?
            .usingColorSpace(.sRGB))
        for underline in underlines {
            let point = rendered.host.convert(NSPoint(x: underline.midX, y: underline.midY), from: nil)
            let color = try #require(bitmap.colorAt(x: Int(point.x * scale), y: Int(point.y * scale))?.usingColorSpace(.sRGB))
            #expect(abs(color.redComponent - accent.redComponent) < 0.02)
            #expect(abs(color.greenComponent - accent.greenComponent) < 0.02)
            #expect(abs(color.blueComponent - accent.blueComponent) < 0.02)
        }

        // Hovering changes no geometry.
        #expect(field.frame == fieldFrame)
        #expect(rendered.host.fittingSize == size)

        // Its last line is the same link; plain text after it is not.
        rendered.hover.update(at: Self.center(try #require(lines.last)))
        #expect(rendered.hover.hovered?.range == link)
        let plain = try rendered.editorRects(try rendered.range(of: "plain text"))
        rendered.hover.update(at: Self.center(plain[0]))
        #expect(rendered.hover.hovered == nil)
        #expect(field.subviews.contains { $0 is TextLinkUnderlineView } == false)
        #expect(NSCursor.current == .iBeam)
    }

    @Test func aFilePathLinkInCodeUnderlinesLikeAnyOtherLink() throws {
        let rendered = Rendered("Read [`notes/plan.md`](notes/plan.md) first.")
        defer { rendered.close() }
        let link = try rendered.range(of: "notes/plan.md")
        let rects = try rendered.editorRects(link)
        rendered.hover.update(at: Self.center(rects[0]))
        #expect(rendered.hover.hovered?.range == link)
        #expect(rendered.hover.hovered?.underlines.count == 1)
    }

    @Test func leavingTheRegionClearsTheHover() throws {
        let rendered = Rendered("See [Kit](https://example.com).")
        defer { rendered.close() }
        let rects = try rendered.editorRects(try rendered.range(of: "Kit"))
        rendered.hover.update(at: Self.center(rects[0]))
        #expect(rendered.hover.hovered != nil)
        rendered.hover.update(at: NSPoint(x: -20, y: -20))
        #expect(rendered.hover.hovered == nil)
        #expect(NSCursor.current == .arrow)

        // Leaving through the tracking area's edge restores the arrow too.
        rendered.hover.update(at: Self.center(rects[0]))
        #expect(NSCursor.current == .pointingHand)
        let exit = try #require(NSEvent.enterExitEvent(with: .mouseExited, location: NSPoint(x: -20, y: -20), modifierFlags: [],
            timestamp: 0, windowNumber: rendered.window.windowNumber, context: nil, eventNumber: 0, trackingNumber: 0, userData: nil))
        rendered.hover.mouseExited(with: exit)
        #expect(rendered.hover.hovered == nil)
        #expect(rendered.field?.subviews.contains { $0 is TextLinkUnderlineView } == false)
        #expect(NSCursor.current == .arrow)
    }

    @Test func theTranscriptTracksLinksAcrossItsRowsInTheAccent() {
        let theme = MicaTheme(dark: true)
        let input = NativeTranscript(messages: [], hasHistory: false, historyLoading: false, historyError: nil, active: false,
            presentation: TranscriptPresentationState(), workspace: WorkspaceState(demo: false),
            resumeRequest: 0, latestOutOfView: .constant(false), loadHistory: {}, theme: theme)
        let adapter = NativeTranscriptCoordinator(input)
        defer { adapter.stop() }
        #expect(adapter.scroll.trackingAreas.contains { $0.owner === adapter.linkHover })
        #expect(adapter.linkHover.color == NSColor(theme.accent))
    }

    /// A small button's label on and off its hover fill.
    @MainActor struct Filled {
        let host: NSView
        let theme: MicaTheme
        let size: CGSize

        init(highlighted: Bool, margin: CGFloat = 0, theme: MicaTheme = MicaTheme(dark: false)) {
            let host = NSHostingView(rootView: HoverFillLabel(highlighted: highlighted, margin: margin) {
                Text("Copy").font(.kit(size: 10)).padding(margin)
            }
            .padding(20).environment(\.mica, theme).background(theme.raised))
            host.frame = NSRect(origin: .zero, size: host.fittingSize)
            host.layoutSubtreeIfNeeded()
            self.host = host
            self.theme = theme
            self.size = host.fittingSize
        }

        /// The color at `point`, in points from the top-leading corner.
        func color(at point: CGPoint) throws -> NSColor {
            let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            let scale = CGFloat(bitmap.pixelsWide) / host.bounds.width
            return try #require(bitmap.colorAt(x: Int(point.x * scale), y: Int(point.y * scale))?.usingColorSpace(.sRGB))
        }
    }

    private static func same(_ color: NSColor, _ expected: Color) throws -> Bool {
        let expected = try #require(NSColor(expected).usingColorSpace(.sRGB))
        return abs(color.redComponent - expected.redComponent) < 0.01
            && abs(color.greenComponent - expected.greenComponent) < 0.01
            && abs(color.blueComponent - expected.blueComponent) < 0.01
    }

    @Test(arguments: [false, true])
    func aHoveredSmallButtonSitsOnTheMutedFillReachingPastItsLabel(dark: Bool) throws {
        let theme = MicaTheme(dark: dark)
        let rest = Filled(highlighted: false, theme: theme), hovered = Filled(highlighted: true, theme: theme)
        #expect(hovered.size == rest.size)
        // 2 points left of the label, inside the fill's 4-point outset,
        // and 1 point above it, inside its 2-point outset.
        let beside = CGPoint(x: 18, y: hovered.size.height / 2), above = CGPoint(x: 30, y: 19)
        #expect(try Self.same(rest.color(at: beside), theme.raised))
        #expect(try Self.same(hovered.color(at: beside), theme.hover))
        #expect(try Self.same(hovered.color(at: above), theme.hover))
        // Past the outset, the card shows.
        #expect(try Self.same(hovered.color(at: CGPoint(x: 14, y: hovered.size.height / 2)), theme.raised))
    }

    @Test func aMarginedButtonsFillFollowsWhatItDrawsNotItsHitArea() throws {
        let theme = MicaTheme(dark: false)
        let rest = Filled(highlighted: false, margin: 8, theme: theme), hovered = Filled(highlighted: true, margin: 8, theme: theme)
        #expect(hovered.size == rest.size)
        let middle = hovered.size.height / 2
        // The label draws from 28 points in; its fill starts 4 points before.
        #expect(try Self.same(hovered.color(at: CGPoint(x: 25, y: middle)), theme.hover))
        #expect(try Self.same(hovered.color(at: CGPoint(x: 22, y: middle)), theme.raised))
    }
}

/// A Markdown file's preview shows its links as the transcript does.
@MainActor struct MarkdownPreviewLinkHoverTests {
    private static func region(in view: NSView) -> TextLinkHoverRegion.RegionView? {
        if let region = view as? TextLinkHoverRegion.RegionView { return region }
        for child in view.subviews { if let region = region(in: child) { return region } }
        return nil
    }

    @Test func thePreviewTracksLinksUnderThePointerInTheAccent() throws {
        let theme = MicaTheme(dark: false)
        let host = NSHostingView(rootView: MarkdownDocumentView(source: "See [Kit](https://example.com).")
            .environment(\.mica, theme).frame(width: 600, height: 300))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 600, height: 300))
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        host.layoutSubtreeIfNeeded()
        let region = try #require(Self.region(in: host))
        #expect(region.trackingAreas.contains { $0.owner === region.hover })
        #expect(region.hover.color == NSColor(theme.accent))
    }
}
