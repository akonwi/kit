import Foundation
import SwiftTreeSitter
import CodeEditLanguages
import KitHighlighting

/// The grammars and highlight queries bundled with CodeEditLanguages, which
/// the file and scratchpad editors also use.
struct CodeEditSyntaxGrammars: SyntaxGrammars {
    func grammar(named name: String) throws -> SyntaxGrammar {
        let grammar: CodeLanguage
        switch name {
        case "bash": grammar = .bash
        case "go": grammar = .go
        case "json": grammar = .json
        case "swift": grammar = .swift
        case "python": grammar = .python
        case "javascript": grammar = .jsx
        case "typescript": grammar = .typescript
        default: grammar = .tsx
        }
        guard let language = grammar.language, let queryURL = grammar.queryURL else {
            throw CocoaError(.fileReadNoSuchFile)
        }
        var urls: [URL] = []
        if let parent = grammar.parentQueryURL { urls.append(parent) }
        if name == "tsx", let javascriptURL = CodeLanguage.jsx.queryURL {
            urls.append(javascriptURL.deletingLastPathComponent().appendingPathComponent("highlights-jsx.scm"))
        }
        urls.append(queryURL)
        for name in (grammar.additionalHighlights ?? []).sorted() where name != "injections" {
            urls.append(queryURL.deletingLastPathComponent().appendingPathComponent(name + ".scm"))
        }
        let source = try urls.map { try String(contentsOf: $0, encoding: .utf8) }.joined(separator: "\n")
        let query = try Query(language: language, data: Data(source.utf8))
        return SyntaxGrammar(language: language, query: query)
    }
}

extension CodeSyntaxHighlighter {
    static let shared = CodeSyntaxHighlighter()

    init() {
        self.init(grammars: CodeEditSyntaxGrammars())
    }
}
