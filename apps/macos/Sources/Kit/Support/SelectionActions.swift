import AppKit
import QuartzCore
import SwiftUI

/// Window-scoped selection actions; the panel never becomes the key window.
struct SelectionActions: NSViewRepresentable {
    let theme: MicaTheme
    var enabled = true
    let quote: (String) -> Void

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
        context.coordinator.quote = quote
    }
    func makeCoordinator() -> Coordinator { Coordinator(theme: theme, enabled: enabled, quote: quote) }
    static func dismantleNSView(_ view: NSView, coordinator: Coordinator) { coordinator.stop() }

    @MainActor final class Coordinator {
        weak var host: NSView?
        var theme: MicaTheme
        var enabled: Bool
        var quote: (String) -> Void
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
        init(theme: MicaTheme, enabled: Bool = true, quote: @escaping (String) -> Void) { self.theme = theme; self.enabled = enabled; self.quote = quote }
        func start() {
            monitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseUp, .leftMouseDown, .rightMouseDown, .keyUp, .keyDown, .scrollWheel]) { [weak self] event in
                guard let self, self.enabled, let window = self.host?.window else { return event }
                if event.window === self.panel { return event }
                guard event.window === window else { self.dismiss(); return event }
                if event.type == .rightMouseDown, self.hasSelection(in: window) {
                    self.dismiss()
                    return event
                }
                if event.type == .rightMouseDown, let root = window.contentView,
                   let message = self.messageRegion(in: root, event: event) {
                    self.dismiss()
                    let menu = NSMenu()
                    let target = MessageCopyTarget(markdown: message.markdown)
                    for (title, action) in [("Copy Message as Markdown", #selector(MessageCopyTarget.copyMarkdown)),
                                            ("Copy Message as Plain Text", #selector(MessageCopyTarget.copyPlain))] {
                        let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
                        item.target = target
                        menu.addItem(item)
                    }
                    withExtendedLifetime(target) { NSMenu.popUpContextMenu(menu, with: event, for: message) }
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
            guard !view.isHidden else { return nil }
            if let region = view as? MessageCopyView,
               region.bounds.contains(region.convert(event.locationInWindow, from: nil)) { return region }
            for child in view.subviews {
                if let result = messageRegion(in: child, event: event) { return result }
            }
            return nil
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
            guard enabled, window.isKeyWindow else { dismiss(); return }
            let text = selection.text
            var rect = selection.rect
            let selectionTop = rect.maxY
            dismiss()
            let panel = NSPanel(contentRect: .zero, styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
            panel.isOpaque = false
            panel.backgroundColor = .clear
            panel.hasShadow = true
            panel.hidesOnDeactivate = true
            let content = Button { [weak self] in self?.dismiss(); self?.quote(text) } label: {
                Label("Quote", systemImage: "text.quote").padding(.horizontal, 12).padding(.vertical, 9)
            }.buttonStyle(.plain).font(.kit(size: 12))
                .foregroundStyle(theme.text).background(theme.raised, in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(theme.border))
                .fixedSize()
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

struct MessageCopyRegion: NSViewRepresentable {
    let markdown: String
    func makeNSView(context: Context) -> MessageCopyView { MessageCopyView() }
    func updateNSView(_ view: MessageCopyView, context: Context) { view.markdown = markdown }
}
final class MessageCopyView: NSView {
    var markdown = ""
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
}
@MainActor final class MessageCopyTarget: NSObject {
    let markdown: String
    init(markdown: String) { self.markdown = markdown }
    @objc func copyMarkdown() { copy(markdown) }
    @objc func copyPlain() { copy(MessageText.plain(markdown)) }
    private func copy(_ text: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }
}
