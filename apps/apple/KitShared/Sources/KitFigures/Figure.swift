import Foundation

/// A parsed figure (ADR 0054): what both clients draw, recorded in the
/// shared fixtures. Values are kept as written; the numbers each kind
/// derives from them, such as a bar's length, are computed here as the
/// terminal client's reference computes them.
public struct Figure: Equatable, Sendable {
    public enum Kind: String, Sendable {
        case stat, bars, meter, timeline, steps, decision, delta, spec, tree, compare, score, stack
    }

    /// One row, column, or point of a figure.
    public struct Item: Equatable, Sendable {
        public var label: String
        /// The value as written.
        public var value: String
        public var note = ""
        /// The item to read, which takes the accent.
        public var accent = false
        /// An item that recedes.
        public var muted = false
        /// A bar's length, in thousandths of the track.
        public var scale = 0
        /// A bar's share, such as `33%`, or empty.
        public var share = ""
        /// A stat's sparkline, each point in thousandths of the line's range.
        public var spark: [Int] = []
        /// A meter row's target as written, or empty.
        public var target = ""
        /// A meter past its target or past 100%, which turns warning.
        public var over = false
        /// Where a timeline item or step stands against the bold one.
        public var state: State = .none
        /// A timeline item's or step's indented body, its lines joined.
        public var body = ""
        /// A delta row's value before, as written, or empty.
        public var from = ""
        /// A delta row's sign as shown, `+` or `−`, or empty.
        public var sign = ""
        /// A delta row's computed change, or empty.
        public var change = ""
        /// Whether a delta row's change is for the better or the worse.
        public var trend: Trend = .none
        /// A tree node's depth, from 1 at the top level, or 0.
        public var level = 0
        /// A compare row's cells as written, one per option.
        public var cells: [String] = []
        /// A score's filled half dots, and its dots, or 0.
        public var filled = 0
        public var slots = 0
        /// A stack row's segments, each in thousandths of the row's total.
        public var parts: [Int] = []
    }

    /// Whether a delta row's change is for the better or the worse, or
    /// `none` when the figure doesn't say which way is better or nothing
    /// changed.
    public enum Trend: String, Sendable { case none = "", better, worse }

    /// Where a timeline item or step stands against the bold one: before
    /// it, at it, after it, or `none` when nothing is bold.
    public enum State: String, Sendable { case none = "", done, now, next }

    public let kind: Kind
    public let title: String
    public let items: [Item]
    /// A decision's status, or empty.
    public var status = ""
    /// A decision's date, or empty.
    public var date = ""
    /// A decision's closing paragraph, or empty.
    public var note = ""
    /// Column headings: a delta's from and to, a compare's options, or a
    /// stack's segment names.
    public var headings: [String] = []
    /// The compare option that is the pick, or -1.
    public var pick = -1

    /// The figures drawn. Any other fence is ordinary code.
    public static let kinds = [
        "stat", "bars", "meter", "timeline", "steps", "decision", "delta", "spec", "tree", "compare", "score", "stack",
    ]

    /// The widest label, cell, or heading drawn, in characters.
    public static let maxLabel = 32

    /// text cut to limit characters, ending in `…` when it is longer, as the
    /// terminal cuts labels to cells.
    public static func cut(_ text: String, _ limit: Int = maxLabel) -> String {
        text.count > limit ? String(text.prefix(limit - 1)) + "…" : text
    }

    /// Whether a fenced code block's info string names a figure.
    public static func isFigure(_ info: String) -> Bool {
        kinds.contains(Grammar.name(info))
    }

    /// The figure a fence describes, or why it can't be drawn.
    public static func parse(info: String, body: String) -> Result<Figure, NotDrawn> {
        Grammar.parseInfo(info).flatMap { info in
            switch info.name {
            case "stat": Stat.parse(info, body)
            case "bars": Bars.parse(info, body)
            case "meter": Meter.parse(info, body)
            case "timeline": Timeline.parse(info, body)
            case "steps": Steps.parse(info, body)
            case "decision": Decision.parse(info, body)
            case "delta": Delta.parse(info, body)
            case "spec": Spec.parse(info, body)
            case "tree": Tree.parse(info, body)
            case "compare": Compare.parse(info, body)
            case "score": Score.parse(info, body)
            case "stack": Stack.parse(info, body)
            default: .failure(NotDrawn("`\(info.name)` figures aren't drawn yet"))
            }
        }
    }

    /// The figure as fixture text, one line per field and item, as the
    /// reference records it.
    public var record: String {
        func quoted(_ text: String) -> String {
            "\"" + text.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"") + "\""
        }
        var lines = ["figure \(kind.rawValue)", "title \(quoted(title))"]
        if !status.isEmpty { lines.append("status \(status)") }
        if !date.isEmpty { lines.append("date \(quoted(date))") }
        if !note.isEmpty { lines.append("note \(quoted(note))") }
        if !headings.isEmpty { lines.append("headings \(headings.map(quoted).joined(separator: ","))") }
        if pick >= 0 { lines.append("pick \(pick)") }
        for item in items {
            let spark = item.spark.map(String.init).joined(separator: ",")
            var extra = ""
            if !item.target.isEmpty { extra += " target=\(quoted(item.target))" }
            if item.over { extra += " over=true" }
            if item.state != .none { extra += " state=\(item.state.rawValue)" }
            if !item.body.isEmpty { extra += " body=\(quoted(item.body))" }
            if !item.from.isEmpty { extra += " from=\(quoted(item.from))" }
            if !item.sign.isEmpty { extra += " sign=\(quoted(item.sign))" }
            if !item.change.isEmpty { extra += " change=\(quoted(item.change))" }
            if item.trend != .none { extra += " trend=\(item.trend.rawValue)" }
            if item.level > 0 { extra += " level=\(item.level)" }
            if !item.cells.isEmpty { extra += " cells=\(item.cells.map(quoted).joined(separator: ","))" }
            if item.slots > 0 { extra += " filled=\(item.filled) slots=\(item.slots)" }
            if !item.parts.isEmpty { extra += " parts=\(item.parts.map(String.init).joined(separator: ","))" }
            lines.append("item label=\(quoted(item.label)) value=\(quoted(item.value)) note=\(quoted(item.note)) accent=\(item.accent) muted=\(item.muted) scale=\(item.scale) share=\(quoted(item.share)) spark=\(spark)\(extra)")
        }
        return lines.joined(separator: "\n") + "\n"
    }
}

/// `bars`: one labelled bar per row, scaled to the largest value or `max`,
/// with each row's share of the first row or of the total when `percent` asks.
enum Bars {
    static let maxRows = 20
    /// The largest share shown; a larger one shows as `>9999%`.
    static let maxShare = 9999

    /// value's share of whole, as a whole percent.
    static func share(_ value: Double, _ whole: Double) -> String {
        let percent = value / whole * 100
        if percent == 0 { return "0%" }
        if percent < 1 { return "<1%" }
        if percent >= Double(maxShare) + 0.5 { return ">\(maxShare)%" }
        return "\(Int(percent.rounded()))%"
    }

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title", "max", "percent"]) { return .failure(failure) }
        let items: [Grammar.Item]
        switch Grammar.items(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let parsed): items = parsed
        }
        if items.filter(\.bold).count > 1 { return .failure(NotDrawn("only one item can be bold")) }
        var rows: [(item: Figure.Item, number: Double)] = []
        for each in items {
            guard let (label, value) = Grammar.labelValue(each.text) else {
                return .failure(NotDrawn("a bars row is `label: value`, got `\(Grammar.clip(each.text))`"))
            }
            let number: Double
            switch Grammar.number(value) {
            case .failure(let failure): return .failure(failure)
            case .success(let parsed): number = parsed
            }
            if !each.details.isEmpty { return .failure(NotDrawn("a bars row takes no indented lines")) }
            if number < 0 { return .failure(NotDrawn("bars can't be negative, got `\(Grammar.clip(value))`")) }
            rows.append((Figure.Item(label: label, value: value, note: each.note, accent: each.bold, muted: each.italic), number))
        }
        let most: Double
        let written = info.attribute("max")
        if written.isEmpty {
            most = rows.map(\.number).reduce(0) { max($0, $1) }
        } else {
            switch Grammar.number(written) {
            case .failure(let failure): return .failure(failure)
            case .success(let value):
                guard value > 0 else { return .failure(NotDrawn("`max` must be above zero")) }
                most = value
            }
        }
        let percent = info.attribute("percent")
        let total = rows.reduce(0) { $0 + $1.number }
        let first = rows.first?.number ?? 0
        let whole: Double
        switch percent {
        case "": whole = 0
        case "first":
            guard first > 0 else { return .failure(NotDrawn("`percent=\"first\"` needs a first row above zero")) }
            whole = first
        case "total":
            guard total > 0 else { return .failure(NotDrawn("`percent=\"total\"` needs a total above zero")) }
            whole = total
        default: return .failure(NotDrawn("`percent` is `first` or `total`"))
        }
        let made = rows.enumerated().map { index, row in
            var item = row.item
            item.scale = Grammar.permille(row.number, most)
            if whole > 0 && !(percent == "first" && index == 0) { item.share = share(row.number, whole) }
            return item
        }
        if rows.isEmpty { return .failure(NotDrawn("the figure has no rows")) }
        if rows.count > maxRows { return .failure(NotDrawn("a bars figure has at most \(maxRows) rows")) }
        return .success(Figure(kind: .bars, title: info.attribute("title"), items: made))
    }
}

/// `stat`: two to four large numbers side by side, each a value then a
/// label, with an optional note and a sparkline from a line of numbers under it.
enum Stat {
    static let maxItems = 4
    static let maxValue = 12
    static let maxPoints = 60

    private static func spark(_ details: [String]) -> Result<[Int], NotDrawn> {
        switch details.count {
        case 0: return .success([])
        case 1:
            return Grammar.numbers(details[0], limit: maxPoints).flatMap { values in
                guard values.count >= 2 else { return .failure(NotDrawn("a sparkline needs at least 2 values")) }
                let low = values.min() ?? 0, high = values.max() ?? 0
                return .success(values.map { high > low ? Grammar.permille($0 - low, high - low) : 500 })
            }
        default: return .failure(NotDrawn("a stat item takes one line of numbers under it"))
        }
    }

    private static func stat(_ each: Grammar.Item) -> Result<Figure.Item, NotDrawn> {
        let text = each.text
        let afterOpening = Grammar.slice(text, 2)
        let close = Grammar.index(afterOpening, "**")
        let leading = !each.bold && Grammar.hasPrefix(text, "**") && (close ?? 0) > 0
        let first = Grammar.fields(text).first ?? ""
        let value: String, label: String
        if leading, let close {
            value = Grammar.slice(afterOpening, 0, close)
            label = Grammar.trimSpace(Grammar.slice(afterOpening, close + 2))
        } else {
            value = first
            label = Grammar.trimSpace(String(text.utf8.dropFirst(first.utf8.count)) ?? "")
        }
        return spark(each.details).flatMap { points in
            if value.isEmpty || label.isEmpty {
                return .failure(NotDrawn("a stat item is a value then a label, got `\(Grammar.clip(text))`"))
            }
            if value.unicodeScalars.count > maxValue {
                return .failure(NotDrawn("a stat value is at most \(maxValue) characters, got `\(Grammar.clip(value))`"))
            }
            return .success(Figure.Item(label: label, value: value, note: each.note, accent: each.bold || leading,
                                        muted: each.italic, spark: points))
        }
    }

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title"]) { return .failure(failure) }
        let items: [Grammar.Item]
        switch Grammar.items(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let parsed): items = parsed
        }
        var made: [Figure.Item] = []
        for each in items {
            switch stat(each) {
            case .failure(let failure): return .failure(failure)
            case .success(let item): made.append(item)
            }
        }
        if items.isEmpty { return .failure(NotDrawn("the figure has no items")) }
        if items.count > maxItems { return .failure(NotDrawn("a stat figure has at most \(maxItems) items")) }
        if made.filter(\.accent).count > 1 { return .failure(NotDrawn("only one item can be bold")) }
        return .success(Figure(kind: .stat, title: info.attribute("title"), items: made))
    }
}
