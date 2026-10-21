import AppKit
import SwiftUI

/// Settings › Servers: the server on this Mac and its Kit version. When a
/// newer `kit` release exists, Update… runs `kit update` in a terminal tab,
/// which asks before restarting the server (ADR 0048). The app never updates
/// or restarts a server itself.
struct ServersSettingsView: View {
    @Environment(\.mica) private var theme
    let windows: SessionWindowRegistry
    var updater: AppUpdater = .shared
    @State private var server = LocalServerStatus.read()
    @State private var problem: KitUpdateTerminal.Problem?

    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Servers").font(.kit(size: 23, weight: .semibold))
                Text("The Kit server this app attaches to.").foregroundStyle(theme.muted)
            }
            row
                .clipShape(RoundedRectangle(cornerRadius: 4))
                .overlay { RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border) }
            Text(problem?.message
                 ?? "Update… opens a terminal tab that runs kit update on this Mac. It asks before restarting the server, so running turns aren’t cut off. Servers on other machines are updated there.")
                .font(.kit(size: 12)).foregroundStyle(problem == nil ? theme.muted : theme.warning)
                .fixedSize(horizontal: false, vertical: true)
        }
        .task { await refresh() }
    }

    private var row: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 2) {
                Text("local")
                HStack(spacing: 6) {
                    Text(["This Mac", server.home, server.version.map { "Kit \($0)" }].compactMap { $0 }.joined(separator: " · "))
                        .foregroundStyle(theme.muted)
                    if let newer { Text("\(newer.version) available").foregroundStyle(theme.accent) }
                }
                .font(.kit(size: 12))
            }
            Spacer(minLength: 0)
            HStack(spacing: 6) {
                Circle().fill(server.running ? theme.success : theme.muted).frame(width: 6, height: 6)
                Text(server.running ? "Running" : "Not running").font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
            .frame(width: 110, alignment: .leading)
            if newer != nil {
                Button("Update…") { problem = KitUpdateTerminal.open(in: windows) }
                    .buttonStyle(MicaButtonStyle(compact: true))
            }
        }
        .padding(.horizontal, 14).padding(.vertical, 10)
        .frame(minHeight: 52)
    }

    /// The newest `kit` release when it is newer than the running server.
    private var newer: KitRelease? {
        guard server.running, let version = server.version.flatMap(ReleaseVersion.init),
              let newest = updater.newestCLI, newest.version > version else { return nil }
        return newest
    }

    private func refresh() async {
        server = LocalServerStatus.read()
        if updater.newestCLI == nil { await updater.check() }
    }
}

/// The server registered in this app's Kit home, read without contacting it.
struct LocalServerStatus: Equatable {
    /// The home, abbreviated with ~.
    var home: String
    var running: Bool
    var version: String?

    static func read(home: URL = DevHomeResolver.resolve()) -> Self {
        let shown = (home.path as NSString).abbreviatingWithTildeInPath
        let file = home.appendingPathComponent("run").appendingPathComponent("server.json")
        guard let data = try? Data(contentsOf: file),
              let registry = try? JSONDecoder().decode(LocalDaemonRegistry.self, from: data),
              registry.pid > 0, kill(Int32(registry.pid), 0) == 0 else {
            return Self(home: shown, running: false, version: nil)
        }
        return Self(home: shown, running: true, version: registry.kitVersion)
    }
}

/// Runs `kit update` in a terminal tab of the frontmost session window.
enum KitUpdateTerminal {
    /// Why the tab could not open.
    enum Problem: Equatable {
        case noSession
        case unusualHome

        var message: String {
            switch self {
            case .noSession: "Open a session first: Update… runs kit update in one of its terminal tabs."
            case .unusualHome: "Kit’s home path can’t be typed into a terminal. Run kit update in a terminal yourself."
            }
        }
    }

    /// The command typed into the shell. It names the app's home, so the
    /// update offers to restart that home's server whatever the shell
    /// exports. A path with control characters can't be typed safely.
    static func command(home: URL = DevHomeResolver.resolve()) -> String? {
        let path = home.path
        guard !path.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) }) else { return nil }
        return "KIT_HOME='\(path.replacingOccurrences(of: "'", with: "'\\''"))' kit update\n"
    }

    /// Opens the tab and brings its window forward.
    @MainActor
    static func open(in windows: SessionWindowRegistry) -> Problem? {
        guard let command = command() else { return .unusualHome }
        guard let model = windows.frontmost(where: { $0.selectedTab.store?.selected != nil }),
              let store = model.selectedTab.store else { return .noSession }
        store.ui.workspace.openTerminal(directory: NSHomeDirectory(), initialInput: command)
        model.window?.makeKeyAndOrderFront(nil)
        return nil
    }
}
