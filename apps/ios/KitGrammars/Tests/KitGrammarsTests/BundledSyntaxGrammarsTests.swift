import Foundation
import KitGrammars
import KitHighlighting
import Testing

struct BundledSyntaxGrammarsTests {
    private let highlighter = CodeSyntaxHighlighter(grammars: BundledSyntaxGrammars())

    /// The role that colors the first occurrence of `word`.
    private func role(of word: String, in source: String, language: String) async throws -> String? {
        let spans = try await highlighter.spans(for: .init(source: source, language: language))
        let range = (source as NSString).range(of: word)
        return spans.last { NSLocationInRange(range.location, $0.range) }?.role
    }

    @Test(arguments: [
        ("bash", "echo \"hello\" # note", "echo", "function"),
        ("bash", "echo \"hello\" # note", "\"hello\"", "string"),
        ("bash", "echo \"hello\" # note", "# note", "comment"),
        ("go", "package main\nfunc greet() { println(42) }", "func", "keyword"),
        ("go", "package main\nfunc greet() { println(42) }", "greet", "function"),
        ("go", "package main\nfunc greet() { println(42) }", "42", "number"),
        ("json", "{\"hello\": 42}", "\"hello\"", "string"),
        ("json", "{\"hello\": 42}", "42", "number"),
        ("swift", "let greeting = \"Hello\" // note", "let", "keyword"),
        ("swift", "let greeting = \"Hello\" // note", "Hello", "string"),
        ("swift", "let greeting = \"Hello\" // note", "// note", "comment"),
        ("python", "def greet():\n    return 42 # note", "def", "keyword"),
        ("python", "def greet():\n    return 42 # note", "greet", "function"),
        ("python", "def greet():\n    return 42 # note", "# note", "comment"),
        ("javascript", "function greet() { return 42; }", "function", "keyword"),
        ("javascript", "function greet() { return 42; }", "greet", "function"),
        ("typescript", "const count: number = 42;", "const", "keyword"),
        ("typescript", "const count: number = 42;", "number", "keywordType"),
        ("tsx", "const view = <Button title=\"hello\" />;", "Button", "type"),
        ("tsx", "const view = <Button title=\"hello\" />;", "title", "attribute"),
        ("ruby", "def greet\n  puts \"hello\" # note\nend", "def", "keyword"),
        ("ruby", "def greet\n  puts \"hello\" # note\nend", "\"hello\"", "string"),
        ("ruby", "def greet\n  puts \"hello\" # note\nend", "# note", "comment"),
        ("css", ".card { color: #fff; } /* note */", "color", "member"),
        ("css", ".card { color: #fff; } /* note */", "/* note */", "comment"),
        ("ard", "fn greet() Str {\n  let name = \"Ard\" // note\n  name\n}", "fn", "keyword"),
        ("ard", "fn greet() Str {\n  let name = \"Ard\" // note\n  name\n}", "greet", "function"),
        ("ard", "fn greet() Str {\n  let name = \"Ard\" // note\n  name\n}", "Str", "keywordType"),
        ("ard", "fn greet() Str {\n  let name = \"Ard\" // note\n  name\n}", "\"Ard\"", "string"),
        ("ard", "fn greet() Str {\n  let name = \"Ard\" // note\n  name\n}", "// note", "comment")
    ])
    func grammarsColorEachLanguage(sample: (String, String, String, String)) async throws {
        let (language, source, word, expected) = sample
        #expect(try await role(of: word, in: source, language: language) == expected)
    }

    @Test func unicodeAndSyntaxContextRetainCorrectRoles() async throws {
        let source = "let greeting = \"🌿 return\" // let 42"
        #expect(try await role(of: "let", in: source, language: "swift") == "keyword")
        #expect(try await role(of: "🌿", in: source, language: "swift") == "string")
        #expect(try await role(of: "return", in: source, language: "swift") == "string")
        #expect(try await role(of: "42", in: source, language: "swift") == "comment")
    }

    @Test func aliasesShareAGrammar() async throws {
        let source = "echo \"hi\""
        let shell = try await highlighter.spans(for: .init(source: source, language: "sh"))
        let bash = try await highlighter.spans(for: .init(source: source, language: "bash"))
        #expect(shell == bash)
        #expect(!bash.isEmpty)
    }
}
