import AppKit
import SwiftUI

/// One window: its session tabs, the selected session's bar, and that
/// session's content. The window owns one connection; a server switch in
/// another window never redirects this window's requests.
struct SessionWindowRoot: View {
    @Binding var request: SessionWindowRequest
    let windows: SessionWindowRegistry
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var themeConfiguration = ""
    @AppStorage(SessionTabPlacement.storageKey) private var placement = SessionTabPlacement.side
    @Environment(\.installedThemes) private var installedThemes
    @Environment(\.openWindow) private var openWindow
    @State private var localConnectionRequest = 0
    @State private var signIn: ProviderSignIn?
    /// Whether first run is showing. It stays until Continue, so signing in
    /// to another provider from the connected step keeps the window on it.
    @State private var firstRun = false

    var body: some View {
        let model = windows.model(for: request)
        let app = model.app
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
            openSession: { id in
                guard let client = app.client else { return }
                windows.open(SessionIdentity(server: client.serverID, session: id), from: model)
            },
            sessionTitle: { id in
                model.tabs.first { !$0.isLauncher && $0.identity.session == id }?.title
                    ?? app.sessions.first { $0.id == id }?.title
            },
            openLauncher: { model.openLauncher() }))
        .focusedSceneValue(\.sessionWindow, model)
        .navigationTitle(tab.title)
        .background {
            SessionWindowAttachment(registry: windows, model: model).frame(width: 0, height: 0)
        }
        .task(id: localConnectionRequest) {
            if localConnectionRequest > 0 { await app.connectLocal() }
        }
        .task {
            let servers = Set(model.tabs.map(\.identity.server))
            await app.connectIfNeeded(to: servers.first { $0 != SessionWindowModel.defaultServer } ?? SessionWindowModel.defaultServer)
        }
        .task(id: AttachmentKey(server: app.client?.serverID, tabs: model.tabs.map(\.id))) {
            if let client = app.client { model.attachAll(client: client) }
        }
        .onAppear {
            windows.restoreRemaining { openWindow(id: "session", value: $0) }
        }
        .task(id: app.client?.serverID) { await checkProviders(app, reset: true) }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            Task { await checkProviders(app, reset: false) }
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

    /// Shows first run while the server has no provider connected. A server
    /// that can't list providers keeps the workspace.
    private func checkProviders(_ app: AppModel, reset: Bool) async {
        guard let client = app.client as? any ProviderClient else { signIn = nil; firstRun = false; return }
        if reset || signIn == nil { signIn = ProviderSignIn(client: client) }
        guard let signIn, signIn.step == .list else { return }
        await signIn.load()
        if signIn.loadError == nil && !signIn.anyConnected { firstRun = true }
    }

    /// The new session form, over the whole window while a launcher asks for it.
    @ViewBuilder private func newSessionForm(model: SessionWindowModel, theme: MicaTheme) -> some View {
        let app = model.app
        if let launcher = model.creatingIn, let client = app.client as? any SessionCreationClient {
            NewSessionOverlay(client: client, suggestedModel: app.sessions.first?.model,
                              close: { model.creatingIn = nil }) { session in
                app.recordCreated(session)
                model.showCreated(SessionIdentity(server: client.serverID, session: session.id), from: launcher)
            }
            .environment(\.mica, theme)
        }
    }

    /// The selected tab's content, inset from the window frame. An open
    /// session draws its own tiles; anything else sits in a single tile.
    private func tile(model: SessionWindowModel, tab: SessionTab, theme: MicaTheme) -> some View {
        let tiled = tab.store?.selected != nil
        return VStack(spacing: 0) {
            if let error = windows.restoration.error {
                HStack {
                    Text(error).textSelection(.enabled)
                    Button("Retry saving") { windows.restoration.save() }
                }.font(.kit(size: 12)).padding(12)
            }
            SessionTabContent(model: model, tab: tab, localConnectionRequest: $localConnectionRequest)
                .id(tab.id)
        }
        .background(tiled ? .clear : theme.surface)
        .clipShape(RoundedRectangle(cornerRadius: tiled ? 0 : 4))
        .overlay { if !tiled { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) } }
        .padding([.trailing, .bottom], tileGap)
        .padding(.leading, placement == .top ? tileGap : 0)
    }
}

/// A tab's content: the launcher, its session, or how to reach its server.
private struct SessionTabContent: View {
    let model: SessionWindowModel
    let tab: SessionTab
    @Binding var localConnectionRequest: Int

    var body: some View {
        let app = model.app
        if tab.isLauncher {
            SessionLauncherView(app: app, activity: model.activity, create: { if app.client is any SessionCreationClient { model.creatingIn = tab } }) { session in
                model.show(SessionIdentity(server: app.client?.serverID ?? SessionWindowModel.defaultServer, session: session.id), in: tab)
            }
        } else if let client = app.client, tab.identity.server.isEmpty || tab.identity.server == client.serverID {
            Group {
                if let store = tab.store {
                    SessionScreen(state: store) { model.storeSelected($0, in: tab) }
                } else {
                    KitSpinner("Opening session").frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .task(id: client.serverID) { _ = model.store(for: tab, client: client) }
        } else {
            VStack(spacing: 8) {
                Text("Reopen session").font(.kit(size: 17, weight: .semibold)).padding(.top, 24)
                Text("\(tab.identity.server) · \(tab.identity.session)")
                    .font(.kit(size: 12)).foregroundStyle(.secondary).textSelection(.enabled)
                if tab.identity.server == SessionWindowModel.defaultServer {
                    ConnectionView(app: app)
                } else {
                    Text("This server is not configured.").foregroundStyle(.secondary).padding()
                    Button("Connect to local server") { localConnectionRequest += 1 }
                        .disabled(app.connecting)
                    if let error = app.error { Text(error).foregroundStyle(.secondary) }
                }
            }.frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

/// Reattaches tabs when the window's server or tabs change.
private struct AttachmentKey: Equatable {
    let server: String?
    let tabs: [UUID]
}

extension FocusedValues {
    @Entry var sessionWindow: SessionWindowModel?
}
