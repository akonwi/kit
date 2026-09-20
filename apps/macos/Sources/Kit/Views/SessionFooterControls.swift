import SwiftUI

/// Workspace actions belong to the session, including while its composer is replaced.
struct SessionFooterControls: View {
    @Bindable var state: SessionStore
    @Environment(\.mica) private var theme

    var body: some View {
        @Bindable var ui = state.ui
        HStack(spacing: 4) {
            control("Diff", symbol: "chevron.left.forwardslash.chevron.right") {
                state.ui.workspace.open(.review)
            }.disabled(!(state.catalogClient is any DiffClient))
            control("Scratchpad", symbol: "note.text") {
                state.ui.workspace.open(.scratchpad)
            }.disabled(!state.canOpenScratchpad)
            control("Subagents", symbol: "person.2") {
                state.ui.subagentsPresented.toggle()
            }
            .popover(isPresented: $ui.subagentsPresented, arrowEdge: .bottom) {
                SubagentsPopover(state: state)
                    .environment(\.mica, theme)
                    .preferredColorScheme(theme.dark ? .dark : .light)
            }
            control("Open file", symbol: "doc") { state.showFiles(intent: "open") }
                .disabled(state.unavailable)
            control("Open command palette", symbol: "command", help: "Commands (⌘K)") {
                state.ui.palette = true
            }
        }
        .buttonStyle(.borderless)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Session controls")
    }

    private func control(_ title: String, symbol: String, help: String? = nil,
                         action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 14))
                .frame(width: 28, height: 28)
                .contentShape(Rectangle())
        }
        .help(help ?? title)
        .accessibilityLabel(title)
    }
}
