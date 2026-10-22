import AppKit
import SwiftUI

/// What a tab says in place of its content while its server can't serve it:
/// the server can't be reached (S13), it is older than the app (S14), the
/// app is older than it (S15), an SSH host's key changed (S10), its Kit
/// server stopped (S16), or it waits for the user to sign in (S9).
struct ServerStatePresentation: Equatable {
    enum Action: Equatable {
        /// The server is checked again at `retry`, or now when nil.
        case retry(Date?)
        /// The user restarts the server with `command` once it is updated.
        case restart(command: String, prompt: String)
        /// The user installs a newer app.
        case update
        /// The user removes an SSH host's old key with `command` in
        /// Terminal, then tries again (S10).
        case removeHostKey(command: String, prompt: String)
        /// The user starts an SSH host's Kit server, as `kit attach` does (S16).
        case startServer(note: String)
        /// The user signs in to an SSH host (S9).
        case signIn
    }

    struct Version: Equatable {
        let label: String
        let value: String
        /// The side that needs updating.
        let outdated: Bool
    }

    let title: String
    let message: String
    /// The connector and its address, above `reason`, for a server that
    /// can't be reached.
    let source: String?
    let reason: String?
    let versions: [Version]
    let action: Action
    let footnote: String

    static let releasesURL = URL(string: "https://github.com/akonwi/kit/releases")!

    /// How the app names itself beside a server's Kit version.
    static var appVersion: String {
        "Kit for Mac " + (Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev")
    }

    /// `thisMac` is whether the server last verified as running on this Mac.
    init(name: String, connector: ServerConnector, status: ServerStatus, sessionTab: Bool, thisMac: Bool? = nil,
         appVersion: String = Self.appVersion) {
        let local = name == ServerClient.localID
        // The local server is the app's own, so it goes unnamed.
        let server = local ? "The Kit server on this Mac" : name
        footnote = sessionTab
            ? "Your draft and layout stay in this tab. Tabs on other servers keep working."
            : "Tabs on other servers keep working."
        switch status {
        case .incompatible(let mismatch):
            source = nil
            reason = nil
            let serverVersion = "Kit \(mismatch.serverKitVersion) · protocol \(mismatch.serverProtocol)"
            versions = [
                Version(label: "This app", value: "\(appVersion) · protocol \(mismatch.appProtocol)", outdated: !mismatch.serverIsOlder),
                Version(label: local ? "Server" : name, value: serverVersion, outdated: mismatch.serverIsOlder),
            ]
            if mismatch.serverIsOlder {
                title = local ? "The Kit server needs a newer Kit" : "\(name) needs a newer Kit"
                message = "\(server) runs an older Kit than this app can use. Update kit, then restart the server. Restarting stops work running there, so Kit leaves it to you."
                action = .restart(command: Self.restartCommand(connector: connector),
                                  prompt: Self.runsHere(connector: connector, thisMac: thisMac)
                                      ? "After updating kit, run:" : "After updating kit on \(name), run there:")
            } else {
                title = local ? "Update Kit for Mac" : "Update Kit to use \(name)"
                message = local
                    ? "The Kit server on this Mac runs a newer Kit than this app. Install the latest Kit for Mac; the server and its work keep running."
                    : "\(name) runs a newer Kit than this app. Install the latest Kit for Mac; \(name) and its work keep running."
                action = .update
            }
        case .unreachable(let failure, let retry):
            title = local ? "Can’t connect to Kit" : "Can’t connect to \(name)"
            message = local
                ? "Kit couldn’t reach the Kit server on this Mac. It keeps trying, and this tab picks up where it left off when the server is running."
                : "Kit couldn’t reach \(name)\(Self.overSSH(connector)). It keeps trying, and this tab picks up where it left off when \(name) is back."
            source = Self.source(name: name, connector: connector)
            reason = failure
            versions = []
            action = .retry(retry)
        case .hostKeyChanged(let change):
            title = "\(name)’s host key changed"
            message = "The key \(name) sent doesn’t match the one this Mac saved, so Kit stopped connecting. A reinstalled host does this, but so does someone intercepting the connection."
            source = nil
            reason = nil
            versions = (change.saved.map { [Version(label: "Saved", value: $0, outdated: false)] } ?? [])
                + [Version(label: "Now", value: change.now, outdated: true)]
            action = .removeHostKey(command: change.removeCommand,
                                    prompt: "If you expected this, remove the old key in Terminal, then try again:")
        case .serverStopped:
            title = "\(name)’s Kit server stopped"
            message = "Kit reached \(name) over SSH, but its Kit server isn’t running. Kit doesn’t start it again on its own."
            source = Self.source(name: name, connector: connector)
            reason = "kit server status: not running"
            versions = []
            action = .startServer(note: "Runs kit server start on \(name), as kit attach does.")
        case .signInRequired:
            title = "Sign in to \(name)"
            message = "\(name) asked for something only you can answer, such as a password. Kit doesn’t ask while you’re elsewhere; sign in to keep connecting."
            source = nil
            reason = nil
            versions = []
            action = .signIn
        case .connecting, .connected, .reconnecting, .needsPairing:
            title = local ? "Connecting to Kit" : "Connecting to \(name)"
            message = ""
            source = nil
            reason = nil
            versions = []
            action = .retry(nil)
        }
    }

    private static func overSSH(_ connector: ServerConnector) -> String {
        if case .ssh = connector { " over SSH" } else { "" }
    }

    /// The connector and its address, such as `home · ~/dev/kit`.
    static func source(name: String, connector: ServerConnector) -> String {
        switch connector {
        case .home(let home):
            let kind = name == ServerClient.localID ? "local" : "home"
            return kind + " · " + abbreviated(home.path)
        case .url(let origin, _): return "url · " + origin
        case .ssh(let destination): return "ssh · " + destination
        }
    }

    /// Whether the restart command is run on this Mac: a Kit home's command
    /// names a path on this Mac, and a served server's runs here only when it
    /// verified as this Mac's.
    static func runsHere(connector: ServerConnector, thisMac: Bool?) -> Bool {
        if case .home = connector { return true }
        return thisMac == true
    }

    /// The command that restarts a server once it is updated. A home other
    /// than the default is named, so the command restarts that server.
    static func restartCommand(connector: ServerConnector) -> String {
        switch connector {
        case .home(let home):
            let path = home.standardizedFileURL.path
            guard path != DevHomeResolver.defaultHome.standardizedFileURL.path else { return "kit server restart" }
            return "KIT_HOME=\(shellQuoted(path)) kit server restart"
        case .url, .ssh:
            return "kit server restart"
        }
    }

    static func abbreviated(_ path: String) -> String {
        let user = FileManager.default.homeDirectoryForCurrentUser.path
        if path == user { return "~" }
        if path.hasPrefix(user + "/") { return "~" + path.dropFirst(user.count) }
        return path
    }

    private static func shellQuoted(_ value: String) -> String {
        let safe = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "/._-+:@%"))
        guard value.unicodeScalars.contains(where: { !safe.contains($0) }) else { return value }
        return "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }
}

/// A tab's content while its server can't serve it, with what to do.
struct ServerStateView: View {
    @Environment(\.mica) private var theme
    @Environment(\.openURL) private var openURL
    @Environment(\.openSettings) private var openSettings
    @Environment(\.controlActiveState) private var activeState
    let connection: ServerConnection
    var sessionTab = true
    @State private var signIn = SSHSignIn()
    /// This view already signed in once on its own.
    @State private var askedOnOpen = false

    /// The width of a server state's column.
    static let width: CGFloat = 520

    var body: some View {
        let presentation = ServerStatePresentation(name: connection.name, connector: connection.connector,
                                                   status: connection.status, sessionTab: sessionTab,
                                                   thisMac: connection.isThisMac)
        VStack(alignment: .leading, spacing: 18) {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 8) {
                    IconView(.warning, size: 16).foregroundStyle(theme.warning)
                    Text(presentation.title).font(.kit(size: 17, weight: .semibold))
                }
                Text(presentation.message)
                    .font(.kit(size: 13)).foregroundStyle(theme.muted)
                    .lineSpacing(3).fixedSize(horizontal: false, vertical: true)
            }
            if let reason = presentation.reason { failure(source: presentation.source, reason: reason) }
            if !presentation.versions.isEmpty { versions(presentation.versions) }
            action(presentation.action)
            Text(presentation.footnote)
                .font(.kit(size: 12)).foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(width: ServerStateView.width, alignment: .leading)
        .padding(.top, SessionLauncherContent.top).padding(.bottom, 40)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .textSelection(.enabled)
        .sshSignInSheet(signIn)
        // Opening a server waiting for sign-in asks, in the window in use.
        .onChange(of: activeState, initial: true) { signInOnOpen() }
        .onChange(of: connection.status) { signInOnOpen() }
    }

    private func signInOnOpen() {
        guard connection.status == .signInRequired, activeState == .key, !askedOnOpen, !signIn.signingIn else { return }
        askedOnOpen = true
        Task { await signIn.signIn(connection) }
    }

    /// Checks the server again. An SSH server is signed in to, so a question,
    /// such as a new host key, can be asked in this window.
    private func tryAgain() {
        if case .ssh = connection.connector {
            Task { await signIn.signIn(connection) }
        } else {
            connection.checkNow()
        }
    }

    private func command(_ command: String, prompt: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(prompt).font(.kit(size: 12)).foregroundStyle(theme.muted)
            HStack(spacing: 8) {
                Text(command).font(.kit(size: 12, design: .monospaced)).lineLimit(1).truncationMode(.middle)
                    .padding(.horizontal, 10).frame(maxWidth: .infinity, minHeight: 28, alignment: .leading)
                    .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
                    .overlay { RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border) }
                    .help(command)
                Button("Copy") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(command, forType: .string)
                }.buttonStyle(FormButtonStyle())
                Button(action: tryAgain) {
                    if signIn.signingIn { KitSpinner() } else { Text("Try Again") }
                }
                .buttonStyle(FormButtonStyle(fill: .primary))
                .keyboardShortcut(.defaultAction)
                .disabled(signIn.signingIn)
            }
        }
    }

    private func failure(source: String?, reason: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            if let source { Text(source).font(.kit(size: 11)).foregroundStyle(theme.muted) }
            Text(reason).font(.kit(size: 12, design: .monospaced)).fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 10).padding(.horizontal, 12)
        .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
        .overlay { RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border) }
    }

    private func versions(_ versions: [ServerStatePresentation.Version]) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(versions, id: \.label) { version in
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    Text(version.label).font(.kit(size: 11)).foregroundStyle(theme.muted)
                        .lineLimit(1).frame(width: 64, alignment: .leading)
                    Text(version.value).font(.kit(size: 11, design: .monospaced))
                        .foregroundStyle(version.outdated ? theme.warning : theme.text)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 10).padding(.horizontal, 12)
        .background(theme.raised, in: RoundedRectangle(cornerRadius: 3))
        .overlay { RoundedRectangle(cornerRadius: 3).strokeBorder(theme.border) }
    }

    @ViewBuilder private func action(_ action: ServerStatePresentation.Action) -> some View {
        switch action {
        case .retry(let retry):
            HStack(spacing: 8) {
                Circle().strokeBorder(theme.muted).frame(width: 6, height: 6)
                RetryCountdown(retry: retry).font(.kit(size: 12)).foregroundStyle(theme.muted)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Button("Server Settings…") { SettingsSection.servers.open(with: openSettings, server: connection.name) }
                    .buttonStyle(FormButtonStyle())
                Button("Try Now", action: tryAgain)
                    .buttonStyle(FormButtonStyle(fill: .primary))
                    .keyboardShortcut(.defaultAction)
                    .disabled(retry == nil || signIn.signingIn)
            }
        case .restart(let command, let prompt), .removeHostKey(let command, let prompt):
            self.command(command, prompt: prompt)
        case .startServer(let note):
            HStack(spacing: 10) {
                Button {
                    Task { await signIn.signIn(connection, start: true) }
                } label: {
                    if signIn.signingIn { KitSpinner("Starting…") } else { Text("Start Server") }
                }
                .buttonStyle(FormButtonStyle(fill: .primary))
                .keyboardShortcut(.defaultAction)
                .disabled(signIn.signingIn)
                Text(note).font(.kit(size: 12)).foregroundStyle(theme.muted).fixedSize(horizontal: false, vertical: true)
            }
        case .signIn:
            HStack(spacing: 8) {
                Button {
                    Task { await signIn.signIn(connection) }
                } label: {
                    if signIn.signingIn { KitSpinner("Signing in…") } else { Text("Sign In…") }
                }
                .buttonStyle(FormButtonStyle(fill: .primary))
                .keyboardShortcut(.defaultAction)
                .disabled(signIn.signingIn)
                Button("Server Settings…") { SettingsSection.servers.open(with: openSettings, server: connection.name) }
                    .buttonStyle(FormButtonStyle())
            }
        case .update:
            HStack(spacing: 8) {
                Button("Get the Latest Kit") { openURL(ServerStatePresentation.releasesURL) }
                    .buttonStyle(FormButtonStyle(fill: .primary))
                    .keyboardShortcut(.defaultAction)
                Text("Opens the releases page.").font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
        }
    }
}

/// Counts down to the next check of a server, once a second.
private struct RetryCountdown: View {
    let retry: Date?

    var body: some View {
        if let retry {
            TimelineView(.periodic(from: .now, by: 1)) { context in
                Text(Self.label(until: retry, now: context.date))
            }
        } else {
            Text("Trying now…")
        }
    }

    static func label(until retry: Date, now: Date) -> String {
        let seconds = max(0, Int(retry.timeIntervalSince(now).rounded(.up)))
        return seconds == 0 ? "Trying now…" : String(format: "Trying again in %d:%02d", seconds / 60, seconds % 60)
    }
}

/// A tab whose server isn't saved any more, as when it was removed with
/// `kit servers remove`.
struct UnknownServerView: View {
    @Environment(\.mica) private var theme
    let name: String
    let close: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 8) {
                    IconView(.warning, size: 16).foregroundStyle(theme.warning)
                    Text("\(name) isn’t a saved server").font(.kit(size: 17, weight: .semibold))
                }
                Text("This tab’s session is on a server Kit no longer knows. Save the server again with kit servers add, or close this tab.")
                    .font(.kit(size: 13)).foregroundStyle(theme.muted)
                    .lineSpacing(3).fixedSize(horizontal: false, vertical: true)
            }
            Button("Close Tab", action: close).buttonStyle(FormButtonStyle(fill: .primary))
        }
        .frame(width: ServerStateView.width, alignment: .leading)
        .padding(.top, SessionLauncherContent.top).padding(.bottom, 40)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
    }
}

/// A tab whose served server needs this Mac to pair with it again (S3): a
/// field for a new pairing link from that server.
struct ServerPairingView: View {
    @Environment(\.mica) private var theme
    let connection: ServerConnection
    let need: ClientError.PairingNeed
    let servers: ServerDirectory
    var sessionTab = true

    var body: some View {
        let name = connection.name
        VStack(alignment: .leading, spacing: 18) {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 8) {
                    IconView(.warning, size: 16).foregroundStyle(theme.warning)
                    Text(Self.title(name: name, need: need)).font(.kit(size: 17, weight: .semibold))
                }
                Text(Self.message(name: name, need: need))
                    .font(.kit(size: 13)).foregroundStyle(theme.muted)
                    .lineSpacing(3).fixedSize(horizontal: false, vertical: true)
            }
            PairingLinkField(placeholder: Self.placeholder(connection.connector)) { link in
                try await servers.pairAgain(name, link: link)
            }
            Text(sessionTab
                 ? "Your draft and layout stay in this tab. Tabs on other servers keep working."
                 : "Tabs on other servers keep working.")
                .font(.kit(size: 12)).foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(width: ServerStateView.width, alignment: .leading)
        .padding(.top, SessionLauncherContent.top).padding(.bottom, 40)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
    }

    static func title(name: String, need: ClientError.PairingNeed) -> String {
        need == .missing ? "Pair with \(name)" : "Pair with \(name) again"
    }

    static func message(name: String, need: ClientError.PairingNeed) -> String {
        let steps = "by running kit server pair on \(name), or with Pair a Device on another paired device, and paste it here."
        return need == .missing
            ? "This Mac isn’t paired with \(name). Get a link \(steps)"
            : "This Mac’s pairing with \(name) expired or was revoked. Get a new link \(steps)"
    }

    /// The shape of a link for the server, to show what to paste.
    static func placeholder(_ connector: ServerConnector) -> String {
        if case .url(let origin, _) = connector { return origin + "/pair#…" }
        return "https://host/pair#…"
    }
}

/// A pairing link field with a Pair button, which reports why a link failed.
struct PairingLinkField: View {
    @Environment(\.mica) private var theme
    let placeholder: String
    /// Takes focus when it appears, as the only field in a dialog does.
    var autofocus = false
    let pair: (PairingLink) async throws -> Void
    @State private var text = ""
    @State private var pending = false
    @State private var error: String?
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                TextField("", text: $text, prompt: Text(placeholder).foregroundStyle(theme.strongBorder))
                    .textFieldStyle(.plain).font(.kit(size: 12, design: .monospaced))
                    .focused($focused)
                    .onSubmit(submit)
                    .padding(.horizontal, 10).frame(height: 30)
                    .fieldBox(theme, focused: focused)
                    .accessibilityLabel("Pairing link")
                Button(action: submit) {
                    if pending { KitSpinner() } else { Text("Pair") }
                }
                .buttonStyle(FormButtonStyle(fill: .primary))
                .disabled(pending || text.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            if let error { ProviderErrorLine(message: error) }
        }
        .onChange(of: text) { error = nil }
        .task { if autofocus { focused = true } }
    }

    private func submit() {
        guard !pending else { return }
        guard let link = PairingLink(text) else {
            error = PairingError.invalidLink.localizedDescription
            return
        }
        pending = true
        Task {
            defer { pending = false }
            do {
                try await pair(link)
                text = ""
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
