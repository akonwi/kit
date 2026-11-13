import Foundation
import KitClient
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

/// Find in a file: what a rendered Markdown file searches, how matches are
/// marked in its text and code, and where the screen stands for a search.
@MainActor
struct FileFindTests {
    static let plan = """
    # Pairing a device

    A paired device holds its own credential. Then enter the device code.

    ```sh
    kit server pair device
    ```

    ```mermaid
    graph TD
      device --> server
    ```

    - Remove a device with kit server devices.

    > [!NOTE] Devices
    > A device can be renamed.

    | Name | Role |
    | --- | --- |
    | device | client |
    """

    static var blocks: [MarkdownContent.Block] { MarkdownContent(source: plan).blocks }

    @Test func renderedMarkdownIsSearchedInReadingOrderWithoutItsDiagrams() {
        let found = MarkdownFind.pieces(Self.blocks)
        #expect(found.pieces == [
            "Pairing a device",
            "A paired device holds its own credential. Then enter the device code.",
            "kit server pair device\n",
            "Remove a device with kit server devices.",
            "A device can be renamed.",
            "Name", "Role", "device", "client",
        ])
        // The diagram has no pieces; its block starts where the list does.
        #expect(found.starts == [0, 1, 2, 3, 3, 4, 5, 9])
        #expect(MarkdownFind.block(of: 3, starts: found.starts) == 4)
        #expect(MarkdownFind.block(of: 8, starts: found.starts) == 6)
    }

    @Test func aSearchCountsAndStepsThroughTheRenderedTextWrapping() async {
        let find = FileFind()
        find.open()
        find.setPieces(MarkdownFind.pieces(Self.blocks).pieces, corpus: .rendered)
        find.query = "device"
        await find.refresh()
        #expect(find.label == "1 of 8")
        #expect(find.matches.map(\.piece) == [0, 1, 1, 2, 3, 3, 4, 7])
        for _ in 0..<7 { find.next() }
        #expect(find.label == "8 of 8")
        find.next()
        #expect(find.label == "1 of 8")
        find.previous()
        #expect(find.label == "8 of 8")
        find.wholeWord = true
        await find.refresh()
        // "devices" and "Devices" no longer count; the current match stays.
        #expect(find.label == "7 of 7")
        find.matchCase = true
        find.query = "Pairing"
        await find.refresh()
        #expect(find.label == "1 of 1")
    }

    @Test func marksFillTheCurrentMatchAndTintTheRest() {
        let matches = [FindMatch(piece: 1, range: 9..<15), FindMatch(piece: 1, range: 57..<63), FindMatch(piece: 2, range: 16..<22)]
        let marks = FindMarks.marks(in: 1, matches: matches, current: 1)
        #expect(marks == [FindMark(range: 9..<15, current: false), FindMark(range: 57..<63, current: true)])
        #expect(FindMarks.marks(in: 0, matches: matches, current: 1).isEmpty)
        // A closed bar's match stays marked alone.
        #expect(FindMarks.marks(in: 1, matches: matches, current: nil, kept: matches[1])
            == [FindMark(range: 57..<63, current: true)])

        let text = FindMarks.apply(marks, to: AttributedString("A paired device holds its own credential. Then enter the device code."))
        let runs = text.runs.map { (String(text[$0.range].characters), $0.backgroundColor, $0.foregroundColor) }
        #expect(runs.map(\.0) == ["A paired ", "device", " holds its own credential. Then enter the ", "device", " code."])
        #expect(runs[1].1 == Theme.findMatch && runs[1].2 == nil)
        #expect(runs[3].1 == Theme.accent && runs[3].2 == .white)
        #expect(runs[0].1 == nil && runs[2].1 == nil)
    }

    @Test func aMarkInCodeFallsOnTheLineItsRangeIsOn() {
        let lines = [AttributedString("let a = 1"), AttributedString("let device = a")]
        let marked = FindMarks.apply([FindMark(range: 14..<20, current: true)], toLines: lines)
        #expect(marked[0] == lines[0])
        let run = marked[1].runs.first { $0.backgroundColor != nil }
        #expect(run.map { String(marked[1][$0.range].characters) } == "device")
        #expect(run?.backgroundColor == Theme.accent)
    }

    @Test func renderedBlocksCarryTheirMarksAndCodeMarksGoByItsSource() {
        let found = MarkdownFind.pieces(Self.blocks)
        let matches = FileFind.search(found.pieces, query: "device", options: .init()).matches
        var next = 0
        var code: [String: [FindMark]] = [:]
        let marked = Self.blocks.map {
            MarkdownFind.marked($0, marks: { FindMarks.marks(in: $0, matches: matches, current: 0) },
                                next: &next, code: &code)
        }
        #expect(next == found.pieces.count)
        guard case .heading(1, let heading) = marked[0] else { Issue.record("a heading"); return }
        #expect(heading.runs.contains { String(heading[$0.range].characters) == "device" && $0.backgroundColor == Theme.accent })
        #expect(code == ["kit server pair device\n": [FindMark(range: 16..<22, current: false)]])
        guard case .table(let table) = marked[6] else { Issue.record("a table"); return }
        #expect(table.rows[0][0].runs.first?.backgroundColor == Theme.findMatch)
        #expect(table.header[0].runs.first?.backgroundColor == nil)
    }

    @Test func identicalCodeBlocksMarkOnlyTheirOwnMatches() {
        let blocks = MarkdownContent(source: "```sh\nkit pair\n```\n\nBetween.\n\n```sh\nkit pair\n```\n").blocks
        let found = MarkdownFind.pieces(blocks)
        #expect(found.pieces == ["kit pair\n", "Between.", "kit pair\n"])
        let matches = FileFind.search(found.pieces, query: "pair", options: .init()).matches
        let preview = FilePreviewBlocks(blocks: blocks, viewport: FindViewport(), starts: found.starts,
                                        marks: { FindMarks.marks(in: $0, matches: matches, current: 1) })
        #expect(preview.mark(blocks[0], at: 0).code == ["kit pair\n": [FindMark(range: 4..<8, current: false)]])
        #expect(preview.mark(blocks[2], at: 2).code == ["kit pair\n": [FindMark(range: 4..<8, current: true)]])
        #expect(preview.mark(blocks[1], at: 1).code.isEmpty)

        // Should two land together, the current one still shows.
        let line = FindMarks.apply([FindMark(range: 4..<8, current: true), FindMark(range: 4..<8, current: false)],
                                   to: AttributedString("kit pair"))
        #expect(line.runs.first { $0.backgroundColor != nil }?.backgroundColor == Theme.accent)
    }

    @Test func theScreenSaysWhichBlockIsAtItsTopAndWhichItShows() {
        let viewport = FindViewport()
        viewport.blocks = [0: 12...40, 1: 50...200, 2: 210...900, 3: 910...940]
        viewport.visible = CGRect(x: 0, y: 120, width: 400, height: 600)
        #expect(viewport.topBlock == 1)
        #expect(!viewport.shows(block: 1))
        #expect(viewport.shows(block: 2) == false)
        viewport.visible = CGRect(x: 0, y: 205, width: 400, height: 720)
        #expect(viewport.topBlock == 2)
        #expect(viewport.shows(block: 2))
        #expect(!viewport.shows(block: 3))
    }

    @Test func aLargeFileCountsToTheLimitQuickly() async {
        let lines = (1...50_000).map { "\($0) handler handler" }
        let find = FileFind()
        find.open()
        find.setPieces(lines, corpus: .source)
        let clock = ContinuousClock()
        let took = await clock.measure {
            find.query = "handler"
            await find.refresh()
            find.query = "handlerz"
            await find.refresh()
        }
        #expect(find.label == "No matches")
        #expect(took < .seconds(2), "two searches of 50,000 lines took \(took)")
        find.query = "handler"
        await find.refresh()
        #expect(find.label == "1 of 10,000+")
    }
}
