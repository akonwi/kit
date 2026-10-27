import Foundation
import Observation

/// The subagents picker's rows, as the TUI's `/subagents` picker lists them:
/// each agent's status, name, description, and configuration, running agents
/// first. An agent that hasn't started is listed but can't be opened, since
/// the model starts agents.
enum SubagentPicker {
    static let placeholder = "Search subagents…"
    static let empty = "No subagents are defined"
    static func noMatch(_ query: String) -> String { "No subagents match “\(query)”" }
    /// The reason an agent that hasn't started can't be opened.
    static let notStarted = "not started"

    /// The roster in its own order, which is the TUI's: running, failed,
    /// aborted, interrupted, idle, then agents that haven't started. An
    /// agent without its own model or thinking level shows the session's.
    static func rows(_ roster: SubagentRoster, sessionModel: String, sessionThinking: String,
                     models: [WireModelCapability] = []) -> [PaletteCommand] {
        roster.items.map { agent in
            let started = agent.conversationID != nil
            let thinking = agent.thinkingLevel ?? ""
            return PaletteCommand(
                id: agent.name, name: agent.name, description: agent.description,
                meta: configuration(model: agent.model.isEmpty ? sessionModel : agent.model,
                                    thinking: thinking.isEmpty ? sessionThinking : thinking, models: models),
                unavailableReason: started ? nil : notStarted,
                status: started ? status(agent.status) : nil)
        }
    }

    /// A started conversation's status icon.
    static func status(_ state: String) -> PaletteRowStatus {
        switch state {
        case "running": .working
        case "failed": .failed
        case "aborted", "interrupted": .stopped
        default: .idle
        }
    }

    /// The model and thinking level as `Claude Sonnet 4.5 (high)`. The model
    /// is named as the catalog names it, or from its selector when the
    /// catalog doesn't list it.
    static func configuration(model: String, thinking: String, models: [WireModelCapability] = []) -> String {
        let name = model.isEmpty ? "" : models.first { $0.id == model }?.name ?? modelName(model)
        if thinking.isEmpty { return name }
        return name.isEmpty ? "(\(thinking))" : "\(name) (\(thinking))"
    }

    /// A `provider/model` selector as the TUI shows it: `anthropic/claude-sonnet-4-5`
    /// is `Claude Sonnet 4.5`, `openai/gpt-5.3-codex` is `GPT 5.3 Codex`.
    static func modelName(_ selector: String) -> String {
        let id = selector.firstIndex(of: "/").map { String(selector[selector.index(after: $0)...]) } ?? selector
        func digits(_ part: String) -> Bool { !part.isEmpty && part.allSatisfy(\.isASCIIDigit) }
        func capitalized(_ part: String) -> String {
            switch part.lowercased() {
            case "gpt": return "GPT"
            case "openai": return "OpenAI"
            default: return part.isEmpty || digits(part) ? part : part.prefix(1).uppercased() + part.dropFirst()
            }
        }
        let parts = id.split(separator: "-", omittingEmptySubsequences: false).map(String.init)
        var words: [String] = []
        var index = 0
        while index < parts.count {
            var part = parts[index]
            if index + 1 < parts.count, digits(part), digits(parts[index + 1]) {
                part += "." + parts[index + 1]
                index += 1
            }
            words.append(capitalized(part))
            index += 1
        }
        return words.joined(separator: " ")
    }
}

private extension Character {
    var isASCIIDigit: Bool { ("0"..."9").contains(self) }
}

/// What the subagents picker does with its rows: Return opens a started
/// agent's conversation, and ⌘⌫ asks before dismissing it. It reads the
/// roster live, so it acts on the agent as the server last reported it.
@MainActor @Observable
final class SubagentPickerState {
    /// An agent the dismissal alert asks about, in the session it belongs to.
    struct Confirmation: Equatable {
        let agent: SubagentRoster.Item
        let session: String
    }

    /// The selected row's agent name, as the list reports it.
    var selectedID: String?
    private(set) var refusal = PaletteRefusal.none
    private(set) var confirmation: Confirmation?

    func agent(_ name: String?, in store: SessionStore) -> SubagentRoster.Item? {
        guard let name else { return nil }
        return store.selected?.subagents?.items.first { $0.name == name }
    }

    /// Whether the agent's conversation can be dismissed now.
    func canDismiss(_ name: String?, in store: SessionStore) -> Bool {
        agent(name, in: store)?.canDismiss == true && store.canDismissSubagent
    }

    /// Opens a started agent's conversation and closes the palette. An agent
    /// that hasn't started flashes its reason instead.
    func activate(_ row: PaletteCommand, store: SessionStore) {
        selectedID = row.id
        guard agent(row.id, in: store)?.conversationID != nil else {
            refuse(row.id)
            return
        }
        store.ui.palette = false
        store.ui.workspace.open(.agent(row.id))
    }

    /// Asks before dismissing the agent's conversation, the selected one by default.
    func requestDismissal(_ name: String? = nil, store: SessionStore) {
        let name = name ?? selectedID
        guard canDismiss(name, in: store), let agent = agent(name, in: store) else { return }
        confirmation = Confirmation(agent: agent, session: store.selectedID)
    }

    func cancelDismissal() { confirmation = nil }

    /// Dismisses the conversation the alert asked about. The picker stays
    /// open; a failure shows beneath its search field.
    func confirmDismissal(_ confirmation: Confirmation, store: SessionStore) async {
        if self.confirmation == confirmation { self.confirmation = nil }
        await store.dismissSubagent(confirmation.agent, session: confirmation.session)
    }

    private func refuse(_ id: String) {
        let next = refusal.next(id)
        refusal = next
        // Settle the refusal so a row scrolled back into view doesn't flash again.
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: PaletteRefusal.duration + .milliseconds(300))
            guard let self, self.refusal == next else { return }
            self.refusal = PaletteRefusal(rowID: "", generation: next.generation)
        }
    }
}
