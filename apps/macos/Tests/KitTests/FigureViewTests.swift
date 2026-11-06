import AppKit
import KitFigures
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

/// How figures are laid out in a transcript card (ADR 0054).
@MainActor struct FigureViewTests {
    /// The width a figure's card takes when offered width points.
    private func width(_ info: String, _ body: String, offered width: CGFloat) throws -> CGFloat {
        let block = FigureBlock(info: info, source: body)
        _ = try block.figure.get()
        let host = NSHostingController(rootView: FigureBlockView(block: block)
            .environment(\.mica, MicaTheme(dark: true)))
        return host.sizeThatFits(in: CGSize(width: width, height: 10_000)).width
    }

    @Test func treeGuidesFollowEachAncestorWithALaterSibling() throws {
        let figure = try Figure.parse(info: "tree", body: """
            - registry/default
              - graph-frame
                - graph-frame.tsx
                - graph-motion.ts
              - graph-tree
                - graph-tree.tsx
            - other
            """).get()
        #expect(figure.guides == [[], [.branch], [.through, .branch], [.through, .last], [.last], [.none, .last], []])
    }

    @Test func longTextIsCutToThirtyTwoCharactersWithAnEllipsis() {
        #expect(Figure.cut(String(repeating: "w", count: 40)) == String(repeating: "w", count: 31) + "…")
        #expect(Figure.cut(String(repeating: "w", count: 32)) == String(repeating: "w", count: 32))
        #expect(Figure.cut("From the first week of March", 20) == "From the first week…")
    }

    @Test func aWideCompareTableScrollsInsideItsCard() throws {
        let label = String(repeating: "label ", count: 50)
        let cells = Array(repeating: String(repeating: "c", count: 30), count: 6).joined(separator: " | ")
        let body = "| | a | b | c | d | e | f |\n| --- | --- | --- | --- | --- | --- | --- |\n| \(label) | \(cells) |"
        #expect(try width("compare", body, offered: 400) <= 400)
    }

    @Test func everyPhaseTwoFigureFitsANarrowCard() throws {
        let figures: [(String, String)] = [
            ("decision status=\"accepted\" date=\"Mar 12\"", "- **Postgres** — we already run it\n- *Mongo* — no joins we trust\n\nRevisit if writes pass 2k a second."),
            ("spec", "- Family: Geist Mono\n- **Size: 14 / 21**\n- Features: tabular figures, slashed zero, and the stylistic alternates"),
            ("tree", "- registry/default\n  - graph-frame\n    - graph-frame.tsx — ui"),
            ("delta from=\"2025\" to=\"2026\" change=\"percent\" better=\"up\"", "- docs: 8,200 → 12,400\n- copy: 5,100 → 4,100"),
            ("score", "- Performance: 4/5\n- **Docs: 2.5/5**"),
            ("stack scale=\"shared\"", "- marketing: 48 js, 22 css, 30 images\n- api: 8 js, 4 css, 8 images"),
            ("checklist", "- [x] Port the figure renderer to the Mac\n- [ ] **Web adoption branch** — in review"),
        ]
        for (info, body) in figures {
            #expect(try width(info, body, offered: 360) <= 360, "\(info)")
        }
    }

    /// A checklist drawn in a dark card 300 points wide, with a swatch of
    /// each of `swatches` (its layers drawn in order) in a row under it, so
    /// the figure's colours and the swatches go through the same colour
    /// conversion.
    @MainActor struct ChecklistDrawing {
        let bitmap: NSBitmapImageRep
        let scale: CGFloat

        init(_ body: String, swatches: [[Color]], theme: MicaTheme) throws {
            let figure = try Figure.parse(info: "checklist", body: body).get()
            let host = NSHostingView(rootView: VStack(alignment: .leading, spacing: 0) {
                FigureView(figure: figure).frame(width: 300, height: 90, alignment: .topLeading)
                HStack(spacing: 0) {
                    ForEach(Array(swatches.enumerated()), id: \.offset) { _, layers in
                        ZStack {
                            ForEach(Array(layers.enumerated()), id: \.offset) { _, color in Rectangle().fill(color) }
                        }
                        .frame(width: 10, height: 10)
                    }
                }
            }
            .frame(width: 300, height: 120, alignment: .topLeading).environment(\.mica, theme).background(theme.raised))
            let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 300, height: 120))
            window.contentView = host
            host.layoutSubtreeIfNeeded()
            bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
            host.cacheDisplay(in: host.bounds, to: bitmap)
            scale = CGFloat(bitmap.pixelsWide) / host.bounds.width
        }

        /// The colour at x, y points from the top-leading corner.
        func pixel(_ x: CGFloat, _ y: CGFloat) throws -> NSColor {
            try #require(bitmap.colorAt(x: Int(x * scale), y: Int(y * scale))?.usingColorSpace(.sRGB))
        }

        func swatch(_ index: Int) throws -> NSColor { try pixel(CGFloat(index) * 10 + 5, 95) }

        /// The brightest pixel in the region x by y, or `floor` if none is
        /// brighter.
        func brightest(_ x: ClosedRange<CGFloat>, _ y: ClosedRange<CGFloat>, floor: NSColor) throws -> NSColor {
            var found = floor
            for row in stride(from: y.lowerBound, to: y.upperBound, by: 1 / scale) {
                for column in stride(from: x.lowerBound, to: x.upperBound, by: 1 / scale) {
                    let color = try pixel(column, row)
                    if color.brightnessComponent > found.brightnessComponent { found = color }
                }
            }
            return found
        }

        static func near(_ a: NSColor, _ b: NSColor) -> Bool {
            abs(a.redComponent - b.redComponent) + abs(a.greenComponent - b.greenComponent)
                + abs(a.blueComponent - b.blueComponent) < 0.03
        }
    }

    /// A checklist's done task is a muted check in a border box with a
    /// muted title; the task in hand is an accent box on an accent wash; an
    /// open task is an empty strong-border box with its title in the text
    /// colour.
    @Test func checklistBoxesShowWhetherEachTaskIsDone() throws {
        let theme = MicaTheme(dark: true)
        // The in-hand outline is drawn over the wash.
        let drawing = try ChecklistDrawing("- [x] Draft\n- [ ] **Review**\n- [ ] Merge", swatches: [
            [theme.border], [theme.muted], [theme.accent.opacity(0.15), theme.accent.opacity(0.5)],
            [theme.accent.opacity(0.15)], [theme.strongBorder], [theme.raised], [theme.text],
        ], theme: theme)
        let near = ChecklistDrawing.near
        let (border, muted, accentEdge, accentWash, strongBorder, raised, text) = (
            try drawing.swatch(0), try drawing.swatch(1), try drawing.swatch(2), try drawing.swatch(3),
            try drawing.swatch(4), try drawing.swatch(5), try drawing.swatch(6))

        // Each row is 18 points tall, 10 apart; a box is 16 points square,
        // a point below its row's top, and its title starts 26 points in.
        let rows: [CGFloat] = [0, 28, 56]
        #expect(near(try drawing.pixel(0, rows[0] + 9), border), "done box outline")
        #expect(near(try drawing.brightest(3...13, rows[0] + 4...rows[0] + 14, floor: raised), muted), "done check")
        #expect(near(try drawing.brightest(26...60, rows[0]...rows[0] + 18, floor: raised), muted), "done title")
        #expect(near(try drawing.pixel(0, rows[1] + 9), accentEdge), "in-hand box outline")
        #expect(near(try drawing.pixel(8, rows[1] + 9), accentWash), "in-hand box wash")
        #expect(near(try drawing.pixel(0, rows[2] + 9), strongBorder), "open box outline")
        #expect(near(try drawing.pixel(8, rows[2] + 9), raised), "open box inside")
        #expect(near(try drawing.brightest(26...60, rows[2]...rows[2] + 18, floor: raised), text), "open title")
    }

    /// A subtask's box sits under its task's title, 26 points in, and the
    /// next task starts back at the leading edge.
    @Test func checklistSubtasksSitUnderTheirTasksTitle() throws {
        let theme = MicaTheme(dark: true)
        let drawing = try ChecklistDrawing("- [ ] Release\n  - [ ] Notes\n- [ ] Announce",
                                           swatches: [[theme.strongBorder], [theme.raised]], theme: theme)
        let near = ChecklistDrawing.near
        let (strongBorder, raised) = (try drawing.swatch(0), try drawing.swatch(1))
        let rows: [CGFloat] = [0, 28, 56]
        #expect(near(try drawing.pixel(0, rows[0] + 9), strongBorder), "task box at the leading edge")
        #expect(near(try drawing.pixel(0, rows[1] + 9), raised), "nothing at the leading edge of a subtask")
        #expect(near(try drawing.pixel(26, rows[1] + 9), strongBorder), "subtask box under the task's title")
        #expect(near(try drawing.pixel(0, rows[2] + 9), strongBorder), "next task box at the leading edge")
    }
}
