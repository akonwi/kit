import Foundation

/// The sidebar's session tree. Each open session tab sits under the session
/// it came from: a fork's source, or the session whose agent created it. A
/// parent whose tab is closed appears as a closed row while a child is open.
/// Families keep tab order: a family sits where its earliest tab is, and
/// children follow the same rule among themselves. Tabs without a session,
/// such as the launcher, follow every family.
struct SessionTree<Tab> {
    /// A session the window knows about but hasn't opened.
    struct CatalogEntry {
        let title: String
        let parent: String?
    }

    /// One row of the tree.
    struct Row {
        enum Content {
            case tab(Tab)
            /// A parent whose tab is closed, shown because a child is open.
            case closedParent(session: String, title: String)
        }

        /// The row's session, or a launcher key.
        let id: String
        let content: Content
        let depth: Int
        /// The row has children, and so a disclosure caret.
        let hasChildren: Bool
        let collapsed: Bool
        /// The open tabs a collapsed row hides.
        let hiddenTabs: [Tab]

        var tab: Tab? {
            if case .tab(let tab) = content { tab } else { nil }
        }
    }

    /// Ancestors followed upward before giving up, which also bounds a
    /// malformed parent cycle in the catalog.
    static var maximumAncestors: Int { 64 }

    private struct Node {
        let id: String
        let tab: Tab?
        let title: String
        let order: Int
        var parent: String?
        var children: [String] = []
    }

    /// Builds the visible rows.
    /// - Parameters:
    ///   - session: The tab's session, or nil for a launcher.
    ///   - parent: The session the tab's session came from.
    ///   - catalog: Looks up a session that has no open tab. A tab whose
    ///     parent it doesn't know, as when the parent was deleted, sits at
    ///     the top level.
    ///   - collapsed: Rows whose children are hidden.
    static func rows(_ tabs: [Tab], session: (Tab) -> String?, parent: (Tab) -> String?,
                     catalog: (String) -> CatalogEntry?, collapsed: Set<String> = []) -> [Row] {
        var nodes: [String: Node] = [:]
        var openOrder: [String] = []
        for (index, tab) in tabs.enumerated() {
            let key = session(tab).flatMap { $0.isEmpty ? nil : $0 } ?? "\(launcherPrefix)\(index)"
            guard nodes[key] == nil else { continue }
            nodes[key] = Node(id: key, tab: tab, title: "", order: index)
            openOrder.append(key)
        }

        func isAncestor(_ candidate: String, of id: String) -> Bool {
            var current = nodes[id]?.parent
            var steps = 0
            while let next = current, steps <= nodes.count {
                if next == candidate { return true }
                current = nodes[next]?.parent
                steps += 1
            }
            return false
        }

        func link(_ child: String, to parent: String) {
            guard child != parent, nodes[child]?.parent == nil, !isAncestor(child, of: parent) else { return }
            nodes[child]?.parent = parent
            nodes[parent]?.children.append(child)
        }

        for key in openOrder {
            guard let tab = nodes[key]?.tab, session(tab) != nil,
                  let parentID = parent(tab), !parentID.isEmpty, parentID != key else { continue }
            if nodes[parentID] != nil {
                link(key, to: parentID)
                continue
            }
            guard let entry = catalog(parentID) else { continue }
            // Closed ancestors appear only to connect a tab to its nearest
            // open ancestor; without one, only the closed parent appears.
            var chain = [(parentID, entry)]
            var openAncestor: String?
            var next = entry.parent
            while let id = next, !id.isEmpty, chain.count <= maximumAncestors,
                  !chain.contains(where: { $0.0 == id }) {
                if nodes[id] != nil { openAncestor = id; break }
                guard let ancestor = catalog(id) else { break }
                chain.append((id, ancestor))
                next = ancestor.parent
            }
            let included = openAncestor == nil ? Array(chain.prefix(1)) : chain
            for (id, ancestor) in included where nodes[id] == nil {
                nodes[id] = Node(id: id, tab: nil, title: ancestor.title, order: nodes[key]?.order ?? 0)
            }
            link(key, to: parentID)
            for (index, (id, _)) in included.enumerated().dropFirst() {
                link(included[index - 1].0, to: id)
            }
            if let openAncestor, let top = included.last?.0 { link(top, to: openAncestor) }
        }

        var earliest: [String: Int] = [:]
        func firstTab(_ id: String, depth: Int = 0) -> Int {
            if let known = earliest[id] { return known }
            guard let node = nodes[id] else { return .max }
            let value = depth > nodes.count ? node.order
                : node.children.map { firstTab($0, depth: depth + 1) }.reduce(node.order, min)
            earliest[id] = value
            return value
        }
        func sorted(_ ids: [String]) -> [String] {
            ids.sorted { lhs, rhs in
                let left = isLauncher(lhs), right = isLauncher(rhs)
                if left != right { return right }
                return (firstTab(lhs), lhs) < (firstTab(rhs), rhs)
            }
        }
        func openTabs(under id: String, depth: Int = 0) -> [Tab] {
            guard let node = nodes[id], depth <= nodes.count else { return [] }
            return sorted(node.children).flatMap { child in
                (nodes[child]?.tab.map { [$0] } ?? []) + openTabs(under: child, depth: depth + 1)
            }
        }

        var rows: [Row] = []
        var emitted: Set<String> = []
        func emit(_ id: String, depth: Int) {
            guard let node = nodes[id], emitted.insert(id).inserted else { return }
            let hasChildren = !node.children.isEmpty
            let folded = hasChildren && collapsed.contains(id)
            let content: Row.Content = node.tab.map { .tab($0) } ?? .closedParent(session: id, title: node.title)
            rows.append(Row(id: id, content: content, depth: depth, hasChildren: hasChildren, collapsed: folded,
                            hiddenTabs: folded ? openTabs(under: id) : []))
            guard !folded else { return }
            for child in sorted(node.children) { emit(child, depth: depth + 1) }
        }
        for root in sorted(nodes.values.filter { $0.parent == nil }.map(\.id)) { emit(root, depth: 0) }
        return rows
    }

    /// The tab order that moves a family beside one of its sibling families,
    /// or nil if the two rows aren't siblings. Families at the top level are
    /// siblings. Launchers stay after every family.
    /// - Parameter rows: The tree without collapsed families.
    static func order(moving id: String, beside target: String, after: Bool, in rows: [Row]) -> [Tab]? {
        guard !isLauncher(id), !isLauncher(target),
              let source = rows.firstIndex(where: { $0.id == id }),
              let destination = rows.firstIndex(where: { $0.id == target }), source != destination,
              parent(of: source, in: rows) == parent(of: destination, in: rows) else { return nil }
        let moved = family(source, in: rows)
        var remaining = rows.indices.filter { !moved.contains($0) }
        let span = family(destination, in: rows)
        guard let insertion = after ? remaining.firstIndex(of: span.upperBound - 1).map({ $0 + 1 })
                                    : remaining.firstIndex(of: span.lowerBound) else { return nil }
        remaining.insert(contentsOf: moved, at: insertion)
        return remaining.compactMap { rows[$0].tab }
    }

    /// The parent row's ID, or nil at the top level.
    static func parent(of index: Int, in rows: [Row]) -> String? {
        var current = index
        while current > 0 {
            current -= 1
            if rows[current].depth < rows[index].depth { return rows[current].id }
        }
        return nil
    }

    /// A row and the rows below it that descend from it.
    static func family(_ index: Int, in rows: [Row]) -> Range<Int> {
        var end = index + 1
        while end < rows.count, rows[end].depth > rows[index].depth { end += 1 }
        return index..<end
    }

    static func isLauncher(_ id: String) -> Bool { id.hasPrefix(launcherPrefix) }
    private static var launcherPrefix: String { "launcher:" }
}
