import Foundation

// The grammar every figure reads (ADR 0054, docs/figures.md), ported from
// the terminal client's reference in `apps/cli/tui/figure/grammar.ard`. It
// accepts and rejects exactly what the reference does, with the same
// reasons, as the shared fixtures in `testdata/figures` check.

/// Why a figure isn't drawn.
public struct NotDrawn: Error, Equatable, Sendable {
    public let reason: String
    public init(_ reason: String) { self.reason = reason }
}

enum Grammar {
    /// The largest body drawn.
    static let maxBody = 8192
    /// The longest info string read, in bytes.
    static let maxInfo = 1024
    /// The most attributes an info string has.
    static let maxAttributes = 8
    /// The largest magnitude a number may have.
    static let maxNumber = 1_000_000_000_000_000.0

    struct Attribute: Equatable {
        let key: String
        let value: String
    }

    /// A fence's info string, split into its name and attributes.
    struct Info: Equatable {
        let name: String
        let attributes: [Attribute]

        /// The value of attribute key, or empty.
        func attribute(_ key: String) -> String {
            attributes.last { $0.key == key }?.value ?? ""
        }
    }

    /// One list item of a body: its text without its marker, emphasis, or note.
    struct Item: Equatable {
        var text: String
        var note: String
        var bold: Bool
        var italic: Bool
        var details: [String] = []
        /// The number an ordered item is written with, or -1 for a `- ` item.
        var ordinal = -1
    }

    // MARK: Go string semantics

    static func isSpace(_ scalar: Unicode.Scalar) -> Bool { scalar.properties.isWhitespace }

    /// Go's `strings.TrimSpace`.
    static func trimSpace(_ value: String) -> String {
        let scalars = value.unicodeScalars
        guard let first = scalars.firstIndex(where: { !isSpace($0) }),
              let last = scalars.lastIndex(where: { !isSpace($0) }) else { return "" }
        return String(scalars[first...last])
    }

    /// Go's `strings.Fields`.
    static func fields(_ value: String) -> [String] {
        value.unicodeScalars.split(whereSeparator: isSpace).map { String($0) }
    }

    static func trimLeft(_ value: String, _ set: String) -> String {
        String(value.unicodeScalars.drop { set.unicodeScalars.contains($0) })
    }

    static func trimRight(_ value: String, _ set: String) -> String {
        var scalars = Array(value.unicodeScalars)
        while let last = scalars.last, set.unicodeScalars.contains(last) { scalars.removeLast() }
        return String(String.UnicodeScalarView(scalars))
    }

    // MARK: Bytes

    // Structural matching works on UTF-8 bytes, as Go's `strings` package
    // does in the reference, so a combining mark after a space, `|`, or `*`
    // doesn't fuse with it the way it would in a Swift `Character`.

    private static func offset(_ bytes: [UInt8], _ target: [UInt8], last: Bool) -> Int? {
        guard !target.isEmpty, bytes.count >= target.count else { return nil }
        let starts = Array(0...(bytes.count - target.count))
        for start in last ? starts.reversed() : starts where bytes[start..<start + target.count].elementsEqual(target) {
            return start
        }
        return nil
    }

    /// The byte offset of separator's first occurrence in text.
    static func index(_ text: String, _ separator: String) -> Int? {
        offset(Array(text.utf8), Array(separator.utf8), last: false)
    }

    /// The byte offset of separator's last occurrence in text.
    static func lastIndex(_ text: String, _ separator: String) -> Int? {
        offset(Array(text.utf8), Array(separator.utf8), last: true)
    }

    /// text's bytes from start up to end, or to its end.
    static func slice(_ text: String, _ start: Int, _ end: Int? = nil) -> String {
        let bytes = Array(text.utf8)
        let lower = min(max(start, 0), bytes.count)
        let upper = max(lower, min(end ?? bytes.count, bytes.count))
        return String(decoding: bytes[lower..<upper], as: UTF8.self)
    }

    static func hasPrefix(_ text: String, _ prefix: String) -> Bool { text.utf8.starts(with: prefix.utf8) }

    static func hasSuffix(_ text: String, _ suffix: String) -> Bool {
        text.utf8.reversed().starts(with: suffix.utf8.reversed())
    }

    /// text split at each occurrence of separator, as Go's `strings.Split`.
    static func split(_ text: String, _ separator: String) -> [String] {
        let bytes = Array(text.utf8), target = Array(separator.utf8)
        var parts: [String] = []
        var start = 0, at = 0
        while !target.isEmpty && at + target.count <= bytes.count {
            if bytes[at..<at + target.count].elementsEqual(target) {
                parts.append(String(decoding: bytes[start..<at], as: UTF8.self))
                at += target.count
                start = at
            } else {
                at += 1
            }
        }
        parts.append(String(decoding: bytes[start...], as: UTF8.self))
        return parts
    }

    // MARK: Info strings

    /// The first word of an info string.
    static func name(_ info: String) -> String {
        let trimmed = trimSpace(info)
        guard let space = Array(trimmed.utf8).firstIndex(where: { $0 == 0x20 || $0 == 0x09 }) else { return trimmed }
        return slice(trimmed, 0, space)
    }

    private static func isKeyByte(_ byte: UInt8, first: Bool) -> Bool {
        let letter = (0x61...0x7A).contains(byte)
        return first ? letter : letter || (0x30...0x39).contains(byte) || byte == 0x2D
    }

    /// An info string's name and attributes, each key at most once.
    static func parseInfo(_ info: String) -> Result<Info, NotDrawn> {
        let trimmed = trimSpace(info)
        guard trimmed.utf8.count <= maxInfo else {
            return .failure(NotDrawn("the fence's info string is over \(maxInfo) bytes"))
        }
        let figure = name(trimmed)
        var rest = trimLeft(slice(trimmed, figure.utf8.count), " \t")
        var attributes: [Attribute] = []
        var repeated = ""
        while !rest.isEmpty {
            let bytes = Array(rest.utf8)
            var length = 0
            while length < bytes.count && isKeyByte(bytes[length], first: length == 0) { length += 1 }
            let key = slice(rest, 0, length)
            let after = slice(rest, length)
            guard !key.isEmpty, hasPrefix(after, "=\"") else {
                return .failure(NotDrawn("the attributes aren't `key=\"value\"` pairs near `\(clip(rest))`"))
            }
            let quoted = slice(after, 2)
            guard let close = index(quoted, "\"") else {
                return .failure(NotDrawn("the value of `\(key)` isn't closed"))
            }
            let next = slice(quoted, close + 1)
            if !next.isEmpty && !hasPrefix(next, " ") && !hasPrefix(next, "\t") {
                return .failure(NotDrawn("the attributes aren't `key=\"value\"` pairs near `\(clip(next))`"))
            }
            if repeated.isEmpty && attributes.contains(where: { $0.key == key }) { repeated = key }
            attributes.append(Attribute(key: key, value: clean(slice(quoted, 0, close))))
            rest = trimLeft(next, " \t")
        }
        if attributes.count > maxAttributes { return .failure(NotDrawn("a figure has at most \(maxAttributes) attributes")) }
        guard repeated.isEmpty else { return .failure(NotDrawn("`\(repeated)` is set more than once")) }
        return .success(Info(name: figure, attributes: attributes))
    }

    /// Rejects an attribute that isn't one of allowed.
    static func checkAttributes(_ info: Info, _ allowed: [String]) -> NotDrawn? {
        guard let unknown = info.attributes.first(where: { !allowed.contains($0.key) }) else { return nil }
        return NotDrawn("`\(info.name)` has no `\(unknown.key)` attribute")
    }

    // MARK: Text

    /// Text shown in an error: its first 24 bytes, less a character cut in two.
    static func clip(_ text: String) -> String {
        let bytes = Array(text.utf8)
        guard bytes.count > 24 else { return text }
        var kept = Array(bytes.prefix(24))
        while !kept.isEmpty, String(validating: kept, as: UTF8.self) == nil { kept.removeLast() }
        return (String(validating: kept, as: UTF8.self) ?? "") + "…"
    }

    /// text without control characters; a tab is a space.
    static func clean(_ text: String) -> String {
        var scalars = String.UnicodeScalarView()
        for scalar in text.unicodeScalars {
            if scalar == "\t" { scalars.append(" "); continue }
            let value = scalar.value
            if value <= 0x1F || (0x7F...0x9F).contains(value) { continue }
            scalars.append(scalar)
        }
        return String(scalars)
    }

    /// The leading spaces of line, where a tab counts as two.
    private static func indent(_ line: String) -> Int {
        var count = 0
        for byte in line.utf8 {
            if byte == 0x20 { count += 1 } else if byte == 0x09 { count += 2 } else { break }
        }
        return count
    }

    /// An item's text split into its body and the note after ` — ` or ` -- `.
    private static func splitNote(_ text: String) -> (String, String) {
        let found = index(text, " — ").map { ($0, 5) } ?? index(text, " -- ").map { ($0, 4) }
        guard let (at, width) = found else { return (trimSpace(text), "") }
        return (trimSpace(slice(text, 0, at)), trimSpace(slice(text, at + width)))
    }

    /// Whether text is wrapped whole in marker, with something inside.
    private static func wrapped(_ text: String, _ marker: String) -> Bool {
        text.utf8.count > marker.utf8.count * 2 && hasPrefix(text, marker) && hasSuffix(text, marker)
    }

    private static func parseItem(_ text: String) -> Item {
        let (body, note) = splitNote(clean(text))
        let bold = wrapped(body, "**")
        let italic = !bold && wrapped(body, "*") && !hasPrefix(body, "**")
        let size = body.utf8.count
        let inner = bold ? slice(body, 2, size - 2) : italic ? slice(body, 1, size - 1) : body
        return Item(text: trimSpace(inner), note: note, bold: bold, italic: italic)
    }

    /// One line read as an item is: its emphasis and note split off.
    static func line(_ text: String) -> Item { parseItem(text) }

    /// text with its emphasis split off but no note, as a table cell is read.
    static func emphasis(_ text: String) -> Item {
        let body = trimSpace(clean(text))
        let bold = wrapped(body, "**")
        let italic = !bold && wrapped(body, "*") && !hasPrefix(body, "**")
        let size = body.utf8.count
        let inner = bold ? slice(body, 2, size - 2) : italic ? slice(body, 1, size - 1) : body
        return Item(text: trimSpace(inner), note: "", bold: bold, italic: italic)
    }

    // `.` matches line separators such as U+2028, as it does in Go.
    private static let orderedPattern = try! NSRegularExpression(
        pattern: "^([0-9]{1,9})\\. (.*)\\z", options: [.dotMatchesLineSeparators])

    /// A trimmed line's list marker and text: `- text` or `N. text`.
    private static func marked(_ trimmed: String) -> (ordinal: Int, text: String)? {
        if hasPrefix(trimmed, "- ") { return (-1, slice(trimmed, 2)) }
        guard let match = orderedPattern.firstMatch(in: trimmed, range: NSRange(trimmed.startIndex..., in: trimmed)),
              let number = Range(match.range(at: 1), in: trimmed),
              let text = Range(match.range(at: 2), in: trimmed) else { return nil }
        return (Int(trimmed[number]) ?? 0, String(trimmed[text]))
    }

    /// Whether a trimmed line starts an ordered item, as in `1. text`.
    static func isOrdered(_ trimmed: String) -> Bool { (marked(trimmed)?.ordinal ?? -1) >= 0 }

    /// A body's `- ` or `N. ` list items, each with the indented lines under
    /// it. Blank lines are skipped; any other line is an error.
    static func items(_ body: String) -> Result<[Item], NotDrawn> {
        list(body, closing: false).map(\.items)
    }

    /// A body's list items and the one paragraph after them, its lines
    /// joined, or empty.
    static func itemsAndNote(_ body: String) -> Result<(items: [Item], note: String), NotDrawn> {
        list(body, closing: true)
    }

    private static func list(_ body: String, closing: Bool) -> Result<(items: [Item], note: String), NotDrawn> {
        guard body.utf8.count <= maxBody else { return .failure(NotDrawn("the figure is larger than \(maxBody / 1024) KiB")) }
        var result: [Item] = []
        var current: Item?
        var note: [String] = []
        // Whether the line before was blank, once the closing paragraph started.
        var afterBlank = false
        // Split on line feeds alone: Swift reads `\r\n` as one character.
        for raw in body.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n") {
            let line = trimRight(raw, " \t\r")
            let trimmed = trimSpace(line)
            let marker = indent(line) < 2 ? marked(trimmed) : nil
            let inNote = !note.isEmpty
            if trimmed.isEmpty {
                afterBlank = inNote
            } else if inNote && afterBlank {
                return .failure(NotDrawn("the figure takes one paragraph after its list"))
            } else if inNote && marker != nil {
                return .failure(NotDrawn("the paragraph comes after every item, got `\(clip(trimmed))`"))
            } else if inNote {
                note.append(clean(trimmed))
            } else if let marker {
                if let item = current { result.append(item) }
                current = parseItem(marker.text)
                current?.ordinal = marker.ordinal
            } else if indent(line) >= 2, current != nil {
                current?.details.append(clean(trimmed))
            } else if closing, current != nil {
                note.append(clean(trimmed))
            } else {
                return .failure(NotDrawn("expected a `- ` or `1. ` list item, got `\(clip(trimmed))`"))
            }
        }
        if let item = current { result.append(item) }
        return .success((result, note.joined(separator: " ")))
    }

    /// Rejects more than one bold item.
    static func checkOneBold(_ items: [Item]) -> NotDrawn? {
        items.filter(\.bold).count > 1 ? NotDrawn("only one item can be bold") : nil
    }

    /// text split at its first `: ` into a label and a value.
    static func labelValue(_ text: String) -> (String, String)? {
        guard let at = index(text, ": "), at > 0 else { return nil }
        return (trimSpace(slice(text, 0, at)), trimSpace(slice(text, at + 2)))
    }

    // MARK: Numbers

    private static let numberPattern = try! NSRegularExpression(
        pattern: "^([+-]?)\\$?([0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)(\\.[0-9]+)? ?(?:[A-Za-z]{1,4}|%)?\\z")

    /// A number as written: an optional sign and `$`, digits with optional
    /// `,` groups and `.` fraction, and an optional unit such as `%`, `ms`, or `kb`.
    static func number(_ text: String) -> Result<Double, NotDrawn> {
        let whole = NSRange(text.startIndex..., in: text)
        guard let match = numberPattern.firstMatch(in: text, range: whole) else {
            return .failure(NotDrawn("`\(clip(text))` isn't a number"))
        }
        func group(_ index: Int) -> String {
            guard let range = Range(match.range(at: index), in: text) else { return "" }
            return String(text[range])
        }
        let digits = group(1) + group(2).replacingOccurrences(of: ",", with: "") + group(3)
        guard let value = Double(digits) else { return .failure(NotDrawn("`\(clip(text))` isn't a number")) }
        guard abs(value) <= maxNumber else { return .failure(NotDrawn("`\(clip(text))` is too large")) }
        return .success(value)
    }

    /// The most decimals a computed number is written with.
    static let maxDecimals = 6

    /// How a number is written, so a number computed from it is written alike.
    struct Shape: Equatable {
        var dollar = false
        /// Whether its digits are grouped with `,`.
        var grouped = false
        var decimals = 0
        /// Its unit, with the space before it if there is one.
        var unit = ""
    }

    private static let shapePattern = try! NSRegularExpression(
        pattern: "^[+-]?(\\$?)([0-9,]+)(?:\\.([0-9]+))?( ?(?:[A-Za-z]{1,4}|%)?)\\z")

    /// The shape of a number as written; text is one `number` reads.
    static func shape(_ text: String) -> Shape {
        guard let match = shapePattern.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) else { return Shape() }
        func group(_ index: Int) -> String {
            guard let range = Range(match.range(at: index), in: text) else { return "" }
            return String(text[range])
        }
        return Shape(dollar: group(1) == "$", grouped: group(2).contains(","), decimals: group(3).utf8.count, unit: group(4))
    }

    /// The shape numbers written in each of shapes share: the most
    /// decimals, grouped if any is, and the first one's `$` and unit.
    static func common(_ shapes: [Shape]) -> Shape {
        var result = shapes.first ?? Shape()
        for each in shapes {
            result.decimals = max(result.decimals, each.decimals)
            if each.grouped { result.grouped = true }
        }
        return result
    }

    /// Whether numbers of shapes a and b are in the same unit, `$` included.
    static func sameUnit(_ a: Shape, _ b: Shape) -> Bool {
        a.dollar == b.dollar && trimSpace(a.unit) == trimSpace(b.unit)
    }

    /// value written in shape, with at most maxDecimals. When signed, a
    /// value that isn't zero as written takes `+` or `−`.
    static func written(_ value: Double, _ shape: Shape, signed: Bool) -> String {
        let digits = String(format: "%.\(min(shape.decimals, maxDecimals))f", abs(value))
        let parts = digits.split(separator: ".", maxSplits: 1, omittingEmptySubsequences: false)
        let whole = String(parts[0])
        let fraction = parts.count > 1 ? "." + parts[1] : ""
        let zero = digits.allSatisfy { $0 == "0" || $0 == "." }
        let sign = !signed || zero ? "" : value > 0 ? "+" : "−"
        var grouped = whole
        if shape.grouped {
            var groups: [Substring] = []
            var end = whole.endIndex
            while end > whole.startIndex {
                let start = whole.index(end, offsetBy: -3, limitedBy: whole.startIndex) ?? whole.startIndex
                groups.insert(whole[start..<end], at: 0)
                end = start
            }
            grouped = groups.joined(separator: ",")
        }
        return sign + (shape.dollar ? "$" : "") + grouped + fraction + shape.unit
    }

    /// A list of numbers separated by spaces, where `x*n` repeats x n times.
    static func numbers(_ text: String, limit: Int) -> Result<[Double], NotDrawn> {
        var result: [Double] = []
        for token in fields(text) {
            let star = lastIndex(token, "*")
            let written: String, count: Int
            if let star, star > 0 {
                written = slice(token, 0, star)
                count = Int(slice(token, star + 1)) ?? -1
            } else {
                written = token
                count = 1
            }
            guard (1...200).contains(count) else {
                return .failure(NotDrawn("`\(clip(token))` isn't a run of 1 to 200 values"))
            }
            switch number(written) {
            case .failure(let notDrawn): return .failure(notDrawn)
            case .success(let value):
                var index = 0
                while index < count && result.count <= limit {
                    result.append(value)
                    index += 1
                }
            }
        }
        guard result.count <= limit else { return .failure(NotDrawn("there are more than \(limit) values")) }
        return .success(result)
    }

    /// value as a share of whole, in thousandths from 0 to 1000.
    static func permille(_ value: Double, _ whole: Double) -> Int {
        guard whole > 0 else { return 0 }
        let share = (value / whole * 1000).rounded(.down)
        return share > 1000 ? 1000 : share < 0 ? 0 : Int(share)
    }
}
