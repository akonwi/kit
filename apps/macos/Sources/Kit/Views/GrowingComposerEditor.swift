import AppKit
import SwiftUI
import UniformTypeIdentifiers

/// A wrapping text view whose content determines its height; no scroll container.
struct GrowingComposerEditor: NSViewRepresentable {
    @Binding var text: String
    @Binding var focused: Bool
    var focusRequest: Int
    var foreground: Color
    var placeholderColor: Color
    var attachmentDrop: ([NSItemProvider]) -> Bool
    var attachmentDropTargeted: (Bool) -> Void
    var submit: () -> Void
    var shellEnabled = true
    var placeholder = "Ask Kit..."
    var focusEnabled = true
    var accessibilityLabel = "Message composer"
    var exitShell: (() -> Void)? = nil
    var cancel: (() -> Void)? = nil
    var isolatedUndo = false
    var commands: ComposerCommandState? = nil
    var promptCommands: [PromptCommand] = []
    var mentions: ComposerMentionState? = nil
    var mentionRevision = 0
    var mentionIdentity = ""
    var mentionLoader: (@Sendable (Bool) async throws -> FileIndex)? = nil
    var pickerAnchor: ComposerPickerAnchor? = nil
    var theme = MicaTheme(dark: false)

    func makeNSView(context: Context) -> ComposerTextView {
        let view = ComposerTextView(frame: .zero)
        view.delegate = context.coordinator
        view.registerForDraggedTypes([.fileURL, .png, .tiff])
        view.isRichText = false
        view.allowsUndo = true
        if isolatedUndo { view.localUndoManager = UndoManager() }
        view.isAutomaticQuoteSubstitutionEnabled = false
        view.isAutomaticDashSubstitutionEnabled = false
        view.drawsBackground = false
        view.font = Typography.shared.font(size: 14)
        view.textContainerInset = .zero
        view.textContainer?.lineFragmentPadding = 0
        view.textContainer?.widthTracksTextView = false
        view.isHorizontallyResizable = false
        view.isVerticallyResizable = false
        view.setAccessibilityLabel(accessibilityLabel)
        view.setContentCompressionResistancePriority(.required, for: .vertical)
        return view
    }

    func updateNSView(_ view: ComposerTextView, context: Context) {
        context.coordinator.parent = self
        view.pickerAnchor = pickerAnchor
        pickerAnchor?.changed = { [weak coordinator = context.coordinator, weak view] in
            guard let view else { return }; coordinator?.renderMentions(view)
        }
        view.submit = submit
        view.exitShell = exitShell
        view.cancel = cancel
        view.shellEnabled = shellEnabled
        view.commands = commands
        if commands?.catalog != promptCommands {
            commands?.catalog = promptCommands
            if commands?.isOpen == true { commands?.observe(text: view.string, caret: view.selectedRange(), pasted: false) }
        }
        view.mentions = mentions
        view.mentionOpened = { [weak coordinator = context.coordinator] in coordinator?.loadMentions() }
        view.mentionChanged = { [weak coordinator = context.coordinator, weak view] in
            guard let view else { return }; coordinator?.renderMentions(view)
        }
        if context.coordinator.identity != mentionIdentity {
            context.coordinator.identity = mentionIdentity
            mentions?.reset(); commands?.close()
        }
        view.attachmentDrop = attachmentDrop
        view.attachmentDropTargeted = attachmentDropTargeted
        view.isEditable = context.environment.isEnabled
        view.isSelectable = context.environment.isEnabled
        if view.draftText != text {
            view.setDraftText(text)
            mentions?.close(); commands?.close()
        }
        context.coordinator.previousText = view.string
        view.font = Typography.shared.font(size: 14, mono: !view.shellPrefix.isEmpty)
        view.textColor = NSColor(foreground)
        view.typingAttributes = [.font: view.font!, .foregroundColor: NSColor(foreground)]
        view.placeholder = view.shellPrefix.isEmpty ? placeholder : "Shell command…"
        context.coordinator.highlight(view)
        view.insertionPointColor = NSColor(foreground)
        view.placeholderColor = NSColor(placeholderColor)
        view.needsDisplay = true
        view.invalidateIntrinsicContentSize()
        context.coordinator.renderMentions(view)
        if focusEnabled && context.coordinator.lastFocusRequest != focusRequest {
            context.coordinator.lastFocusRequest = focusRequest
            view.requestFocusWhenAttached()
        }
    }

    func sizeThatFits(_ proposal: ProposedViewSize, nsView: ComposerTextView, context: Context) -> CGSize? {
        guard let width = proposal.width, width > 0,
              let container = nsView.textContainer, let layout = nsView.layoutManager,
              let font = nsView.font else { return nil }
        container.containerSize = NSSize(width: width, height: .greatestFiniteMagnitude)
        layout.ensureLayout(for: container)
        let used = layout.usedRect(for: container).maxY
        let trailingLine = layout.extraLineFragmentRect.maxY
        return CGSize(width: width, height: ceil(max(layout.defaultLineHeight(for: font), used, trailingLine)))
    }

    func makeCoordinator() -> Coordinator { Coordinator(self) }
    static func dismantleNSView(_ view: ComposerTextView, coordinator: Coordinator) {
        coordinator.highlightTask?.cancel()
        coordinator.commandPanel.dismiss(); coordinator.parent.commands?.close()
        coordinator.panel.dismiss(); coordinator.parent.mentions?.close()
        view.mentionChanged = nil; view.mentionOpened = nil
    }

    @MainActor final class Coordinator: NSObject, NSTextViewDelegate {
        var parent: GrowingComposerEditor
        let commandPanel = ComposerMentionPanel()
        let panel = ComposerMentionPanel()
        var highlightTask: Task<Void, Never>?
        var highlightedSource: String?
        var highlightedTheme: MicaTheme?
        var previousText: String
        var identity: String
        var lastFocusRequest: Int
        init(_ parent: GrowingComposerEditor) {
            self.parent = parent
            lastFocusRequest = parent.focusRequest
            previousText = parent.text; identity = parent.mentionIdentity
        }
        deinit { highlightTask?.cancel() }
        func highlight(_ view: ComposerTextView) {
            guard !view.hasMarkedText() else { return }
            let source = view.string, theme = parent.theme
            guard !view.shellPrefix.isEmpty else {
                highlightTask?.cancel(); highlightedSource = nil; highlightedTheme = nil
                view.layoutManager?.removeTemporaryAttribute(.foregroundColor, forCharacterRange: NSRange(location: 0, length: view.string.utf16.count))
                return
            }
            guard highlightedSource != source || highlightedTheme != theme else { return }
            highlightedSource = source; highlightedTheme = theme
            highlightTask?.cancel()
            view.layoutManager?.removeTemporaryAttribute(.foregroundColor, forCharacterRange: NSRange(location: 0, length: source.utf16.count))
            highlightTask = Task { [weak view] in
                do {
                    try await Task.sleep(for: .milliseconds(40))
                    let spans = try await CodeSyntaxHighlighter.shared.spans(for: .init(source: source, language: "bash"))
                    guard !Task.isCancelled, let view, view.string == source, !view.shellPrefix.isEmpty, !view.hasMarkedText() else { return }
                    view.applyShellHighlighting(spans, theme: theme)
                } catch { /* Plain text stays editable if parsing is unavailable. */ }
            }
        }
        func loadMentions(force: Bool = false) {
            guard let loader = parent.mentionLoader else { return }
            parent.mentions?.load(loader, force: force)
        }
        func renderMentions(_ view: ComposerTextView) {
            guard view.shellPrefix.isEmpty else {
                if parent.commands?.isOpen == true { parent.commands?.close() }
                if parent.mentions?.isOpen == true { parent.mentions?.close() }
                commandPanel.dismiss(); panel.dismiss(); return
            }
            if let commands = parent.commands {
                commandPanel.showCommands(editor: view, state: commands, theme: parent.theme) { [weak view] in view?.insertCommand($0) }
            }

            guard let mentions = parent.mentions else { panel.dismiss(); return }
            panel.show(editor: view, state: mentions, theme: parent.theme, select: { [weak view] path in
                view?.insertMention(path)
            }, retry: { [weak self] in self?.loadMentions(force: true) })
        }
        func textViewDidChangeSelection(_ notification: Notification) {
            if let view = notification.object as? ComposerTextView, let commands = parent.commands, commands.isOpen,
               view.selectedRange().length != 0 || view.selectedRange().location != NSMaxRange(commands.range) {
                commands.close(); commandPanel.dismiss()
            }

            guard let view = notification.object as? ComposerTextView, view.string == previousText,
                  let mentions = parent.mentions, mentions.isOpen else { return }
            if view.selectedRange().length != 0 || view.selectedRange().location != NSMaxRange(mentions.range) {
                mentions.close(); panel.dismiss()
            }
        }
        func textDidChange(_ notification: Notification) {
            guard let view = notification.object as? ComposerTextView else { return }
            let draft = view.draftText
            if !view.hasMarkedText() { view.setDraftText(draft) }
            parent.commands?.observe(text: view.string, caret: view.selectedRange(), pasted: view.pasting || view.hasMarkedText())
            let opened = parent.mentions?.observe(previous: previousText, next: view.string, pasted: view.pasting || view.hasMarkedText()) == true
            previousText = view.string
            parent.text = draft
            highlight(view)
            if opened { loadMentions() }
            renderMentions(view)
            view.invalidateIntrinsicContentSize()
            view.needsDisplay = true
        }
        func textDidBeginEditing(_ notification: Notification) { parent.focused = true }
        func textDidEndEditing(_ notification: Notification) { parent.focused = false }
    }
}

final class ComposerTextView: NSTextView {
    // Embedded annotation inputs must not inherit CodeEdit's source undo manager.
    var localUndoManager: UndoManager?
    override var undoManager: UndoManager? { localUndoManager ?? super.undoManager }
    private var pendingFocus = false
    func requestFocusWhenAttached() {
        pendingFocus = true
        DispatchQueue.main.async { [weak self] in self?.applyPendingFocus() }
    }
    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window != nil, pendingFocus {
            DispatchQueue.main.async { [weak self] in self?.applyPendingFocus() }
        }
    }
    private func applyPendingFocus() {
        guard pendingFocus, let window else { return }
        if window.makeFirstResponder(self) { pendingFocus = false }
    }

    weak var pickerAnchor: ComposerPickerAnchor?
    var placeholderColor = NSColor.placeholderTextColor
    var placeholder = "Ask Kit..."
    var shellEnabled = true
    private(set) var shellPrefix = ""
    var exitShell: (() -> Void)?
    var draftText: String { shellPrefix + string }

    /// Prefixes are transport syntax; the editor contains only the command.
    func setDraftText(_ draft: String) {
        let prefix = shellEnabled ? (draft.hasPrefix("!!") ? "!!" : draft.hasPrefix("!") ? "!" : "") : ""
        let visible = String(draft.dropFirst(prefix.count))
        let oldPrefix = shellPrefix
        let selection = selectedRange()
        shellPrefix = prefix
        if string != visible {
            string = visible
            let removed = max(0, prefix.count - oldPrefix.count)
            let location = min(visible.utf16.count, max(0, selection.location - removed))
            setSelectedRange(NSRange(location: location, length: min(selection.length, visible.utf16.count - location)))
        }
        if oldPrefix != prefix {
            // Prefix removal changes native edit ranges. Begin a fresh undo group
            // for the new editing mode; syntax coloring never changes that group.
            undoManager?.removeAllActions()
        }
    }

    func applyShellHighlighting(_ spans: [SyntaxSpan], theme: MicaTheme) {
        guard let layoutManager else { return }
        let length = string.utf16.count
        layoutManager.removeTemporaryAttribute(.foregroundColor, forCharacterRange: NSRange(location: 0, length: length))
        for span in spans where span.range.location >= 0 && NSMaxRange(span.range) <= length {
            let fallback: Color = switch span.role {
            case "comment": theme.muted
            case "string", "escape": theme.success
            case "keyword", "keywordType", "function", "type", "builtin": theme.accent
            default: theme.text
            }
            layoutManager.addTemporaryAttribute(.foregroundColor, value: NSColor(theme.syntax(span.role, fallback: fallback)), forCharacterRange: span.range)
        }
    }
    var attachmentDrop: (([NSItemProvider]) -> Bool)?
    var attachmentDropTargeted: ((Bool) -> Void)?

    var submit: (() -> Void)?
    var commands: ComposerCommandState?
    var mentions: ComposerMentionState?
    var mentionOpened: (() -> Void)?
    var mentionChanged: (() -> Void)?
    var pasting = false

    override func paste(_ sender: Any?) {
        pasting = true; defer { pasting = false }
        super.paste(sender)
    }
    override func resignFirstResponder() -> Bool {
        let result = super.resignFirstResponder()
        if result { commands?.close(); mentions?.close(); mentionChanged?() }
        return result
    }
    override func layout() { super.layout(); mentionChanged?() }

    override func deleteBackward(_ sender: Any?) {
        if isEditable, !hasMarkedText(), shellPrefix == "!!", selectedRange() == NSRange(location: 0, length: 0) {
            shellPrefix = "!"
            didChangeText()
            return
        }
        super.deleteBackward(sender)
    }

    func insertCommand(_ name: String? = nil) {
        guard let state = commands, let name = name ?? state.selected?.name else { return }
        let range = state.range
        state.close()
        insertText("/" + name + " ", replacementRange: range)
        window?.makeFirstResponder(self)
        mentionChanged?()
    }

    func insertMention(_ path: String? = nil) {
        guard let replacement = mentions?.replacement(in: string, path: path) else { return }
        mentions?.close()
        insertText(replacement.text, replacementRange: replacement.range)
        window?.makeFirstResponder(self)
        mentionChanged?()
    }

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        if window?.firstResponder === self, isEditable, !hasMarkedText(),
           [36, 76].contains(event.keyCode),
           event.modifierFlags.intersection([.command, .control, .option, .shift]) == .command {
            insertNewline(nil)
            return true
        }
        return super.performKeyEquivalent(with: event)
    }

    var cancel: (() -> Void)?

    override func keyDown(with event: NSEvent) {
        if !hasMarkedText(), event.keyCode == 53, let cancel { cancel(); return }
        if isEditable, !hasMarkedText(), !shellPrefix.isEmpty, event.keyCode == 53,
           event.modifierFlags.intersection([.command, .control, .option, .shift]).isEmpty {
            exitShell?(); return
        }
        if isEditable, !hasMarkedText(), commands?.isOpen == true,
           event.modifierFlags.intersection([.command, .control, .option, .shift]).isEmpty {
            switch event.keyCode {
            case 126: commands?.move(-1)
            case 125: commands?.move(1)
            case 36, 76, 48: insertCommand()
            case 53: commands?.close()
            default: super.keyDown(with: event); return
            }
            mentionChanged?(); return
        }

        if isEditable, !hasMarkedText(), mentions?.isOpen == true,
           event.modifierFlags.intersection([.command, .control, .option, .shift]).isEmpty {
            switch event.keyCode {
            case 126: mentions?.move(-1)
            case 125: mentions?.move(1)
            case 36, 76: insertMention()
            case 53: mentions?.close()
            default: super.keyDown(with: event); return
            }
            mentionChanged?(); return
        }
        if isEditable, !hasMarkedText(), [36, 76].contains(event.keyCode) {
            let modifiers = event.modifierFlags.intersection([.command, .control, .option, .shift])
            if modifiers.isEmpty { submit?(); return }
            if modifiers == .command { insertNewline(nil); return }
        }
        super.keyDown(with: event)
    }

    private func attachmentProviders(_ sender: NSDraggingInfo) -> [NSItemProvider] {
        (sender.draggingPasteboard.pasteboardItems ?? []).compactMap { item in
            if let value = item.string(forType: .fileURL), let url = URL(string: value), url.isFileURL {
                return NSItemProvider(item: url as NSURL, typeIdentifier: UTType.fileURL.identifier)
            }
            guard let type = item.types.first(where: { UTType($0.rawValue)?.conforms(to: .image) == true }),
                  let data = item.data(forType: type) else { return nil }
            return NSItemProvider(item: data as NSData, typeIdentifier: type.rawValue)
        }
    }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        guard !attachmentProviders(sender).isEmpty else { return super.draggingEntered(sender) }
        attachmentDropTargeted?(true)
        return .copy
    }

    override func draggingUpdated(_ sender: NSDraggingInfo) -> NSDragOperation {
        attachmentProviders(sender).isEmpty ? super.draggingUpdated(sender) : .copy
    }

    override func draggingExited(_ sender: NSDraggingInfo?) {
        attachmentDropTargeted?(false)
        super.draggingExited(sender)
    }

    override func prepareForDragOperation(_ sender: NSDraggingInfo) -> Bool {
        !attachmentProviders(sender).isEmpty || super.prepareForDragOperation(sender)
    }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        attachmentDropTargeted?(false)
        let providers = attachmentProviders(sender)
        if !providers.isEmpty { return attachmentDrop?(providers) ?? false }
        return super.performDragOperation(sender)
    }


    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        guard string.isEmpty, let font else { return }
        (placeholder as NSString).draw(
            at: textContainerOrigin,
            withAttributes: [.font: font, .foregroundColor: placeholderColor]
        )
    }
}
