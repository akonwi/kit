import Foundation
import Markdown

/// Native presentation data projected from Swift Markdown's document tree.
struct MarkdownContent {
    indirect enum Block: Equatable {
        case paragraph(AttributedString)
        case heading(Int, AttributedString)
        case code(String, String)
        case quote([Block])
        case list([Item])
        case table(TableContent)
        case rule
    }

    struct Item: Equatable {
        let marker: String
        let blocks: [Block]
    }

    struct TableContent: Equatable {
        enum Alignment: Equatable { case leading, center, trailing }
        let alignments: [Alignment]
        let header: [AttributedString]
        let rows: [[AttributedString]]
    }

    let blocks: [Block]

    init(source: String) {
        blocks = Self.blocks(in: Document(parsing: source))
    }

    private static func blocks(in parent: any Markup) -> [Block] {
        parent.children.flatMap { node -> [Block] in
            switch node {
            case let value as Paragraph: return [.paragraph(inline(value))]
            case let value as Heading: return [.heading(value.level, inline(value))]
            case let value as Markdown.CodeBlock: return [.code(value.language ?? "", value.code)]
            case let value as BlockQuote: return [.quote(blocks(in: value))]
            case let value as OrderedList:
                return [.list(value.listItems.enumerated().map { offset, item in
                    Item(marker: "\(value.startIndex + UInt(offset)).", blocks: blocks(in: item))
                })]
            case let value as UnorderedList:
                return [.list(value.listItems.map { item in
                    let marker: String
                    switch item.checkbox {
                    case .checked?: marker = "☑"
                    case .unchecked?: marker = "☐"
                    case nil: marker = "•"
                    }
                    return Item(marker: marker, blocks: blocks(in: item))
                })]
            case let value as Markdown.Table:
                return [.table(TableContent(
                    alignments: value.columnAlignments.map { alignment in
                        switch alignment {
                        case .center?: return .center
                        case .right?: return .trailing
                        default: return .leading
                        }
                    },
                    header: value.head.cells.map { inline($0) },
                    rows: value.body.rows.map { $0.cells.map { inline($0) } }
                ))]
            case is ThematicBreak: return [.rule]
            case let value as HTMLBlock: return [.code("html", value.rawHTML)]
            default: return blocks(in: node)
            }
        }
    }

    private static func inline(_ node: any Markup) -> AttributedString {
        switch node {
        case let value as Markdown.Text: return AttributedString(value.string)
        case let value as InlineCode:
            var text = AttributedString(value.code)
            text.inlinePresentationIntent = .code
            return text
        case is SoftBreak: return AttributedString(" ")
        case is LineBreak: return AttributedString("\n")
        case let value as InlineHTML: return AttributedString(value.rawHTML)
        case let value as Markdown.Image:
            return AttributedString("[Image: \(value.plainText)]")
        default: break
        }
        var text = node.children.reduce(into: AttributedString()) { $0 += inline($1) }
        let intent: InlinePresentationIntent?
        switch node {
        case is Strong: intent = .stronglyEmphasized
        case is Emphasis: intent = .emphasized
        case is Strikethrough: intent = .strikethrough
        default: intent = nil
        }
        if let intent {
            for run in Array(text.runs) {
                text[run.range].inlinePresentationIntent = (run.inlinePresentationIntent ?? []).union(intent)
            }
        }
        if let link = node as? Markdown.Link, let destination = link.destination,
           let url = URL(string: destination), let scheme = url.scheme?.lowercased(),
           ["https", "http", "mailto"].contains(scheme) {
            text.link = url
        }
        return text
    }
}

/// Avoid reparsing unchanged transcripts during scroll geometry and theme updates.
@MainActor
final class MarkdownContentCache {
    static let shared = MarkdownContentCache()
    private final class Entry: NSObject {
        let content: MarkdownContent
        init(_ content: MarkdownContent) { self.content = content }
    }
    private let entries = NSCache<NSString, Entry>()

    private init() {
        entries.countLimit = 64
        entries.totalCostLimit = 8 * 1024 * 1024
    }

    func content(for source: String) -> MarkdownContent {
        let key = source as NSString
        if let entry = entries.object(forKey: key) { return entry.content }
        let content = MarkdownContent(source: source)
        entries.setObject(Entry(content), forKey: key, cost: source.utf8.count)
        return content
    }
}
