import AppKit
import SwiftUI

struct RootHost: View {
    let model: AppModel
    var body: some View {
        RootView(theme: model.theme).environment(model)
    }
}

@MainActor final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    let model = AppModel()
    var window: NSWindow!

    func applicationDidFinishLaunching(_ notification: Notification) {
        Fonts.register()
        do {
            try GhosttyRuntime.shared.start(configPath: ShellTheme.writeGhosttyConfig(model.theme))
        } catch {
            NSLog("spike: ghostty start failed: %@", "\(error)")
        }
        NativeViews.shared.model = model
        GhosttyRuntime.shared.delegate = SurfaceRouter.shared
        for (name, focused) in [(NSApplication.didBecomeActiveNotification, true), (NSApplication.didResignActiveNotification, false)] {
            NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { _ in
                GhosttyRuntime.shared.setFocus(focused)
            }
        }

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1440, height: 900),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered, defer: false)
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        window.title = "Kit"
        window.isMovableByWindowBackground = false
        window.minSize = NSSize(width: 820, height: 520)
        window.delegate = self
        window.contentView = NSHostingView(rootView: RootHost(model: model))
        window.center()
        window.setFrameAutosaveName("KitNativeSpike")
        AppWindow.window = window
        NSApp.mainMenu = buildMenu()
        if ProcessInfo.processInfo.environment["SPIKE_BACKGROUND"] != nil {
            // Screenshot runs: don't steal focus from whoever is typing.
            window.orderBack(nil)
        } else {
            window.makeKeyAndOrderFront(nil)
            NSApp.activate()
        }
        TrafficLights.place(in: window)
        DispatchQueue.main.async { self.model.focusGeneration += 1 }
        applyDemoEnvironment()
    }

    /// Launch-time state for screenshot checks without synthetic input:
    /// SPIKE_THEME=<id>, SPIKE_TABS=top, SPIKE_DEMO=palette,toasts,terminal,browser,zoom,question,split,drag
    private func applyDemoEnvironment() {
        let env = ProcessInfo.processInfo.environment
        if let id = env["SPIKE_THEME"], let theme = Theme.all.first(where: { $0.id == id }) { model.theme = theme }
        if env["SPIKE_TABS"] == "top" { model.sideTabs = false }
        let demos = Set((env["SPIKE_DEMO"] ?? "").split(separator: ",").map(String.init))
        guard !demos.isEmpty else { return }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1) { [model] in
            let session = model.current
            let bottom = session.root.tileIDs.last!
            let top = session.root.tileIDs[1]
            if demos.contains("terminal"), let tile = session.tiles[bottom] { model.select(pane: tile.panes[1], in: bottom) }
            if demos.contains("browser"), let tile = session.tiles[top] { model.select(pane: tile.panes[2], in: top) }
            if demos.contains("split") { model.split(.horizontal) }
            if demos.contains("zoom") { model.focus(tile: bottom); model.toggleZoom() }
            if demos.contains("question") { model.askDemo() }
            if demos.contains("toasts") { for _ in 0..<(demos.contains("fold") ? 6 : 3) { model.demoToasts() } }
            if demos.contains("palette") { FocusBridge.save(); model.paletteOpen = true }
            if demos.contains("move"), let tile = session.tiles[bottom] {
                model.move(pane: tile.panes[1], from: bottom, to: session.root.tileIDs[0], edge: .bottom)
            }
            if demos.contains("switch") {
                model.switchSession(1)
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.8) { model.switchSession(0) }
            }
            if demos.contains("drag"), let tile = session.tiles[bottom] {
                let frame = model.tileFrames[top] ?? .zero
                model.drag = .init(pane: tile.panes[1], source: bottom,
                                   location: CGPoint(x: frame.maxX - 40, y: frame.midY),
                                   target: top, edge: .right)
            }
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func windowDidResize(_ notification: Notification) { TrafficLights.place(in: window) }
    func windowDidBecomeKey(_ notification: Notification) { TrafficLights.place(in: window) }
    func windowDidExitFullScreen(_ notification: Notification) { TrafficLights.place(in: window) }

    // MARK: Menu

    private func buildMenu() -> NSMenu {
        let main = NSMenu()
        func submenu(_ title: String, _ items: [NSMenuItem]) {
            let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
            let menu = NSMenu(title: title)
            items.forEach(menu.addItem)
            item.submenu = menu
            main.addItem(item)
        }
        func item(_ title: String, _ key: String, _ mods: NSEvent.ModifierFlags = .command, _ run: @escaping (AppModel) -> Void) -> NSMenuItem {
            let item = ClosureMenuItem(title: title, key: key, mods: mods) { [model] in run(model) }
            return item
        }

        submenu("Kit", [
            NSMenuItem(title: "Hide Kit", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h"),
            .separator(),
            NSMenuItem(title: "Quit Kit", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"),
        ])
        submenu("Edit", [
            NSMenuItem(title: "Undo", action: Selector(("undo:")), keyEquivalent: "z"),
            NSMenuItem(title: "Redo", action: Selector(("redo:")), keyEquivalent: "Z"),
            .separator(),
            NSMenuItem(title: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x"),
            NSMenuItem(title: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c"),
            NSMenuItem(title: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v"),
            NSMenuItem(title: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a"),
        ])
        submenu("Session", [
            item("New Session", "n") { $0.newSession() },
            .separator(),
        ] + (1...9).map { i in item("Session \(i)", "\(i)") { $0.switchSession(i - 1) } } + [
            .separator(),
            item("Ask a Question (demo)", "i", [.command, .shift]) { $0.askDemo() },
            item("Raise a Toast (demo)", "t", [.command, .shift]) { $0.demoToasts() },
        ])
        submenu("Tile", [
            item("New Terminal Tab", "t") { $0.open(.terminal, title: "Terminal") },
            item("New Browser Tab", "b", [.command, .shift]) {
                $0.open(.browser(URL(string: "https://developer.apple.com/documentation/swiftui")!), title: "Browser")
            },
            item("New Scratchpad Tab", "e", [.command, .shift]) { $0.open(.scratchpad, title: "Scratchpad") },
            .separator(),
            item("Split Right", "d") { $0.split(.horizontal) },
            item("Split Down", "d", [.command, .shift]) { $0.split(.vertical) },
            item("Zoom Tile", "\r", [.command, .shift]) { $0.toggleZoom() },
            item("Equalize Splits", "=", [.command, .control]) { $0.equalize() },
            .separator(),
            item("Close Tab", "w") { $0.closeSelected() },
        ])
        let themes = Theme.all.map { theme in item(theme.name, "") { $0.theme = theme } }
        submenu("View", [
            item("Command Palette", "k") { model in
                if !model.paletteOpen { FocusBridge.save() }
                model.paletteOpen.toggle()
            },
            item("Command Palette ", "p") { model in
                if !model.paletteOpen { FocusBridge.save() }
                model.paletteOpen.toggle()
            },
            item("Toggle Side/Top Tabs", "\\") { $0.sideTabs.toggle() },
            .separator(),
        ] + themes)
        submenu("Window", [
            NSMenuItem(title: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m"),
            NSMenuItem(title: "Zoom", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: ""),
        ])
        return main
    }
}

final class ClosureMenuItem: NSMenuItem {
    private let run: () -> Void

    init(title: String, key: String, mods: NSEvent.ModifierFlags, run: @escaping () -> Void) {
        self.run = run
        super.init(title: title, action: #selector(fire), keyEquivalent: key)
        keyEquivalentModifierMask = mods
        target = self
    }

    required init(coder: NSCoder) { fatalError() }

    @objc private func fire() { run() }
}

/// Places the standard traffic lights in the 44 pt chrome row the design
/// uses (16 pt from the left edge, vertically centred).
@MainActor enum TrafficLights {
    static let rowHeight: CGFloat = 44
    private static var observing = false
    private static var placing = false

    /// AppKit re-lays out the titlebar on activation and resize; follow it.
    private static func observe(_ views: [NSView], window: NSWindow) {
        guard !observing else { return }
        observing = true
        for view in views {
            view.postsFrameChangedNotifications = true
            NotificationCenter.default.addObserver(forName: NSView.frameDidChangeNotification, object: view, queue: .main) { _ in
                MainActor.assumeIsolated { place(in: window) }
            }
        }
    }

    static func place(in window: NSWindow) {
        guard let close = window.standardWindowButton(.closeButton),
              let mini = window.standardWindowButton(.miniaturizeButton),
              let zoom = window.standardWindowButton(.zoomButton),
              let titlebar = close.superview, let container = titlebar.superview,
              !window.styleMask.contains(.fullScreen), !placing else { return }
        placing = true
        defer { placing = false }
        observe([container, titlebar, close], window: window)
        let height = window.frame.height
        container.frame = NSRect(x: 0, y: height - rowHeight, width: window.frame.width, height: rowHeight)
        titlebar.frame = container.bounds
        let spacing = mini.frame.minX - close.frame.minX
        for (index, button) in [close, mini, zoom].enumerated() {
            button.setFrameOrigin(NSPoint(
                x: 16 + CGFloat(index) * spacing,
                y: ((rowHeight - button.frame.height) / 2).rounded()))
        }
        if ProcessInfo.processInfo.environment["SPIKE_DEBUG"] != nil {
            NSLog("lights container=%@ %@ titlebar=%@ %@ close=%@ flipped=%d", "\(type(of: container))", "\(container.frame)",
                  "\(type(of: titlebar))", "\(titlebar.frame)", "\(close.frame)", titlebar.isFlipped ? 1 : 0)
        }
    }
}
