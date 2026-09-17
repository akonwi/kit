import AppKit
import SwiftUI

/// Route AppKit's tab-bar action before SwiftUI's window controller consumes it.
@objc(KitApplication)
final class KitApplication: NSApplication {
    weak var sessionWindows: SessionWindowRegistry?

    override func sendAction(_ action: Selector, to target: Any?, from sender: Any?) -> Bool {
        if action == #selector(NSResponder.newWindowForTab(_:)), let sessionWindows {
            let source = (sender as? NSView)?.window ?? (sender as? NSWindow) ?? keyWindow ?? mainWindow
            sessionWindows.showLauncher(from: source)
            return true
        }
        return super.sendAction(action, to: target, from: sender)
    }
}

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
    @objc func newWindowForTab(_ sender: Any?) { sessionWindows.showLauncher() }
    func applicationDidFinishLaunching(_ notification: Notification) {
        (NSApp as? KitApplication)?.sessionWindows = sessionWindows
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
    }
}

@main
struct KitApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    private var windows: SessionWindowRegistry { delegate.sessionWindows }

    init() {
        EditorAccessibility.install()
        Typography.registerFonts()
        ThemeConfiguration.migrate(in: .standard)
    }

    var body: some Scene {
        WindowGroup("Kit", id: "session", for: SessionWindowRequest.self) { $request in
            SessionWindowRoot(request: $request, windows: windows)
        }
        defaultValue: { windows.defaultRequest() }

        .defaultSize(width: 1220, height: 880)
        .windowResizability(.contentMinSize)
        .windowToolbarStyle(.unified)
        .commands { SessionCommands(windows: windows) }
        Settings { SettingsView() }
    }
}

private struct SessionCommands: Commands {
    let windows: SessionWindowRegistry
    @Environment(\.openWindow) private var openWindow
    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button("New Tab") {
                windows.showLauncher { openWindow(id: "session", value: $0) }
            }.keyboardShortcut("t", modifiers: .command)
        }
    }
}
