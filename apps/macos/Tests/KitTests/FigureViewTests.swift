import AppKit
import KitFigures
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
        ]
        for (info, body) in figures {
            #expect(try width(info, body, offered: 360) <= 360, "\(info)")
        }
    }
}
