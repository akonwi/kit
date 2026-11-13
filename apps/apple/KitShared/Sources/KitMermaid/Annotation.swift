import Foundation

// Annotations on a drawn Mermaid diagram: the text the model gets for a
// comment on one of its parts, ported from the reference in
// `apps/cli/tui/mermaid/annotation.ard` and held to the fixtures in
// `testdata/mermaid/annotations`. Each annotation is a block appended to the
// message: the person's comment, then what Kit read from the diagram, fenced
// as untrusted with a nonce since the diagram's text came from a reply. The
// diagram's source goes in the first block that annotates it; later blocks on
// the same diagram point back to that one.

/// One annotation in a message: its comment on a part of a diagram's
/// source, and the nonce its fence is tagged with.
public struct DiagramAnnotation: Equatable, Sendable {
    public var comment: String
    public var source: String
    public var part: Part
    public var nonce: String

    public init(comment: String, source: String, part: Part,
                nonce: String = UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()) {
        self.comment = comment; self.source = source; self.part = part; self.nonce = nonce
    }
}

public enum DiagramAnnotationText {
    public static let open = "<diagram-annotation n=\""
    public static let close = "</diagram-annotation>"
    static let fenceName = "UNTRUSTED_DIAGRAM_CONTENT"
    /// The most edges, members, or statements listed for one target; the
    /// rest are counted.
    static let maxListed = 20

    // MARK: Describing a part

    private static func quoted(_ text: String) -> String {
        "\"" + Go.replaceAll(Go.replaceAll(text, "\\", "\\\\"), "\"", "\\\"") + "\""
    }

    /// id with its label in quotes when the label says something else.
    private static func named(_ id: String, _ label: String) -> String {
        label == "" || label.bytes == id.bytes ? id : "\(id) \(quoted(label))"
    }

    /// items joined with "; ", the first maxListed of them, then how many more.
    private static func listed(_ items: [String]) -> String {
        if items.isEmpty { return "none" }
        let kept = items.prefix(maxListed).joined(separator: "; ")
        return items.count > maxListed ? kept + "; and \(items.count - maxListed) more" : kept
    }

    private static func shapeName(_ shape: Layers.Shape) -> String {
        switch shape {
        case .box, .elsewhere: "box"
        case .round: "rounded"
        case .stadium: "stadium"
        case .circle: "circle"
        case .database: "database"
        case .decision: "decision"
        }
    }

    /// An edge's link as Mermaid writes it, with its text.
    private static func link(_ edge: Layers.Edge) -> String {
        let head = switch edge.head {
        case .arrow: ">"
        case .cross: "x"
        case .circle: "o"
        case .none: ""
        }
        let line = switch edge.stroke {
        case .solid: head == "" ? "---" : "--\(head)"
        case .dotted: head == "" ? "-.-" : "-.-\(head)"
        case .thick: head == "" ? "===" : "==\(head)"
        }
        return edge.label == "" ? line : "\(line)|\(edge.label)|"
    }

    private static func endName(_ chart: Flowchart.Chart, _ node: Int) -> String {
        let group = chart.alias.at(node) ?? -1
        if group >= 0 { return "subgraph " + groupName(chart, group) }
        let found = chart.nodes[node]
        return named(found.id, found.text)
    }

    private static func edgeText(_ chart: Flowchart.Chart, _ edge: Layers.Edge) -> String {
        "\(chart.nodes[edge.from].id) \(link(edge)) \(chart.nodes[edge.to].id)"
    }

    private static func groupName(_ chart: Flowchart.Chart, _ group: Int) -> String {
        let found = chart.groups[group]
        return named(found.id, Flowchart.clean(found.title))
    }

    private static func inFlowchart(_ chart: Flowchart.Chart, _ part: Part) -> Bool {
        let index = part.index
        guard index >= 0 else { return false }
        switch part.target {
        case .node: return index < chart.nodes.count && (chart.alias.at(index) ?? -1) < 0
        case .subgraph: return index < chart.groups.count
        case .edge: return index < chart.edges.count
        default: return false
        }
    }

    /// The lines describing a flowchart's part.
    private static func flowchartLines(_ chart: Flowchart.Chart, _ part: Part) -> [String] {
        let index = part.index
        var lines: [String] = []
        switch part.target {
        case .node:
            let node = chart.nodes[index]
            lines.append("Target: node \(named(node.id, node.text)) (\(shapeName(node.shape)))")
            let group = chart.member.at(index) ?? -1
            if group >= 0 { lines.append("In subgraph: \(groupName(chart, group))") }
            let into = chart.edges.filter { $0.to == index }.map { edgeText(chart, $0) }
            let out = chart.edges.filter { $0.from == index }.map { edgeText(chart, $0) }
            var neighbors: [String] = []
            var seen: [Int] = []
            for edge in chart.edges where edge.to == index && edge.from != index && !seen.contains(edge.from) {
                seen.append(edge.from)
                neighbors.append(endName(chart, edge.from))
            }
            for edge in chart.edges where edge.from == index && edge.to != index && !seen.contains(edge.to) {
                seen.append(edge.to)
                neighbors.append(endName(chart, edge.to))
            }
            lines.append("Edges in: \(listed(into))")
            lines.append("Edges out: \(listed(out))")
            lines.append("Neighbors: \(listed(neighbors))")
        case .subgraph:
            let group = chart.groups[index]
            lines.append("Target: subgraph \(groupName(chart, index))")
            if group.parent >= 0 { lines.append("In subgraph: \(groupName(chart, group.parent))") }
            var members: [String] = []
            for (node, found) in chart.nodes.enumerated()
            where chart.member.at(node) ?? -1 == index && (chart.alias.at(node) ?? -1) < 0 {
                members.append(named(found.id, found.text))
            }
            for (inner, found) in chart.groups.enumerated() where found.parent == index {
                members.append("subgraph \(groupName(chart, inner))")
            }
            lines.append("Holds: \(listed(members))")
            var into: [String] = []
            var out: [String] = []
            for edge in chart.edges {
                if chart.alias.at(edge.to) ?? -1 == index { into.append(edgeText(chart, edge)) }
                if chart.alias.at(edge.from) ?? -1 == index { out.append(edgeText(chart, edge)) }
            }
            if !into.isEmpty { lines.append("Edges in: \(listed(into))") }
            if !out.isEmpty { lines.append("Edges out: \(listed(out))") }
        case .edge:
            let edge = chart.edges[index]
            lines.append("Target: edge \(edgeText(chart, edge))")
            lines.append("From: \(endName(chart, edge.from))")
            lines.append("To: \(endName(chart, edge.to))")
        default: break
        }
        return lines
    }

    private static func participantName(_ diagram: Sequence.Diagram, _ index: Int) -> String {
        let found = diagram.participants[index]
        return named(found.id, found.label)
    }

    /// A message as Mermaid writes it.
    private static func messageText(_ diagram: Sequence.Diagram, _ item: Sequence.Event) -> String {
        let statement = diagram.participants[item.from].id + Sequence.arrow(item) + diagram.participants[item.to].id
        return item.written == "" ? statement : "\(statement): \(item.written)"
    }

    /// A note as Mermaid writes it.
    private static func noteText(_ diagram: Sequence.Diagram, _ item: Sequence.Event) -> String {
        let id = { (index: Int) in diagram.participants[index].id }
        let place = switch item.placement {
        case .left: "left of \(id(item.from))"
        case .right: "right of \(id(item.to))"
        case .over: item.from == item.to ? "over \(id(item.from))" : "over \(id(item.from)),\(id(item.to))"
        }
        return "Note \(place): \(item.text)"
    }

    /// A block's opening or divider statement.
    private static func blockText(_ item: Sequence.Event) -> String {
        item.text == "" ? item.keyword : "\(item.keyword) \(item.text)"
    }

    /// How many statements of kind come before event index, counting it.
    private static func ordinal(_ diagram: Sequence.Diagram, _ kind: Sequence.Kind, _ index: Int) -> Int {
        diagram.events.enumerated().filter { $0.offset <= index && $0.element.kind == kind }.count
    }

    private static func total(_ diagram: Sequence.Diagram, _ kind: Sequence.Kind) -> Int {
        diagram.events.filter { $0.kind == kind }.count
    }

    /// The blocks around event index, outermost first, each as its opening
    /// statement and, past a divider, the divider's.
    private static func enclosing(_ diagram: Sequence.Diagram, _ index: Int) -> String {
        var opens: [String] = []
        var sections: [String] = []
        for item in diagram.events.prefix(max(index, 0)) {
            switch item.kind {
            case .open:
                opens.append(blockText(item))
                sections.append("")
            case .divider:
                if !sections.isEmpty { sections[sections.count - 1] = blockText(item) }
            case .close:
                if !opens.isEmpty {
                    opens.removeLast()
                    sections.removeLast()
                }
            default: break
            }
        }
        return zip(opens, sections).map { $1 == "" ? $0 : "\($0) / \($1)" }.joined(separator: " > ")
    }

    private static func inSequence(_ diagram: Sequence.Diagram, _ part: Part) -> Bool {
        let index = part.index
        guard index >= 0 else { return false }
        let kind = diagram.events.at(index)?.kind
        switch part.target {
        case .participant: return index < diagram.participants.count
        case .message: return kind == .message
        case .note: return kind == .note
        case .block: return kind == .open
        default: return false
        }
    }

    /// The lines describing a sequence diagram's part.
    private static func sequenceLines(_ diagram: Sequence.Diagram, _ part: Part) -> [String] {
        let index = part.index
        var lines: [String] = []
        func inside() {
            let around = enclosing(diagram, index)
            if around != "" { lines.append("Inside: \(around)") }
        }
        switch part.target {
        case .participant:
            let found = diagram.participants[index]
            lines.append("Target: \(found.actor ? "actor" : "participant") \(participantName(diagram, index))")
            var sent: [String] = []
            for (at, item) in diagram.events.enumerated()
            where item.kind == .message && (item.from == index || item.to == index) {
                sent.append("\(ordinal(diagram, .message, at)). \(messageText(diagram, item))")
            }
            lines.append("Messages: \(listed(sent))")
        case .message:
            let item = diagram.events[index]
            let shown = item.number == "" ? "" : ", numbered \(item.number)"
            lines.append("Target: message \(ordinal(diagram, .message, index)) of \(total(diagram, .message))\(shown): "
                + messageText(diagram, item))
            lines.append("From: \(participantName(diagram, item.from))")
            lines.append("To: \(participantName(diagram, item.to))")
            inside()
        case .note:
            let item = diagram.events[index]
            lines.append("Target: note \(ordinal(diagram, .note, index)) of \(total(diagram, .note)): "
                + noteText(diagram, item))
            inside()
        case .block:
            let item = diagram.events[index]
            lines.append("Target: block \(ordinal(diagram, .open, index)) of \(total(diagram, .open)): \(blockText(item))")
            // The statements inside, up to the block's end.
            var dividers: [String] = []
            var held: [String] = []
            var depth = 0
            scan: for (at, inner) in diagram.events.enumerated() where at > index {
                switch inner.kind {
                case .open: depth += 1
                case .close:
                    if depth == 0 { break scan }
                    depth -= 1
                case .divider: if depth == 0 { dividers.append(blockText(inner)) }
                case .message: held.append("message \(ordinal(diagram, .message, at))")
                case .note: held.append("note \(ordinal(diagram, .note, at))")
                }
            }
            if !dividers.isEmpty { lines.append("Sections: \(listed(dividers))") }
            lines.append("Holds: \(listed(held))")
            inside()
        default: break
        }
        return lines
    }

    /// What Kit reads from source's diagram about a part: its type, title,
    /// and the part with what it connects to.
    public static func describe(_ source: String, _ part: Part) throws -> [String] {
        if source.size > Mermaid.maxSource {
            throw NotDrawn("the source is larger than \(Mermaid.maxSource / 1024) KiB")
        }
        let parts = Mermaid.read(source)
        if parts.failure != "" { throw NotDrawn(parts.failure) }
        var lines = ["Diagram: \(parts.header)"]
        let known: Bool
        if Mermaid.firstWord(parts.header) == "sequenceDiagram" {
            let diagram = try Sequence.parse(parts.body, title: parts.title)
            known = inSequence(diagram, part)
            if diagram.title != "" { lines.append("Title: \(diagram.title)") }
            if known { lines += sequenceLines(diagram, part) }
        } else {
            let chart = try Flowchart.parse(header: parts.header, lines: parts.body)
            known = inFlowchart(chart, part)
            let title = Flowchart.clean(parts.title)
            if title != "" { lines.append("Title: \(title)") }
            if known { lines += flowchartLines(chart, part) }
        }
        guard known else { throw NotDrawn("the diagram has no such part") }
        return lines
    }

    /// The part, as its description's `Target:` line names it.
    public static func summary(_ source: String, _ part: Part) throws -> String {
        let line = try describe(source, part).first { Go.hasPrefix($0, "Target: ") } ?? ""
        return Go.trimPrefix(line, "Target: ")
    }

    // MARK: The message's text

    /// The source as the model gets it: its lines without carriage returns
    /// or the blank lines around them.
    private static func sourceText(_ source: String) -> String {
        Go.trim(Go.replaceAll(source, "\r\n", "\n"), "\n")
    }

    /// One annotation's block, numbered number. shared is the number of an
    /// earlier block in the message with the same diagram's source, or 0.
    public static func block(number: Int, _ item: DiagramAnnotation, shared: Int) throws -> String {
        // A comment's later lines are indented, so none can pass for a line
        // of the block.
        let comment = Go.split(Go.trimSpace(item.comment), "\n").joined(separator: "\n  ")
        var lines = ["\(open)\(number)\">", "Comment: \(comment)", "[\(fenceName) nonce=\(item.nonce)]"]
        lines += try describe(item.source, item.part)
        if shared > 0 {
            lines.append("Source: as in annotation \(shared)")
        } else {
            lines.append("Source:")
            lines.append(sourceText(item.source))
        }
        lines.append("[/\(fenceName) nonce=\(item.nonce)]")
        lines.append(close)
        return lines.joined(separator: "\n")
    }

    /// The blocks for a message's annotations, numbered from 1, a blank line
    /// apart.
    public static func text(_ items: [DiagramAnnotation]) throws -> String {
        var blocks: [String] = []
        var sources: [[UInt8]] = []
        for (offset, item) in items.enumerated() {
            let source = item.source.bytes
            let shared = (sources.firstIndex(of: source) ?? -1) + 1
            sources.append(source)
            blocks.append(try block(number: offset + 1, item, shared: shared))
        }
        return blocks.joined(separator: "\n\n")
    }

    // MARK: Sent annotations

    /// A sent annotation, as a transcript shows it.
    public struct Sent: Equatable, Identifiable, Sendable {
        public var id: Int { number }
        public let number: Int
        public let comment: String
        /// The diagram's type, as its `Diagram:` line names it.
        public let diagram: String
        /// The part, as its `Target:` line names it.
        public let target: String

        public init(number: Int, comment: String, diagram: String, target: String) {
            self.number = number; self.comment = comment; self.diagram = diagram; self.target = target
        }
    }

    /// Splits a message into its own text and the diagram annotation blocks
    /// appended to it. Text without blocks comes back unchanged.
    public static func split(_ message: String) -> (text: String, annotations: [Sent]) {
        guard message.contains(open) else { return (message, []) }
        var text = "", annotations: [Sent] = []
        var rest = Substring(message)
        while let start = rest.range(of: open) {
            guard let (sent, end) = parse(rest[start.lowerBound...]) else { break }
            text += rest[..<start.lowerBound]
            annotations.append(sent)
            rest = rest[end...]
        }
        text += rest
        return (text.trimmingCharacters(in: .whitespacesAndNewlines), annotations)
    }

    /// The block at the start of text and where it ends: its fence closes
    /// with its own nonce, so nothing inside can end it early.
    private static func parse(_ text: Substring) -> (Sent, Substring.Index)? {
        guard let headEnd = text.range(of: "\">\n"),
              let number = Int(text[text.index(text.startIndex, offsetBy: open.count)..<headEnd.lowerBound]) else {
            return nil
        }
        var rest = text[headEnd.upperBound...]
        guard rest.hasPrefix("Comment: ") else { return nil }
        rest = rest.dropFirst("Comment: ".count)
        let fenceStart = "\n[\(fenceName) nonce="
        guard let fence = rest.range(of: fenceStart) else { return nil }
        let comment = String(rest[..<fence.lowerBound]).replacingOccurrences(of: "\n  ", with: "\n")
        rest = rest[fence.upperBound...]
        guard let nonceEnd = rest.range(of: "]\n") else { return nil }
        let nonce = rest[..<nonceEnd.lowerBound]
        guard !nonce.isEmpty, !nonce.contains("\n") else { return nil }
        let closing = "\n[/\(fenceName) nonce=\(nonce)]\n\(close)"
        let body = rest[nonceEnd.upperBound...]
        guard let end = body.range(of: closing) else { return nil }
        let lines = body[..<end.lowerBound].split(separator: "\n", omittingEmptySubsequences: false)
        func value(_ key: String) -> String {
            lines.first { $0.hasPrefix(key + ": ") }.map { String($0.dropFirst(key.count + 2)) } ?? ""
        }
        return (Sent(number: number, comment: comment, diagram: value("Diagram"), target: value("Target")), end.upperBound)
    }
}
