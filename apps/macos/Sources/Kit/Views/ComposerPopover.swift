import AppKit
import SwiftUI

/// A composer chip's popover: a flat card 6 pt above the chip half that opened
/// it, aligned to that half's left edge, with no arrow. It takes keyboard focus so arrows,
/// Return, and Esc reach the picker, but never becomes the main window, so the
/// session window keeps its active appearance. Any click outside closes it.
@MainActor final class ComposerPopover {
    private final class Panel: NSPanel {
        var cancel: (() -> Void)?
        override var canBecomeKey: Bool { true }
        override var canBecomeMain: Bool { false }
        override func cancelOperation(_ sender: Any?) { cancel?() }
    }

    static let gap: CGFloat = 6

    private var panel: Panel?
    private var observers: [NSObjectProtocol] = []
    private var bottom: CGFloat = 0
    private var closed: (() -> Void)?
    /// When the popover last closed. A click on the chip first closes an open
    /// popover by taking focus from it; the chip uses this to stay closed.
    private(set) var closedAt: Date?

    var isOpen: Bool { panel != nil }

    /// Opens `content` above `anchor`, a rectangle in screen coordinates.
    func show(anchor: NSRect, parent: NSWindow, content: some View, closed: @escaping () -> Void) {
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
        bottom = anchor.maxY + Self.gap
        panel.setFrame(Self.frame(anchor: anchor, size: size, screen: parent.screen?.visibleFrame), display: false)
        self.panel = panel
        self.closed = closed
        let center = NotificationCenter.default
        observers = [
            center.addObserver(forName: NSWindow.didResignKeyNotification, object: panel, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.dismiss() }
            },
            // Content grows and shrinks (a turn starts, the model changes);
            // keep the card's bottom edge above the chip.
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
        guard let panel, panel.frame.minY != bottom else { return }
        panel.setFrameOrigin(NSPoint(x: panel.frame.minX, y: bottom))
    }

    static func frame(anchor: NSRect, size: CGSize, screen: NSRect?) -> NSRect {
        var frame = NSRect(x: anchor.minX, y: anchor.maxY + gap, width: size.width, height: size.height)
        if let screen {
            frame.origin.x = min(max(frame.minX, screen.minX), screen.maxX - frame.width)
            frame.size.height = min(frame.height, max(0, screen.maxY - frame.minY))
        }
        return frame
    }
}

/// The model half's popover: the session's models, dimmed while a turn runs.
/// Picking a model applies it and closes the popover.
struct ComposerModelMenu: View {
    @Bindable var state: SessionStore
    let theme: MicaTheme
    let dismiss: () -> Void

    static let lockedNote = "Model can change when the turn ends"

    var body: some View {
        ComposerModelPicker(models: state.configuration.models, selectedID: state.model,
            select: { id in
                dismiss()
                guard id != state.model, !state.configuration.changing else { return }
                Task { await state.configure(model: id) }
            }, reload: {
                Task { await state.loadComposer() }
            }, dismiss: dismiss,
            lockedNote: state.running ? Self.lockedNote : nil)
            .popoverCard(theme)
            .accessibilityLabel("Model")
    }
}

/// The thinking half's popover: the model's levels under a caption, the
/// current one checked. Picking a level applies it and closes the popover.
struct ComposerThinkingMenu: View {
    let theme: MicaTheme
    let levels: [String]
    let selected: String
    let select: (String) -> Void
    let dismiss: () -> Void
    @State private var selection = 0
    @FocusState private var focused: Bool

    static let minimumWidth: CGFloat = 128
    static let rowHeight: CGFloat = 26

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("THINKING").font(.kit(size: 10, weight: .medium)).tracking(0.8)
                .foregroundStyle(theme.muted)
                .padding(.horizontal, 8).frame(height: 24)
                .accessibilityAddTraits(.isHeader)
            ForEach(Array(levels.enumerated()), id: \.element) { index, level in
                Button { pick(level) } label: {
                    HStack(spacing: 8) {
                        Image(systemName: "checkmark").font(.kit(size: 10, weight: .semibold))
                            .opacity(level == selected ? 1 : 0).frame(width: 12)
                        Text(level).font(.kit(size: 12)).lineLimit(1)
                        Spacer(minLength: 8)
                    }
                    .foregroundStyle(theme.text)
                    .padding(.horizontal, 8)
                    .frame(height: Self.rowHeight)
                    .background(selection == index ? theme.accent.opacity(0.18) : .clear,
                                in: RoundedRectangle(cornerRadius: 3))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .onHover { if $0 { selection = index } }
                .accessibilityAddTraits(level == selected ? .isSelected : [])
            }
        }
        .padding(4)
        .frame(minWidth: Self.minimumWidth, alignment: .leading)
        .fixedSize()
        .background(theme.surface)
        .focusable().focused($focused)
        .focusEffectDisabled()
        .onAppear {
            selection = levels.firstIndex(of: selected) ?? 0
            focused = true
        }
        .onKeyPress(.downArrow) { selection = min(selection + 1, levels.count - 1); return .handled }
        .onKeyPress(.upArrow) { selection = max(selection - 1, 0); return .handled }
        .onKeyPress(.return) {
            guard levels.indices.contains(selection) else { return .ignored }
            pick(levels[selection]); return .handled
        }
        .onExitCommand(perform: dismiss)
        .popoverCard(theme)
        .accessibilityLabel("Thinking")
    }

    private func pick(_ level: String) {
        dismiss()
        if level != selected { select(level) }
    }
}

private extension View {
    /// The popover's surface: 4 pt corners and a hairline border.
    func popoverCard(_ theme: MicaTheme) -> some View {
        clipShape(RoundedRectangle(cornerRadius: 4))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
            .environment(\.mica, theme)
            .preferredColorScheme(theme.dark ? .dark : .light)
    }
}
