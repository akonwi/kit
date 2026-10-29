import Foundation

/// `delta`: what changed, either signed values with an optional total or
/// values before and after, with an optional computed change.
enum Delta {
    static let maxRows = 20
    /// The largest percent change shown; a larger one shows as `>9999%`.
    static let maxPercent = 9999

    /// A row's value before and after, split at ` → ` or ` -> `.
    private static func change(_ text: String) -> (String, String)? {
        let found = Grammar.index(text, " → ").map { ($0, 5) } ?? Grammar.index(text, " -> ").map { ($0, 4) }
        guard let (at, width) = found, at > 0 else { return nil }
        return (Grammar.trimSpace(Grammar.slice(text, 0, at)), Grammar.trimSpace(Grammar.slice(text, at + width)))
    }

    private static func trend(_ difference: Double, _ better: String) -> Figure.Trend {
        if better.isEmpty || difference == 0 { return .none }
        return (difference > 0) == (better == "up") ? .better : .worse
    }

    /// The change from before to after as a whole percent of before, `–`
    /// when before is zero.
    static func percent(_ before: Double, _ after: Double) -> String {
        guard before != 0 else { return "–" }
        let share = (after - before) / abs(before) * 100
        let rounded = share.rounded()
        let sign = rounded > 0 ? "+" : rounded < 0 ? "−" : ""
        if abs(share) >= Double(maxPercent) + 0.5 { return "\(sign)>\(maxPercent)%" }
        return "\(sign)\(Int(abs(rounded)))%"
    }

    private static func choice(_ info: Grammar.Info, _ key: String, _ allowed: [String]) -> NotDrawn? {
        let value = info.attribute(key)
        guard !value.isEmpty, !allowed.contains(value) else { return nil }
        return NotDrawn("`\(key)` is `\(allowed.joined(separator: "` or `"))`")
    }

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title", "from", "to", "change", "better"]) { return .failure(failure) }
        if let failure = choice(info, "change", ["percent", "absolute"]) { return .failure(failure) }
        if let failure = choice(info, "better", ["up", "down"]) { return .failure(failure) }
        let better = info.attribute("better")
        let computed = info.attribute("change")
        let items: [Grammar.Item]
        switch Grammar.items(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let parsed): items = parsed
        }
        var made: [Figure.Item] = []
        var paired = 0
        for each in items {
            guard let (label, written) = Grammar.labelValue(each.text) else {
                return .failure(NotDrawn("a delta row is `label: value` or `label: from → to`, got `\(Grammar.clip(each.text))`"))
            }
            var item = Figure.Item(label: label, value: written, note: each.note, accent: each.bold, muted: each.italic)
            if !each.details.isEmpty { return .failure(NotDrawn("a delta row takes no indented lines")) }
            if let (before, after) = change(written) {
                let from: Double, to: Double
                switch Grammar.number(before).flatMap({ from in Grammar.number(after).map { (from, $0) } }) {
                case .failure(let failure): return .failure(failure)
                case .success(let pair): (from, to) = pair
                }
                guard Grammar.sameUnit(Grammar.shape(before), Grammar.shape(after)) else {
                    return .failure(NotDrawn("a delta row's values before and after are in the same unit, got `\(Grammar.clip(written))`"))
                }
                item.from = before
                item.value = after
                item.trend = trend(to - from, better)
                switch computed {
                case "percent": item.change = percent(from, to)
                case "absolute":
                    item.change = Grammar.written(to - from, Grammar.common([Grammar.shape(after), Grammar.shape(before)]), signed: true)
                default: break
                }
                paired += 1
            } else {
                let value: Double
                switch Grammar.number(written) {
                case .failure(let failure): return .failure(failure)
                case .success(let parsed): value = parsed
                }
                let signed = Grammar.hasPrefix(written, "+") || Grammar.hasPrefix(written, "-")
                if signed { item.value = Grammar.trimSpace(Grammar.slice(written, 1)) }
                // A signed zero changed nothing, so it has no sign or trend.
                if signed && value != 0 {
                    let negative = Grammar.hasPrefix(written, "-")
                    item.sign = value < 0 || negative ? "−" : "+"
                    item.trend = trend(negative ? -1 : 1, better)
                }
            }
            made.append(item)
        }
        if made.isEmpty { return .failure(NotDrawn("the figure has no rows")) }
        if made.count > maxRows { return .failure(NotDrawn("a delta figure has at most \(maxRows) rows")) }
        if paired > 0 && paired < made.count {
            return .failure(NotDrawn("a delta's rows are all `label: value` or all `label: from → to`"))
        }
        if made.dropLast().contains(where: \.accent) { return .failure(NotDrawn("only the last row can be bold, as the total")) }
        if paired == 0, let key = ["from", "to", "change"].first(where: { !info.attribute($0).isEmpty }) {
            return .failure(NotDrawn("`\(key)` needs rows of `label: from → to`"))
        }
        var figure = Figure(kind: .delta, title: info.attribute("title"), items: made)
        let from = info.attribute("from"), to = info.attribute("to")
        if !from.isEmpty || !to.isEmpty { figure.headings = [from, to] }
        return .success(figure)
    }
}

/// `score`: rows rated out of the same whole, each drawn as dots with the
/// value as written.
enum Score {
    static let maxRows = 20
    static let maxSlots = 10

    private static let pattern = try! NSRegularExpression(pattern: "^([0-9]{1,2})(\\.[05])? ?/ ?([0-9]{1,2})\\z")

    /// A score's halves and its whole, from `N/M`.
    private static func rating(_ text: String) -> Result<(halves: Int, slots: Int), NotDrawn> {
        guard let match = pattern.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) else {
            return .failure(NotDrawn("a score is `N/M`, got `\(Grammar.clip(text))`"))
        }
        func group(_ index: Int) -> String {
            guard let range = Range(match.range(at: index), in: text) else { return "" }
            return String(text[range])
        }
        let slots = Int(group(3)) ?? 0
        let halves = (Int(group(1)) ?? 0) * 2 + (group(2) == ".5" ? 1 : 0)
        if slots < 1 || slots > maxSlots {
            return .failure(NotDrawn("a score is out of 1 to \(maxSlots), got `\(Grammar.clip(text))`"))
        }
        if halves > slots * 2 { return .failure(NotDrawn("a score can't be more than its whole, got `\(Grammar.clip(text))`")) }
        return .success((halves, slots))
    }

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title"]) { return .failure(failure) }
        let items: [Grammar.Item]
        switch Grammar.items(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let parsed): items = parsed
        }
        if let failure = Grammar.checkOneBold(items) { return .failure(failure) }
        var made: [Figure.Item] = []
        for each in items {
            guard let (label, written) = Grammar.labelValue(each.text) else {
                return .failure(NotDrawn("a score row is `label: N/M`, got `\(Grammar.clip(each.text))`"))
            }
            let rated: (halves: Int, slots: Int)
            switch rating(written) {
            case .failure(let failure): return .failure(failure)
            case .success(let parsed): rated = parsed
            }
            if !each.details.isEmpty { return .failure(NotDrawn("a score row takes no indented lines")) }
            var item = Figure.Item(label: label, value: written, note: each.note, accent: each.bold, muted: each.italic)
            item.filled = rated.halves
            item.slots = rated.slots
            made.append(item)
        }
        if made.isEmpty { return .failure(NotDrawn("the figure has no rows")) }
        if made.count > maxRows { return .failure(NotDrawn("a score figure has at most \(maxRows) rows")) }
        if Set(made.map(\.slots)).count > 1 { return .failure(NotDrawn("every score is out of the same whole")) }
        return .success(Figure(kind: .score, title: info.attribute("title"), items: made))
    }
}

/// `stack`: rows split into the same named segments, each row filling the
/// track or, with `scale="shared"`, scaled to the largest total.
enum Stack {
    static let maxRows = 12
    static let segments = 2...4

    private struct Segment {
        let name: String
        let written: String
        let value: Double
    }

    private static func segment(_ text: String) -> Result<Segment, NotDrawn> {
        let trimmed = Grammar.trimSpace(text)
        guard let space = Grammar.index(trimmed, " "), space > 0 else {
            return .failure(NotDrawn("a stack segment is `value name`, got `\(Grammar.clip(trimmed))`"))
        }
        let written = Grammar.slice(trimmed, 0, space)
        let name = Grammar.trimSpace(Grammar.slice(trimmed, space + 1))
        guard !name.isEmpty else { return .failure(NotDrawn("a stack segment is `value name`, got `\(Grammar.clip(trimmed))`")) }
        return Grammar.number(written).flatMap { value in
            value < 0
                ? .failure(NotDrawn("stack values can't be negative, got `\(Grammar.clip(written))`"))
                : .success(Segment(name: name, written: written, value: value))
        }
    }

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title", "scale"]) { return .failure(failure) }
        let scale = info.attribute("scale")
        let items: [Grammar.Item]
        switch Grammar.items(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let parsed): items = parsed
        }
        if let failure = Grammar.checkOneBold(items) { return .failure(failure) }
        var rows: [(item: Figure.Item, names: [String], total: Double, shapes: [Grammar.Shape])] = []
        for each in items {
            guard let (label, written) = Grammar.labelValue(each.text) else {
                return .failure(NotDrawn("a stack row is `label: value name, …`, got `\(Grammar.clip(each.text))`"))
            }
            var parsed: [Segment] = []
            for text in Grammar.split(written, ", ") {
                switch segment(text) {
                case .failure(let failure): return .failure(failure)
                case .success(let made): parsed.append(made)
                }
            }
            if !each.details.isEmpty { return .failure(NotDrawn("a stack row takes no indented lines")) }
            guard segments.contains(parsed.count) else {
                return .failure(NotDrawn("a stack row has \(segments.lowerBound) to \(segments.upperBound) segments, got \(parsed.count)"))
            }
            let total = parsed.reduce(0) { $0 + $1.value }
            var item = Figure.Item(label: label, value: Grammar.written(total, Grammar.common(parsed.map { Grammar.shape($0.written) }), signed: false),
                                   note: each.note, accent: each.bold, muted: each.italic)
            item.parts = parsed.map { Grammar.permille($0.value, total) }
            rows.append((item, parsed.map(\.name), total, parsed.map { Grammar.shape($0.written) }))
        }
        if !scale.isEmpty && scale != "shared" { return .failure(NotDrawn("`scale` is `shared`")) }
        if rows.isEmpty { return .failure(NotDrawn("the figure has no rows")) }
        if rows.count > maxRows { return .failure(NotDrawn("a stack figure has at most \(maxRows) rows")) }
        let headings = rows[0].names
        if let different = rows.first(where: { $0.names != headings }) {
            return .failure(NotDrawn(
                "every stack row has the segments `\(Grammar.clip(headings.joined(separator: ", ")))` in order, unlike `\(Grammar.clip(different.item.label))`"))
        }
        let unit = rows[0].shapes[0]
        if let unlike = rows.first(where: { !$0.shapes.allSatisfy { Grammar.sameUnit($0, unit) } }) {
            return .failure(NotDrawn("every stack value is in the same unit, unlike `\(Grammar.clip(unlike.item.label))`"))
        }
        let largest = rows.map(\.total).max() ?? 0
        let made = rows.map { row in
            var item = row.item
            item.scale = scale == "shared" ? Grammar.permille(row.total, largest) : row.total > 0 ? 1000 : 0
            return item
        }
        var figure = Figure(kind: .stack, title: info.attribute("title"), items: made)
        figure.headings = headings
        return .success(figure)
    }
}
