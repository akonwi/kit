import AppKit
import SwiftUI

/// A nonactivating completion surface: all typing and key commands stay in NSTextView.
@MainActor final class ComposerMentionPanel {
    private final class Panel: NSPanel {
        override var canBecomeKey: Bool { false }
        override var canBecomeMain: Bool { false }
    }
    private var panel: NSPanel?
    private var observer: NSObjectProtocol?

    func dismiss() {
        if let observer { NotificationCenter.default.removeObserver(observer); self.observer = nil }
        if let panel { panel.parent?.removeChildWindow(panel); panel.close() }
        panel = nil
    }
    func show(editor: ComposerTextView, state: ComposerMentionState, theme: MicaTheme,
              select: @escaping (String) -> Void, retry: @escaping () -> Void) {
        present(editor: editor, open: state.isOpen,
                height: CGFloat(max(1, state.visibleMatches.count) * 30 + 34 + (state.error == nil ? 0 : 34) + (state.indexNotice == nil ? 0 : 26)),
                label: "File mentions", close: { [weak state] in state?.close() },
                content: AnyView(ComposerMentionList(state: state, select: select, retry: retry)
                    .environment(\.mica, theme).preferredColorScheme(theme.dark ? .dark : .light)))
    }

    func showCommands(editor: ComposerTextView, state: ComposerCommandState, theme: MicaTheme,
                      select: @escaping (String) -> Void) {
        present(editor: editor, open: state.isOpen,
                height: CGFloat(state.visibleMatches.count * 46 + 34), label: "Prompt commands",
                close: { [weak state] in state?.close() },
                content: AnyView(ComposerCommandList(state: state, select: select)
                    .environment(\.mica, theme).preferredColorScheme(theme.dark ? .dark : .light)))
    }

    private func present(editor: ComposerTextView, open: Bool, height: CGFloat,
                         label: String, close: @escaping @MainActor @Sendable () -> Void, content: AnyView) {
        guard open, editor.isEditable, let window = editor.window, window.isKeyWindow,
              window.firstResponder === editor else { dismiss(); return }

        let panel: NSPanel
        if let existing = self.panel { panel = existing }
        else {
            panel = Panel(contentRect: .zero, styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
            panel.isReleasedWhenClosed = false; panel.isOpaque = false; panel.backgroundColor = .clear
            panel.hasShadow = true; panel.hidesOnDeactivate = true
            panel.setAccessibilityLabel(label)
            self.panel = panel
            window.addChildWindow(panel, ordered: .above)
            observer = NotificationCenter.default.addObserver(forName: NSWindow.didResignKeyNotification, object: window, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { close(); self?.dismiss() }
            }
        }
        guard let anchor = editor.pickerAnchor?.screenFrame, anchor.width > 0 else { dismiss(); return }
        let screen = window.screen?.visibleFrame ?? window.frame
        let frame = Self.frame(above: anchor, height: height, screen: screen)
        panel.contentView = NSHostingView(rootView: ScrollView(.vertical) {
            content.frame(maxWidth: .infinity, alignment: .leading)
        }.scrollIndicators(.hidden))
        panel.setFrame(frame, display: true)
        panel.orderFront(nil)
    }

    static func frame(above composer: NSRect, height: CGFloat, screen: NSRect) -> NSRect {
        let bottom = composer.maxY + 6
        return NSRect(x: composer.minX, y: bottom, width: composer.width,
                      height: min(height, max(0, screen.maxY - bottom)))
    }

}

private struct ComposerMentionList: View {
    @Environment(\.mica) private var theme
    @Bindable var state: ComposerMentionState
    var select: (String) -> Void
    var retry: () -> Void
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if state.visibleMatches.isEmpty {
                Text(state.loading ? "Loading…" : state.error ?? "No results")
                    .foregroundStyle(theme.muted).lineLimit(1).frame(height: 30).padding(.horizontal, 10)
            }
            ForEach(state.visibleMatches, id: \.self) { path in
                Button { select(path) } label: {
                    HStack(spacing: 8) {
                        Image(systemName: path.hasSuffix("/") ? "folder" : "doc")
                        Text(path).lineLimit(1).truncationMode(.middle)
                        Spacer(minLength: 0)
                        if path.hasSuffix("/") { Text("directory").foregroundStyle(theme.muted) }
                    }.padding(.horizontal, 10).frame(height: 30)
                        .background(state.selected == path ? theme.hover : Color.clear)
                        .contentShape(Rectangle())
                }.buttonStyle(.plain).accessibilityAddTraits(state.selected == path ? .isSelected : [])
            }
            if let notice = state.indexNotice { Text(notice).font(.kit(size: 10)).foregroundStyle(theme.muted).padding(.horizontal, 10).frame(height: 26) }
            if state.error != nil { Button("Retry", action: retry).buttonStyle(.plain).padding(8) }
            Text("↑↓ move · enter insert · esc close").font(.kit(size: 10)).foregroundStyle(theme.muted)
                .padding(.horizontal, 10).frame(height: 26)
        }.font(.kit(size: 12)).foregroundStyle(theme.text).padding(.vertical, 4)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background(theme.surface, in: RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.border))
    }
}

private struct ComposerCommandList: View {
    @Environment(\.mica) private var theme
    @Bindable var state: ComposerCommandState
    let select: (String) -> Void
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(state.visibleMatches) { command in
                Button { select(command.name) } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 6) {
                            Text("/" + command.name).font(.kit(size: 13, weight: .medium))
                                .lineLimit(1)
                            Text("[args]").font(.kit(size: 13)).foregroundStyle(theme.muted)
                                .fixedSize()
                        }
                        Text(command.description).font(.kit(size: 12)).foregroundStyle(theme.muted).lineLimit(1)
                    }.frame(maxWidth: .infinity, alignment: .leading).frame(height: 46).padding(.horizontal, 10)
                        .background(state.selected?.name == command.name ? theme.hover : Color.clear)
                        .contentShape(Rectangle())
                }.buttonStyle(.plain)
            }
            Text("↑↓ move · enter/tab insert · esc close").font(.kit(size: 10)).foregroundStyle(theme.muted)
                .padding(.horizontal, 10).frame(height: 26)
        }.foregroundStyle(theme.text).padding(.vertical, 4)
            .background(theme.surface, in: RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.border))
    }
}
