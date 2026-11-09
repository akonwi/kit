import KitClient
import KitTranscript
import SwiftUI
import Testing
import UIKit
@testable import Kit

/// An Ask row with a long free-text answer stays on one line within its
/// row on a phone: the question chip leads, the answer is cut at the end,
/// and neither pushes the transcript past the screen.
@MainActor struct AskRowLayoutTests {
    nonisolated static let question = "Who saw it?"
    nonisolated static let longQuestion = "Who reported the Appointments page failing, and what exactly did they see?"
    static let answer = "for Erika Renteria, she's a clinician and said that the Appointments page in the rails app doesn't load for her at all, it just spins forever and then shows a blank page"

    static func ask(_ question: String) -> ToolActivity {
        ToolActivity(id: "ask", name: "input_from_user", summary: "", output: #"{"value":"\#(answer)"}"#,
                     arguments: #"{"title":"\#(question)"}"#, failed: false, status: "Completed")
    }

    /// Chip and facts share what's left of the row after the title: the chip
    /// keeps its width up to half of it, or more beside short facts.
    @Test func chipAndFactsShareTheRow() {
        #expect(ToolRowDetails.widths(300, chip: 100, facts: 150, spacing: 8) == (100, 150))
        #expect(ToolRowDetails.widths(300, chip: 100, facts: 900, spacing: 8) == (100, 192))
        #expect(ToolRowDetails.widths(300, chip: 700, facts: 900, spacing: 8) == (146, 146))
        #expect(ToolRowDetails.widths(300, chip: 900, facts: 40, spacing: 8) == (252, 40))
    }

    /// The row in a 353 pt column, the width of a 393 pt phone's transcript:
    /// a 16 pt icon, the title, and 8 pt gaps put the chip after the title;
    /// chip and answer share the rest less an 8 pt gap.
    @Test(arguments: [(question: question, long: false), (question: longQuestion, long: true)])
    func theChipShowsTheQuestionAndTheAnswerIsCut(question: String, long: Bool) async throws {
        let image = try await Self.render(ToolBatch(tools: [Self.ask(question)], live: false).frame(width: 353, alignment: .leading)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading).ignoresSafeArea(),
                                           size: CGSize(width: 353, height: 200))
        // The row's centre: a lone call is its 32 pt row.
        let runs = Self.runs(image, y: 16)
        let title = ceil(("Ask" as NSString).size(withAttributes: [.font: UIFont.systemFont(ofSize: 15)]).width)
        let start = 16 + 8 + title + 8
        let room = 353 - start - 8
        let natural = ceil((question as NSString).size(withAttributes: [
            .font: UIFont.monospacedSystemFont(ofSize: 12, weight: .regular)]).width) + 12
        let allotted = long ? room / 2 : natural
        // The chip's fill paints one unbroken run from the end of the title's gap.
        let box = try #require(runs.first { abs($0.lowerBound - start) <= 1 }, "runs \(runs), chip at \(start)")
        if long {
            // Cut at the end, the question fills its half less the last glyph.
            #expect(box.upperBound - box.lowerBound <= allotted + 1 && box.upperBound - box.lowerBound >= allotted - 12, "chip \(box)")
        } else {
            #expect(abs(box.upperBound - box.lowerBound - natural) <= 1, "chip \(box), natural \(natural)")
        }
        // The answer takes the rest after an 8 pt gap and ends at the row's
        // edge; cut to whole glyphs, it falls short of its share by under one.
        let answer = runs.filter { $0.lowerBound > box.upperBound + 4 }
        let first = try #require(answer.first), last = try #require(answer.last)
        let answerStart = start + allotted + 8
        #expect(first.lowerBound >= answerStart && first.lowerBound <= answerStart + 18, "answer starts \(first)")
        #expect(last.upperBound <= 353 && last.upperBound >= 337, "answer ends \(last)")
    }

    /// Every row of the transcript, the Ask row's included, stays in its
    /// 20 pt insets on a 393 pt screen.
    @Test func theTranscriptStaysOnScreen() async throws {
        let read = ToolActivity(id: "read", name: "read", summary: "", output: "ok", arguments: #"{"path":"a.go"}"#,
                                failed: false, status: "Completed")
        let messages = [
            TranscriptMessage(id: "tools", role: "tools", text: "", tools: [Self.ask(Self.longQuestion), read]),
            TranscriptMessage(id: "after", role: "assistant",
                              text: "Next I'll check whether Datadog RUM is available through the integration so I can see her browser session.", tools: []),
        ]
        let image = try await Self.render(TranscriptList(messages: messages, active: false) { EmptyView() },
                                           size: CGSize(width: 393, height: 400))
        var lines = 0, leading = CGFloat.infinity
        for y in stride(from: 0, to: 400, by: 2) as StrideTo<CGFloat> {
            let runs = Self.runs(image, y: y)
            guard let first = runs.first, let last = runs.last else { continue }
            lines += 1
            leading = min(leading, first.lowerBound)
            // A glyph's antialiased edge may reach a point past the inset.
            #expect(first.lowerBound >= 19 && last.upperBound <= 374, "y \(y): \(first.lowerBound)...\(last.upperBound)")
        }
        #expect(lines > 20)
        // The rows are aligned at the inset.
        #expect(leading >= 19 && leading <= 21, "leading \(leading)")
    }

    /// `view` drawn in a light window of `size`.
    static func render(_ view: some View, size: CGSize) async throws -> UIImage {
        let window = UIWindow(frame: CGRect(origin: .zero, size: size))
        window.overrideUserInterfaceStyle = .light
        window.rootViewController = UIHostingController(rootView: view
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading).background(Theme.surface))
        window.makeKeyAndVisible()
        defer { window.isHidden = true }
        try await Task.sleep(for: .milliseconds(500))
        window.layoutIfNeeded()
        let format = UIGraphicsImageRendererFormat()
        format.scale = 2
        let image = UIGraphicsImageRenderer(bounds: window.bounds, format: format).image { context in
            window.layer.render(in: context.cgContext)
        }
        return image
    }

    /// The runs of painted points, unlike white, on the line `y` points from the top.
    static func runs(_ image: UIImage, y: CGFloat) -> [ClosedRange<CGFloat>] {
        guard let cg = image.cgImage else { return [] }
        let width = cg.width, height = cg.height, scale = CGFloat(width) / image.size.width
        var pixels = [UInt8](repeating: 0, count: width * height * 4)
        let context = CGContext(data: &pixels, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
                                space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        context?.draw(cg, in: CGRect(x: 0, y: 0, width: width, height: height))
        let row = Int(y * scale)
        func painted(_ x: Int) -> Bool {
            guard x < width else { return false }
            let i = (row * width + x) * 4
            return pixels[i] < 250 || pixels[i + 1] < 250 || pixels[i + 2] < 250
        }
        var runs: [ClosedRange<CGFloat>] = [], start: Int?
        for x in 0...width {
            if painted(x) { if start == nil { start = x } } else if let first = start {
                runs.append(CGFloat(first) / scale...CGFloat(x) / scale)
                start = nil
            }
        }
        return runs
    }
}
