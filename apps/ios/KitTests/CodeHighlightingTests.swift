import Foundation
import KitHighlighting
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

struct CodeHighlightingTests {
    private func colored(_ text: String, _ runs: [(String, Color)]) -> AttributedString {
        var line = AttributedString(text)
        for (word, color) in runs {
            if let range = line.range(of: word) { line[range].foregroundColor = color }
        }
        return line
    }

    @Test func spansColorTheirLinesAndOneCanCrossALine() {
        let source = "let a = \"x\" /* one\ntwo */ f()\n"
        let spans = [
            SyntaxSpan(range: NSRange(location: 0, length: 3), role: "keyword"),
            SyntaxSpan(range: NSRange(location: 8, length: 3), role: "string"),
            SyntaxSpan(range: NSRange(location: 12, length: 13), role: "comment"),
            SyntaxSpan(range: NSRange(location: 26, length: 1), role: "function"),
            SyntaxSpan(range: NSRange(location: 4, length: 1), role: "variable")
        ]
        #expect(CodeColoring.lines(source, spans: spans) == [
            colored("let a = \"x\" /* one", [
                ("let", Theme.accent), ("\"x\"", Theme.success), ("/* one", Theme.muted), ("a", Theme.text)
            ]),
            colored("two */ f()", [("two */", Theme.muted), ("f", Theme.accent)])
        ])
    }

    @Test func codeWithoutSpansKeepsItsLines() {
        #expect(CodeColoring.lines("a\n\nb", spans: []) == [
            AttributedString("a"), AttributedString(""), AttributedString("b")
        ])
    }

    @Test func anEditColorsEachLineAsItsSideReads() {
        let lines = [
            ToolDiffLine(kind: .context, text: "let a = 1"),
            ToolDiffLine(kind: .removed, text: "var b = \"x\""),
            ToolDiffLine(kind: .added, text: "let b = \"y\"")
        ]
        let sides = CodeColoring.sides(lines)
        #expect(sides.old == "let a = 1\nvar b = \"x\"")
        #expect(sides.new == "let a = 1\nlet b = \"y\"")
        let old = [
            SyntaxSpan(range: NSRange(location: 0, length: 3), role: "keyword"),
            SyntaxSpan(range: NSRange(location: 10, length: 3), role: "keyword"),
            SyntaxSpan(range: NSRange(location: 18, length: 3), role: "string")
        ]
        let new = [
            SyntaxSpan(range: NSRange(location: 0, length: 3), role: "keyword"),
            SyntaxSpan(range: NSRange(location: 8, length: 1), role: "number"),
            SyntaxSpan(range: NSRange(location: 10, length: 3), role: "keyword"),
            SyntaxSpan(range: NSRange(location: 18, length: 3), role: "string")
        ]
        #expect(CodeColoring.diff(lines, oldSpans: old, newSpans: new) == [
            CodeLine(text: AttributedString("  ") + colored("let a = 1", [("let", Theme.accent), ("1", Theme.text)])),
            CodeLine(text: AttributedString("- ") + colored("var b = \"x\"", [("var", Theme.accent), ("\"x\"", Theme.success)]),
                     fill: .removed),
            CodeLine(text: AttributedString("+ ") + colored("let b = \"y\"", [("let", Theme.accent), ("\"y\"", Theme.success)]),
                     fill: .added)
        ])
        #expect(CodeColoring.diffText(lines) == "  let a = 1\n- var b = \"x\"\n+ let b = \"y\"")
    }

    @Test func theAppFindsItsBundledGrammars() async throws {
        let source = "func greet() {}"
        let spans = try await CodeSyntaxHighlighter.shared.spans(for: .init(source: source, language: "go"))
        let greet = (source as NSString).range(of: "greet")
        #expect(spans.contains(SyntaxSpan(range: NSRange(location: 0, length: 4), role: "keyword")))
        #expect(spans.contains(SyntaxSpan(range: greet, role: "function")))
    }
}
