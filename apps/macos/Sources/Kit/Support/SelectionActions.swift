import AppKit
import QuartzCore
import SwiftUI

/// Window-scoped selection actions; the panel never becomes the key window.
/// Selected text that can be commented on (ADR 0070) offers Comment beside
/// it and first in its context menu.
struct SelectionActions: NSViewRepresentable {
    let theme: MicaTheme
    var enabled = true
    let comments: TextCommentDraft

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        context.coordinator.host = view
        context.coordinator.start()
        return view
    }
    func updateNSView(_ view: NSView, context: Context) {
        context.coordinator.enabled = enabled
        if !enabled { context.coordinator.dismiss() }
        if context.coordinator.theme != theme {
            context.coordinator.dismiss()
            context.coordinator.theme = theme
        }
        context.coordinator.comments = comments
    }
    func makeCoordinator() -> Coordinator { Coordinator(theme: theme, enabled: enabled, comments: comments) }
    static func dismantleNSView(_ view: NSView, coordinator: Coordinator) { coordinator.stop() }

    @MainActor final class Coordinator {
        weak var host: NSView?
        var theme: MicaTheme
        var enabled: Bool
        var comments: TextCommentDraft
        var monitor: Any?
        var panel: NSPanel?
        var presentedSelection: Selection?
        var selectionBeforeMouseDown: Selection?
        var pendingPresentation: DispatchWorkItem?
        var selectionCheck: Timer?
        var mouseSelectionPoll: Timer?

        @MainActor struct Selection {
            let text: String
            let rect: NSRect
            let source: NSObject
            let range: NSRange?
            let responder: NSResponder?
            let viaAccessibility: Bool

            func isNew(comparedTo previous: Selection?) -> Bool {
                guard let previous else { return true }
                return text != previous.text || range != previous.range
                    || !(source === previous.source || source.isEqual(previous.source))
            }

            func isCurrent(in window: NSWindow) -> Bool {
                guard window.firstResponder === responder else { return false }
                if !viaAccessibility, let client = source as? NSTextInputClient, let range {
                    guard client.selectedRange() == range,
                          let selected = client.attributedSubstring(forProposedRange: range, actualRange: nil),
                          selected.string == text else { return false }
                    let currentRect = client.firstRect(forCharacterRange: range, actualRange: nil)
                    return abs(currentRect.midX - rect.midX) < 3 && abs(currentRect.midY - rect.midY) < 3
                }
                guard source.accessibilityAttributeValue(.selectedText) as? String == text else { return false }
                let currentRange = (source.accessibilityAttributeValue(.selectedTextRange) as? NSValue)?.rangeValue
                return currentRange == range
            }
        }
        init(theme: MicaTheme, enabled: Bool = true, comments: TextCommentDraft) {
            self.theme = theme; self.enabled = enabled; self.comments = comments
        }

        /// What commenting on a selection does: begin a line annotation
        /// where its editor takes them, or open the comment box for a text
        /// comment.
        enum Destination {
            case lines(() -> Void)
            case text(TextCommentSource)
        }

        /// Where a comment on selection goes, or nil where it can't be
        /// commented on.
        func destination(for selection: Selection, in window: NSWindow) -> Destination? {
            if !selection.viaAccessibility, let range = selection.range,
               let begin = SelectionLineComments.handler(for: selection.responder as? NSView)?(range) {
                return .lines(begin)
            }
            guard let root = window.contentView else { return nil }
            let point = window.convertPoint(fromScreen: NSPoint(x: selection.rect.midX, y: selection.rect.midY))
            guard let region = CommentSourceView.containing(point, in: root) else { return nil }
            if let begin = region.lines?() { return .lines(begin) }
            return region.source.map(Destination.text)
        }

        /// Comments on selection: begins its line annotation, or opens the
        /// comment box at it.
        func comment(on selection: Selection, in window: NSWindow) {
            switch destination(for: selection, in: window) {
            case .lines(let begin)?:
                begin()
            case .text(let source)?:
                comments.present(quote: selection.text, source: source, at: selection.rect, in: window, theme: theme)
            case nil: break
            }
        }

        /// The context menu for a click on a selection that can be
        /// commented on: Comment, then what the text view offers, or Copy.
        func commentMenu(for event: NSEvent, in window: NSWindow) -> NSMenu? {
            guard let selection = selection(in: window), destination(for: selection, in: window) != nil else { return nil }
            let menu = OwningMenu()
            let target = SelectionCommentMenuTarget { [weak self, weak window] in
                guard let self, let window else { return }
                self.comment(on: selection, in: window)
            }
            menu.owner = target
            let item = NSMenuItem(title: "Comment", action: #selector(SelectionCommentMenuTarget.comment), keyEquivalent: "")
            item.target = target
            menu.addItem(item)
            menu.addItem(.separator())
            if let view = selection.responder as? NSTextView, let native = view.menu(for: event) {
                let items = native.items
                native.removeAllItems()
                items.forEach(menu.addItem)
            } else {
                menu.addItem(NSMenuItem(title: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: ""))
            }
            return menu
        }
        func start() {
            monitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseUp, .leftMouseDown, .rightMouseDown, .keyUp, .keyDown, .scrollWheel]) { [weak self] event in
                guard let self, self.enabled, let window = self.host?.window else { return event }
                if event.window === self.panel { return event }
                guard event.window === window else { self.dismiss(); return event }
                // A link's own menu, or ⌘-click, wins over the message's
                // menu and AppKit's.
                if TextLinkHover.takes(event) {
                    self.dismiss()
                    return event
                }
                if event.type == .rightMouseDown, self.hasSelection(in: window) {
                    self.dismiss()
                    if let menu = self.commentMenu(for: event, in: window), let view = window.contentView {
                        NSMenu.popUpContextMenu(menu, with: event, for: view)
                        return nil
                    }
                    return event
                }
                if event.type == .rightMouseDown, let root = window.contentView,
                   let message = self.messageRegion(in: root, event: event) {
                    self.dismiss()
                    let menu = NSMenu()
                    for item in MessageCopyTarget.items(copying: message.markdown) { menu.addItem(item) }
                    NSMenu.popUpContextMenu(menu, with: event, for: message)
                    return nil
                }
                switch event.type {
                case .leftMouseDown:
                    self.selectionBeforeMouseDown = self.selection(in: window)
                    self.dismiss()
                    self.pollForMouseSelection(in: window)
                case .leftMouseUp:
                    self.mouseSelectionPoll?.invalidate()
                    self.mouseSelectionPoll = nil
                    self.scheduleSelection(after: self.selectionBeforeMouseDown)
                    self.selectionBeforeMouseDown = nil
                case .keyDown:
                    // A nonactivating mouse-only panel is not useful for keyboard selection.
                    self.dismiss()
                case .scrollWheel:
                    self.dismiss()
                default: break
                }
                return event
            }
        }
        func hasSelection(in window: NSWindow) -> Bool {
            if let client = window.firstResponder as? NSTextInputClient,
               !(client is ComposerTextView), client.selectedRange().length > 0 {
                return true
            }
            if let element = window.accessibilityHitTest(NSEvent.mouseLocation) as? NSObject,
               let text = element.accessibilityAttributeValue(.selectedText) as? String {
                return !text.isEmpty
            }
            return false
        }
        func messageRegion(in view: NSView, event: NSEvent) -> MessageCopyView? {
            MessageCopyView.containing(event, in: view)
        }
        func stop() {
            if let monitor { NSEvent.removeMonitor(monitor) }
            monitor = nil
            dismiss()
        }
        func dismiss() {
            mouseSelectionPoll?.invalidate()
            mouseSelectionPoll = nil
            pendingPresentation?.cancel()
            pendingPresentation = nil
            selectionCheck?.invalidate()
            selectionCheck = nil
            if let panel { panel.parent?.removeChildWindow(panel) }
            panel?.orderOut(nil)
            panel = nil
            presentedSelection = nil
        }
        // SwiftUI's selectable Text can consume the mouse-up in its own tracking
        // loop, bypassing local event monitors. The timer resumes once tracking
        // ends and captures the newly selected text without relying on mouse-up.
        func pollForMouseSelection(in window: NSWindow) {
            mouseSelectionPoll?.invalidate()
            mouseSelectionPoll = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self, weak window] timer in
                guard NSEvent.pressedMouseButtons & 1 == 0 else { return }
                timer.invalidate()
                MainActor.assumeIsolated {
                    guard let self, self.enabled, self.host?.window === window,
                          let window, window.isKeyWindow else { return }
                    self.mouseSelectionPoll = nil
                    let previous = self.selectionBeforeMouseDown
                    self.selectionBeforeMouseDown = nil
                    guard self.selectionAfterMouseGesture(in: window, after: previous, mouseIsDown: false) != nil else { return }
                    self.scheduleSelection(after: previous)
                }
            }
        }
        func selectionAfterMouseGesture(in window: NSWindow, after previous: Selection?, mouseIsDown: Bool) -> Selection? {
            guard !mouseIsDown, let selection = selection(in: window), selection.isNew(comparedTo: previous) else { return nil }
            return selection
        }
        func scheduleSelection(after previous: Selection?) {
            pendingPresentation?.cancel()
            // Read the selection after AppKit handles mouse-up but before the
            // pointer can move off the selected text. Delay only the panel.
            let capture = DispatchWorkItem { [weak self] in
                guard let self, self.enabled, self.monitor != nil,
                      let window = self.host?.window, window.isKeyWindow else { return }
                self.pendingPresentation = nil
                guard let selection = self.selectionAfterMouseGesture(in: window, after: previous, mouseIsDown: false) else { return }
                let present = DispatchWorkItem { [weak self] in
                    guard let self, self.enabled, self.monitor != nil,
                          self.host?.window === window, window.isKeyWindow,
                          selection.isCurrent(in: window) else { return }
                    self.pendingPresentation = nil
                    self.showSelection(selection, in: window)
                }
                self.pendingPresentation = present
                DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(180), execute: present)
            }
            pendingPresentation = capture
            DispatchQueue.main.async(execute: capture)
        }
        func selection(in window: NSWindow, accessibilityElement: NSObject? = nil) -> Selection? {
            if let client = window.firstResponder as? NSTextInputClient, !(client is ComposerTextView),
               let source = window.firstResponder {
                let range = client.selectedRange()
                if range.length > 0,
                   let selected = client.attributedSubstring(forProposedRange: range, actualRange: nil),
                   !selected.string.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    return Selection(text: selected.string,
                                     rect: client.firstRect(forCharacterRange: range, actualRange: nil),
                                     source: source, range: range, responder: source, viaAccessibility: false)
                }
            }
            let point = NSEvent.mouseLocation
            if let composer = window.firstResponder as? ComposerTextView,
               composer.bounds.contains(composer.convert(window.convertPoint(fromScreen: point), from: nil)) { return nil }
            guard let element = accessibilityElement ?? window.accessibilityHitTest(point) as? NSObject,
                  let text = element.accessibilityAttributeValue(.selectedText) as? String,
                  !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
            return Selection(text: text,
                             rect: NSRect(x: point.x, y: point.y, width: 1, height: 18),
                             source: element,
                             range: (element.accessibilityAttributeValue(.selectedTextRange) as? NSValue)?.rangeValue,
                             responder: window.firstResponder, viaAccessibility: true)
        }
        func checkPresentedSelection() {
            guard let window = host?.window, window.isKeyWindow,
                  let presentedSelection, presentedSelection.isCurrent(in: window) else { dismiss(); return }
        }
        func showSelection(_ selection: Selection, in window: NSWindow) {
            guard enabled, window.isKeyWindow, destination(for: selection, in: window) != nil else { dismiss(); return }
            var rect = selection.rect
            let selectionTop = rect.maxY
            dismiss()
            let panel = NSPanel(contentRect: .zero, styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
            panel.isOpaque = false
            panel.backgroundColor = .clear
            panel.hasShadow = true
            panel.hidesOnDeactivate = true
            let content = SelectionActionsMenu(theme: theme) { [weak self, weak window] in
                guard let self, let window else { return }
                self.dismiss()
                self.comment(on: selection, in: window)
            }
            let hosting = NSHostingView(rootView: content)
            panel.contentView = hosting
            let size = hosting.fittingSize
            let visible = window.screen?.visibleFrame ?? window.frame
            rect.origin.x = min(max(rect.midX - size.width / 2, visible.minX + 8), visible.maxX - size.width - 8)
            rect.origin.y = rect.minY - size.height - 6
            if rect.minY < visible.minY { rect.origin.y = selectionTop + 6 }
            let targetFrame = NSRect(origin: rect.origin, size: size)
            let reduceMotion = NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
            panel.setFrame(targetFrame.offsetBy(dx: 0, dy: reduceMotion ? 0 : -4), display: true)
            panel.alphaValue = 0
            window.addChildWindow(panel, ordered: .above)
            panel.orderFront(nil)
            self.panel = panel
            presentedSelection = selection
            selectionCheck = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self] _ in
                MainActor.assumeIsolated { self?.checkPresentedSelection() }
            }
            NSAnimationContext.runAnimationGroup { context in
                context.duration = reduceMotion ? 0.1 : 0.16
                context.timingFunction = CAMediaTimingFunction(name: .easeOut)
                panel.animator().alphaValue = 1
                if !reduceMotion { panel.animator().setFrame(targetFrame, display: true) }
            }
        }
    }
}

/// The pop-up's card: the shared popover surface (4 pt corners, hairline
/// border, window shadow) around menu rows like the tile add menu's, so it
/// reads as one of the app's anchored menus rather than a floating pill. It
/// sizes to its content, since it holds one action centred on the selection.
struct SelectionActionsMenu: View {
    let theme: MicaTheme
    let comment: () -> Void
    @State private var hovering = false

    static let rowHeight: CGFloat = 28

    var body: some View {
        Button(action: comment) {
            HStack(spacing: 8) {
                IconView(.chatCenteredText).foregroundStyle(theme.muted)
                Text("Comment").font(.kit(size: 12))
            }
            .padding(.horizontal, 8)
            .frame(height: Self.rowHeight)
            .background(hovering ? theme.hover : .clear, in: RoundedRectangle(cornerRadius: 3))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .padding(4)
        .fixedSize()
        .foregroundStyle(theme.text)
        .background(theme.raised)
        .popoverCard(theme)
    }
}

struct MessageCopyRegion: NSViewRepresentable {
    let markdown: String
    func makeNSView(context: Context) -> MessageCopyView { MessageCopyView() }
    func updateNSView(_ view: MessageCopyView, context: Context) { view.markdown = markdown }
}
final class MessageCopyView: NSView {
    var markdown = ""
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    /// The message region in `view` that `event` is over.
    static func containing(_ event: NSEvent, in view: NSView) -> MessageCopyView? {
        guard !view.isHidden else { return nil }
        if let region = view as? MessageCopyView,
           region.bounds.contains(region.convert(event.locationInWindow, from: nil)) { return region }
        for child in view.subviews {
            if let result = containing(event, in: child) { return result }
        }
        return nil
    }
}
@MainActor final class MessageCopyTarget: NSObject {
    let markdown: String
    init(markdown: String) { self.markdown = markdown }

    /// Copy Message as Markdown and as Plain Text, for `markdown`. Each item
    /// keeps the target that carries it out.
    static func items(copying markdown: String) -> [NSMenuItem] {
        let target = MessageCopyTarget(markdown: markdown)
        return [("Copy Message as Markdown", #selector(copyMarkdown)), ("Copy Message as Plain Text", #selector(copyPlain))].map { title, action in
            let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
            item.target = target
            item.representedObject = target
            return item
        }
    }
    @objc func copyMarkdown() { copy(markdown) }
    @objc func copyPlain() { copy(MessageText.plain(markdown)) }
    private func copy(_ text: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }
}

/// Carries out a selection's Comment menu item.
@MainActor final class SelectionCommentMenuTarget: NSObject {
    let action: () -> Void
    init(_ action: @escaping () -> Void) { self.action = action }
    @objc func comment() { action() }
}
