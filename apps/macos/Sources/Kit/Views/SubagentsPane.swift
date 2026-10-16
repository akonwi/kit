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
    @State private var reading = TranscriptReadingState()
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
                if let agent, let conversationID = agent.conversationID, agent.generation != nil {
                    SubagentConfigurationControls(state: state, agent: agent,
                        operation: state.subagentConfiguration(conversationID))
                } else if let agent, !agent.model.isEmpty {
                    Text(agent.configurationLabel).foregroundStyle(theme.muted).lineLimit(1)
                }
                Spacer()
                if conversation.loading || agent?.status == "running" { KitSpinner() }
                if let agent, ["failed", "aborted", "interrupted"].contains(agent.status) {
                    Text(agent.status.capitalized)
                        .foregroundStyle(agent.status == "failed" ? theme.danger : theme.muted)
                }
            }.font(.kit(size: 12)).padding(.horizontal, 16).frame(height: 36)
            Rule()
            if let conversationID = agent?.conversationID {
                SubagentConfigurationFeedback(operation: state.subagentConfiguration(conversationID))
            }
            if let error = conversation.error {
                HStack {
                    Text(error).font(.kit(size: 12)).foregroundStyle(theme.muted)
                    Spacer()
                    Button("Retry") { retry += 1 }.buttonStyle(MicaButtonStyle(compact: true))
                }.padding(16)
                Rule()
            }
            if conversation.loaded && !conversation.messages.isEmpty {
                TranscriptReadingNavigation(state: reading)
                NativeTranscript(messages: conversation.messages, hasHistory: false,
                    historyLoading: false, historyError: nil, active: conversation.activity != nil, presentation: presentation,
                    workspace: state.ui.workspace, resumeRequest: resumeRequest,
                    latestOutOfView: $latestOutOfView, loadHistory: {}, theme: theme, attachmentClient: state.attachmentClient, attachmentSession: state.selectedID, pane: .agent(name), reading: reading)
                    .overlay(alignment: .bottomTrailing) {
                        if latestOutOfView {
                            Button("Latest", icon: .arrowDown) { resumeRequest += 1 }
                                .buttonStyle(MicaButtonStyle(compact: true)).padding(16)
                        }
                    }
            } else {
                Text(emptyMessage).font(.kit(size: 13)).foregroundStyle(theme.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            // This pane's activity card, queued tray, and composer address the
            // subagent's own conversation. Interactions belong to the parent
            // session and appear in the Agent pane.
            VStack(alignment: .leading, spacing: 8) {
                PendingActivityView(activity: conversation.activity)
                if let count = agent?.queuedTasks, count > 0 {
                    Text("\(count) queued \(count == 1 ? "task" : "tasks")")
                        .font(.kit(size: 12)).foregroundStyle(theme.muted)
                        .padding(.horizontal, 4)
                        .accessibilityLabel("\(count) queued \(count == 1 ? "task" : "tasks") for \(name)")
                }
                if SessionStore.subagentMessagingEnabled {
                    if state.isTemporary {
                        Text("Subagents require a persistent session.")
                            .font(.kit(size: 12)).foregroundStyle(theme.muted)
                    } else {
                        SubagentComposer(operation: state.subagentSend(name), pane: .agent(name), isNew: conversationID == nil,
                            enabled: state.canSendSubagent && agent != nil, active: active,
                            focused: $focused, focusRequest: focusRequest) {
                                Task { await state.sendToSubagent(name, conversation: conversationID) }
                            }
                    }
                }
            }
            .padding([.horizontal, .bottom], 14)
            .padding(.top, 8)
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

/// Pane-owned model and thinking controls for one durable child conversation:
/// the composer's split chip, opening its popovers below. They never read or
/// change the attached session's configuration.
struct SubagentConfigurationControls: View {
    @Environment(\.mica) private var theme
    @Bindable var state: SessionStore
    let agent: SubagentRoster.Item
    let operation: SubagentConfigurationOperation
    @State private var popover = AnchoredPopover()
    @State private var openMenu: ComposerChipMenu?
    @State private var lastClosedMenu: ComposerChipMenu?
    @State private var modelAnchor = ComposerPickerAnchor()
    @State private var thinkingAnchor = ComposerPickerAnchor()

    private var models: [WireModelCapability] { state.configuration.models }
    private var thinkingLevels: [String] {
        let levels = ComposerView.thinkingLevels(model: agent.model, models: models)
        return levels.isEmpty ? (agent.thinkingLevel.map { [$0] } ?? []) : levels
    }
    var enabled: Bool { state.canConfigureSubagent && !operation.pending }
    /// The model can't change while the subagent has active or queued work.
    var modelEnabled: Bool { enabled && !agent.hasOutstandingWork }
    static func lockedNote(_ name: String) -> String { "Model can change once \(name)'s work finishes" }

    var body: some View {
        HStack(spacing: 8) {
            ComposerConfigurationChip(
                model: ComposerView.activeModelLabel(id: agent.model, models: models),
                modelHelp: models.first(where: { $0.id == agent.model }).map(ComposerView.modelLabel) ?? agent.model,
                thinking: agent.thinkingLevel.flatMap { $0.isEmpty ? nil : $0 },
                open: openMenu, modelAnchor: modelAnchor, thinkingAnchor: thinkingAnchor,
                toggle: toggle, subject: agent.name)
                .disabled(!enabled)
            if operation.pending { KitSpinner().accessibilityLabel("Applying \(agent.name) configuration") }
        }
        .onChange(of: enabled) { _, enabled in if !enabled { popover.dismiss() } }
        .onDisappear { popover.dismiss() }
        .task(id: state.selectedID) {
            if models.isEmpty { await state.loadComposer() }
        }
    }

    private func toggle(_ menu: ComposerChipMenu) {
        if popover.isOpen {
            let same = openMenu == menu
            popover.dismiss()
            if same { return }
        } else if lastClosedMenu == menu, let closedAt = popover.closedAt,
                  Date().timeIntervalSince(closedAt) < 0.3 {
            // The click that reached this half already closed its popover.
            return
        }
        let anchor = menu == .model ? modelAnchor : thinkingAnchor
        guard let frame = anchor.screenFrame, let window = anchor.view?.window else { return }
        let popover = popover, theme = theme, name = agent.name
        let content: AnyView = switch menu {
        case .model:
            AnyView(SubagentModelMenu(state: state, name: name) { popover.dismiss() }.popoverCard(theme))
        case .thinking:
            AnyView(ComposerThinkingMenu(theme: theme, levels: thinkingLevels,
                selected: agent.thinkingLevel ?? "",
                select: { [state] level in Task { await state.configureSubagent(name, thinking: level) } },
                dismiss: { popover.dismiss() }))
        }
        openMenu = menu
        popover.show(anchor: frame, parent: window, edge: .below, content: content) {
            lastClosedMenu = menu
            if openMenu == menu { openMenu = nil }
        }
    }
}

/// The model half's popover for a subagent. It reads the roster live, so the
/// list locks when the subagent picks up work while the popover is open.
private struct SubagentModelMenu: View {
    @Bindable var state: SessionStore
    let name: String
    let dismiss: () -> Void

    private var agent: SubagentRoster.Item? { state.selected?.subagents?.items.first { $0.name == name } }

    var body: some View {
        ComposerModelPicker(models: state.configuration.models, selectedID: agent?.model ?? "",
            select: { id in
                dismiss()
                Task { await state.configureSubagent(name, model: id) }
            }, reload: {
                Task { await state.loadComposer() }
            }, dismiss: dismiss,
            title: "Model for \(name)",
            lockedNote: agent?.hasOutstandingWork == false ? nil : SubagentConfigurationControls.lockedNote(name))
            .accessibilityLabel("Model for \(name)")
    }
}

/// Configuration errors and warnings stay with the pane that caused them.
private struct SubagentConfigurationFeedback: View {
    @Environment(\.mica) private var theme
    let operation: SubagentConfigurationOperation

    var body: some View {
        if let message = operation.error ?? operation.warning {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(message).font(.kit(size: 12))
                    .foregroundStyle(operation.error != nil ? theme.danger : theme.muted)
                    .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 0)
                Button { operation.clearFeedback() } label: { IconView(.x, size: 10) }
                    .buttonStyle(.plain).foregroundStyle(theme.muted)
                    .help("Dismiss").accessibilityLabel("Dismiss configuration message")
            }.padding(.horizontal, 16).padding(.vertical, 10)
            Rule()
        }
    }
}

private struct SubagentComposer: View {
    @Environment(\.mica) private var theme
    @Bindable var operation: SubagentSendOperation
    let pane: WorkspacePane
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
                    .workspaceFocusTarget(pane)
                Button(action: send) {
                    if operation.pending { KitSpinner() }
                    else { IconView(.arrowUp, size: 12) }
                }.buttonStyle(MicaButtonStyle(compact: true))
                    .disabled(!enabled || operation.pending || operation.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    .help(isNew ? "Start task" : "Send message")
                    .accessibilityLabel(isNew ? "Start task" : "Send message")
            }.padding(.horizontal, 12).padding(.vertical, 10)
                .background(theme.raised, in: RoundedRectangle(cornerRadius: 4))
                .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(focused ? theme.focusBorder : theme.border))
        }
    }
}
