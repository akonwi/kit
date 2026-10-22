import SwiftUI

/// How the launcher's server menu shows one server: where it is while it can
/// list sessions, otherwise what it needs. A server that needs fixing
/// elsewhere is dimmed and can't be chosen; Manage Servers… is where it's
/// fixed. An SSH server waiting for sign-in or with its Kit server stopped
/// can be chosen: its launcher signs in or offers Start Server.
struct LauncherServerOption: Equatable, Identifiable {
    var id: String { name }
    let name: String
    let detail: String
    /// Whether the detail is an address, set in mono.
    let address: Bool
    let available: Bool
    /// The server needs the user, so its detail is a warning.
    let needsAttention: Bool

    init(name: String, detail: String, address: Bool, available: Bool, needsAttention: Bool = false) {
        (self.name, self.detail, self.address, self.available, self.needsAttention) = (name, detail, address, available, needsAttention)
    }

    init(name: String, connector: ServerConnector, status: ServerStatus) {
        self.name = name
        available = Self.available(status)
        switch status {
        case .needsPairing: (detail, address, needsAttention) = ("Pair again", false, true)
        case .signInRequired: (detail, address, needsAttention) = ("Sign in", false, true)
        case .serverStopped: (detail, address, needsAttention) = ("Server stopped", false, true)
        case .unreachable, .incompatible, .hostKeyChanged:
            (detail, address, needsAttention) = (ServerRowPresentation(name: name, connector: connector, status: status).status, false, false)
        case .connecting, .connected, .reconnecting:
            needsAttention = false
            switch connector {
            case .home: (detail, address) = ("This Mac", false)
            case .url(let origin, _): (detail, address) = (Self.host(origin), true)
            case .ssh(let destination): (detail, address) = (destination, true)
            }
        }
    }

    /// Whether a server in `status` can be chosen: it can list sessions, now
    /// or once it connects, or its launcher can get it there.
    static func available(_ status: ServerStatus) -> Bool {
        switch status {
        case .connecting, .connected, .reconnecting, .signInRequired, .serverStopped: true
        case .unreachable, .incompatible, .needsPairing, .hostKeyChanged: false
        }
    }

    /// Whether a server in `status` can list sessions, now or once it connects.
    static func lists(_ status: ServerStatus) -> Bool {
        switch status {
        case .connecting, .connected, .reconnecting: true
        case .unreachable, .incompatible, .needsPairing, .signInRequired, .hostKeyChanged, .serverStopped: false
        }
    }

    /// An origin's host, with its port when it names one.
    static func host(_ origin: String) -> String {
        guard let components = URLComponents(string: origin), let host = components.host, !host.isEmpty else { return origin }
        let name = host.contains(":") ? "[" + host + "]" : host
        return components.port.map { name + ":" + String($0) } ?? name
    }
}

extension ServerDirectory {
    /// The server a new launcher lists: the one last chosen while it can
    /// list sessions, otherwise this Mac's.
    var launcherDefault: String {
        guard let connection = connection(named: launcherServer),
              LauncherServerOption.lists(connection.status) else { return ServerClient.localID }
        return connection.name
    }

    /// Every server, as the launcher's menu shows it.
    var launcherOptions: [LauncherServerOption] {
        connections.map { LauncherServerOption(name: $0.name, connector: $0.connector, status: $0.status) }
    }
}

/// The server a launcher lists, and how to choose another.
struct LauncherServerChoice {
    let servers: ServerDirectory
    let selected: String
    let choose: (String) -> Void
}

/// The launcher's server chip, before New session. It opens the server
/// menu, which ends with Add Server… and Manage Servers….
struct LauncherServerButton: View {
    @Environment(\.openSettings) private var openSettings
    let choice: LauncherServerChoice

    var body: some View {
        let openSettings = openSettings
        ServerMenuButton(choice: choice) { theme, popover, choice in
            LauncherServerMenu(theme: theme, servers: choice.servers, selected: choice.selected,
                choose: { name in
                    popover.dismiss()
                    choice.choose(name)
                },
                add: {
                    popover.dismiss()
                    SettingsSection.servers.open(with: openSettings, server: choice.selected, dialog: .add)
                },
                manage: {
                    popover.dismiss()
                    SettingsSection.servers.open(with: openSettings, server: choice.selected)
                })
        }
    }
}

/// A server chip that opens a menu of servers below itself.
struct ServerMenuButton: View {
    @Environment(\.mica) private var theme
    let choice: LauncherServerChoice
    var height: CGFloat = 30
    /// The menu, given the theme, the popover it shows in, and the choice.
    let menu: (MicaTheme, AnchoredPopover, LauncherServerChoice) -> LauncherServerMenu
    @State private var popover = AnchoredPopover()
    @State private var anchor = ComposerPickerAnchor()
    @State private var open = false

    var body: some View {
        Button(action: toggle) {
            HStack(spacing: 8) {
                IconView(.hardDrives, size: 13)
                Text(choice.selected).font(.kit(size: 13, weight: .medium)).lineLimit(1)
                IconView(.caretDown, size: 10).foregroundStyle(theme.muted)
            }
            .padding(.horizontal, 10).frame(height: height)
            .background(open ? theme.hover : theme.raised, in: RoundedRectangle(cornerRadius: 3))
            .overlay { RoundedRectangle(cornerRadius: 3).strokeBorder(theme.strongBorder) }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .fixedSize()
        .background(ComposerPickerAnchorView(anchor: anchor))
        .help("Choose a server")
        .accessibilityLabel("Server")
        .accessibilityValue(choice.selected)
        .onDisappear { popover.dismiss() }
    }

    private func toggle() {
        if popover.isOpen { popover.dismiss(); return }
        // The click that reached the button already closed the menu.
        if let closedAt = popover.closedAt, Date().timeIntervalSince(closedAt) < 0.3 { return }
        guard let frame = anchor.screenFrame, let window = anchor.view?.window else { return }
        let menu = menu(theme, popover, choice)
        open = true
        popover.show(anchor: frame, parent: window, edge: .below, alignment: .leading, content: menu) {
            open = false
        }
    }
}

/// The launcher's server menu: every server with where it is or what it
/// needs, then Add Server… and Manage Servers….
struct LauncherServerMenu: View {
    let theme: MicaTheme
    let servers: ServerDirectory
    let selected: String
    let choose: (String) -> Void
    /// Add Server… and Manage Servers…, which the launcher's menu ends with.
    var add: (() -> Void)? = nil
    var manage: (() -> Void)? = nil
    @State private var hovered: String?

    static let width: CGFloat = 340
    static let rowHeight: CGFloat = 30
    private static let addID = "\u{0}add"
    private static let manageID = "\u{0}manage"

    var body: some View {
        VStack(alignment: .leading, spacing: 1) {
            ForEach(servers.launcherOptions) { option in server(option) }
            if let add, let manage {
                VStack(alignment: .leading, spacing: 1) {
                    item(Self.addID, "Add Server…", icon: .plus, action: add)
                    item(Self.manageID, "Manage Servers…", icon: nil, action: manage)
                }
                .padding(.top, 4)
                .overlay(alignment: .top) { Rectangle().fill(theme.border).frame(height: 1) }
            }
        }
        .padding(4)
        .frame(width: Self.width)
        .foregroundStyle(theme.text)
        .background(theme.raised)
        .popoverCard(theme)
        .accessibilityLabel("Servers")
    }

    private func server(_ option: LauncherServerOption) -> some View {
        let current = option.name == selected
        return Button { choose(option.name) } label: {
            HStack(spacing: 10) {
                ZStack {
                    if option.needsAttention { IconView(.warning, size: 12).foregroundStyle(theme.warning) }
                    else if current { IconView(.check, size: 12) }
                }
                .frame(width: 14, height: 12)
                Text(option.name).font(.kit(size: 13)).lineLimit(1)
                    .foregroundStyle(option.available ? theme.text : theme.muted)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Text(option.detail)
                    .font(option.address ? .kit(size: 11, design: .monospaced) : .kit(size: 12))
                    .foregroundStyle(option.needsAttention ? theme.warning : theme.muted)
                    .lineLimit(1).truncationMode(.middle)
            }
            .padding(.horizontal, 8).frame(height: Self.rowHeight)
            .background(highlight(option.name, enabled: option.available) ? theme.accent.opacity(0.14) : .clear,
                        in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!option.available)
        .onHover { hover(option.name, $0) }
        .accessibilityLabel(option.name)
        .accessibilityValue(option.detail)
        .accessibilityAddTraits(current ? .isSelected : [])
    }

    private func item(_ id: String, _ title: String, icon: Icon?, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 10) {
                ZStack { if let icon { IconView(icon, size: 12).foregroundStyle(theme.muted) } }.frame(width: 14, height: 12)
                Text(title).font(.kit(size: 13)).frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.horizontal, 8).frame(height: Self.rowHeight)
            .background(hovered == id ? theme.accent.opacity(0.14) : .clear, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover(id, $0) }
    }

    /// The pointer's row, or the chosen server while the pointer is elsewhere.
    private func highlight(_ name: String, enabled: Bool) -> Bool {
        enabled && (hovered.map { $0 == name } ?? (name == selected))
    }

    private func hover(_ id: String, _ inside: Bool) {
        hovered = inside ? id : (hovered == id ? nil : hovered)
    }
}

/// The Server menu at the top of Settings › Models and Providers: its
/// label, then a chip listing every server. Servers that can't be reached
/// are dimmed.
struct SettingsServerPicker: View {
    @Environment(\.mica) private var theme
    let choice: LauncherServerChoice

    var body: some View {
        HStack(spacing: 10) {
            Text("Server").font(.kit(size: 13)).foregroundStyle(theme.muted)
            ServerMenuButton(choice: choice, height: 28) { theme, popover, choice in
                LauncherServerMenu(theme: theme, servers: choice.servers, selected: choice.selected) { name in
                    popover.dismiss()
                    choice.choose(name)
                }
            }
        }
    }
}
