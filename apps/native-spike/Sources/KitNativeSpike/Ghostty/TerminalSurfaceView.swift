import AppKit
import GhosttyKit

/// Host-owned terminal surface boundary: an AppKit view backed by one
/// libghostty surface running one hosted command.
///
/// Input and IME handling are adapted from Ghostty's macOS SurfaceView
/// (MIT licensed), reduced to what the Kit TUI needs for this spike.
final class TerminalSurfaceView: NSView, NSTextInputClient {
    /// Receives surface lifecycle callbacks on the main thread.
    weak var hostDelegate: TerminalSurfaceHostDelegate?

    private(set) var surface: ghostty_surface_t?
    let id = UUID()

    private var markedText = NSMutableAttributedString()
    private var keyTextAccumulator: [String]?
    private var trackingArea: NSTrackingArea?
    private(set) var focused = false

    /// Creates a surface that runs `command` in `workingDirectory`.
    init?(command: String, workingDirectory: String?, environment: [String: String]) {
        super.init(frame: NSRect(x: 0, y: 0, width: 900, height: 600))
        guard let app = GhosttyRuntime.shared.app else { return nil }

        var config = ghostty_surface_config_new()
        config.userdata = Unmanaged.passUnretained(self).toOpaque()
        config.platform_tag = GHOSTTY_PLATFORM_MACOS
        config.platform = ghostty_platform_u(macos: ghostty_platform_macos_s(
            nsview: Unmanaged.passUnretained(self).toOpaque()))
        config.scale_factor = Double(NSScreen.main?.backingScaleFactor ?? 2)
        config.font_size = 0
        config.context = GHOSTTY_SURFACE_CONTEXT_WINDOW

        let keys = Array(environment.keys)
        let created: ghostty_surface_t? = command.withCString { cCommand in
            (workingDirectory ?? NSHomeDirectory()).withCString { cDir in
                config.command = cCommand
                config.working_directory = cDir
                let cKeys = keys.map { strdup($0) }
                let cValues = keys.map { strdup(environment[$0]!) }
                defer { (cKeys + cValues).forEach { free($0) } }
                var vars = zip(cKeys, cValues).map { ghostty_env_var_s(key: $0, value: $1) }
                return vars.withUnsafeMutableBufferPointer { buffer in
                    config.env_vars = buffer.baseAddress
                    config.env_var_count = buffer.count
                    return ghostty_surface_new(app, &config)
                }
            }
        }
        guard let created else { return nil }
        surface = created
        registerForDraggedTypes([.fileURL, .string])
        updateTrackingAreas()
    }

    required init?(coder: NSCoder) { fatalError("unsupported") }

    deinit {
        if let surface { ghostty_surface_free(surface) }
    }

    static func from(_ userdata: UnsafeMutableRawPointer?) -> TerminalSurfaceView? {
        guard let userdata else { return nil }
        return Unmanaged<TerminalSurfaceView>.fromOpaque(userdata).takeUnretainedValue()
    }

    // MARK: Host operations

    /// Sends text as if pasted (bracketed when the program enables it).
    func sendText(_ text: String) {
        guard let surface else { return }
        let length = text.utf8CString.count - 1
        text.withCString { ghostty_surface_text(surface, $0, UInt(length)) }
    }

    func performBinding(_ action: String) {
        guard let surface else { return }
        _ = ghostty_surface_binding_action(surface, action, UInt(action.utf8.count))
    }

    func requestClose() {
        guard let surface else { return }
        ghostty_surface_request_close(surface)
    }

    // MARK: View lifecycle

    override var acceptsFirstResponder: Bool { true }

    override func becomeFirstResponder() -> Bool {
        let result = super.becomeFirstResponder()
        if result { setFocused(true) }
        return result
    }

    override func resignFirstResponder() -> Bool {
        let result = super.resignFirstResponder()
        if result { setFocused(false) }
        return result
    }

    func setFocused(_ value: Bool) {
        focused = value
        if let surface { ghostty_surface_set_focus(surface, value) }
        onFocusChange?(value)
    }

    /// Spike: reports AppKit first-responder changes to the embedding host.
    var onFocusChange: ((Bool) -> Void)?

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        updateDisplayID()
        viewDidChangeBackingProperties()
    }

    func updateDisplayID() {
        guard let surface,
              let number = window?.screen?.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber
        else { return }
        ghostty_surface_set_display_id(surface, number.uint32Value)
    }

    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        syncSize()
    }

    override func viewDidChangeBackingProperties() {
        super.viewDidChangeBackingProperties()
        guard let window else { return }
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        layer?.contentsScale = window.backingScaleFactor
        CATransaction.commit()
        guard let surface, frame.width > 0, frame.height > 0 else { return }
        let backing = convertToBacking(frame)
        ghostty_surface_set_content_scale(surface, backing.width / frame.width, backing.height / frame.height)
        syncSize()
    }

    private func syncSize() {
        guard let surface else { return }
        let size = convertToBacking(bounds.size)
        ghostty_surface_set_size(surface, UInt32(max(size.width, 1)), UInt32(max(size.height, 1)))
    }

    var cellSize: NSSize {
        guard let surface else { return .zero }
        let size = ghostty_surface_size(surface)
        let scale = window?.backingScaleFactor ?? 2
        return NSSize(width: CGFloat(size.cell_width_px) / scale, height: CGFloat(size.cell_height_px) / scale)
    }

    // MARK: Mouse

    override func updateTrackingAreas() {
        if let trackingArea { removeTrackingArea(trackingArea) }
        let area = NSTrackingArea(
            rect: bounds,
            options: [.mouseEnteredAndExited, .mouseMoved, .inVisibleRect, .activeAlways],
            owner: self)
        addTrackingArea(area)
        trackingArea = area
        super.updateTrackingAreas()
    }

    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        mouseButton(GHOSTTY_MOUSE_PRESS, GHOSTTY_MOUSE_LEFT, event)
    }

    override func mouseUp(with event: NSEvent) {
        mouseButton(GHOSTTY_MOUSE_RELEASE, GHOSTTY_MOUSE_LEFT, event)
    }

    override func rightMouseDown(with event: NSEvent) {
        if !mouseButton(GHOSTTY_MOUSE_PRESS, GHOSTTY_MOUSE_RIGHT, event) { super.rightMouseDown(with: event) }
    }

    override func rightMouseUp(with event: NSEvent) {
        if !mouseButton(GHOSTTY_MOUSE_RELEASE, GHOSTTY_MOUSE_RIGHT, event) { super.rightMouseUp(with: event) }
    }

    override func otherMouseDown(with event: NSEvent) {
        mouseButton(GHOSTTY_MOUSE_PRESS, GHOSTTY_MOUSE_MIDDLE, event)
    }

    override func otherMouseUp(with event: NSEvent) {
        mouseButton(GHOSTTY_MOUSE_RELEASE, GHOSTTY_MOUSE_MIDDLE, event)
    }

    @discardableResult
    private func mouseButton(_ state: ghostty_input_mouse_state_e, _ button: ghostty_input_mouse_button_e, _ event: NSEvent) -> Bool {
        guard let surface else { return false }
        // Report the position first: a click is not always preceded by a move.
        let point = convert(event.locationInWindow, from: nil)
        shellDebug("mouse \(state.rawValue) button=\(button.rawValue) at=\(point) frame=\(frame.size) captured=\(ghostty_surface_mouse_captured(surface))")
        ghostty_surface_mouse_pos(surface, point.x, frame.height - point.y, ghosttyMods(event.modifierFlags))
        return ghostty_surface_mouse_button(surface, state, button, ghosttyMods(event.modifierFlags))
    }

    override func mouseMoved(with event: NSEvent) {
        guard let surface else { return }
        let point = convert(event.locationInWindow, from: nil)
        ghostty_surface_mouse_pos(surface, point.x, frame.height - point.y, ghosttyMods(event.modifierFlags))
    }

    override func mouseEntered(with event: NSEvent) { mouseMoved(with: event) }
    override func mouseDragged(with event: NSEvent) { mouseMoved(with: event) }
    override func rightMouseDragged(with event: NSEvent) { mouseMoved(with: event) }
    override func otherMouseDragged(with event: NSEvent) { mouseMoved(with: event) }

    override func mouseExited(with event: NSEvent) {
        guard let surface, NSEvent.pressedMouseButtons == 0 else { return }
        ghostty_surface_mouse_pos(surface, -1, -1, ghosttyMods(event.modifierFlags))
    }

    override func scrollWheel(with event: NSEvent) {
        guard let surface else { return }
        var x = event.scrollingDeltaX
        var y = event.scrollingDeltaY
        let precise = event.hasPreciseScrollingDeltas
        if precise { x *= 2; y *= 2 }
        var mods: Int32 = precise ? 1 : 0
        mods |= Int32(momentum(event.momentumPhase)) << 1
        ghostty_surface_mouse_scroll(surface, x, y, mods)
    }

    private func momentum(_ phase: NSEvent.Phase) -> UInt8 {
        switch phase {
        case .began: return 1
        case .stationary: return 2
        case .changed: return 3
        case .ended: return 4
        case .cancelled: return 5
        case .mayBegin: return 6
        default: return 0
        }
    }

    // MARK: Keyboard

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        guard event.type == .keyDown, focused else { return false }
        // Command shortcuts belong to the app menu. Control combinations AppKit
        // would otherwise swallow are delivered to the terminal.
        let flags = event.modifierFlags
        if flags.contains(.command) { return false }
        if flags.contains(.control), let chars = event.charactersIgnoringModifiers, chars == "\r" || chars == "/" {
            keyDown(with: event)
            return true
        }
        return false
    }

    override func keyDown(with event: NSEvent) {
        shellDebug("keyDown code=\(event.keyCode) chars=\(event.characters?.debugDescription ?? "nil") mods=\(event.modifierFlags.rawValue)")
        guard let surface else {
            interpretKeyEvents([event])
            return
        }

        // Respect option-as-alt and similar translation settings from config.
        let translationGhostty = ghostty_surface_key_translation_mods(surface, ghosttyMods(event.modifierFlags))
        var translationMods = event.modifierFlags
        for (flag, mod) in [(NSEvent.ModifierFlags.shift, GHOSTTY_MODS_SHIFT), (.control, GHOSTTY_MODS_CTRL),
                            (.option, GHOSTTY_MODS_ALT), (.command, GHOSTTY_MODS_SUPER)] {
            if translationGhostty.rawValue & mod.rawValue != 0 { translationMods.insert(flag) } else { translationMods.remove(flag) }
        }
        let translationEvent: NSEvent
        if translationMods == event.modifierFlags {
            translationEvent = event
        } else {
            translationEvent = NSEvent.keyEvent(
                with: event.type, location: event.locationInWindow, modifierFlags: translationMods,
                timestamp: event.timestamp, windowNumber: event.windowNumber, context: nil,
                characters: event.characters(byApplyingModifiers: translationMods) ?? "",
                charactersIgnoringModifiers: event.charactersIgnoringModifiers ?? "",
                isARepeat: event.isARepeat, keyCode: event.keyCode) ?? event
        }

        let action = event.isARepeat ? GHOSTTY_ACTION_REPEAT : GHOSTTY_ACTION_PRESS
        keyTextAccumulator = []
        defer { keyTextAccumulator = nil }
        let markedBefore = markedText.length > 0

        interpretKeyEvents([translationEvent])
        syncPreedit(clearIfNeeded: markedBefore)
        let composing = markedText.length > 0 || markedBefore

        if let list = keyTextAccumulator, !list.isEmpty {
            for text in list where !(composing && text.isSingleControlCharacter) {
                keyAction(action, event: event, translationEvent: translationEvent, text: text)
            }
        } else {
            if composing && (event.characters?.isSingleControlCharacter ?? false) { return }
            keyAction(action, event: event, translationEvent: translationEvent,
                      text: translationEvent.ghosttyCharacters, composing: composing)
        }
    }

    override func keyUp(with event: NSEvent) {
        keyAction(GHOSTTY_ACTION_RELEASE, event: event)
    }

    override func flagsChanged(with event: NSEvent) {
        let mod: UInt32
        switch event.keyCode {
        case 0x39: mod = GHOSTTY_MODS_CAPS.rawValue
        case 0x38, 0x3C: mod = GHOSTTY_MODS_SHIFT.rawValue
        case 0x3B, 0x3E: mod = GHOSTTY_MODS_CTRL.rawValue
        case 0x3A, 0x3D: mod = GHOSTTY_MODS_ALT.rawValue
        case 0x37, 0x36: mod = GHOSTTY_MODS_SUPER.rawValue
        default: return
        }
        if hasMarkedText() { return }
        let pressed = ghosttyMods(event.modifierFlags).rawValue & mod != 0
        keyAction(pressed ? GHOSTTY_ACTION_PRESS : GHOSTTY_ACTION_RELEASE, event: event)
    }

    @discardableResult
    private func keyAction(
        _ action: ghostty_input_action_e,
        event: NSEvent,
        translationEvent: NSEvent? = nil,
        text: String? = nil,
        composing: Bool = false
    ) -> Bool {
        guard let surface else { return false }
        var key = ghostty_input_key_s()
        key.action = action
        key.keycode = UInt32(event.keyCode)
        key.mods = ghosttyMods(event.modifierFlags)
        // Control and command never contribute to text translation on macOS.
        key.consumed_mods = ghosttyMods((translationEvent?.modifierFlags ?? event.modifierFlags).subtracting([.control, .command]))
        key.composing = composing
        if event.type == .keyDown || event.type == .keyUp,
           let scalar = event.characters(byApplyingModifiers: [])?.unicodeScalars.first {
            key.unshifted_codepoint = scalar.value
        }
        if let text, !text.isEmpty, !text.isSingleControlCharacter {
            return text.withCString { pointer in
                key.text = pointer
                return ghostty_surface_key(surface, key)
            }
        }
        return ghostty_surface_key(surface, key)
    }

    // MARK: NSTextInputClient

    func hasMarkedText() -> Bool { markedText.length > 0 }

    func markedRange() -> NSRange {
        markedText.length > 0 ? NSRange(location: 0, length: markedText.length) : NSRange(location: NSNotFound, length: 0)
    }

    func selectedRange() -> NSRange { NSRange(location: NSNotFound, length: 0) }

    func setMarkedText(_ string: Any, selectedRange: NSRange, replacementRange: NSRange) {
        switch string {
        case let value as NSAttributedString: markedText = NSMutableAttributedString(attributedString: value)
        case let value as String: markedText = NSMutableAttributedString(string: value)
        default: return
        }
        if keyTextAccumulator == nil { syncPreedit() }
    }

    func unmarkText() {
        guard markedText.length > 0 else { return }
        markedText.mutableString.setString("")
        syncPreedit()
    }

    func validAttributesForMarkedText() -> [NSAttributedString.Key] { [] }

    func attributedSubstring(forProposedRange range: NSRange, actualRange: NSRangePointer?) -> NSAttributedString? { nil }

    func characterIndex(for point: NSPoint) -> Int { 0 }

    func firstRect(forCharacterRange range: NSRange, actualRange: NSRangePointer?) -> NSRect {
        guard let surface, let window else { return .zero }
        var x = 0.0, y = 0.0, width = 0.0, height = 0.0
        ghostty_surface_ime_point(surface, &x, &y, &width, &height)
        let rect = NSRect(x: x, y: frame.height - y, width: 0, height: max(height, cellSize.height))
        return window.convertToScreen(convert(rect, to: nil))
    }

    func insertText(_ string: Any, replacementRange: NSRange) {
        guard NSApp.currentEvent != nil else { return }
        let chars: String
        switch string {
        case let value as NSAttributedString: chars = value.string
        case let value as String: chars = value
        default: return
        }
        unmarkText()
        if keyTextAccumulator != nil {
            keyTextAccumulator?.append(chars)
            return
        }
        // Committed text outside a key event (dictation, emoji picker).
        guard let surface, !chars.isEmpty else { return }
        var key = ghostty_input_key_s()
        key.action = GHOSTTY_ACTION_PRESS
        chars.withCString { pointer in
            key.text = pointer
            _ = ghostty_surface_key(surface, key)
        }
    }

    override func doCommand(by selector: Selector) {
        // Swallow AppKit text commands; keyDown encodes the raw key instead.
    }

    private func syncPreedit(clearIfNeeded: Bool = true) {
        guard let surface else { return }
        if markedText.length > 0 {
            let string = markedText.string
            string.withCString { ghostty_surface_preedit(surface, $0, UInt(string.utf8.count)) }
        } else if clearIfNeeded {
            ghostty_surface_preedit(surface, nil, 0)
        }
    }

    // MARK: Edit menu

    /// Copies the terminal selection when Ghostty has one. Otherwise the TUI
    /// owns the selection it draws, so Command-C is delivered as `super+c`;
    /// the TUI copies through OSC 52, which reaches the host clipboard
    /// callback.
    @objc func copy(_ sender: Any?) {
        guard let surface else { return }
        if ghostty_surface_has_selection(surface) {
            performBinding("copy_to_clipboard")
            return
        }
        sendCommandKey("c", fallbackKeyCode: 8 /* kVK_ANSI_C */)
    }

    @objc func paste(_ sender: Any?) { performBinding("paste_from_clipboard") }
    @objc override func selectAll(_ sender: Any?) { performBinding("select_all") }

    /// Delivers a Command-modified key to the hosted program as a press and
    /// release, using the triggering key event's physical key when available.
    private func sendCommandKey(_ character: String, fallbackKeyCode: UInt16) {
        guard let surface, let codepoint = character.unicodeScalars.first?.value else { return }
        var keyCode = fallbackKeyCode
        if let event = NSApp.currentEvent, event.type == .keyDown,
           event.modifierFlags.contains(.command),
           event.charactersIgnoringModifiers?.lowercased() == character {
            keyCode = event.keyCode
        }
        var key = ghostty_input_key_s()
        key.keycode = UInt32(keyCode)
        key.mods = GHOSTTY_MODS_SUPER
        key.consumed_mods = GHOSTTY_MODS_NONE
        key.unshifted_codepoint = codepoint
        key.action = GHOSTTY_ACTION_PRESS
        character.withCString { pointer in
            key.text = pointer
            _ = ghostty_surface_key(surface, key)
        }
        key.text = nil
        key.action = GHOSTTY_ACTION_RELEASE
        _ = ghostty_surface_key(surface, key)
        shellDebug("forwarded super+\(character) keycode=\(keyCode)")
    }

    // MARK: Drag and drop

    override func draggingEntered(_ sender: any NSDraggingInfo) -> NSDragOperation { .copy }

    override func performDragOperation(_ sender: any NSDraggingInfo) -> Bool {
        guard let text = sender.draggingPasteboard.kitPlainText(), !text.isEmpty else { return false }
        hostDelegate?.surface(self, didReceiveDrop: text)
        return true
    }
}

/// Host-level callbacks for a terminal surface.
protocol TerminalSurfaceHostDelegate: AnyObject {
    func surfaceDidClose(_ view: TerminalSurfaceView)
    func surface(_ view: TerminalSurfaceView, didReceiveDrop text: String)
}

func ghosttyMods(_ flags: NSEvent.ModifierFlags) -> ghostty_input_mods_e {
    var mods = GHOSTTY_MODS_NONE.rawValue
    if flags.contains(.shift) { mods |= GHOSTTY_MODS_SHIFT.rawValue }
    if flags.contains(.control) { mods |= GHOSTTY_MODS_CTRL.rawValue }
    if flags.contains(.option) { mods |= GHOSTTY_MODS_ALT.rawValue }
    if flags.contains(.command) { mods |= GHOSTTY_MODS_SUPER.rawValue }
    if flags.contains(.capsLock) { mods |= GHOSTTY_MODS_CAPS.rawValue }
    let raw = flags.rawValue
    if raw & UInt(NX_DEVICERSHIFTKEYMASK) != 0 { mods |= GHOSTTY_MODS_SHIFT_RIGHT.rawValue }
    if raw & UInt(NX_DEVICERCTLKEYMASK) != 0 { mods |= GHOSTTY_MODS_CTRL_RIGHT.rawValue }
    if raw & UInt(NX_DEVICERALTKEYMASK) != 0 { mods |= GHOSTTY_MODS_ALT_RIGHT.rawValue }
    if raw & UInt(NX_DEVICERCMDKEYMASK) != 0 { mods |= GHOSTTY_MODS_SUPER_RIGHT.rawValue }
    return ghostty_input_mods_e(mods)
}

private extension NSEvent {
    /// Key text for libghostty: control characters and function-key PUA
    /// values are encoded by Ghostty itself.
    var ghosttyCharacters: String? {
        guard let characters else { return nil }
        if characters.count == 1, let scalar = characters.unicodeScalars.first {
            if scalar.value < 0x20 { return self.characters(byApplyingModifiers: modifierFlags.subtracting(.control)) }
            if scalar.value >= 0xF700 && scalar.value <= 0xF8FF { return nil }
        }
        return characters
    }
}

private extension String {
    var isSingleControlCharacter: Bool {
        let scalars = unicodeScalars
        guard let first = scalars.first, scalars.count == 1 else { return false }
        return first.value < 0x20 || first.value == 0x7F
    }
}

/// Writes to stderr when KIT_SHELL_DEBUG is set (spike diagnostics).
func shellDebug(_ message: @autoclosure () -> String) {
    guard ProcessInfo.processInfo.environment["KIT_SHELL_DEBUG"] != nil else { return }
    FileHandle.standardError.write(Data(("[kitshell] " + message() + "\n").utf8))
}
