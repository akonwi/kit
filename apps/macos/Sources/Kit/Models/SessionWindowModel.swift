import AppKit
import Foundation
import Observation

/// Session activity shown on a session tab.
enum SessionTabStatus: String, Decodable, Sendable {
    case idle, running, awaitingResponse

    var label: String? {
        switch self {
        case .idle: nil
        case .running: "Working"
        case .awaitingResponse: "Awaiting response"
        }
    }
}

/// Where a window lists its session tabs.
enum SessionTabPlacement: String {
    case side, top
    static let storageKey = "sessionTabPlacement"
}

/// One session tab. A tab without a session shows the launcher. Its store,
/// and therefore its stream, drafts, and workspace, lives as long as the tab,
/// so switching tabs keeps every session's state.
@MainActor @Observable
final class SessionTab: Identifiable {
    let id = UUID()
    fileprivate(set) var identity: SessionIdentity
    fileprivate(set) var store: SessionStore?
    /// A restored tile layout, applied when the tab's store is created.
    fileprivate var savedArrangement: TileArrangement?
    /// The window's session catalog, which describes tabs not yet opened.
    private weak var catalog: AppModel?

    init(identity: SessionIdentity, catalog: AppModel? = nil) {
        self.identity = identity
        self.catalog = catalog
    }

    var isLauncher: Bool { identity.session.isEmpty }
    /// The live session once opened; until then, its catalog entry.
    var session: SessionExcerpt? {
        store?.selected ?? catalog?.sessions.first { $0.id == identity.session }
    }
    var title: String { session?.title ?? (isLauncher ? "New session" : identity.session) }
    var status: SessionTabStatus { store?.tabStatus ?? .idle }
    /// A turn finished while the user was elsewhere; cleared when the tab is
    /// next shown in a focused window.
    fileprivate(set) var finished = false
    /// The last status the window recorded, to detect a finished turn.
    fileprivate var recordedStatus: SessionTabStatus = .idle
    /// Whether the tab's marker calls for attention; idle tabs recede.
    var needsAttention: Bool { status != .idle || finished }
    var statusLabel: String? { status.label ?? (finished ? "Finished" : nil) }
    /// The session's working directory, which names its project on a top tab.
    var directory: String { session?.cwd ?? session?.workspace ?? "" }
    /// The session this one came from, as a fork or an agent's work.
    var parentSessionID: String? { isLauncher ? nil : session?.parentSessionID }
}

/// A row of the sidebar's session tree.
typealias SessionTreeRow = SessionTree<SessionTab>.Row

/// A window's session tabs and the connection they share.
@MainActor @Observable
final class SessionWindowModel: Identifiable {
    let id: UUID
    let app = AppModel()
    private(set) var tabs: [SessionTab]
    private(set) var selectedTabID: UUID
    /// Sessions whose children the sidebar hides.
    private(set) var collapsed: Set<String>
    @ObservationIgnored weak var window: NSWindow?
    /// Keeps the window's traffic lights in the chrome row while it is open.
    @ObservationIgnored var trafficLights: TrafficLightPlacement?
    /// Called after tabs, selection, or tile layouts change, to record
    /// restoration intent.
    @ObservationIgnored var onChange: (SessionWindowModel) -> Void = { _ in }
    /// A pending capture of tile layout changes, which can arrive with every
    /// divider drag step.
    @ObservationIgnored private var layoutCapture: Task<Void, Never>?
    static let layoutCaptureDelay: Duration = .milliseconds(500)

    static let defaultServer = "local-v2"

    init(id: UUID, sessions: [SessionIdentity] = [], selected: SessionIdentity? = nil,
         layouts: [SessionIdentity: TileArrangement] = [:], collapsed: Set<String> = []) {
        self.id = id
        self.collapsed = collapsed
        let app = app
        let tabs = sessions.isEmpty
            ? [SessionTab(identity: Self.launcherIdentity, catalog: app)]
            : sessions.map { identity in
                let tab = SessionTab(identity: identity, catalog: app)
                tab.savedArrangement = layouts[identity]
                return tab
            }
        self.tabs = tabs
        selectedTabID = (selected.flatMap { identity in tabs.first { $0.identity == identity } } ?? tabs[0]).id
    }

    static var launcherIdentity: SessionIdentity { SessionIdentity(server: defaultServer, session: "") }

    var selectedTab: SessionTab { tabs.first { $0.id == selectedTabID } ?? tabs[0] }
    var selectedIndex: Int { tabs.firstIndex { $0.id == selectedTabID } ?? 0 }

    /// The sidebar's session tree, with collapsed families folded.
    var rows: [SessionTreeRow] { tree(collapsed: collapsed) }
    /// Every tab in family order, as the top tabs show them.
    var familyOrder: [SessionTab] { tree(collapsed: []).compactMap(\.tab) }

    /// The tabs ⌘1–⌘9 and next/previous follow: the sidebar's visible tab
    /// rows, or every top tab.
    func navigableTabs(_ placement: SessionTabPlacement) -> [SessionTab] {
        placement == .side ? rows.compactMap(\.tab) : familyOrder
    }

    /// The parent's title for a tab whose session came from another.
    func parentTitle(of tab: SessionTab) -> String? {
        guard let parent = tab.parentSessionID else { return nil }
        return tabs.first { !$0.isLauncher && $0.identity.session == parent }?.title
            ?? app.sessions.first { $0.id == parent }?.title
            ?? tab.session?.parentSessionName
    }

    private func tree(collapsed: Set<String>) -> [SessionTreeRow] {
        let catalog = Dictionary(app.sessions.map { ($0.id, $0) }) { first, _ in first }
        return SessionTree.rows(tabs, session: { $0.isLauncher ? nil : $0.identity.session },
                                parent: \.parentSessionID,
                                catalog: { id in
                                    if let session = catalog[id] { return .init(title: session.title, parent: session.parentSessionID) }
                                    // Outside the catalog, a child may still know its parent's name.
                                    let name = self.tabs.lazy.compactMap { $0.parentSessionID == id ? $0.session?.parentSessionName : nil }.first
                                    return name.map { .init(title: $0, parent: nil) }
                                },
                                collapsed: collapsed)
    }

    /// Shows or hides a session's children in the sidebar.
    func toggleCollapsed(_ session: String) {
        if collapsed.remove(session) == nil { collapsed.insert(session) }
        onChange(self)
    }

    /// Moves a family before or after a sibling family.
    func move(_ id: String, beside target: String, after: Bool) {
        guard let order = SessionTree.order(moving: id, beside: target, after: after, in: tree(collapsed: [])),
              order.count == tabs.count, !zip(order, tabs).allSatisfy({ $0 === $1 }) else { return }
        tabs = order
        onChange(self)
    }

    /// Expands the families that hide a tab, so a selected tab is visible.
    private func reveal(_ tab: SessionTab) {
        guard !collapsed.isEmpty else { return }
        let all = tree(collapsed: [])
        guard var index = all.firstIndex(where: { $0.tab === tab }) else { return }
        var depth = all[index].depth
        var ancestors: [String] = []
        while index > 0, depth > 0 {
            index -= 1
            if all[index].depth < depth {
                ancestors.append(all[index].id)
                depth = all[index].depth
            }
        }
        collapsed.subtract(ancestors)
    }

    /// Forgets collapsed families that no longer appear.
    private func pruneCollapsed() {
        guard !collapsed.isEmpty else { return }
        let parents = Set(tree(collapsed: []).filter(\.hasChildren).map(\.id))
        collapsed.formIntersection(parents)
    }

    /// Session identities to restore, in tab order; launcher tabs are not intent.
    var sessions: [SessionIdentity] { tabs.filter { !$0.isLauncher }.map(\.identity) }
    var selectedSession: SessionIdentity? { selectedTab.isLauncher ? nil : selectedTab.identity }
    /// Tile layouts to restore, for session tabs whose layout isn't the default.
    var layouts: [SessionIdentity: TileArrangement] {
        var result: [SessionIdentity: TileArrangement] = [:]
        for tab in tabs where !tab.isLauncher {
            if let arrangement = tab.store?.ui.workspace.arrangement ?? tab.savedArrangement, !arrangement.isDefault {
                result[tab.identity] = arrangement
            }
        }
        return result
    }

    var hasPendingLayoutCapture: Bool { layoutCapture != nil }

    /// Records pending layout changes now, as when the app quits.
    func captureLayouts() {
        guard layoutCapture != nil else { return }
        layoutCapture?.cancel()
        layoutCapture = nil
        onChange(self)
    }

    func tab(for identity: SessionIdentity) -> SessionTab? {
        tabs.first { !$0.isLauncher && $0.identity == identity }
    }

    func select(_ tab: SessionTab) {
        guard tabs.contains(where: { $0 === tab }) else { return }
        let folded = collapsed
        reveal(tab)
        guard selectedTabID != tab.id else {
            if collapsed != folded { onChange(self) }
            return
        }
        selectedTabID = tab.id
        if isFocused { tab.finished = false }
        onChange(self)
    }

    /// Whether the window is key, so its selected tab is in front of the user.
    @ObservationIgnored var isFocused = true {
        didSet { if isFocused { selectedTab.finished = false } }
    }

    /// Records a tab's status. A turn that ends while the tab is not in front
    /// of the user marks it finished.
    func record(_ status: SessionTabStatus, for tab: SessionTab) {
        let previous = tab.recordedStatus
        tab.recordedStatus = status
        if status != .idle {
            tab.finished = false
        } else if previous != .idle, !(isFocused && tab.id == selectedTabID) {
            tab.finished = true
        }
    }

    /// Selects the tab at a display position.
    func select(index: Int, in placement: SessionTabPlacement) {
        let displayed = navigableTabs(placement)
        guard displayed.indices.contains(index) else { return }
        select(displayed[index])
    }

    func selectAdjacent(_ offset: Int, in placement: SessionTabPlacement) {
        let displayed = navigableTabs(placement)
        guard displayed.count > 1, let current = displayed.firstIndex(where: { $0.id == selectedTabID }) else { return }
        select(displayed[(current + offset + displayed.count) % displayed.count])
    }

    /// Selects the session's tab. Otherwise the session replaces a selected
    /// launcher, or opens in a new tab after the selected one.
    @discardableResult
    func open(_ identity: SessionIdentity) -> SessionTab {
        if let existing = tab(for: identity) {
            select(existing)
            return existing
        }
        if selectedTab.isLauncher, !identity.session.isEmpty {
            show(identity, in: selectedTab)
            return selectedTab
        }
        let tab = SessionTab(identity: identity, catalog: app)
        tabs.insert(tab, at: selectedIndex + 1)
        selectedTabID = tab.id
        reveal(tab)
        onChange(self)
        return tab
    }

    /// Opens a launcher tab, reusing an open one.
    func openLauncher() {
        if let launcher = tabs.first(where: \.isLauncher) {
            select(launcher)
        } else {
            open(Self.launcherIdentity)
        }
    }

    /// Points a tab at another session, as when the launcher or chooser picks one.
    func show(_ identity: SessionIdentity, in tab: SessionTab) {
        guard tabs.contains(where: { $0 === tab }), tab.identity != identity else { return }
        if let existing = self.tab(for: identity), existing !== tab {
            close(tab)
            select(existing)
            return
        }
        if tab.store?.selectedID != identity.session { release(tab) }
        tab.identity = identity
        tab.savedArrangement = nil
        reveal(tab)
        pruneCollapsed()
        onChange(self)
    }

    /// Closes a tab. Closing the last tab leaves a launcher in its place.
    /// Closing the selected tab selects the next tab in family order.
    func close(_ tab: SessionTab) {
        guard tabs.contains(where: { $0 === tab }) else { return }
        // Tabs take their family order first, so a family whose parent
        // closes stays where it was.
        let order = familyOrder
        if order.count == tabs.count { tabs = order }
        guard let index = tabs.firstIndex(where: { $0 === tab }) else { return }
        let position = order.firstIndex { $0 === tab } ?? 0
        release(tab)
        tabs.remove(at: index)
        if tabs.isEmpty { tabs = [SessionTab(identity: Self.launcherIdentity, catalog: app)] }
        if selectedTabID == tab.id {
            let next = order[(position + 1)...].first ?? order[..<position].last ?? tabs[0]
            selectedTabID = next.id
            reveal(next)
        }
        pruneCollapsed()
        onChange(self)
    }

    /// Creates the tab's store on first display and keeps it attached, so
    /// background tabs keep streaming status.
    func store(for tab: SessionTab, client: any SessionClient) -> SessionStore? {
        guard !tab.isLauncher, tab.identity.server.isEmpty || tab.identity.server == client.serverID else { return nil }
        if let store = tab.store, store.serverID == client.serverID { return store }
        let store = SessionStore(fixture: Fixture(sessions: app.sessions), client: client, sessionID: tab.identity.session)
        if let arrangement = tab.savedArrangement { store.ui.workspace.restore(arrangement) }
        tab.savedArrangement = nil
        tab.store = store
        tab.recordedStatus = store.tabStatus
        store.attach()
        observeStatus(of: tab, store: store)
        observeLayout(of: tab, store: store)
        return store
    }

    /// Attaches every session tab, so background tabs report their status.
    func attachAll(client: any SessionClient) {
        for tab in tabs { _ = store(for: tab, client: client) }
    }

    private func observeStatus(of tab: SessionTab, store: SessionStore) {
        withObservationTracking {
            _ = store.tabStatus
        } onChange: { [weak self, weak tab, weak store] in
            Task { @MainActor in
                guard let self, let tab, let store, tab.store === store else { return }
                self.record(store.tabStatus, for: tab)
                self.observeStatus(of: tab, store: store)
            }
        }
    }

    /// Captures the tab's tile layout shortly after it changes.
    private func observeLayout(of tab: SessionTab, store: SessionStore) {
        withObservationTracking {
            _ = store.ui.workspace.arrangement
        } onChange: { [weak self, weak tab, weak store] in
            Task { @MainActor in
                guard let self, let tab, let store, tab.store === store else { return }
                self.scheduleLayoutCapture()
                self.observeLayout(of: tab, store: store)
            }
        }
    }

    private func scheduleLayoutCapture() {
        layoutCapture?.cancel()
        layoutCapture = Task { @MainActor [weak self] in
            try? await Task.sleep(for: Self.layoutCaptureDelay)
            guard !Task.isCancelled, let self else { return }
            self.layoutCapture = nil
            self.onChange(self)
        }
    }

    /// Records a session change made inside a tab's own store.
    func storeSelected(_ sessionID: String, in tab: SessionTab) {
        guard !sessionID.isEmpty, tab.identity.session != sessionID else { return }
        let identity = SessionIdentity(server: tab.store?.serverID ?? tab.identity.server, session: sessionID)
        if let existing = self.tab(for: identity), existing !== tab {
            // Another tab already shows that session; keep one tab per session.
            close(tab)
            select(existing)
            return
        }
        tab.identity = identity
        onChange(self)
    }

    /// Detaches every session when the window closes.
    func closeAll() {
        for tab in tabs { release(tab) }
    }

    private func release(_ tab: SessionTab) {
        tab.store?.detach()
        tab.store?.stop()
        tab.store = nil
        tab.finished = false
        tab.recordedStatus = .idle
    }
}
