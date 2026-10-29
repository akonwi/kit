import Foundation
import KitFigures
import Testing
@testable import Kit

struct FigurePresentationTests {
    @Test func aClosedFigureFenceIsParsedFromItsWholeInfoString() throws {
        let content = MarkdownContent(source: "Routes:\n\n```bars title=\"Routes\"\n- /docs: 12,400\n- **/plot: 860**\n```\n")
        guard case .figure(let block) = content.blocks.last else {
            Issue.record("expected a figure, got \(content.blocks)")
            return
        }
        #expect(block.name == "bars")
        #expect(block.fenced == "```bars title=\"Routes\"\n- /docs: 12,400\n- **/plot: 860**\n```")
        let figure = try block.figure.get()
        #expect(figure.title == "Routes")
        #expect(figure.items.map(\.label) == ["/docs", "/plot"])
        #expect(figure.items.map(\.scale) == [1000, 69])
        #expect(figure.items.map(\.accent) == [false, true])
    }

    @Test func aFigureThatIsNotDrawnKeepsItsSourceAndReason() throws {
        let content = MarkdownContent(source: "```bars sort=\"desc\"\n- a: 1\n```\n")
        guard case .figure(let block) = content.blocks.first else {
            Issue.record("expected a figure, got \(content.blocks)")
            return
        }
        #expect(block.figure == .failure(KitFigures.NotDrawn("`bars` has no `sort` attribute")))
    }

    /// The terminal client's server decodes the info string the same way
    /// (`internal/markdown`), so both read the same attributes.
    @Test(arguments: [
        ("```bars title=\"R&amp;D\"\n- a: 1\n```", "bars title=\"R&D\""),
        ("```bars title=\"&quot;x&quot;\"\n- a: 1\n```", "bars title=\"\"x\"\""),
        ("```bars title=\"&#35;1 &#x41;\"\n- a: 1\n```", "bars title=\"#1 A\""),
        ("```bars title=\"&amp;#35;\"\n- a: 1\n```", "bars title=\"&#35;\""),
        ("```bars title=\"a\\_b \\q\"\n- a: 1\n```", "bars title=\"a_b \\q\""),
        ("```bars title=\"&nosuch; & x\"\n- a: 1\n```", "bars title=\"&nosuch; & x\""),
    ])
    func aFiguresInfoStringIsDecodedAsCommonMarkDoes(source: String, info: String) {
        guard case .figure(let block) = MarkdownContent(source: source).blocks.first else {
            Issue.record("expected a figure from \(source)")
            return
        }
        #expect(block.info == info)
    }

    @Test func aStreamingFigureShowsItsSourceUntilItsFenceCloses() {
        #expect(MarkdownContent(source: "```stat\n- 12 open").blocks == [.code("stat", "- 12 open\n")])
    }

    @Test func otherFencesKeepTheirFirstWordAsTheirLanguage() {
        #expect(MarkdownContent(source: "```python file=\"a.py\"\nx = 1\n```").blocks == [.code("python", "x = 1\n")])
        #expect(MarkdownContent(source: "```plot\n- a\n```").blocks == [.code("plot", "- a\n")])
        guard case .mermaid = MarkdownContent(source: "```mermaid title=\"Flow\"\nflowchart LR\nA --> B\n```").blocks.first else {
            Issue.record("a Mermaid fence with more after its language is still a diagram")
            return
        }
    }

    @Test func githubAlertsBecomeCalloutsWithoutTheirMarker() {
        #expect(MarkdownContent(source: "> [!WARNING] Breaking change\n> Edit the **source**.").blocks == [
            .callout(Callout(kind: .warning, title: "Breaking change", blocks: [
                .paragraph(AttributedString("Edit the ") + bold("source") + AttributedString(".")),
            ])),
        ])
        #expect(MarkdownContent(source: "> [!tip]\n>\n> - first").blocks == [
            .callout(Callout(kind: .tip, title: "", blocks: [.list([.init(marker: "•", blocks: [.paragraph(AttributedString("first"))])])])),
        ])
        #expect(MarkdownContent(source: "> [!NOTE]").blocks == [.callout(Callout(kind: .note, title: "", blocks: []))])
    }

    @Test func otherQuotesStayQuotes() {
        #expect(MarkdownContent(source: "> [!TODO]\n> not an alert").blocks == [
            .quote([.paragraph(AttributedString("[!TODO] not an alert"))]),
        ])
        #expect(MarkdownContent(source: "> body\n> [!NOTE]").blocks == [
            .quote([.paragraph(AttributedString("body [!NOTE]"))]),
        ])
    }

    @Test func calloutsReadAsPlainTextWithTheirKind() {
        #expect(MessageText.plain("> [!CAUTION] Careful\n> Removing a kind breaks transcripts.") ==
            "Caution · Careful\n\nRemoving a kind breaks transcripts.")
    }

    private func bold(_ text: String) -> AttributedString {
        var value = AttributedString(text)
        value.inlinePresentationIntent = .stronglyEmphasized
        return value
    }
}
