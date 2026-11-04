import Foundation
import KitClient
import KitHighlighting

/// Colors a file's diff by its syntax. Each run of lines between gaps is
/// parsed twice: removed lines as the old text reads, added and unchanged
/// lines as the new text, so each line is colored in the code it belongs to.
@MainActor
enum DiffHighlighting {
    /// The grammar for a file, from its extension, or nil when none covers it.
    static func language(_ path: String) -> String? {
        let ext = (path as NSString).pathExtension.lowercased()
        return CodeSyntaxHighlighter.canonicalLanguage(["mjs", "cjs"].contains(ext) ? "javascript" : ext)
    }

    /// Each row's text as `DiffLines` shows it, colored, or nil for a gap
    /// or marker. Nil when the file has no grammar or highlighting fails.
    static func lines(_ document: DiffDocument) async -> [AttributedString?]? {
        guard let language = language(document.file.path) else { return nil }
        var colored = [AttributedString?](repeating: nil, count: document.rows.count)
        var start = 0
        while start < document.rows.count {
            var end = start
            while end < document.rows.count, document.rows[end].kind != "hunk" { end += 1 }
            for side in [Side.old, .new] {
                let indices = (start..<end).filter { side.shows(document.rows[$0].kind) }
                guard !indices.isEmpty else { continue }
                let texts = indices.map { DiffLines.display(document.rows[$0].text) }
                let request = CodeHighlightRequest(source: texts.joined(separator: "\n"), language: language)
                guard let spans = await CodeColoring.spans(for: request) else { continue }
                if Task.isCancelled { return nil }
                for (index, text) in zip(indices, CodeColoring.lines(texts, spans: spans)) {
                    // An unchanged line reads the same on both sides; it
                    // takes the new side's colors.
                    if side == .new || document.rows[index].kind == "deletion" { colored[index] = text }
                }
            }
            start = end + 1
        }
        return colored.contains { $0 != nil } ? colored : nil
    }

    private enum Side {
        case old, new

        func shows(_ kind: String) -> Bool {
            switch self {
            case .old: kind == "context" || kind == "deletion"
            case .new: kind == "context" || kind == "addition"
            }
        }
    }
}
