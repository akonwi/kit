import Foundation
import SwiftTreeSitter
import CodeEditLanguages

struct SyntaxSpan: Equatable, Sendable {
    let range: NSRange
    let role: String
}

struct CodeHighlightRequest: Hashable, Sendable {
    let source: String
    let language: String
}

/// Parse off the main actor; cache captures independently of the active theme.
actor CodeSyntaxHighlighter {
    static let shared = CodeSyntaxHighlighter()
    private var configurations: [String: (Language, Query)] = [:]
    private var cache: [CodeHighlightRequest: [SyntaxSpan]] = [:]
    private var order: [CodeHighlightRequest] = []
    private var cacheBytes = 0

    static func canonicalLanguage(_ label: String) -> String? {
        let first = label.trimmingCharacters(in: .whitespacesAndNewlines)
            .split(whereSeparator: { $0.isWhitespace }).first.map(String.init)?.lowercased() ?? ""
        switch first {
        case "sh", "shell", "bash", "zsh": return "bash"
        case "go", "golang": return "go"
        case "json": return "json"
        case "swift": return "swift"
        case "py", "python", "python3": return "python"
        case "js", "javascript", "jsx": return "javascript"
        case "ts", "typescript": return "typescript"
        case "tsx": return "tsx"
        default: return nil
        }
    }

    func spans(for request: CodeHighlightRequest) throws -> [SyntaxSpan] {
        guard let name = Self.canonicalLanguage(request.language), !request.source.isEmpty else { return [] }
        // Oversized blocks still render in full, but without blocking on highlighting.
        guard request.source.utf8.count <= 256 * 1024 else { return [] }
        let key = CodeHighlightRequest(source: request.source, language: name)
        if let cached = cache[key] { return cached }
        let (language, query) = try configuration(for: name)
        let parser = Parser()
        parser.timeout = 0.1
        try parser.setLanguage(language)
        guard let tree = parser.parse(request.source) else { return [] }
        let captures = query.execute(in: tree).resolve(with: .init(string: request.source)).highlights()
        let length = request.source.utf16.count
        let candidates = captures.compactMap { capture -> SyntaxSpan? in
            guard capture.range.location >= 0, capture.range.length > 0,
                  capture.range.location <= length, capture.range.length <= length - capture.range.location,
                  let role = Self.role(for: capture.name) else { return nil }
            return SyntaxSpan(range: capture.range, role: role)
        }
        // Grammars can capture one node as both a generic identifier and a
        // semantic function/type. Query order must not erase the semantic role.
        var preferred: [NSRange: Int] = [:]
        for (index, span) in candidates.enumerated() {
            if let previous = preferred[span.range],
               Self.priority(candidates[previous].role) > Self.priority(span.role) { continue }
            preferred[span.range] = index
        }
        let spans = candidates.enumerated().compactMap { index, span in
            preferred[span.range] == index ? span : nil
        }
        while order.count >= 64 || cacheBytes + key.source.utf8.count > 2 * 1024 * 1024 {
            guard !order.isEmpty else { break }
            let oldest = order.removeFirst()
            cacheBytes -= oldest.source.utf8.count
            cache.removeValue(forKey: oldest)
        }
        order.append(key)
        cacheBytes += key.source.utf8.count
        cache[key] = spans
        return spans
    }

    private static func priority(_ role: String) -> Int {
        switch role {
        case "variable": return 0
        case "member": return 1
        case "function", "type", "keywordType": return 2
        case "builtin": return 3
        default: return 1
        }
    }

    static func role(for capture: String) -> String? {
        if capture == "keyword.type" || capture == "type.builtin" { return "keywordType" }
        if capture.hasPrefix("function.builtin") || capture.hasPrefix("variable.builtin") ||
            capture.hasPrefix("constant.builtin") { return "builtin" }
        if capture.hasPrefix("string.escape") || capture == "escape" { return "escape" }
        if capture == "property" || capture.hasPrefix("variable.member") { return "member" }
        if capture.hasPrefix("tag.attribute") { return "tagAttribute" }
        if capture.hasPrefix("tag.delimiter") { return "tagDelimiter" }
        switch capture.split(separator: ".").first.map(String.init) {
        case "comment": return "comment"
        case "keyword", "conditional", "repeat", "include", "exception", "boolean": return "keyword"
        case "function", "method", "constructor": return "function"
        case "type": return "type"
        case "variable", "parameter": return "variable"
        case "constant": return "builtin"
        case "number", "float": return "number"
        case "string", "character": return "string"
        case "operator": return "operator"
        case "punctuation": return "punctuation"
        case "attribute": return "attribute"
        case "label": return "label"
        case "tag": return "tag"
        default: return nil
        }
    }

    private func configuration(for name: String) throws -> (Language, Query) {
        if let configuration = configurations[name] { return configuration }
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
        configurations[name] = (language, query)
        return (language, query)
    }
}
