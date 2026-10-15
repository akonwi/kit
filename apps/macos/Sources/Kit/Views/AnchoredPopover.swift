import AppKit
import SwiftUI

/// A flat popover card pinned 6 pt from the control that opened it, with no
/// arrow. Composer chips open theirs above, aligned to the chip's left edge;
/// the Diff pane's header controls open theirs below. It takes keyboard focus
/// so arrows, Return, and Esc reach the content, but never becomes the main
/// window, so the session window keeps its active appearance. Any click
/// outside closes it.
@MainActor final class AnchoredPopover {
    private final class Panel: NSPanel {
        var cancel: (() -> Void)?
        override var canBecomeKey: Bool { true }
        override var canBecomeMain: Bool { false }
        override func cancelOperation(_ sender: Any?) { cancel?() }
    }

    /// Which side of the anchor the card opens on.
    enum Edge { case above, below }
    /// Which of the anchor's edges the card lines up with.
    enum Alignment { case leading, trailing }

    static let gap: CGFloat = 6

    private var panel: Panel?
    private var observers: [NSObjectProtocol] = []
    private var anchor: NSRect = .zero
    private var edge = Edge.above
    private var alignment = Alignment.leading
    private var closed: (() -> Void)?
    /// When the popover last closed. A click on the control first closes an
    /// open popover by taking focus from it; the control uses this to stay closed.
    private(set) var closedAt: Date?

    var isOpen: Bool { panel != nil }

    /// Opens `content` beside `anchor`, a rectangle in screen coordinates.
    func show(anchor: NSRect, parent: NSWindow, edge: Edge = .above, alignment: Alignment = .leading,
              content: some View, closed: @escaping () -> Void) {
        dismiss()
        let panel = Panel(contentRect: .zero, styleMask: [.borderless], backing: .buffered, defer: false)
        panel.isReleasedWhenClosed = false
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.hidesOnDeactivate = true
        panel.cancel = { [weak self] in self?.dismiss() }
        let controller = NSHostingController(rootView: AnyView(content))
        controller.sizingOptions = [.preferredContentSize]
        panel.contentViewController = controller
        let size = controller.view.fittingSize
        self.anchor = anchor
        self.edge = edge
        self.alignment = alignment
        panel.setFrame(Self.frame(anchor: anchor, size: size, screen: parent.screen?.visibleFrame,
                                  edge: edge, alignment: alignment), display: false)
        self.panel = panel
        self.closed = closed
        let center = NotificationCenter.default
        observers = [
            center.addObserver(forName: NSWindow.didResignKeyNotification, object: panel, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.dismiss() }
            },
            // Content grows and shrinks (a turn starts, the list refreshes);
            // keep the card's edge against the control.
            center.addObserver(forName: NSWindow.didResizeNotification, object: panel, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.pin() }
            },
        ]
        parent.addChildWindow(panel, ordered: .above)
        panel.makeKeyAndOrderFront(nil)
    }

    func dismiss() {
        guard let panel else { return }
        observers.forEach(NotificationCenter.default.removeObserver)
        observers = []
        self.panel = nil
        let wasKey = panel.isKeyWindow
        let parent = panel.parent
        parent?.removeChildWindow(panel)
        panel.orderOut(nil)
        panel.close()
        if wasKey { parent?.makeKey() }
        closedAt = Date()
        let closed = self.closed
        self.closed = nil
        closed?()
    }

    private func pin() {
        guard let panel else { return }
        let frame = Self.frame(anchor: anchor, size: panel.frame.size, screen: nil, edge: edge, alignment: alignment)
        guard panel.frame.origin != frame.origin else { return }
        panel.setFrameOrigin(frame.origin)
    }

    static func frame(anchor: NSRect, size: CGSize, screen: NSRect?,
                      edge: Edge = .above, alignment: Alignment = .leading) -> NSRect {
        let x = alignment == .leading ? anchor.minX : anchor.maxX - size.width
        let y = edge == .above ? anchor.maxY + gap : anchor.minY - gap - size.height
        var frame = NSRect(x: x, y: y, width: size.width, height: size.height)
        if let screen {
            frame.origin.x = min(max(frame.minX, screen.minX), screen.maxX - frame.width)
            switch edge {
            case .above:
                frame.size.height = min(frame.height, max(0, screen.maxY - frame.minY))
            case .below:
                let top = frame.maxY
                frame.size.height = min(frame.height, max(0, top - screen.minY))
                frame.origin.y = top - frame.height
            }
        }
        return frame
    }
}

extension View {
    /// The popover's surface: 4 pt corners and a hairline border.
    func popoverCard(_ theme: MicaTheme) -> some View {
        clipShape(RoundedRectangle(cornerRadius: 4))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
            .environment(\.mica, theme)
            .preferredColorScheme(theme.dark ? .dark : .light)
    }
}
