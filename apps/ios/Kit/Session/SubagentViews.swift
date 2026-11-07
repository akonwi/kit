import KitClient
import SwiftUI

extension EnvironmentValues {
    /// The subagents a Start agent row's chip can open, and how.
    @Entry var agentLinks: AgentLinks?
    /// A line above each user message, set where those messages aren't the
    /// reader's own, such as a subagent's tasks.
    @Entry var userMessageCaption: String?
}

/// Opens a subagent's transcript from a tool row that names it.
struct AgentLinks {
    let names: Set<String>
    let open: (String) -> Void
}

// MARK: Strip

/// The agents under the session's title: Main pinned at the left, then each
/// started subagent. Tapping one shows its transcript in place.
struct AgentStrip: View {
    let focus: SubagentFocus
    /// Whether the main agent waits on the reader, such as for an answer.
    let mainNeedsYou: Bool
    @State private var overflow = Overflow()

    /// Which ends of the scrolling agents have more beyond them.
    private struct Overflow: Equatable {
        var leading = false
        var trailing = false
    }

    var body: some View {
        HStack(spacing: 8) {
            AgentChip(label: "Main", status: nil, selected: focus.focus == nil, needsYou: mainNeedsYou) {
                focus.show(nil)
            }
            ScrollViewReader { proxy in
                ScrollView(.horizontal) {
                    HStack(spacing: 8) {
                        ForEach(focus.strip.agents) { agent in
                            AgentChip(label: agent.name, status: AgentChip.Status(agent),
                                      selected: focus.focus == agent.name) { focus.show(agent.name) }
                                .id(agent.name)
                        }
                    }
                    .padding(.trailing, 16)
                }
                .scrollIndicators(.hidden)
                .onScrollGeometryChange(for: Overflow.self) { geometry in
                    Overflow(leading: geometry.contentOffset.x > 1,
                             trailing: geometry.contentOffset.x + geometry.containerSize.width < geometry.contentSize.width - 1)
                } action: { overflow = $1 }
                // The end with more agents beyond it fades out.
                .mask {
                    HStack(spacing: 0) {
                        LinearGradient(colors: [.clear, .black], startPoint: .leading, endPoint: .trailing)
                            .frame(width: overflow.leading ? 36 : 0)
                        Rectangle()
                        LinearGradient(colors: [.black, .clear], startPoint: .leading, endPoint: .trailing)
                            .frame(width: overflow.trailing ? 36 : 0)
                    }
                }
                // The agent in view scrolls into sight, also when the strip
                // reorders; Main brings back the start.
                .onChange(of: focus.focus) { _, name in
                    guard let target = name ?? focus.strip.agents.first?.name else { return }
                    withAnimation(.snappy) { proxy.scrollTo(target, anchor: name == nil ? .leading : .center) }
                }
                .onChange(of: focus.strip.agents.map(\.name)) {
                    guard let name = focus.focus else { return }
                    withAnimation(.snappy) { proxy.scrollTo(name, anchor: .center) }
                }
            }
        }
        .padding(.leading, 16)
        .padding(.top, 2)
        .padding(.bottom, 10)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Agents")
        .accessibilityIdentifier("agent-strip")
    }
}

/// One agent's button in the strip: filled when its transcript is in view.
private struct AgentChip: View {
    enum Status {
        case working, queued, failed, interrupted, stopped, done

        init(_ agent: SubagentRoster.Item) {
            switch agent.status {
            case "running": self = .working
            case "failed": self = .failed
            case "interrupted": self = .interrupted
            case "aborted": self = .stopped
            default: self = agent.hasOutstandingWork ? .queued : .done
            }
        }

        var description: String {
            switch self {
            case .working: "working"
            case .queued: "queued"
            case .failed: "failed"
            case .interrupted: "interrupted"
            case .stopped: "stopped"
            case .done: "done"
            }
        }
    }

    let label: String
    let status: Status?
    let selected: Bool
    var needsYou = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                // The label says the status; a checkmark would read as selected.
                if let status { icon(status).frame(width: 14, height: 14).accessibilityHidden(true) }
                Text(label)
                    .font(.system(size: 15, weight: selected ? .semibold : .regular))
                    .foregroundStyle(selected ? Theme.surface : Theme.text)
                    .lineLimit(1)
                if needsYou {
                    Circle().fill(Theme.accent).frame(width: 8, height: 8)
                }
            }
            .padding(.leading, status == nil ? 14 : 11)
            .padding(.trailing, 13)
            .frame(height: 32)
            .background(selected ? Theme.text : Theme.hover, in: Capsule())
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(accessibilityLabel)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityIdentifier("agent-\(label)")
    }

    @ViewBuilder
    private func icon(_ status: Status) -> some View {
        switch status {
        case .working: LaneSpinner()
        case .queued: symbol("clock", Theme.muted)
        case .failed: symbol("exclamationmark.circle", Theme.danger)
        case .interrupted: symbol("exclamationmark.circle", Theme.warning)
        case .stopped: symbol("stop.circle", Theme.muted)
        case .done: symbol("checkmark.circle", Theme.success)
        }
    }

    private func symbol(_ name: String, _ color: Color) -> some View {
        Image(systemName: name)
            .font(.system(size: 13, weight: .medium))
            .foregroundStyle(selected ? Theme.surface : color)
    }

    private var accessibilityLabel: String {
        var parts = [label]
        if let status { parts.append(status.description) }
        if needsYou { parts.append("needs you") }
        return parts.joined(separator: ", ")
    }
}

// MARK: Transcript

/// A subagent's transcript, watched while it's in view. Its user messages are
/// the tasks the main agent gave it.
struct SubagentTranscript: View {
    let agent: SubagentRoster.Item
    let conversation: SubagentConversationState
    let focus: SubagentFocus
    let visible: Bool
    @State private var retry = 0

    private struct Request: Equatable {
        let conversation: String?
        let visible: Bool
        let retry: Int
    }

    var body: some View {
        Group {
            if conversation.loaded && !conversation.messages.isEmpty {
                TranscriptList(messages: conversation.messages,
                               active: agent.status == "running" || conversation.activity != nil) { EmptyView() }
                    .environment(\.userMessageCaption, "Task from the main agent")
            } else {
                placeholder
            }
        }
        .task(id: Request(conversation: agent.conversationID, visible: visible, retry: retry)) {
            guard visible, let id = agent.conversationID else { return }
            await conversation.watch(client: focus.client, session: focus.session, conversation: id)
        }
    }

    @ViewBuilder
    private var placeholder: some View {
        VStack(spacing: 12) {
            if let error = conversation.error {
                Text(error)
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.muted)
                    .multilineTextAlignment(.center)
                if !conversation.unavailable {
                    Button("Try again") { retry += 1 }
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(Theme.accent)
                }
            } else if conversation.loaded {
                Text("No messages yet")
                    .font(.system(size: 15))
                    .foregroundStyle(Theme.muted)
            } else {
                HStack(spacing: 8) {
                    LaneSpinner().frame(width: 16, height: 16)
                    Text("Loading conversation…")
                        .font(.system(size: 15))
                        .foregroundStyle(Theme.muted)
                }
            }
        }
        .padding(.horizontal, 32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: Agent bar

/// What takes the composer's place while a subagent is in view: who it is,
/// how it's configured, and a menu to change or dismiss it. The reader
/// doesn't message a subagent; its results go to the main agent.
struct AgentBar: View {
    let agent: SubagentRoster.Item
    let focus: SubagentFocus
    /// The session's actions, for the model catalog.
    let actions: SessionActions
    let conversation: SubagentConversationState?
    @State private var confirmingDismiss = false

    var body: some View {
        VStack(spacing: 8) {
            statusLine.padding(.horizontal, 8)
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 0) {
                    Text(Self.label(agent, models: actions.configuration.models))
                        .font(.system(size: 15))
                        .foregroundStyle(Theme.text)
                        .lineLimit(1)
                    Text("Its results go to Main")
                        .font(.system(size: 13))
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("agent-bar")
                menu
            }
            .padding(.leading, 20)
            .padding(.trailing, 8)
            .frame(height: 52)
            .glassEffect(.regular, in: Capsule())
        }
        .padding(.horizontal, 12)
        .padding(.bottom, 4)
        .alert("Dismiss “\(agent.name)”?", isPresented: $confirmingDismiss) {
            Button("Cancel", role: .cancel) {}
            Button("Dismiss", role: .destructive) { Task { await focus.dismiss(agent) } }
        } message: {
            Text("Its active and queued work will be aborted. This can’t be undone.")
        }
    }

    /// `reviewer · GPT-5.5 · High`.
    static func label(_ agent: SubagentRoster.Item, models: [WireModelCapability]) -> String {
        var parts = [agent.name]
        if !agent.model.isEmpty { parts.append(SessionActions.modelName(agent.model, models: models)) }
        if let level = agent.thinkingLevel, !level.isEmpty {
            parts.append(level == "off" ? "Thinking off" : SessionContext.thinkingName(level))
        }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder
    private var statusLine: some View {
        if let notice = focus.notice {
            NoticeLine(text: notice.text, tone: notice.tone, dismiss: focus.dismissNotice)
        } else if let conversation, let error = conversation.error, conversation.loaded {
            NoticeLine(text: error, tone: .info)
        } else if let conversation, let activity = ActivityLine.Content(
            messages: conversation.messages, activity: conversation.activity, running: agent.status == "running") {
            ActivityLine(content: activity)
        } else if let ending = Self.ending(agent) {
            NoticeLine(text: ending.text, tone: ending.tone)
        }
    }

    /// How the agent's last task ended, when it didn't finish.
    static func ending(_ agent: SubagentRoster.Item) -> SessionActions.Notice? {
        switch agent.status {
        case "failed": .init(text: "\(agent.name)’s last task failed.", tone: .danger)
        case "interrupted": .init(text: "\(agent.name)’s last task was interrupted.", tone: .info)
        case "aborted": .init(text: "\(agent.name)’s last task was stopped.", tone: .info)
        default: nil
        }
    }

    // MARK: Menu

    private var menu: some View {
        Menu {
            Section {
                modelPicker
                if !thinkingLevels.isEmpty { thinkingPicker }
            }
            Section {
                if let reason = focus.dismissUnavailableReason(agent) {
                    locked("Dismiss \(agent.name)…", reason)
                } else {
                    Button("Dismiss \(agent.name)…", systemImage: "trash", role: .destructive) { confirmingDismiss = true }
                        .accessibilityIdentifier("dismiss-agent")
                }
            }
        } label: {
            Image(systemName: "ellipsis")
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(Theme.text)
                .frame(width: 36, height: 36)
                .background(Theme.glassButton, in: Circle())
                .contentShape(Circle())
        }
        // Model first, as drawn, though the menu opens upward.
        .menuOrder(.fixed)
        .accessibilityLabel("\(agent.name) menu")
        .accessibilityIdentifier("agent-menu")
        .task { await actions.loadModels() }
    }

    private var thinkingLevels: [String] {
        let levels = ComposerConfiguration.thinkingLevels(model: agent.model, models: actions.configuration.models)
        return levels.isEmpty ? (agent.thinkingLevel.map { [$0] } ?? []) : levels
    }

    /// An item that can't be used now, saying why.
    private func locked(_ title: String, _ reason: String) -> some View {
        Button {} label: {
            Text(title)
            Text(reason)
        }
        .disabled(true)
        // A disabled item doesn't read its subtitle.
        .accessibilityLabel("\(title), \(reason)")
    }

    @ViewBuilder
    private var modelPicker: some View {
        if let reason = focus.modelLockedReason(agent) {
            locked("Model", reason)
        } else if actions.configuration.models.isEmpty {
            Menu {
                if actions.configuration.loading {
                    Text("Loading models…")
                } else {
                    Button("Load models", systemImage: "arrow.clockwise") { Task { await actions.loadModels() } }
                }
            } label: {
                Text("Model")
                Text(actions.configuration.error.map { "Couldn’t load models. \($0)" }
                     ?? SessionActions.modelName(agent.model, models: []))
            }
        } else {
            Picker(selection: Binding(get: { agent.model }, set: { focus.changeModel(agent, to: $0) })) {
                ForEach(actions.modelGroups, id: \.provider) { group in
                    Section(group.provider) {
                        ForEach(group.models, id: \.id) { model in
                            Text(model.name).tag(model.id)
                        }
                    }
                }
            } label: {
                Text("Model")
                Text(SessionActions.modelName(agent.model, models: actions.configuration.models))
            }
            .pickerStyle(.menu)
        }
    }

    @ViewBuilder
    private var thinkingPicker: some View {
        if let reason = focus.thinkingLockedReason(agent) {
            locked("Thinking", reason)
        } else {
            Picker(selection: Binding(get: { agent.thinkingLevel ?? "" }, set: { focus.changeThinking(agent, to: $0) })) {
                ForEach(thinkingLevels, id: \.self) { level in
                    Text(SessionContext.thinkingName(level)).tag(level)
                }
            } label: {
                Text("Thinking")
                Text(SessionContext.thinkingName(agent.thinkingLevel ?? ""))
            }
            .pickerStyle(.menu)
        }
    }
}
