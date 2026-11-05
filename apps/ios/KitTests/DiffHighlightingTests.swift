import Foundation
import KitClient
import KitHighlighting
import SwiftUI
import Testing
@testable import Kit

@MainActor
struct DiffHighlightingTests {
    /// The lines of `texts` colored as one Swift source.
    private func swift(_ texts: [String]) async throws -> [AttributedString] {
        let spans = try #require(await CodeColoring.spans(for: .init(source: texts.joined(separator: "\n"), language: "swift")))
        return CodeColoring.lines(texts, spans: spans)
    }

    /// Each run between gaps is colored per side: a removed line as the old
    /// code reads, added and unchanged lines as the new code. Gaps stay plain.
    @Test func eachLineIsColoredInTheCodeItBelongsTo() async throws {
        let document = try DiffCommentsTests.document()
        let old = try await swift(["func greet() {", "  print(1)", "}"])
        let new = try await swift(["func greet() {", "  print(2)", "  print(3)", "}"])
        let later = try await swift(["let a = 1", "let b = 2"])
        let colored = try #require(await DiffHighlighting.lines(document))
        #expect(colored == [new[0], old[1], new[1], new[2], new[3], nil, later[0], later[1]])

        let keyword = try #require(colored[6]?.range(of: "let"))
        #expect(colored[6]?[keyword].foregroundColor == Theme.accent)
    }

    @Test func aFileWithoutAGrammarStaysPlain() async throws {
        #expect(await DiffHighlighting.lines(try DiffCommentsTests.document(path: "README.md")) == nil)
    }

    /// The app finds the Ard grammar and its highlight query in its bundle.
    @Test func ardCodeIsColored() async throws {
        let source = "fn greet() Str {\n  \"Ard\"\n}"
        let spans = try #require(await CodeColoring.spans(for: .init(source: source, language: "ard")))
        let keyword = (source as NSString).range(of: "fn")
        #expect(spans.contains(SyntaxSpan(range: keyword, role: "keyword")))
    }

    @Test func extensionsNameTheirGrammar() {
        #expect(DiffHighlighting.language("cmd/main.go") == "go")
        #expect(DiffHighlighting.language("web/app.TSX") == "tsx")
        #expect(DiffHighlighting.language("scripts/build.mjs") == "javascript")
        #expect(DiffHighlighting.language("install.sh") == "bash")
        #expect(DiffHighlighting.language("app/models/user.rb") == "ruby")
        #expect(DiffHighlighting.language("std/list.ard") == "ard")
        #expect(DiffHighlighting.language("styles/site.css") == "css")
        #expect(DiffHighlighting.language("docs/README.md") == nil)
        #expect(DiffHighlighting.language("Makefile") == nil)
    }
}
