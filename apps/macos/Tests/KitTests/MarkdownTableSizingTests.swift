import Foundation
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
