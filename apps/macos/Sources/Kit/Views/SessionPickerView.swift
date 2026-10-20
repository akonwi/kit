import SwiftUI

extension EnvironmentValues {
    /// The app's servers, for views that reach beyond their own session's server.
    @Entry var serverDirectory: ServerDirectory? = nil
}

/// The palette's Open session: the sessions of every connected server, each
/// labeled with its server like the sidebar. Opening one binds its tab to
/// that server.
struct SessionPickerView: View {
    @Environment(\.mica) private var theme
    let servers: [ServerConnection]
    /// The session the palette was opened from.
    let current: SessionIdentity
    let close: () -> Void
    let open: (SessionIdentity) -> Void
    @State private var target: SessionIdentity?
    @State private var deleting = false

    /// The connected servers' sessions, keyed by tree key so equal session
    /// IDs on two servers stay distinct.
    static func entries(_ servers: [ServerConnection]) -> [SessionExcerpt] {
        servers.filter(\.status.isConnected).flatMap { server in
            server.catalog.sessions.map { session in
                var entry = session
                entry.id = SessionWindowModel.treeKey(SessionIdentity(server: server.name, session: session.id))
                return entry
            }
        }
    }

    var body: some View {
        let connected = servers.filter(\.status.isConnected)
        let catalogs = connected.map(\.catalog)
        let entries = Self.entries(servers)
        PaletteSheet(title: "Open session", height: PaletteMetrics.height) {
            SessionLauncherContent(sessions: entries, connecting: catalogs.contains(where: \.loading),
                connected: catalogs.contains(where: \.loaded), error: entries.isEmpty ? catalogs.lazy.compactMap(\.error).first : nil,
                open: { open(SessionWindowModel.identity(treeKey: $0.id)) },
                refresh: { Task { await refresh(catalogs) } },
                focusRequest: target == nil, compact: true, currentID: SessionWindowModel.treeKey(current),
                rename: { deleting = false; target = SessionWindowModel.identity(treeKey: $0.id) },
                delete: { deleting = true; target = SessionWindowModel.identity(treeKey: $0.id) },
                serverLabel: { SessionWindowModel.identity(treeKey: $0.id).serverLabel })
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
        .task { await refresh(catalogs) }
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

    private func refresh(_ catalogs: [SessionCatalog]) async {
        await withTaskGroup(of: Void.self) { group in
            for catalog in catalogs { group.addTask { await catalog.refresh() } }
        }
    }
}

extension SessionIdentity {
    /// The server's name when it isn't the app's own, to label the session.
    var serverLabel: String? { server == ServerClient.localID ? nil : server }
}
