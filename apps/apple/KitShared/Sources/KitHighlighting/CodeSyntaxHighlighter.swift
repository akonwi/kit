import Foundation
import SwiftTreeSitter

/// A run of highlighted code and the syntax role that colors it.
public struct SyntaxSpan: Equatable, Sendable {
    public let range: NSRange
    public let role: String

    public init(range: NSRange, role: String) {
        self.range = range
        self.role = role
    }
}

/// Code to highlight and the language its fence or file names.
public struct CodeHighlightRequest: Hashable, Sendable {
    public let source: String
    public let language: String

    public init(source: String, language: String) {
        self.source = source
        self.language = language
    }
}

/// A language's parser and the highlight query that captures its syntax.
public struct SyntaxGrammar: Sendable {
    public let language: Language
    public let query: Query

    public init(language: Language, query: Query) {
        self.language = language
        self.query = query
    }
}

/// Supplies the grammars a highlighter parses with. Each app bundles its own.
public protocol SyntaxGrammars: Sendable {
    /// The grammar for a canonical language name from
    /// `CodeSyntaxHighlighter.canonicalLanguage(_:)`. Throws
    /// `SyntaxGrammarError.unavailable` for a language this app doesn't
    /// bundle, whose code then stays plain.
    func grammar(named name: String) throws -> SyntaxGrammar
}

/// Why a `SyntaxGrammars` has no grammar for a language.
public enum SyntaxGrammarError: Error, Equatable, Sendable {
    /// The app doesn't bundle a grammar for the language.
    case unavailable
}

/// Parse off the main actor; cache captures independently of the active theme.
public actor CodeSyntaxHighlighter {
    private let grammars: any SyntaxGrammars
    private var configurations: [String: SyntaxGrammar] = [:]
    private var cache: [CodeHighlightRequest: [SyntaxSpan]] = [:]
    private var order: [CodeHighlightRequest] = []
    private var cacheBytes = 0

    public init(grammars: any SyntaxGrammars) {
        self.grammars = grammars
    }

    /// The canonical name of a fence's language, or nil when no grammar covers it.
    public static func canonicalLanguage(_ label: String) -> String? {
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
        case "rb", "ruby": return "ruby"
        case "css": return "css"
        case "ard": return "ard"
        default: return nil
        }
    }

    /// The highlighted spans of a request's source, empty when its language has no grammar.
    public func spans(for request: CodeHighlightRequest) throws -> [SyntaxSpan] {
        guard let name = Self.canonicalLanguage(request.language), !request.source.isEmpty else { return [] }
        // Oversized blocks still render in full, but without blocking on highlighting.
        guard request.source.utf8.count <= 256 * 1024 else { return [] }
        let key = CodeHighlightRequest(source: request.source, language: name)
        if let cached = cache[key] { return cached }
        let grammar: SyntaxGrammar
        do {
            grammar = try configuration(for: name)
        } catch SyntaxGrammarError.unavailable {
            return []
        }
        let parser = Parser()
        parser.timeout = 0.1
        try parser.setLanguage(grammar.language)
        guard let tree = parser.parse(request.source) else { return [] }
        let captures = grammar.query.execute(in: tree).resolve(with: .init(string: request.source)).highlights()
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

    /// The syntax role a highlight query's capture name colors as.
    public static func role(for capture: String) -> String? {
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

    private func configuration(for name: String) throws -> SyntaxGrammar {
        if let configuration = configurations[name] { return configuration }
        let grammar = try grammars.grammar(named: name)
        configurations[name] = grammar
        return grammar
    }
}
