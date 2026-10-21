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
        sessionWindows = SessionWindowRegistry(servers: ServerDirectory())
        super.init()
    }
    init(sessionWindows: SessionWindowRegistry) {
        self.sessionWindows = sessionWindows
        super.init()
    }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        // Quitting ends every shell; ask first if a command is running.
        let busy = sessionWindows.liveModels.flatMap(\.busyTerminals)
        if !busy.isEmpty, TerminalCloseConfirmation.makeAlert(busy, scope: .quit).runModal() != .alertFirstButtonReturn {
            return .terminateCancel
        }
        sessionWindows.captureBeforeQuit()
        sessionWindows.terminating = true
        return .terminateNow
    }
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSWindow.allowsAutomaticWindowTabbing = false
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
        // Saved servers connect when Kit opens, whether or not a tab uses them.
        sessionWindows.servers.start()
    }
    func applicationDidBecomeActive(_ notification: Notification) {
        sessionWindows.servers.refresh()
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
                .environment(\.reloadThemes, { [themeLibrary] in themeLibrary.reload() })
        }
        defaultValue: { windows.defaultRequest() }

        .defaultSize(width: 1220, height: 880)
        .windowResizability(.contentMinSize)
        .windowStyle(.hiddenTitleBar)
        .commands {
            SessionCommands(windows: windows)
            TileCommands()
        }
        Settings {
            SettingsView(themeLibrary: themeLibrary)
                .environment(\.installedThemes, themeLibrary.themes)
                .environment(\.serverDirectory, windows.servers)
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
            Button("Close Session Tab") { closeSessionTab() }
                .keyboardShortcut("w", modifiers: [.command, .shift])
                .disabled(window == nil)
        }
        CommandMenu("Session") {
            Button("Command Palette…") { store?.ui.palette = true }
                .keyboardShortcut("p", modifiers: .command)
                .disabled(store == nil)
            Button("Subagents…") {
                store?.ui.subagentsPresented = true
                store?.ui.palette = true
            }.disabled(store?.selected == nil)
            Divider()
            let navigable = window?.navigableTabs(placement).count ?? 0
            Button("Show Previous Tab") { window?.selectAdjacent(-1, in: placement) }
                .keyboardShortcut("[", modifiers: [.command, .shift])
                .disabled(navigable < 2)
            Button("Show Next Tab") { window?.selectAdjacent(1, in: placement) }
                .keyboardShortcut("]", modifiers: [.command, .shift])
                .disabled(navigable < 2)
            Divider()
            let numbered = window?.numberedRows(placement).count ?? 0
            ForEach(1...9, id: \.self) { number in
                Button("Show Tab \(number)") {
                    // A closed parent opens as clicking its row does.
                    if let window, let parent = window.select(index: number - 1, in: placement) {
                        windows.open(parent, from: window)
                    }
                }
                .keyboardShortcut(KeyEquivalent(Character("\(number)")), modifiers: .command)
                .disabled(numbered < number)
            }
            Divider()
            Picker("Session Tabs", selection: $placement) {
                Text("In Sidebar").tag(SessionTabPlacement.side)
                Text("On Top").tag(SessionTabPlacement.top)
            }
        }
    }

    private var store: SessionStore? { window?.selectedTab.store }

    /// Closes the focused tile's selected tab. The Agent tab can't close, so
    /// on it, or without a session, this closes the session tab instead.
    private func closeTab() {
        if let store, store.selected != nil, store.ui.workspace.selected.closable {
            store.closePane(store.ui.workspace.selected)
        } else {
            closeSessionTab()
        }
    }

    /// Closes the selected session tab; a window showing only the launcher closes.
    private func closeSessionTab() {
        guard let window else { return }
        if window.tabs.count == 1, window.selectedTab.isLauncher {
            window.window?.performClose(nil)
        } else {
            window.requestClose(window.selectedTab)
        }
    }
}

/// Workspace and tile commands for the selected session. Shortcuts follow
/// Ghostty's split bindings where they overlap.
private struct TileCommands: Commands {
    @FocusedValue(\.sessionWindow) private var window

    /// The selected session's store, once its workspace is showing.
    private var store: SessionStore? {
        guard let store = window?.selectedTab.store, store.selected != nil else { return nil }
        return store
    }
    private var workspace: WorkspaceState? { store?.ui.workspace }

    var body: some Commands {
        CommandMenu("Tile") {
            Button("Open Diff") { workspace?.open(.review) }
                .disabled(!(store?.catalogClient is any DiffClient))
            Button("Open Scratchpad") { workspace?.open(.scratchpad) }
                .disabled(store?.canOpenScratchpad != true)
            Button("Open File…") { store?.showFiles(intent: "open") }
                .keyboardShortcut("o", modifiers: .command)
                .disabled(store == nil || store?.unavailable == true)
            Divider()
            Button("Split Right") { if let workspace { workspace.split(workspace.selected, .right) } }
                .keyboardShortcut("d", modifiers: .command)
                .disabled(!(workspace.map { $0.canSplit($0.selected) } ?? false))
            Button("Split Down") { if let workspace { workspace.split(workspace.selected, .bottom) } }
                .keyboardShortcut("d", modifiers: [.command, .shift])
                .disabled(!(workspace.map { $0.canSplit($0.selected) } ?? false))
            Button(workspace?.zoomedTile == nil ? "Zoom Tile" : "Unzoom Tile") { workspace?.toggleZoom() }
                .keyboardShortcut(.return, modifiers: [.command, .shift])
                .disabled((workspace?.root.tiles.count ?? 0) < 2)
            Button("Equalize Tiles") { workspace?.equalize() }
                .keyboardShortcut("=", modifiers: [.command, .control])
                .disabled((workspace?.root.tiles.count ?? 0) < 2)
            Button("Join All Tabs") { if let workspace { workspace.joinTiles(selecting: workspace.selected) } }
                .disabled((workspace?.root.tiles.count ?? 0) < 2)
            Divider()
            // Directional commands stay enabled and do nothing where there
            // is no tile that way. Gating them on the focused tile left a
            // shortcut disabled when the menu's state wasn't refreshed after
            // focus moved, so ⌘⌥← stopped working after ⌘⌥→.
            Menu("Focus Tile") {
                ForEach(Self.directions, id: \.0) { name, direction, key in
                    Button(name) { workspace?.focus(direction) }
                        .keyboardShortcut(key, modifiers: [.command, .option])
                        .disabled(workspace == nil)
                }
            }
            Menu("Move Tab") {
                ForEach(Self.directions, id: \.0) { name, direction, key in
                    Button(name) { workspace?.moveSelectedTab(direction) }
                        .keyboardShortcut(key, modifiers: [.command, .option, .shift])
                        .disabled(workspace == nil)
                }
            }
            Menu("Resize Tile") {
                ForEach(Self.directions, id: \.0) { name, direction, key in
                    Button("Move Divider \(name)") { workspace?.resize(direction) }
                        .keyboardShortcut(key, modifiers: [.command, .control])
                        .disabled(workspace == nil || workspace?.zoomedTile != nil)
                }
            }
            Divider()
            Button("Show Previous Tab in Tile") { workspace?.selectAdjacentTab(-1) }
                .keyboardShortcut(.tab, modifiers: [.control, .shift])
                .disabled(workspace?.canSelectAdjacentTab() != true)
            Button("Show Next Tab in Tile") { workspace?.selectAdjacentTab(1) }
                .keyboardShortcut(.tab, modifiers: .control)
                .disabled(workspace?.canSelectAdjacentTab() != true)
        }
    }

    private static let directions: [(String, TileDirection, KeyEquivalent)] = [
        ("Left", .left, .leftArrow), ("Right", .right, .rightArrow),
        ("Up", .up, .upArrow), ("Down", .down, .downArrow),
    ]
}
