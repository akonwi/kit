import AppKit
import SwiftUI

/// What a tab says in place of its content while its server can't serve it:
/// the server can't be reached (S13), it is older than the app (S14), or the
/// app is older than it (S15).
struct ServerStatePresentation: Equatable {
    enum Action: Equatable {
        /// The server is checked again at `retry`, or now when nil.
        case retry(Date?)
        /// The user restarts the server with `command` once it is updated.
        case restart(command: String, prompt: String)
        /// The user installs a newer app.
        case update
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

    init(name: String, connector: ServerConnector, status: ServerStatus, sessionTab: Bool,
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
                                  prompt: "After updating kit, run:")
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
                : "Kit couldn’t reach \(name). It keeps trying, and this tab picks up where it left off when \(name) is back."
            source = Self.source(name: name, connector: connector)
            reason = failure
            versions = []
            action = .retry(retry)
        case .connecting, .connected, .reconnecting:
            title = local ? "Connecting to Kit" : "Connecting to \(name)"
            message = ""
            source = nil
            reason = nil
            versions = []
            action = .retry(nil)
        }
    }

    /// The connector and its address, such as `home · ~/dev/kit`.
    static func source(name: String, connector: ServerConnector) -> String {
        switch connector {
        case .home(let home):
            let kind = name == ServerClient.localID ? "local" : "home"
            return kind + " · " + abbreviated(home.path)
        }
    }

    /// The command that restarts a server once it is updated. A home other
    /// than the default is named, so the command restarts that server.
    static func restartCommand(connector: ServerConnector) -> String {
        switch connector {
        case .home(let home):
            let path = home.standardizedFileURL.path
            guard path != DevHomeResolver.defaultHome.standardizedFileURL.path else { return "kit server restart" }
            return "KIT_HOME=\(shellQuoted(path)) kit server restart"
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
    let connection: ServerConnection
    var sessionTab = true

    var body: some View {
        let presentation = ServerStatePresentation(name: connection.name, connector: connection.connector,
                                                   status: connection.status, sessionTab: sessionTab)
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
        .frame(width: 520, alignment: .leading)
        .padding(.top, 150).padding(.bottom, 40)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .textSelection(.enabled)
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
                Button("Try Now") { connection.checkNow() }
                    .buttonStyle(FormButtonStyle(fill: .primary))
                    .keyboardShortcut(.defaultAction)
                    .disabled(retry == nil)
            }
        case .restart(let command, let prompt):
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
                    Button("Try Again") { connection.checkNow() }
                        .buttonStyle(FormButtonStyle(fill: .primary))
                        .keyboardShortcut(.defaultAction)
                }
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
        .frame(width: 520, alignment: .leading)
        .padding(.top, 150).padding(.bottom, 40)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
    }
}
