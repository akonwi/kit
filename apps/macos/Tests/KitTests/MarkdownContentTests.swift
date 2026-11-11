import Foundation
import KitTranscript
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

    /// Text whose runs link where a URL is given.
    private static func text(_ runs: [(String, String?)]) -> AttributedString {
        runs.reduce(into: AttributedString()) { result, run in
            var part = AttributedString(run.0)
            part.link = run.1.flatMap(URL.init(string:))
            result += part
        }
    }

    @Test(arguments: [
        ("See https://example.com/guide.", [("See ", nil), ("https://example.com/guide", "https://example.com/guide"), (".", nil)]),
        ("Is it at http://localhost:5173/settings?", [("Is it at ", nil), ("http://localhost:5173/settings", "http://localhost:5173/settings"), ("?", nil)]),
        ("(See https://en.wikipedia.org/wiki/Swift_(programming_language))", [
            ("(See ", nil),
            ("https://en.wikipedia.org/wiki/Swift_(programming_language)", "https://en.wikipedia.org/wiki/Swift_(programming_language)"),
            (")", nil)
        ]),
        ("Both HTTPS://Example.com/a, and https://example.com/b?q=1#top: done", [
            ("Both ", nil), ("HTTPS://Example.com/a", "HTTPS://Example.com/a"), (", and ", nil),
            ("https://example.com/b?q=1#top", "https://example.com/b?q=1#top"), (": done", nil)
        ]),
        ("Ends at https://example.com.", [("Ends at ", nil), ("https://example.com", "https://example.com"), (".", nil)]),
        ("Not xhttps://example.com, ftp://example.com, https://, or https://exa_mple.com", [
            ("Not xhttps://example.com, ftp://example.com, https://, or https://exa_mple.com", nil)
        ]),
        // What follows another link is a word, not an address.
        ("[label](https://target.example.com)https://example.com", [
            ("label", "https://target.example.com"), ("https://example.com", nil)
        ])
    ] as [(String, [(String, String?)])])
    func bareWebAddressesInProseAreLinksAsGitHubLinksThem(source: String, runs: [(String, String?)]) {
        #expect(MarkdownContent(source: source).blocks == [.paragraph(Self.text(runs))])
    }

    @Test func bareWebAddressesAreLinkedInEveryKindOfProseButNotInCode() throws {
        let source = """
        # Docs at https://example.com/docs

        - Item *https://example.com/em* and `https://example.com/code`
        - [https://shown.example.com](https://target.example.com)

        ```sh
        curl https://example.com/fenced
        ```
        """
        let blocks = MarkdownContent(source: source).blocks
        #expect(blocks.count == 3)
        #expect(blocks.first == .heading(1, Self.text([("Docs at ", nil), ("https://example.com/docs", "https://example.com/docs")])))
        guard case .list(let items) = blocks.dropFirst().first, items.count == 2,
              case .paragraph(let item) = items[0].blocks.first, case .paragraph(let labeled) = items[1].blocks.first else {
            Issue.record("Expected a list of two paragraphs"); return
        }
        let runs = item.runs.map { (String(item[$0.range].characters), $0.link, $0.inlinePresentationIntent) }
        #expect(runs.map(\.0) == ["Item ", "https://example.com/em", " and ", "https://example.com/code"])
        #expect(runs.map(\.1) == [nil, URL(string: "https://example.com/em"), nil, nil])
        #expect(runs.map(\.2) == [nil, .emphasized, nil, .code])
        // A link's text is its own: a URL in it doesn't link elsewhere.
        #expect(labeled == Self.text([("https://shown.example.com", "https://target.example.com")]))
        #expect(blocks.last == .code("sh", "curl https://example.com/fenced\n"))
    }

    @Test func anAddressRightAfterACodeSpanIsntOne() throws {
        guard case .paragraph(let text) = try #require(MarkdownContent(source: "`code`https://example.com").blocks.first) else {
            Issue.record("Expected a paragraph"); return
        }
        #expect(text.runs.map { (String(text[$0.range].characters), $0.link, $0.inlinePresentationIntent) }.map { "\($0.0)|\($0.1?.absoluteString ?? "-")|\($0.2 == .code)" }
                == ["code|-|true", "https://example.com|-|false"])
    }

    @Test func textFullOfAddressesThatArentIsReadOnce() {
        // Each candidate's host is checked before the rest of the word is read.
        let word = String(repeating: "http://bad_host(", count: 5_000)
        let clock = ContinuousClock()
        let elapsed = clock.measure { _ = MarkdownContent(source: "See " + word + " and https://example.com") }
        #expect(elapsed < .seconds(1))
    }
}
