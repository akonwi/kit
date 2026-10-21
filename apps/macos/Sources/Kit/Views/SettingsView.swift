import SwiftUI

struct SettingsView: View {
    @AppStorage("appearance") private var appearance = "system"
    @AppStorage("themeConfiguration") private var storedConfiguration = ""
    @Environment(\.installedThemes) private var installedThemes
    @AppStorage(SettingsSection.storageKey) private var selection = SettingsSection.appearance.rawValue
    @Environment(\.serverDirectory) private var servers
    @State private var dialog: ServersDialog?
    let themeLibrary: NativeThemeLibrary
    let windows: SessionWindowRegistry

    var body: some View {
        let scheme = SystemAppearance.shared.resolve(appearance)
        let dark = scheme == .dark
        let theme = ThemeConfiguration.decode(storedConfiguration).resolved(dark: dark, installed: installedThemes)
        HStack(spacing: 0) {
            VStack(spacing: 6) {
                section("Appearance", icon: .circleHalf, id: "appearance", theme: theme)
                section("Workspace", icon: .squareSplitHorizontal, id: "workspace", theme: theme)
                section("Servers", icon: .hardDrives, id: "servers", theme: theme)
                section("Models", icon: .stack, id: "models", theme: theme)
                section("Providers", icon: .plugsConnected, id: "providers", theme: theme)
                Spacer()
            }.padding(12).padding(.top, 16).frame(width: 170).background(theme.raised)
            Rectangle().fill(theme.border).frame(width: 1)
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    if selection == "appearance" {
                        VStack(alignment: .leading, spacing: 6) {
                            Text("Appearance").font(.kit(size: 23, weight: .semibold))
                            Text("These preferences apply to this app.").foregroundStyle(theme.muted)
                        }
                        ThemeSettingsView(library: themeLibrary)
                        Divider()
                        TypographySettingsView()
                    } else if selection == "workspace" {
                        WorkspaceSettingsView()
                    } else if selection == "servers" {
                        ServersSettingsView(dialog: $dialog, windows: windows)
                    } else if selection == "providers" {
                        if let connection = settingsServer {
                            ProviderSettingsView(client: connection.client, server: choice(connection)).id(connection.name)
                        } else {
                            ProviderSettingsView()
                        }
                    } else if let connection = settingsServer {
                        ModelSettingsView(client: connection.client, server: choice(connection)).id(connection.name)
                    } else {
                        ModelSettingsView()
                    }
                }.padding(28).frame(maxWidth: .infinity, alignment: .leading)
            }.background(theme.surface)
        }
        .disabled(dialog != nil)
        .accessibilityHidden(dialog != nil)
        .overlay { dialogOverlay }
        .font(.kit(size: 13)).foregroundStyle(theme.text).tint(theme.accent)
        .environment(\.mica, theme)
        .frame(width: 800, height: 690)
        .onAppear(perform: takeRoute)
        .onChange(of: SettingsRoute.shared.dialog) { takeRoute() }
        .preferredColorScheme(theme.dark ? .dark : .light)
    }

    /// The server Models and Providers show: the one asked for, while the
    /// app knows it, otherwise this Mac's.
    private var settingsServer: ServerConnection? {
        guard let servers else { return nil }
        return servers.connection(named: SettingsRoute.shared.server) ?? servers.local
    }

    private func choice(_ connection: ServerConnection) -> LauncherServerChoice? {
        guard let servers else { return nil }
        return LauncherServerChoice(servers: servers, selected: connection.name) { SettingsRoute.shared.server = $0 }
    }

    /// Opens the dialog another window asked for.
    private func takeRoute() {
        guard let requested = SettingsRoute.shared.dialog else { return }
        SettingsRoute.shared.dialog = nil
        selection = SettingsSection.servers.rawValue
        dialog = requested
    }

    @ViewBuilder private var dialogOverlay: some View {
        if let dialog, let servers {
            let close = { self.dialog = nil }
            SettingsDialogOverlay(close: close) {
                switch dialog {
                case .add:
                    AddServerDialog(servers: servers, close: close)
                case .pair(let name):
                    if let connection = servers.connection(named: name) {
                        PairDeviceDialog(connection: connection, close: close)
                    }
                case .pairAgain(let name):
                    if let connection = servers.connection(named: name) {
                        PairAgainDialog(servers: servers, connection: connection, close: close)
                    }
                case .rename(let name):
                    RenameServerDialog(servers: servers, name: name, close: close)
                case .remove(let name):
                    RemoveServerDialog(servers: servers, name: name, close: close)
                }
            }
        }
    }

    private func section(_ title: String, icon: Icon, id: String, theme: MicaTheme) -> some View {
        Button { selection = id } label: {
            Label(title, icon: icon, size: 14).frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 12).padding(.vertical, 10)
                .background(selection == id ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 6))
        }.buttonStyle(.plain).accessibilityAddTraits(selection == id ? .isSelected : [])
    }
}
