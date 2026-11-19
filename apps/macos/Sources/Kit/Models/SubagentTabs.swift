import Foundation
import KitTranscript

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
/// A tab restored for an agent whose conversation ended while the app
/// wasn't watching closes when the roster first arrives.
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

    /// Takes in a roster and says which tabs open and close. `messages` is
    /// the main transcript, whose Start agent calls order agents that start
    /// in the same update; agents it doesn't mention follow by name. `open`
    /// names the agents whose tabs are open.
    mutating func reconcile(_ roster: SubagentRoster, messages: [TranscriptMessage], open: [String]) -> Changes {
        let current = Dictionary(roster.items.compactMap { item in item.conversationID.map { (item.name, $0) } }) { first, _ in first }
        defer {
            conversations = current
            seen.formUnion(current.values)
        }
        var changes = Changes()
        guard let previous = conversations else {
            changes.close = open.filter { current[$0] == nil }
            return changes
        }
        let order = Self.startOrder(messages)
        changes.open = current.filter { !seen.contains($0.value) }.map(\.key).sorted { a, b in
            switch (order[a], order[b]) {
            case let (x?, y?): return x < y
            case (_?, nil): return true
            case (nil, _?): return false
            case (nil, nil): return a.localizedStandardCompare(b) == .orderedAscending
            }
        }
        changes.close = previous.keys.filter { current[$0] == nil }
            .sorted { $0.localizedStandardCompare($1) == .orderedAscending }
        return changes
    }

    /// Each agent's position among the transcript's Start agent calls, as
    /// the iOS app orders its agent strip. Other calls naming an agent, such
    /// as a message to it, don't count.
    static func startOrder(_ messages: [TranscriptMessage]) -> [String: Int] {
        var order: [String: Int] = [:]
        for message in messages where message.role == "tools" {
            for tool in message.tools where isStart(tool) {
                guard case .agent(let name)? = ToolPresentation(tool).link, order[name] == nil else { continue }
                order[name] = order.count
            }
        }
        return order
    }

    private static func isStart(_ tool: ToolActivity) -> Bool {
        guard tool.name == "subagent", let arguments = tool.arguments,
              let object = try? JSONSerialization.jsonObject(with: Data(arguments.utf8)) as? [String: Any] else { return false }
        return object["action"] as? String == "start"
    }
}
