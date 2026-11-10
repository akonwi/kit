import AppKit
import SwiftUI
import Testing
@testable import Kit

struct MarkdownTableSizingTests {
    @Test func twoColumnsFillTranscriptWidth() {
        // The screenshot regression: two 260-point columns in a 780-point outline.
        #expect(MarkdownTableSizing.columnWidth(viewport: 780, columns: 2) == 390)
        #expect(MarkdownTableSizing.columnWidth(viewport: 520, columns: 2) == 260)
    }

    @Test func columnsResizeWithViewport() {
        #expect(MarkdownTableSizing.columnWidth(viewport: 900, columns: 3) == 300)
        #expect(MarkdownTableSizing.columnWidth(viewport: 600, columns: 3) == 200)
        #expect(MarkdownTableSizing.columnWidth(viewport: 780, columns: 1) == 780)
    }

    @Test func narrowAndWideTablesScrollAtReadableWidths() {
        #expect(MarkdownTableSizing.columnWidth(viewport: 300, columns: 2) == 180)
        #expect(MarkdownTableSizing.columnWidth(viewport: 780, columns: 6) == 180)
    }
}

@MainActor struct MarkdownTableLayoutTests {
    static let table = """
    | Topic | `v1.md` / `protocol.schema.json` | Kit |
    | --- | --- | --- |
    | `kit/session/submit-message` params | `sessionId` required; no `idempotencyKey` | rejects `sessionId`; accepts `idempotencyKey` (ADR 0041) |
    | `kit/system/open-url` | platform opener | asks the session's attached clients (ADR 0049); rate limit `-32006` `rate_limited` |
    | Web mode (`ui.toast.requested`, `activate_chrome_contribution`) | specified | the browser client is a session API client; plugins see none of this |
    """

    /// A lazy stack measures rows that are off screen outside the scroll
    /// view that will hold them; a table measured that way must be as tall
    /// as it is on screen, or rows shift as they come into view.
    @Test func aTableMeasuresTheSameWhereverItIsHosted() {
        let theme = ThemeConfiguration.decode("").theme(dark: true)
        func height(window width: CGFloat?) -> CGFloat {
            let host = NSHostingView(rootView: MarkdownView(source: Self.table).environment(\.mica, theme)
                .frame(width: 600).fixedSize(horizontal: false, vertical: true))
            guard let width else { return host.fittingSize.height }
            let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: width, height: 900))
            window.contentView = host
            window.orderFront(nil)
            defer { window.close() }
            host.layoutSubtreeIfNeeded()
            return host.fittingSize.height
        }
        let unhosted = height(window: nil)
        #expect(height(window: 600) == unhosted)
        #expect(height(window: 1550) == unhosted)
    }
}
