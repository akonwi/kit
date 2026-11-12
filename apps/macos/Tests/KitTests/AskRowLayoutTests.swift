import AppKit
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

/// An Ask row with a long question and a long free-text answer stays on one
/// line within its row: the question chip leads, the answer is cut at the
/// end, and neither widens the transcript column.
@MainActor struct AskRowLayoutTests {
    nonisolated static let question = "Who reported it and what did they see?"
    nonisolated static let longQuestion = "Who reported the Appointments page failing, which browser were they using, and what exactly did they see on the screen?"
    static let answer = "for Erika Renteria, she's a clinician and said that the Appointments page in the rails app doesn't load for her at all, it just spins forever and then shows a blank page"
    static let prose = "Next I'll check whether Datadog RUM is available through the integration so I can see her browser session, and then I'll look at the slow query logs for the appointments controller around the time she reported it."

    static func ask(_ question: String) -> ToolActivity {
        ToolActivity(id: "ask", name: "input_from_user", summary: "", output: #"{"value":"\#(answer)"}"#,
                     arguments: #"{"title":"\#(question)"}"#, failed: false, status: "Completed")
    }

    @Test func theRowShowsTheQuestionAndTheAnswerOnOneLine() {
        let row = ToolPresentation(Self.ask(Self.question))
        #expect(row.title == "Ask")
        #expect(row.chip == Self.question)
        #expect(row.facts == [ToolFact(text: Self.answer, tone: .text, monospaced: false)])
        #expect(row.expandable)
        // Without a question the row has no chip, only the answer.
        let untitled = ToolActivity(id: "ask", name: "input_from_user", summary: "", output: #"{"value":"Erika"}"#,
                                    arguments: #"{"title":"  "}"#, failed: false, status: "Completed")
        #expect(ToolPresentation(untitled).chip == nil)
        #expect(ToolPresentation(untitled).facts.map(\.text) == ["Erika"])
    }

    /// Chip and facts share what's left of the row after the title: the chip
    /// keeps its width up to half of it, or more beside short facts.
    @Test func chipAndFactsShareTheRow() {
        // Both fit: each at its own width.
        #expect(ToolRowDetails.widths(600, chip: 200, facts: 300, spacing: 10) == (200, 300))
        // A long answer beside a short question: the question whole, the answer cut.
        #expect(ToolRowDetails.widths(600, chip: 200, facts: 900, spacing: 10) == (200, 390))
        // Both long: half each.
        #expect(ToolRowDetails.widths(600, chip: 700, facts: 900, spacing: 10) == (295, 295))
        // A long command beside an exit code: the code whole, the command cut.
        #expect(ToolRowDetails.widths(600, chip: 900, facts: 40, spacing: 10) == (550, 40))
        // No facts: the chip takes the row.
        #expect(ToolRowDetails.widths(600, chip: 900, facts: 0, spacing: 10) == (600, 0))
    }

    /// The row at 800 pt: an 8 pt inset, a 16 pt icon, the 64 pt title
    /// column, and 10 pt gaps put the chip at 108; the 10 pt chevron ends 8
    /// pt from the edge after a 10 pt gap, so chip and answer share 108...772
    /// less a 10 pt gap between them: half each, 327 pt, when both are long.
    @Test(arguments: [(question: question, long: false), (question: longQuestion, long: true)])
    func theChipShowsTheQuestionAndTheAnswerIsCut(question: String, long: Bool) async throws {
        let theme = MicaTheme(dark: false)
        let host = NSHostingView(rootView: ToolActivityView(tools: [Self.ask(question)], workspace: WorkspaceState(demo: false),
                                                            state: ToolDrawerState())
            .frame(width: 800).environment(\.mica, theme).background(theme.surface))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 800, height: 36))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        // The row's centre: 4 pt above it, 14 pt into it.
        try await eventually { Self.runs(host, y: 18, background: theme.surface).count > 3 }
        let runs = Self.runs(host, y: 18, background: theme.surface)
        // The chip's fill paints one unbroken run from 108.
        let box = try #require(runs.first { abs($0.lowerBound - 108) <= 1 }, "runs \(runs)")
        let font = Typography.shared.font(size: 11, mono: true)
        let natural = ceil((question as NSString).size(withAttributes: [.font: font]).width) + 12
        let allotted: CGFloat = long ? 327 : natural
        if long {
            // Cut at the end, the question fills its half less the last glyph.
            #expect(box.upperBound - box.lowerBound <= allotted && box.upperBound - box.lowerBound >= allotted - 12, "chip \(box)")
        } else {
            // The whole question.
            #expect(abs(box.upperBound - box.lowerBound - natural) <= 1, "chip \(box), natural \(natural)")
        }
        // The answer takes the rest after a 10 pt gap and ends by the chevron's
        // gap; cut to whole glyphs, it falls short of its share by under one.
        let answer = runs.filter { $0.lowerBound > box.upperBound + 5 && $0.upperBound < 778 }
        let first = try #require(answer.first), last = try #require(answer.last)
        #expect(first.lowerBound >= 108 + allotted + 10 && first.lowerBound <= 108 + allotted + 18, "answer starts \(first)")
        #expect(last.upperBound <= 772.5 && last.upperBound >= 756, "answer ends \(last)")
        // The chevron is the last thing painted, 8 pt from the edge.
        #expect(runs.last.map { $0.lowerBound >= 782 && $0.upperBound <= 792 } == true, "runs \(runs)")
    }

    /// The transcript column keeps its width below a long answer: the row,
    /// the assistant text inside the group, and the reply after it stay in
    /// 60...840 (780 pt centred in 900) or, full width, 32...868, with or
    /// without a legacy scroller in the right gutter.
    @Test(arguments: [(fullWidth: false, column: 60.0...840.0), (fullWidth: true, column: 32.0...868.0)],
          [NSScroller.Style.overlay, .legacy])
    func theColumnKeepsItsWidth(_ layout: (fullWidth: Bool, column: ClosedRange<CGFloat>),
                                scrollers: NSScroller.Style) async throws {
        let (fullWidth, column) = layout
        let theme = MicaTheme(dark: false)
        let run = ToolActivity(id: "run", name: "bash", summary: "", output: "ok", arguments: #"{"command":"ls"}"#,
                               failed: false, status: "Completed", thinking: Self.prose)
        let input = NativeTranscript(messages: [
            TranscriptMessage(id: "tools", role: "tools", text: "", tools: [Self.ask(Self.question), run]),
            TranscriptMessage(id: "after", role: "assistant", text: Self.prose + " " + Self.prose, tools: []),
        ], hasHistory: false, historyLoading: false, historyError: nil, active: false,
           presentation: TranscriptPresentationState(), workspace: WorkspaceState(demo: false),
           resumeRequest: 0, latestOutOfView: .constant(false), loadHistory: {}, theme: theme, fullWidth: fullWidth)
        let adapter = NativeTranscriptCoordinator(input)
        adapter.scroll.scrollerStyle = scrollers
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 900, height: 500))
        window.contentView = adapter.scroll
        window.orderFront(nil)
        defer { adapter.stop(); window.close() }
        adapter.receive(input)
        // The Ask row's centre: 12 pt cell padding, 4 pt, a 24 pt header, 2 pt, then 14 pt into the row.
        try await eventually { TranscriptWidthTests.ink(adapter.scroll, y: 56, background: theme.surface) != nil }
        try await Task.sleep(for: .milliseconds(100))
        let row = try #require(TranscriptWidthTests.ink(adapter.scroll, y: 56, background: theme.surface))
        // The icon is inset 10 pt; the chevron ends 8 pt inside the column, its glyph slightly short of that.
        #expect(abs(row.lowerBound - (column.lowerBound + 10)) <= 1, "row \(row)")
        #expect(row.upperBound <= column.upperBound - 8 && row.upperBound >= column.upperBound - 12, "row \(row)")
        var widest: CGFloat = 0, lines = 0
        for y in stride(from: 0, to: 500, by: 2) as StrideTo<CGFloat> {
            guard let ink = TranscriptWidthTests.ink(adapter.scroll, y: y, background: theme.surface) else { continue }
            lines += 1
            widest = max(widest, ink.upperBound)
            #expect(ink.lowerBound >= column.lowerBound - 1 && ink.upperBound <= column.upperBound + 1, "y \(y): \(ink)")
        }
        // The prose wraps at the column: its longest lines reach within a word of the edge.
        #expect(lines > 20)
        #expect(widest >= column.upperBound - 80, "widest \(widest)")
    }

    /// The runs of painted points on the line `y` points from the top of `view`.
    static func runs(_ view: NSView, y: CGFloat, background: Color) -> [ClosedRange<CGFloat>] {
        view.layoutSubtreeIfNeeded()
        guard let bitmap = view.bitmapImageRepForCachingDisplay(in: view.bounds),
              let surface = NSColor(background).usingColorSpace(.sRGB) else { return [] }
        view.cacheDisplay(in: view.bounds, to: bitmap)
        let scale = CGFloat(bitmap.pixelsWide) / view.bounds.width
        let row = Int(y * scale)
        func painted(_ x: Int) -> Bool {
            guard x < bitmap.pixelsWide, let color = bitmap.colorAt(x: x, y: row)?.usingColorSpace(.sRGB) else { return false }
            return abs(color.redComponent - surface.redComponent) > 0.02
                || abs(color.greenComponent - surface.greenComponent) > 0.02
                || abs(color.blueComponent - surface.blueComponent) > 0.02
        }
        var runs: [ClosedRange<CGFloat>] = [], start: Int?
        for x in 0...bitmap.pixelsWide {
            if painted(x) { if start == nil { start = x } } else if let first = start {
                runs.append(CGFloat(first) / scale...CGFloat(x) / scale)
                start = nil
            }
        }
        return runs
    }
}
