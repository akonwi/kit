import AppKit
import SwiftUI

/// One window: its session tabs, the selected session's bar, and that
/// session's content. Each tab is bound to one server, through the
/// connection every window shares.
struct SessionWindowRoot: View {
    @Binding var request: SessionWindowRequest
    let windows: SessionWindowRegistry
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var themeConfiguration = ""
    @AppStorage(SessionTabPlacement.storageKey) private var placement = SessionTabPlacement.side
    @Environment(\.installedThemes) private var installedThemes
    @Environment(\.openWindow) private var openWindow
    @State private var signIn: ProviderSignIn?
    /// Whether first run is showing. It stays until Continue, so signing in
    /// to another provider from the connected step keeps the window on it.
    @State private var firstRun = false

    var body: some View {
        let model = windows.model(for: request)
        let launcher = model.launcherServer
        let scheme = SystemAppearance.shared.resolve(appearance)
        let theme = ThemeConfiguration.decode(themeConfiguration).resolved(dark: scheme == .dark, installed: installedThemes)
        let tab = model.selectedTab
        Group {
            if firstRun, let signIn {
                FirstRunView(signIn: signIn) { firstRun = false }
            } else {
                workspace(model: model, tab: tab, theme: theme)
            }
        }
        .disabled(model.creatingIn != nil)
        .accessibilityHidden(model.creatingIn != nil)
        .overlay { newSessionForm(model: model, theme: theme) }
        .environment(\.mica, theme)
        .foregroundStyle(theme.text)
        .background(theme.frame)
        .tint(theme.accent)
        .preferredColorScheme(theme.dark ? .dark : .light)
        .ignoresSafeArea(.container, edges: .top)
        .frame(minWidth: 820, minHeight: 540)
        .environment(\.sessionNavigation, SessionNavigation(
            openSession: { identity in windows.open(identity, from: model) },
            sessionTitle: { identity in model.tab(for: identity)?.title ?? model.catalogEntry(identity)?.title },
            openLauncher: { model.openLauncher() }))
        .environment(\.serverDirectory, model.servers)
        .focusedSceneValue(\.sessionWindow, model)
        .navigationTitle(tab.title)
        .background {
            SessionWindowAttachment(registry: windows, model: model).frame(width: 0, height: 0)
        }
        .task(id: AttachmentKey(model: model)) { model.attachAll() }
        .onAppear {
            windows.restoreRemaining { openWindow(id: "session", value: $0) }
        }
        .task(id: launcher.status.identity?.id) { await checkProviders(launcher, reset: true) }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            Task { await checkProviders(launcher, reset: false) }
        }
    }

    /// The session tabs, the selected session's bar, and its content.
    @ViewBuilder private func workspace(model: SessionWindowModel, tab: SessionTab, theme: MicaTheme) -> some View {
        switch placement {
        case .side:
            HStack(spacing: 0) {
                SessionSidebar(model: model, placement: $placement)
                VStack(spacing: 0) {
                    SessionBar(tab: tab)
                    tile(model: model, tab: tab, theme: theme)
                }
            }
        case .top:
            VStack(spacing: 0) {
                SessionTopTabs(model: model, placement: $placement)
                if !tab.isLauncher {
                    SessionBar(tab: tab, showsTitle: false).padding(.horizontal, 6)
                }
                tile(model: model, tab: tab, theme: theme)
            }
        }
    }

    /// Shows first run while the launcher's server has no provider
    /// connected. A server that can't list providers keeps the workspace.
    private func checkProviders(_ server: ServerConnection, reset: Bool) async {
        guard server.status.isConnected else { signIn = nil; firstRun = false; return }
        if reset || signIn == nil { signIn = ProviderSignIn(client: server.client) }
        guard let signIn, signIn.step == .list else { return }
        await signIn.load()
        if signIn.loadError == nil && !signIn.anyConnected { firstRun = true }
    }

    /// The new session form, over the whole window while a launcher asks for it.
    @ViewBuilder private func newSessionForm(model: SessionWindowModel, theme: MicaTheme) -> some View {
        let server = model.launcherServer
        if let launcher = model.creatingIn, server.status.isConnected {
            let client = server.client
            NewSessionOverlay(client: client, suggestedModel: server.catalog.sessions.first?.model,
                              close: { model.creatingIn = nil }) { session in
                server.catalog.recordCreated(session)
                model.showCreated(SessionIdentity(server: client.serverID, session: session.id), from: launcher)
            }
            .environment(\.mica, theme)
        }
    }

    /// The selected tab's content, inset from the window frame. An open
    /// session draws its own tiles; anything else sits in a single tile.
    private func tile(model: SessionWindowModel, tab: SessionTab, theme: MicaTheme) -> some View {
        // A session draws its own tiles only while its server can show it.
        let tiled = tab.store?.selected != nil && tab.serverIssue == nil
        return VStack(spacing: 0) {
            if let error = windows.restoration.error {
                HStack {
                    Text(error).textSelection(.enabled)
                    Button("Retry saving") { windows.restoration.save() }
                }.font(.kit(size: 12)).padding(12)
            }
            SessionTabContent(model: model, tab: tab)
                .id(tab.id)
        }
        .background(tiled ? .clear : theme.surface)
        .clipShape(RoundedRectangle(cornerRadius: tiled ? 0 : 4))
        .overlay { if !tiled { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) } }
        .padding([.trailing, .bottom], tileGap)
        .padding(.leading, placement == .top ? tileGap : 0)
    }
}

/// A tab's content: the launcher, its session, or what its server needs.
private struct SessionTabContent: View {
    let model: SessionWindowModel
    let tab: SessionTab

    var body: some View {
        if let connection = tab.connection {
            switch connection.status {
            case .connected:
                connected(connection)
            case .connecting where tab.isLauncher || tab.store == nil:
                KitSpinner(connection.isLocal ? "Connecting to Kit" : "Connecting to \(connection.name)")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            case .connecting:
                connected(connection)
            case .unreachable, .incompatible:
                ServerStateView(connection: connection)
            }
        } else {
            UnknownServerView(name: tab.identity.server) { model.requestClose(tab) }
        }
    }

    @ViewBuilder private func connected(_ connection: ServerConnection) -> some View {
        if tab.isLauncher {
            SessionLauncherView(connection: connection, activity: model.activity, create: { model.creatingIn = tab }) { session in
                model.show(SessionIdentity(server: connection.name, session: session.id), in: tab)
            }
        } else if let store = tab.store {
            SessionScreen(state: store) { model.storeSelected($0, in: tab) }
        } else {
            KitSpinner("Opening session").frame(maxWidth: .infinity, maxHeight: .infinity)
                .task { model.store(for: tab) }
        }
    }
}

/// Reattaches tabs when the window's tabs or the servers they use change.
private struct AttachmentKey: Equatable {
    let tabs: [UUID]
    let connected: [String]

    @MainActor init(model: SessionWindowModel) {
        tabs = model.tabs.map(\.id)
        connected = model.servers.connections.filter(\.status.isConnected).map(\.name)
    }
}

extension FocusedValues {
    @Entry var sessionWindow: SessionWindowModel?
}
