import Foundation
import KitClient
import KitFigures
import KitMermaid
import Markdown

/// Native presentation data projected from Swift Markdown's document tree.
public struct MarkdownContent: Sendable {
    public indirect enum Block: Equatable, Sendable {
        case paragraph(AttributedString)
        case heading(Int, AttributedString)
        case code(String, String)
        case mermaid(MermaidDiagram)
        case figure(FigureBlock)
        case quote([Block])
        case callout(Callout)
        case list([Item])
        case table(TableContent)
        case rule
    }

    public struct Item: Equatable, Sendable {
        public let marker: String
        public let blocks: [Block]
        /// The 1-based source lines the item spans, with its marker. Where
        /// the item came from, not what it shows, so not compared.
        public var lines: ClosedRange<Int>? = nil

        public init(marker: String, blocks: [Block], lines: ClosedRange<Int>? = nil) {
            self.marker = marker
            self.blocks = blocks
            self.lines = lines
        }

        public static func == (a: Item, b: Item) -> Bool { a.marker == b.marker && a.blocks == b.blocks }
    }

    public struct TableContent: Equatable, Sendable {
        public enum Alignment: Equatable, Sendable { case leading, center, trailing }
        public let alignments: [Alignment]
        public let header: [AttributedString]
        public let rows: [[AttributedString]]

        public init(alignments: [Alignment], header: [AttributedString], rows: [[AttributedString]]) {
            self.alignments = alignments
            self.header = header
            self.rows = rows
        }
    }

    public let blocks: [Block]
    /// The 1-based source lines each of `blocks` spans, including a code
    /// block's fences; nil where unknown.
    public let lines: [ClosedRange<Int>?]

    public init(source: String) {
        var parsed: [Block] = [], lines: [ClosedRange<Int>?] = []
        for child in Document(parsing: source).children {
            let made = Self.blocks(of: child)
            parsed += made
            lines += Array(repeating: Self.lines(child.range), count: made.count)
        }
        // A diagram whose fence hasn't closed, as while streaming, shows its source.
        blocks = Self.endsInsideFence(source) ? Self.openingTrailingDiagram(parsed) : parsed
        self.lines = lines
    }

    /// The 1-based lines a source range spans. A range ending at the start
    /// of a line ends on the line before.
    public static func lines(_ range: SourceRange?) -> ClosedRange<Int>? {
        guard let range, range.lowerBound.line > 0 else { return nil }
        let start = range.lowerBound.line
        var end = range.upperBound.line
        if range.upperBound.column <= 1 && end > start { end -= 1 }
        return start...max(start, end)
    }

    /// The Mermaid diagrams drawn in these blocks, in order.
    public var diagrams: [MermaidDiagram] {
        Self.diagrams(in: blocks)
    }

    private static func diagrams(in blocks: [Block]) -> [MermaidDiagram] {
        blocks.flatMap { block -> [MermaidDiagram] in
            switch block {
            case .mermaid(let diagram): [diagram]
            case .quote(let children): diagrams(in: children)
            case .callout(let callout): diagrams(in: callout.blocks)
            case .list(let items): items.flatMap { diagrams(in: $0.blocks) }
            default: []
            }
        }
    }

    /// blocks with the last one, when it is or ends in a diagram, shown as source.
    private static func openingTrailingDiagram(_ blocks: [Block]) -> [Block] {
        guard let last = blocks.last else { return blocks }
        let open: Block
        switch last {
        case .mermaid(let diagram): open = .code("mermaid", diagram.source)
        case .figure(let figure): open = .code(figure.name, figure.source)
        case .quote(let children): open = .quote(openingTrailingDiagram(children))
        case .callout(let callout):
            open = .callout(Callout(kind: callout.kind, title: callout.title, blocks: openingTrailingDiagram(callout.blocks)))
        case .list(let items):
            guard let item = items.last else { return blocks }
            open = .list(Array(items.dropLast()) + [Item(marker: item.marker, blocks: openingTrailingDiagram(item.blocks), lines: item.lines)])
        default: return blocks
        }
        return Array(blocks.dropLast()) + [open]
    }

    /// Whether source ends inside an unclosed code fence, as streaming output
    /// does before the fence closes. A fence closes on a line of at least as
    /// many of its own characters and nothing else. The terminal client
    /// decides the same way.
    public static func endsInsideFence(_ source: String) -> Bool {
        var open: Substring = ""
        // Split on line feeds alone: Swift reads `\r\n` as one character.
        for text in source.components(separatedBy: "\n") {
            let line = Substring(text)
            let run = fenceRun(line)
            let info = line.range(of: run).map { line[$0.upperBound...].trimmingCharacters(in: .whitespacesAndNewlines) } ?? ""
            if open.isEmpty {
                // A backtick fence's info string can't contain backticks.
                if !run.isEmpty && !(run.hasPrefix("`") && info.contains("`")) { open = run }
            } else if !run.isEmpty && run.first == open.first && run.count >= open.count && info.isEmpty {
                open = ""
            }
        }
        return !open.isEmpty
    }

    /// The run of three or more backticks or tildes opening line, after any
    /// quote and list markers, or empty.
    private static func fenceRun(_ line: Substring) -> Substring {
        var rest = line.drop { $0 == " " || $0 == "\t" }
        while rest.hasPrefix(">") {
            rest = rest.dropFirst().drop { $0 == " " || $0 == "\t" }
        }
        for marker in ["- ", "* ", "+ "] where rest.hasPrefix(marker) {
            rest = rest.dropFirst(2).drop { $0 == " " || $0 == "\t" }
        }
        let digits = rest.drop { $0.isASCII && $0.isNumber }
        if digits.count < rest.count && (digits.hasPrefix(". ") || digits.hasPrefix(") ")) {
            rest = digits.dropFirst(2).drop { $0 == " " || $0 == "\t" }
        }
        guard let character = rest.first, character == "`" || character == "~" else { return "" }
        let run = rest.prefix { $0 == character }
        return run.count >= 3 ? run : ""
    }

    private static func blocks(in parent: any Markup) -> [Block] {
        parent.children.flatMap { blocks(of: $0) }
    }

    private static func blocks(of node: any Markup) -> [Block] {
        switch node {
        case let value as Paragraph: return [.paragraph(text(value))]
        case let value as Heading: return [.heading(value.level, text(value))]
        case let value as Markdown.CodeBlock:
            // swift-markdown's language is the whole info string; the
            // terminal client reads its first word as the language, too.
            let info = value.language ?? ""
            let language = info.split(whereSeparator: { $0 == " " || $0 == "\t" }).first.map(String.init) ?? ""
            if Mermaid.isMermaid(language) { return [.mermaid(MermaidDiagram(source: value.code))] }
            if KitFigures.Figure.isFigure(info) { return [.figure(FigureBlock(info: info, source: value.code))] }
            return [.code(language, value.code)]
        case let value as BlockQuote:
            if let callout = callout(value) { return [.callout(callout)] }
            return [.quote(blocks(in: value))]
        case let value as OrderedList:
            return [.list(value.listItems.enumerated().map { offset, item in
                Item(marker: "\(value.startIndex + UInt(offset)).", blocks: blocks(in: item), lines: lines(item.range))
            })]
        case let value as UnorderedList:
            return [.list(value.listItems.map { item in
                let marker: String
                switch item.checkbox {
                case .checked?: marker = "☑"
                case .unchecked?: marker = "☐"
                case nil: marker = "•"
                }
                return Item(marker: marker, blocks: blocks(in: item), lines: lines(item.range))
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
                header: value.head.cells.map { text($0) },
                rows: value.body.rows.map { $0.cells.map { text($0) } }
            ))]
        case is ThematicBreak: return [.rule]
        case let value as HTMLBlock: return [.code("html", value.rawHTML)]
        default: return blocks(in: node)
        }
    }

    /// A GitHub alert: a quote whose first line is `[!KIND]`, optionally
    /// followed by a title, read as the terminal client reads it. The marker
    /// line is removed from the alert's first paragraph.
    private static func callout(_ quote: BlockQuote) -> Callout? {
        guard let paragraph = quote.child(at: 0) as? Paragraph else { return nil }
        let inlines = Array(paragraph.inlineChildren)
        let lineEnd = inlines.firstIndex { $0 is SoftBreak || $0 is LineBreak } ?? inlines.endIndex
        let firstLine = inlines[..<lineEnd].map { ($0 as? Markdown.Text)?.string ?? $0.format() }.joined()
        guard let marker = Callout.marker(firstLine) else { return nil }
        let rest = inlines.dropFirst(lineEnd + 1)
        var body: [Block] = []
        if !rest.isEmpty {
            body.append(.paragraph(SessionMentions.linked(rest.reduce(into: AttributedString()) {
                $0 += inline($1, after: $0.characters.last)
            })))
        }
        for child in quote.children.dropFirst() { body += blocks(of: child) }
        return Callout(kind: marker.kind, title: marker.title, blocks: body)
    }

    /// A block's inline text. Session mentions become links once the whole
    /// block is joined: the parser splits `#[session:<id>]` at its brackets.
    private static func text(_ node: any Markup) -> AttributedString {
        SessionMentions.linked(inline(node))
    }

    /// A node's inline text. Bare web addresses in it are links, as GitHub
    /// links them, except in a link's own text (`autolinks` false there).
    /// `previous` is the character shown before the node, which decides
    /// whether an address at its start can be one.
    private static func inline(_ node: any Markup, autolinks: Bool = true, after previous: Character? = nil) -> AttributedString {
        switch node {
        case let value as Markdown.Text:
            return (autolinks ? WebAutolinks.linked(value.string, after: previous) : nil) ?? AttributedString(value.string)
        case let value as InlineCode:
            var text = AttributedString(value.code)
            text.inlinePresentationIntent = .code
            // A code span that is exactly a file path opens it (ADR 0050).
            if let path = FilePaths.codePath(value.code), let url = FilePaths.link(path) { text.link = url }
            return text
        case is SoftBreak: return AttributedString(" ")
        case is LineBreak: return AttributedString("\n")
        case let value as InlineHTML: return AttributedString(value.rawHTML)
        case let value as Markdown.Image:
            return AttributedString("[Image: \(value.plainText)]")
        default: break
        }
        let autolinks = autolinks && !(node is Markdown.Link)
        // Text just inside emphasis follows its `*`, `_`, or `~` delimiter.
        let opening: Character? = node is Strong || node is Emphasis || node is Strikethrough ? "*" : previous
        var text = AttributedString()
        for child in node.children {
            text += inline(child, autolinks: autolinks, after: text.characters.last ?? opening)
        }
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
        if let link = node as? Markdown.Link, let destination = link.destination {
            if let url = URL(string: destination), let scheme = url.scheme?.lowercased(),
               ["https", "http", "mailto"].contains(scheme) {
                text.link = url
            } else if let path = FilePaths.linkedPath(destination: destination), let url = FilePaths.link(path) {
                text.link = url
            }
        }
        return text
    }
}

/// A fenced figure (ADR 0054): its info string and body. It's parsed the
/// first time it's shown, and the result is kept by fence, so reparsing a
/// streaming message doesn't parse it again.
public struct FigureBlock: Equatable, Sendable {
    public let info: String
    public let source: String

    public init(info: String, source: String) {
        self.info = info
        self.source = source
    }

    /// The figure both clients draw, or why it isn't drawn.
    public var figure: Result<KitFigures.Figure, KitFigures.NotDrawn> {
        FigureParses.shared.figure(info: info, body: source)
    }

    /// The figure's kind, as its fence names it.
    public var name: String {
        info.split(whereSeparator: { $0 == " " || $0 == "\t" }).first.map(String.init) ?? ""
    }

    /// The whole fence, as it was written.
    public var fenced: String { "```\(info)\n\(source.hasSuffix("\n") ? source : source + "\n")```" }

    public static func == (lhs: Self, rhs: Self) -> Bool { lhs.info == rhs.info && lhs.source == rhs.source }
}

/// A GitHub alert (`> [!WARNING]`): its kind, the title after its marker,
/// and the blocks under it.
public struct Callout: Equatable, Sendable {
    public enum Kind: String, CaseIterable, Sendable {
        case note, tip, important, warning, caution
        /// The kind as a callout's header names it.
        public var label: String { rawValue.prefix(1).uppercased() + rawValue.dropFirst() }
    }
    public let kind: Kind
    public let title: String
    public let blocks: [MarkdownContent.Block]

    public init(kind: Kind, title: String, blocks: [MarkdownContent.Block]) {
        self.kind = kind
        self.title = title
        self.blocks = blocks
    }

    /// The kind and title a quote's first line marks, as `[!KIND] title`.
    public static func marker(_ line: String) -> (kind: Kind, title: String)? {
        let text = line.trimmingCharacters(in: .whitespaces)
        guard text.hasPrefix("[!"), let end = text.firstIndex(of: "]"),
              let kind = Kind(rawValue: text[text.index(text.startIndex, offsetBy: 2)..<end].lowercased()) else { return nil }
        let rest = text[text.index(after: end)...]
        guard rest.isEmpty || rest.first == " " || rest.first == "\t" else { return nil }
        return (kind, rest.trimmingCharacters(in: .whitespaces))
    }
}

/// Recently shown figures by fence. A figure depends only on its info
/// string and body, so one is shared by every message and theme that shows it.
public final class FigureParses: Sendable {
    public static let shared = FigureParses()
    private final class Entry: NSObject, Sendable {
        public let figure: Result<KitFigures.Figure, KitFigures.NotDrawn>
        public init(_ figure: Result<KitFigures.Figure, KitFigures.NotDrawn>) { self.figure = figure }
    }
    // NSCache is thread-safe.
    nonisolated(unsafe) private let entries = NSCache<NSString, Entry>()

    private init() {
        entries.countLimit = 128
    }

    public func figure(info: String, body: String) -> Result<KitFigures.Figure, KitFigures.NotDrawn> {
        // A line feed can't be part of an info string, so it separates the two.
        let key = "\(info)\n\(body)" as NSString
        if let entry = entries.object(forKey: key) { return entry.figure }
        let figure = KitFigures.Figure.parse(info: info, body: body)
        entries.setObject(Entry(figure), forKey: key)
        return figure
    }
}

/// A fenced Mermaid diagram (ADR 0046). It's drawn the first time it's
/// shown, and its drawing is kept by source, so reparsing a streaming
/// message or copying its text doesn't draw it again.
public struct MermaidDiagram: Equatable, Sendable {
    public let source: String

    public init(source: String) {
        self.source = source
    }

    /// The drawing, or why the diagram isn't drawn.
    public var drawing: Result<Canvas, KitMermaid.NotDrawn> {
        MermaidDrawings.shared.drawing(for: source)
    }

    public static func == (lhs: Self, rhs: Self) -> Bool { lhs.source == rhs.source }
}

/// Drawings of recently shown diagrams by source. A drawing depends only on
/// its source, so one is shared by every message and theme that shows it.
public final class MermaidDrawings: Sendable {
    public static let shared = MermaidDrawings()
    private final class Entry: NSObject, Sendable {
        public let drawing: Result<Canvas, KitMermaid.NotDrawn>
        public init(_ drawing: Result<Canvas, KitMermaid.NotDrawn>) { self.drawing = drawing }
    }
    // NSCache is thread-safe.
    nonisolated(unsafe) private let entries = NSCache<NSString, Entry>()

    private init() {
        entries.countLimit = 64
        entries.totalCostLimit = 16 * 1024 * 1024
    }

    public func drawing(for source: String) -> Result<Canvas, KitMermaid.NotDrawn> {
        let key = source as NSString
        if let entry = entries.object(forKey: key) { return entry.drawing }
        let drawing = Mermaid.render(source)
        // A drawn diagram keeps a few bytes for each of its cells.
        let cells = (try? drawing.get()).map { $0.width * $0.height } ?? 0
        entries.setObject(Entry(drawing), forKey: key, cost: source.utf8.count + 24 * cells)
        return drawing
    }
}

/// Avoid reparsing unchanged transcripts during scroll geometry and theme updates.
@MainActor
public final class MarkdownContentCache {
    public static let shared = MarkdownContentCache()
    private final class Entry: NSObject {
        public let content: MarkdownContent
        public init(_ content: MarkdownContent) { self.content = content }
    }
    private let entries = NSCache<NSString, Entry>()

    private init() {
        entries.countLimit = 64
        entries.totalCostLimit = 8 * 1024 * 1024
    }

    public func content(for source: String) -> MarkdownContent {
        let key = source as NSString
        if let entry = entries.object(forKey: key) { return entry.content }
        let content = MarkdownContent(source: source)
        entries.setObject(Entry(content), forKey: key, cost: source.utf8.count)
        return content
    }
}
