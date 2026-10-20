import Foundation
import Testing
@testable import Kit

struct SessionTreeTests {
    private struct Tab: Equatable {
        let session: String?
        var parent: String? = nil
    }

    private typealias Tree = SessionTree<Tab>

    private func outline(_ tabs: [Tab], catalog: [String: Tree.CatalogEntry] = [:],
                         collapsed: Set<String> = []) -> [String] {
        Tree.rows(tabs, session: \.session, parent: \.parent, catalog: { catalog[$0] },
                  collapsed: collapsed).map { row in
            let indent = String(repeating: "  ", count: row.depth)
            let name: String = switch row.content {
            case .tab(let tab): tab.session ?? "launcher"
            case .closedParent(_, let title): "(\(title))"
            }
            let caret = row.hasChildren ? (row.collapsed ? "+ " : "- ") : ""
            let hidden = row.hiddenTabs.isEmpty ? "" : " [\(row.hiddenTabs.compactMap(\.session).joined(separator: ","))]"
            return indent + caret + name + hidden
        }
    }

    @Test func childrenNestUnderTheirOpenParentInTabOrder() {
        let tabs = [Tab(session: "a"), Tab(session: "b"), Tab(session: "a2", parent: "a"),
                    Tab(session: "a1", parent: "a"), Tab(session: "a1x", parent: "a1")]
        #expect(outline(tabs) == ["- a", "  a2", "  - a1", "    a1x", "b"])
    }

    @Test func aFamilySitsWhereItsEarliestTabIs() {
        let tabs = [Tab(session: "b"), Tab(session: "a1", parent: "a"), Tab(session: "a")]
        #expect(outline(tabs) == ["b", "- a", "  a1"])
    }

    @Test func aClosedParentAppearsWhileAChildIsOpen() {
        let tabs = [Tab(session: "b"), Tab(session: "a1", parent: "a")]
        let catalog = ["a": Tree.CatalogEntry(title: "Parent", parent: nil)]
        #expect(outline(tabs, catalog: catalog) == ["b", "- (Parent)", "  a1"])
    }

    @Test func closedAncestorsConnectATabToItsNearestOpenAncestor() {
        let tabs = [Tab(session: "root"), Tab(session: "c", parent: "b")]
        let catalog = ["b": Tree.CatalogEntry(title: "B", parent: "a"),
                       "a": Tree.CatalogEntry(title: "A", parent: "root")]
        #expect(outline(tabs, catalog: catalog) == ["- root", "  - (A)", "    - (B)", "      c"])
    }

    @Test func withoutAnOpenAncestorOnlyTheClosedParentAppears() {
        let tabs = [Tab(session: "c", parent: "b")]
        let catalog = ["b": Tree.CatalogEntry(title: "B", parent: "a"),
                       "a": Tree.CatalogEntry(title: "A", parent: nil)]
        #expect(outline(tabs, catalog: catalog) == ["- (B)", "  c"])
    }

    @Test func anUnknownParentLeavesTheTabAtTheTopLevel() {
        let tabs = [Tab(session: "b"), Tab(session: "c", parent: "gone")]
        #expect(outline(tabs) == ["b", "c"])
    }

    @Test func launchersFollowEveryFamily() {
        let tabs = [Tab(session: nil), Tab(session: "a"), Tab(session: "a1", parent: "a")]
        #expect(outline(tabs) == ["- a", "  a1", "launcher"])
    }

    @Test func aCollapsedRowHidesItsDescendants() {
        let tabs = [Tab(session: "a"), Tab(session: "a1", parent: "a"), Tab(session: "a1x", parent: "a1"),
                    Tab(session: "b")]
        #expect(outline(tabs, collapsed: ["a"]) == ["+ a [a1,a1x]", "b"])
        #expect(outline(tabs, collapsed: ["a1"]) == ["- a", "  + a1 [a1x]", "b"])
        #expect(outline(tabs, collapsed: ["b"]) == ["- a", "  - a1", "    a1x", "b"])
    }

    private func moving(_ id: String, beside target: String, after: Bool, _ tabs: [Tab],
                        catalog: [String: Tree.CatalogEntry] = [:]) -> [String?]? {
        let rows = Tree.rows(tabs, session: \.session, parent: \.parent, catalog: { catalog[$0] })
        return Tree.order(moving: id, beside: target, after: after, in: rows)?.map(\.session)
    }

    @Test func movingAFamilyCarriesItsDescendants() {
        let tabs = [Tab(session: "a"), Tab(session: "a1", parent: "a"), Tab(session: "b"), Tab(session: nil)]
        #expect(moving("b", beside: "a", after: false, tabs) == ["b", "a", "a1", nil])
        #expect(moving("a", beside: "b", after: true, tabs) == ["b", "a", "a1", nil])
        let moved = (moving("a", beside: "b", after: true, tabs) ?? []).compactMap { session in tabs.first { $0.session == session } }
        #expect(outline(moved) == ["b", "- a", "  a1", "launcher"])
    }

    @Test func movingReordersSiblingsUnderTheirParent() {
        let tabs = [Tab(session: "a"), Tab(session: "a1", parent: "a"), Tab(session: "a2", parent: "a"),
                    Tab(session: "a2x", parent: "a2")]
        #expect(moving("a2", beside: "a1", after: false, tabs) == ["a", "a2", "a2x", "a1"])
    }

    @Test func aClosedParentMovesWithItsChildren() {
        let tabs = [Tab(session: "b"), Tab(session: "p1", parent: "p")]
        let catalog = ["p": Tree.CatalogEntry(title: "P", parent: nil)]
        #expect(moving("p", beside: "b", after: false, tabs, catalog: catalog) == ["p1", "b"])
    }

    @Test func onlySiblingsMove() {
        let tabs = [Tab(session: "a"), Tab(session: "a1", parent: "a"), Tab(session: "b"), Tab(session: nil)]
        #expect(moving("a1", beside: "b", after: false, tabs) == nil)
        #expect(moving("a", beside: "a1", after: true, tabs) == nil)
        #expect(moving("b", beside: "launcher:3", after: true, tabs) == nil)
    }

    @Test func aParentCycleInTheCatalogDoesNotHang() {
        let tabs = [Tab(session: "a", parent: "b"), Tab(session: "b", parent: "a")]
        #expect(outline(tabs) == ["- b", "  a"])
        let closed = [Tab(session: "c", parent: "x")]
        let catalog = ["x": Tree.CatalogEntry(title: "X", parent: "y"),
                       "y": Tree.CatalogEntry(title: "Y", parent: "x")]
        #expect(outline(closed, catalog: catalog) == ["- (X)", "  c"])
    }
}

@MainActor struct SessionTreeWindowTests {
    private func id(_ session: String) -> SessionIdentity { SessionIdentity(server: "local", session: session) }

    /// A window whose catalog knows each session's parent.
    private func model(_ sessions: [String], parents: [String: String], selected: String,
                       closed: [String] = [], collapsed: Set<String> = []) -> SessionWindowModel {
        let model = SessionWindowModel(id: UUID(), sessions: sessions.map(id), selected: id(selected), collapsed: collapsed)
        for session in (sessions + closed).reversed() {
            model.launcherServer.catalog.recordCreated(SessionExcerpt(id: session, title: session.uppercased(), parentSessionID: parents[session],
                                                   sourceTitle: "", model: "m", thinking: "high", workspace: "",
                                                   date: "", messages: []))
        }
        return model
    }

    private func titles(_ tabs: [SessionTab]) -> [String] { tabs.map(\.title) }

    @Test func topTabsAndShortcutsFollowFamilyOrder() {
        let model = model(["a", "b", "a1"], parents: ["a1": "a"], selected: "a")
        #expect(titles(model.familyOrder) == ["A", "A1", "B"])
        #expect(model.select(index: 1, in: .top) == nil)
        #expect(model.selectedSession == id("a1"))
        model.selectAdjacent(1, in: .side)
        #expect(model.selectedSession == id("b"))
        #expect(model.parentTitle(of: model.tabs[2]) == "A")
    }

    @Test func numberedShortcutsFollowVisibleRowsAndReopenClosedParents() {
        let model = model(["b", "p1", "p2", "c"], parents: ["p1": "p", "p2": "p"], selected: "b",
                          closed: ["p"], collapsed: [])
        #expect(model.rows.map(\.id) == ["b", "p", "p1", "p2", "c"])
        #expect(model.select(index: 2, in: .side) == nil)
        #expect(model.selectedSession == id("p1"))
        // A closed parent is for the caller to open.
        #expect(model.select(index: 1, in: .side) == id("p"))
        #expect(model.selectedSession == id("p1"))
        model.toggleCollapsed("p")
        #expect(model.rows.map(\.id) == ["b", "p", "c"])
        #expect(model.select(index: 2, in: .side) == nil)
        #expect(model.selectedSession == id("c"))
        // With top tabs, the strip has no closed parents.
        #expect(model.select(index: 1, in: .top) == nil)
        #expect(model.selectedSession == id("p1"))
    }

    @Test func adjacentSelectionSkipsCollapsedChildrenAndClosedParents() {
        let model = model(["b", "p1", "p2", "c"], parents: ["p1": "p", "p2": "p"], selected: "b",
                          closed: ["p"], collapsed: [])
        #expect(titles(model.navigableTabs(.side)) == ["B", "P1", "P2", "C"])
        model.toggleCollapsed("p")
        #expect(titles(model.navigableTabs(.side)) == ["B", "C"])
        #expect(titles(model.navigableTabs(.top)) == ["B", "P1", "P2", "C"])
        model.selectAdjacent(1, in: .side)
        #expect(model.selectedSession == id("c"))
    }

    @Test func selectingAHiddenTabExpandsItsFamily() {
        let model = model(["a", "a1", "a1x"], parents: ["a1": "a", "a1x": "a1"], selected: "a", collapsed: ["a", "a1"])
        #expect(model.rows.map(\.id) == ["a"])
        #expect(model.select(index: 2, in: .top) == nil)
        #expect(model.selectedSession == id("a1x"))
        #expect(model.collapsed.isEmpty)
        #expect(model.rows.map(\.id) == ["a", "a1", "a1x"])
    }

    @Test func closingTheSelectedTabSelectsTheNextInFamilyOrder() {
        let model = model(["a", "b", "a1"], parents: ["a1": "a"], selected: "a")
        model.close(model.selectedTab)
        // The closed parent stays while its child is open.
        #expect(model.rows.map(\.id) == ["a", "a1", "b"])
        #expect(model.selectedSession == id("a1"))
        model.close(model.selectedTab)
        #expect(model.rows.map(\.id) == ["b"])
        #expect(model.selectedSession == id("b"))
    }

    @Test func aFamilyThatNoLongerAppearsIsForgotten() {
        let model = model(["a", "a1", "b"], parents: ["a1": "a"], selected: "b", collapsed: ["a"])
        model.close(model.tabs[1])
        #expect(model.collapsed.isEmpty)
    }

    @Test func draggingAFamilyReordersTabs() {
        let model = model(["a", "b", "a1"], parents: ["a1": "a"], selected: "a")
        model.move("b", beside: "a", after: false)
        #expect(model.sessions == [id("b"), id("a"), id("a1")])
        #expect(model.rows.map(\.id) == ["b", "a", "a1"])
    }

    @Test func aDropLandsBesideTheSiblingFamilyUnderThePointer() {
        let model = model(["a", "a1", "b", "c"], parents: ["a1": "a"], selected: "a")
        let rows = model.rows
        let drag = SessionTreeDrag()
        // a 0–32, a1 34–66, b 74–106, c 114–146.
        for (row, top) in zip(rows.map(\.id), [0, 34, 74, 114] as [CGFloat]) {
            drag.frames[row] = CGRect(x: 0, y: top, width: 200, height: 32)
        }
        #expect(drag.resolve("c", y: 10, rows: rows) == .init(id: "a", after: false))
        // The family spans its child, so its lower half is past the child.
        #expect(drag.resolve("c", y: 50, rows: rows) == .init(id: "a", after: true))
        #expect(drag.resolve("a", y: 200, rows: rows) == .init(id: "c", after: true))
        #expect(drag.resolve("b", y: 80, rows: rows) == nil)
        // A child only finds its own siblings.
        #expect(drag.resolve("a1", y: 120, rows: rows) == nil)
    }

    @Test func collapsedFamiliesAreRestorationIntent() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(url: url))
        let request = SessionWindowRequest(windowID: UUID())
        let model = registry.model(for: request)
        model.open(id("a"))
        model.toggleCollapsed("a")
        let saved = WindowRestorationStore(url: url).window(request.windowID)
        #expect(saved?.collapsed == ["a"])
        let restored = SessionWindowRegistry(restoration: WindowRestorationStore(url: url)).model(for: request)
        #expect(restored.collapsed == ["a"])
    }
}
