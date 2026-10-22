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
            text = "SSH host. Kit starts its server there if it isn’t running."
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
    /// Asks the new SSH host's questions in the Settings window.
    let signIn: SSHSignIn
    let close: () -> Void
    @State private var location = ""
    @State private var name = ""
    /// The name was typed rather than suggested.
    @State private var named = false
    @State private var pending = false
    @State private var error: String?
    /// The server was saved, but connecting failed; only Done is left.
    @State private var added = false
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
                if added {
                    Button("Done", action: close).buttonStyle(FormButtonStyle(fill: .primary)).keyboardShortcut(.defaultAction)
                } else {
                    Button("Cancel", action: close).buttonStyle(FormButtonStyle()).keyboardShortcut(.cancelAction).disabled(pending)
                    Button(action: add) {
                        if pending { KitSpinner(parsed.map { if case .ssh = $0 { "Connecting…" } else { "Pairing…" } } ?? "Pairing…") }
                        else { Text("Add Server") }
                    }
                    .buttonStyle(FormButtonStyle(fill: .primary))
                    .disabled(!canAdd)
                }
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
            guard save(SavedServer(name: name, kind: .ssh, address: destination), closing: false),
                  let connection = servers.connection(named: name) else { return }
            // Kit starts the host's server if it isn't running, as kit attach
            // does; ssh's questions appear as sheets here (S6, S9).
            pending = true
            Task {
                let started = await signIn.signIn(connection, start: true)
                pending = false
                if let problem = Self.problem(name: name, status: connection.status, started: started) {
                    self.error = problem
                    added = true
                } else {
                    close()
                }
            }
        case .home(let path):
            save(SavedServer(name: name, kind: .home, address: path))
        }
    }

    /// What went wrong connecting to a new SSH server, which stays saved; nil
    /// when it connected or waits for the user to sign in.
    static func problem(name: String, status: ServerStatus, started: Bool) -> String? {
        guard started else { return "Kit saved \(name). Finish signing in to another server first, then choose Sign In… for \(name)." }
        switch status {
        case .unreachable(let reason, _): return "Kit saved \(name) but couldn’t connect: \(reason)"
        case .hostKeyChanged: return "Kit saved \(name) but stopped connecting: its host key changed. A tab on \(name) shows both keys."
        case .incompatible(let mismatch):
            return mismatch.serverIsOlder ? "Kit saved \(name), but its Kit is older than this app can use. Update Kit on \(name)."
                : "Kit saved \(name), but its Kit is newer than this app. Update Kit for Mac."
        case .serverStopped: return "Kit saved \(name) but couldn’t start its Kit server."
        case .needsPairing, .connecting, .connected, .reconnecting, .signInRequired: return nil
        }
    }

    @discardableResult
    private func save(_ server: SavedServer, closing: Bool = true) -> Bool {
        do {
            try servers.add(server)
            if closing { close() }
            return true
        } catch {
            self.error = error.localizedDescription
            return false
        }
    }
}
