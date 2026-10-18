import AppKit
import GhosttyKit

/// Receives what a terminal's libghostty surface reports, on the main thread.
@MainActor protocol TerminalSurfaceOwner: AnyObject {
    func terminal(_ view: TerminalSurfaceView, received event: TerminalEvent)
    /// Text dropped onto the terminal, such as a file's path.
    func terminal(_ view: TerminalSurfaceView, receivedDrop text: String)
}

/// Frees a libghostty surface once nothing refers to it, whichever thread
/// releases the last reference.
private final class SurfaceHandle: @unchecked Sendable {
    private(set) var surface: ghostty_surface_t?
    init(_ surface: ghostty_surface_t) { self.surface = surface }
    func free() {
        if let surface { ghostty_surface_free(surface) }
        surface = nil
    }
    deinit { free() }
}

/// A terminal: an AppKit view drawn by one libghostty surface, which runs
/// the user's login shell. Freeing the surface ends the shell.
///
/// Input and IME handling are adapted from Ghostty's macOS SurfaceView
/// (MIT licensed).
final class TerminalSurfaceView: NSView, @preconcurrency NSTextInputClient {
    weak var owner: TerminalSurfaceOwner?
    private var handle: SurfaceHandle?
    var surface: ghostty_surface_t? { handle?.surface }

    private var markedText = NSMutableAttributedString()
    private var keyTextAccumulator: [String]?
    private var cursor: NSCursor = .iBeam
    private var shownToGhostty: Bool?
    private var focusedInGhostty = false
    private var windowObservers: [NSObjectProtocol] = []

    /// Starts a login shell in `directory`. Fails if libghostty isn't running.
    init?(directory: String) {
        super.init(frame: NSRect(x: 0, y: 0, width: 800, height: 600))
        guard let app = TerminalRuntime.shared.app else { return nil }
        var config = ghostty_surface_config_new()
        config.userdata = Unmanaged.passUnretained(self).toOpaque()
        config.platform_tag = GHOSTTY_PLATFORM_MACOS
        config.platform = ghostty_platform_u(macos: ghostty_platform_macos_s(
            nsview: Unmanaged.passUnretained(self).toOpaque()))
        config.scale_factor = Double(NSScreen.main?.backingScaleFactor ?? 2)
        config.font_size = 0
        config.context = GHOSTTY_SURFACE_CONTEXT_TAB
        // No command: libghostty runs the user's login shell.
        let created = directory.withCString { path -> ghostty_surface_t? in
            config.working_directory = path
            return ghostty_surface_new(app, &config)
        }
        guard let created else { return nil }
        handle = SurfaceHandle(created)
        TerminalRuntime.shared.register(self)
        registerForDraggedTypes([.fileURL, .string])
    }

    required init?(coder: NSCoder) { fatalError("unsupported") }

    /// Ends the shell and frees the surface.
    func close() {
        TerminalRuntime.shared.unregister(self)
        stopObservingWindow()
        handle?.free()
        handle = nil
        owner = nil
    }

    func receive(_ event: TerminalEvent) {
        switch event {
        case .mouseShape(let shape):
            cursor = Self.cursor(for: shape)
            window?.invalidateCursorRects(for: self)
        case .mouseVisible(let visible):
            NSCursor.setHiddenUntilMouseMoves(!visible)
        default:
            owner?.terminal(self, received: event)
        }
    }

    /// Whether closing would end a running command, as shell integration
    /// reports it.
    var needsConfirmClose: Bool { surface.map(ghostty_surface_needs_confirm_quit) ?? false }

    /// Sends text as if pasted.
    func sendText(_ text: String) {
        guard let surface else { return }
        text.withCString { ghostty_surface_text(surface, $0, UInt(text.utf8.count)) }
    }

    private func performBinding(_ action: String) {
        guard let surface else { return }
        _ = ghostty_surface_binding_action(surface, action, UInt(action.utf8.count))
    }

    nonisolated static func shellQuoted(_ value: String) -> String {
        let safe = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "/._-+@%:,"))
        if !value.isEmpty, value.unicodeScalars.allSatisfy({ safe.contains($0) }) { return value }
        return "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    // MARK: Visibility and focus

    override var acceptsFirstResponder: Bool { true }

    override func becomeFirstResponder() -> Bool {
        let result = super.becomeFirstResponder()
        if result { syncFocus(responder: true) }
        return result
    }

    override func resignFirstResponder() -> Bool {
        let result = super.resignFirstResponder()
        if result { syncFocus(responder: false) }
        return result
    }

    /// libghostty draws the cursor as focused only while the window is key
    /// and this view holds the keyboard.
    private func syncFocus(responder: Bool? = nil) {
        let focused = window?.isKeyWindow == true && (responder ?? (window?.firstResponder === self))
        guard focused != focusedInGhostty, let surface else { return }
        focusedInGhostty = focused
        ghostty_surface_set_focus(surface, focused)
    }

    /// A terminal out of a window, or in a hidden one, stops drawing.
    private func syncVisibility() {
        let shown = window.map { $0.occlusionState.contains(.visible) } ?? false
        guard shown != shownToGhostty, let surface else { return }
        shownToGhostty = shown
        ghostty_surface_set_occlusion(surface, shown)
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        stopObservingWindow()
        if let window {
            let center = NotificationCenter.default
            for name in [NSWindow.didBecomeKeyNotification, NSWindow.didResignKeyNotification,
                         NSWindow.didChangeOcclusionStateNotification, NSWindow.didChangeScreenNotification] {
                windowObservers.append(center.addObserver(forName: name, object: window, queue: .main) { [weak self] _ in
                    MainActor.assumeIsolated {
                        self?.syncFocus()
                        self?.syncVisibility()
                        self?.updateDisplayID()
                    }
                })
            }
            updateDisplayID()
            viewDidChangeBackingProperties()
        }
        syncVisibility()
        syncFocus()
    }

    private func stopObservingWindow() {
        for observer in windowObservers { NotificationCenter.default.removeObserver(observer) }
        windowObservers = []
    }

    private func updateDisplayID() {
        guard let surface,
              let number = window?.screen?.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber
        else { return }
        ghostty_surface_set_display_id(surface, number.uint32Value)
    }

    // MARK: Size

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

    private var cellHeight: CGFloat {
        guard let surface else { return 0 }
        return CGFloat(ghostty_surface_size(surface).cell_height_px) / (window?.backingScaleFactor ?? 2)
    }

    // MARK: Mouse

    override func resetCursorRects() {
        addCursorRect(bounds, cursor: cursor)
    }

    override func updateTrackingAreas() {
        for area in trackingAreas where area.owner === self { removeTrackingArea(area) }
        addTrackingArea(NSTrackingArea(rect: bounds,
            options: [.mouseEnteredAndExited, .mouseMoved, .inVisibleRect, .activeAlways], owner: self))
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
    private func mouseButton(_ state: ghostty_input_mouse_state_e, _ button: ghostty_input_mouse_button_e,
                             _ event: NSEvent) -> Bool {
        guard let surface else { return false }
        // Report the position first: a click isn't always preceded by a move.
        let point = convert(event.locationInWindow, from: nil)
        ghostty_surface_mouse_pos(surface, point.x, frame.height - point.y, Self.mods(event.modifierFlags))
        return ghostty_surface_mouse_button(surface, state, button, Self.mods(event.modifierFlags))
    }

    override func mouseMoved(with event: NSEvent) {
        guard let surface else { return }
        let point = convert(event.locationInWindow, from: nil)
        ghostty_surface_mouse_pos(surface, point.x, frame.height - point.y, Self.mods(event.modifierFlags))
    }

    override func mouseEntered(with event: NSEvent) { mouseMoved(with: event) }
    override func mouseDragged(with event: NSEvent) { mouseMoved(with: event) }
    override func rightMouseDragged(with event: NSEvent) { mouseMoved(with: event) }
    override func otherMouseDragged(with event: NSEvent) { mouseMoved(with: event) }

    override func mouseExited(with event: NSEvent) {
        guard let surface, NSEvent.pressedMouseButtons == 0 else { return }
        ghostty_surface_mouse_pos(surface, -1, -1, Self.mods(event.modifierFlags))
    }

    override func scrollWheel(with event: NSEvent) {
        guard let surface else { return }
        var x = event.scrollingDeltaX
        var y = event.scrollingDeltaY
        let precise = event.hasPreciseScrollingDeltas
        if precise { x *= 2; y *= 2 }
        var mods: Int32 = precise ? 1 : 0
        mods |= Int32(Self.momentum(event.momentumPhase)) << 1
        ghostty_surface_mouse_scroll(surface, x, y, mods)
    }

    private static func momentum(_ phase: NSEvent.Phase) -> UInt8 {
        switch phase {
        case .began: 1
        case .stationary: 2
        case .changed: 3
        case .ended: 4
        case .cancelled: 5
        case .mayBegin: 6
        default: 0
        }
    }

    override func menu(for event: NSEvent) -> NSMenu? {
        let menu = NSMenu()
        menu.addItem(withTitle: "Copy", action: #selector(copy(_:)), keyEquivalent: "").isEnabled = hasSelection
        menu.addItem(withTitle: "Paste", action: #selector(paste(_:)), keyEquivalent: "")
        menu.autoenablesItems = false
        return menu
    }

    private static func cursor(for shape: ghostty_action_mouse_shape_e) -> NSCursor {
        switch shape {
        case GHOSTTY_MOUSE_SHAPE_DEFAULT: .arrow
        case GHOSTTY_MOUSE_SHAPE_POINTER: .pointingHand
        case GHOSTTY_MOUSE_SHAPE_CROSSHAIR, GHOSTTY_MOUSE_SHAPE_CELL: .crosshair
        case GHOSTTY_MOUSE_SHAPE_VERTICAL_TEXT: .iBeamCursorForVerticalLayout
        case GHOSTTY_MOUSE_SHAPE_CONTEXT_MENU: .contextualMenu
        case GHOSTTY_MOUSE_SHAPE_NOT_ALLOWED, GHOSTTY_MOUSE_SHAPE_NO_DROP: .operationNotAllowed
        case GHOSTTY_MOUSE_SHAPE_GRAB: .openHand
        case GHOSTTY_MOUSE_SHAPE_GRABBING: .closedHand
        case GHOSTTY_MOUSE_SHAPE_COL_RESIZE, GHOSTTY_MOUSE_SHAPE_EW_RESIZE: .resizeLeftRight
        case GHOSTTY_MOUSE_SHAPE_ROW_RESIZE, GHOSTTY_MOUSE_SHAPE_NS_RESIZE: .resizeUpDown
        case GHOSTTY_MOUSE_SHAPE_COPY: .dragCopy
        case GHOSTTY_MOUSE_SHAPE_ALIAS: .dragLink
        default: .iBeam
        }
    }

    // MARK: Keyboard

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        guard event.type == .keyDown, window?.firstResponder === self else { return false }
        // Command shortcuts belong to Kit's menus; ones they don't use come
        // back as key presses. Control combinations AppKit would otherwise
        // swallow go to the terminal.
        let flags = event.modifierFlags
        if flags.contains(.command) { return false }
        if flags.contains(.control), let chars = event.charactersIgnoringModifiers, chars == "\r" || chars == "/" {
            keyDown(with: event)
            return true
        }
        return false
    }

    override func keyDown(with event: NSEvent) {
        guard let surface else {
            interpretKeyEvents([event])
            return
        }
        // Option-as-alt and similar settings change which modifiers AppKit
        // should apply when translating the key to text.
        let translation = ghostty_surface_key_translation_mods(surface, Self.mods(event.modifierFlags))
        var translationMods = event.modifierFlags
        for (flag, mod) in [(NSEvent.ModifierFlags.shift, GHOSTTY_MODS_SHIFT), (.control, GHOSTTY_MODS_CTRL),
                            (.option, GHOSTTY_MODS_ALT), (.command, GHOSTTY_MODS_SUPER)] {
            if translation.rawValue & mod.rawValue != 0 { translationMods.insert(flag) } else { translationMods.remove(flag) }
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
                      text: translationEvent.terminalCharacters, composing: composing)
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
        let pressed = Self.mods(event.modifierFlags).rawValue & mod != 0
        keyAction(pressed ? GHOSTTY_ACTION_PRESS : GHOSTTY_ACTION_RELEASE, event: event)
    }

    @discardableResult
    private func keyAction(_ action: ghostty_input_action_e, event: NSEvent, translationEvent: NSEvent? = nil,
                           text: String? = nil, composing: Bool = false) -> Bool {
        guard let surface else { return false }
        var key = ghostty_input_key_s()
        key.action = action
        key.keycode = UInt32(event.keyCode)
        key.mods = Self.mods(event.modifierFlags)
        // Control and command never contribute to text translation on macOS.
        key.consumed_mods = Self.mods((translationEvent?.modifierFlags ?? event.modifierFlags)
            .subtracting([.control, .command]))
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

    static func mods(_ flags: NSEvent.ModifierFlags) -> ghostty_input_mods_e {
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
        let rect = NSRect(x: x, y: frame.height - y, width: 0, height: max(height, cellHeight))
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
        // Committed text outside a key press, such as dictation or emoji.
        guard let surface, !chars.isEmpty else { return }
        var key = ghostty_input_key_s()
        key.action = GHOSTTY_ACTION_PRESS
        chars.withCString { pointer in
            key.text = pointer
            _ = ghostty_surface_key(surface, key)
        }
    }

    override func doCommand(by selector: Selector) {
        // AppKit text commands are ignored; keyDown sends the key itself.
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

    private var hasSelection: Bool { surface.map(ghostty_surface_has_selection) ?? false }

    @objc func copy(_ sender: Any?) {
        if hasSelection { performBinding("copy_to_clipboard") }
    }

    @objc func paste(_ sender: Any?) { performBinding("paste_from_clipboard") }

    @objc override func selectAll(_ sender: Any?) { performBinding("select_all") }

    // MARK: Drag and drop

    override func draggingEntered(_ sender: any NSDraggingInfo) -> NSDragOperation { .copy }

    override func performDragOperation(_ sender: any NSDraggingInfo) -> Bool {
        guard let text = sender.draggingPasteboard.terminalText(), !text.isEmpty else { return false }
        owner?.terminal(self, receivedDrop: text)
        return true
    }
}

private extension NSEvent {
    /// Key text for libghostty. It encodes control characters and function
    /// keys itself.
    var terminalCharacters: String? {
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
