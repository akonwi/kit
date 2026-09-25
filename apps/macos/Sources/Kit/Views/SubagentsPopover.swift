import SwiftUI

struct SubagentsPopover: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    @State private var dismissalTarget: SubagentRoster.Item?
    @State private var dismissalSession = ""
    private var contentHeight: CGFloat {
        let rows = state.selected?.subagents?.items.count ?? 0
        let diagnostics = state.selected?.subagents?.diagnostics.count ?? 0
        return min(420, CGFloat(max(1, rows) * 64 + diagnostics * 56 + 12))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Subagents").font(.kit(size: 13, weight: .semibold)).padding(16)
            Rule()
            if state.subagentDismissal.pending {
                KitSpinner("Dismissing conversation…").padding(12)
            }
            if let error = state.subagentDismissal.error {
                Text(error).font(.kit(size: 12)).foregroundStyle(theme.danger).padding(12)
            }
            if let roster = state.selected?.subagents {
                ScrollView {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(roster.items) { agent in row(agent) }
                        if roster.items.isEmpty {
                            Text("No subagents available")
                                .font(.kit(size: 12)).foregroundStyle(theme.muted).padding(12)
                        }
                        ForEach(Array(roster.diagnostics.enumerated()), id: \.offset) { _, message in
                            Text(message).font(.kit(size: 12)).foregroundStyle(theme.muted).padding(12)
                        }
                    }.padding(6)
                }.frame(height: contentHeight)
            } else if state.connectionState == .loading {
                KitSpinner("Loading subagents…").padding(24).frame(maxWidth: .infinity)
            } else {
                VStack(alignment: .leading, spacing: 12) {
                    Text("Subagents unavailable")
                    Text(state.connectionStatus).foregroundStyle(theme.muted)
                    Button("Retry") { state.attach() }.buttonStyle(MicaButtonStyle(compact: true))
                }.font(.kit(size: 12)).padding(16)
            }
        }.frame(width: 360).foregroundStyle(theme.text).background(theme.surface)
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

    private func row(_ agent: SubagentRoster.Item) -> some View {
        SubagentPopoverRow(agent: agent) {
            state.ui.subagentsPresented = false
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

private struct SubagentPopoverRow: View {
    @Environment(\.mica) private var theme
    let agent: SubagentRoster.Item
    let open: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: open) {
            VStack(alignment: .leading, spacing: 5) {
                HStack(spacing: 8) {
                    Text(agent.name).fontWeight(.medium).lineLimit(1)
                    Spacer(minLength: 8)
                    if agent.status == "running" { KitSpinner() }
                    Text(agent.status.capitalized).font(.kit(size: 11)).foregroundStyle(agent.status == "failed" ? theme.danger : theme.muted)
                }
                Text(agent.conversationID == nil ? agent.description : agent.configurationLabel)
                    .font(.kit(size: 11)).foregroundStyle(theme.muted).lineLimit(1)
            }.font(.kit(size: 12)).padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(hovering ? theme.raised : theme.surface, in: RoundedRectangle(cornerRadius: 6))
                .contentShape(Rectangle())
        }.buttonStyle(.plain).onHover { hovering = $0 }
            .help(agent.description).accessibilityHint("Open subagent conversation")
    }
}
