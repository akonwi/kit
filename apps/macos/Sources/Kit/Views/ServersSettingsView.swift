import AppKit
import SwiftUI

/// What Settings is asked to show from elsewhere in the app: a section's
/// server, or a dialog to open, as Add Server… in the launcher does.
@MainActor @Observable
final class SettingsRoute {
    static let shared = SettingsRoute()
    /// The server Models and Providers start on: that of the tab Settings
    /// was opened from.
    var server = ServerClient.localID
    /// A Servers dialog to open when Settings shows.
    var dialog: ServersDialog?
}

extension SettingsSection {
    /// Opens Settings at this section for `server`, with `dialog` open.
    @MainActor func open(with openSettings: OpenSettingsAction, server: String, dialog: ServersDialog? = nil) {
        SettingsRoute.shared.server = server
        SettingsRoute.shared.dialog = dialog
        open(with: openSettings)
    }
}

/// A dialog over Settings › Servers.
enum ServersDialog: Identifiable, Equatable {
    case add
    case pair(String)
    case pairAgain(String)
    case rename(String)
    case remove(String)

    var id: String {
        switch self {
        case .add: "add"
        case .pair(let name): "pair:" + name
        case .pairAgain(let name): "pair-again:" + name
        case .rename(let name): "rename:" + name
        case .remove(let name): "remove:" + name
        }
    }
}

/// How Settings › Servers shows one server.
struct ServerRowPresentation: Equatable {
    enum Tone: Equatable { case success, warning, muted, hollow }
    enum Action: Equatable { case menu, pairAgain }

    let name: String
    let detail: String
    let status: String
    let tone: Tone
    let action: Action

    init(name: String, detail: String, status: String, tone: Tone, action: Action) {
        (self.name, self.detail, self.status, self.tone, self.action) = (name, detail, status, tone, action)
    }

    init(name: String, connector: ServerConnector, status: ServerStatus) {
        self.name = name
        let local = name == ServerClient.localID
        switch connector {
        case .home(let home): detail = "This Mac · " + ServerStatePresentation.abbreviated(home.path)
        case .url(let origin, _): detail = origin
        case .ssh(let destination): detail = "SSH · " + destination
        }
        action = if case .needsPairing = status { .pairAgain } else { .menu }
        switch status {
        case .connected:
            (self.status, tone) = (local ? "Running" : "Connected", .success)
        case .connecting:
            (self.status, tone) = ("Connecting…", .muted)
        case .reconnecting:
            (self.status, tone) = ("Reconnecting…", .muted)
        case .unreachable:
            (self.status, tone) = ("Can’t connect", .hollow)
        case .incompatible(let mismatch):
            (self.status, tone) = (mismatch.serverIsOlder ? "Needs a newer Kit" : "Needs a newer Kit for Mac", .warning)
        case .needsPairing(let need):
            (self.status, tone) = (need == .expired ? "Pairing expired" : need == .missing ? "Not paired" : "Pairing revoked", .warning)
        }
    }
}

/// Settings › Servers: this Mac's server and the saved servers, which the
/// kit command shares, each with its status and what can be done with it.
struct ServersSettingsView: View {
    @Environment(\.mica) private var theme
    @Environment(\.serverDirectory) private var servers
    @Binding var dialog: ServersDialog?

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Servers").font(.kit(size: 23, weight: .semibold))
                Text("Session tabs can come from any of these. The kit command uses the same list.").foregroundStyle(theme.muted)
            }
            if let servers {
                VStack(spacing: 0) {
                    ForEach(Array(servers.connections.enumerated()), id: \.element.id) { index, connection in
                        if index > 0 { Rule() }
                        row(connection)
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: 4))
                .overlay { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
                Button { dialog = .add } label: {
                    HStack(spacing: 6) { IconView(.plus, size: 12); Text("Add Server…") }
                }
                .buttonStyle(FormButtonStyle())
            }
        }
    }

    private func row(_ connection: ServerConnection) -> some View {
        let row = ServerRowPresentation(name: connection.name, connector: connection.connector, status: connection.status)
        return HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(row.name)
                Text(row.detail).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1).truncationMode(.middle)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 6) {
                switch row.tone {
                case .hollow: Circle().strokeBorder(theme.muted).frame(width: 6, height: 6)
                case .success: Circle().fill(theme.success).frame(width: 6, height: 6)
                case .warning: Circle().fill(theme.warning).frame(width: 6, height: 6)
                case .muted: Circle().fill(theme.muted).frame(width: 6, height: 6)
                }
                Text(row.status).foregroundStyle(row.tone == .warning ? theme.warning : theme.muted)
            }
            .font(.kit(size: 12))
            .frame(width: 150, alignment: .leading)
            Group {
                switch row.action {
                case .pairAgain:
                    Button("Pair Again") { dialog = .pairAgain(connection.name) }
                        .buttonStyle(FormButtonStyle(fill: .primary))
                case .menu:
                    menu(connection)
                }
            }
            .fixedSize()
            // One width for every row keeps the status column aligned.
            .frame(width: 96, alignment: .trailing)
        }
        .padding(.horizontal, 14).frame(height: 52)
        .contentShape(Rectangle())
        .contextMenu { items(connection) }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(row.name)
        .accessibilityValue(row.status)
    }

    private func menu(_ connection: ServerConnection) -> some View {
        Menu { items(connection) } label: {
            IconView(.dotsThree, size: 16).foregroundStyle(theme.muted)
                .frame(width: 28, height: 28).contentShape(Rectangle())
        }
        .menuStyle(.button).buttonStyle(.plain).menuIndicator(.hidden)
        .fixedSize()
        .accessibilityLabel("Actions for \(connection.name)")
    }

    @ViewBuilder private func items(_ connection: ServerConnection) -> some View {
        // A link comes from the server itself, so it must be answering.
        if connection.status.identity != nil {
            Button("Pair a Device…") { dialog = .pair(connection.name) }
        }
        if case .needsPairing = connection.status {
            Button("Pair Again…") { dialog = .pairAgain(connection.name) }
        }
        if !connection.isLocal {
            Button("Rename…") { dialog = .rename(connection.name) }
            Divider()
            Button("Remove…", role: .destructive) { dialog = .remove(connection.name) }
        }
    }
}

/// Dims Settings and holds a dialog near its top. Esc closes it.
struct SettingsDialogOverlay<Content: View>: View {
    @Environment(\.mica) private var theme
    let close: () -> Void
    @ViewBuilder let content: Content

    var body: some View {
        ZStack(alignment: .top) {
            Color.black.opacity(0.45).contentShape(Rectangle())
                .accessibilityHidden(true)
            content
                .font(.kit(size: 13))
                .padding(24).frame(width: 460)
                .background(theme.surface)
                .clipShape(RoundedRectangle(cornerRadius: 4))
                .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
                .shadow(color: .black.opacity(theme.dark ? 0.5 : 0.18), radius: 24, y: 18)
                .padding(.top, 96)
                .onExitCommand(perform: close)
        }
    }
}

/// A dialog's heading: its title and what it does.
struct DialogHeading: View {
    @Environment(\.mica) private var theme
    let title: String
    let message: String

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.kit(size: 15, weight: .semibold))
            Text(message).foregroundStyle(theme.muted).fixedSize(horizontal: false, vertical: true)
        }
    }
}

/// Saves a server under another name. Its tabs follow.
struct RenameServerDialog: View {
    @Environment(\.mica) private var theme
    let servers: ServerDirectory
    let name: String
    let close: () -> Void
    @State private var newName: String
    @State private var error: String?
    @FocusState private var focused: Bool

    init(servers: ServerDirectory, name: String, close: @escaping () -> Void) {
        self.servers = servers
        self.name = name
        self.close = close
        _newName = State(initialValue: name)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            DialogHeading(title: "Rename \(name)", message: "Tabs on \(name) and the kit command use the new name.")
            VStack(alignment: .leading, spacing: 6) {
                Text("Name").font(.kit(size: 12)).foregroundStyle(theme.muted)
                TextField("", text: $newName)
                    .textFieldStyle(.plain).font(.kit(size: 13, design: .monospaced)).focused($focused)
                    .onSubmit(save)
                    .padding(.horizontal, 10).frame(height: 30)
                    .fieldBox(theme, focused: focused)
                    .accessibilityLabel("Name")
                if let error { ProviderErrorLine(message: error) }
            }
            Rule()
            HStack(spacing: 8) {
                Spacer()
                Button("Cancel", action: close).buttonStyle(FormButtonStyle()).keyboardShortcut(.cancelAction)
                Button("Rename", action: save).buttonStyle(FormButtonStyle(fill: .primary))
                    .disabled(newName == name || newName.isEmpty)
            }
        }
        .onAppear { focused = true }
        .onChange(of: newName) { error = nil }
    }

    private func save() {
        guard newName != name else { return }
        do {
            try servers.rename(name, to: newName)
            close()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// Asks before forgetting a server and closing its tabs.
struct RemoveServerDialog: View {
    let servers: ServerDirectory
    let name: String
    let close: () -> Void
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            DialogHeading(title: "Remove \(name)?",
                          message: Self.message(name: name, token: hasToken, tabs: servers.openTabs(name),
                                                 running: servers.busyTerminals(name)))
            if let error { ProviderErrorLine(message: error) }
            Rule()
            HStack(spacing: 8) {
                Spacer()
                Button("Cancel", action: close).buttonStyle(FormButtonStyle()).keyboardShortcut(.cancelAction)
                Button("Remove") {
                    do {
                        try servers.remove(name)
                        close()
                    } catch {
                        self.error = error.localizedDescription
                    }
                }
                .buttonStyle(FormButtonStyle(fill: .primary))
            }
        }
    }

    /// A served server keeps a device token unless it was never paired.
    private var hasToken: Bool {
        guard servers.savedServers.first(where: { $0.name == name })?.kind == .url else { return false }
        return servers.connections.first { $0.name == name }?.status != .needsPairing(.missing)
    }

    static func message(name: String, token hasToken: Bool, tabs: Int, running: Int = 0) -> String {
        let token = hasToken ? " Its device token is removed from the Keychain." : ""
        let closing = switch tabs {
        case 0: ""
        case 1: " Its open tab closes."
        default: " Its \(tabs) open tabs close."
        }
        let ending = switch running {
        case 0: ""
        case 1: " A command still running in its terminals ends."
        default: " \(running) commands still running in its terminals end."
        }
        return "Kit and the kit command forget \(name).\(token)\(closing)\(ending) The server and its sessions keep running."
    }
}

/// Pairs this Mac with a served server again from a new link.
struct PairAgainDialog: View {
    let servers: ServerDirectory
    let connection: ServerConnection
    let close: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            DialogHeading(title: "Pair with \(connection.name) Again",
                          message: "Get a new link by running kit server pair on \(connection.name), or with Pair a Device on another paired device, and paste it here.")
            PairingLinkField(placeholder: ServerPairingView.placeholder(connection.connector), autofocus: true) { link in
                try await servers.pairAgain(connection.name, link: link)
                close()
            }
            Rule()
            HStack {
                Spacer()
                Button("Cancel", action: close).buttonStyle(FormButtonStyle()).keyboardShortcut(.cancelAction)
            }
        }
    }
}
