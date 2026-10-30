import Foundation

/// Child conversations belong to a session on a particular server.
public protocol SubagentClient: SessionClient {
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage]
}

public protocol SubagentStreamingClient: SubagentClient {
    func watchSubagent(session: String, conversation: String,
                      receive: @escaping @Sendable (SubagentTranscriptUpdate) async -> Void) async throws
}

public struct SubagentTranscriptUpdate: Sendable {
    public let messages: [TranscriptMessage]
    public let activity: String?

    public init(messages: [TranscriptMessage], activity: String?) {
        self.messages = messages
        self.activity = activity
    }
}

public struct SubagentRoster: Decodable, Sendable, Equatable {
    public struct Item: Decodable, Identifiable, Sendable, Equatable {
        public var id: String { name }
        public let name: String
        public let description: String
        public let model: String
        public let status: String
        public let conversationID: String?
        public let updatedAt: String?
        public var thinkingLevel: String? = nil
        public var configurationLabel: String {
            guard let thinkingLevel, !thinkingLevel.isEmpty else { return model }
            return model + " · " + (thinkingLevel == "off" ? "Thinking off" : thinkingLevel.capitalized + " thinking")
        }
        public var queuedTasks: Int? = nil
        public var generation: UInt64? = nil
        public var activeTaskID: String? = nil
        public var hasOutstandingWork: Bool { status == "running" || activeTaskID != nil || (queuedTasks ?? 0) > 0 }
        public var canDismiss: Bool { conversationID != nil && (generation ?? 0) > 0 }

    public init(name: String, description: String, model: String, status: String, conversationID: String?, updatedAt: String?, thinkingLevel: String? = nil, queuedTasks: Int? = nil, generation: UInt64? = nil, activeTaskID: String? = nil) {
        self.name = name
        self.description = description
        self.model = model
        self.status = status
        self.conversationID = conversationID
        self.updatedAt = updatedAt
        self.thinkingLevel = thinkingLevel
        self.queuedTasks = queuedTasks
        self.generation = generation
        self.activeTaskID = activeTaskID
    }
    }
    public let items: [Item]
    public let diagnostics: [String]

    public init(items: [Item], diagnostics: [String] = []) {
        self.items = items
        self.diagnostics = diagnostics
    }

    public init(_ snapshot: WireSessionSnapshot) throws {
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
