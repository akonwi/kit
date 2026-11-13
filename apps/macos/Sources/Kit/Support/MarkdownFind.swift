import AppKit
import KitTranscript
import SwiftUI

/// A find match in rendered Markdown text, current or not.
enum FindMark: Hashable, Sendable { case match, current }

/// Marks the characters of rendered Markdown text that a find bar matched.
/// `MarkdownInline` draws them; nothing else reads it.
enum FindMarkAttribute: AttributedStringKey {
    typealias Value = FindMark
    static let name = "KitFindMark"
}

/// A find match in a code block's source, as `HighlightedCodeText` draws it.
struct FindCodeMark: Equatable {
    var range: NSRange
    var mark: FindMark
}

extension EnvironmentValues {
    /// Find matches in the code blocks of the row being drawn, by the
    /// block's source. Two identical blocks in one row show the same marks.
    @Entry var findCodeMarks: [String: [FindCodeMark]] = [:]
}

/// What a rendered Markdown file's find bar searches, and how it marks what
/// it found. Each row shows texts in reading order (its paragraphs,
/// headings, list items, quotes, callouts, table cells, and code blocks),
/// each a piece of its own, so no match spans two. Diagrams, data figures,
/// and diff fences draw rather than show their text, so aren't searched.
enum MarkdownFind {
    /// The pieces of every row, and where each row's start.
    struct Index: Equatable, Sendable {
        var pieces: [String] = []
        /// The index of each row's first piece, by row ID, and one past the
        /// last row's last.
        var starts: [Int] = [0]

        init(rows: [MarkdownDocumentRow]) {
            for row in rows {
                pieces += MarkdownFind.pieces(of: row)
                starts.append(pieces.count)
            }
        }

        /// The row showing piece `piece`.
        func row(of piece: Int) -> Int? {
            var low = 0, high = starts.count - 1
            while low < high {
                let middle = (low + high) / 2
                if starts[middle + 1] <= piece { low = middle + 1 } else { high = middle }
            }
            return low < starts.count - 1 ? low : nil
        }

        /// The pieces row `row` shows.
        func pieces(of row: Int) -> Range<Int> {
            guard row + 1 < starts.count else { return 0..<0 }
            return starts[row]..<starts[row + 1]
        }
    }

    /// The texts `row` shows, in reading order.
    static func pieces(of row: MarkdownDocumentRow) -> [String] {
        var pieces: [String] = []
        _ = walk(row, text: { pieces.append(String($0.characters)) }, code: { pieces.append($0) })
        return pieces
    }

    /// `row` with `marks[i]` marked in its `i`th piece, and the marks of its
    /// code blocks, by their source.
    static func marked(_ row: MarkdownDocumentRow, marks: [Int: [(range: Range<Int>, mark: FindMark)]])
        -> (row: MarkdownDocumentRow, code: [String: [FindCodeMark]]) {
        var piece = 0
        var code: [String: [FindCodeMark]] = [:]
        let marked = walk(row, text: { text in
            if let found = marks[piece] { mark(&text, found) }
            piece += 1
        }, code: { source in
            if let found = marks[piece] {
                code[source, default: []] += found.map { FindCodeMark(range: NSRange(location: $0.range.lowerBound, length: $0.range.count), mark: $0.mark) }
            }
            piece += 1
        })
        return (marked, code)
    }

    /// Marks UTF-16 `ranges` of `text`.
    static func mark(_ text: inout AttributedString, _ ranges: [(range: Range<Int>, mark: FindMark)]) {
        let wanted = Set(ranges.flatMap { [$0.range.lowerBound, $0.range.upperBound] })
        var indices: [Int: AttributedString.Index] = [:]
        var offset = 0
        var index = text.unicodeScalars.startIndex
        while true {
            if wanted.contains(offset) { indices[offset] = index }
            guard index < text.unicodeScalars.endIndex else { break }
            offset += text.unicodeScalars[index].utf16.count
            index = text.unicodeScalars.index(after: index)
        }
        for (range, mark) in ranges {
            guard let lower = indices[range.lowerBound], let upper = indices[range.upperBound], lower < upper else { continue }
            text[lower..<upper][FindMarkAttribute.self] = mark
        }
    }

    /// `row` with each of its texts passed through `text` and each code
    /// block's source shown to `code`, in reading order.
    private static func walk(_ row: MarkdownDocumentRow, text: (inout AttributedString) -> Void,
                             code: (String) -> Void) -> MarkdownDocumentRow {
        let content: MarkdownDocumentRow.Content = switch row.content {
        case .block(let block): .block(walk(block, text, code))
        case .item(let item): .item(walk(item, text, code))
        }
        return MarkdownDocumentRow(id: row.id, content: content, gap: row.gap, lines: row.lines)
    }

    private static func walk(_ item: MarkdownContent.Item, _ text: (inout AttributedString) -> Void,
                             _ code: (String) -> Void) -> MarkdownContent.Item {
        MarkdownContent.Item(marker: item.marker, blocks: item.blocks.map { walk($0, text, code) }, lines: item.lines)
    }

    private static func walk(_ block: MarkdownContent.Block, _ text: (inout AttributedString) -> Void,
                             _ code: (String) -> Void) -> MarkdownContent.Block {
        switch block {
        case .paragraph(var value):
            text(&value)
            return .paragraph(value)
        case .heading(let level, var value):
            text(&value)
            return .heading(level, value)
        case .code(let language, let source):
            if !UnifiedDiff.isFenceLanguage(language) { code(source) }
            return block
        case .quote(let children):
            return .quote(children.map { walk($0, text, code) })
        case .callout(let callout):
            return .callout(Callout(kind: callout.kind, title: callout.title, blocks: callout.blocks.map { walk($0, text, code) }))
        case .list(let items):
            return .list(items.map { walk($0, text, code) })
        case .table(let table):
            let header = table.header.map { cell in var cell = cell; text(&cell); return cell }
            let rows = table.rows.map { row in row.map { cell in var cell = cell; text(&cell); return cell } }
            return .table(.init(alignments: table.alignments, header: header, rows: rows))
        case .mermaid, .figure, .rule:
            return block
        }
    }
}
