import Foundation

enum MessageText {
    static func quote(_ selection: String, draft: String) -> String {
        let quoted = selection.components(separatedBy: .newlines).map { "> " + $0 }.joined(separator: "\n")
        return draft + (draft.isEmpty ? "" : "\n\n") + quoted + "\n\n"
    }

    static func plain(_ markdown: String) -> String {
        render(MarkdownContent(source: markdown).blocks)
    }
    private static func render(_ blocks: [MarkdownContent.Block]) -> String {
        blocks.map { block in
            switch block {
            case .paragraph(let value), .heading(_, let value): String(value.characters)
            case .code(_, let source): source.trimmingCharacters(in: .newlines)
            case .quote(let children): render(children)
            case .list(let items): items.map { $0.marker + " " + render($0.blocks) }.joined(separator: "\n")
            case .table(let table): ([table.header] + table.rows).map { $0.map { String($0.characters) }.joined(separator: "\t") }.joined(separator: "\n")
            case .rule: "—"
            }
        }.joined(separator: "\n\n")
    }
}
