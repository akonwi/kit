import Foundation

/// `meter`: one share of a whole, or rows each measured against its own
/// target, with the targets lined up in one column.
enum Meter {
    static let maxRows = 12

    /// A row's value and target, split at ` / `.
    private static func fraction(_ text: String) -> (String, String)? {
        guard let at = Grammar.index(text, " / "), at > 0 else { return nil }
        return (Grammar.trimSpace(Grammar.slice(text, 0, at)), Grammar.trimSpace(Grammar.slice(text, at + 3)))
    }

    /// A value and the target it is measured against.
    private static func against(_ value: String, _ target: String) -> Result<(Double, Double), NotDrawn> {
        Grammar.number(value).flatMap { amount in
            Grammar.number(target).flatMap { whole in
                if amount < 0 { return .failure(NotDrawn("a meter can't be negative, got `\(Grammar.clip(value))`")) }
                if whole <= 0 { return .failure(NotDrawn("a meter's target must be above zero, got `\(Grammar.clip(target))`")) }
                return .success((amount, whole))
            }
        }
    }

    /// A single meter's share of its whole: `N%`, a fraction from 0 to 1, or `a / b`.
    static func ratio(_ text: String) -> Result<Double, NotDrawn> {
        if let (value, target) = fraction(text) {
            return against(value, target).map { $0.0 / $0.1 }
        }
        return Grammar.number(text).flatMap { value in
            if value < 0 { return .failure(NotDrawn("a meter can't be negative, got `\(Grammar.clip(text))`")) }
            if Grammar.hasSuffix(text, "%") { return .success(value / 100) }
            if value > 1 {
                return .failure(NotDrawn("a meter is `N%`, a fraction from 0 to 1, or `a / b`, got `\(Grammar.clip(text))`"))
            }
            return .success(value)
        }
    }

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title"]) { return .failure(failure) }
        let lines = body.components(separatedBy: "\n").map(Grammar.trimSpace).filter { !$0.isEmpty }
        let first = lines.first ?? ""
        if body.utf8.count > Grammar.maxBody {
            return .failure(NotDrawn("the figure is larger than \(Grammar.maxBody / 1024) KiB"))
        }
        if lines.isEmpty { return .failure(NotDrawn("the figure has no rows")) }
        if Grammar.hasPrefix(first, "- ") || Grammar.isOrdered(first) { return rows(info, body) }
        if lines.count > 1 {
            return .failure(NotDrawn("a single meter is one line, as in `67% — note`; rows are a list"))
        }
        let each = Grammar.line(first)
        return ratio(each.text).map { share in
            var item = Figure.Item(label: "", value: each.text, note: each.note, accent: true)
            item.scale = Grammar.permille(share, 1)
            item.over = share > 1
            return Figure(kind: .meter, title: info.attribute("title"), items: [item])
        }
    }

    private static func rows(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        let items: [Grammar.Item]
        switch Grammar.items(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let parsed): items = parsed
        }
        if items.filter(\.bold).count > 1 { return .failure(NotDrawn("only one item can be bold")) }
        var made: [Figure.Item] = []
        for each in items {
            guard let (label, written) = Grammar.labelValue(each.text) else {
                return .failure(NotDrawn("a meter row is `label: value / target`, got `\(Grammar.clip(each.text))`"))
            }
            guard let (value, target) = fraction(written) else {
                return .failure(NotDrawn("a meter row needs a target, as in `label: 9 / 12`, got `\(Grammar.clip(each.text))`"))
            }
            let amount: Double, whole: Double
            switch against(value, target) {
            case .failure(let failure): return .failure(failure)
            case .success(let pair): (amount, whole) = pair
            }
            if !each.details.isEmpty { return .failure(NotDrawn("a meter row takes no indented lines")) }
            var item = Figure.Item(label: label, value: value, note: each.note, accent: each.bold, muted: each.italic)
            item.target = target
            item.scale = Grammar.permille(amount, whole * 1.25)
            item.over = amount > whole
            made.append(item)
        }
        if made.isEmpty { return .failure(NotDrawn("the figure has no rows")) }
        if made.count > maxRows { return .failure(NotDrawn("a meter figure has at most \(maxRows) rows")) }
        return .success(Figure(kind: .meter, title: info.attribute("title"), items: made))
    }
}

/// Where each item stands against the first bold one.
private func states(_ items: [Grammar.Item]) -> [Figure.State] {
    guard let found = items.firstIndex(where: \.bold) else { return items.map { _ in .none } }
    return items.indices.map { $0 < found ? .done : $0 == found ? .now : .next }
}

/// `timeline`: dated events in order, each `when: event` with an optional
/// indented body. The bold item is now; items before it are done.
enum Timeline {
    static let maxItems = 20

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title"]) { return .failure(failure) }
        let items: [Grammar.Item]
        switch Grammar.items(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let parsed): items = parsed
        }
        if items.filter(\.bold).count > 1 { return .failure(NotDrawn("only one item can be bold")) }
        let standing = states(items)
        var made: [Figure.Item] = []
        for (index, each) in items.enumerated() {
            guard let (when, event) = Grammar.labelValue(each.text) else {
                return .failure(NotDrawn("a timeline item is `when: event`, got `\(Grammar.clip(each.text))`"))
            }
            var item = Figure.Item(label: when, value: event, note: each.note, accent: each.bold, muted: each.italic)
            item.body = each.details.joined(separator: " ")
            item.state = standing[index]
            made.append(item)
        }
        if made.isEmpty { return .failure(NotDrawn("the figure has no items")) }
        if made.count > maxItems { return .failure(NotDrawn("a timeline has at most \(maxItems) items")) }
        return .success(Figure(kind: .timeline, title: info.attribute("title"), items: made))
    }
}

/// `steps`: an ordered list of steps, each a title with an optional
/// indented body. The bold step is current; the steps before it are done.
enum Steps {
    static let maxItems = 20

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title"]) { return .failure(failure) }
        let items: [Grammar.Item]
        switch Grammar.items(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let parsed): items = parsed
        }
        if items.filter(\.bold).count > 1 { return .failure(NotDrawn("only one item can be bold")) }
        let standing = states(items)
        let first = items.first?.ordinal ?? 1
        let made = items.enumerated().map { index, each in
            var item = Figure.Item(label: String(first + index), value: each.text, note: each.note, accent: each.bold, muted: each.italic)
            item.body = each.details.joined(separator: " ")
            item.state = standing[index]
            return item
        }
        if let unordered = items.first(where: { $0.ordinal < 0 }) {
            return .failure(NotDrawn("steps are an ordered list, as in `1. \(Grammar.clip(unordered.text))`"))
        }
        if made.isEmpty { return .failure(NotDrawn("the figure has no steps")) }
        if made.count > maxItems { return .failure(NotDrawn("a steps figure has at most \(maxItems) steps")) }
        return .success(Figure(kind: .steps, title: info.attribute("title"), items: made))
    }
}
