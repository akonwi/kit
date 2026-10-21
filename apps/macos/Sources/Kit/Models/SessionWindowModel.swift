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
    /// The title the tab last showed, until its server describes the session.
    fileprivate var savedTitle: String?
    /// The route of its server the tab's store attached by.
    fileprivate var attachedRoute = 0
    /// The app's servers, whose catalogs describe tabs not yet opened.
    private weak var servers: ServerDirectory?

    init(identity: SessionIdentity, servers: ServerDirectory? = nil) {
        self.identity = identity
        self.servers = servers
    }

    var isLauncher: Bool { identity.session.isEmpty }
    /// The connection to the server the tab is bound to, while that server is known.
    var connection: ServerConnection? { servers?.connection(named: identity.server) }
    /// The server's name when it isn't this app's own, to label the tab.
    var serverLabel: String? { isLauncher ? nil : identity.serverLabel }
    /// What the tab's server needs from the user while it can't serve the tab.
    var serverIssue: String? {
        guard !isLauncher, servers != nil else { return nil }
        guard let connection else { return "Server isn’t saved" }
        switch connection.status {
        case .unreachable: return "Can’t connect"
        case .incompatible(let mismatch): return mismatch.serverIsOlder ? "Server needs a newer Kit" : "Needs a newer Kit for Mac"
        case .needsPairing: return "Pair again"
        case .connecting, .connected, .reconnecting: return nil
        }
    }
    /// Whether the tab's server missed a check and is being tried again.
    var reconnecting: Bool {
        if case .reconnecting = connection?.status { true } else { false }
    }
    /// The live session once opened; until then, its catalog entry.
    var session: SessionExcerpt? {
        store?.selected ?? connection?.catalog.sessions.first { $0.id == identity.session }
    }
    var title: String { session?.title ?? savedTitle ?? (isLauncher ? "New session" : identity.session) }
    var status: SessionTabStatus { store?.tabStatus ?? .idle }
    /// A turn finished while the user was elsewhere; cleared when the tab is
    /// next shown in a focused window.
    fileprivate(set) var finished = false
    /// The last status the window recorded, to detect a finished turn.
    fileprivate var recordedStatus: SessionTabStatus = .idle
    /// Whether the tab's marker calls for attention; idle tabs recede.
    var needsAttention: Bool { status != .idle || finished || serverIssue != nil }
    var statusLabel: String? { serverIssue ?? status.label ?? (finished ? "Finished" : nil) }
    /// The session's working directory, which names its project on a top tab.
    var directory: String { session?.cwd ?? session?.workspace ?? "" }
    /// The session this one came from, as a fork or an agent's work.
    var parentSessionID: String? { isLauncher ? nil : session?.parentSessionID }
}

/// A row of the sidebar's session tree.
typealias SessionTreeRow = SessionTree<SessionTab>.Row

/// A window's session tabs. Each tab is bound to one server; the app's
/// servers are shared by every window.
@MainActor @Observable
final class SessionWindowModel: Identifiable {
    let id: UUID
    let servers: ServerDirectory
    private(set) var tabs: [SessionTab]
    private(set) var selectedTabID: UUID
    /// Sessions whose children the sidebar hides.
    private(set) var collapsed: Set<String>
    /// The launcher tab whose new session form is open over the window.
    var creatingIn: SessionTab?
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

    static let defaultServer = ServerClient.localID

    init(id: UUID, servers: ServerDirectory = .inert(), sessions: [SessionIdentity] = [], selected: SessionIdentity? = nil,
         layouts: [SessionIdentity: TileArrangement] = [:], collapsed: Set<String> = [],
         titles: [SessionIdentity: String] = [:]) {
        self.id = id
        self.servers = servers
        self.collapsed = collapsed
        let tabs = sessions.isEmpty
            ? [SessionTab(identity: Self.launcherIdentity(servers), servers: servers)]
            : sessions.map { identity in
                let tab = SessionTab(identity: identity, servers: servers)
                tab.savedArrangement = layouts[identity]
                tab.savedTitle = titles[identity]
                return tab
            }
        self.tabs = tabs
        selectedTabID = (selected.flatMap { identity in tabs.first { $0.identity == identity } } ?? tabs[0]).id
    }

    /// A new launcher, on the server last chosen in a launcher.
    static func launcherIdentity(_ servers: ServerDirectory) -> SessionIdentity {
        SessionIdentity(server: servers.launcherDefault, session: "")
    }

    var launcherIdentity: SessionIdentity { Self.launcherIdentity(servers) }

    /// A session's key in the sidebar tree and in collapsed families. Keys
    /// of the local server's sessions are their IDs, as earlier builds
    /// saved; other servers' are prefixed with the server's name, which
    /// session IDs can't contain.
    static func treeKey(_ identity: SessionIdentity) -> String {
        identity.server == defaultServer ? identity.session : identity.server + "/" + identity.session
    }

    /// The session a tree key names.
    static func identity(treeKey key: String) -> SessionIdentity {
        guard let slash = key.firstIndex(of: "/") else { return SessionIdentity(server: defaultServer, session: key) }
        return SessionIdentity(server: String(key[..<slash]), session: String(key[key.index(after: slash)...]))
    }

    /// The catalog entry for a session, from its server's catalog.
    func catalogEntry(_ identity: SessionIdentity) -> SessionExcerpt? {
        servers.connection(named: identity.server)?.catalog.sessions.first { $0.id == identity.session }
    }

    var selectedTab: SessionTab { tabs.first { $0.id == selectedTabID } ?? tabs[0] }
    var selectedIndex: Int { tabs.firstIndex { $0.id == selectedTabID } ?? 0 }

    /// The sidebar's session tree, with collapsed families folded.
    var rows: [SessionTreeRow] { tree(collapsed: collapsed) }
    /// Every tab in family order, as the top tabs show them.
    var familyOrder: [SessionTab] { tree(collapsed: []).compactMap(\.tab) }

    /// The tabs next/previous follow: the sidebar's visible tab rows, or
    /// every top tab.
    func navigableTabs(_ placement: SessionTabPlacement) -> [SessionTab] {
        placement == .side ? rows.compactMap(\.tab) : familyOrder
    }

    /// What ⌘1–⌘9 reach: each visible sidebar row, including closed
    /// parents, or every top tab.
    func numberedRows(_ placement: SessionTabPlacement) -> [SessionTreeRow.Content] {
        placement == .side ? rows.map(\.content) : familyOrder.map { .tab($0) }
    }

    /// The parent's title for a tab whose session came from another.
    func parentTitle(of tab: SessionTab) -> String? {
        guard let parent = tab.parentSessionID else { return nil }
        let identity = SessionIdentity(server: tab.identity.server, session: parent)
        return self.tab(for: identity)?.title
            ?? catalogEntry(identity)?.title
            ?? tab.session?.parentSessionName
    }

    /// A session's children and forks share its server, so the tree links
    /// each tab only to sessions on its own server.
    private func tree(collapsed: Set<String>) -> [SessionTreeRow] {
        SessionTree.rows(tabs, session: { $0.isLauncher ? nil : Self.treeKey($0.identity) },
                         parent: { tab in
                             tab.parentSessionID.map { Self.treeKey(SessionIdentity(server: tab.identity.server, session: $0)) }
                         },
                         catalog: { key in
                             let identity = Self.identity(treeKey: key)
                             if let session = self.catalogEntry(identity) {
                                 let parent = session.parentSessionID.map { Self.treeKey(SessionIdentity(server: identity.server, session: $0)) }
                                 return .init(title: session.title, parent: parent)
                             }
                             // Outside the catalog, a child may still know its parent's name.
                             let name = self.tabs.lazy.compactMap { tab in
                                 tab.identity.server == identity.server && tab.parentSessionID == identity.session
                                     ? tab.session?.parentSessionName : nil
                             }.first
                             return name.map { .init(title: $0, parent: nil) }
                         },
                         collapsed: collapsed)
    }

    /// Shows or hides a session's children in the sidebar, by its tree key.
    func toggleCollapsed(_ key: String) {
        if collapsed.remove(key) == nil { collapsed.insert(key) }
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
    /// The status of each of `server`'s session tabs that is working or
    /// awaiting a response, for a launcher listing that server.
    func activity(on server: String) -> [String: SessionTabStatus] {
        var activity: [String: SessionTabStatus] = [:]
        for tab in tabs where !tab.isLauncher && tab.status != .idle && tab.identity.server == server {
            activity[tab.identity.session] = tab.status
        }
        return activity
    }

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

    /// The titles session tabs show, to name them while their server can't.
    var titles: [SessionIdentity: String] {
        var result: [SessionIdentity: String] = [:]
        for tab in tabs where !tab.isLauncher {
            if let title = tab.session?.title ?? tab.savedTitle { result[tab.identity] = title }
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

    /// Selects the tab at a numbered position. A closed parent there isn't
    /// a tab of this window, so its identity is returned for the caller to
    /// open, which may focus another window that has it.
    func select(index: Int, in placement: SessionTabPlacement) -> SessionIdentity? {
        let numbered = numberedRows(placement)
        guard numbered.indices.contains(index) else { return nil }
        switch numbered[index] {
        case .tab(let tab):
            select(tab)
            return nil
        case .closedParent(let key, _):
            return Self.identity(treeKey: key)
        }
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
        let tab = SessionTab(identity: identity, servers: servers)
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
            open(launcherIdentity)
        }
    }

    /// Points a launcher at another server's sessions. Later launchers open
    /// on that server too.
    func point(_ tab: SessionTab, at server: String) {
        guard tab.isLauncher, tabs.contains(where: { $0 === tab }), servers.connection(named: server) != nil else { return }
        servers.launcherServer = server
        guard tab.identity.server != server else { return }
        if creatingIn === tab { creatingIn = nil }
        tab.identity = SessionIdentity(server: server, session: "")
        onChange(self)
    }

    /// Opens a session made by the new session form: in the launcher tab that
    /// asked for it, or in a new tab when that launcher has gone.
    func showCreated(_ identity: SessionIdentity, from launcher: SessionTab) {
        if tabs.contains(where: { $0 === launcher }), launcher.isLauncher { show(identity, in: launcher) }
        else { open(identity) }
    }

    /// Points a tab at another session, as when the launcher or chooser picks one.
    func show(_ identity: SessionIdentity, in tab: SessionTab) {
        guard tabs.contains(where: { $0 === tab }), tab.identity != identity else { return }
        if let existing = self.tab(for: identity), existing !== tab {
            close(tab)
            select(existing)
            return
        }
        if tab.store?.selectedID != identity.session || tab.store?.serverID != identity.server { release(tab) }
        tab.identity = identity
        tab.savedArrangement = nil
        reveal(tab)
        pruneCollapsed()
        onChange(self)
    }

    /// Closes a tab, first asking if that would end commands running in its
    /// terminals.
    func requestClose(_ tab: SessionTab) {
        TerminalCloseConfirmation.confirm(tab.store?.busyTerminals ?? [], scope: .sessionTab, in: window) { [weak self, weak tab] in
            if let self, let tab { self.close(tab) }
        }
    }

    /// Terminals in this window whose commands are still running.
    var busyTerminals: [TerminalTab] { tabs.flatMap { $0.store?.busyTerminals ?? [] } }

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
        if tabs.isEmpty { tabs = [SessionTab(identity: launcherIdentity, servers: servers)] }
        if selectedTabID == tab.id {
            let next = order[(position + 1)...].first ?? order[..<position].last ?? tabs[0]
            selectedTabID = next.id
            reveal(next)
        }
        pruneCollapsed()
        onChange(self)
    }

    /// Creates the tab's store once its server is connected and keeps it
    /// attached, so background tabs keep streaming status. A store outlives
    /// its server's outages, keeping the tab's drafts and layout.
    @discardableResult
    func store(for tab: SessionTab) -> SessionStore? {
        guard let connection = tab.connection, connection.status.isConnected else { return tab.store }
        return store(for: tab, client: connection.client)
    }

    func store(for tab: SessionTab, client: any SessionClient) -> SessionStore? {
        guard !tab.isLauncher, tab.identity.server == client.serverID else { return nil }
        let route = servers.connection(named: client.serverID)?.route ?? 0
        if let store = tab.store, store.serverID == client.serverID {
            // A watch that gave up, as on a restart's transient errors,
            // resumes once the server is back. Streams opened before the
            // server was repointed still read the old home, so they reattach.
            if store.watchStopped || tab.attachedRoute != route {
                tab.attachedRoute = route
                store.attach()
            }
            return store
        }
        tab.attachedRoute = route
        let catalog = servers.connection(named: client.serverID)?.catalog.sessions ?? []
        let store = SessionStore(fixture: Fixture(sessions: catalog), client: client, sessionID: tab.identity.session)
        if let arrangement = tab.savedArrangement { store.ui.workspace.restore(arrangement) }
        tab.savedArrangement = nil
        tab.store = store
        tab.recordedStatus = store.tabStatus
        store.attach()
        observeStatus(of: tab, store: store)
        observeLayout(of: tab, store: store)
        return store
    }

    /// Attaches every session tab whose server is connected, so background
    /// tabs report their status, and resumes watches that gave up.
    func attachAll() {
        for tab in tabs { store(for: tab) }
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

    /// Moves the tabs of a server saved under a new name to that name. Each
    /// reattaches through the renamed server, keeping its layout and title.
    func serverRenamed(from name: String, to newName: String) {
        var changed = false
        for tab in tabs where tab.identity.server == name {
            if !tab.isLauncher {
                tab.savedArrangement = tab.store?.ui.workspace.arrangement ?? tab.savedArrangement
                tab.savedTitle = tab.session?.title ?? tab.savedTitle
                release(tab)
            }
            tab.identity = SessionIdentity(server: newName, session: tab.identity.session)
            changed = true
        }
        guard changed else { return }
        collapsed = Set(collapsed.map { key in
            let identity = Self.identity(treeKey: key)
            return identity.server == name ? Self.treeKey(SessionIdentity(server: newName, session: identity.session)) : key
        })
        onChange(self)
    }

    /// Closes the tabs of a server that is no longer saved; a launcher on it
    /// lists this Mac's sessions instead.
    func serverRemoved(_ name: String) {
        for tab in tabs where tab.identity.server == name {
            if tab.isLauncher {
                tab.identity = launcherIdentity
                onChange(self)
            } else {
                close(tab)
            }
        }
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
