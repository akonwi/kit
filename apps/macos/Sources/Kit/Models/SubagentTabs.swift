import Foundation

/// Which subagent tabs a session's workspace opens and closes by itself as
/// the session's roster changes.
///
/// A conversation that starts while the session is attached opens its
/// agent's tab, once: a tab the user closes stays closed for that
/// conversation, and an agent started again, in a new conversation, opens
/// again. Conversations already started when the roster first arrives, as
/// in loaded history or after a relaunch, are only remembered. When an
/// agent's conversation ends, as when it's dismissed anywhere, its tab
/// closes; a conversation that replaces it under the same name keeps it.
struct SubagentTabs: Equatable {
    /// What the workspace does after a roster update.
    struct Changes: Equatable {
        /// Agents whose tabs open, in the order they open.
        var open: [String] = []
        /// Agents whose tabs close.
        var close: [String] = []
    }

    /// Each agent's conversation in the last roster; nil before the first.
    private var conversations: [String: String]?
    /// Every conversation seen, opened or not, so none opens twice.
    private var seen: Set<String> = []

    /// Takes in a roster and says which tabs open and close. Agents that
    /// start in the same update open in name order.
    mutating func reconcile(_ roster: SubagentRoster) -> Changes {
        let current = Dictionary(roster.items.compactMap { item in item.conversationID.map { (item.name, $0) } }) { first, _ in first }
        defer {
            conversations = current
            seen.formUnion(current.values)
        }
        guard let previous = conversations else { return Changes() }
        var changes = Changes()
        changes.open = current.filter { !seen.contains($0.value) }.map(\.key)
            .sorted { $0.localizedStandardCompare($1) == .orderedAscending }
        changes.close = previous.keys.filter { current[$0] == nil }
            .sorted { $0.localizedStandardCompare($1) == .orderedAscending }
        return changes
    }
}
