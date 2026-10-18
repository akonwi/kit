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
/// thread, and applies the terminal appearance to every terminal.
///
/// Callback handling is adapted from Ghostty's macOS app (MIT licensed).
@MainActor
final class TerminalRuntime {
    static let shared = TerminalRuntime()

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

    private init() {}

    /// Starts libghostty with `appearance`, or applies it if running.
    /// Returns false if libghostty can't start.
    @discardableResult
    func start(_ appearance: TerminalAppearance) -> Bool {
        if app != nil { apply(appearance); return true }
        guard failure == nil else { return false }
        Self.initializeLibrary()
        guard let config = Self.makeConfig(appearance) else { failure = "The terminal’s settings couldn’t be loaded."; return false }
        var runtime = Self.runtimeConfig()
        guard let app = ghostty_app_new(&runtime, config) else {
            ghostty_config_free(config)
            failure = "The terminal couldn’t start."
            return false
        }
        self.app = app
        self.config = config
        self.appearance = appearance
        ghostty_app_set_focus(app, NSApp.isActive)
        let center = NotificationCenter.default
        for (name, focused) in [(NSApplication.didBecomeActiveNotification, true),
                                (NSApplication.didResignActiveNotification, false)] {
            observers.append(center.addObserver(forName: name, object: nil, queue: .main) { _ in
                MainActor.assumeIsolated {
                    if let app = TerminalRuntime.shared.app { ghostty_app_set_focus(app, focused) }
                }
            })
        }
        return true
    }

    /// Applies a new appearance to every terminal.
    func apply(_ appearance: TerminalAppearance) {
        guard let app, appearance != self.appearance, let next = Self.makeConfig(appearance) else { return }
        ghostty_app_update_config(app, next)
        // libghostty copies what it needs, but Ghostty's app keeps the
        // current configuration alive; do the same.
        if let config { ghostty_config_free(config) }
        config = next
        self.appearance = appearance
    }

    func tick() {
        if let app { ghostty_app_tick(app) }
    }

    func register(_ view: TerminalSurfaceView) {
        views[UInt(bitPattern: Unmanaged.passUnretained(view).toOpaque())] = WeakView(view)
    }

    func unregister(_ view: TerminalSurfaceView) {
        views[UInt(bitPattern: Unmanaged.passUnretained(view).toOpaque())] = nil
    }

    private func view(_ key: UInt) -> TerminalSurfaceView? { views[key]?.value }

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

    /// A finalized configuration from `appearance` alone.
    private static func makeConfig(_ appearance: TerminalAppearance) -> ghostty_config_t? {
        guard let config = ghostty_config_new() else { return nil }
        let file = FileManager.default.temporaryDirectory
            .appendingPathComponent("kit-terminal-\(UUID().uuidString).conf")
        do {
            try appearance.configuration.write(to: file, atomically: true, encoding: .utf8)
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

    // MARK: Callbacks

    /// libghostty's callbacks. They are built outside the main actor:
    /// libghostty calls them from its own threads.
    nonisolated private static func runtimeConfig() -> ghostty_runtime_config_s {
        ghostty_runtime_config_s(
            userdata: nil,
            supports_selection_clipboard: false,
            wakeup_cb: { _ in
                DispatchQueue.main.async { MainActor.assumeIsolated { TerminalRuntime.shared.tick() } }
            },
            action_cb: { _, target, action in TerminalRuntime.action(target, action) },
            read_clipboard_cb: { userdata, location, state, _, _, _ in
                TerminalRuntime.readClipboard(userdata, location, state)
            },
            confirm_read_clipboard_cb: { userdata, _, state, _ in
                // Reads are denied in the configuration; refuse any that remain.
                let key = UInt(bitPattern: userdata), state = UInt(bitPattern: state)
                TerminalRuntime.onMain {
                    guard let surface = TerminalRuntime.shared.view(key)?.surface,
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
        onMain { TerminalRuntime.shared.view(key)?.receive(event) }
    }

    /// Copies an action's payload and delivers it to its terminal. Payload
    /// pointers are only valid during the callback.
    nonisolated private static func action(_ target: ghostty_target_s, _ action: ghostty_action_s) -> Bool {
        guard target.tag == GHOSTTY_TARGET_SURFACE, let surface = target.target.surface else { return false }
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
            guard let surface = TerminalRuntime.shared.view(key)?.surface,
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
