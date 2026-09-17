import SwiftUI

struct AgentPane: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    let name: String
    @State private var conversation = SubagentConversationState()
    @State private var focused = false
    @State private var focusRequest = 0
    @State private var presentation = TranscriptPresentationState()
    @State private var latestOutOfView = false
    @State private var resumeRequest = 0
    @State private var retry = 0

    private var agent: SubagentRoster.Item? { state.selected?.subagents?.items.first { $0.name == name } }
    private var conversationID: String? { agent?.conversationID ?? state.subagentSend(name).conversationID }
    private var active: Bool { state.ui.workspace.isVisible(.agent(name)) }
    private var request: Request {
        Request(identity: SessionIdentity(server: state.serverID, session: state.selectedID),
                conversation: conversationID, active: active, retry: retry)
    }
    private struct Request: Equatable {
        let identity: SessionIdentity
        let conversation: String?
        let active: Bool
        let retry: Int
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                if let agent, !agent.model.isEmpty {
                    Text(agent.configurationLabel).foregroundStyle(theme.muted).lineLimit(1)
                }
                Spacer()
                if conversation.loading || agent?.status == "running" { KitSpinner() }
                if let count = agent?.queuedTasks, count > 0 {
                    Text("\(count) queued").foregroundStyle(theme.muted)
                }
                if let agent, ["failed", "aborted", "interrupted"].contains(agent.status) {
                    Text(agent.status.capitalized)
                        .foregroundStyle(agent.status == "failed" ? theme.danger : theme.muted)
                }
            }.font(.kit(size: 12)).padding(16)
            Rule()
            if let error = conversation.error {
                HStack {
                    Text(error).font(.kit(size: 12)).foregroundStyle(theme.muted)
                    Spacer()
                    Button("Retry") { retry += 1 }.buttonStyle(MicaButtonStyle(compact: true))
                }.padding(16)
                Rule()
            }
            if conversation.loaded && !conversation.messages.isEmpty {
                NativeTranscript(messages: conversation.messages, hasHistory: false,
                    historyLoading: false, historyError: nil, active: conversation.activity != nil, presentation: presentation,
                    workspace: state.ui.workspace, resumeRequest: resumeRequest,
                    latestOutOfView: $latestOutOfView, loadHistory: {}, theme: theme, attachmentClient: state.attachmentClient, attachmentSession: state.selectedID)
                    .overlay(alignment: .bottomTrailing) {
                        if latestOutOfView {
                            Button("Latest", systemImage: "arrow.down") { resumeRequest += 1 }
                                .buttonStyle(MicaButtonStyle(compact: true)).padding(16)
                        }
                    }
            } else {
                Text(emptyMessage).font(.kit(size: 13)).foregroundStyle(theme.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            if conversation.activity != nil {
                PendingActivityView(activity: conversation.activity).padding(.horizontal, 16).padding(.vertical, 8)
            }
            if SessionStore.subagentMessagingEnabled {
                Rule()
                if state.isTemporary {
                    Text("Subagents require a persistent session.")
                        .font(.kit(size: 12)).foregroundStyle(theme.muted).padding(16)
                } else {
                    SubagentComposer(operation: state.subagentSend(name), isNew: conversationID == nil,
                        enabled: state.canSendSubagent && agent != nil, active: active,
                        focused: $focused, focusRequest: focusRequest) {
                            Task { await state.sendToSubagent(name, conversation: conversationID) }
                        }.padding(16)
                }
            }
        }
        .onChange(of: active, initial: true) { _, active in
            if active && SessionStore.subagentMessagingEnabled { focusRequest += 1 }
        }
        .task(id: request) {
            let request = request
            guard request.active, let id = request.conversation, let client = state.subagentClient else { return }
            await conversation.watch(client: client, session: request.identity.session, conversation: id)
        }
    }

    private var emptyMessage: String {
        if conversation.loading { return "Loading conversation…" }
        if conversation.error != nil { return "" }
        if conversation.loaded { return "No messages yet" }
        if state.selected?.subagents == nil { return "Subagent information unavailable" }
        if agent == nil { return "This subagent is no longer available." }
        return "This subagent has not started a conversation."
    }
}

private struct SubagentComposer: View {
    @Environment(\.mica) private var theme
    @Bindable var operation: SubagentSendOperation
    let isNew: Bool
    let enabled: Bool
    let active: Bool
    @Binding var focused: Bool
    let focusRequest: Int
    let submit: () -> Void

    private func send() {
        guard enabled, !operation.pending else { return }
        submit()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let error = operation.error {
                Text(error).font(.kit(size: 12)).foregroundStyle(theme.danger)
            }
            if let warning = operation.warning, !warning.isEmpty {
                Text(warning).font(.kit(size: 12)).foregroundStyle(theme.muted)
            }
            HStack(alignment: .bottom, spacing: 12) {
                GrowingComposerEditor(text: $operation.draft, focused: $focused,
                    focusRequest: focusRequest, foreground: theme.text, placeholderColor: theme.muted,
                    attachmentDrop: { _ in false }, attachmentDropTargeted: { _ in }, submit: send,
                    shellEnabled: false, placeholder: isNew ? "Describe the task…" : "Message subagent…",
                    focusEnabled: active, accessibilityLabel: "Subagent message", theme: theme)
                    .disabled(operation.pending)
                Button(action: send) {
                    if operation.pending { KitSpinner() }
                    else { Image(systemName: "arrow.up") }
                }.buttonStyle(MicaButtonStyle(compact: true))
                    .disabled(!enabled || operation.pending || operation.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    .help(isNew ? "Start task" : "Send message")
                    .accessibilityLabel(isNew ? "Start task" : "Send message")
            }.padding(12)
                .background(RoundedRectangle(cornerRadius: 10).stroke(focused ? theme.accent : theme.border, lineWidth: 1))
        }
    }
}
