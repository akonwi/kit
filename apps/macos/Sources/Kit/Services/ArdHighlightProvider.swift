import AppKit
import CodeEditLanguages
@preconcurrency import CodeEditSourceEditor
@preconcurrency import CodeEditTextView
import KitArdGrammar
import KitHighlighting
import SwiftTreeSitter

/// Colors Ard in the file and diff editors, whose CodeEditLanguages grammars
/// have no Ard. It parses the whole document with tree-sitter-ard off the main
/// actor and names captures the way the editor's own tree-sitter client does.
@MainActor
final class ArdHighlightProvider: HighlightProviding {
    private var parse: Task<[HighlightRange], Never>?
    /// The storage last parsed. The editor replaces its storage, rather than
    /// editing it, when it sets a document's text.
    private weak var parsedStorage: NSTextStorage?

    /// Whether a file's editor colors it with this provider rather than the
    /// editor's own tree-sitter client.
    nonisolated static func colors(_ path: String) -> Bool {
        (path as NSString).pathExtension.lowercased() == "ard"
    }

    func setUp(textView: TextView, codeLanguage: CodeLanguage) {
        reparse(textView)
    }

    func applyEdit(textView: TextView, range: NSRange, delta: Int,
                   completion: @escaping @MainActor (Result<IndexSet, Error>) -> Void) {
        reparse(textView)
        let length = textView.length
        let parse = self.parse
        Task { @MainActor in
            _ = await parse?.value
            completion(.success(IndexSet(integersIn: 0..<length)))
        }
    }

    func queryHighlightsFor(textView: TextView, range: NSRange,
                            completion: @escaping @MainActor (Result<[HighlightRange], Error>) -> Void) {
        if textView.textStorage !== parsedStorage { reparse(textView) }
        let parse = self.parse
        Task { @MainActor in
            let highlights = await parse?.value ?? []
            completion(.success(highlights.compactMap { highlight in
                guard let clipped = highlight.range.intersection(range), clipped.length > 0 else { return nil }
                return HighlightRange(range: clipped, capture: highlight.capture)
            }))
        }
    }

    private func reparse(_ textView: TextView) {
        let source = textView.string
        parsedStorage = textView.textStorage
        parse?.cancel()
        parse = Task.detached(priority: .userInitiated) { Self.highlights(in: source) }
    }

    private nonisolated static let grammar = Result { try ArdSyntaxGrammar.grammar() }

    /// The document's highlights in document order, outer before inner: the
    /// editor applies them in order and skips one that overlaps an earlier
    /// one. As in the editor's tree-sitter client, a capture colors only when
    /// its whole name is one the editor knows, and for a range captured more
    /// than once the earliest pattern wins.
    nonisolated static func highlights(in source: String) -> [HighlightRange] {
        guard !source.isEmpty, let grammar = try? grammar.get() else { return [] }
        let parser = Parser()
        guard (try? parser.setLanguage(grammar.language)) != nil,
              let tree = parser.parse(source) else { return [] }
        var levels: [NSRange: Int] = [:]
        return grammar.query.execute(in: tree)
            .resolve(with: .init(string: source))
            .flatMap(\.captures)
            .reversed()
            .compactMap { capture in
                if let level = levels[capture.range], level <= capture.index { return nil }
                guard let name = CaptureName.fromString(capture.name), capture.range.length > 0 else { return nil }
                levels[capture.range] = capture.index
                return HighlightRange(range: capture.range, capture: name)
            }
            .sorted { ($0.range.location, -$0.range.length) < ($1.range.location, -$1.range.length) }
    }
}
