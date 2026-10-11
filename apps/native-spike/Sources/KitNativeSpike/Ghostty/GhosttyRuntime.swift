import AppKit
import GhosttyKit

/// Owns the single libghostty app instance and routes runtime callbacks to the
/// terminal surfaces that requested them.
///
/// Callback handling is adapted from Ghostty's macOS app (MIT licensed).
final class GhosttyRuntime {
    static let shared = GhosttyRuntime()

    private(set) var app: ghostty_app_t?
    private var config: ghostty_config_t?

    /// Receives surface-targeted actions on the main thread.
    weak var delegate: GhosttyRuntimeDelegate?

    private init() {}

    /// Creates the libghostty app from a host-generated configuration file.
    /// The user's own Ghostty configuration is intentionally not loaded.
    func start(configPath: String) throws {
        guard let cfg = ghostty_config_new() else { throw ShellError("ghostty_config_new failed") }
        ghostty_config_load_file(cfg, configPath)
        ghostty_config_finalize(cfg)
        let diagnostics = ghostty_config_diagnostics_count(cfg)
        for index in 0..<diagnostics {
            let diag = ghostty_config_get_diagnostic(cfg, index)
            NSLog("spike ghostty config: %@", String(cString: diag.message))
        }

        var runtime = ghostty_runtime_config_s(
            userdata: nil,
            supports_selection_clipboard: false,
            wakeup_cb: { _ in DispatchQueue.main.async { GhosttyRuntime.shared.tick() } },
            action_cb: { _, target, action in GhosttyRuntime.shared.handle(target: target, action: action) },
            read_clipboard_cb: { userdata, location, state, mimes, mimesLen, list in
                GhosttyRuntime.readClipboard(userdata, location, state, mimes, mimesLen, list)
            },
            confirm_read_clipboard_cb: { userdata, _, state, _ in
                // Paste protection is disabled in the generated config and OSC 52
                // reads are never granted to the hosted program.
                guard let view = TerminalSurfaceView.from(userdata), let surface = view.surface else { return }
                ghostty_surface_deny_clipboard_request(surface, state)
            },
            write_clipboard_cb: { _, location, content, len, _ in
                GhosttyRuntime.writeClipboard(location, content, len)
            },
            close_surface_cb: { userdata, _ in
                guard let view = TerminalSurfaceView.from(userdata) else { return }
                DispatchQueue.main.async { view.hostDelegate?.surfaceDidClose(view) }
            }
        )
        guard let app = ghostty_app_new(&runtime, cfg) else { throw ShellError("ghostty_app_new failed") }
        self.app = app
        self.config = cfg
        ghostty_app_set_focus(app, NSApp.isActive)
    }

    func tick() {
        guard let app else { return }
        ghostty_app_tick(app)
    }

    func setFocus(_ focused: Bool) {
        guard let app else { return }
        ghostty_app_set_focus(app, focused)
    }

    // MARK: Actions

    private func handle(target: ghostty_target_s, action: ghostty_action_s) -> Bool {
        guard target.tag == GHOSTTY_TARGET_SURFACE,
              let surface = target.target.surface,
              let view = TerminalSurfaceView.from(ghostty_surface_userdata(surface)) else {
            return false
        }

        // Payload pointers are only valid for the duration of this callback,
        // so copy them into Swift values before hopping to the main thread.
        let event: SurfaceEvent
        switch action.tag {
        case GHOSTTY_ACTION_SET_TITLE, GHOSTTY_ACTION_SET_TAB_TITLE:
            guard let title = action.action.set_title.title else { return false }
            event = .title(String(cString: title))
        case GHOSTTY_ACTION_DESKTOP_NOTIFICATION:
            let note = action.action.desktop_notification
            event = .notification(
                title: note.title.map { String(cString: $0) } ?? "",
                body: note.body.map { String(cString: $0) } ?? "")
        case GHOSTTY_ACTION_PROGRESS_REPORT:
            event = .progress(SurfaceProgress(action.action.progress_report))
        case GHOSTTY_ACTION_RING_BELL:
            event = .bell
        case GHOSTTY_ACTION_PWD:
            guard let pwd = action.action.pwd.pwd else { return false }
            event = .workingDirectory(String(cString: pwd))
        case GHOSTTY_ACTION_OPEN_URL:
            let payload = action.action.open_url
            guard let ptr = payload.url else { return false }
            let data = Data(bytes: ptr, count: Int(payload.len))
            guard let string = String(data: data, encoding: .utf8) else { return false }
            event = .openURL(string)
        case GHOSTTY_ACTION_MOUSE_SHAPE:
            event = .mouseShape(action.action.mouse_shape)
        case GHOSTTY_ACTION_SHOW_CHILD_EXITED:
            event = .childExited(action.action.child_exited.exit_code)
        default:
            return false
        }

        if Thread.isMainThread {
            delegate?.surface(view, didReceive: event)
        } else {
            DispatchQueue.main.async { self.delegate?.surface(view, didReceive: event) }
        }
        return true
    }

    // MARK: Clipboard

    private static func readClipboard(
        _ userdata: UnsafeMutableRawPointer?,
        _ location: ghostty_clipboard_e,
        _ state: UnsafeMutableRawPointer?,
        _ mimes: UnsafePointer<UnsafePointer<CChar>?>?,
        _ mimesLen: Int,
        _ list: Bool
    ) -> ghostty_clipboard_read_result_e {
        guard location == GHOSTTY_CLIPBOARD_STANDARD,
              let view = TerminalSurfaceView.from(userdata),
              let surface = view.surface else {
            return GHOSTTY_CLIPBOARD_READ_UNSUPPORTED
        }
        let pasteboard = NSPasteboard.general
        var requested: [String] = []
        if let mimes {
            for index in 0..<mimesLen {
                if let mime = mimes[index] { requested.append(String(cString: mime)) }
            }
        }

        var contents: [(String, Data)] = []
        for mime in requested where mime == "text/plain" {
            if let text = pasteboard.kitPlainText() { contents.append((mime, Data(text.utf8))) }
        }
        if contents.isEmpty && !list { return GHOSTTY_CLIPBOARD_READ_UNAVAILABLE }
        let available = list && pasteboard.kitPlainText() != nil ? ["text/plain"] : []
        complete(surface, contents: contents, available: available, state: state)
        return GHOSTTY_CLIPBOARD_READ_STARTED
    }

    private static func complete(
        _ surface: ghostty_surface_t,
        contents: [(String, Data)],
        available: [String],
        state: UnsafeMutableRawPointer?
    ) {
        var owned: [UnsafeMutableRawPointer] = []
        defer { owned.forEach { free($0) } }

        var cContents: [ghostty_clipboard_content_s] = []
        for (mime, data) in contents {
            guard let cMime = strdup(mime), let buffer = malloc(max(data.count, 1)) else { continue }
            owned.append(UnsafeMutableRawPointer(cMime))
            owned.append(buffer)
            data.withUnsafeBytes { bytes in
                if let base = bytes.baseAddress { buffer.copyMemory(from: base, byteCount: bytes.count) }
            }
            cContents.append(ghostty_clipboard_content_s(
                mime: cMime, data: buffer.assumingMemoryBound(to: CChar.self), len: data.count))
        }
        var cAvailable: [UnsafePointer<CChar>?] = []
        for mime in available {
            guard let cMime = strdup(mime) else { continue }
            owned.append(UnsafeMutableRawPointer(cMime))
            cAvailable.append(UnsafePointer(cMime))
        }

        cContents.withUnsafeBufferPointer { contentsBuffer in
            cAvailable.withUnsafeBufferPointer { availableBuffer in
                var request = ghostty_clipboard_complete_s(
                    contents: contentsBuffer.baseAddress,
                    contents_len: contentsBuffer.count,
                    available: availableBuffer.baseAddress,
                    available_len: availableBuffer.count,
                    confirmed: false,
                    remember: false)
                ghostty_surface_complete_clipboard_request(surface, &request, state)
            }
        }
    }

    private static func writeClipboard(
        _ location: ghostty_clipboard_e,
        _ content: UnsafePointer<ghostty_clipboard_content_s>?,
        _ len: Int
    ) {
        guard location == GHOSTTY_CLIPBOARD_STANDARD, let content, len > 0 else { return }
        var text: String?
        for index in 0..<len {
            let item = content[index]
            guard let mime = item.mime, String(cString: mime) == "text/plain", let data = item.data else { continue }
            text = String(data: Data(bytes: data, count: item.len), encoding: .utf8)
        }
        guard let text else { return }
        DispatchQueue.main.async {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(text, forType: .string)
        }
    }
}

/// Main-thread receiver for actions libghostty targets at a surface.
protocol GhosttyRuntimeDelegate: AnyObject {
    func surface(_ view: TerminalSurfaceView, didReceive event: SurfaceEvent)
}

/// Host-relevant surface signals projected from libghostty actions.
enum SurfaceEvent {
    case title(String)
    case notification(title: String, body: String)
    case progress(SurfaceProgress)
    case bell
    case workingDirectory(String)
    case openURL(String)
    case mouseShape(ghostty_action_mouse_shape_e)
    case childExited(UInt32)
}

/// OSC 9;4 progress state reported by the hosted program.
enum SurfaceProgress: Equatable {
    case none
    case indeterminate
    case paused
    case error
    case percent(Int)

    init(_ report: ghostty_action_progress_report_s) {
        switch report.state {
        case GHOSTTY_PROGRESS_STATE_REMOVE: self = .none
        case GHOSTTY_PROGRESS_STATE_INDETERMINATE: self = .indeterminate
        case GHOSTTY_PROGRESS_STATE_PAUSE: self = .paused
        case GHOSTTY_PROGRESS_STATE_ERROR: self = .error
        default: self = report.progress >= 0 ? .percent(Int(report.progress)) : .indeterminate
        }
    }
}

struct ShellError: Error, CustomStringConvertible {
    let description: String
    init(_ description: String) { self.description = description }
}

extension NSPasteboard {
    /// Plain text for a terminal paste: file URLs become shell-escaped paths.
    func kitPlainText() -> String? {
        if let urls = readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL],
           !urls.isEmpty {
            return urls.map { $0.path.shellEscaped }.joined(separator: " ")
        }
        return string(forType: .string)
    }
}

extension String {
    var shellEscaped: String {
        let safe = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "/._-+@%:,"))
        if !isEmpty, unicodeScalars.allSatisfy({ safe.contains($0) }) { return self }
        return "'" + replacingOccurrences(of: "'", with: "'\\''") + "'"
    }
}
