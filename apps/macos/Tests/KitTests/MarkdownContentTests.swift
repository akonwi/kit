import Foundation
import Testing
@testable import Kit

struct MarkdownContentTests {
    @Test func indentedFenceInsideListRetainsCodeAndDiagramSpacing() {
        let source = """
        - Desired structure:
          ```text
          Main events ─┐
                       ├─→ TranscriptView
          Child events ┘
          ```
        """
        #expect(MarkdownContent(source: source).blocks == [
            .list([.init(marker: "•", blocks: [
                .paragraph(AttributedString("Desired structure:")),
                .code("text", "Main events ─┐\n             ├─→ TranscriptView\nChild events ┘\n")
            ])])
        ])
    }

    @Test func longerFencesKeepEmbeddedBackticksAndUnclosedFencesKeepCode() {
        #expect(MarkdownContent(source: "````markdown\n```swift\nlet x = 1\n```\n````").blocks == [
            .code("markdown", "```swift\nlet x = 1\n```\n")
        ])
        #expect(MarkdownContent(source: "~~~swift\nlet x = 1").blocks == [.code("swift", "let x = 1\n")])
    }

    @Test func orderedListsQuotesAndTasksRetainHierarchy() {
        let source = """
        3. Third
           - [x] Done
           - [ ] Next
        4. Fourth

        > Quoted
        >
        > Second paragraph

        ---
        """
        #expect(MarkdownContent(source: source).blocks == [
            .list([
                .init(marker: "3.", blocks: [.paragraph(AttributedString("Third")), .list([
                    .init(marker: "☑", blocks: [.paragraph(AttributedString("Done"))]),
                    .init(marker: "☐", blocks: [.paragraph(AttributedString("Next"))])
                ])]),
                .init(marker: "4.", blocks: [.paragraph(AttributedString("Fourth"))])
            ]),
            .quote([.paragraph(AttributedString("Quoted")), .paragraph(AttributedString("Second paragraph"))]),
            .rule
        ])
    }

    @Test func tablesRetainCellsAndColumnAlignment() {
        #expect(MarkdownContent(source: "| Name | Count |\n| :--- | ---: |\n| Kit | 2 |").blocks == [
            .table(.init(alignments: [.leading, .trailing],
                         header: [AttributedString("Name"), AttributedString("Count")],
                         rows: [[AttributedString("Kit"), AttributedString("2")]]))
        ])
    }

    @Test func inlineFormattingLinksAndBreaksAreProjected() throws {
        let content = MarkdownContent(source: "**Bold and *italic*** with `code`, ~~old~~ and [Kit](https://example.com).  \nNext")
        guard case .paragraph(let text) = try #require(content.blocks.first) else {
            Issue.record("Expected a paragraph"); return
        }
        #expect(String(text.characters) == "Bold and italic with code, old and Kit.\nNext")
        let italic = try #require(text.runs.first { String(text[$0.range].characters) == "italic" })
        #expect(italic.inlinePresentationIntent == [.stronglyEmphasized, .emphasized])
        let code = try #require(text.runs.first { String(text[$0.range].characters) == "code" })
        #expect(code.inlinePresentationIntent == .code)
        let old = try #require(text.runs.first { String(text[$0.range].characters) == "old" })
        #expect(old.inlinePresentationIntent == .strikethrough)
        let link = try #require(text.runs.first { $0.link != nil })
        #expect(link.link == URL(string: "https://example.com"))
    }

    @Test func htmlAndImagesHaveNativeTextFallbacks() {
        #expect(MarkdownContent(source: "![Diagram](https://example.com/image.png)").blocks == [
            .paragraph(AttributedString("[Image: Diagram]"))
        ])
        #expect(MarkdownContent(source: "<div>Example</div>").blocks == [.code("html", "<div>Example</div>\n")])
        #expect(MarkdownContent(source: "[Readable](javascript:example)").blocks == [
            .paragraph(AttributedString("Readable"))
        ])
    }
}
