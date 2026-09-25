import Foundation
import Testing
@testable import Kit

struct CodeSyntaxHighlighterTests {
    @Test func goSignatureFragment() async throws {
        let source = "Watch(context.Context) (protocol.SessionSnapshot, EventStream, error)"
        let spans = try await CodeSyntaxHighlighter().spans(for: .init(source: source, language: "go"))
        let watchRange = (source as NSString).range(of: "Watch")
        #expect(spans.filter { $0.range == watchRange } == [.init(range: watchRange, role: "function")])
    }

    @Test(arguments: [
        ("bash", "echo \"hello\" # comment"),
        ("go", "package main\nfunc greet() { println(42) }"),
        ("json", "{\"hello\": 42}"),
        ("swift", "let greeting = \"Hello 🌿\" // comment"),
        ("python", "def greet():\n    return 42 # comment"),
        ("javascript", "function greet() { return 42; }"),
        ("typescript", "const count: number = 42;"),
        ("tsx", "const view = <Button title=\"hello\" />;")
    ])
    func bundledGrammarsHighlightCode(sample: (String, String)) async throws {
        let spans = try await CodeSyntaxHighlighter().spans(for: .init(source: sample.1, language: sample.0))
        #expect(!spans.isEmpty)
        #expect(spans.allSatisfy { NSMaxRange($0.range) <= sample.1.utf16.count })
    }

    @Test func unicodeAndSyntaxContextRetainCorrectRoles() async throws {
        let source = "let greeting = \"🌿 return\" // let 42"
        let spans = try await CodeSyntaxHighlighter().spans(for: .init(source: source, language: "swift"))
        func role(at word: String) -> String? {
            let range = (source as NSString).range(of: word)
            return spans.last { NSLocationInRange(range.location, $0.range) }?.role
        }
        #expect(role(at: "let") == "keyword")
        #expect(role(at: "🌿") == "string")
        #expect(role(at: "return") == "string")
        #expect(role(at: "42") == "comment")
    }

    @Test func aliasesAndPlainTextFallback() async throws {
        let highlighter = CodeSyntaxHighlighter()
        let source = "echo \"hi\""
        let shell = try await highlighter.spans(for: .init(source: source, language: "sh"))
        let bash = try await highlighter.spans(for: .init(source: source, language: "bash"))
        #expect(shell == bash)
        for label in ["", "text", "plaintext", "unknown"] {
            let spans = try await highlighter.spans(for: .init(source: source, language: label))
            #expect(spans == [])
        }
        #expect(CodeSyntaxHighlighter.role(for: "function.method") == "function")
        #expect(CodeSyntaxHighlighter.role(for: "variable.member") == "member")
        #expect(CodeSyntaxHighlighter.role(for: "operator") == "operator")
    }
}
