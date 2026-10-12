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
    /// The window's session catalog, which describes tabs not yet opened.
    private weak var catalog: AppModel?

    init(identity: SessionIdentity, catalog: AppModel? = nil) {
        self.identity = identity
        self.catalog = catalog
    }

    var isLauncher: Bool { identity.session.isEmpty }
    /// The live session once opened; until then, its catalog entry.
    private var session: SessionExcerpt? {
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
    /// The session's working directory, for grouping tabs by project.
    var directory: String { session?.cwd ?? session?.workspace ?? "" }
}

/// A window's session tabs and the connection they share.
@MainActor @Observable
final class SessionWindowModel: Identifiable {
    let id: UUID
    let app = AppModel()
    private(set) var tabs: [SessionTab]
    private(set) var selectedTabID: UUID
    @ObservationIgnored weak var window: NSWindow?
    /// Keeps the window's traffic lights in the chrome row while it is open.
    @ObservationIgnored var trafficLights: TrafficLightPlacement?
    /// Called after tabs or selection change, to record restoration intent.
    @ObservationIgnored var onChange: (SessionWindowModel) -> Void = { _ in }

    static let defaultServer = "local-v2"

    init(id: UUID, sessions: [SessionIdentity] = [], selected: SessionIdentity? = nil) {
        self.id = id
        let app = app
        let tabs = sessions.isEmpty
            ? [SessionTab(identity: Self.launcherIdentity, catalog: app)]
            : sessions.map { SessionTab(identity: $0, catalog: app) }
        self.tabs = tabs
        selectedTabID = (selected.flatMap { identity in tabs.first { $0.identity == identity } } ?? tabs[0]).id
    }

    static var launcherIdentity: SessionIdentity { SessionIdentity(server: defaultServer, session: "") }

    var selectedTab: SessionTab { tabs.first { $0.id == selectedTabID } ?? tabs[0] }
    var selectedIndex: Int { tabs.firstIndex { $0.id == selectedTabID } ?? 0 }

    /// Tabs grouped by project, as the tab list shows them.
    var groups: [SessionTabGroup<SessionTab>] { SessionTabGroup.grouped(tabs, directory: \.directory) }
    /// Tabs in display order, which ⌘1–⌘9 and next/previous follow.
    var displayedTabs: [SessionTab] { groups.flatMap(\.tabs) }

    /// Session identities to restore, in tab order; launcher tabs are not intent.
    var sessions: [SessionIdentity] { tabs.filter { !$0.isLauncher }.map(\.identity) }
    var selectedSession: SessionIdentity? { selectedTab.isLauncher ? nil : selectedTab.identity }

    func tab(for identity: SessionIdentity) -> SessionTab? {
        tabs.first { !$0.isLauncher && $0.identity == identity }
    }

    func select(_ tab: SessionTab) {
        guard tabs.contains(where: { $0 === tab }), selectedTabID != tab.id else { return }
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
    func select(index: Int) {
        let displayed = displayedTabs
        guard displayed.indices.contains(index) else { return }
        select(displayed[index])
    }

    func selectAdjacent(_ offset: Int) {
        let displayed = displayedTabs
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
        onChange(self)
    }

    /// Closes a tab. Closing the last tab leaves a launcher in its place.
    func close(_ tab: SessionTab) {
        guard let index = tabs.firstIndex(where: { $0 === tab }) else { return }
        release(tab)
        tabs.remove(at: index)
        if tabs.isEmpty { tabs = [SessionTab(identity: Self.launcherIdentity, catalog: app)] }
        if selectedTabID == tab.id {
            selectedTabID = tabs[min(index, tabs.count - 1)].id
        }
        onChange(self)
    }

    /// Creates the tab's store on first display and keeps it attached, so
    /// background tabs keep streaming status.
    func store(for tab: SessionTab, client: any SessionClient) -> SessionStore? {
        guard !tab.isLauncher, tab.identity.server.isEmpty || tab.identity.server == client.serverID else { return nil }
        if let store = tab.store, store.serverID == client.serverID { return store }
        let store = SessionStore(fixture: Fixture(sessions: app.sessions), client: client, sessionID: tab.identity.session)
        tab.store = store
        tab.recordedStatus = store.tabStatus
        store.attach()
        observeStatus(of: tab, store: store)
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
