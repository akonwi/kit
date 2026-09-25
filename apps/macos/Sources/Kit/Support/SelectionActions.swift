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
        context.coordinator.theme = theme
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
        var presentedText: String?
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
                if event.type == .leftMouseDown || event.type == .scrollWheel || (event.type == .keyDown && event.keyCode == 53) {
                    self.dismiss()
                    if event.type == .leftMouseDown { self.afterSelectionGesture() }
                } else if event.type == .leftMouseUp || (event.type == .keyUp && event.modifierFlags.contains(.shift)) {
                    DispatchQueue.main.async { [weak self] in self?.showSelection() }
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
            if let panel { panel.parent?.removeChildWindow(panel) }
            panel?.orderOut(nil)
            panel = nil
            presentedText = nil
        }
        func afterSelectionGesture() {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) { [weak self] in
                guard let self, self.enabled, self.monitor != nil, self.host?.window?.isKeyWindow == true else { return }
                if NSEvent.pressedMouseButtons & 1 != 0 { self.afterSelectionGesture() }
                else { self.showSelection() }
            }
        }
        func showSelection() {
            guard enabled, let window = host?.window, window.isKeyWindow else { dismiss(); return }
            var text: String
            var rect: NSRect
            if let client = window.firstResponder as? NSTextInputClient,
               !(client is ComposerTextView), client.selectedRange().length > 0,
               let selected = client.attributedSubstring(forProposedRange: client.selectedRange(), actualRange: nil) {
                text = selected.string
                rect = client.firstRect(forCharacterRange: client.selectedRange(), actualRange: nil)
            } else if let element = window.accessibilityHitTest(NSEvent.mouseLocation) as? NSObject,
                      let selected = element.accessibilityAttributeValue(.selectedText) as? String, !selected.isEmpty {
                text = selected
                rect = NSRect(x: NSEvent.mouseLocation.x, y: NSEvent.mouseLocation.y, width: 1, height: 18)
            } else { dismiss(); return }
            guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { dismiss(); return }
            if panel != nil, presentedText == text { return }
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
            presentedText = text
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
