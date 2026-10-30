import Foundation

// Mermaid sequence diagrams: a reviewed subset parsed, laid out in columns,
// and drawn with box-drawing characters. See `apps/cli/tui/mermaid/sequence.ard`,
// the reference this ports.

enum Sequence {
    static let maxParticipants = 24
    static let maxStatements = 200
    static let maxWidth = 480
    static let maxHeight = 800
    /// Text wider than this is cut short with an ellipsis.
    static let maxText = 64
    /// The cells kept clear between adjacent participant boxes.
    static let boxGap = 3

    struct Participant {
        var id: String
        var label: String
        var actor: Bool
    }

    enum Kind { case message, note, open, divider, close }
    enum Head { case none, filled, open, cross }
    enum Placement { case left, right, over }

    struct Event {
        var kind: Kind
        var from = 0
        var to = 0
        var dashed = false
        var start = Head.none
        var end = Head.none
        var placement = Placement.over
        var keyword = ""
        var text = ""
    }

    struct Diagram {
        var title: String
        var participants: [Participant]
        var events: [Event]
    }

    static func clean(_ value: String) -> String { cut(value, to: maxText) }

    private struct Arrow {
        let token: String
        let dashed: Bool
        let start: Head
        let end: Head
    }

    private static let arrows: [Arrow] = [
        Arrow(token: "<<-->>", dashed: true, start: .filled, end: .filled),
        Arrow(token: "<<->>", dashed: false, start: .filled, end: .filled),
        Arrow(token: "-->>", dashed: true, start: .none, end: .filled),
        Arrow(token: "->>", dashed: false, start: .none, end: .filled),
        Arrow(token: "--x", dashed: true, start: .none, end: .cross),
        Arrow(token: "-x", dashed: false, start: .none, end: .cross),
        Arrow(token: "--)", dashed: true, start: .none, end: .open),
        Arrow(token: "-)", dashed: false, start: .none, end: .open),
        Arrow(token: "-->", dashed: true, start: .none, end: .none),
        Arrow(token: "->", dashed: false, start: .none, end: .none),
    ]

    /// Whether id can name a participant: not empty, and free of the
    /// characters and arrow pieces that separate statement parts.
    static func validName(_ id: String) -> Bool {
        var valid = id != "" && !Go.containsAny(id, ":,;<>")
        for piece in ["->", "--", "-x", "-)"] where Go.contains(id, piece) {
            valid = false
        }
        return valid
    }

    private static func article(_ word: String) -> String {
        Go.hasPrefix(word, "a") || Go.hasPrefix(word, "o") ? "an" : "a"
    }

    private struct Parser {
        var title: String
        var participants: [Participant] = []
        var indexes: [[UInt8]: Int] = [:]
        var events: [Event] = []
        /// Open blocks, innermost last; `rect` blocks are open but not drawn.
        var blocks: [String] = []
        var numbered = false
        var number = 1
        var step = 1

        /// The index of the participant id, declaring it on first use.
        mutating func participant(_ raw: String) throws -> Int {
            let id = Go.trimSpace(raw)
            if id == "" { throw NotDrawn("a participant needs a name") }
            if !validName(id) { throw NotDrawn("`\(id)` isn't a participant name") }
            if let index = indexes[id.bytes] { return index }
            let index = participants.count
            participants.append(Participant(id: id, label: clean(id), actor: false))
            indexes[id.bytes] = index
            return index
        }

        mutating func declare(_ rest: String, actor: Bool) throws {
            let at = Go.index(rest, " as ")
            let id = at >= 0 ? rest.slice(end: at) ?? rest : rest
            let label = at >= 0 ? clean(rest.slice(start: at + 4) ?? "") : clean(id)
            if Go.contains(rest, "@{") { throw NotDrawn("participant metadata isn't supported") }
            if Go.hasSuffix(rest, " as") || label == "" { throw NotDrawn("`\(rest)` needs a label after `as`") }
            let index = try participant(id)
            participants.put(index, Participant(id: Go.trimSpace(id), label: label, actor: actor))
        }

        mutating func note(_ rest: String) throws {
            let colon = Go.index(rest, ":")
            let head = Go.trimSpace(colon >= 0 ? rest.slice(end: colon) ?? rest : rest)
            let text = colon >= 0 ? clean(rest.slice(start: colon + 1) ?? "") : ""
            let lower = Go.toLower(head)
            let placement: Placement
            let names: String
            if Go.hasPrefix(lower, "left of ") {
                placement = .left
                names = head.slice(start: 8) ?? ""
            } else if Go.hasPrefix(lower, "right of ") {
                placement = .right
                names = head.slice(start: 9) ?? ""
            } else if Go.hasPrefix(lower, "over ") {
                placement = .over
                names = head.slice(start: 5) ?? ""
            } else {
                throw NotDrawn("unsupported note placement `\(head)`")
            }
            let targets = placement == .over ? Go.split(names, ",") : [names]
            if targets.count > 2 { throw NotDrawn("a note spans at most two participants") }
            let first = try participant(targets.at(0) ?? "")
            let second = try participant(targets.at(targets.count - 1) ?? "")
            events.append(Event(kind: .note, from: min(first, second), to: max(first, second),
                                placement: placement, text: text))
        }

        mutating func message(_ line: String) throws {
            // The earliest arrow separates sender from receiver.
            var found: Arrow?
            var foundAt = -1
            for arrow in arrows {
                let at = Go.index(line, arrow.token)
                if at > 0 && (foundAt < 0 || at < foundAt) {
                    found = arrow
                    foundAt = at
                }
            }
            guard let arrow = found else { throw NotDrawn("unsupported line `\(line)`") }
            let sender = line.slice(end: foundAt) ?? ""
            let rest = line.slice(start: foundAt + arrow.token.size) ?? ""
            let colon = Go.index(rest, ":")
            var receiver = Go.trimSpace(colon >= 0 ? rest.slice(end: colon) ?? rest : rest)
            if Go.hasPrefix(receiver, "+") || Go.hasPrefix(receiver, "-") {
                receiver = receiver.slice(start: 1) ?? ""
            }
            var text = colon >= 0 ? clean(rest.slice(start: colon + 1) ?? "") : ""
            let from = try participant(sender)
            let to = try participant(receiver)
            if numbered {
                text = clean("\(number). \(text)")
                // Numbers wrap as the reference's Go integers do, rather than trap.
                number = number &+ step
            }
            events.append(Event(kind: .message, from: from, to: to, dashed: arrow.dashed,
                                start: arrow.start, end: arrow.end, text: text))
        }

        mutating func open(_ keyword: String, _ rest: String) {
            blocks.append(keyword)
            events.append(Event(kind: .open, keyword: keyword, text: clean(rest)))
        }

        mutating func divider(_ keyword: String, _ rest: String, within: String) throws {
            guard blocks.last ?? "" == within else {
                throw NotDrawn("`\(keyword)` outside \(article(within)) `\(within)` block")
            }
            events.append(Event(kind: .divider, keyword: keyword, text: clean(rest)))
        }

        mutating func close() throws {
            guard let top = blocks.popLast() else { throw NotDrawn("`end` without an open block") }
            if top != "rect" { events.append(Event(kind: .close)) }
        }

        /// `autonumber`, optionally with a start and step, or `autonumber off`.
        mutating func autonumber(_ rest: String) throws {
            let fields = Go.fields(rest)
            if rest == "off" {
                numbered = false
                return
            }
            if fields.count > 2 { throw NotDrawn("`autonumber` takes at most a start and a step") }
            let start = fields.at(0).map(Go.atoi) ?? 1
            let by = fields.at(1).map(Go.atoi) ?? 1
            guard let start, let by else { throw NotDrawn("`autonumber` takes whole numbers") }
            numbered = true
            number = start
            step = by
        }

        mutating func statement(_ line: String) throws {
            let space = Go.indexAny(line, " \t")
            let original = space >= 0 ? line.slice(end: space) ?? line : line
            // Keywords match in any case, as Mermaid's do.
            let word = Go.toLower(original)
            let rest = space >= 0 ? Go.trimSpace(line.slice(start: space + 1) ?? "") : ""
            switch word {
            case "participant": try declare(rest, actor: false)
            case "actor": try declare(rest, actor: true)
            case "note": try note(rest)
            case "loop", "alt", "opt", "par", "critical", "break": open(word, rest)
            case "rect": blocks.append("rect")
            case "else": try divider(word, rest, within: "alt")
            case "and": try divider(word, rest, within: "par")
            case "option": try divider(word, rest, within: "critical")
            case "end" where rest == "": try close()
            case "end": throw NotDrawn("unexpected text after `end`")
            case "activate", "deactivate": _ = try participant(rest)
            case "autonumber": try autonumber(rest)
            case "box", "create", "destroy", "link", "links", "properties", "details":
                throw NotDrawn("`\(word)` isn't supported")
            default:
                if word == "title" || Go.hasPrefix(word, "title:") {
                    title = clean(Go.trimPrefix(line.slice(start: 5) ?? "", ":"))
                } else {
                    try message(line)
                }
            }
        }
    }

    /// Parses the statements after a `sequenceDiagram` header. title is the
    /// frontmatter title, which a `title` statement replaces.
    static func parse(_ lines: [String], title: String) throws -> Diagram {
        var parser = Parser(title: clean(title))
        var statements = 0
        for raw in lines {
            let line = Go.trimSpace(raw)
            if line != "" && !Go.hasPrefix(line, "%%") {
                statements += 1
                try parser.statement(line)
            }
        }
        if let unclosed = parser.blocks.last { throw NotDrawn("unclosed `\(unclosed)` block") }
        if parser.participants.isEmpty { throw NotDrawn("the diagram has no participants") }
        if parser.participants.count > maxParticipants { throw NotDrawn("more than \(maxParticipants) participants") }
        if statements > maxStatements { throw NotDrawn("more than \(maxStatements) statements") }
        return Diagram(title: parser.title, participants: parser.participants, events: parser.events)
    }

    /// A drawn block frame.
    private struct Frame {
        var x0 = 0
        var x1 = 0
        var top: Int
        var bottom: Int
        var keyword: String
        var condition: String
        /// Rows of `else`, `and`, and `option` dividers, with their keyword and condition.
        var dividers: [Int] = []
        var dividerKeywords: [String] = []
        var dividerConditions: [String] = []
        /// The horizontal extent of the frame's contents, before its padding.
        var min = 0
        var max = 0
        var empty = true
    }

    /// Where an event is drawn.
    private struct Placed {
        /// The event's first row; a message's text row when it has text.
        var row: Int
        /// A message's arrow row.
        var line: Int
        var x0 = 0
        var x1 = 0
        /// The extent an enclosing frame wraps.
        var reach0 = 0
        var reach1 = 0
    }

    private static func frameLabel(_ keyword: String, _ condition: String) -> String {
        condition == "" ? " \(keyword) " : " \(keyword) [\(condition)] "
    }

    private static func boxWidth(_ participant: Participant) -> Int {
        TextWidth.of(participant.label) + 4
    }

    /// Lays the diagram out in columns and rows and draws it.
    static func draw(_ diagram: Diagram) throws -> Canvas {
        let participants = diagram.participants
        let count = participants.count

        // Column centers: each must clear the boxes, texts, and notes between
        // it and the columns to its left.
        var needs: [(Int, Int, Int)] = []
        var index = 0
        while index + 1 < count {
            let width = boxWidth(participants[index])
            let next = boxWidth(participants[index + 1])
            needs.append((index, index + 1, width - width / 2 + next / 2 + boxGap))
            index += 1
        }
        for item in diagram.events {
            let text = TextWidth.of(item.text)
            switch item.kind {
            case .message:
                if item.from != item.to {
                    needs.append((Swift.min(item.from, item.to), Swift.max(item.from, item.to), text + 4))
                } else if item.from + 1 < count {
                    needs.append((item.from, item.from + 1, text + 7))
                }
            case .note:
                let width = text + 4
                switch item.placement {
                case .right:
                    if item.to + 1 < count { needs.append((item.to, item.to + 1, width + 3)) }
                case .left:
                    if item.from > 0 { needs.append((item.from - 1, item.from, width + 3)) }
                case .over:
                    if item.from == item.to {
                        if item.from > 0 { needs.append((item.from - 1, item.from, width / 2 + 2)) }
                        if item.from + 1 < count { needs.append((item.from, item.from + 1, width - width / 2 + 2)) }
                    } else {
                        needs.append((item.from, item.to, width - 5))
                    }
                }
            default: break
            }
        }
        var centers: [Int] = []
        index = 0
        while index < count {
            var center = boxWidth(participants[index]) / 2
            for need in needs where need.1 == index {
                center = Swift.max(center, (centers.at(need.0) ?? 0) + need.2)
            }
            centers.append(center)
            index += 1
        }
        let center = { (participant: Int) -> Int in centers.at(participant) ?? 0 }

        // Rows, and each event's horizontal extent.
        let header = diagram.title == "" ? 0 : 2
        var row = header + 4
        var placed: [Placed] = []
        var frames: [Frame] = []
        // The frames still open, innermost at depth - 1.
        var open: [Int] = []
        var depth = 0
        var minX = 0
        var maxX = 0
        index = 0
        while index < count {
            let width = boxWidth(participants[index])
            maxX = Swift.max(maxX, center(index) - width / 2 + width - 1)
            index += 1
        }
        for item in diagram.events {
            let text = TextWidth.of(item.text)
            var place = Placed(row: row, line: row)
            switch item.kind {
            case .message:
                let a = center(item.from)
                let b = center(item.to)
                if a == b {
                    place.x0 = a
                    place.x1 = text == 0 ? a + 3 : a + 4 + text
                    row += 4
                } else {
                    place.x0 = Swift.min(a, b)
                    place.x1 = Swift.max(a, b)
                    if text > 0 {
                        place.line = row + 1
                        row += 1
                    }
                    row += 2
                }
            case .note:
                let width = text + 4
                switch item.placement {
                case .right:
                    place.x0 = center(item.to) + 2
                    place.x1 = place.x0 + width - 1
                case .left:
                    place.x1 = center(item.from) - 2
                    place.x0 = place.x1 - width + 1
                case .over:
                    if item.from == item.to {
                        place.x0 = center(item.from) - width / 2
                        place.x1 = place.x0 + width - 1
                    } else {
                        place.x0 = center(item.from) - 2
                        place.x1 = Swift.max(center(item.to) + 2, place.x0 + width - 1)
                    }
                }
                row += 4
            case .open:
                if depth < open.count { open[depth] = frames.count } else { open.append(frames.count) }
                depth += 1
                frames.append(Frame(top: row, bottom: row, keyword: item.keyword, condition: item.text))
                row += 1
            case .divider:
                let current = open.at(depth - 1) ?? 0
                frames[current].dividers.append(row)
                frames[current].dividerKeywords.append(item.keyword)
                frames[current].dividerConditions.append(item.text)
                row += 1
            case .close:
                let current = open.at(depth - 1) ?? 0
                depth -= 1
                var frame = frames[current]
                if frame.empty {
                    frame.min = center(0)
                    frame.max = center(count - 1)
                }
                frame.bottom = row
                frame.x0 = frame.min - 2
                frame.x1 = Swift.max(frame.max + 2,
                                     frame.x0 + TextWidth.of(frameLabel(frame.keyword, frame.condition)) + 2)
                for divider in frame.dividers.indices {
                    let label = frameLabel(frame.dividerKeywords[divider], frame.dividerConditions[divider])
                    frame.x1 = Swift.max(frame.x1, frame.x0 + TextWidth.of(label) + 2)
                }
                // Keep the frame's sides off the lifelines.
                var moved = true
                while moved {
                    moved = false
                    for participant in 0..<count {
                        if center(participant) == frame.x0 {
                            frame.x0 -= 1
                            moved = true
                        }
                        if center(participant) == frame.x1 {
                            frame.x1 += 1
                            moved = true
                        }
                    }
                }
                frames[current] = frame
                place.x0 = frame.x0
                place.x1 = frame.x1
                row += 2
            }
            place.reach0 = place.x0
            place.reach1 = place.x1
            if item.kind == .note && item.placement == .left { place.reach1 = center(item.from) }
            if item.kind == .note && item.placement == .right { place.reach0 = center(item.to) }
            if item.kind != .open && item.kind != .divider {
                // The contents widen the innermost open frame.
                if let current = open.at(depth - 1) {
                    if frames[current].empty {
                        frames[current].min = place.reach0
                        frames[current].max = place.reach1
                    } else {
                        frames[current].min = Swift.min(frames[current].min, place.reach0)
                        frames[current].max = Swift.max(frames[current].max, place.reach1)
                    }
                    frames[current].empty = false
                }
                minX = Swift.min(minX, place.x0)
                maxX = Swift.max(maxX, place.x1)
            }
            placed.append(place)
        }
        let height = row
        let shift = 0 - minX
        let width = Swift.max(maxX + shift + 1, TextWidth.of(diagram.title))
        if width > maxWidth { throw NotDrawn("the diagram is wider than \(maxWidth) columns") }
        if height > maxHeight { throw NotDrawn("the diagram is taller than \(maxHeight) rows") }

        var drawing = Canvas(width: width, height: height)
        drawing.kind = .sequence
        if diagram.title != "" {
            drawing.write(0, 0, diagram.title, .title, bold: true)
        }

        // Participant boxes and their lifelines.
        for (index, participant) in participants.enumerated() {
            let box = boxWidth(participant)
            let x = center(index) + shift
            let x0 = x - box / 2
            drawing.outline(x0, header, x0 + box - 1, header + 2, .frame, rounded: participant.actor)
            drawing.figures.append(Figure(kind: .participant(actor: participant.actor), x0: x0, y0: header,
                                          x1: x0 + box - 1, y1: header + 2))
            drawing.vertical(x, header + 2, height - 1, .frame, dashed: false)
            drawing.write(x0 + 2, header + 1, participant.label, .name, bold: true)
        }

        // Block frames, so their borders cross the lifelines.
        for frame in frames {
            let x0 = frame.x0 + shift
            let x1 = frame.x1 + shift
            drawing.outline(x0, frame.top, x1, frame.bottom, .frame, rounded: false)
            drawing.figures.append(Figure(kind: .block, x0: x0, y0: frame.top, x1: x1, y1: frame.bottom))
            drawing.write(x0 + 1, frame.top, " \(frame.keyword) ", .keyword, bold: true)
            if frame.condition != "" {
                drawing.write(x0 + 1 + TextWidth.of(" \(frame.keyword) "), frame.top, "[\(frame.condition)] ",
                              .condition, bold: false)
            }
            for divider in frame.dividers.indices {
                let y = frame.dividers[divider]
                let keyword = frame.dividerKeywords[divider]
                let condition = frame.dividerConditions[divider]
                drawing.horizontal(y, x0, x1, .frame, dashed: true)
                drawing.write(x0 + 1, y, " \(keyword) ", .keyword, bold: true)
                if condition != "" {
                    drawing.write(x0 + 1 + TextWidth.of(" \(keyword) "), y, "[\(condition)] ", .condition, bold: false)
                }
            }
        }

        // Messages and notes.
        for (eventIndex, item) in diagram.events.enumerated() {
            let place = placed[eventIndex]
            switch item.kind {
            case .message:
                drawMessage(&drawing, item, place, from: center(item.from) + shift, to: center(item.to) + shift)
            case .note:
                let x0 = place.x0 + shift
                let x1 = place.x1 + shift
                clear(&drawing, x0, place.row, x1, place.row + 2)
                drawing.outline(x0, place.row, x1, place.row + 2, .note, rounded: false)
                drawing.figures.append(Figure(kind: .note, x0: x0, y0: place.row, x1: x1, y1: place.row + 2))
                let inner = x1 - x0 - 1
                let text = TextWidth.of(item.text)
                drawing.write(x0 + 1 + (inner - text) / 2, place.row + 1, item.text, .note, bold: false)
            default: break
            }
        }
        return drawing
    }

    /// Erases a rectangle so a note covers the lifelines behind it.
    private static func clear(_ drawing: inout Canvas, _ x0: Int, _ y0: Int, _ x1: Int, _ y1: Int) {
        var y = y0
        while y <= y1 {
            drawing.write(x0, y, Go.repeated(" ", x1 - x0 + 1), .note, bold: false)
            y += 1
        }
    }

    private static func headGlyph(_ head: Head, rightward: Bool) -> String {
        switch head {
        case .filled: return rightward ? "►" : "◄"
        case .open: return rightward ? "▷" : "◁"
        case .cross: return "×"
        case .none: return ""
        }
    }

    private static func drawMessage(_ drawing: inout Canvas, _ item: Event, _ place: Placed, from: Int, to: Int) {
        let text = TextWidth.of(item.text)
        if from == to {
            // A message to itself loops out to the right and back.
            let y = place.row
            drawing.horizontal(y, from, from + 3, .arrow, dashed: item.dashed)
            drawing.vertical(from + 3, y, y + 2, .arrow, dashed: item.dashed)
            let back = item.end == .none ? from : from + 1
            drawing.horizontal(y + 2, back, from + 3, .arrow, dashed: item.dashed)
            if item.end != .none {
                drawing.write(from + 1, y + 2, headGlyph(item.end, rightward: false), .arrow, bold: false)
            }
            if text > 0 {
                drawing.write(from + 5, y, item.text, .label, bold: false)
            }
            return
        }
        let rightward = to > from
        let step = rightward ? 1 : -1
        let first = item.start == .none ? from : from + step
        let last = item.end == .none ? to : to - step
        drawing.horizontal(place.line, Swift.min(first, last), Swift.max(first, last), .arrow, dashed: item.dashed)
        if item.start != .none {
            drawing.write(first, place.line, headGlyph(item.start, rightward: !rightward), .arrow, bold: false)
        }
        if item.end != .none {
            drawing.write(last, place.line, headGlyph(item.end, rightward: rightward), .arrow, bold: false)
        }
        if text > 0 {
            let x0 = Swift.min(from, to)
            let inner = Swift.max(from, to) - x0 - 1
            drawing.write(x0 + 1 + (inner - text) / 2, place.row, item.text, .label, bold: false)
        }
    }

    /// Draws the statements after a `sequenceDiagram` header.
    static func render(_ lines: [String], title: String) throws -> Canvas {
        try draw(parse(lines, title: title))
    }
}
