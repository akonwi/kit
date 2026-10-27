import SwiftUI

/// The session's subagents, shown inside the command palette as the TUI's
/// `/subagents` picker shows them. Return opens a started agent's
/// conversation; ⌘⌫ asks before dismissing it.
struct SubagentsPanel: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    @State private var picker = SubagentPickerState()
    @State private var query = ""

    var body: some View {
        Group {
            if let roster = state.selected?.subagents {
                list(roster)
            } else if state.connectionState == .loading {
                PaletteSheet(title: "Subagents", height: PaletteMetrics.height) {
                    KitSpinner("Loading subagents…").padding(24).frame(maxWidth: .infinity)
                }
            } else {
                PaletteSheet(title: "Subagents", height: PaletteMetrics.height) {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("Subagents unavailable")
                        Text(state.connectionStatus).foregroundStyle(theme.muted)
                        Button("Retry") { state.attach() }.buttonStyle(MicaButtonStyle(compact: true))
                    }.font(.kit(size: 12)).padding(16)
                }
            }
        }
        .onAppear { state.subagentDismissal.clearError() }
        .onExitCommand { state.ui.palette = false }
        .alert(Text(picker.confirmation.map { "Dismiss “\($0.agent.name)”?" } ?? ""), isPresented: Binding(
            get: { picker.confirmation != nil }, set: { if !$0 { picker.cancelDismissal() } }
        ), presenting: picker.confirmation) { confirmation in
            Button("Dismiss", role: .destructive) {
                Task { await picker.confirmDismissal(confirmation, store: state) }
            }
            Button("Cancel", role: .cancel) { picker.cancelDismissal() }
        } message: { _ in
            Text("Active and queued work will be aborted. This cannot be undone.")
        }
    }

    private func list(_ roster: SubagentRoster) -> some View {
        let rows = SubagentPicker.rows(roster, sessionModel: state.model, sessionThinking: state.thinking,
                                       models: state.configuration.models)
        return PaletteListScreen(
            query: $query, placeholder: SubagentPicker.placeholder, catalog: rows, wholeQuery: true,
            emptyMessage: { $0.isEmpty ? SubagentPicker.empty : SubagentPicker.noMatch($0) },
            selectionChanged: { picker.selectedID = $0?.id },
            problem: state.subagentDismissal.error, refusal: picker.refusal,
            accessory: AnyView(SubagentPickerFooter(state: state, picker: picker, diagnostics: roster.diagnostics)),
            rowMenu: { row in AnyView(rowMenu(row)) },
            activate: { picker.activate($0, store: state) },
            dismiss: { state.ui.palette = false })
    }

    @ViewBuilder private func rowMenu(_ row: PaletteCommand) -> some View {
        if picker.agent(row.id, in: state)?.canDismiss == true {
            Button("Dismiss…", role: .destructive) { picker.requestDismissal(row.id, store: state) }
                .disabled(!state.canDismissSubagent)
        }
    }
}

/// The picker's definition warnings, then its action row: Dismiss… for
/// the selected agent.
private struct SubagentPickerFooter: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    let picker: SubagentPickerState
    let diagnostics: [String]
    @State private var warningsHeight: CGFloat = 0
    /// The tallest the warnings grow before they scroll.
    static let warningsLimit: CGFloat = 76

    var body: some View {
        let enabled = picker.canDismiss(picker.selectedID, in: state)
        let pending = state.subagentDismissal.pending
        VStack(spacing: 0) {
            if !diagnostics.isEmpty {
                Rule()
                ScrollView {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(Array(diagnostics.enumerated()), id: \.offset) { _, message in
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                IconView(.warning, size: 12)
                                Text(message).fixedSize(horizontal: false, vertical: true)
                                Spacer(minLength: 0)
                            }
                            .accessibilityElement(children: .combine)
                        }
                    }
                    .font(.kit(size: 12)).foregroundStyle(theme.warning)
                    .padding(.horizontal, 16).padding(.vertical, 8)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { warningsHeight = $0 }
                }
                .scrollBounceBehavior(.basedOnSize)
                .frame(height: min(warningsHeight, Self.warningsLimit))
            }
            Rule()
            HStack(spacing: 8) {
                Button { picker.requestDismissal(store: state) } label: {
                    HStack(spacing: 8) {
                        Text(pending ? "Dismissing…" : "Dismiss…")
                        Text("⌘⌫").foregroundStyle(enabled ? theme.muted : theme.strongBorder)
                    }
                    .foregroundStyle(enabled ? theme.text : theme.strongBorder)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .keyboardShortcut(KeyEquivalent("\u{7f}"), modifiers: .command)
                .disabled(!enabled)
                .accessibilityLabel(picker.selectedID.map { "Dismiss \($0)" } ?? "Dismiss")
                // SwiftUI can keep a shortcut's action from an earlier render,
                // so each selected agent gets its own button.
                .id(picker.selectedID)
                if pending { KitSpinner() }
                Spacer()
            }
            .font(.kit(size: 12))
            .padding(.horizontal, 16)
            .frame(height: PaletteMetrics.footerHeight)
        }
    }
}
