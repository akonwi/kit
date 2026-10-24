import SwiftUI

extension EnvironmentValues {
    /// The app's servers, for views that reach beyond their own session's server.
    @Entry var serverDirectory: ServerDirectory? = nil
}

/// The palette's Open session: the sessions of every connected server as a
/// tree, each labeled with its server like the sidebar and marked with what
/// it is doing. Opening one binds its tab to that server.
struct SessionPickerView: View {
    @Environment(\.mica) private var theme
    let servers: [ServerConnection]
    /// The session the palette was opened from.
    let current: SessionIdentity
    let close: () -> Void
    let open: (SessionIdentity) -> Void
    @State private var target: SessionIdentity?
    @State private var deleting = false
    /// Each working or waiting session's status, by tree key, as its server
    /// last reported.
    @State private var activity: [String: SessionTabStatus] = [:]
    /// Bumped by Refresh. A newer refresh cancels the one in progress.
    @State private var refreshRequest = 0

    /// The connected servers' sessions, keyed and linked to their parents by
    /// tree key, so equal session IDs on two servers stay distinct.
    static func entries(_ servers: [ServerConnection]) -> [SessionExcerpt] {
        servers.filter(\.status.isConnected).flatMap { server in
            server.catalog.sessions.map { session in
                var entry = session
                entry.id = SessionWindowModel.treeKey(SessionIdentity(server: server.name, session: session.id))
                entry.parentSessionID = session.parentSessionID.map {
                    SessionWindowModel.treeKey(SessionIdentity(server: server.name, session: $0))
                }
                return entry
            }
        }
    }

    var body: some View {
        let connected = servers.filter(\.status.isConnected)
        let catalogs = connected.map(\.catalog)
        let entries = Self.entries(servers)
        PaletteSheet(title: "Open session", height: PaletteMetrics.height) {
            SessionPickerContent(sessions: entries, activity: activity, connecting: catalogs.contains(where: \.loading),
                connected: catalogs.contains(where: \.loaded), error: entries.isEmpty ? catalogs.lazy.compactMap(\.error).first : nil,
                currentID: SessionWindowModel.treeKey(current),
                open: { open(SessionWindowModel.identity(treeKey: $0.id)) },
                refresh: { refreshRequest += 1 },
                focusRequest: target == nil,
                rename: { deleting = false; target = SessionWindowModel.identity(treeKey: $0.id) },
                delete: { deleting = true; target = SessionWindowModel.identity(treeKey: $0.id) })
        }
        .onExitCommand(perform: close)
        .foregroundStyle(theme.text)
        .background(theme.surface)
        .modifier(SessionCatalogActions(client: targetServer?.client, refresh: { await targetServer?.catalog.refresh() },
                                        target: targetSession, deleting: $deleting))
        .onReceive(NotificationCenter.default.publisher(for: .kitSessionDeleted)) { notification in
            guard let identity = notification.object as? SessionIdentity else { return }
            servers.first { $0.name == identity.server }?.catalog.sessionDeleted(identity)
        }
        .task(id: refreshRequest) { await refresh(connected) }
    }

    private var targetServer: ServerConnection? {
        target.flatMap { target in servers.first { $0.name == target.server } }
    }

    /// The catalog entry the rename or delete sheet acts on, by its own ID.
    private var targetSession: Binding<SessionExcerpt?> {
        Binding {
            guard let target else { return nil }
            return targetServer?.catalog.sessions.first { $0.id == target.session }
        } set: { session in
            if session == nil { target = nil }
        }
    }

    /// Reloads the servers' catalogs and their sessions' activity.
    private func refresh(_ servers: [ServerConnection]) async {
        let catalogs = servers.map(\.catalog)
        let clients = servers.map { (server: $0.name, client: $0.client as any SessionActivityClient) }
        async let reported = Self.activity(clients)
        await withTaskGroup(of: Void.self) { group in
            for catalog in catalogs { group.addTask { await catalog.refresh() } }
        }
        let statuses = await reported
        guard !Task.isCancelled else { return }
        activity = statuses
    }

    /// Each server's working and waiting sessions, by tree key. A server
    /// that can't report its activity shows its sessions without status.
    static func activity(_ clients: [(server: String, client: any SessionActivityClient)]) async -> [String: SessionTabStatus] {
        await withTaskGroup(of: (String, [String: SessionTabStatus]).self) { group in
            for (server, client) in clients {
                group.addTask { (server, (try? await client.sessionActivity()) ?? [:]) }
            }
            var result: [String: SessionTabStatus] = [:]
            for await (server, statuses) in group {
                for (session, status) in statuses {
                    result[SessionWindowModel.treeKey(SessionIdentity(server: server, session: session))] = status
                }
            }
            return result
        }
    }
}

extension SessionIdentity {
    /// The server's name when it isn't the app's own, to label the session.
    var serverLabel: String? { server == ServerClient.localID ? nil : server }
}

/// The Open session list: a search field over the session tree, with rename
/// and delete for the highlighted session.
struct SessionPickerContent: View {
    let sessions: [SessionExcerpt]
    let activity: [String: SessionTabStatus]
    let connecting: Bool
    let connected: Bool
    let error: String?
    /// The tree key of the session the palette was opened from.
    let currentID: String
    let open: (SessionExcerpt) -> Void
    let refresh: () -> Void
    var focusRequest = true
    var rename: ((SessionExcerpt) -> Void)? = nil
    var delete: ((SessionExcerpt) -> Void)? = nil
    @Environment(\.mica) private var theme
    @State private var picker: SessionPicker
    @FocusState private var searchFocused: Bool

    init(sessions: [SessionExcerpt], activity: [String: SessionTabStatus], connecting: Bool, connected: Bool,
         error: String?, currentID: String, open: @escaping (SessionExcerpt) -> Void, refresh: @escaping () -> Void,
         focusRequest: Bool = true, rename: ((SessionExcerpt) -> Void)? = nil, delete: ((SessionExcerpt) -> Void)? = nil) {
        self.sessions = sessions
        self.activity = activity
        self.connecting = connecting
        self.connected = connected
        self.error = error
        self.currentID = currentID
        self.open = open
        self.refresh = refresh
        self.focusRequest = focusRequest
        self.rename = rename
        self.delete = delete
        _picker = State(initialValue: SessionPicker(highlightedID: currentID))
    }

    private var rows: [SessionPicker.Row] { picker.rows(sessions, activity: activity) }

    var body: some View {
        let rows = rows
        VStack(alignment: .leading, spacing: 26) {
            VStack(spacing: 0) {
                searchField
                HStack {
                    Text("Recent sessions").font(.kit(size: 12, weight: .medium))
                    Spacer()
                    if connecting { KitSpinner() }
                    else {
                        Button(action: refresh) { IconView(.arrowClockwise) }
                            .buttonStyle(.plain).help("Refresh sessions").accessibilityLabel("Refresh sessions")
                    }
                }.foregroundStyle(theme.muted).padding(.top, 24).padding(.bottom, 10)
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(spacing: 0) {
                            ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                                SessionPickerRowView(row: row, rails: SessionTreeRails(depth: row.depth, next: rows.indices.contains(index + 1) ? rows[index + 1].depth : nil),
                                                     highlighted: picker.highlightedID == row.id, current: row.id == currentID,
                                                     open: { open(row.session) }, toggle: { picker.toggle(row.id) },
                                                     rename: rename, delete: delete)
                                    .id(row.id)
                            }
                            if rows.isEmpty { emptyState.padding(.vertical, 42) }
                        }
                    }
                    .onChange(of: picker.highlightedID, initial: true) {
                        if let id = picker.highlightedID { proxy.scrollTo(id) }
                    }
                    .onChange(of: rows.map(\.id), initial: true) {
                        let rows = self.rows
                        // Before the catalog loads, keep the highlight on the current session.
                        guard !rows.isEmpty else { return }
                        picker.reveal(in: rows, sessions: sessions)
                        if let id = picker.highlightedID { proxy.scrollTo(id) }
                    }
                }.frame(maxHeight: .infinity)
            }
            if rename != nil || delete != nil {
                let selected = selectedSession(in: rows)
                // SwiftUI can keep a keyboard shortcut's action from an
                // earlier render, so each highlighted session gets its own
                // buttons, whose actions name it.
                actions(selected).id(selected?.id)
            }
            if let error, !rows.isEmpty {
                HStack(spacing: 12) {
                    Label("Connection unavailable", icon: .warningCircle)
                        .help(error)
                    Spacer()
                    Button("Retry connection", action: refresh)
                }.font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
        }
        .padding(20).frame(maxWidth: .infinity, maxHeight: .infinity)
        .task(id: focusRequest) {
            guard focusRequest else { searchFocused = false; return }
            // Let the outgoing palette field leave the responder chain before
            // assigning focus to the newly mounted session search field.
            do { try await Task.sleep(for: .milliseconds(100)) } catch { return }
            searchFocused = true
        }
        // Keys read the rows afresh, since several can arrive before the
        // list is drawn again.
        .onKeyPress(.pageDown) { picker.move(5, in: self.rows); return .handled }
        .onKeyPress(.pageUp) { picker.move(-5, in: self.rows); return .handled }
        .onKeyPress(.downArrow) { picker.move(1, in: self.rows); return .handled }
        .onKeyPress(.upArrow) { picker.move(-1, in: self.rows); return .handled }
        .onKeyPress(.leftArrow) { picker.disclose(expand: false, in: self.rows) ? .handled : .ignored }
        .onKeyPress(.rightArrow) { picker.disclose(expand: true, in: self.rows) ? .handled : .ignored }
        .onKeyPress(.return) { openHighlighted(); return .handled }
    }

    /// The search text. A new query chooses its highlight in the same
    /// update, so Return and the shortcuts never act on a highlight left
    /// from the previous query.
    private var query: Binding<String> {
        Binding { picker.query } set: { query in
            guard query != picker.query else { return }
            var next = picker
            next.query = query
            next.queryChanged(rows: next.rows(sessions, activity: activity), sessions: sessions)
            picker = next
        }
    }

    private var searchField: some View {
        HStack(spacing: 10) {
            IconView(.magnifyingGlass, size: 16).foregroundStyle(theme.muted)
            TextField("Search sessions or directories", text: query)
                .textFieldStyle(.plain).focused($searchFocused)
                .onSubmit { openHighlighted() }
            if !picker.query.isEmpty {
                Button { query.wrappedValue = "" } label: { IconView(.xCircle) }
                    .buttonStyle(.plain).foregroundStyle(theme.muted).help("Clear search").accessibilityLabel("Clear search")
            }
        }
        .font(.kit(size: 14)).padding(14)
        .overlay(alignment: .bottom) { Rectangle().fill(searchFocused ? theme.accent : theme.border).frame(height: 1) }
    }

    private func actions(_ selectedSession: SessionExcerpt?) -> some View {
        HStack {
            if let rename {
                Button { if let selectedSession { rename(selectedSession) } } label: {
                    HStack(spacing: 8) {
                        Text("Rename…")
                        Text("⌘R").foregroundStyle(theme.muted)
                    }
                }
                    .keyboardShortcut("r", modifiers: .command)
            }
            if let delete {
                Button(role: .destructive) { if let selectedSession { delete(selectedSession) } } label: {
                    HStack(spacing: 8) {
                        Text(selectedSession?.isTemporary == true ? "Dispose…" : "Delete…")
                        Text("⌘⌫").foregroundStyle(theme.muted)
                    }
                }
                    .keyboardShortcut(KeyEquivalent("\u{7f}"), modifiers: .command)
            }
            Spacer()
        }
        .font(.kit(size: 12))
        .disabled(selectedSession == nil || !focusRequest)
    }

    private func selectedSession(in rows: [SessionPicker.Row]) -> SessionExcerpt? {
        picker.highlighted(in: rows)?.session
    }

    private func openHighlighted() {
        guard focusRequest, let session = selectedSession(in: rows) else { return }
        open(session)
    }

    @ViewBuilder private var emptyState: some View {
        VStack(spacing: 10) {
            if let error {
                Text("Couldn’t connect to Kit").font(.kit(size: 15, weight: .medium))
                Text(error).font(.kit(size: 13)).foregroundStyle(theme.muted).multilineTextAlignment(.center)
                Button("Retry connection", action: refresh)
            } else if connecting || !connected {
                Text("Loading sessions…").foregroundStyle(theme.muted)
            } else if picker.searching {
                Text("No matching sessions").font(.kit(size: 15, weight: .medium))
                Text("Try another name or folder.").foregroundStyle(theme.muted)
            } else {
                Text("No sessions yet").font(.kit(size: 15, weight: .medium))
            }
        }.font(.kit(size: 13)).frame(maxWidth: .infinity)
    }
}

/// One session in the Open session tree: its status, title, server, folder,
/// children, and age, indented under its parent with the sidebar's rails.
struct SessionPickerRowView: View {
    @Environment(\.mica) private var theme
    let row: SessionPicker.Row
    let rails: SessionTreeRails
    let highlighted: Bool
    let current: Bool
    let open: () -> Void
    let toggle: () -> Void
    let rename: ((SessionExcerpt) -> Void)?
    let delete: ((SessionExcerpt) -> Void)?
    @State private var hovering = false

    static let folderWidth: CGFloat = 96
    static let disclosureWidth: CGFloat = 52
    static let ageWidth: CGFloat = 76

    private var session: SessionExcerpt { row.session }
    private var path: String { session.cwd ?? session.workspace }

    var body: some View {
        HStack(spacing: 0) {
            ForEach(0..<rails.level, id: \.self) { _ in
                Color.clear.frame(width: SessionTreeRails.indent)
            }
            Button(action: open) { label }
                .buttonStyle(.plain)
                .onHover { hovering = $0 }
                .help(WorkspaceLocation.homeRelative(path))
                .contextMenu { SessionCatalogMenu(session: session, rename: rename, delete: delete) }
                .accessibilityLabel("\(session.title), \(path)")
                .accessibilityValue(row.status.label ?? "")
                .accessibilityAddTraits(highlighted ? .isSelected : [])
                .modifier(DisclosureAction(row: row, toggle: toggle))
        }
        .padding(.bottom, rails.gap)
        .overlay(alignment: .topLeading) {
            ForEach(1..<(rails.level + 1), id: \.self) { level in
                Rectangle().fill(theme.border)
                    .frame(width: 1, height: SessionTreeRails.rowHeight + (level <= rails.continuing ? rails.gap : 0))
                    .offset(x: CGFloat(level) * SessionTreeRails.indent - 1)
            }
        }
    }

    private var label: some View {
        HStack(spacing: 10) {
            SessionTabStatusMarker(status: row.status, finished: false).frame(width: 14)
            HStack(spacing: 8) {
                Text(session.title).lineLimit(1).truncationMode(.tail)
                    .foregroundStyle(row.context ? theme.muted : theme.text)
                // A family shares its server, so only its top row names it.
                if row.depth == 0, let server = SessionWindowModel.identity(treeKey: row.id).serverLabel {
                    ServerLabel(name: server)
                }
                if session.isTemporary == true {
                    Text("Temporary").font(.kit(size: 11)).foregroundStyle(theme.muted)
                }
            }
            .font(.kit(size: 13))
            .frame(maxWidth: .infinity, alignment: .leading)
            Text(session.workspace).font(.kit(size: 11, design: .monospaced)).foregroundStyle(theme.muted)
                .lineLimit(1).truncationMode(.middle)
                .frame(width: Self.folderWidth, alignment: .trailing)
            disclosure.frame(width: Self.disclosureWidth, alignment: .trailing)
            Text(SessionLauncher.timestamp(session.lastActivity ?? session.date).map { SessionLauncher.age(of: $0) } ?? "")
                .font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                .frame(width: Self.ageWidth, alignment: .trailing)
            IconView(current ? .check : .arrowRight, size: 12).foregroundStyle(theme.muted)
        }
        .padding(.leading, 8).padding(.trailing, 10)
        .frame(height: SessionTreeRails.rowHeight)
        .background(background, in: RoundedRectangle(cornerRadius: 3))
        .contentShape(Rectangle())
    }

    /// A parent's child count and caret. A collapsed parent also shows the
    /// most urgent status among the sessions it hides.
    @ViewBuilder private var disclosure: some View {
        if let children = row.children, children > 0 {
            HStack(spacing: 4) {
                if let hidden = row.hidden {
                    SessionTabStatusMarker(status: hidden, finished: false).frame(width: 12)
                        .padding(.trailing, 2)
                }
                Button(action: toggle) {
                    HStack(spacing: 4) {
                        Text("\(children)").font(.kit(size: 11, design: .monospaced))
                        IconView(row.collapsed ? .caretRight : .caretDown, size: 10)
                    }
                    .foregroundStyle(theme.muted)
                    .frame(height: 18).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help(row.collapsed ? "Show sessions from this one" : "Hide sessions from this one")
                .accessibilityHidden(true)
            }
        }
    }

    private var background: Color {
        highlighted ? theme.accent.opacity(0.14) : hovering ? theme.hover : .clear
    }

    /// Lets assistive technology expand or collapse a family.
    private struct DisclosureAction: ViewModifier {
        let row: SessionPicker.Row
        let toggle: () -> Void

        func body(content: Content) -> some View {
            if let children = row.children, children > 0 {
                content.accessibilityAction(named: row.collapsed ? "Expand" : "Collapse", toggle)
            } else {
                content
            }
        }
    }
}
