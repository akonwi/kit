import Foundation

struct Fixture: Decodable, Sendable {
    let sessions: [SessionExcerpt]


}

struct SessionExcerpt: Decodable, Identifiable, Sendable {
    let id: String
    var isTemporary: Bool? = nil
    var title: String
    var parentSessionID: String? = nil
    var parentSessionName: String? = nil
    var sourceNotice: String? = nil
    let sourceTitle: String
    let model: String
    let thinking: String
    let workspace: String
    var cwd: String? = nil
    var usage: SessionUsage? = nil
    var contextTokens: Int? = nil
    var contextWindow: Int? = nil
    var gitHead: String? = nil
    var gitDirty: Bool? = nil
    var gitHeadKind: String? = nil
    var pullRequestNumber: Int? = nil
    var pullRequestURL: String? = nil
    /// Local attachment identity, renewed when the event stream resynchronizes.
    var watchGeneration: String? = nil
    let date: String
    var lastActivity: String? = nil
    var activity: String? = nil
    var tabStatus: SessionTabStatus? = nil
    var messages: [TranscriptMessage]
    var historyCursor: String? = nil
    var historyStart: Int64? = nil
    var subagents: SubagentRoster? = nil
    var subagentDiagnostics: [WireSubagentDiagnostic]? = nil
    var promptCommands: [PromptCommand]? = nil
    var pluginCommands: [PluginCommand]? = nil
    var pluginFooter: WirePluginFooter? = nil
    var activeCompactionID: String? = nil
    var compactionOutcome: CompactionOutcome? = nil
    var activeBashID: String? = nil
    var activeRunID: String? = nil
    var observedTurns: [String]? = nil
    var followUps: FollowUpState? = nil
    var providerRetryAt: String? = nil
    var providerRetryCount: Int? = nil
    var terminalError: String? = nil
    var configurationRevision: UInt64? = nil
    var annotations: [FileAnnotation]? = nil
    var pendingInteractions: [WireInteractionRequest]? = nil
    var scratchpad: ScratchpadRecord? = nil
    var mcpServers: [MCPServerStatus]? = nil
    var mcpWarnings: [String]? = nil
}

struct MCPServerStatus: Decodable, Sendable, Equatable, Identifiable {
    var id: String { name }
    let name: String
    let state: String
    let transport: String
    let toolCount: Int
    let oauthSaved: Bool
    let description: String
    let source: String
    let configPath: String
    let lastError: String

    init(_ wire: WireMCPServerStatus) throws {
        guard !wire.name.isEmpty, wire.name.count <= 128, wire.toolCount >= 0,
              ["stdio", "http"].contains(wire.transport),
              ["kit-user", "shared-project", "kit-project"].contains(wire.source),
              ["disabled", "configured", "connecting", "authorizing", "connected", "error"].contains(wire.state),
              (wire.description?.utf8.count ?? 0) <= 512, (wire.configPath?.utf8.count ?? 0) <= 1024,
              (wire.lastError?.utf8.count ?? 0) <= 512 else {
            throw ClientError.invalidPayload
        }
        name = wire.name; state = wire.state; transport = wire.transport; toolCount = wire.toolCount
        oauthSaved = wire.oauthSaved; description = wire.description ?? ""; source = wire.source
        configPath = wire.configPath ?? ""; lastError = wire.lastError ?? ""
    }
}

struct TranscriptMessage: Decodable, Identifiable, Sendable, Equatable {
    let id: String
    let role: String
    var text: String
    var tools: [ToolActivity]
    var attachments: [TranscriptAttachment]? = nil
    var annotations: [FileAnnotation]? = nil
    var bash: BashExecution? = nil
}

struct ToolActivity: Decodable, Identifiable, Sendable, Equatable {
    let id: String
    let name: String
    let summary: String
    let output: String
    var arguments: String? = nil
    let failed: Bool
    var status: String? = nil
    var thinking: String? = nil
    var attachments: [TranscriptAttachment]? = nil
    var contentTruncated: Bool? = nil
}

struct TranscriptAttachment: Decodable, Sendable, Equatable, Hashable {
    let id: String?
    let filename: String
    let mediaType: String?
    let isImage: Bool
}

struct CompactionOutcome: Decodable, Sendable {
    let id: String
    let failed: Bool
    let detail: String
}
