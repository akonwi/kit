import AppKit
import GhosttyKit

/// Something libghostty reports about one terminal, copied out of the
/// callback that delivered it.
enum TerminalEvent: Sendable {
    case title(String)
    case workingDirectory(String)
    case bell
    case notification(title: String, body: String)
    case openURL(String)
    case mouseShape(ghostty_action_mouse_shape_e)
    case mouseVisible(Bool)
    /// The shell exited.
    case closed
}

/// The process's one libghostty app. It starts with the first terminal,
/// routes libghostty's callbacks to the terminal they concern on the main
/// thread, and keeps every terminal, on screen or not, in the appearance
/// its `TerminalTheme` calls for.
///
/// Callback handling is adapted from Ghostty's macOS app (MIT licensed).
@MainActor
final class TerminalRuntime {
    static let shared = TerminalRuntime(theme: .shared)

    private let theme: TerminalTheme
    private(set) var app: ghostty_app_t?
    private var config: ghostty_config_t?
    private(set) var appearance: TerminalAppearance?
    /// Why libghostty couldn't start, once it has failed.
    private(set) var failure: String?
    /// Live terminals by the userdata pointer libghostty hands back.
    private var views: [UInt: WeakView] = [:]
    private var observers: [NSObjectProtocol] = []

    private final class WeakView {
        weak var value: TerminalSurfaceView?
        init(_ value: TerminalSurfaceView) { self.value = value }
    }

    private final class WeakRuntime {
        weak var value: TerminalRuntime?
        init(_ value: TerminalRuntime) { self.value = value }
    }

    /// Runtimes by the userdata their libghostty app hands back. The app
    /// has one; tests make others.
    private static var running: [Int: WeakRuntime] = [:]
    private static var nextID = 0
    private let id: Int

    init(theme: TerminalTheme) {
        self.theme = theme
        Self.nextID += 1
        id = Self.nextID
        Self.running[id] = WeakRuntime(self)
    }

    /// Starts libghostty in the theme's appearance, then follows the theme.
    /// Returns false if libghostty can't start.
    @discardableResult
    func start() -> Bool {
        if app != nil { return true }
        guard failure == nil else { return false }
        Self.initializeLibrary()
        let appearance = theme.current
        guard let config = Self.makeConfig(appearance) else { failure = "The terminal’s settings couldn’t be loaded."; return false }
        var runtime = Self.runtimeConfig(id: id)
        guard let app = ghostty_app_new(&runtime, config) else {
            ghostty_config_free(config)
            failure = "The terminal couldn’t start."
            return false
        }
        self.app = app
        self.config = config
        self.appearance = appearance
        theme.follow { [weak self] in self?.apply($0) }
        ghostty_app_set_focus(app, NSApp?.isActive == true)
        let center = NotificationCenter.default
        for (name, focused) in [(NSApplication.didBecomeActiveNotification, true),
                                (NSApplication.didResignActiveNotification, false)] {
            observers.append(center.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated {
                    if let app = self?.app { ghostty_app_set_focus(app, focused) }
                }
            })
        }
        return true
    }

    /// Frees libghostty's app once every terminal has closed. The app's
    /// runtime runs until Kit quits; tests stop the ones they make.
    func stop() {
        for observer in observers { NotificationCenter.default.removeObserver(observer) }
        observers = []
        if let app { ghostty_app_free(app) }
        if let config { ghostty_config_free(config) }
        app = nil
        config = nil
        appearance = nil
        Self.running[id] = nil
    }

    /// Applies a new appearance to every terminal, on screen or not, and
    /// tells their programs whether it is light or dark.
    private func apply(_ appearance: TerminalAppearance) {
        guard let app, appearance != self.appearance, let next = Self.makeConfig(appearance) else { return }
        let previous = config
        config = next
        self.appearance = appearance
        // A surface whose scheme changes takes the new configuration in it
        // (see `reloadConfig`), then programs that asked (mode 2031) hear
        // of the change; later queries (CSI ? 996 n) are answered with it.
        // This comes first so no program hears the old scheme in the new
        // colors. The app's own scheme stays light, as the configuration is
        // made: libghostty rebuilds a new surface's configuration when the
        // two differ, dropping its working directory, command, and initial
        // input. A new terminal gets the scheme as it registers.
        let scheme = appearance.colorScheme
        for view in views.values {
            if let surface = view.value?.surface { ghostty_surface_set_color_scheme(surface, scheme) }
        }
        // Every surface takes the new configuration and redraws.
        ghostty_app_update_config(app, next)
        // libghostty copies what it needs, but Ghostty's app keeps the
        // current configuration alive; do the same.
        if let previous { ghostty_config_free(previous) }
    }

    /// A configuration value, such as `background`, read back from the
    /// configuration libghostty was last given.
    func configValue<Value>(_ key: String, as type: Value.Type) -> Value? {
        guard let config else { return nil }
        let pointer = UnsafeMutablePointer<Value>.allocate(capacity: 1)
        defer { pointer.deallocate() }
        guard ghostty_config_get(config, pointer, key, UInt(key.utf8.count)) else { return nil }
        return pointer.pointee
    }

    /// Gives a surface the current configuration again. libghostty asks
    /// for this when a surface's color scheme changes: a configuration takes
    /// the scheme only as it's applied, and the scheme a terminal reports to
    /// programs is the one its configuration was applied in.
    private func reloadConfig(_ surface: ghostty_surface_t) {
        guard let config, views.values.contains(where: { $0.value?.surface == surface }) else { return }
        ghostty_surface_update_config(surface, config)
    }

    func tick() {
        if let app { ghostty_app_tick(app) }
    }

    /// Tracks a new terminal and gives it the current color scheme.
    func register(_ view: TerminalSurfaceView) {
        views[UInt(bitPattern: Unmanaged.passUnretained(view).toOpaque())] = WeakView(view)
        if let surface = view.surface, let appearance {
            ghostty_surface_set_color_scheme(surface, appearance.colorScheme)
        }
    }

    func unregister(_ view: TerminalSurfaceView) {
        views[UInt(bitPattern: Unmanaged.passUnretained(view).toOpaque())] = nil
    }

    /// The terminal with libghostty's userdata `key`, in whichever runtime
    /// holds it.
    private static func view(_ key: UInt) -> TerminalSurfaceView? {
        for runtime in running.values {
            if let view = runtime.value?.views[key]?.value { return view }
        }
        return nil
    }

    // MARK: Setup

    private static var initialized = false

    /// Runs libghostty's one-time setup. Its resources (terminfo and shell
    /// integration) come from this bundle, never an inherited location.
    private static func initializeLibrary() {
        guard !initialized else { return }
        initialized = true
        if let resources = Bundle.main.resourceURL?.appendingPathComponent("ghostty"),
           FileManager.default.fileExists(atPath: resources.appendingPathComponent("shell-integration").path) {
            setenv("GHOSTTY_RESOURCES_DIR", resources.path, 1)
        } else {
            unsetenv("GHOSTTY_RESOURCES_DIR")
        }
        _ = ghostty_init(UInt(CommandLine.argc), CommandLine.unsafeArgv)
    }

    /// A finalized configuration from `appearance` alone. Without the
    /// scheme themes, terminals still start; they just report light.
    static func makeConfig(_ appearance: TerminalAppearance,
                           schemes directory: URL = schemesDirectory) -> ghostty_config_t? {
        initializeLibrary()
        guard let config = ghostty_config_new() else { return nil }
        let file = FileManager.default.temporaryDirectory
            .appendingPathComponent("kit-terminal-\(UUID().uuidString).conf")
        do {
            var text = appearance.configuration
            if let schemes = try? schemeThemes(in: directory) {
                text += "theme = light:\(schemes.light.path),dark:\(schemes.dark.path)\n"
            }
            try text.write(to: file, atomically: true, encoding: .utf8)
        } catch {
            ghostty_config_free(config)
            return nil
        }
        defer { try? FileManager.default.removeItem(at: file) }
        ghostty_config_load_file(config, file.path)
        ghostty_config_finalize(config)
        for index in 0..<ghostty_config_diagnostics_count(config) {
            let diagnostic = ghostty_config_get_diagnostic(config, index)
            if let message = diagnostic.message { NSLog("Kit terminal configuration: %@", String(cString: message)) }
        }
        return config
    }

    /// Two empty Ghostty themes, one for each color scheme. libghostty
    /// tracks a terminal's light or dark scheme only in a configuration
    /// whose theme differs between them, and reports to programs the scheme
    /// its configuration was applied in. Kit's colors are set after the
    /// theme, so they always win; the themes only make the scheme count.
    /// They stay in place because libghostty reads them again each time a
    /// scheme changes, and are written again, private to the user, for
    /// each configuration in case the system has cleaned them away.
    static func schemeThemes(in directory: URL) throws -> (light: URL, dark: URL) {
        let manager = FileManager.default
        try manager.createDirectory(at: directory, withIntermediateDirectories: true,
                                    attributes: [.posixPermissions: 0o700])
        try manager.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)
        let light = directory.appendingPathComponent("light"), dark = directory.appendingPathComponent("dark")
        for url in [light, dark] {
            try Data().write(to: url, options: .atomic)
            try manager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
        }
        return (light, dark)
    }

    static let schemesDirectory = FileManager.default.temporaryDirectory
        .appendingPathComponent("kit-terminal-schemes", isDirectory: true)

    // MARK: Callbacks

    /// libghostty's callbacks. They are built outside the main actor:
    /// libghostty calls them from its own threads.
    nonisolated private static func runtimeConfig(id: Int) -> ghostty_runtime_config_s {
        ghostty_runtime_config_s(
            userdata: UnsafeMutableRawPointer(bitPattern: id),
            supports_selection_clipboard: false,
            wakeup_cb: { userdata in
                let id = Int(bitPattern: userdata)
                DispatchQueue.main.async {
                    MainActor.assumeIsolated { TerminalRuntime.running[id]?.value?.tick() }
                }
            },
            action_cb: { app, target, action in TerminalRuntime.action(app, target, action) },
            read_clipboard_cb: { userdata, location, state, _, _, _ in
                TerminalRuntime.readClipboard(userdata, location, state)
            },
            confirm_read_clipboard_cb: { userdata, _, state, _ in
                // Reads are denied in the configuration; refuse any that remain.
                let key = UInt(bitPattern: userdata), state = UInt(bitPattern: state)
                TerminalRuntime.onMain {
                    guard let surface = TerminalRuntime.view(key)?.surface,
                          let state = UnsafeMutableRawPointer(bitPattern: state) else { return }
                    ghostty_surface_deny_clipboard_request(surface, state)
                }
            },
            write_clipboard_cb: { _, location, content, count, _ in
                TerminalRuntime.writeClipboard(location, content, count)
            },
            close_surface_cb: { userdata, _ in
                TerminalRuntime.deliver(.closed, to: UInt(bitPattern: userdata))
            }
        )
    }

    /// Runs `work` on the main thread: now if already there, else soon.
    nonisolated private static func onMain(_ work: @escaping @MainActor () -> Void) {
        if Thread.isMainThread {
            MainActor.assumeIsolated(work)
        } else {
            DispatchQueue.main.async { MainActor.assumeIsolated(work) }
        }
    }

    nonisolated private static func deliver(_ event: TerminalEvent, to key: UInt) {
        onMain { TerminalRuntime.view(key)?.receive(event) }
    }

    /// Copies an action's payload and delivers it to its terminal. Payload
    /// pointers are only valid during the callback.
    nonisolated private static func action(_ app: ghostty_app_t?, _ target: ghostty_target_s,
                                           _ action: ghostty_action_s) -> Bool {
        guard target.tag == GHOSTTY_TARGET_SURFACE, let surface = target.target.surface else { return false }
        if action.tag == GHOSTTY_ACTION_RELOAD_CONFIG {
            // libghostty asks while Kit sets a surface's color scheme on the
            // main thread (Kit's configuration binds no reload key). The
            // reload must finish before this returns, so the scheme report
            // libghostty sends next reads the new scheme.
            guard Thread.isMainThread, let app else { return false }
            let id = Int(bitPattern: ghostty_app_userdata(app))
            return MainActor.assumeIsolated {
                guard let runtime = TerminalRuntime.running[id]?.value else { return false }
                runtime.reloadConfig(surface)
                return true
            }
        }
        let key = UInt(bitPattern: ghostty_surface_userdata(surface))
        let event: TerminalEvent
        switch action.tag {
        case GHOSTTY_ACTION_SET_TITLE:
            guard let title = action.action.set_title.title else { return false }
            event = .title(String(cString: title))
        case GHOSTTY_ACTION_PWD:
            guard let pwd = action.action.pwd.pwd else { return false }
            event = .workingDirectory(String(cString: pwd))
        case GHOSTTY_ACTION_RING_BELL:
            event = .bell
        case GHOSTTY_ACTION_DESKTOP_NOTIFICATION:
            let note = action.action.desktop_notification
            event = .notification(title: note.title.map { String(cString: $0) } ?? "",
                                  body: note.body.map { String(cString: $0) } ?? "")
        case GHOSTTY_ACTION_OPEN_URL:
            let payload = action.action.open_url
            guard let pointer = payload.url else { return false }
            let bytes = UnsafeRawBufferPointer(start: pointer, count: Int(payload.len))
            event = .openURL(String(decoding: bytes, as: UTF8.self))
        case GHOSTTY_ACTION_MOUSE_SHAPE:
            event = .mouseShape(action.action.mouse_shape)
        case GHOSTTY_ACTION_MOUSE_VISIBILITY:
            event = .mouseVisible(action.action.mouse_visibility == GHOSTTY_MOUSE_VISIBLE)
        default:
            return false
        }
        deliver(event, to: key)
        return true
    }

    /// Answers a paste with the pasteboard's plain text. Files paste as
    /// their shell-quoted paths.
    nonisolated private static func readClipboard(
        _ userdata: UnsafeMutableRawPointer?, _ location: ghostty_clipboard_e, _ state: UnsafeMutableRawPointer?
    ) -> ghostty_clipboard_read_result_e {
        guard location == GHOSTTY_CLIPBOARD_STANDARD else { return GHOSTTY_CLIPBOARD_READ_UNSUPPORTED }
        let key = UInt(bitPattern: userdata), state = UInt(bitPattern: state)
        onMain {
            guard let surface = TerminalRuntime.view(key)?.surface,
                  let state = UnsafeMutableRawPointer(bitPattern: state) else { return }
            let text = NSPasteboard.general.terminalText() ?? ""
            complete(surface, text: text, state: state)
        }
        return GHOSTTY_CLIPBOARD_READ_STARTED
    }

    private static func complete(_ surface: ghostty_surface_t, text: String, state: UnsafeMutableRawPointer) {
        "text/plain".withCString { mime in
            text.withCString { data in
                var content = ghostty_clipboard_content_s(mime: mime, data: data, len: text.utf8.count)
                withUnsafePointer(to: &content) { pointer in
                    var request = ghostty_clipboard_complete_s(
                        contents: pointer, contents_len: 1, available: nil, available_len: 0,
                        confirmed: false, remember: false)
                    ghostty_surface_complete_clipboard_request(surface, &request, state)
                }
            }
        }
    }

    nonisolated private static func writeClipboard(
        _ location: ghostty_clipboard_e, _ content: UnsafePointer<ghostty_clipboard_content_s>?, _ count: Int
    ) {
        guard location == GHOSTTY_CLIPBOARD_STANDARD, let content else { return }
        var text: String?
        for index in 0..<count {
            let item = content[index]
            guard let mime = item.mime, String(cString: mime) == "text/plain", let data = item.data else { continue }
            text = String(decoding: UnsafeRawBufferPointer(start: data, count: item.len), as: UTF8.self)
        }
        guard let text else { return }
        onMain {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(text, forType: .string)
        }
    }
}

extension NSPasteboard {
    /// Plain text for a terminal: files become shell-quoted paths.
    func terminalText() -> String? {
        if let urls = readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL],
           !urls.isEmpty {
            return urls.map { TerminalSurfaceView.shellQuoted($0.path) }.joined(separator: " ")
        }
        return string(forType: .string)
    }
}
