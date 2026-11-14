import AppKit
import SwiftUI
import Testing
@testable import Kit

/// A list item's text keeps every line. A list is placed at its height, and
/// its stack shares that height out among the items; an item whose paragraph
/// wraps above a sublist, beside an item that wraps as much, is offered less
/// than it needs, and its paragraph still draws every line instead of ending
/// in an ellipsis.
@MainActor struct MarkdownListLayoutTests {
    static let source = """
    - **What changed:**
      - Coming back from another screen returns to the reply you were reading instead of the end of the session, so a long answer keeps its place while you look at something else and then switch back again, in each of these:
        - Changes
        - Files
        - Browser
      - If a run is still going, or the last message in the session isn't a reply, the session opens at the end as before and follows new output as it arrives, so nothing changes while you watch a turn that the model is still working on.
    """

    /// A reply in the transcript: in a 700 pt pane, rows 636 pt wide at
    /// either setting, the heading takes a line, the first nested item three
    /// above its three, and the last three; in a 1440 pt pane, in the 780 pt
    /// column or the 1376 pt full width, the nested items take two each.
    @Test(arguments: [(pane: 700.0, fullWidth: false, lines: 10), (pane: 700.0, fullWidth: true, lines: 10),
                      (pane: 1440.0, fullWidth: false, lines: 8), (pane: 1440.0, fullWidth: true, lines: 8)])
    func aReplyDrawsEveryLineOfANestedItem(_ layout: (pane: Double, fullWidth: Bool, lines: Int)) async throws {
        let theme = MicaTheme(dark: false)
        let input = NativeTranscript(messages: [
            TranscriptMessage(id: "reply", role: "assistant", text: Self.source, tools: [])
        ], hasHistory: false, historyLoading: false, historyError: nil, active: false,
           presentation: TranscriptPresentationState(), workspace: WorkspaceState(demo: false),
           resumeRequest: 0, latestOutOfView: .constant(false), loadHistory: {}, theme: theme, fullWidth: layout.fullWidth)
        let adapter = NativeTranscriptCoordinator(input)
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: layout.pane, height: 600))
        window.contentView = adapter.scroll
        window.orderFront(nil)
        defer { adapter.stop(); window.close() }
        adapter.receive(input)
        // Rows start at a 90 pt estimate until they're measured.
        try await eventually { adapter.table.numberOfRows == 1 && adapter.table.rect(ofRow: 0).height != 90 }
        let lines = try Self.lines(adapter.scroll)
        #expect(lines == layout.lines)
    }

    /// A Markdown file's preview: in a 700 pt pane, its text 652 pt wide
    /// less the comment margin, the nested items take three lines each; in
    /// a 1440 pt pane, in the 844 pt column or the full width, two.
    @Test(arguments: [(pane: 700.0, fullWidth: false, lines: 10), (pane: 1440.0, fullWidth: false, lines: 8),
                      (pane: 1440.0, fullWidth: true, lines: 8)])
    func aMarkdownFileDrawsEveryLineOfANestedItem(_ layout: (pane: Double, fullWidth: Bool, lines: Int)) async throws {
        let theme = MicaTheme(dark: false)
        let host = NSHostingView(rootView: MarkdownDocumentView(source: Self.source)
            .environment(\.mica, theme).environment(\.transcriptFullWidth, layout.fullWidth)
            .foregroundStyle(theme.text).background(theme.surface))
        let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: layout.pane, height: 600))
        window.contentView = host
        window.orderFront(nil)
        defer { window.close() }
        // The document parses off the main actor before it shows.
        try await eventually { (try? Self.lines(host)) ?? 0 > 0 }
        let lines = try Self.lines(host)
        #expect(lines == layout.lines)
    }

    /// The bands of text drawn in `view`, top to bottom.
    static func lines(_ view: NSView) throws -> Int {
        view.layoutSubtreeIfNeeded()
        let rep = try #require(view.bitmapImageRepForCachingDisplay(in: view.bounds))
        view.cacheDisplay(in: view.bounds, to: rep)
        let image = try #require(rep.cgImage)
        let width = image.width, height = image.height
        var pixels = [UInt8](repeating: 0, count: width * height * 4)
        try pixels.withUnsafeMutableBytes { buffer in
            let context = try #require(CGContext(data: buffer.baseAddress, width: width, height: height,
                bitsPerComponent: 8, bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(),
                bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
            context.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
        }
        // Text is dark on the light surface.
        func ink(_ y: Int) -> Bool {
            (0..<width).contains { x in
                let i = (y * width + x) * 4
                return pixels[i] < 140 && pixels[i + 1] < 140 && pixels[i + 2] < 140
            }
        }
        var count = 0, inLine = false
        for y in 0..<height {
            let painted = ink(y)
            if painted, !inLine { count += 1 }
            inLine = painted
        }
        return count
    }
}
