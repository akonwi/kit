import AppKit
import SwiftUI

/// The bundle's principal class.
@objc(KitApplication)
final class KitApplication: NSApplication {}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    // The delegate owns the registry so termination never depends on casting
    // SwiftUI's NSApp.delegate wrapper back to our adapted delegate.
    let sessionWindows: SessionWindowRegistry
    override init() {
        sessionWindows = SessionWindowRegistry()
        super.init()
    }
    init(sessionWindows: SessionWindowRegistry) {
        self.sessionWindows = sessionWindows
        super.init()
    }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        sessionWindows.terminating = true
        return .terminateNow
    }
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSWindow.allowsAutomaticWindowTabbing = false
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
    }
}

@main
struct KitApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var themeLibrary: NativeThemeLibrary
    private var windows: SessionWindowRegistry { delegate.sessionWindows }

    init() {
        EditorAccessibility.install()
        Typography.registerFonts()
        ThemeConfiguration.migrate(in: .standard)
        let library = NativeThemeLibrary()
        var configuration = ThemeConfiguration.decode(UserDefaults.standard.string(forKey: "themeConfiguration") ?? "")
        if configuration.normalize(installed: library.themes) {
            UserDefaults.standard.set(configuration.json, forKey: "themeConfiguration")
        }
        _themeLibrary = State(initialValue: library)
    }

    var body: some Scene {
        WindowGroup("Kit", id: "session", for: SessionWindowRequest.self) { $request in
            SessionWindowRoot(request: $request, windows: windows)
                .environment(\.installedThemes, themeLibrary.themes)
        }
        defaultValue: { windows.defaultRequest() }

        .defaultSize(width: 1220, height: 880)
        .windowResizability(.contentMinSize)
        .windowStyle(.hiddenTitleBar)
        .commands { SessionCommands(windows: windows) }
        Settings {
            SettingsView(themeLibrary: themeLibrary)
                .environment(\.installedThemes, themeLibrary.themes)
        }
    }
}

private struct SessionCommands: Commands {
    let windows: SessionWindowRegistry
    @Environment(\.openWindow) private var openWindow
    @FocusedValue(\.sessionWindow) private var window
    @AppStorage(SessionTabPlacement.storageKey) private var placement = SessionTabPlacement.side

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button("New Tab") { window?.openLauncher() }
                .keyboardShortcut("t", modifiers: .command)
                .disabled(window == nil)
            Button("New Window") { openWindow(id: "session", value: SessionWindowRequest(windowID: UUID())) }
                .keyboardShortcut("n", modifiers: .command)
            Divider()
            Button("Close Tab") { closeTab() }
                .keyboardShortcut("w", modifiers: .command)
                .disabled(window == nil)
        }
        CommandMenu("Session") {
            Button("Show Previous Tab") { window?.selectAdjacent(-1) }
                .keyboardShortcut("[", modifiers: [.command, .shift])
                .disabled((window?.tabs.count ?? 0) < 2)
            Button("Show Next Tab") { window?.selectAdjacent(1) }
                .keyboardShortcut("]", modifiers: [.command, .shift])
                .disabled((window?.tabs.count ?? 0) < 2)
            Divider()
            ForEach(1...9, id: \.self) { number in
                Button("Show Tab \(number)") { window?.select(index: number - 1) }
                    .keyboardShortcut(KeyEquivalent(Character("\(number)")), modifiers: .command)
                    .disabled((window?.tabs.count ?? 0) < number)
            }
            Divider()
            Picker("Session Tabs", selection: $placement) {
                Text("In Sidebar").tag(SessionTabPlacement.side)
                Text("On Top").tag(SessionTabPlacement.top)
            }
        }
    }

    /// Closes the selected tab; a window showing only the launcher closes.
    private func closeTab() {
        guard let window else { return }
        if window.tabs.count == 1, window.selectedTab.isLauncher {
            window.window?.performClose(nil)
        } else {
            window.close(window.selectedTab)
        }
    }
}
