import Foundation

/// `decision`: the options weighed, the chosen one bold and those ruled out
/// in italics, each with its reason, under the decision's status and date
/// and over a closing note.
enum Decision {
    static let minOptions = 2
    static let maxOptions = 8
    static let statuses = ["proposed", "accepted", "rejected", "superseded"]

    static func parse(_ info: Grammar.Info, _ body: String) -> Result<Figure, NotDrawn> {
        if let failure = Grammar.checkAttributes(info, ["title", "status", "date"]) { return .failure(failure) }
        let status = info.attribute("status")
        guard status.isEmpty || statuses.contains(status) else {
            return .failure(NotDrawn("`status` is `proposed`, `accepted`, `rejected` or `superseded`"))
        }
        let listed: (items: [Grammar.Item], note: String)
        switch Grammar.itemsAndNote(body) {
        case .failure(let failure): return .failure(failure)
        case .success(let parsed): listed = parsed
        }
        if let failure = Grammar.checkOneBold(listed.items) { return .failure(failure) }
        if listed.items.contains(where: { !$0.details.isEmpty }) {
            return .failure(NotDrawn("a decision's options take no indented lines"))
        }
        guard (minOptions...maxOptions).contains(listed.items.count) else {
            return .failure(NotDrawn("a decision has \(minOptions) to \(maxOptions) options, got \(listed.items.count)"))
        }
        let made = listed.items.map {
            Figure.Item(label: $0.text, value: "", note: $0.note, accent: $0.bold, muted: $0.italic)
        }
        var figure = Figure(kind: .decision, title: info.attribute("title"), items: made)
        figure.status = status
        figure.date = info.attribute("date")
        figure.note = listed.note
        return .success(figure)
    }
}

/// `spec`: labelled values in one aligned column, the values wrapping as
/// prose, each with an optional indented note under it.
enum Spec {
    static let maxRows = 30

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
            guard let (label, value) = Grammar.labelValue(each.text) else {
                return .failure(NotDrawn("a spec row is `label: value`, got `\(Grammar.clip(each.text))`"))
            }
            var item = Figure.Item(label: label, value: value, note: each.note, accent: each.bold, muted: each.italic)
            item.body = each.details.joined(separator: " ")
            made.append(item)
        }
        if made.isEmpty { return .failure(NotDrawn("the figure has no rows")) }
        if made.count > maxRows { return .failure(NotDrawn("a spec has at most \(maxRows) rows")) }
        return .success(Figure(kind: .spec, title: info.attribute("title"), items: made))
    }
}
