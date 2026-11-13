import KitClient
import KitTranscript
import SwiftUI

/// A find match drawn in a text: its UTF-16 range, and whether it's current.
struct FindMark: Equatable {
    var range: Range<Int>
    var current: Bool
}

/// How a file's find bar marks what it found in the text a file shows.
enum FindMarks {
    /// The marks in piece `piece`: its matches, with the current one marked.
    /// A match `kept` from a closed bar is marked current alone.
    static func marks(in piece: Int, matches: [FindMatch], current: Int?, kept: FindMatch? = nil) -> [FindMark] {
        if let kept {
            return kept.piece == piece ? [FindMark(range: kept.range, current: true)] : []
        }
        guard var index = FileFind.following(matches, anchor: FindPosition(piece: piece, offset: 0)) else { return [] }
        var marks: [FindMark] = []
        while index < matches.count, matches[index].piece == piece {
            marks.append(FindMark(range: matches[index].range, current: index == current))
            index += 1
        }
        return marks
    }

    /// `text` with `marks` drawn: a match tinted, the current one filled
    /// with the accent under light text.
    static func apply(_ marks: [FindMark], to text: AttributedString) -> AttributedString {
        guard !marks.isEmpty else { return text }
        var marked = text
        // The current match last, so a match at the same place, as in two
        // identical code blocks, can't cover it.
        for mark in marks.filter({ !$0.current }) + marks.filter(\.current) {
            guard let range = Range(NSRange(location: mark.range.lowerBound, length: mark.range.count), in: marked)
            else { continue }
            marked[range].backgroundColor = mark.current ? Theme.accent : Theme.findMatch
            if mark.current { marked[range].foregroundColor = Color.white }
        }
        return marked
    }

    /// The lines of one source, each with the marks that fall in it, by
    /// their range in the whole source.
    static func apply(_ marks: [FindMark], toLines lines: [AttributedString]) -> [AttributedString] {
        guard !marks.isEmpty else { return lines }
        var start = 0
        return lines.map { line in
            let length = line.characters.reduce(0) { $0 + $1.utf16.count }
            let end = start + length
            let inLine = marks.compactMap { mark -> FindMark? in
                let lower = max(mark.range.lowerBound, start), upper = min(mark.range.upperBound, end)
                return lower < upper ? FindMark(range: (lower - start)..<(upper - start), current: mark.current) : nil
            }
            start = end + 1
            return apply(inLine, to: line)
        }
    }
}

/// What a rendered Markdown file's find bar searches: the texts its blocks
/// show, in reading order (paragraphs, headings, list items, quotes,
/// callouts, table cells, and code), each a piece of its own, so no match
/// spans two. Diagrams and data figures draw rather than show their text,
/// so aren't searched. A diff fence shows as code here, so it is.
enum MarkdownFind {
    /// The pieces of `blocks`, and the index of each top-level block's first
    /// piece, with one past the last block's last.
    static func pieces(_ blocks: [MarkdownContent.Block]) -> (pieces: [String], starts: [Int]) {
        var pieces: [String] = [], starts: [Int] = []
        for block in blocks {
            starts.append(pieces.count)
            pieces += self.pieces(of: block)
        }
        starts.append(pieces.count)
        return (pieces, starts)
    }

    /// The texts `block` shows, in reading order.
    static func pieces(of block: MarkdownContent.Block) -> [String] {
        switch block {
        case .paragraph(let text), .heading(_, let text): [String(text.characters)]
        case .code(_, let source): [source]
        case .mermaid, .figure, .rule: []
        case .quote(let blocks): blocks.flatMap(pieces(of:))
        case .callout(let callout): callout.blocks.flatMap(pieces(of:))
        case .list(let items): items.flatMap { $0.blocks.flatMap(pieces(of:)) }
        case .table(let table):
            table.header.map { String($0.characters) } + table.rows.flatMap { $0.map { String($0.characters) } }
        }
    }

    /// `block` with its pieces marked, numbering them from `next`. Code
    /// can't carry marks in its block, so its marks are gathered in `code`,
    /// by its source, for `CodeBlock` to draw.
    static func marked(_ block: MarkdownContent.Block, marks: (Int) -> [FindMark], next: inout Int,
                       code: inout [String: [FindMark]]) -> MarkdownContent.Block {
        func text(_ value: AttributedString) -> AttributedString {
            defer { next += 1 }
            return FindMarks.apply(marks(next), to: value)
        }
        func children(_ blocks: [MarkdownContent.Block]) -> [MarkdownContent.Block] {
            blocks.map { marked($0, marks: marks, next: &next, code: &code) }
        }
        switch block {
        case .paragraph(let value): return .paragraph(text(value))
        case .heading(let level, let value): return .heading(level, text(value))
        case .code(_, let source):
            let found = marks(next)
            if !found.isEmpty { code[source, default: []] += found }
            next += 1
            return block
        case .mermaid, .figure, .rule: return block
        case .quote(let blocks): return .quote(children(blocks))
        case .callout(let callout):
            return .callout(Callout(kind: callout.kind, title: callout.title, blocks: children(callout.blocks)))
        case .list(let items):
            return .list(items.map { MarkdownContent.Item(marker: $0.marker, blocks: children($0.blocks), lines: $0.lines) })
        case .table(let table):
            let header = table.header.map(text)
            let rows = table.rows.map { $0.map(text) }
            return .table(.init(alignments: table.alignments, header: header, rows: rows))
        }
    }

    /// The top-level block showing piece `piece`, by each block's first
    /// piece as `pieces` lists them.
    static func block(of piece: Int, starts: [Int]) -> Int {
        var low = 0, high = starts.count - 2, found = 0
        while low <= high {
            let middle = (low + high) / 2
            if starts[middle] <= piece { found = middle; low = middle + 1 } else { high = middle - 1 }
        }
        return found
    }
}

extension EnvironmentValues {
    /// Find marks in the code blocks of one of a rendered Markdown file's
    /// blocks, by each code block's source. Two identical code blocks in one
    /// list or quote show the same marks.
    @Entry var findCodeMarks: [String: [FindMark]] = [:]
}

/// Where a file's view is scrolled, as find needs it: written as the view
/// scrolls, read when a search starts or a match is shown. Not observed, so
/// scrolling doesn't redraw what reads it.
@MainActor final class FindViewport {
    /// The visible part of the scrolled content.
    var visible: CGRect = .zero
    /// In the source, the first line at the top of the screen.
    var topLine = 1
    /// In the preview, each top-level block's top and bottom in the content.
    var blocks: [Int: ClosedRange<CGFloat>] = [:]

    /// The first preview block at the top of the screen.
    var topBlock: Int {
        blocks.filter { $0.value.upperBound > visible.minY }.min { $0.key < $1.key }?.key ?? 0
    }

    /// Whether preview block `index` is wholly on screen, or fills it.
    func shows(block index: Int) -> Bool {
        guard let frame = blocks[index], visible.height > 0 else { return false }
        return (frame.lowerBound >= visible.minY && frame.upperBound <= visible.maxY)
            || (frame.lowerBound <= visible.minY && frame.upperBound >= visible.maxY)
    }
}

/// A request to bring a match in a file's lines into view: its line,
/// counted from 1, and the characters before and through it.
struct FindReveal: Equatable {
    var request: Int
    var line: Int
    var start: Int
    var end: Int
}
