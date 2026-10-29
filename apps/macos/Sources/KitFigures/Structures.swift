import Foundation

/// `tree`: nested `- ` items, two spaces per level, drawn with guides from
/// each node to its parent. Any number of nodes may be bold.
enum Tree {
    static let maxNodes = 200
    static let maxDepth = 8

    // `.` matches line separators such as U+2028, as it does in Go.
    private static let nodePattern = try! NSRegularExpression(pattern: "^([ \\t]*)- (.*)\\z", options: [.dotMatchesLineSeparators])

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title"]) { return .failure(failure) }
        guard body.utf8.count <= Grammar.maxBody else {
            return .failure(NotDrawn("the figure is larger than \(Grammar.maxBody / 1024) KiB"))
        }
        var made: [Figure.Item] = []
        var previous = 0
        for raw in body.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n") {
            let line = Grammar.trimRight(raw, " \t\r")
            let trimmed = Grammar.trimSpace(line)
            if trimmed.isEmpty { continue }
            guard let match = nodePattern.firstMatch(in: line, range: NSRange(line.startIndex..., in: line)),
                  let indentRange = Range(match.range(at: 1), in: line),
                  let textRange = Range(match.range(at: 2), in: line) else {
                return .failure(NotDrawn("expected a `- ` tree node, got `\(Grammar.clip(trimmed))`"))
            }
            let indent = line[indentRange]
            let level = indent.utf8.count / 2 + 1
            if indent.contains("\t") || indent.utf8.count % 2 == 1 {
                return .failure(NotDrawn("a tree is indented with two spaces per level, got `\(Grammar.clip(trimmed))`"))
            }
            if level > previous + 1 {
                return .failure(NotDrawn("a node is nested more than one level below its parent, got `\(Grammar.clip(trimmed))`"))
            }
            if level > maxDepth { return .failure(NotDrawn("a tree is at most \(maxDepth) levels deep")) }
            let each = Grammar.line(String(line[textRange]))
            if each.text.isEmpty { return .failure(NotDrawn("a tree node needs a name, got `\(Grammar.clip(trimmed))`")) }
            var item = Figure.Item(label: each.text, value: "", note: each.note, accent: each.bold, muted: each.italic)
            item.level = level
            made.append(item)
            previous = level
        }
        if made.isEmpty { return .failure(NotDrawn("the figure has no nodes")) }
        if made.count > maxNodes { return .failure(NotDrawn("a tree has at most \(maxNodes) nodes")) }
        return .success(Figure(kind: .tree, title: info.attribute("title"), items: made))
    }
}

extension Figure {
    /// One column of the guides before a tree node, for each level above
    /// it: a line that runs on to a later sibling, none, or for the node's
    /// own level, a line that goes on past it or ends at it.
    public enum Guide: Sendable { case none, through, branch, last }

    /// Whether the node at index has a later sibling: a node at its level
    /// before any node above it.
    private static func continues(_ items: [Figure.Item], _ index: Int, _ level: Int) -> Bool {
        for next in items[(index + 1)...] {
            if next.level < level { return false }
            if next.level == level { return true }
        }
        return false
    }

    /// The guides before each node, as the terminal draws them with `│`,
    /// `├─`, and `└─`.
    public var guides: [[Guide]] {
        items.indices.map { index in
            let level = items[index].level
            guard level > 1 else { return [] }
            var columns: [Guide] = (2..<level).map { ancestorLevel in
                let ancestor = items[..<index].lastIndex { $0.level == ancestorLevel } ?? index
                return Self.continues(items, ancestor, ancestorLevel) ? .through : .none
            }
            columns.append(Self.continues(items, index, level) ? .branch : .last)
            return columns
        }
    }
}

/// `compare`: a table of options side by side, rows of features against
/// them, and the bold option as the pick.
enum Compare {
    static let maxRows = 30
    static let maxOptions = 6

    private static let dividerPattern = try! NSRegularExpression(pattern: "^:?-+:?\\z")

    /// A table row's cells, without the pipes around them.
    private static func cells(_ line: String) -> [String] {
        var inner = Grammar.hasPrefix(line, "|") ? Grammar.slice(line, 1) : line
        if Grammar.hasSuffix(inner, "|") { inner = Grammar.slice(inner, 0, inner.utf8.count - 1) }
        return Grammar.split(inner, "|").map { Grammar.trimSpace(Grammar.clean($0)) }
    }

    private static func isDivider(_ row: [String]) -> Bool {
        !row.isEmpty && row.allSatisfy { dividerPattern.firstMatch(in: $0, range: NSRange($0.startIndex..., in: $0)) != nil }
    }

    private static func table(_ body: String) -> Result<(heading: [String], rows: [[String]]), NotDrawn> {
        let lines = body.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")
            .map(Grammar.trimSpace).filter { !$0.isEmpty }
        if let line = lines.first(where: { !Grammar.hasPrefix($0, "|") }) {
            return .failure(NotDrawn("a compare row is a table row, as in `| label | a | b |`, got `\(Grammar.clip(line))`"))
        }
        let rows = lines.map(cells)
        let heading = rows.first ?? []
        for (index, row) in rows.enumerated().dropFirst(2) where row.count != heading.count {
            return .failure(NotDrawn("every compare row has \(heading.count) cells, got \(row.count) in `\(Grammar.clip(lines[index]))`"))
        }
        guard rows.count > 1, isDivider(rows[1]) else {
            return .failure(NotDrawn("a compare table's second row is its `| --- |` divider"))
        }
        return .success((heading, Array(rows.dropFirst(2))))
    }

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title"]) { return .failure(failure) }
        guard body.utf8.count <= Grammar.maxBody else {
            return .failure(NotDrawn("the figure is larger than \(Grammar.maxBody / 1024) KiB"))
        }
        let parsed: (heading: [String], rows: [[String]])
        switch table(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let table): parsed = table
        }
        let options = parsed.heading.dropFirst().map(Grammar.emphasis)
        guard (1...maxOptions).contains(options.count) else {
            return .failure(NotDrawn("a compare table has 1 to \(maxOptions) options, got \(options.count)"))
        }
        if options.contains(where: { $0.text.isEmpty }) { return .failure(NotDrawn("every compare option needs a heading")) }
        if options.filter(\.bold).count > 1 { return .failure(NotDrawn("only one option can be bold")) }
        if parsed.rows.isEmpty { return .failure(NotDrawn("the figure has no rows")) }
        if parsed.rows.count > maxRows { return .failure(NotDrawn("a compare table has at most \(maxRows) rows")) }
        let made = parsed.rows.map { row in
            var item = Figure.Item(label: row.first ?? "", value: "")
            item.cells = Array(row.dropFirst())
            return item
        }
        var figure = Figure(kind: .compare, title: info.attribute("title"), items: made)
        figure.headings = options.map(\.text)
        figure.pick = options.firstIndex(where: \.bold) ?? -1
        return .success(figure)
    }
}
