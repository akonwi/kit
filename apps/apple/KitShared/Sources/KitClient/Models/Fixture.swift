import Foundation

public struct Fixture: Decodable, Sendable {
    public let sessions: [SessionExcerpt]

    public init(sessions: [SessionExcerpt]) {
        self.sessions = sessions
    }


}

public struct SessionExcerpt: Decodable, Identifiable, Sendable, Equatable {
    public var id: String
    public var isTemporary: Bool? = nil
    public var title: String
    public var parentSessionID: String? = nil
    public var parentSessionName: String? = nil
    public var sourceNotice: String? = nil
    public let sourceTitle: String
    public var model: String
    public var thinking: String
    public var workspace: String
    public var cwd: String? = nil
    public var usage: SessionUsage? = nil
    public var contextTokens: Int? = nil
    public var contextWindow: Int? = nil
    public var gitHead: String? = nil
    public var gitDirty: Bool? = nil
    public var gitHeadKind: String? = nil
    public var pullRequestNumber: Int? = nil
    public var pullRequestURL: String? = nil
    /// Local attachment identity, renewed when the event stream resynchronizes.
    public var watchGeneration: String? = nil
    public let date: String
    public var lastActivity: String? = nil
    public var activity: String? = nil
    public var tabStatus: SessionTabStatus? = nil
    public var messages: [TranscriptMessage]
    public var historyCursor: String? = nil
    public var historyStart: Int64? = nil
    public var subagents: SubagentRoster? = nil
    public var subagentDiagnostics: [WireSubagentDiagnostic]? = nil
    public var promptCommands: [PromptCommand]? = nil
    public var pluginCommands: [PluginCommand]? = nil
    public var pluginFooter: WirePluginFooter? = nil
    public var activeCompactionID: String? = nil
    public var compactionOutcome: CompactionOutcome? = nil
    public var activeBashID: String? = nil
    public var activeRunID: String? = nil
    public var observedTurns: [String]? = nil
    public var followUps: FollowUpState? = nil
    public var providerRetryAt: String? = nil
    public var providerRetryCount: Int? = nil
    public var configurationRevision: UInt64? = nil
    /// What the session's model accepts in user messages; nil when the server omits it.
    public var inputs: [String]? = nil
    public var annotations: [FileAnnotation]? = nil
    public var pendingInteractions: [WireInteractionRequest]? = nil
    public var scratchpad: ScratchpadRecord? = nil
    public var mcpServers: [MCPServerStatus]? = nil
    public var mcpWarnings: [String]? = nil
    /// Pages a plugin asked this app to open that arrived live on the event
    /// stream, oldest first and bounded. Never part of a snapshot.
    public var requestedPages: [RequestedPage]? = nil

    public init(id: String, isTemporary: Bool? = nil, title: String, parentSessionID: String? = nil, parentSessionName: String? = nil, sourceNotice: String? = nil, sourceTitle: String, model: String, thinking: String, workspace: String, cwd: String? = nil, usage: SessionUsage? = nil, contextTokens: Int? = nil, contextWindow: Int? = nil, gitHead: String? = nil, gitDirty: Bool? = nil, gitHeadKind: String? = nil, pullRequestNumber: Int? = nil, pullRequestURL: String? = nil, watchGeneration: String? = nil, date: String, lastActivity: String? = nil, activity: String? = nil, tabStatus: SessionTabStatus? = nil, messages: [TranscriptMessage], historyCursor: String? = nil, historyStart: Int64? = nil, subagents: SubagentRoster? = nil, subagentDiagnostics: [WireSubagentDiagnostic]? = nil, promptCommands: [PromptCommand]? = nil, pluginCommands: [PluginCommand]? = nil, pluginFooter: WirePluginFooter? = nil, activeCompactionID: String? = nil, compactionOutcome: CompactionOutcome? = nil, activeBashID: String? = nil, activeRunID: String? = nil, observedTurns: [String]? = nil, followUps: FollowUpState? = nil, providerRetryAt: String? = nil, providerRetryCount: Int? = nil, configurationRevision: UInt64? = nil, inputs: [String]? = nil, annotations: [FileAnnotation]? = nil, pendingInteractions: [WireInteractionRequest]? = nil, scratchpad: ScratchpadRecord? = nil, mcpServers: [MCPServerStatus]? = nil, mcpWarnings: [String]? = nil, requestedPages: [RequestedPage]? = nil) {
        self.id = id
        self.isTemporary = isTemporary
        self.title = title
        self.parentSessionID = parentSessionID
        self.parentSessionName = parentSessionName
        self.sourceNotice = sourceNotice
        self.sourceTitle = sourceTitle
        self.model = model
        self.thinking = thinking
        self.workspace = workspace
        self.cwd = cwd
        self.usage = usage
        self.contextTokens = contextTokens
        self.contextWindow = contextWindow
        self.gitHead = gitHead
        self.gitDirty = gitDirty
        self.gitHeadKind = gitHeadKind
        self.pullRequestNumber = pullRequestNumber
        self.pullRequestURL = pullRequestURL
        self.watchGeneration = watchGeneration
        self.date = date
        self.lastActivity = lastActivity
        self.activity = activity
        self.tabStatus = tabStatus
        self.messages = messages
        self.historyCursor = historyCursor
        self.historyStart = historyStart
        self.subagents = subagents
        self.subagentDiagnostics = subagentDiagnostics
        self.promptCommands = promptCommands
        self.pluginCommands = pluginCommands
        self.pluginFooter = pluginFooter
        self.activeCompactionID = activeCompactionID
        self.compactionOutcome = compactionOutcome
        self.activeBashID = activeBashID
        self.activeRunID = activeRunID
        self.observedTurns = observedTurns
        self.followUps = followUps
        self.providerRetryAt = providerRetryAt
        self.providerRetryCount = providerRetryCount
        self.configurationRevision = configurationRevision
        self.inputs = inputs
        self.annotations = annotations
        self.pendingInteractions = pendingInteractions
        self.scratchpad = scratchpad
        self.mcpServers = mcpServers
        self.mcpWarnings = mcpWarnings
        self.requestedPages = requestedPages
    }
}

/// A web page a session asked the app to open, identified by the event that
/// carried the request so it opens at most once.
public struct RequestedPage: Decodable, Sendable, Equatable {
    public let id: String
    public let url: URL

    public init(id: String, url: URL) {
        self.id = id
        self.url = url
    }
}

public struct MCPServerStatus: Decodable, Sendable, Equatable, Identifiable {
    public var id: String { name }
    public let name: String
    public let state: String
    public let transport: String
    public let toolCount: Int
    public let oauthSaved: Bool
    public let description: String
    public let source: String
    public let configPath: String
    public let lastError: String

    public init(_ wire: WireMCPServerStatus) throws {
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

public struct TranscriptMessage: Decodable, Identifiable, Sendable, Equatable {
    public var id: String
    public let role: String
    public var text: String
    public var tools: [ToolActivity]
    public var attachments: [TranscriptAttachment]? = nil
    public var annotations: [FileAnnotation]? = nil
    public var bash: BashExecution? = nil
    /// Set on a plugin-submitted message, which presents as a "plugin" row.
    public var plugin: PluginMessageOrigin? = nil
    /// Set on a context marker, which presents as a "marker" row.
    public var marker: TranscriptMarker? = nil
    /// Set on a message from another subagent, which presents as an "inbox" row.
    public var inbox: SubagentInboxMessage? = nil

    public init(id: String, role: String, text: String, tools: [ToolActivity], attachments: [TranscriptAttachment]? = nil, annotations: [FileAnnotation]? = nil, bash: BashExecution? = nil, plugin: PluginMessageOrigin? = nil, marker: TranscriptMarker? = nil, inbox: SubagentInboxMessage? = nil) {
        self.id = id
        self.role = role
        self.text = text
        self.tools = tools
        self.attachments = attachments
        self.annotations = annotations
        self.bash = bash
        self.plugin = plugin
        self.marker = marker
        self.inbox = inbox
    }
}

/// The plugin that submitted a transcript message and the turn the message
/// started. The turn identifies the message across its live and persisted rows.
public struct PluginMessageOrigin: Decodable, Sendable, Equatable {
    public let pluginID: String
    public let turnID: String

    public init(pluginID: String, turnID: String) {
        self.pluginID = pluginID
        self.turnID = turnID
    }
}

public struct ToolActivity: Decodable, Identifiable, Sendable, Equatable {
    public let id: String
    public let name: String
    public let summary: String
    public let output: String
    public var arguments: String? = nil
    public let failed: Bool
    public var status: String? = nil
    public var thinking: String? = nil
    public var attachments: [TranscriptAttachment]? = nil
    public var contentTruncated: Bool? = nil
    /// The result's structured details as JSON, such as an exit code or a match count.
    public var details: String? = nil
    /// When the app saw the call start and finish. Only calls the app watched
    /// live carry times; the server does not record them (#187).
    public var startedAt: Date? = nil
    public var finishedAt: Date? = nil
    /// True when the turn ended before the call ran.
    public var notRun: Bool? = nil
    /// True when the app received the call's completion live, rather than in
    /// a snapshot or a replay of events from before it attached. Client
    /// actions requested by a tool, such as open_url, run only for these.
    public var completedLive: Bool? = nil

    /// A call that is planned or executing.
    public var running: Bool { status == "Running…" || status == "Planned" }

    public init(id: String, name: String, summary: String, output: String, arguments: String? = nil, failed: Bool, status: String? = nil, thinking: String? = nil, attachments: [TranscriptAttachment]? = nil, contentTruncated: Bool? = nil, details: String? = nil, startedAt: Date? = nil, finishedAt: Date? = nil, notRun: Bool? = nil, completedLive: Bool? = nil) {
        self.id = id
        self.name = name
        self.summary = summary
        self.output = output
        self.arguments = arguments
        self.failed = failed
        self.status = status
        self.thinking = thinking
        self.attachments = attachments
        self.contentTruncated = contentTruncated
        self.details = details
        self.startedAt = startedAt
        self.finishedAt = finishedAt
        self.notRun = notRun
        self.completedLive = completedLive
    }
}

public struct TranscriptAttachment: Decodable, Sendable, Equatable, Hashable {
    public let id: String?
    public let filename: String
    public let mediaType: String?
    public let isImage: Bool

    public init(id: String?, filename: String, mediaType: String?, isImage: Bool) {
        self.id = id
        self.filename = filename
        self.mediaType = mediaType
        self.isImage = isImage
    }
}

public struct CompactionOutcome: Decodable, Sendable, Equatable {
    public let id: String
    public let failed: Bool
    public let detail: String
    /// Whether the compaction ran inside a turn, as automatic compaction
    /// does, rather than for a client's request or a model change.
    public var automatic = true

    public init(id: String, failed: Bool, detail: String, automatic: Bool = true) {
        self.id = id
        self.failed = failed
        self.detail = detail
        self.automatic = automatic
    }
}
