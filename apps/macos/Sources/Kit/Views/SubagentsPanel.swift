import SwiftUI

/// The session's configured subagents, shown inside the command palette.
/// Choosing one opens its conversation pane.
struct SubagentsPanel: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    @State private var dismissalTarget: SubagentRoster.Item?
    @State private var dismissalSession = ""
    var body: some View {
        let roster = state.selected?.subagents
        PaletteSheet(title: "Subagents", detail: roster.map { "\($0.items.count) configured" },
                     height: PaletteMetrics.height) {
            VStack(alignment: .leading, spacing: 0) {
                if state.subagentDismissal.pending {
                    KitSpinner("Dismissing conversation…").padding(12)
                }
                if let error = state.subagentDismissal.error {
                    Text(error).font(.kit(size: 12)).foregroundStyle(theme.danger).padding(12)
                }
                if let roster {
                    ScrollView {
                        VStack(alignment: .leading, spacing: 0) {
                            let columns = PaletteColumns(roster.items.map { PaletteCommand(id: $0.name, name: $0.name, description: "") })
                            ForEach(roster.items) { agent in row(agent, columns: columns) }
                            if roster.items.isEmpty {
                                Text("No subagents available")
                                    .font(.kit(size: 13)).foregroundStyle(theme.muted)
                                    .frame(maxWidth: .infinity).padding(.vertical, 72)
                            }
                            ForEach(Array(roster.diagnostics.enumerated()), id: \.offset) { _, message in
                                Text(message).font(.kit(size: 12)).foregroundStyle(theme.warning)
                                    .fixedSize(horizontal: false, vertical: true).padding(.horizontal, 14).padding(.vertical, 8)
                            }
                        }.padding(PaletteMetrics.listPadding)
                    }
                } else if state.connectionState == .loading {
                    KitSpinner("Loading subagents…").padding(24).frame(maxWidth: .infinity)
                } else {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("Subagents unavailable")
                        Text(state.connectionStatus).foregroundStyle(theme.muted)
                        Button("Retry") { state.attach() }.buttonStyle(MicaButtonStyle(compact: true))
                    }.font(.kit(size: 12)).padding(16)
                }
            }
        }
            .onExitCommand { state.ui.palette = false }
            .alert("Dismiss this conversation and stop its outstanding work?", isPresented: Binding(
                get: { dismissalTarget != nil }, set: { if !$0 { dismissalTarget = nil } }
            ), presenting: dismissalTarget) { agent in
                Button("Dismiss conversation", role: .destructive) {
                    let session = dismissalSession
                    Task { await state.dismissSubagent(agent, session: session) }
                }
                Button("Cancel", role: .cancel) { }
            } message: { agent in
                Text("This will dismiss \(agent.name) and stop its running or queued tasks.")
            }
    }

    private func row(_ agent: SubagentRoster.Item, columns: PaletteColumns) -> some View {
        SubagentPanelRow(agent: agent, columns: columns) {
            state.ui.palette = false
            state.ui.workspace.open(.agent(agent.name))
        }.contextMenu {
            if agent.canDismiss {
                Button("Dismiss conversation", role: .destructive) {
                    let session = state.selectedID
                    if agent.hasOutstandingWork {
                        dismissalSession = session
                        dismissalTarget = agent
                    } else {
                        Task { await state.dismissSubagent(agent, session: session) }
                    }
                }.disabled(!state.canDismissSubagent)
            }
        }
    }
}

private struct SubagentPanelRow: View {
    let agent: SubagentRoster.Item
    let columns: PaletteColumns
    let open: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: open) {
            PaletteRow(row: PaletteCommand(id: agent.name, name: agent.name,
                                           description: agent.conversationID == nil ? agent.description : agent.configurationLabel,
                                           meta: agent.status),
                       columns: columns, selected: false, hovered: hovering)
        }
            .buttonStyle(.plain)
            .onHover { hovering = $0 }
            .help(agent.description)
            .accessibilityHint("Open subagent conversation")
    }
}
