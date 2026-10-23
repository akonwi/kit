import Foundation

/// The palette's Open session list: sessions as a tree, each under the
/// session it came from, families in order of their most recent session.
/// A session whose parent isn't listed sits at the top level. Families start
/// expanded. While searching, matches show beneath their ancestors and
/// collapsed families are ignored.
///
/// Sessions are identified by `id` and linked by `parentSessionID`, so a
/// caller listing several servers keys both by tree key.
struct SessionPicker {
    /// One row of the list.
    struct Row: Equatable, Identifiable {
        let session: SessionExcerpt
        let depth: Int
        /// The session's direct children, or nil while searching, when rows
        /// have no disclosure.
        let children: Int?
        let collapsed: Bool
        let status: SessionTabStatus
        /// The most urgent status among the sessions a collapsed row hides.
        let hidden: SessionTabStatus?
        /// The row is shown only as the ancestor of a search match.
        let context: Bool

        var id: String { session.id }
    }

    var query = ""
    var highlightedID: String?
    var collapsed: Set<String> = []

    var searching: Bool { !query.allSatisfy(\.isWhitespace) }

    /// The visible rows.
    /// - Parameter activity: The status of each working or waiting session.
    func rows(_ sessions: [SessionExcerpt], activity: [String: SessionTabStatus] = [:]) -> [Row] {
        let ordered = SessionLauncher.recent(sessions)
        guard searching else {
            return tree(ordered, collapsed: collapsed).map { row in
                Row(session: row.session, depth: row.depth, children: row.children, collapsed: row.collapsed,
                    status: activity[row.id] ?? .idle,
                    hidden: row.collapsed ? SessionTreeRollUp(row.hidden.map { (activity[$0.id] ?? .idle, false) }).marker?.status : nil,
                    context: false)
            }
        }
        let full = tree(ordered, collapsed: [])
        let parents = Self.parents(full)
        let matched = Set(ordered.filter { SessionLauncher.matches($0, query: query) }.map(\.id))
        var shown = matched
        for id in matched { shown.formUnion(Self.ancestors(of: id, parents: parents)) }
        // Every shown row's ancestors are shown, so the tree keeps its shape.
        return full.filter { shown.contains($0.id) }.map { row in
            Row(session: row.session, depth: row.depth, children: nil, collapsed: false,
                status: activity[row.id] ?? .idle, hidden: nil, context: !matched.contains(row.id))
        }
    }

    // MARK: Highlight

    mutating func move(_ offset: Int, in rows: [Row]) {
        guard !rows.isEmpty else { highlightedID = nil; return }
        let current = rows.firstIndex { $0.id == highlightedID } ?? (offset > 0 ? -1 : rows.count)
        highlightedID = rows[max(0, min(rows.count - 1, current + offset))].id
    }

    /// Keeps the highlight on a shown row after the rows change: the row
    /// itself, else its nearest shown ancestor, else the first row.
    mutating func reveal(in rows: [Row], sessions: [SessionExcerpt]) {
        let shown = Set(rows.map(\.id))
        if let highlightedID, shown.contains(highlightedID) { return }
        let parents = Self.parents(tree(SessionLauncher.recent(sessions), collapsed: []))
        let ancestor = highlightedID.flatMap { id in Self.ancestors(of: id, parents: parents).first(where: shown.contains) }
        highlightedID = ancestor ?? rows.first?.id
    }

    /// Highlights the first match after the query changes, rather than an
    /// ancestor shown for context. A cleared query keeps the highlighted row
    /// or its nearest shown ancestor.
    mutating func queryChanged(rows: [Row], sessions: [SessionExcerpt]) {
        if searching {
            highlightedID = (rows.first { !$0.context } ?? rows.first)?.id
        } else {
            reveal(in: rows, sessions: sessions)
        }
    }

    // MARK: Disclosure

    /// Collapses or expands the highlighted row, as ←/→ do. They only change
    /// disclosure while the search is empty; otherwise they move the cursor
    /// in the search field, so this returns false.
    mutating func disclose(expand: Bool, in rows: [Row]) -> Bool {
        guard query.isEmpty else { return false }
        guard let row = rows.first(where: { $0.id == highlightedID }), (row.children ?? 0) > 0 else { return true }
        if expand { collapsed.remove(row.id) } else { collapsed.insert(row.id) }
        return true
    }

    /// Collapses or expands a row, as its caret does.
    mutating func toggle(_ id: String) {
        if collapsed.remove(id) == nil { collapsed.insert(id) }
    }

    // MARK: Tree

    private struct TreeRow {
        let session: SessionExcerpt
        let depth: Int
        let children: Int
        let collapsed: Bool
        let hidden: [SessionExcerpt]
        var id: String { session.id }
    }

    private func tree(_ sessions: [SessionExcerpt], collapsed: Set<String>) -> [TreeRow] {
        SessionTree<SessionExcerpt>.rows(sessions, session: { $0.id }, parent: { $0.parentSessionID },
                                         catalog: { _ in nil }, collapsed: collapsed).compactMap { row in
            row.tab.map { TreeRow(session: $0, depth: row.depth, children: row.children, collapsed: row.collapsed,
                                  hidden: row.hiddenTabs) }
        }
    }

    /// Each row's parent in a complete tree, by ID.
    private static func parents(_ rows: [TreeRow]) -> [String: String] {
        var parents: [String: String] = [:]
        var path: [String] = []
        for row in rows {
            path.removeLast(max(0, path.count - row.depth))
            if let parent = path.last { parents[row.id] = parent }
            path.append(row.id)
        }
        return parents
    }

    /// A session's ancestors in the tree, nearest first.
    private static func ancestors(of id: String, parents: [String: String]) -> [String] {
        var result: [String] = []
        var next = parents[id]
        while let parent = next, result.count < parents.count {
            result.append(parent)
            next = parents[parent]
        }
        return result
    }
}
