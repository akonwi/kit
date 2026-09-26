import AppKit
import Observation
import SwiftUI

@Observable private final class ComposerPickerTheme {
    var value: MicaTheme
    init(_ value: MicaTheme) { self.value = value }
}

private struct ComposerPickerHost: View {
    @Bindable var theme: ComposerPickerTheme
    let content: AnyView
    let footer: String

    var body: some View {
        VStack(spacing: 0) {
            ScrollView(.vertical) {
                content.frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollIndicators(.hidden)
            .frame(maxHeight: .infinity)
            Text(footer).font(.kit(size: 10)).foregroundStyle(theme.value.muted)
                .padding(.horizontal, 10).frame(maxWidth: .infinity, alignment: .leading)
                .frame(height: 26).padding(.bottom, 4)
        }
        // The picker frame is taller than its history content while loading and
        // shorter once it has enough rows to scroll. Keep the rounded surface
        // on the fixed viewport, not on the scrolling content: otherwise the
        // lower corners and border scroll out of sight.
        .background(theme.value.surface, in: RoundedRectangle(cornerRadius: 8))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.value.border))
        .environment(\.mica, theme.value)
        .preferredColorScheme(theme.value.dark ? .dark : .light)
    }
}

/// A nonactivating completion surface: all typing and key commands stay in NSTextView.
@MainActor final class ComposerMentionPanel {
    private final class Panel: NSPanel {
        override var canBecomeKey: Bool { false }
        override var canBecomeMain: Bool { false }
    }
    private var panel: NSPanel?
    private var observer: NSObjectProtocol?
    private var presentedFrame: NSRect?
    private var themeHolder: ComposerPickerTheme?
    private var historyGeometryScheduled = false
    private var historyGeometryGeneration = 0

    func dismiss() {
        if let observer { NotificationCenter.default.removeObserver(observer); self.observer = nil }
        if let panel {
            historyGeometryGeneration &+= 1
            historyGeometryScheduled = false
            panel.parent?.removeChildWindow(panel); panel.close()
        }
        panel = nil
        presentedFrame = nil
        themeHolder = nil
    }
    func show(editor: ComposerTextView, state: ComposerMentionState, theme: MicaTheme,
              select: @escaping (String) -> Void, retry: @escaping () -> Void) {
        present(editor: editor, open: state.isOpen,
                height: CGFloat(max(1, state.visibleMatches.count) * 30 + 34 + (state.error == nil ? 0 : 34) + (state.indexNotice == nil ? 0 : 26)),
                label: "File mentions", theme: theme, footer: "↑↓ move · enter insert · esc close",
                close: { [weak state] in state?.close() },
                content: AnyView(ComposerMentionList(state: state, select: select, retry: retry)))
    }

    func showHistory(editor: ComposerTextView, state: ComposerHistoryState, theme: MicaTheme,
                     select: @escaping (ComposerHistoryState.Entry) -> Void, retry: @escaping () -> Void) {
        let generation = historyGeometryGeneration
        present(editor: editor, open: state.isOpen,
                height: Self.historyHeight(rows: state.visibleMatches.count, hasError: state.error != nil),
                label: state.mode == .bash ? "Shell history" : "Message history", theme: theme,
                footer: "↑↓ move · enter insert · esc close",
                close: { [weak state] in state?.close() },
                content: AnyView(ComposerHistoryList(state: state, select: select, retry: retry,
                    geometryChanged: { [weak self, weak editor] in
                        guard let self, self.historyGeometryGeneration == generation else { return }
                        self.scheduleHistoryGeometryUpdate(editor: editor)
                    })))
    }

    static func historyHeight(rows: Int, hasError: Bool) -> CGFloat {
        // Header 30 + rows/empty state 42 each + top inset 4 + fixed footer 30.
        CGFloat(min(6, max(1, rows)) * 42 + 64 + (hasError ? 30 : 0))
    }

    private func scheduleHistoryGeometryUpdate(editor: ComposerTextView?) {
        guard panel != nil, !historyGeometryScheduled else { return }
        historyGeometryScheduled = true
        let generation = historyGeometryGeneration
        // SwiftUI can report new rows during an AppKit display cycle. Defer and
        // coalesce geometry requests; present() keeps the hosting root stable
        // and skips setFrame when the requested frame has not changed.
        DispatchQueue.main.async { [weak self, weak editor] in
            guard let self, self.historyGeometryGeneration == generation else { return }
            self.historyGeometryScheduled = false
            guard self.panel != nil else { return }
            editor?.mentionChanged?()
        }
    }

    private func present(editor: ComposerTextView, open: Bool, height: CGFloat,
                         label: String, theme: MicaTheme, footer: String,
                         close: @escaping @MainActor @Sendable () -> Void, content: AnyView) {
        guard open, editor.isEditable, let window = editor.window, window.isKeyWindow,
              window.firstResponder === editor else { dismiss(); return }

        let panel: NSPanel
        let created: Bool
        if let existing = self.panel { panel = existing; created = false }
        else {
            created = true
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
        // The hosting root observes picker state directly. Replacing it during
        // NSTextView or SwiftUI layout re-enters AppKit's constraint cycle and
        // raises NSInternalInconsistencyException on current macOS releases.
        if created {
            let themeHolder = ComposerPickerTheme(theme)
            self.themeHolder = themeHolder
            let hosting = NSHostingView(rootView: ComposerPickerHost(theme: themeHolder, content: content, footer: footer))
            // The panel owns the fixed picker geometry. Do not let the hosting
            // view negotiate intrinsic size back into AppKit while SwiftUI is
            // laying out scrollable history content.
            hosting.sizingOptions = []
            panel.contentView = hosting
        } else if themeHolder?.value != theme {
            // Keep the hosting root stable, but allow an open picker to follow
            // live theme and system-appearance changes.
            themeHolder?.value = theme
        }
        // Compare against the requested frame rather than panel.frame. AppKit
        // may normalize a child panel's actual frame by subpixels; comparing
        // that normalized value on every editor layout can repeatedly call
        // setFrame and feed another SwiftUI/AppKit layout pass.
        if presentedFrame != frame {
            presentedFrame = frame
            panel.setFrame(frame, display: false)
            // The new viewport may expose rows after SwiftUI's prior display
            // transaction. Request paint on the next cycle, not synchronously.
            panel.contentView?.needsDisplay = true
        }
        if created || !panel.isVisible { panel.orderFront(nil) }
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
        }.font(.kit(size: 12)).foregroundStyle(theme.text).padding(.top, 4)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

private struct ComposerHistoryList: View {
    @Environment(\.mica) private var theme
    @Bindable var state: ComposerHistoryState
    let select: (ComposerHistoryState.Entry) -> Void
    let retry: () -> Void
    let geometryChanged: () -> Void
    var body: some View {
        let matches = state.visibleMatches
        let selectedID = state.selected?.id
        return VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                Text(state.query.isEmpty ? (state.mode == .bash ? "Shell history" : "Message history") : state.query)
                    .lineLimit(1)
                Spacer(minLength: 0)
            }.foregroundStyle(theme.muted).padding(.horizontal, 10).frame(height: 30)
            if matches.isEmpty {
                Text(state.loading ? "Loading…" : state.error ?? "No history")
                    .foregroundStyle(theme.muted).lineLimit(1).frame(height: 42).padding(.horizontal, 10)
            }
            ForEach(matches) { entry in
                Button { select(entry) } label: {
                    HStack(spacing: 8) {
                        if case .bash(let bash) = entry {
                            Text(bash.excluded ? "!!" : "!").font(.kit(size: 12, weight: .medium)).foregroundStyle(theme.accent)
                        }
                        Text(entry.text.replacingOccurrences(of: "\n", with: " "))
                            .lineLimit(1).truncationMode(.tail)
                        Spacer(minLength: 8)
                        if case .bash(let bash) = entry {
                            Text(bash.excluded ? "excluded" : "included").foregroundStyle(theme.muted).fixedSize()
                        }
                    }.padding(.horizontal, 10).frame(height: 42)
                        .background(selectedID == entry.id ? theme.hover : Color.clear)
                        .contentShape(Rectangle())
                }.buttonStyle(.plain).accessibilityAddTraits(selectedID == entry.id ? .isSelected : [])
            }
            if state.error != nil { Button("Retry", action: retry).buttonStyle(.plain).padding(.horizontal, 10).frame(height: 30) }
        }.font(.kit(size: 12)).foregroundStyle(theme.text).padding(.top, 4)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .onAppear(perform: geometryChanged)
            .onChange(of: matches.count) { _, _ in geometryChanged() }
            .onChange(of: state.error != nil) { _, _ in geometryChanged() }
    }
}
