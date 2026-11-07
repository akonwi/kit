import Foundation
import KitClient
import Observation

/// The client the agent switcher acts through: each subagent's transcript,
/// configuration, and dismissal.
typealias SubagentFocusClient = SubagentConfigurationClient & SubagentDismissalClient

/// Which agent's transcript the session screen shows, Main's or a
/// subagent's, and what the agent bar can do with the subagent in view.
///
/// The roster on each snapshot is authoritative: changes and dismissals
/// refresh it rather than projecting their own values.
@MainActor @Observable
final class SubagentFocus {
    let session: String
    let client: any SubagentFocusClient
    /// Fetches a fresh snapshot and hands it to the screen.
    @ObservationIgnored var refresh: (@MainActor () async throws -> Void)?
    /// Whether the session's stream is live. Nothing changes without it.
    var live = false

    /// The subagent in view, or nil for Main.
    private(set) var focus: String?
    /// The subagents shown this visit, so each keeps its place in its
    /// transcript when the reader comes back to it.
    private(set) var visited: [String] = []
    private(set) var strip = SubagentStrip(roster: nil, messages: [])
    let dismissal = SubagentDismissalOperation()
    /// Keyed by conversation, so an agent started again begins afresh.
    @ObservationIgnored private var conversations: [String: SubagentConversationState] = [:]
    @ObservationIgnored private var configurations: [String: SubagentConfigurationOperation] = [:]

    init(session: String, client: any SubagentFocusClient) {
        self.session = session
        self.client = client
    }

    /// Takes in each snapshot the session's stream delivers. A subagent that
    /// is no longer started, such as one dismissed elsewhere, leaves the
    /// strip, and Main comes back into view if it was showing.
    func reconcile(_ snapshot: SessionExcerpt) {
        strip = SubagentStrip(roster: snapshot.subagents, messages: snapshot.messages)
        visited.removeAll { strip.agent(named: $0) == nil }
        if let focus, strip.agent(named: focus) == nil { self.focus = nil }
        // A conversation that's gone takes its transcript and feedback with it.
        let current = Set(strip.agents.compactMap(\.conversationID))
        conversations = conversations.filter { current.contains($0.key) }
        configurations = configurations.filter { current.contains($0.key) }
    }

    /// Shows an agent's transcript, or Main's for nil.
    func show(_ name: String?) {
        if let name, strip.agent(named: name) == nil { return }
        // A failed dismissal belongs to the agent it was for.
        if name != focus { dismissal.clearError() }
        focus = name
        if let name, !visited.contains(name) { visited.append(name) }
    }

    /// The subagent in view.
    var focused: SubagentRoster.Item? { focus.flatMap(strip.agent(named:)) }

    func conversation(_ agent: SubagentRoster.Item) -> SubagentConversationState? {
        guard let id = agent.conversationID else { return nil }
        if let state = conversations[id] { return state }
        let state = SubagentConversationState()
        conversations[id] = state
        return state
    }

    func configuration(_ agent: SubagentRoster.Item) -> SubagentConfigurationOperation? {
        guard let id = agent.conversationID else { return nil }
        if let operation = configurations[id] { return operation }
        let operation = SubagentConfigurationOperation()
        configurations[id] = operation
        return operation
    }

    // MARK: Model and thinking

    /// Why the agent's model can't change now, or nil when it can. The server
    /// takes a new model only when the agent has no work running or queued.
    func modelLockedReason(_ agent: SubagentRoster.Item) -> String? {
        if let reason = configurationLockedReason(agent) { return reason }
        if agent.hasOutstandingWork { return "Can change once \(agent.name)’s work finishes" }
        return nil
    }

    /// Why the agent's thinking level can't change now. It can change while
    /// the agent works; its next request uses it.
    func thinkingLockedReason(_ agent: SubagentRoster.Item) -> String? {
        configurationLockedReason(agent)
    }

    private func configurationLockedReason(_ agent: SubagentRoster.Item) -> String? {
        if !live { return "Reconnect to change" }
        if configuration(agent)?.pending == true { return "Changing…" }
        if (agent.generation ?? 0) == 0 { return "Unavailable" }
        return nil
    }

    func changeModel(_ agent: SubagentRoster.Item, to model: String) {
        guard modelLockedReason(agent) == nil, model != agent.model else { return }
        configure(agent, model: model, thinking: nil)
    }

    func changeThinking(_ agent: SubagentRoster.Item, to level: String) {
        guard thinkingLockedReason(agent) == nil, level != agent.thinkingLevel else { return }
        configure(agent, model: nil, thinking: level)
    }

    private func configure(_ agent: SubagentRoster.Item, model: String?, thinking: String?) {
        guard let conversation = agent.conversationID, let generation = agent.generation,
              let operation = configuration(agent) else { return }
        dismissal.clearError()
        Task {
            await operation.change(session: session, conversation: conversation, generation: generation,
                agent: agent.name, model: model, thinking: thinking, client: client,
                outstandingWork: { [weak self] in
                    self?.strip.agents.first { $0.conversationID == conversation }?.hasOutstandingWork ?? false
                }, refresh: refreshSnapshot)
        }
    }

    // MARK: Dismiss

    /// Why the agent can't be dismissed now, or nil when it can.
    func dismissUnavailableReason(_ agent: SubagentRoster.Item) -> String? {
        if !live { return "Reconnect to dismiss" }
        if dismissal.pending { return "Dismissing…" }
        if !agent.canDismiss { return "Unavailable" }
        return nil
    }

    /// Ends the agent's conversation, aborting its work. Main comes back into
    /// view when it was showing.
    func dismiss(_ agent: SubagentRoster.Item) async {
        guard dismissUnavailableReason(agent) == nil,
              let conversation = agent.conversationID, let generation = agent.generation else { return }
        configuration(agent)?.clearFeedback()
        let dismissed = await dismissal.dismiss(session: session, conversation: conversation,
                                                generation: generation, client: client, refresh: refreshSnapshot)
        guard dismissed else { return }
        // Only this conversation goes; one started again under the name stays.
        guard strip.agent(named: agent.name).map({ $0.conversationID == conversation }) ?? true else { return }
        visited.removeAll { $0 == agent.name }
        if focus == agent.name { focus = nil }
    }

    // MARK: Feedback

    /// What the last change or dismissal of the agent in view came to, when
    /// it needs saying.
    var notice: SessionActions.Notice? {
        if let error = dismissal.error { return .init(text: "Couldn’t dismiss. \(error)", tone: .danger) }
        guard let agent = focused, let operation = configuration(agent) else { return nil }
        if let error = operation.error { return .init(text: error, tone: .danger) }
        if let warning = operation.warning { return .init(text: warning, tone: .info) }
        return nil
    }

    func dismissNotice() {
        dismissal.clearError()
        if let agent = focused { configuration(agent)?.clearFeedback() }
    }

    private func refreshSnapshot() async throws {
        guard let refresh else { throw CancellationError() }
        try await refresh()
    }
}
