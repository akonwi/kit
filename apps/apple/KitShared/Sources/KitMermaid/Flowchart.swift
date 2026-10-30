import Foundation

// Mermaid flowcharts: a reviewed subset parsed into a graph and drawn with
// the layered layout. See `apps/cli/tui/mermaid/flowchart.ard`, the reference
// this ports. The parser reads each line as UTF-8 bytes, as the reference does.

enum Flowchart {
    static let maxNodes = 60
    static let maxEdges = 120
    static let maxStatements = 200
    static let maxGroups = 20
    static let maxHeight = 800
    /// Node text wider than this is cut short with an ellipsis.
    static let maxText = 40

    /// A subgraph: the subgraph it sits in, or -1 at the top level, and the
    /// direction it flows in when it names one.
    struct Group {
        var id: String
        var title: String
        var parent: Int
        var flow: Layers.Flow?
    }

    struct Chart {
        var nodes: [Layers.Node]
        var edges: [Layers.Edge]
        var flow: Layers.Flow
        var groups: [Group]
        /// Each node's subgraph, or -1 at the top level.
        var member: [Int]
        /// The subgraph a node's id names, or -1. Such a node stands for the
        /// subgraph in edges and is not drawn itself.
        var alias: [Int]
    }

    static func clean(_ value: String) -> String { cut(value, to: maxText) }

    /// Link text as one line, without the quotes it may be written in.
    private static func labelText(_ value: String) -> String {
        let text = Go.trimSpace(value)
        let quoted = text.size >= 2 && Go.hasPrefix(text, "\"") && Go.hasSuffix(text, "\"")
        return quoted ? clean(text.slice(start: 1, end: text.size - 1) ?? "") : clean(text)
    }

    private static func hasEdge(_ edges: [Layers.Edge], _ edge: Layers.Edge) -> Bool {
        edges.contains { $0.from == edge.from && $0.to == edge.to && $0.stroke == edge.stroke
            && $0.head == edge.head && $0.label == edge.label }
    }

    /// A line of source as bytes, read as the reference reads it.
    struct Line {
        let text: String
        let bytes: [UInt8]

        init(_ text: String) {
            self.text = text
            bytes = text.bytes
        }

        var size: Int { bytes.count }

        func byte(_ index: Int) -> UInt8? {
            index >= 0 && index < bytes.count ? bytes[index] : nil
        }

        func isByte(_ index: Int, _ value: Character) -> Bool {
            byte(index) == value.asciiValue
        }

        /// Whether the byte at index is one of the ASCII characters in chars.
        func isAny(_ index: Int, _ chars: String) -> Bool {
            guard let value = byte(index), value < 0x80 else { return false }
            return chars.utf8.contains(value)
        }

        func slice(_ start: Int, _ end: Int? = nil) -> String? {
            let stop = end ?? bytes.count
            guard start >= 0, start <= stop, stop <= bytes.count else { return nil }
            return String(bytes: bytes[start..<stop])
        }

        /// Whether a link starts at index.
        func linkStarts(_ index: Int) -> Bool {
            let current = byte(index), next = byte(index + 1)
            let dash = UInt8(ascii: "-"), dot = UInt8(ascii: "."), equal = UInt8(ascii: "=")
            let tilde = UInt8(ascii: "~"), less = UInt8(ascii: "<")
            return current == dash && (next == dash || next == dot) || current == equal && next == equal
                || current == tilde && next == tilde || current == less && (next == dash || next == equal)
        }

        func skipSpace(_ index: Int) -> Int {
            var at = index
            while at < size && (isByte(at, " ") || isByte(at, "\t")) { at += 1 }
            return at
        }

        /// The end of the name that starts at index: up to whitespace,
        /// punctuation that opens a shape or separates statements, or the
        /// start of a link.
        func nameEnd(_ index: Int) -> Int {
            var at = index
            while at < size && !isAny(at, " \t[](){}<>&;|\":") && !linkStarts(at) { at += 1 }
            return at
        }

        /// The end of the run of chars characters starting at index.
        func runOf(_ index: Int, _ chars: String) -> Int {
            var at = index
            while isAny(at, chars) { at += 1 }
            return at
        }

        /// The end of the link token starting at index: its line, then a head.
        func linkEnd(_ index: Int) -> Int {
            let end = runOf(index, "-=.~<")
            return isAny(end, ">xo") ? end + 1 : end
        }

        /// Whether a link starts anywhere outside quoted text.
        func hasLink() -> Bool {
            var found = false
            var quoted = false
            for index in 0..<size {
                if isByte(index, "\"") { quoted = !quoted }
                if !quoted && linkStarts(index) { found = true }
            }
            return found
        }
    }

    private struct Link {
        var stroke = Layers.Stroke.solid
        var head = Layers.Head.none
        var label = ""
    }

    /// Where the scanner is after reading part of a statement.
    private struct Scan {
        var next: Int
        var nodes: [Int] = []
        var link = Link()
    }

    /// The stroke and head of a whole link token such as `-->`, `-.-`, or `==>`.
    private static func classify(_ token: String) throws -> Link {
        let last = token.slice(start: token.size - 1) ?? ""
        let head: Layers.Head
        switch last {
        case ">": head = .arrow
        case "x": head = .cross
        case "o": head = .circle
        default: head = .none
        }
        let line = head == .none ? token : token.slice(end: token.size - 1) ?? ""
        let headed = head != .none
        let dashes = line.size >= 2 && Go.trim(line, "-") == ""
        let equals = line.size >= 2 && Go.trim(line, "=") == ""
        // `-.-` and `-..-`, with or without a head.
        let inner = line.slice(start: 1, end: line.size - 1) ?? ""
        let dotted = line.size >= 3 && Go.hasPrefix(line, "-") && Go.hasSuffix(line, "-") && inner != ""
            && Go.trim(inner, ".") == ""
        if dashes && (headed || line.size >= 3) { return Link(stroke: .solid, head: head) }
        if equals && (headed || line.size >= 3) { return Link(stroke: .thick, head: head) }
        if dotted { return Link(stroke: .dotted, head: head) }
        throw NotDrawn("`\(token)` links aren't drawn yet")
    }

    private struct Parser {
        var nodes: [Layers.Node] = []
        var indexes: [[UInt8]: Int] = [:]
        var edges: [Layers.Edge] = []
        /// Statements read so far, counting each one between `;`s.
        var statements = 0
        var groups: [Group] = []
        /// The subgraphs open where the parser is, innermost last.
        var open: [Int] = []
        var member: [Int] = []
        /// The nodes each subgraph's own statements mention.
        var mentions: [[Int]] = []
        /// Whether each node was given text or a shape.
        var explicit: [Bool] = []

        /// The index of node id, declaring it on first use. Text, when given,
        /// replaces the node's text.
        mutating func node(_ id: String, text: String?, shape: Layers.Shape) -> Int {
            let index: Int
            if let found = indexes[id.bytes] {
                index = found
            } else {
                index = nodes.count
                nodes.append(Layers.Node(id: id, text: clean(id), shape: .box))
                indexes[id.bytes] = index
                member.append(-1)
                explicit.append(false)
            }
            let current = open.last ?? -1
            if current >= 0 {
                mentions[current].append(index)
            }
            if let text {
                explicit[index] = true
                nodes[index] = Layers.Node(id: id, text: clean(text), shape: shape)
            }
            return index
        }

        /// One node: an id, then optionally a shape with text and a class suffix.
        mutating func one(_ line: Line, _ start: Int) throws -> Scan {
            let end = line.nameEnd(start)
            let id = line.slice(start, end) ?? ""
            var at = end
            var text: String?
            var shape = Layers.Shape.box
            var failure = ""
            let rest = line.slice(at) ?? ""
            // The opening of each supported shape, longest first.
            let opened: String?
            if Go.hasPrefix(rest, "(((") {
                opened = nil
            } else if Go.hasPrefix(rest, "((") {
                opened = "(("
            } else if Go.hasPrefix(rest, "([") {
                opened = "(["
            } else if Go.hasPrefix(rest, "(") {
                opened = "("
            } else if Go.hasPrefix(rest, "[(") {
                opened = "[("
            } else if Go.hasPrefix(rest, "[[") || Go.hasPrefix(rest, "[/") || Go.hasPrefix(rest, "[\\") {
                opened = nil
            } else if Go.hasPrefix(rest, "[") {
                opened = "["
            } else if Go.hasPrefix(rest, "{{") {
                opened = nil
            } else if Go.hasPrefix(rest, "{") {
                opened = "{"
            } else {
                opened = nil
            }
            let shaped = line.isAny(at, "[({>")
            if id == "" {
                failure = "unsupported line `\(line.text)`"
            } else if shaped && opened == nil {
                failure = "`\(rest.slice(end: 2) ?? rest)` nodes aren't drawn yet"
            } else if let open = opened {
                let close: String
                switch open {
                case "((": close = "))"; shape = .circle
                case "([": close = "])"; shape = .stadium
                case "(": close = ")"; shape = .round
                case "[(": close = ")]"; shape = .database
                case "{": close = "}"; shape = .decision
                default: close = "]"; shape = .box
                }
                let content = at + open.size
                let quoted = line.isByte(content, "\"")
                let closing: Int
                if quoted {
                    let quote = Go.index(line.slice(content + 1) ?? "", "\"")
                    if quote < 0 {
                        closing = -1
                    } else {
                        let after = content + 1 + quote + 1
                        closing = Go.hasPrefix(line.slice(after) ?? "", close) ? after : -1
                    }
                } else {
                    let found = Go.index(line.slice(content) ?? "", close)
                    closing = found < 0 ? -1 : content + found
                }
                if closing < 0 {
                    failure = "unclosed `\(open)` in `\(line.text)`"
                } else {
                    var value = line.slice(content, closing) ?? ""
                    if quoted {
                        value = value.slice(start: 1, end: value.size - 1) ?? ""
                    }
                    text = value
                    at = closing + close.size
                }
            }
            if failure == "" && line.slice(at, at + 3) ?? "" == ":::" {
                at = line.nameEnd(at + 3)
            }
            if failure != "" { throw NotDrawn(failure) }
            return Scan(next: at, nodes: [node(id, text: text, shape: shape)])
        }

        /// Nodes joined by `&`.
        mutating func group(_ line: Line, _ start: Int) throws -> Scan {
            var found: [Int] = []
            var at = line.skipSpace(start)
            var more = true
            while more {
                let scan = try one(line, at)
                found.append(contentsOf: scan.nodes)
                at = line.skipSpace(scan.next)
                more = line.isByte(at, "&")
                if more { at = line.skipSpace(at + 1) }
                // Ids naming subgraphs are counted out after parsing.
                if nodes.count > maxNodes + maxGroups { throw NotDrawn("more than \(maxNodes) nodes") }
            }
            return Scan(next: at, nodes: found)
        }

        /// A link between groups, with its text when it has one.
        mutating func link(_ line: Line, _ start: Int) throws -> Scan {
            let runEnd = line.runOf(start, "-=.")
            let run = line.slice(start, runEnd) ?? ""
            let opening = run == "--" || run == "-." || run == "=="
            let spaced = line.isByte(runEnd, " ")
            if opening && spaced {
                // `-- text -->`: the text runs up to the closing part of the link.
                let closings: [String]
                switch run {
                case "-.": closings = [".->", ".-"]
                case "==": closings = ["==>", "==="]
                default: closings = ["-->", "---", "--x", "--o"]
                }
                let rest = line.slice(runEnd) ?? ""
                var found = -1
                for closing in closings {
                    let at = Go.index(rest, closing)
                    if at >= 0 && (found < 0 || at < found) { found = at }
                }
                if found < 0 { throw NotDrawn("unsupported link in `\(line.text)`") }
                let label = Go.trimSpace(rest.slice(end: found) ?? "")
                let closeStart = runEnd + found
                let closeEnd = line.linkEnd(closeStart)
                var link = try classify(run + (line.slice(closeStart, closeEnd) ?? ""))
                link.label = labelText(label)
                return Scan(next: closeEnd, link: link)
            }
            let end = line.linkEnd(start)
            var link = try classify(line.slice(start, end) ?? "")
            // `-->|text|`, or `--> |text|`
            let bar = line.skipSpace(end)
            guard line.isByte(bar, "|") else { return Scan(next: end, link: link) }
            let close = Go.index(line.slice(bar + 1) ?? "", "|")
            if close < 0 { throw NotDrawn("unclosed `|` in `\(line.text)`") }
            link.label = labelText(line.slice(bar + 1, bar + 1 + close) ?? "")
            return Scan(next: bar + 1 + close + 1, link: link)
        }

        /// A statement: groups joined by links. Returns where it ended.
        mutating func statement(_ line: Line, _ start: Int) throws -> Int {
            statements += 1
            if statements > maxStatements { throw NotDrawn("more than \(maxStatements) statements") }
            let first = try group(line, start)
            var previous = first.nodes
            var at = line.skipSpace(first.next)
            while at < line.size && !line.isByte(at, ";") {
                guard line.linkStarts(at) else { throw NotDrawn("unsupported line `\(line.text)`") }
                let link = try link(line, at)
                let next = try group(line, link.next)
                // Checked before adding, since `&` groups multiply edges.
                if edges.count + previous.count * next.nodes.count > maxEdges {
                    throw NotDrawn("more than \(maxEdges) edges")
                }
                for from in previous {
                    for to in next.nodes {
                        let edge = Layers.Edge(from: from, to: to, stroke: link.link.stroke, head: link.link.head,
                                               label: link.link.label)
                        // A repeated edge is drawn once.
                        if !hasEdge(edges, edge) { edges.append(edge) }
                    }
                }
                previous = next.nodes
                at = line.skipSpace(next.next)
            }
            return at + 1
        }

        mutating func line(_ text: String) throws {
            let line = Line(text)
            let space = Go.indexAny(text, " \t")
            // Keywords match exactly, as in Mermaid, so `End` or `Style` can name a node.
            let word = space >= 0 ? text.slice(end: space) ?? text : text
            switch word {
            case "subgraph":
                // As in Mermaid, a subgraph takes the nodes its statements
                // mention when it ends, unless a subgraph that ended before
                // took them, so an inner subgraph takes its nodes before its parent.
                try subgraph(Go.trimSpace(text.slice(start: word.size) ?? ""))
            case "end", "end;":
                let closing = open.last ?? -1
                if text != "end" && text != "end;" { throw NotDrawn("unexpected text after `end`") }
                if closing < 0 { throw NotDrawn("`end` without a `subgraph`") }
                for node in mentions.at(closing) ?? [] where (member.at(node) ?? -1) < 0 {
                    member.put(node, closing)
                }
                open.removeLast()
            case "direction":
                let direction = Go.toUpper(Go.trimSuffix(Go.trimSpace(text.slice(start: word.size) ?? ""), ";"))
                let current = open.last ?? -1
                if current < 0 { throw NotDrawn("`direction` is only drawn inside a subgraph") }
                guard let flow = flow(direction) else { throw NotDrawn("unknown direction `\(direction)`") }
                groups[current].flow = flow
            case "classDef", "class", "style", "linkStyle", "click":
                // Styling is accepted without being drawn, but a link means
                // the line isn't styling.
                if line.hasLink() { throw NotDrawn("unsupported line `\(text)`") }
            default:
                var at = 0
                while at < line.size {
                    at = line.skipSpace(at)
                    if at < line.size { at = try statement(line, at) }
                }
            }
        }

        /// Opens a subgraph: `subgraph id`, `subgraph id[title]`, or `subgraph "title"`.
        mutating func subgraph(_ rest: String) throws {
            let quoted = rest.size >= 2 && Go.hasPrefix(rest, "\"") && Go.hasSuffix(rest, "\"")
            let bracket = Go.index(rest, "[")
            var id = rest
            var title = rest
            var failure = ""
            if quoted {
                id = labelText(rest)
                title = id
            } else if bracket >= 0 {
                if Go.hasSuffix(rest, "]") {
                    id = Go.trimSpace(rest.slice(end: bracket) ?? "")
                    title = labelText(rest.slice(start: bracket + 1, end: rest.size - 1) ?? "")
                } else {
                    failure = "unclosed `[` in `subgraph \(rest)`"
                }
            }
            for group in groups where group.id.bytes == id.bytes {
                failure = "subgraph `\(id)` is declared twice"
            }
            if failure != "" { throw NotDrawn(failure) }
            if id == "" { throw NotDrawn("a subgraph needs a name") }
            if groups.count >= maxGroups { throw NotDrawn("more than \(maxGroups) subgraphs") }
            open.append(groups.count)
            mentions.append([])
            groups.append(Group(id: id, title: clean(title), parent: open.at(open.count - 2) ?? -1, flow: nil))
        }
    }

    /// The flow a header's direction names.
    private static func flow(_ direction: String) -> Layers.Flow? {
        switch direction {
        case "TD", "TB": return .down
        case "BT": return .up
        case "LR": return .right
        case "RL": return .left
        default: return nil
        }
    }

    /// Parses the statements after a flowchart header.
    static func parse(header: String, lines: [String]) throws -> Chart {
        let fields = Go.fields(header)
        // `graph TD;` ends its header with a `;`.
        let direction = Go.toUpper(Go.trimSuffix(fields.at(1) ?? "TD", ";"))
        var parser = Parser()
        if fields.count > 2 { throw NotDrawn("unexpected text after `\(fields.at(0) ?? "") \(direction)`") }
        guard let headerFlow = flow(direction) else { throw NotDrawn("unknown direction `\(direction)`") }
        for raw in lines {
            let line = Go.trimSpace(raw)
            if line != "" && !Go.hasPrefix(line, "%%") {
                try parser.line(line)
            }
        }
        if let open = parser.open.last {
            throw NotDrawn("subgraph `\(parser.groups[open].id)` isn't closed with `end`")
        }
        // A node whose id names a subgraph stands for it.
        var alias: [Int] = []
        var aliases = 0
        for (index, node) in parser.nodes.enumerated() {
            var named = -1
            for (number, group) in parser.groups.enumerated() where group.id.bytes == node.id.bytes {
                named = number
            }
            if named >= 0 && parser.explicit[index] {
                throw NotDrawn("`\(node.id)` names both a node and a subgraph")
            }
            if named >= 0 { aliases += 1 }
            alias.append(named)
        }
        if parser.nodes.isEmpty { throw NotDrawn("the flowchart has no nodes") }
        if parser.nodes.count - aliases > maxNodes { throw NotDrawn("more than \(maxNodes) nodes") }
        if parser.edges.count > maxEdges { throw NotDrawn("more than \(maxEdges) edges") }
        return Chart(nodes: parser.nodes, edges: parser.edges, flow: headerFlow, groups: parser.groups,
                     member: parser.member, alias: alias)
    }

    /// Draws the statements after a flowchart header.
    static func render(header: String, lines: [String], title: String) throws -> Canvas {
        let chart = try parse(header: header, lines: lines)
        if chart.groups.isEmpty {
            return try Layers.draw(Layers.Graph(nodes: chart.nodes, edges: chart.edges), title: clean(title),
                                   flow: chart.flow)
        }
        return try sections(chart, title: clean(title))
    }

    /// Draws the flowchart with each subgraph as one box, then each subgraph
    /// on its own beneath it, in the order they are declared.
    private static func sections(_ chart: Chart, title: String) throws -> Canvas {
        // An edge can appear in each section along its way, so the sections
        // are bounded together: each counts as the square of its nodes and
        // edges, and together they may cost what the largest flowchart
        // without subgraphs does.
        var graphs: [Layers.Graph] = []
        var work = 0
        for group in -1..<chart.groups.count {
            let graph = section(chart, group)
            let size = graph.nodes.count + graph.edges.count
            work += size * size
            graphs.append(graph)
        }
        if work > (maxNodes + maxEdges) * (maxNodes + maxEdges) {
            throw NotDrawn("the subgraphs repeat too many nodes and edges to draw")
        }
        var parts: [Canvas] = []
        var height = 0
        for (offset, graph) in graphs.enumerated() where !graph.nodes.isEmpty {
            let group = offset - 1
            let heading = group < 0 ? title : chart.groups[group].title
            do {
                let drawing = try Layers.draw(graph, title: heading, flow: sectionFlow(chart, group))
                height += drawing.height + 1
                parts.append(drawing)
            } catch let notDrawn as NotDrawn {
                throw group < 0 ? notDrawn : NotDrawn("in subgraph `\(chart.groups[group].id)`, \(notDrawn.reason)")
            }
            // Stop as soon as the sections can't fit.
            if height > maxHeight + 1 { throw NotDrawn("the flowchart is taller than \(maxHeight) rows") }
        }
        return Canvas.stack(parts)
    }

    /// The direction a subgraph flows in: its own, else its parent's.
    private static func sectionFlow(_ chart: Chart, _ group: Int) -> Layers.Flow {
        var at = group
        while at >= 0 {
            let current = chart.groups[at]
            if let flow = current.flow { return flow }
            at = current.parent
        }
        return chart.flow
    }

    /// Whether subgraph group lies within outer, or is it; -1 is the whole flowchart.
    private static func inside(_ chart: Chart, _ group: Int, _ outer: Int) -> Bool {
        if outer < 0 { return true }
        var at = group
        while at >= 0 {
            if at == outer { return true }
            at = chart.groups[at].parent
        }
        return false
    }

    /// The subgraph directly inside outer that holds group.
    private static func childOf(_ chart: Chart, _ group: Int, _ outer: Int) -> Int {
        var at = group
        var parent = chart.groups[at].parent
        while parent != outer && parent >= 0 {
            at = parent
            parent = chart.groups[at].parent
        }
        return at
    }

    /// The box standing for a subgraph drawn on its own.
    private static func groupBox(_ chart: Chart, _ group: Int) -> Layers.Node {
        let found = chart.groups[group]
        return Layers.Node(id: found.id, text: clean("▸ \(found.title)"), shape: .elsewhere)
    }

    /// What an end of an edge is in the drawing of subgraph outer: the node
    /// itself (`n`), the box of the subgraph inside outer that holds it
    /// (`g`), a box for outer as a whole (`s`), or a box for something
    /// outside (`x`).
    private struct End {
        let key: String
        let node: Layers.Node
    }

    private static func groupEnd(_ chart: Chart, _ group: Int, _ outer: Int) -> End {
        let found = chart.groups[group]
        if group == outer {
            return End(key: "s", node: Layers.Node(id: found.id, text: clean("all of \(found.title)"), shape: .elsewhere))
        }
        if inside(chart, group, outer) {
            let child = childOf(chart, group, outer)
            return End(key: "g\(child)", node: groupBox(chart, child))
        }
        return End(key: "xg\(group)",
                   node: Layers.Node(id: found.id, text: placeName(chart, found.parent, found.title), shape: .elsewhere))
    }

    /// text named with the subgraph that holds it, if any.
    private static func placeName(_ chart: Chart, _ group: Int, _ text: String) -> String {
        group < 0 ? text : clean("\(chart.groups[group].title) · \(text)")
    }

    private static func nodeEnd(_ chart: Chart, _ node: Int, _ outer: Int) -> End {
        let alias = chart.alias.at(node) ?? -1
        let group = chart.member.at(node) ?? -1
        let found = chart.nodes[node]
        if alias >= 0 { return groupEnd(chart, alias, outer) }
        if group == outer { return End(key: "n\(node)", node: found) }
        if group >= 0 && inside(chart, group, outer) { return groupEnd(chart, group, outer) }
        return End(key: "xn\(node)",
                   node: Layers.Node(id: found.id, text: placeName(chart, group, found.text), shape: .elsewhere))
    }

    /// The graph drawn for subgraph outer, or the whole flowchart for -1: its
    /// own nodes, a box for each subgraph inside it, and a box for each node
    /// outside it that its edges reach. Edges within one inner subgraph are
    /// left to that subgraph's drawing, and repeated edges are drawn once.
    private static func section(_ chart: Chart, _ outer: Int) -> Layers.Graph {
        var nodes: [Layers.Node] = []
        var keys: [String: Int] = [:]
        func add(_ end: End) -> Int {
            if let found = keys[end.key] { return found }
            let index = nodes.count
            nodes.append(end.node)
            keys[end.key] = index
            return index
        }
        for index in chart.nodes.indices {
            let end = nodeEnd(chart, index, outer)
            if (chart.alias.at(index) ?? -1) < 0 && !Go.hasPrefix(end.key, "x") {
                _ = add(end)
            }
        }
        for (index, group) in chart.groups.enumerated() where group.parent == outer {
            _ = add(groupEnd(chart, index, outer))
        }
        var edges: [Layers.Edge] = []
        for edge in chart.edges {
            let from = nodeEnd(chart, edge.from, outer)
            let to = nodeEnd(chart, edge.to, outer)
            let outside = Go.hasPrefix(from.key, "x") && Go.hasPrefix(to.key, "x")
            let inner = from.key == to.key && Go.hasPrefix(from.key, "g")
            if !(outside || inner) {
                let mapped = Layers.Edge(from: add(from), to: add(to), stroke: edge.stroke, head: edge.head,
                                         label: edge.label)
                if !hasEdge(edges, mapped) { edges.append(mapped) }
            }
        }
        return Layers.Graph(nodes: nodes, edges: edges)
    }
}
