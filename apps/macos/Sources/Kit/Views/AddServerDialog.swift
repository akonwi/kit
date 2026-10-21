import Network
import SwiftUI

/// What Add Server says about the location typed so far.
struct AddServerHint: Equatable {
    enum Kind: Equatable { case link, ssh, home, invalid }
    let kind: Kind
    let text: String
    /// A plain HTTP link sends the code and token unencrypted.
    var warning: String? = nil

    init?(_ value: String, location: ServerLocation?) {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        switch location {
        case .link(let link):
            let host = URLComponents(string: link.server)?.host ?? link.server
            kind = .link
            text = "Pairing link for \(host). Kit keeps this Mac’s device token in the Keychain."
            if link.server.hasPrefix("http://") && !Self.isLoopback(host) {
                warning = "Plain HTTP: the code and token cross the network unencrypted unless it’s private, like a tailnet."
            }
        case .ssh:
            kind = .ssh
            text = "SSH host. Kit for Mac can’t connect to SSH servers yet; kit attach can."
        case .home:
            kind = .home
            text = "Kit home on this Mac."
        case nil:
            kind = .invalid
            if PairingLink.looksLikeLink(trimmed) {
                text = "This pairing link is incomplete. Copy the whole link, which ends in /pair# and a code."
            } else if trimmed.hasPrefix("http://") || trimmed.hasPrefix("https://") {
                text = "A server URL needs pairing. Run kit server pair on that server and paste the link it prints."
            } else {
                text = "Enter a pairing link, an SSH host, or a Kit home path."
            }
        }
    }

    /// Loopback traffic never leaves this Mac, so plain HTTP there is fine.
    private static func isLoopback(_ host: String) -> Bool {
        let bare = host.trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        return bare == "localhost" || IPv4Address(bare)?.rawValue.first == 127 || IPv6Address(bare)?.isLoopback == true
    }
}

/// Add Server (S6): saves a server from a pairing link, an SSH host, or a
/// Kit home on this Mac, named from its address.
struct AddServerDialog: View {
    @Environment(\.mica) private var theme
    let servers: ServerDirectory
    let close: () -> Void
    @State private var location = ""
    @State private var name = ""
    /// The name was typed rather than suggested.
    @State private var named = false
    @State private var pending = false
    @State private var error: String?
    @FocusState private var focus: Field?

    private enum Field { case location, name }

    private var parsed: ServerLocation? { ServerLocation(location) }
    private var canAdd: Bool { parsed != nil && SavedServer.validName(name) && !pending }

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            DialogHeading(title: "Add Server",
                          message: "Paste a pairing link from kit server pair, or enter an SSH host or a Kit home on this Mac.")
            VStack(alignment: .leading, spacing: 6) {
                Text("Location").font(.kit(size: 12)).foregroundStyle(theme.muted)
                TextField("", text: $location, prompt: Text("https://host/pair#…, me@host, or ~/path").foregroundStyle(theme.strongBorder))
                    .textFieldStyle(.plain).font(.kit(size: 12, design: .monospaced)).focused($focus, equals: .location)
                    .onSubmit(add)
                    .padding(.horizontal, 10).frame(height: 30)
                    .fieldBox(theme, focused: focus == .location)
                    .accessibilityLabel("Location")
                if let hint = AddServerHint(location, location: parsed) { hintView(hint) }
            }
            VStack(alignment: .leading, spacing: 6) {
                Text("Name").font(.kit(size: 12)).foregroundStyle(theme.muted)
                TextField("", text: Binding(get: { name }, set: { name = $0; named = !$0.isEmpty }))
                    .textFieldStyle(.plain).font(.kit(size: 13, design: .monospaced)).focused($focus, equals: .name)
                    .onSubmit(add)
                    .padding(.horizontal, 10).frame(height: 30)
                    .fieldBox(theme, focused: focus == .name)
                    .accessibilityLabel("Name")
                if !name.isEmpty && !SavedServer.validName(name) {
                    Text(SavedServerError.invalid(SavedServer(name: name, kind: .home, address: "/")).localizedDescription)
                        .font(.kit(size: 12)).foregroundStyle(theme.muted).fixedSize(horizontal: false, vertical: true)
                }
            }
            if let error { ProviderErrorLine(message: error) }
            Rule()
            HStack(spacing: 8) {
                Spacer()
                Button("Cancel", action: close).buttonStyle(FormButtonStyle()).keyboardShortcut(.cancelAction).disabled(pending)
                Button(action: add) {
                    if pending { KitSpinner("Pairing…") } else { Text("Add Server") }
                }
                .buttonStyle(FormButtonStyle(fill: .primary))
                .disabled(!canAdd)
            }
        }
        // After the click that opened the dialog finishes, which would take focus back.
        .task { focus = .location }
        .onChange(of: location) {
            error = nil
            if !named { name = parsed.map(servers.suggestedName(for:)) ?? "" }
        }
        .onChange(of: name) { error = nil }
    }

    private func hintView(_ hint: AddServerHint) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .top, spacing: 6) {
                switch hint.kind {
                case .link: IconView(.key, size: 12)
                case .ssh: IconView(.terminalWindow, size: 12)
                case .home: IconView(.folderSimple, size: 12)
                case .invalid: IconView(.warningCircle, size: 12)
                }
                Text(hint.text).fixedSize(horizontal: false, vertical: true)
            }
            if let warning = hint.warning {
                Text(warning).foregroundStyle(theme.warning).fixedSize(horizontal: false, vertical: true)
            }
        }
        .font(.kit(size: 12)).foregroundStyle(theme.muted)
    }

    private func add() {
        guard canAdd, let parsed else { return }
        let name = name
        switch parsed {
        case .link(let link):
            pending = true
            Task {
                defer { pending = false }
                do {
                    try await servers.add(name: name, link: link)
                    close()
                } catch {
                    self.error = error.localizedDescription
                }
            }
        case .ssh(let destination):
            save(SavedServer(name: name, kind: .ssh, address: destination))
        case .home(let path):
            save(SavedServer(name: name, kind: .home, address: path))
        }
    }

    private func save(_ server: SavedServer) {
        do {
            try servers.add(server)
            close()
        } catch {
            self.error = error.localizedDescription
        }
    }
}
