import AppKit
import SwiftUI
import Testing
@testable import Kit

/// A server's sessions, renamed, deleted, and disposed of in memory.
private actor TabMenuClient: SessionNamingClient, SessionDeletionClient, SessionDisposalClient {
    nonisolated let serverID: String
    nonisolated let isDemo = false
    private var listed: [SessionExcerpt]
    private let busy: Bool
    private(set) var renamed: [String: String] = [:]
    private(set) var deleted: [String] = []
    private(set) var disposed: [String] = []

    init(serverID: String, sessions: [SessionExcerpt], busy: Bool = false) {
        self.serverID = serverID
        listed = sessions
        self.busy = busy
    }

    func sessions() async throws -> [SessionExcerpt] { listed }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        guard let session = listed.first(where: { $0.id == id }) else { throw ClientError.http(404) }
        return session
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func renameSession(_ id: String, name: String) async throws -> SessionExcerpt {
        guard let index = listed.firstIndex(where: { $0.id == id }) else { throw ClientError.http(404) }
        renamed[id] = name
        listed[index].title = name
        return listed[index]
    }
    func deleteSession(_ id: String) async throws {
        if busy { throw ClientError.http(409) }
        deleted.append(id)
        listed.removeAll { $0.id == id }
    }
    func disposeSession(_ id: String) async throws {
        disposed.append(id)
        listed.removeAll { $0.id == id }
    }
}

/// A window whose session tabs are on a connected server, and possibly on
/// another server that can't be reached.
@MainActor private struct TabMenuWindow {
    let model: SessionWindowModel
    let client: TabMenuClient
    let servers: ServerDirectory

    static let local = ServerClient.localID

    static func session(_ id: String, parent: String? = nil, parentName: String? = nil,
                        temporary: Bool = false) -> SessionExcerpt {
        var session = SessionExcerpt(id: id, title: id.uppercased(), parentSessionID: parent, sourceTitle: "", model: "m",
                                     thinking: "high", workspace: "", date: "", messages: [])
        session.parentSessionName = parentName
        if temporary { session.isTemporary = true }
        return session
    }

    static func make(catalog: [SessionExcerpt], tabs: [SessionIdentity], selected: SessionIdentity,
                     busy: Bool = false, servers existing: ServerDirectory? = nil,
                     client existingClient: TabMenuClient? = nil) async throws -> TabMenuWindow {
        let client = existingClient ?? TabMenuClient(serverID: local, sessions: catalog, busy: busy)
        let servers = existing ?? directory(client)
        if existing == nil {
            for connection in servers.connections { connection.start() }
            try await eventually {
                servers.local.status.isConnected && servers.local.catalog.loaded
                    && servers.connection(named: "vps")?.status.sessionIssue != nil
            }
        }
        return TabMenuWindow(model: SessionWindowModel(id: UUID(), servers: servers, sessions: tabs, selected: selected),
                             client: client, servers: servers)
    }

    /// This Mac's server, which `client` lists, and a saved server that can't be reached.
    private static func directory(_ client: TabMenuClient) -> ServerDirectory {
        let saved = [SavedServer(name: "vps", kind: .home, address: "/tmp/vps")]
        return ServerDirectory(home: URL(fileURLWithPath: "/tmp/kit-home"), saved: { saved }) { name, connector in
            let schedule = ServerConnection.Schedule(firstRetry: .seconds(60), maximumRetry: .seconds(60), check: .seconds(60))
            let server = ServerClient(serverID: name, connector: connector)
            if name == local {
                let handshake: ServerConnection.Handshake = { Self.identity }
                return ServerConnection(client: server, catalogClient: client, schedule: schedule, handshake: handshake)
            }
            let handshake: ServerConnection.Handshake = { throw ClientError.noDaemon }
            return ServerConnection(client: server, schedule: schedule, handshake: handshake)
        }
    }

    nonisolated static let identity = ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: "i",
                                                     kitVersion: "0.48.0", protocolVersion: kitWireVersion,
                                                     machine: String(repeating: "b", count: 64), home: "/Users/kit")

    static func id(_ session: String, server: String = local) -> SessionIdentity {
        SessionIdentity(server: server, session: session)
    }
}

/// A rendered tab strip, right-clicked the way a person would.
@MainActor private final class RenderedStrip {
    let host: NSView
    let window: NSWindow

    init(_ view: some View, size: NSSize) async throws {
        _ = NSApplication.shared
        let host = NSHostingView(rootView: view.frame(width: size.width, height: size.height, alignment: .topLeading)
            .environment(\.mica, ThemeConfiguration.decode("").theme(dark: true)))
        host.sizingOptions = []
        window = NSWindow(testContentRect: NSRect(origin: .zero, size: size))
        window.contentView = host
        window.orderFront(nil)
        self.host = host
        for _ in 0..<10 { host.layoutSubtreeIfNeeded(); try await Task.sleep(for: .milliseconds(20)) }
    }

    /// The context menu a right-click at `point`, from the top left, opens.
    func menu(at point: CGPoint) throws -> NSMenu {
        let location = NSPoint(x: point.x, y: window.frame.height - point.y)
        let event = try #require(NSEvent.mouseEvent(with: .rightMouseDown, location: location, modifierFlags: [], timestamp: 0,
                                                    windowNumber: window.windowNumber, context: nil, eventNumber: 0,
                                                    clickCount: 1, pressure: 1))
        return try #require(host.menu(for: event))
    }

    func close() { window.close() }
}

/// Each item's title, with whether it is enabled; "—" for a separator.
@MainActor private func entries(_ menu: NSMenu) -> [String] {
    menu.items.map { $0.isSeparatorItem ? "—" : $0.isEnabled ? $0.title : "\($0.title) (disabled)" }
}

@MainActor private func choose(_ title: String, in menu: NSMenu) throws {
    let index = try #require(menu.items.firstIndex { $0.title == title })
    menu.performActionForItem(at: index)
}

/// Side rows: the header row, then 32 pt rows 8 pt apart between families.
private func sideRow(_ index: Int) -> CGPoint {
    let top: CGFloat = 44 + 8, pitch: CGFloat = 32 + 8
    return CGPoint(x: 100, y: top + CGFloat(index) * pitch + 16)
}
/// Top tabs: after the traffic lights' space, 220 pt tabs 9 pt apart.
private func topTab(_ index: Int) -> CGPoint {
    let leading: CGFloat = 16 + 68 + 6, pitch: CGFloat = 220 + 9
    return CGPoint(x: leading + CGFloat(index) * pitch + 110, y: 22)
}

@MainActor struct SessionTabMenuTests {
    private func sidebar(_ model: SessionWindowModel) async throws -> RenderedStrip {
        try await RenderedStrip(SessionSidebar(model: model, placement: .constant(.side)), size: NSSize(width: 248, height: 400))
    }

    private func topTabs(_ model: SessionWindowModel) async throws -> RenderedStrip {
        try await RenderedStrip(SessionTopTabs(model: model, placement: .constant(.top)), size: NSSize(width: 900, height: 44))
    }

    @Test func sideAndTopTabsOfferRenameAndCloseThenDeleteApart() async throws {
        let window = try await TabMenuWindow.make(catalog: [TabMenuWindow.session("a"), TabMenuWindow.session("b")],
                                                  tabs: [TabMenuWindow.id("a"), TabMenuWindow.id("b")],
                                                  selected: TabMenuWindow.id("a"))
        let side = try await sidebar(window.model)
        defer { side.close() }
        #expect(entries(try side.menu(at: sideRow(0))) == ["Rename…", "Close Tab", "—", "Delete…"])
        #expect(entries(try side.menu(at: sideRow(1))) == ["Rename…", "Close Tab", "—", "Delete…"])
        let top = try await topTabs(window.model)
        defer { top.close() }
        #expect(entries(try top.menu(at: topTab(0))) == ["Rename…", "Close Tab", "—", "Delete…"])
        #expect(entries(try top.menu(at: topTab(1))) == ["Rename…", "Close Tab", "—", "Delete…"])
    }

    @Test func aLauncherTabOnlyCloses() async throws {
        let window = try await TabMenuWindow.make(catalog: [TabMenuWindow.session("a")], tabs: [TabMenuWindow.id("a")],
                                                  selected: TabMenuWindow.id("a"))
        window.model.openLauncher()
        let top = try await topTabs(window.model)
        defer { top.close() }
        #expect(entries(try top.menu(at: topTab(1))) == ["Close Tab"])
    }

    @Test func aClosedParentOpensItsTabOrRenamesOrDeletesIt() async throws {
        let window = try await TabMenuWindow.make(catalog: [TabMenuWindow.session("p"), TabMenuWindow.session("c", parent: "p")],
                                                  tabs: [TabMenuWindow.id("c")], selected: TabMenuWindow.id("c"))
        #expect(window.model.rows.map(\.id) == ["p", "c"])
        let side = try await sidebar(window.model)
        defer { side.close() }
        let menu = try side.menu(at: sideRow(0))
        #expect(entries(menu) == ["Open Tab", "—", "Rename…", "Delete…"])
        try choose("Delete…", in: menu)
        #expect(window.model.catalogAction == SessionCatalogAction(identity: TabMenuWindow.id("p"), kind: .delete))
        #expect(window.model.catalogTarget(TabMenuWindow.id("p"))?.title == "P")
    }

    @Test func deletingATemporarySessionDisposesOfIt() async throws {
        let window = try await TabMenuWindow.make(catalog: [TabMenuWindow.session("a"), TabMenuWindow.session("t", temporary: true)],
                                                  tabs: [TabMenuWindow.id("a"), TabMenuWindow.id("t")],
                                                  selected: TabMenuWindow.id("a"))
        let side = try await sidebar(window.model)
        defer { side.close() }
        let menu = try side.menu(at: sideRow(1))
        #expect(entries(menu) == ["Rename…", "Close Tab", "—", "Delete…"])
        try choose("Delete…", in: menu)
        let target = try #require(window.model.catalogTarget(TabMenuWindow.id("t")))
        #expect(SessionCatalogActionCopy(session: target, deleting: true) == SessionCatalogActionCopy(
            title: "Delete session?",
            message: "“T” will be removed from daemon memory and any active work will be cancelled. This cannot be undone.",
            confirm: "Delete session"))
        try await SessionCatalogMutation.perform(session: target, deleting: true, name: "", client: window.client)
        #expect(await window.client.disposed == ["t"])
        #expect(await window.client.deleted.isEmpty)
    }

    @Test func aTabOnAServerThatCantBeReachedSaysWhyItCantBeRenamedOrDeleted() async throws {
        let window = try await TabMenuWindow.make(catalog: [TabMenuWindow.session("a")],
                                                  tabs: [TabMenuWindow.id("a"), TabMenuWindow.id("r", server: "vps")],
                                                  selected: TabMenuWindow.id("a"))
        let side = try await sidebar(window.model)
        defer { side.close() }
        let menu = try side.menu(at: sideRow(1))
        #expect(entries(menu) == ["Rename… (disabled)", "Close Tab", "—", "Delete… (disabled)", "Can’t connect (disabled)"])
        let top = try await topTabs(window.model)
        defer { top.close() }
        #expect(entries(try top.menu(at: topTab(1)))
            == ["Rename… (disabled)", "Close Tab", "—", "Delete… (disabled)", "Can’t connect (disabled)"])
        window.model.requestCatalogAction(.rename, for: TabMenuWindow.id("r", server: "vps"))
        #expect(window.model.catalogAction == nil)
    }

    @Test func renamingAnotherTabRenamesThatSessionAndKeepsTheSelection() async throws {
        let window = try await TabMenuWindow.make(catalog: [TabMenuWindow.session("a"), TabMenuWindow.session("b")],
                                                  tabs: [TabMenuWindow.id("a"), TabMenuWindow.id("b")],
                                                  selected: TabMenuWindow.id("a"))
        let top = try await topTabs(window.model)
        defer { top.close() }
        try choose("Rename…", in: try top.menu(at: topTab(1)))
        #expect(window.model.catalogAction == SessionCatalogAction(identity: TabMenuWindow.id("b"), kind: .rename))
        #expect(window.model.selectedSession == TabMenuWindow.id("a"))
        let target = try #require(window.model.catalogTarget(TabMenuWindow.id("b")))
        #expect(SessionCatalogActionCopy(session: target, deleting: false).title == "Rename session")
        try await SessionCatalogMutation.perform(session: target, deleting: false, name: "  Renamed  ", client: window.client)
        #expect(await window.client.renamed == ["b": "Renamed"])
        await window.servers.local.catalog.refresh()
        #expect(window.model.tabs.map(\.title) == ["A", "Renamed"])
        #expect(window.model.selectedSession == TabMenuWindow.id("a"))
    }

    @Test func deletingTheSelectedTabConfirmsThenClosesItEverywhereAndSelectsTheNext() async throws {
        let catalog = ["a", "b", "c"].map { TabMenuWindow.session($0) }
        let window = try await TabMenuWindow.make(catalog: catalog, tabs: ["a", "b", "c"].map { TabMenuWindow.id($0) },
                                                  selected: TabMenuWindow.id("b"))
        // Another window shows the same session.
        let other = try await TabMenuWindow.make(catalog: catalog, tabs: [TabMenuWindow.id("a"), TabMenuWindow.id("b")],
                                                 selected: TabMenuWindow.id("a"), servers: window.servers, client: window.client)
        let side = try await sidebar(window.model)
        defer { side.close() }
        try choose("Delete…", in: try side.menu(at: sideRow(1)))
        #expect(window.model.catalogAction == SessionCatalogAction(identity: TabMenuWindow.id("b"), kind: .delete))
        let target = try #require(window.model.catalogTarget(TabMenuWindow.id("b")))
        #expect(SessionCatalogActionCopy(session: target, deleting: true) == SessionCatalogActionCopy(
            title: "Delete session?", message: "“B” and its saved history will be permanently deleted. This cannot be undone.",
            confirm: "Delete session"))
        // Each window hears of the deletion, as their roots do.
        let deleted = TabMenuWindow.id("b")
        let observer = NotificationCenter.default.addObserver(forName: .kitSessionDeleted, object: nil, queue: nil) { note in
            guard let identity = note.object as? SessionIdentity, identity == deleted else { return }
            MainActor.assumeIsolated {
                window.model.sessionDeleted(identity, from: .side)
                other.model.sessionDeleted(identity, from: .top)
            }
        }
        defer { NotificationCenter.default.removeObserver(observer) }
        try await SessionCatalogMutation.perform(session: target, deleting: true, name: "", client: window.client)
        #expect(await window.client.deleted == ["b"])
        #expect(window.model.tabs.map(\.title) == ["A", "C"])
        #expect(window.model.selectedSession == TabMenuWindow.id("c"))
        #expect(window.model.catalogAction == nil)
        #expect(other.model.tabs.map(\.title) == ["A"])
        #expect(other.model.selectedSession == TabMenuWindow.id("a"))
        #expect(window.servers.local.catalog.sessions.map(\.id) == ["a", "c"])
    }

    @Test func aBusySessionStaysOpenAndSaysWhy() async throws {
        let window = try await TabMenuWindow.make(catalog: [TabMenuWindow.session("a"), TabMenuWindow.session("b")],
                                                  tabs: [TabMenuWindow.id("a"), TabMenuWindow.id("b")],
                                                  selected: TabMenuWindow.id("b"), busy: true)
        window.model.requestCatalogAction(.delete, for: TabMenuWindow.id("b"))
        let target = try #require(window.model.catalogTarget(TabMenuWindow.id("b")))
        do {
            try await SessionCatalogMutation.perform(session: target, deleting: true, name: "", client: window.client)
            Issue.record("Expected the server to refuse")
        } catch {
            #expect(SessionCatalogMutation.failure(error)
                == "This session is in use and cannot be deleted. Stop its active work before trying again.")
        }
        #expect(window.model.tabs.map(\.title) == ["A", "B"])
        #expect(window.model.selectedSession == TabMenuWindow.id("b"))
    }

    @Test func aDeletedParentsChildrenMoveToTheTopLevel() async throws {
        let window = try await TabMenuWindow.make(
            catalog: [TabMenuWindow.session("p"), TabMenuWindow.session("c", parent: "p", parentName: "P"), TabMenuWindow.session("d")],
            tabs: [TabMenuWindow.id("p"), TabMenuWindow.id("c"), TabMenuWindow.id("d")], selected: TabMenuWindow.id("d"))
        #expect(window.model.rows.map(\.id) == ["p", "c", "d"])
        window.model.sessionDeleted(TabMenuWindow.id("p"), from: .side)
        #expect(window.model.rows.map(\.id) == ["c", "d"])
        #expect(window.model.rows.map(\.depth) == [0, 0])
        #expect(window.model.selectedSession == TabMenuWindow.id("d"))
    }

    @Test func voiceOverCanRenameOrDeleteATabWhileTheyreAvailable() async throws {
        let window = try await TabMenuWindow.make(catalog: [TabMenuWindow.session("a"), TabMenuWindow.session("t", temporary: true)],
                                                  tabs: [TabMenuWindow.id("a"), TabMenuWindow.id("t"),
                                                         TabMenuWindow.id("r", server: "vps")],
                                                  selected: TabMenuWindow.id("a"))
        func actions(_ tab: SessionIdentity) throws -> [String] {
            let tab = try #require(window.model.tab(for: tab))
            return SessionTabMenu.accessibilityActions(window.model.menuItems(for: tab)).map(\.name)
        }
        #expect(try actions(TabMenuWindow.id("a")) == ["Rename", "Delete"])
        #expect(try actions(TabMenuWindow.id("t")) == ["Rename", "Delete"])
        #expect(try actions(TabMenuWindow.id("r", server: "vps")) == [])
        window.model.openLauncher()
        #expect(SessionTabMenu.accessibilityActions(window.model.menuItems(for: window.model.selectedTab)).isEmpty)
    }
}
