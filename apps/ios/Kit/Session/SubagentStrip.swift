import Foundation
import KitClient
import KitTranscript

/// The started subagents, in the order their buttons appear beside Main.
///
/// Agents that need a look come first, then the ones working, then the idle
/// ones. Within a group they keep the order the main agent started them, so a
/// button moves only when its agent's group changes.
struct SubagentStrip: Equatable {
    /// How urgently an agent's button asks for attention.
    enum Group: Int, Comparable {
        /// Its last task failed or was interrupted.
        case needsLook
        /// It has work running or queued.
        case working
        /// It finished, or its work was stopped.
        case idle

        static func < (a: Group, b: Group) -> Bool { a.rawValue < b.rawValue }

        init(_ agent: SubagentRoster.Item) {
            switch agent.status {
            case "failed", "interrupted": self = .needsLook
            case "running": self = .working
            default: self = agent.hasOutstandingWork ? .working : .idle
            }
        }
    }

    let agents: [SubagentRoster.Item]

    /// Orders the roster's started agents. `messages` is the main transcript,
    /// whose Start agent calls give the order the agents started in; agents it
    /// doesn't mention, such as ones started before the loaded history, follow
    /// by name.
    init(roster: SubagentRoster?, messages: [TranscriptMessage]) {
        let started = (roster?.items ?? []).filter { $0.conversationID != nil }
        let order = Self.startOrder(messages)
        agents = started.sorted { a, b in
            let ga = Group(a), gb = Group(b)
            if ga != gb { return ga < gb }
            switch (order[a.name], order[b.name]) {
            case let (x?, y?): return x < y
            case (_?, nil): return true
            case (nil, _?): return false
            case (nil, nil): return a.name.localizedStandardCompare(b.name) == .orderedAscending
            }
        }
    }

    /// Whether there is anything to switch to.
    var isEmpty: Bool { agents.isEmpty }

    func agent(named name: String) -> SubagentRoster.Item? { agents.first { $0.name == name } }

    /// Each agent's position among the transcript's Start agent calls. Other
    /// calls naming an agent, such as a message to it, don't count.
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
