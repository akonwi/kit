import Foundation

/// Child conversations belong to a session on a particular server.
protocol SubagentClient: SessionClient {
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage]
}

protocol SubagentStreamingClient: SubagentClient {
    func watchSubagent(session: String, conversation: String,
                      receive: @escaping @Sendable (SubagentTranscriptUpdate) async -> Void) async throws
}

struct SubagentTranscriptUpdate: Sendable {
    let messages: [TranscriptMessage]
    let activity: String?
}

struct SubagentRoster: Decodable, Sendable {
    struct Item: Decodable, Identifiable, Sendable {
        var id: String { name }
        let name: String
        let description: String
        let model: String
        let status: String
        let conversationID: String?
        let updatedAt: String?
        var thinkingLevel: String? = nil
        var configurationLabel: String {
            guard let thinkingLevel, !thinkingLevel.isEmpty else { return model }
            return model + " · " + (thinkingLevel == "off" ? "Thinking off" : thinkingLevel.capitalized + " thinking")
        }
        var queuedTasks: Int? = nil
        var generation: UInt64? = nil
        var activeTaskID: String? = nil
        var hasOutstandingWork: Bool { status == "running" || activeTaskID != nil || (queuedTasks ?? 0) > 0 }
        var canDismiss: Bool { conversationID != nil && (generation ?? 0) > 0 }
    }
    let items: [Item]
    let diagnostics: [String]

    init(_ snapshot: WireSessionSnapshot) throws {
        let definitions = snapshot.subagentDefinitions ?? []
        let conversations = snapshot.subagentConversations ?? []
        guard Set(definitions.map(\.name)).count == definitions.count,
              Set(conversations.map(\.id)).count == conversations.count,
              Set(conversations.map(\.agentName)).count == conversations.count,
              definitions.allSatisfy({ !$0.name.isEmpty }),
              conversations.allSatisfy({ !$0.id.isEmpty && !$0.agentName.isEmpty }) else { throw ClientError.invalidPayload }
        let byName = Dictionary(uniqueKeysWithValues: conversations.map { ($0.agentName, $0) })
        let names = Set(definitions.map(\.name))
        var rows = definitions.map { definition in
            let conversation = byName[definition.name]
            return Item(name: definition.name, description: definition.description,
                        model: conversation?.model ?? definition.model ?? "",
                        status: conversation?.state ?? "inactive", conversationID: conversation?.id,
                        updatedAt: conversation?.updatedAt, thinkingLevel: conversation?.thinkingLevel, queuedTasks: conversation?.queuedTasks, generation: conversation?.generation, activeTaskID: conversation?.activeTaskId)
        }
        rows += conversations.filter { !names.contains($0.agentName) }.map {
            Item(name: $0.agentName, description: "Previously active subagent conversation", model: $0.model,
                 status: $0.state, conversationID: $0.id, updatedAt: $0.updatedAt, thinkingLevel: $0.thinkingLevel, queuedTasks: $0.queuedTasks, generation: $0.generation, activeTaskID: $0.activeTaskId)
        }
        func rank(_ status: String) -> Int {
            switch status { case "running": 0; case "failed": 1; case "aborted": 2; case "interrupted": 3; case "idle": 4; default: 5 }
        }
        items = rows.sorted {
            if rank($0.status) != rank($1.status) { return rank($0.status) < rank($1.status) }
            return $0.name.localizedStandardCompare($1.name) == .orderedAscending
        }
        diagnostics = (snapshot.subagentDiagnostics ?? []).map(\.message)
    }
}
