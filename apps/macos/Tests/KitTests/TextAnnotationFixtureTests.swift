import Foundation
import KitTranscript
import Testing

/// Comments on selected text read and write the shared fixtures in
/// `testdata/text-annotations` (ADR 0070), as the terminal and browser
/// clients do.
struct TextAnnotationFixtureTests {
    /// `testdata/text-annotations` at the repository root, five levels above this file.
    static let directory = (0..<5).reduce(URL(fileURLWithPath: #filePath)) { url, _ in
        url.deletingLastPathComponent()
    }.appendingPathComponent("testdata/text-annotations")

    struct Case: Decodable {
        struct Comment: Decodable {
            let number: Int
            let comment: String
            let source: String
            let quote: String
            let cut: Bool
        }
        struct Input: Decodable {
            let comment: String
            let source: String
            let quote: String
            let nonce: String
        }
        let written: Bool
        let text: String
        let comments: [Comment]
        let input: [Input]?
    }

    static var names: [String] {
        let files = (try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []
        return files.filter { $0.hasSuffix(".json") }.map { String($0.dropLast(5)) }.sorted()
    }

    static func load(_ name: String) throws -> (message: String, expected: Case) {
        let raw = try String(contentsOf: directory.appendingPathComponent("\(name).txt"), encoding: .utf8)
        let message = String(raw.dropLast())
        let data = try Data(contentsOf: directory.appendingPathComponent("\(name).json"))
        return (message, try JSONDecoder().decode(Case.self, from: data))
    }

    @Test func fixturesArePresent() {
        #expect(Self.names.count >= 8)
    }

    @Test(arguments: names) func readsLikeTheFixture(_ name: String) throws {
        let (message, expected) = try Self.load(name)
        let parts = TextAnnotationText.split(message)
        #expect(parts.text == expected.text, "\(name)")
        #expect(parts.annotations == expected.comments.map {
            TextAnnotationText.Sent(number: $0.number, comment: $0.comment, source: $0.source, quote: $0.quote, cut: $0.cut)
        }, "\(name)")
    }

    @Test(arguments: names) func writesLikeTheFixture(_ name: String) throws {
        let (message, expected) = try Self.load(name)
        guard expected.written, let input = expected.input else { return }
        let items = input.map { TextAnnotation(comment: $0.comment, source: $0.source, quote: $0.quote, nonce: $0.nonce) }
        #expect(TextAnnotationText.join(expected.text, TextAnnotationText.text(items)) == message, "\(name)")
    }

    @Test func aLongQuoteIsCutAtACharacterBoundary() {
        let quote = String(repeating: "a", count: TextAnnotationText.maxQuoteBytes - 1) + "é tail"
        let trimmed = TextAnnotationText.trimmedQuote(quote)
        #expect(trimmed.cut)
        #expect(trimmed.text == String(repeating: "a", count: TextAnnotationText.maxQuoteBytes - 1))
        let block = TextAnnotationText.block(number: 1, TextAnnotation(comment: "Too long", source: "Scratchpad",
                                                                         quote: quote, nonce: "n1"))
        let sent = TextAnnotationText.split(block).annotations
        #expect(sent == [TextAnnotationText.Sent(number: 1, comment: "Too long", source: "Scratchpad",
                                                 quote: trimmed.text, cut: true)])
    }

    @Test func aSourceIsKeptToOneLine() {
        let block = TextAnnotationText.block(number: 1, TextAnnotation(comment: "Hm", source: "Tool output · a\nb",
                                                                         quote: "x", nonce: "n1"))
        #expect(TextAnnotationText.split(block).annotations.first?.source == "Tool output · a b")
    }
}
