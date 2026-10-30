// Application projection records for protocol values used by the native UI.
// Closed OpenAPI structures are aliases where their generated shape is directly
// usable. Flattened unions and compatibility-facing records remain explicit app
// models and are decoded only at the generated-client boundary.

import Foundation

public struct WirePluginCommandInput: Codable, Sendable {
    public let `id`: String
    public let `instance`: String
    public let `args`: String

    public init(`id`: String, `instance`: String, `args`: String) {
        self.`id` = `id`
        self.`instance` = `instance`
        self.`args` = `args`
    }
}

public enum WireModelInputKind: String, Codable, Sendable {
    case value0 = "text"
    case value1 = "image"
}

public typealias WireSessionInfo = Components.Schemas.SessionInfo

public typealias WireWorkspaceRef = Components.Schemas.WorkspaceRef

public enum WireTranscriptContentKind: String, Codable, Sendable {
    case value0 = "text"
    case value1 = "thinking"
    case value2 = "toolCall"
    case value3 = "image"
    case value4 = "file"
    case value5 = "annotations"
    case value6 = "promptCommand"
}

public enum WireAnnotationAnchorKind: String, Codable, Sendable {
    case value0 = "workspace_file"
    case value1 = "working_tree_diff"
}

public struct WireWorkspaceFileAnnotationAnchor: Codable, Sendable {
    public let `workspaceId`: String
    public let `path`: String
    public let `fileRevision`: String
    public let `startLine`: Int
    public let `endLine`: Int

    public init(`workspaceId`: String, `path`: String, `fileRevision`: String, `startLine`: Int, `endLine`: Int) {
        self.`workspaceId` = `workspaceId`
        self.`path` = `path`
        self.`fileRevision` = `fileRevision`
        self.`startLine` = `startLine`
        self.`endLine` = `endLine`
    }
}

public struct WireWorkingTreeDiffAnnotationAnchor: Codable, Sendable, Equatable {
    public let `targetId`: String
    public let `targetRevision`: String
    public let `path`: String
    public let `fileRevision`: String
    public let `side`: String
    public let `startLine`: Int
    public let `endLine`: Int

    public init(`targetId`: String, `targetRevision`: String, `path`: String, `fileRevision`: String, `side`: String, `startLine`: Int, `endLine`: Int) {
        self.`targetId` = `targetId`
        self.`targetRevision` = `targetRevision`
        self.`path` = `path`
        self.`fileRevision` = `fileRevision`
        self.`side` = `side`
        self.`startLine` = `startLine`
        self.`endLine` = `endLine`
    }
}

public struct WireAnnotationAnchor: Codable, Sendable {
    public let `kind`: WireAnnotationAnchorKind
    public let `workspaceFile`: WireWorkspaceFileAnnotationAnchor?
    public let `workingTreeDiff`: WireWorkingTreeDiffAnnotationAnchor?

    public init(`kind`: WireAnnotationAnchorKind, `workspaceFile`: WireWorkspaceFileAnnotationAnchor?, `workingTreeDiff`: WireWorkingTreeDiffAnnotationAnchor?) {
        self.`kind` = `kind`
        self.`workspaceFile` = `workspaceFile`
        self.`workingTreeDiff` = `workingTreeDiff`
    }
}

public struct WireDiffEndpoint: Codable, Sendable, Equatable {
    public let `kind`: String
    public let `oid`: String?

    public init(`kind`: String, `oid`: String?) {
        self.`kind` = `kind`
        self.`oid` = `oid`
    }
}

public struct WirePinnedDiffTarget: Codable, Sendable, Equatable {
    public let `workspaceId`: String
    public let `kind`: String
    public let `base`: WireDiffEndpoint
    public let `head`: WireDiffEndpoint

    public init(`workspaceId`: String, `kind`: String, `base`: WireDiffEndpoint, `head`: WireDiffEndpoint) {
        self.`workspaceId` = `workspaceId`
        self.`kind` = `kind`
        self.`base` = `base`
        self.`head` = `head`
    }
}

public struct WireAnnotationPreview: Codable, Sendable {
    public let `startLine`: Int
    public let `endLine`: Int
    public let `text`: String
    public let `truncated`: Bool?

    public init(`startLine`: Int, `endLine`: Int, `text`: String, `truncated`: Bool?) {
        self.`startLine` = `startLine`
        self.`endLine` = `endLine`
        self.`text` = `text`
        self.`truncated` = `truncated`
    }
}

public struct WireSubmittedAnnotation: Codable, Sendable {
    public let `originalAnnotationId`: UInt64
    public let `anchor`: WireAnnotationAnchor
    public let `diffTarget`: WirePinnedDiffTarget?
    public let `body`: String
    public let `preview`: WireAnnotationPreview

    public init(`originalAnnotationId`: UInt64, `anchor`: WireAnnotationAnchor, `diffTarget`: WirePinnedDiffTarget?, `body`: String, `preview`: WireAnnotationPreview) {
        self.`originalAnnotationId` = `originalAnnotationId`
        self.`anchor` = `anchor`
        self.`diffTarget` = `diffTarget`
        self.`body` = `body`
        self.`preview` = `preview`
    }
}

public struct WireTranscriptContent: Codable, Sendable {
    public let `kind`: WireTranscriptContentKind
    public let `text`: String?
    public let `toolCallId`: String?
    public let `toolName`: String?
    public let `arguments`: String?
    public let `argumentsTruncated`: Bool?
    public let `filename`: String?
    public let `mediaType`: String?
    public let `attachmentId`: String?
    public let `annotations`: [WireSubmittedAnnotation]?
    /// The invoked command name and its discovery source of a promptCommand block.
    public var `name`: String? = nil
    public var `source`: String? = nil

    /// The "/<name> <arguments>" invocation of a promptCommand block.
    public var promptCommandInvocation: String? {
        guard kind == .value6, let name, !name.isEmpty else { return nil }
        let arguments = (self.arguments ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return "/" + name + (arguments.isEmpty ? "" : " " + arguments)
    }

    public init(`kind`: WireTranscriptContentKind, `text`: String?, `toolCallId`: String?, `toolName`: String?, `arguments`: String?, `argumentsTruncated`: Bool?, `filename`: String?, `mediaType`: String?, `attachmentId`: String?, `annotations`: [WireSubmittedAnnotation]?, `name`: String? = nil, `source`: String? = nil) {
        self.`kind` = `kind`
        self.`text` = `text`
        self.`toolCallId` = `toolCallId`
        self.`toolName` = `toolName`
        self.`arguments` = `arguments`
        self.`argumentsTruncated` = `argumentsTruncated`
        self.`filename` = `filename`
        self.`mediaType` = `mediaType`
        self.`attachmentId` = `attachmentId`
        self.`annotations` = `annotations`
        self.`name` = `name`
        self.`source` = `source`
    }
}

public struct WireTranscriptMessage: Codable, Sendable {
    public let `id`: String
    public let `turnId`: String
    public let `sequence`: Int64
    public let `role`: String
    public let `content`: [WireTranscriptContent]?
    public let `stopReason`: String?
    public let `errorMessage`: String?
    public let `toolCallId`: String?
    public let `toolName`: String?
    public let `boundaryId`: String?
    public let `boundaryKind`: String?
    public let `boundarySource`: String?
    public let `details`: WireJSON?
    public let `isError`: Bool?
    public let `createdAt`: String

    public init(`id`: String, `turnId`: String, `sequence`: Int64, `role`: String, `content`: [WireTranscriptContent]?, `stopReason`: String?, `errorMessage`: String?, `toolCallId`: String?, `toolName`: String?, `boundaryId`: String?, `boundaryKind`: String?, `boundarySource`: String?, `details`: WireJSON?, `isError`: Bool?, `createdAt`: String) {
        self.`id` = `id`
        self.`turnId` = `turnId`
        self.`sequence` = `sequence`
        self.`role` = `role`
        self.`content` = `content`
        self.`stopReason` = `stopReason`
        self.`errorMessage` = `errorMessage`
        self.`toolCallId` = `toolCallId`
        self.`toolName` = `toolName`
        self.`boundaryId` = `boundaryId`
        self.`boundaryKind` = `boundaryKind`
        self.`boundarySource` = `boundarySource`
        self.`details` = `details`
        self.`isError` = `isError`
        self.`createdAt` = `createdAt`
    }
}

public typealias WireProviderRetry = Components.Schemas.ProviderRetry

public typealias WireActiveCompaction = Components.Schemas.ActiveCompaction

public typealias WireSessionUsage = Components.Schemas.SessionUsage

public struct WirePluginFooterStyle: Codable, Sendable, Equatable {
    public let `fg`: String?
    public let `bg`: String?
    public let `bold`: Bool?
    public let `dim`: Bool?
    public let `italic`: Bool?
    public let `underline`: Bool?
    public let `strikethrough`: Bool?

    public init(`fg`: String?, `bg`: String?, `bold`: Bool?, `dim`: Bool?, `italic`: Bool?, `underline`: Bool?, `strikethrough`: Bool?) {
        self.`fg` = `fg`
        self.`bg` = `bg`
        self.`bold` = `bold`
        self.`dim` = `dim`
        self.`italic` = `italic`
        self.`underline` = `underline`
        self.`strikethrough` = `strikethrough`
    }
}

public struct WirePluginFooterSegment: Codable, Sendable, Equatable {
    public let `text`: String
    public let `style`: WirePluginFooterStyle

    public init(`text`: String, `style`: WirePluginFooterStyle) {
        self.`text` = `text`
        self.`style` = `style`
    }
}

public struct WirePluginFooterItem: Codable, Sendable, Equatable {
    public let `id`: String
    public let `pluginId`: String
    public let `instance`: String
    public let `content`: [WirePluginFooterSegment]?

    public init(`id`: String, `pluginId`: String, `instance`: String, `content`: [WirePluginFooterSegment]?) {
        self.`id` = `id`
        self.`pluginId` = `pluginId`
        self.`instance` = `instance`
        self.`content` = `content`
    }
}

public struct WirePluginFooter: Codable, Sendable, Equatable {
    public let `items`: [WirePluginFooterItem]?
    public let `locationHidden`: Bool

    public init(`items`: [WirePluginFooterItem]?, `locationHidden`: Bool) {
        self.`items` = `items`
        self.`locationHidden` = `locationHidden`
    }
}

public struct WirePluginCommand: Codable, Sendable {
    public let `id`: String
    public let `localId`: String
    public let `pluginId`: String
    public let `instance`: String
    public let `description`: String
    public let `argName`: String?
    public let `category`: String?

    public init(`id`: String, `localId`: String, `pluginId`: String, `instance`: String, `description`: String, `argName`: String?, `category`: String?) {
        self.`id` = `id`
        self.`localId` = `localId`
        self.`pluginId` = `pluginId`
        self.`instance` = `instance`
        self.`description` = `description`
        self.`argName` = `argName`
        self.`category` = `category`
    }
}

public struct WirePromptCommand: Codable, Sendable {
    public let `argumentHint`: String?
    public let `name`: String
    public let `description`: String
    public let `source`: String
    public let `location`: String

    public init(`argumentHint`: String?, `name`: String, `description`: String, `source`: String, `location`: String) {
        self.`argumentHint` = `argumentHint`
        self.`name` = `name`
        self.`description` = `description`
        self.`source` = `source`
        self.`location` = `location`
    }
}

public struct WireFollowUpQueue: Codable, Sendable {
    public let `count`: Int
    public let `previews`: [String]?
    public let `annotationIds`: [UInt64]?
    /// Why the first queued message could not start; the queue is held until
    /// a client restores it.
    public var `admissionFailure`: WireFollowUpAdmissionFailure? = nil

    public init(`count`: Int, `previews`: [String]?, `annotationIds`: [UInt64]?, `admissionFailure`: WireFollowUpAdmissionFailure? = nil) {
        self.`count` = `count`
        self.`previews` = `previews`
        self.`annotationIds` = `annotationIds`
        self.`admissionFailure` = `admissionFailure`
    }
}

public struct WireFollowUpAdmissionFailure: Codable, Sendable {
    public let `message`: String

    public init(`message`: String) {
        self.`message` = `message`
    }
}

/// A session's applied model configuration, as session.configuration.changed carries it.
public struct WireSessionConfiguration: Codable, Sendable {
    public let `model`: String
    public let `thinkingLevel`: String
    public let `inputs`: [WireModelInputKind]?
    public let `configurationRevision`: UInt64

    public init(`model`: String, `thinkingLevel`: String, `inputs`: [WireModelInputKind]?, `configurationRevision`: UInt64) {
        self.`model` = `model`
        self.`thinkingLevel` = `thinkingLevel`
        self.`inputs` = `inputs`
        self.`configurationRevision` = `configurationRevision`
    }
}

public typealias WireMCPServerStatus = Components.Schemas.MCPServerStatus

public struct WireSubagentSource: Codable, Sendable, Equatable {
    public let `kind`: String
    public let `path`: String
    public let `pluginId`: String?

    public init(`kind`: String, `path`: String, `pluginId`: String?) {
        self.`kind` = `kind`
        self.`path` = `path`
        self.`pluginId` = `pluginId`
    }
}

public struct WireSubagentDefinition: Codable, Sendable {
    public let `name`: String
    public let `description`: String
    public let `model`: String?
    public let `source`: WireSubagentSource

    public init(`name`: String, `description`: String, `model`: String?, `source`: WireSubagentSource) {
        self.`name` = `name`
        self.`description` = `description`
        self.`model` = `model`
        self.`source` = `source`
    }
}

public struct WireSubagentDiagnostic: Codable, Sendable, Equatable {
    public let `severity`: String
    public let `code`: String
    public let `message`: String
    public let `source`: WireSubagentSource

    public init(`severity`: String, `code`: String, `message`: String, `source`: WireSubagentSource) {
        self.`severity` = `severity`
        self.`code` = `code`
        self.`message` = `message`
        self.`source` = `source`
    }
}

public struct WireSubagentTask: Codable, Sendable {
    public let `id`: String
    public let `sequence`: UInt64
    public let `state`: String
    public let `cancellationGeneration`: UInt64
    public let `queuedAt`: String
    public let `startedAt`: String?
    public let `finishedAt`: String?
    public let `resultSummary`: String?
    public let `error`: String?

    public init(`id`: String, `sequence`: UInt64, `state`: String, `cancellationGeneration`: UInt64, `queuedAt`: String, `startedAt`: String?, `finishedAt`: String?, `resultSummary`: String?, `error`: String?) {
        self.`id` = `id`
        self.`sequence` = `sequence`
        self.`state` = `state`
        self.`cancellationGeneration` = `cancellationGeneration`
        self.`queuedAt` = `queuedAt`
        self.`startedAt` = `startedAt`
        self.`finishedAt` = `finishedAt`
        self.`resultSummary` = `resultSummary`
        self.`error` = `error`
    }
}

public struct WireSubagentConversation: Codable, Sendable {
    public let `id`: String
    public let `agentName`: String
    public let `model`: String
    public let `thinkingLevel`: String
    public let `state`: String
    public let `generation`: UInt64
    public let `activeTaskId`: String?
    public let `queuedTasks`: Int
    public let `lastCompletedTaskId`: String?
    public let `lastResultSummary`: String?
    public let `updatedAt`: String
    public let `tasks`: [WireSubagentTask]?

    public init(`id`: String, `agentName`: String, `model`: String, `thinkingLevel`: String, `state`: String, `generation`: UInt64, `activeTaskId`: String?, `queuedTasks`: Int, `lastCompletedTaskId`: String?, `lastResultSummary`: String?, `updatedAt`: String, `tasks`: [WireSubagentTask]?) {
        self.`id` = `id`
        self.`agentName` = `agentName`
        self.`model` = `model`
        self.`thinkingLevel` = `thinkingLevel`
        self.`state` = `state`
        self.`generation` = `generation`
        self.`activeTaskId` = `activeTaskId`
        self.`queuedTasks` = `queuedTasks`
        self.`lastCompletedTaskId` = `lastCompletedTaskId`
        self.`lastResultSummary` = `lastResultSummary`
        self.`updatedAt` = `updatedAt`
        self.`tasks` = `tasks`
    }
}

public struct WireSubagentMailboxItem: Codable, Sendable {
    public let `id`: String
    public let `kind`: String
    public let `conversationId`: String
    public let `agentName`: String
    public let `state`: String
    public let `summary`: String?
    public let `error`: String?
    public let `createdAt`: String

    public init(`id`: String, `kind`: String, `conversationId`: String, `agentName`: String, `state`: String, `summary`: String?, `error`: String?, `createdAt`: String) {
        self.`id` = `id`
        self.`kind` = `kind`
        self.`conversationId` = `conversationId`
        self.`agentName` = `agentName`
        self.`state` = `state`
        self.`summary` = `summary`
        self.`error` = `error`
        self.`createdAt` = `createdAt`
    }
}

public struct WirePluginInteractionOwner: Codable, Sendable, Equatable {
    public let `pluginId`: String
    public let `instance`: String

    public init(`pluginId`: String, `instance`: String) {
        self.`pluginId` = `pluginId`
        self.`instance` = `instance`
    }
}

public enum WireInteractionKind: String, Codable, Sendable {
    case value0 = "confirm"
    case value1 = "input"
    case value2 = "select"
    case value3 = "guided"
    case value4 = "authorize"
}

public struct WireInteractionOption: Codable, Sendable, Equatable {
    public let `id`: String
    public let `label`: String
    public let `detail`: String?

    public init(`id`: String, `label`: String, `detail`: String?) {
        self.`id` = `id`
        self.`label` = `label`
        self.`detail` = `detail`
    }
}

public enum WireInteractionQuestionKind: String, Codable, Sendable {
    case value0 = "text"
    case value1 = "select"
    case value2 = "multiselect"
    case value3 = "boolean"
}

public struct WireInteractionQuestion: Codable, Sendable, Equatable {
    public let `id`: String
    public let `prompt`: String
    public let `detail`: String?
    public let `kind`: WireInteractionQuestionKind
    public let `required`: Bool
    public let `options`: [WireInteractionOption]?

    public init(`id`: String, `prompt`: String, `detail`: String?, `kind`: WireInteractionQuestionKind, `required`: Bool, `options`: [WireInteractionOption]?) {
        self.`id` = `id`
        self.`prompt` = `prompt`
        self.`detail` = `detail`
        self.`kind` = `kind`
        self.`required` = `required`
        self.`options` = `options`
    }
}

public struct WireInteractionRequest: Codable, Sendable, Equatable {
    public let `plugin`: WirePluginInteractionOwner?
    public let `confirmLabel`: String?
    public let `cancelLabel`: String?
    public let `defaultValue`: Bool?
    public let `placeholder`: String?
    public let `initialValue`: String?
    public let `filterable`: Bool?
    public let `id`: String
    public let `sessionId`: String
    public let `turnId`: String?
    public let `toolCallId`: String?
    public let `kind`: WireInteractionKind
    public let `title`: String
    public let `detail`: String?
    public let `options`: [WireInteractionOption]?
    public let `questions`: [WireInteractionQuestion]?
    public let `url`: String?
    public let `redirectUri`: String?
    public let `createdAt`: String
    public init(plugin: WirePluginInteractionOwner? = nil, confirmLabel: String? = nil, cancelLabel: String? = nil, defaultValue: Bool? = nil, placeholder: String? = nil, initialValue: String? = nil, filterable: Bool? = nil, id: String, sessionId: String, turnId: String? = nil, toolCallId: String? = nil, kind: WireInteractionKind, title: String, detail: String? = nil, options: [WireInteractionOption]? = nil, questions: [WireInteractionQuestion]? = nil, url: String? = nil, redirectUri: String? = nil, createdAt: String) { self.plugin = plugin; self.confirmLabel = confirmLabel; self.cancelLabel = cancelLabel; self.defaultValue = defaultValue; self.placeholder = placeholder; self.initialValue = initialValue; self.filterable = filterable; self.id = id; self.sessionId = sessionId; self.turnId = turnId; self.toolCallId = toolCallId; self.kind = kind; self.title = title; self.detail = detail; self.options = options; self.questions = questions; self.url = url; self.redirectUri = redirectUri; self.createdAt = createdAt }
}

public enum WireAnnotationStaleReason: String, Codable, Sendable {
    case value0 = "workspace_changed"
    case value1 = "target_changed"
    case value2 = "file_changed"
    case value3 = "resource_unavailable"
    case value4 = "validation_deferred"
}

public struct WireAnnotationSummary: Codable, Sendable {
    public let `id`: UInt64
    public let `anchor`: WireAnnotationAnchor
    public let `diffTarget`: WirePinnedDiffTarget?
    public let `bodyPreview`: String
    public let `preview`: String
    public let `stale`: Bool?
    public let `staleReason`: WireAnnotationStaleReason?
    public let `validationDeferred`: Bool?

    public init(`id`: UInt64, `anchor`: WireAnnotationAnchor, `diffTarget`: WirePinnedDiffTarget?, `bodyPreview`: String, `preview`: String, `stale`: Bool?, `staleReason`: WireAnnotationStaleReason?, `validationDeferred`: Bool?) {
        self.`id` = `id`
        self.`anchor` = `anchor`
        self.`diffTarget` = `diffTarget`
        self.`bodyPreview` = `bodyPreview`
        self.`preview` = `preview`
        self.`stale` = `stale`
        self.`staleReason` = `staleReason`
        self.`validationDeferred` = `validationDeferred`
    }
}

public struct WireScratchpad: Codable, Sendable {
    public let `ownerSessionId`: String
    public let `content`: String
    public let `revision`: String
    public let `updatedAt`: String

    public init(`ownerSessionId`: String, `content`: String, `revision`: String, `updatedAt`: String) {
        self.`ownerSessionId` = `ownerSessionId`
        self.`content` = `content`
        self.`revision` = `revision`
        self.`updatedAt` = `updatedAt`
    }
}

public struct WireSessionSnapshot: Codable, Sendable {
    public let `session`: WireSessionInfo
    public let `workspace`: WireWorkspaceRef?
    public let `messages`: [WireTranscriptMessage]?
    public let `previousMessageCursor`: String?
    public let `hasMoreMessages`: Bool?
    public let `activeTurnId`: String?
    public let `providerRetry`: WireProviderRetry?
    public let `activeCompaction`: WireActiveCompaction?
    public let `activeBashExecutionId`: String?
    public let `eventStreamId`: String?
    public let `eventCursor`: Int64?
    public let `eventReplayFrom`: Int64?
    public let `eventReplayAvailable`: Bool?
    public let `contextTokens`: Int?
    public let `contextWindow`: Int?
    public let `usage`: WireSessionUsage
    public let `pluginFooter`: WirePluginFooter?
    public let `pluginCommands`: [WirePluginCommand]?
    public let `promptCommands`: [WirePromptCommand]?
    public let `followUps`: WireFollowUpQueue
    public let `warnings`: [String]?
    public let `mcpServers`: [WireMCPServerStatus]?
    public let `mcpWarnings`: [String]?
    public let `subagentDefinitions`: [WireSubagentDefinition]?
    public let `subagentDiagnostics`: [WireSubagentDiagnostic]?
    public let `subagentConversations`: [WireSubagentConversation]?
    public let `subagentMailbox`: [WireSubagentMailboxItem]?
    public let `pendingInteractions`: [WireInteractionRequest]?
    public let `annotations`: [WireAnnotationSummary]?
    public let `scratchpad`: WireScratchpad?

    public init(`session`: WireSessionInfo, `workspace`: WireWorkspaceRef?, `messages`: [WireTranscriptMessage]?, `previousMessageCursor`: String?, `hasMoreMessages`: Bool?, `activeTurnId`: String?, `providerRetry`: WireProviderRetry?, `activeCompaction`: WireActiveCompaction?, `activeBashExecutionId`: String?, `eventStreamId`: String?, `eventCursor`: Int64?, `eventReplayFrom`: Int64?, `eventReplayAvailable`: Bool?, `contextTokens`: Int?, `contextWindow`: Int?, `usage`: WireSessionUsage, `pluginFooter`: WirePluginFooter?, `pluginCommands`: [WirePluginCommand]?, `promptCommands`: [WirePromptCommand]?, `followUps`: WireFollowUpQueue, `warnings`: [String]?, `mcpServers`: [WireMCPServerStatus]?, `mcpWarnings`: [String]?, `subagentDefinitions`: [WireSubagentDefinition]?, `subagentDiagnostics`: [WireSubagentDiagnostic]?, `subagentConversations`: [WireSubagentConversation]?, `subagentMailbox`: [WireSubagentMailboxItem]?, `pendingInteractions`: [WireInteractionRequest]?, `annotations`: [WireAnnotationSummary]?, `scratchpad`: WireScratchpad?) {
        self.`session` = `session`
        self.`workspace` = `workspace`
        self.`messages` = `messages`
        self.`previousMessageCursor` = `previousMessageCursor`
        self.`hasMoreMessages` = `hasMoreMessages`
        self.`activeTurnId` = `activeTurnId`
        self.`providerRetry` = `providerRetry`
        self.`activeCompaction` = `activeCompaction`
        self.`activeBashExecutionId` = `activeBashExecutionId`
        self.`eventStreamId` = `eventStreamId`
        self.`eventCursor` = `eventCursor`
        self.`eventReplayFrom` = `eventReplayFrom`
        self.`eventReplayAvailable` = `eventReplayAvailable`
        self.`contextTokens` = `contextTokens`
        self.`contextWindow` = `contextWindow`
        self.`usage` = `usage`
        self.`pluginFooter` = `pluginFooter`
        self.`pluginCommands` = `pluginCommands`
        self.`promptCommands` = `promptCommands`
        self.`followUps` = `followUps`
        self.`warnings` = `warnings`
        self.`mcpServers` = `mcpServers`
        self.`mcpWarnings` = `mcpWarnings`
        self.`subagentDefinitions` = `subagentDefinitions`
        self.`subagentDiagnostics` = `subagentDiagnostics`
        self.`subagentConversations` = `subagentConversations`
        self.`subagentMailbox` = `subagentMailbox`
        self.`pendingInteractions` = `pendingInteractions`
        self.`annotations` = `annotations`
        self.`scratchpad` = `scratchpad`
    }
}

public enum WireSessionEventKind: String, Codable, Sendable {
    case value0 = "turn.started"
    case value1 = "user.message.added"
    case value2 = "plugin.message.added"
    case value3 = "assistant.started"
    case value4 = "assistant.text.delta"
    case value5 = "assistant.thinking.delta"
    case value6 = "assistant.completed"
    case value7 = "tool.planned"
    case value8 = "tool.started"
    case value9 = "tool.output.delta"
    case value10 = "tool.completed"
    case value11 = "compaction.started"
    case value12 = "compaction.completed"
    case value13 = "provider.retry.scheduled"
    case value14 = "provider.retry.started"
    case value15 = "context.changed"
    case value16 = "usage.changed"
    case value17 = "turn.completed"
    case value18 = "session.name.changed"
    case value19 = "session.cwd.changed"
    case value20 = "subagent.changed"
    case value21 = "peer_query.changed"
    case value22 = "interaction.requested"
    case value23 = "interaction.resolved"
    case value24 = "annotation.created"
    case value25 = "annotation.updated"
    case value26 = "annotation.deleted"
    case value27 = "annotation.submitted"
    case value28 = "scratchpad.changed"
    case value29 = "boundary.created"
    case value30 = "url.open.requested"
    case value31 = "client_plugin.call.requested"
    case value32 = "client_plugin.call.resolved"
    case value33 = "session.configuration.changed"
    case value34 = "follow_ups.changed"
}

public enum WireTurnStatus: String, Codable, Sendable {
    case value0 = "queued"
    case value1 = "running"
    case value2 = "completed"
    case value3 = "failed"
    case value4 = "aborted"
    case value5 = "interrupted"
}

public enum WireProviderErrorKind: String, Codable, Sendable {
    case value0 = "authentication"
    case value1 = "entitlement"
    case value2 = "usage_limit"
    case value3 = "rate_limit"
    case value4 = "transport"
    case value5 = "protocol"
}

/// Why a context compaction failed. The vocabulary is closed, so an event
/// with a reason this app does not know fails to decode.
public enum WireCompactionFailureReason: String, Codable, Sendable, CaseIterable {
    case summaryTruncated = "summary_truncated"
    case summaryInvalid = "summary_invalid"
    case policyStop = "policy_stop"
    case providerError = "provider_error"
    case notAdaptable = "not_adaptable"
    case canceled = "canceled"
    case conflict = "conflict"
    case internalError = "internal_error"
}

public struct WireAnnotation: Codable, Sendable {
    public let `id`: UInt64
    public let `sessionId`: String
    public let `anchor`: WireAnnotationAnchor
    public let `diffTarget`: WirePinnedDiffTarget?
    public let `body`: String
    public let `preview`: WireAnnotationPreview
    public let `stale`: Bool?
    public let `staleReason`: WireAnnotationStaleReason?
    public let `validationDeferred`: Bool?

    public init(`id`: UInt64, `sessionId`: String, `anchor`: WireAnnotationAnchor, `diffTarget`: WirePinnedDiffTarget?, `body`: String, `preview`: WireAnnotationPreview, `stale`: Bool?, `staleReason`: WireAnnotationStaleReason?, `validationDeferred`: Bool?) {
        self.`id` = `id`
        self.`sessionId` = `sessionId`
        self.`anchor` = `anchor`
        self.`diffTarget` = `diffTarget`
        self.`body` = `body`
        self.`preview` = `preview`
        self.`stale` = `stale`
        self.`staleReason` = `staleReason`
        self.`validationDeferred` = `validationDeferred`
    }
}

public struct WireSessionEvent: Codable, Sendable {
    public let `streamId`: String
    public let `sequence`: Int64
    public let `sessionId`: String
    public let `turnId`: String?
    public let `messageId`: String?
    public let `kind`: WireSessionEventKind
    public let `contentIndex`: Int?
    public let `delta`: String?
    public let `text`: String?
    public let `pluginId`: String?
    public let `thinking`: String?
    public let `toolCallId`: String?
    public let `toolName`: String?
    public let `arguments`: String?
    public let `argumentsTruncated`: Bool?
    public let `content`: [WireTranscriptContent]?
    public let `contentTruncated`: Bool?
    public let `details`: WireJSON?
    public let `detailsOmitted`: Bool?
    public let `isError`: Bool?
    public let `status`: WireTurnStatus?
    public let `errorKind`: WireProviderErrorKind?
    public let `errorMessage`: String?
    public let `compactionId`: String?
    public let `compactionFailureReason`: WireCompactionFailureReason?
    public let `providerRetry`: WireProviderRetry?
    public let `contextTokens`: Int?
    public let `contextWindow`: Int?
    public let `usage`: WireSessionUsage?
    public let `sessionName`: String?
    public let `workspace`: WireWorkspaceRef?
    public let `subagentConversationId`: String?
    public let `subagentTaskId`: String?
    public let `peerRequestId`: String?
    public let `interaction`: WireInteractionRequest?
    public let `interactionId`: String?
    public let `interactionResolution`: String?
    public let `annotationId`: UInt64?
    public let `annotation`: WireAnnotation?
    public let `annotationIds`: [UInt64]?
    public let `acceptedMessageId`: String?
    public let `scratchpad`: WireScratchpad?
    public let `boundaryId`: String?
    public let `boundaryKind`: String?
    public let `boundarySource`: String?
    public let `createdAt`: String?
    public let `url`: String?
    public let `clientPluginCallId`: String?
    public let `clientPluginInstanceId`: String?
    public let `clientPluginCall`: WireClientPluginCall?
    public let `clientPluginCallResolution`: String?
    public let `configuration`: WireSessionConfiguration?
    public let `followUps`: WireFollowUpQueue?

    public init(`streamId`: String, `sequence`: Int64, `sessionId`: String, `turnId`: String?, `messageId`: String?, `kind`: WireSessionEventKind, `contentIndex`: Int?, `delta`: String?, `text`: String?, `pluginId`: String?, `thinking`: String?, `toolCallId`: String?, `toolName`: String?, `arguments`: String?, `argumentsTruncated`: Bool?, `content`: [WireTranscriptContent]?, `contentTruncated`: Bool?, `details`: WireJSON?, `detailsOmitted`: Bool?, `isError`: Bool?, `status`: WireTurnStatus?, `errorKind`: WireProviderErrorKind?, `errorMessage`: String?, `compactionId`: String?, `compactionFailureReason`: WireCompactionFailureReason?, `providerRetry`: WireProviderRetry?, `contextTokens`: Int?, `contextWindow`: Int?, `usage`: WireSessionUsage?, `sessionName`: String?, `workspace`: WireWorkspaceRef?, `subagentConversationId`: String?, `subagentTaskId`: String?, `peerRequestId`: String?, `interaction`: WireInteractionRequest?, `interactionId`: String?, `interactionResolution`: String?, `annotationId`: UInt64?, `annotation`: WireAnnotation?, `annotationIds`: [UInt64]?, `acceptedMessageId`: String?, `scratchpad`: WireScratchpad?, `boundaryId`: String?, `boundaryKind`: String?, `boundarySource`: String?, `createdAt`: String?, `url`: String?, `clientPluginCallId`: String?, `clientPluginInstanceId`: String?, `clientPluginCall`: WireClientPluginCall?, `clientPluginCallResolution`: String?, `configuration`: WireSessionConfiguration?, `followUps`: WireFollowUpQueue?) {
        self.`streamId` = `streamId`
        self.`sequence` = `sequence`
        self.`sessionId` = `sessionId`
        self.`turnId` = `turnId`
        self.`messageId` = `messageId`
        self.`kind` = `kind`
        self.`contentIndex` = `contentIndex`
        self.`delta` = `delta`
        self.`text` = `text`
        self.`pluginId` = `pluginId`
        self.`thinking` = `thinking`
        self.`toolCallId` = `toolCallId`
        self.`toolName` = `toolName`
        self.`arguments` = `arguments`
        self.`argumentsTruncated` = `argumentsTruncated`
        self.`content` = `content`
        self.`contentTruncated` = `contentTruncated`
        self.`details` = `details`
        self.`detailsOmitted` = `detailsOmitted`
        self.`isError` = `isError`
        self.`status` = `status`
        self.`errorKind` = `errorKind`
        self.`errorMessage` = `errorMessage`
        self.`compactionId` = `compactionId`
        self.`compactionFailureReason` = `compactionFailureReason`
        self.`providerRetry` = `providerRetry`
        self.`contextTokens` = `contextTokens`
        self.`contextWindow` = `contextWindow`
        self.`usage` = `usage`
        self.`sessionName` = `sessionName`
        self.`workspace` = `workspace`
        self.`subagentConversationId` = `subagentConversationId`
        self.`subagentTaskId` = `subagentTaskId`
        self.`peerRequestId` = `peerRequestId`
        self.`interaction` = `interaction`
        self.`interactionId` = `interactionId`
        self.`interactionResolution` = `interactionResolution`
        self.`annotationId` = `annotationId`
        self.`annotation` = `annotation`
        self.`annotationIds` = `annotationIds`
        self.`acceptedMessageId` = `acceptedMessageId`
        self.`scratchpad` = `scratchpad`
        self.`boundaryId` = `boundaryId`
        self.`boundaryKind` = `boundaryKind`
        self.`boundarySource` = `boundarySource`
        self.`createdAt` = `createdAt`
        self.`url` = `url`
        self.`clientPluginCallId` = `clientPluginCallId`
        self.`clientPluginInstanceId` = `clientPluginInstanceId`
        self.`clientPluginCall` = `clientPluginCall`
        self.`clientPluginCallResolution` = `clientPluginCallResolution`
        self.`configuration` = `configuration`
        self.`followUps` = `followUps`
    }
}

/// The work a client_plugin.call.requested event asks this app to do
/// (ADR 0051). It is present only on the stream bound to the instance.
public struct WireClientPluginCall: Codable, Sendable {
    public let `pluginId`: String
    public let `toolId`: String
    public let `toolCallId`: String
    public let `turnId`: String?
    public let `subagentConversationId`: String?
    public let `input`: WireJSON

    public init(`pluginId`: String, `toolId`: String, `toolCallId`: String, `turnId`: String?, `subagentConversationId`: String?, `input`: WireJSON) {
        self.`pluginId` = `pluginId`
        self.`toolId` = `toolId`
        self.`toolCallId` = `toolCallId`
        self.`turnId` = `turnId`
        self.`subagentConversationId` = `subagentConversationId`
        self.`input` = `input`
    }
}

public struct WireClientPluginToolDeclaration: Codable, Sendable {
    public let `id`: String
    public let `label`: String?
    public let `description`: String
    public let `inputSchema`: WireJSON
    public let `executionMode`: String?
    public let `promptSnippet`: String?
    public let `promptGuidelines`: [String]?

    public init(`id`: String, `label`: String?, `description`: String, `inputSchema`: WireJSON, `executionMode`: String?, `promptSnippet`: String?, `promptGuidelines`: [String]?) {
        self.`id` = `id`
        self.`label` = `label`
        self.`description` = `description`
        self.`inputSchema` = `inputSchema`
        self.`executionMode` = `executionMode`
        self.`promptSnippet` = `promptSnippet`
        self.`promptGuidelines` = `promptGuidelines`
    }
}

public struct WireAttachClientPluginInput: Codable, Sendable {
    public let `pluginId`: String
    public let `tools`: [WireClientPluginToolDeclaration]

    public init(`pluginId`: String, `tools`: [WireClientPluginToolDeclaration]) {
        self.`pluginId` = `pluginId`
        self.`tools` = `tools`
    }
}

public struct WireClientPluginContent: Codable, Sendable {
    public let `type`: String
    public let `text`: String?
    public let `data`: String?
    public let `mimeType`: String?

    public init(`type`: String, `text`: String?, `data`: String?, `mimeType`: String?) {
        self.`type` = `type`
        self.`text` = `text`
        self.`data` = `data`
        self.`mimeType` = `mimeType`
    }
}

public struct WireCompleteClientPluginCallInput: Codable, Sendable {
    public let `content`: [WireClientPluginContent]?
    public let `details`: WireJSON?
    public let `error`: String?

    public init(`content`: [WireClientPluginContent]?, `details`: WireJSON?, `error`: String?) {
        self.`content` = `content`
        self.`details` = `details`
        self.`error` = `error`
    }
}

public struct WireSessionEventBatch: Codable, Sendable {
    public let `streamId`: String?
    public let `firstSequence`: Int64?
    public let `lastSequence`: Int64?
    public let `resyncRequired`: Bool?
    public let `usageBaseline`: WireSessionUsage?
    public let `events`: [WireSessionEvent]?

    public init(`streamId`: String?, `firstSequence`: Int64?, `lastSequence`: Int64?, `resyncRequired`: Bool?, `usageBaseline`: WireSessionUsage?, `events`: [WireSessionEvent]?) {
        self.`streamId` = `streamId`
        self.`firstSequence` = `firstSequence`
        self.`lastSequence` = `lastSequence`
        self.`resyncRequired` = `resyncRequired`
        self.`usageBaseline` = `usageBaseline`
        self.`events` = `events`
    }
}

public struct WireGitHubPullRequest: Codable, Sendable {
    public let `number`: Int
    public let `url`: String

    public init(`number`: Int, `url`: String) {
        self.`number` = `number`
        self.`url` = `url`
    }
}

public enum WireVCSHeadKind: String, Codable, Sendable {
    case value0 = "branch"
    case value1 = "detached"
    case value2 = "unborn"
}

public struct WireVCSHead: Codable, Sendable {
    public let `kind`: WireVCSHeadKind
    public let `name`: String?
    public let `oid`: String?

    public init(`kind`: WireVCSHeadKind, `name`: String?, `oid`: String?) {
        self.`kind` = `kind`
        self.`name` = `name`
        self.`oid` = `oid`
    }
}

public struct WireVCSStatus: Codable, Sendable {
    public let `pullRequest`: WireGitHubPullRequest?
    public let `root`: String
    public let `head`: WireVCSHead
    public let `dirty`: Bool

    public init(`pullRequest`: WireGitHubPullRequest?, `root`: String, `head`: WireVCSHead, `dirty`: Bool) {
        self.`pullRequest` = `pullRequest`
        self.`root` = `root`
        self.`head` = `head`
        self.`dirty` = `dirty`
    }
}

public struct WireSessionVCSStatus: Codable, Sendable {
    public let `sessionId`: String
    public let `cwd`: String
    public let `status`: WireVCSStatus?

    public init(`sessionId`: String, `cwd`: String, `status`: WireVCSStatus?) {
        self.`sessionId` = `sessionId`
        self.`cwd` = `cwd`
        self.`status` = `status`
    }
}

public struct WireTranscriptPage: Codable, Sendable {
    public let `sessionId`: String
    public let `messages`: [WireTranscriptMessage]?
    public let `previousMessageCursor`: String?
    public let `hasMoreMessages`: Bool?

    public init(`sessionId`: String, `messages`: [WireTranscriptMessage]?, `previousMessageCursor`: String?, `hasMoreMessages`: Bool?) {
        self.`sessionId` = `sessionId`
        self.`messages` = `messages`
        self.`previousMessageCursor` = `previousMessageCursor`
        self.`hasMoreMessages` = `hasMoreMessages`
    }
}

public struct WireSubagentTranscript: Codable, Sendable {
    public let `conversationId`: String
    public let `messages`: [WireTranscriptMessage]?
    public let `previousMessageCursor`: String?
    public let `hasMoreMessages`: Bool?

    public init(`conversationId`: String, `messages`: [WireTranscriptMessage]?, `previousMessageCursor`: String?, `hasMoreMessages`: Bool?) {
        self.`conversationId` = `conversationId`
        self.`messages` = `messages`
        self.`previousMessageCursor` = `previousMessageCursor`
        self.`hasMoreMessages` = `hasMoreMessages`
    }
}

public enum WireSubagentAction: String, Codable, Sendable {
    case value0 = "list_agents"
    case value1 = "start"
    case value2 = "message"
    case value3 = "inspect"
    case value4 = "wait"
    case value5 = "cancel"
    case value6 = "dismiss"
}

public struct WireSubagentOperationInput: Codable, Sendable {
    public let `action`: WireSubagentAction
    public let `agent`: String?
    public let `message`: String?
    public let `conversationId`: String?
    public let `taskId`: String?
    public let `timeoutMs`: Int64?
    public let `generation`: UInt64?

    public init(`action`: WireSubagentAction, `agent`: String?, `message`: String?, `conversationId`: String?, `taskId`: String?, `timeoutMs`: Int64?, `generation`: UInt64?) {
        self.`action` = `action`
        self.`agent` = `agent`
        self.`message` = `message`
        self.`conversationId` = `conversationId`
        self.`taskId` = `taskId`
        self.`timeoutMs` = `timeoutMs`
        self.`generation` = `generation`
    }
}

public struct WireSubagentOperationResult: Codable, Sendable {
    public let `definitions`: [WireSubagentDefinition]?
    public let `diagnostics`: [WireSubagentDiagnostic]?
    public let `conversation`: WireSubagentConversation?
    public let `conversations`: [WireSubagentConversation]?
    public let `task`: WireSubagentTask?
    public let `tasks`: [WireSubagentTask]?
    public let `timedOut`: Bool?
    public let `dismissed`: Bool?
    public let `warning`: String?

    public init(`definitions`: [WireSubagentDefinition]?, `diagnostics`: [WireSubagentDiagnostic]?, `conversation`: WireSubagentConversation?, `conversations`: [WireSubagentConversation]?, `task`: WireSubagentTask?, `tasks`: [WireSubagentTask]?, `timedOut`: Bool?, `dismissed`: Bool?, `warning`: String?) {
        self.`definitions` = `definitions`
        self.`diagnostics` = `diagnostics`
        self.`conversation` = `conversation`
        self.`conversations` = `conversations`
        self.`task` = `task`
        self.`tasks` = `tasks`
        self.`timedOut` = `timedOut`
        self.`dismissed` = `dismissed`
        self.`warning` = `warning`
    }
}

public struct WireSubagentLiveEvent: Codable, Sendable {
    public let `sequence`: Int64
    public let `kind`: String
    public let `turnId`: String?
    public let `messageId`: String?
    public let `contentIndex`: Int?
    public let `delta`: String?
    public let `text`: String?
    public let `toolCallId`: String?
    public let `toolName`: String?
    public let `isError`: Bool?
    public let `boundaryId`: String?
    public let `boundaryKind`: String?
    public let `boundarySource`: String?
    public let `details`: WireJSON?
    public let `createdAt`: String?

    public init(`sequence`: Int64, `kind`: String, `turnId`: String?, `messageId`: String?, `contentIndex`: Int?, `delta`: String?, `text`: String?, `toolCallId`: String?, `toolName`: String?, `isError`: Bool?, `boundaryId`: String?, `boundaryKind`: String?, `boundarySource`: String?, `details`: WireJSON?, `createdAt`: String?) {
        self.`sequence` = `sequence`
        self.`kind` = `kind`
        self.`turnId` = `turnId`
        self.`messageId` = `messageId`
        self.`contentIndex` = `contentIndex`
        self.`delta` = `delta`
        self.`text` = `text`
        self.`toolCallId` = `toolCallId`
        self.`toolName` = `toolName`
        self.`isError` = `isError`
        self.`boundaryId` = `boundaryId`
        self.`boundaryKind` = `boundaryKind`
        self.`boundarySource` = `boundarySource`
        self.`details` = `details`
        self.`createdAt` = `createdAt`
    }
}

public struct WireSubagentLiveEventPage: Codable, Sendable {
    public let `streamId`: String?
    public let `firstSequence`: Int64?
    public let `lastSequence`: Int64?
    public let `resyncRequired`: Bool?
    public let `events`: [WireSubagentLiveEvent]?

    public init(`streamId`: String?, `firstSequence`: Int64?, `lastSequence`: Int64?, `resyncRequired`: Bool?, `events`: [WireSubagentLiveEvent]?) {
        self.`streamId` = `streamId`
        self.`firstSequence` = `firstSequence`
        self.`lastSequence` = `lastSequence`
        self.`resyncRequired` = `resyncRequired`
        self.`events` = `events`
    }
}

public struct WirePromptInput: Codable, Sendable {
    public let `text`: String
    public let `attachmentIds`: [String]?
    public let `annotationIds`: [UInt64]?

    public init(`text`: String, `attachmentIds`: [String]?, `annotationIds`: [UInt64]?) {
        self.`text` = `text`
        self.`attachmentIds` = `attachmentIds`
        self.`annotationIds` = `annotationIds`
    }
}

public struct WirePromptCommandInput: Codable, Sendable {
    public let `name`: String
    public let `args`: String?

    public init(`name`: String, `args`: String?) {
        self.`name` = `name`
        self.`args` = `args`
    }
}

public struct WireBashExecutionInput: Codable, Sendable {
    public let `executionId`: String
    public let `command`: String
    public let `excludeFromContext`: Bool?

    public init(`executionId`: String, `command`: String, `excludeFromContext`: Bool?) {
        self.`executionId` = `executionId`
        self.`command` = `command`
        self.`excludeFromContext` = `excludeFromContext`
    }
}

public enum WireBashExecutionStatus: String, Codable, Sendable {
    case value0 = "running"
    case value1 = "completed"
    case value2 = "failed"
    case value3 = "aborted"
    case value4 = "interrupted"
}

public struct WireBashExecution: Codable, Sendable {
    public let `id`: String
    public let `sessionId`: String
    public let `sequence`: Int64
    public let `command`: String
    public let `status`: WireBashExecutionStatus
    public let `output`: String?
    public let `exitCode`: Int?
    public let `excludeFromContext`: Bool?
    public let `truncated`: Bool?
    public let `timedOut`: Bool?
    public let `errorMessage`: String?
    public let `startedAt`: String
    public let `completedAt`: String?

    public init(`id`: String, `sessionId`: String, `sequence`: Int64, `command`: String, `status`: WireBashExecutionStatus, `output`: String?, `exitCode`: Int?, `excludeFromContext`: Bool?, `truncated`: Bool?, `timedOut`: Bool?, `errorMessage`: String?, `startedAt`: String, `completedAt`: String?) {
        self.`id` = `id`
        self.`sessionId` = `sessionId`
        self.`sequence` = `sequence`
        self.`command` = `command`
        self.`status` = `status`
        self.`output` = `output`
        self.`exitCode` = `exitCode`
        self.`excludeFromContext` = `excludeFromContext`
        self.`truncated` = `truncated`
        self.`timedOut` = `timedOut`
        self.`errorMessage` = `errorMessage`
        self.`startedAt` = `startedAt`
        self.`completedAt` = `completedAt`
    }
}

public struct WireBashHistoryEntry: Codable, Sendable {
    public let `id`: String
    public let `sequence`: Int64
    public let `command`: String
    public let `status`: String
    public let `excludeFromContext`: Bool?
    public let `startedAt`: String
    public let `completedAt`: String?

    public init(`id`: String, `sequence`: Int64, `command`: String, `status`: String, `excludeFromContext`: Bool?, `startedAt`: String, `completedAt`: String?) {
        self.`id` = `id`
        self.`sequence` = `sequence`
        self.`command` = `command`
        self.`status` = `status`
        self.`excludeFromContext` = `excludeFromContext`
        self.`startedAt` = `startedAt`
        self.`completedAt` = `completedAt`
    }
}

public struct WireBashHistoryPage: Codable, Sendable {
    public let `sessionId`: String
    public let `entries`: [WireBashHistoryEntry]?
    public let `nextCursor`: String?
    public let `hasMore`: Bool

    public init(`sessionId`: String, `entries`: [WireBashHistoryEntry]?, `nextCursor`: String?, `hasMore`: Bool) {
        self.`sessionId` = `sessionId`
        self.`entries` = `entries`
        self.`nextCursor` = `nextCursor`
        self.`hasMore` = `hasMore`
    }
}

public struct WireTurnReservation: Codable, Sendable {
    public let `sessionId`: String
    public let `turnId`: String

    public init(`sessionId`: String, `turnId`: String) {
        self.`sessionId` = `sessionId`
        self.`turnId` = `turnId`
    }
}

public struct WirePromptSubmission: Codable, Sendable {
    public let `reservation`: WireTurnReservation?
    public let `queued`: Bool
    public let `queue`: WireFollowUpQueue

    public init(`reservation`: WireTurnReservation?, `queued`: Bool, `queue`: WireFollowUpQueue) {
        self.`reservation` = `reservation`
        self.`queued` = `queued`
        self.`queue` = `queue`
    }
}

public struct WireRestoreFollowUpsResult: Codable, Sendable {
    public let `messages`: [WirePromptInput]?
    public let `queue`: WireFollowUpQueue

    public init(`messages`: [WirePromptInput]?, `queue`: WireFollowUpQueue) {
        self.`messages` = `messages`
        self.`queue` = `queue`
    }
}

public struct WirePromoteFollowUpsResult: Codable, Sendable {
    public let `promoted`: Int
    public let `queue`: WireFollowUpQueue

    public init(`promoted`: Int, `queue`: WireFollowUpQueue) {
        self.`promoted` = `promoted`
        self.`queue` = `queue`
    }
}

public struct WireCreateSessionInput: Codable, Sendable {
    public let `id`: String?
    public let `cwd`: String
    public let `name`: String?
    public let `model`: String
    public let `thinkingLevel`: String?
    public let `temporary`: Bool?

    public init(`id`: String?, `cwd`: String, `name`: String?, `model`: String, `thinkingLevel`: String?, `temporary`: Bool?) {
        self.`id` = `id`
        self.`cwd` = `cwd`
        self.`name` = `name`
        self.`model` = `model`
        self.`thinkingLevel` = `thinkingLevel`
        self.`temporary` = `temporary`
    }
}

public struct WireRenameSessionInput: Codable, Sendable {
    public let `name`: String

    public init(`name`: String) {
        self.`name` = `name`
    }
}

public struct WireChangeCWDInput: Codable, Sendable {
    public let `mutationId`: String
    public let `path`: String

    public init(`mutationId`: String, `path`: String) {
        self.`mutationId` = `mutationId`
        self.`path` = `path`
    }
}

public typealias WireChangeWorkspaceCWDResult = Components.Schemas.ChangeWorkspaceCWDResult

public enum WirePromptSectionKind: String, Codable, Sendable {
    case value0 = "core"
    case value1 = "feature"
    case value2 = "skillCatalog"
    case value3 = "plugin"
    case value4 = "context"
}

public struct WirePromptSource: Codable, Sendable {
    public let `sectionId`: String
    public let `id`: String
    public let `kind`: WirePromptSectionKind
    public let `path`: String?

    public init(`sectionId`: String, `id`: String, `kind`: WirePromptSectionKind, `path`: String?) {
        self.`sectionId` = `sectionId`
        self.`id` = `id`
        self.`kind` = `kind`
        self.`path` = `path`
    }
}

public struct WirePromptDiagnostic: Codable, Sendable {
    public let `severity`: String
    public let `code`: String
    public let `message`: String
    public let `source`: WirePromptSource

    public init(`severity`: String, `code`: String, `message`: String, `source`: WirePromptSource) {
        self.`severity` = `severity`
        self.`code` = `code`
        self.`message` = `message`
        self.`source` = `source`
    }
}

public struct WireReloadSessionResult: Codable, Sendable {
    public let `sessionId`: String
    public let `eventStreamId`: String
    public let `sources`: [WirePromptSource]?
    public let `diagnostics`: [WirePromptDiagnostic]?
    public let `warnings`: [String]?

    public init(`sessionId`: String, `eventStreamId`: String, `sources`: [WirePromptSource]?, `diagnostics`: [WirePromptDiagnostic]?, `warnings`: [String]?) {
        self.`sessionId` = `sessionId`
        self.`eventStreamId` = `eventStreamId`
        self.`sources` = `sources`
        self.`diagnostics` = `diagnostics`
        self.`warnings` = `warnings`
    }
}

public struct WireCompactSessionInput: Codable, Sendable {
    public let `operationId`: String

    public init(`operationId`: String) {
        self.`operationId` = `operationId`
    }
}

public struct WireCompactSessionResult: Codable, Sendable {
    public let `operationId`: String
    public let `compacted`: Bool
    public let `checkpointId`: String?
    public let `eventStreamId`: String

    public init(`operationId`: String, `compacted`: Bool, `checkpointId`: String?, `eventStreamId`: String) {
        self.`operationId` = `operationId`
        self.`compacted` = `compacted`
        self.`checkpointId` = `checkpointId`
        self.`eventStreamId` = `eventStreamId`
    }
}

public struct WireForkSessionInput: Codable, Sendable {
    public let `name`: String?
    public let `prompt`: WirePromptInput?

    public init(`name`: String?, `prompt`: WirePromptInput?) {
        self.`name` = `name`
        self.`prompt` = `prompt`
    }
}

public typealias WireForkSessionResult = Components.Schemas.ForkSessionResult

public enum WireThinkingLevel: String, Codable, Sendable {
    case value0 = "off"
    case value1 = "minimal"
    case value2 = "low"
    case value3 = "medium"
    case value4 = "high"
    case value5 = "xhigh"
    case value6 = "max"
}

public struct WireModelCapability: Codable, Sendable {
    public let `id`: String
    public let `name`: String
    public let `provider`: String
    public let `api`: String
    public let `contextWindow`: Int
    public let `maxInputTokens`: Int?
    public let `maxOutputTokens`: Int?
    public let `thinkingLevels`: [WireThinkingLevel]?
    public let `inputs`: [WireModelInputKind]?
    public let `available`: Bool

    public init(`id`: String, `name`: String, `provider`: String, `api`: String, `contextWindow`: Int, `maxInputTokens`: Int?, `maxOutputTokens`: Int?, `thinkingLevels`: [WireThinkingLevel]?, `inputs`: [WireModelInputKind]?, `available`: Bool) {
        self.`id` = `id`
        self.`name` = `name`
        self.`provider` = `provider`
        self.`api` = `api`
        self.`contextWindow` = `contextWindow`
        self.`maxInputTokens` = `maxInputTokens`
        self.`maxOutputTokens` = `maxOutputTokens`
        self.`thinkingLevels` = `thinkingLevels`
        self.`inputs` = `inputs`
        self.`available` = `available`
    }
}

public struct WireModelCatalog: Codable, Sendable {
    public let `models`: [WireModelCapability]?

    public init(`models`: [WireModelCapability]?) {
        self.`models` = `models`
    }
}

public struct WireConfigureSessionInput: Codable, Sendable {
    public let `expectedRevision`: UInt64
    public let `model`: String
    public let `thinkingLevel`: WireThinkingLevel?

    public init(`expectedRevision`: UInt64, `model`: String, `thinkingLevel`: WireThinkingLevel?) {
        self.`expectedRevision` = `expectedRevision`
        self.`model` = `model`
        self.`thinkingLevel` = `thinkingLevel`
    }
}

public typealias WireConfigureSessionResult = Components.Schemas.ConfigureSessionResult

public struct WireFileIndexEntry: Codable, Sendable {
    public let `path`: String
    public let `isDir`: Bool?

    public init(`path`: String, `isDir`: Bool?) {
        self.`path` = `path`
        self.`isDir` = `isDir`
    }
}

public struct WireSessionFileIndex: Codable, Sendable {
    public let `sessionId`: String
    public let `cwd`: String
    public let `entries`: [WireFileIndexEntry]?
    public let `truncated`: Bool

    public init(`sessionId`: String, `cwd`: String, `entries`: [WireFileIndexEntry]?, `truncated`: Bool) {
        self.`sessionId` = `sessionId`
        self.`cwd` = `cwd`
        self.`entries` = `entries`
        self.`truncated` = `truncated`
    }
}

public struct WireReadWorkspaceFileInput: Codable, Sendable {
    public let `workspaceId`: String
    public let `path`: String
    public let `expectedFileRevision`: String?

    public init(`workspaceId`: String, `path`: String, `expectedFileRevision`: String?) {
        self.`workspaceId` = `workspaceId`
        self.`path` = `path`
        self.`expectedFileRevision` = `expectedFileRevision`
    }
}

public typealias WireWorkspaceFileRead = Components.Schemas.WorkspaceFileRead

public struct WireAttachmentInfo: Codable, Sendable {
    public let `id`: String
    public let `sessionId`: String
    public let `filename`: String
    public let `mediaType`: String
    public let `size`: Int64
    public let `sha256`: String
    public let `createdAt`: String
    public let `width`: Int?
    public let `height`: Int?

    public init(`id`: String, `sessionId`: String, `filename`: String, `mediaType`: String, `size`: Int64, `sha256`: String, `createdAt`: String, `width`: Int?, `height`: Int?) {
        self.`id` = `id`
        self.`sessionId` = `sessionId`
        self.`filename` = `filename`
        self.`mediaType` = `mediaType`
        self.`size` = `size`
        self.`sha256` = `sha256`
        self.`createdAt` = `createdAt`
        self.`width` = `width`
        self.`height` = `height`
    }
}

public struct WireAttachmentResolutionInput: Codable, Sendable {
    public let `attachmentIds`: [String]?

    public init(`attachmentIds`: [String]?) {
        self.`attachmentIds` = `attachmentIds`
    }
}

public struct WireAttachmentResolution: Codable, Sendable {
    public let `attachments`: [WireAttachmentInfo]?
    public let `missingAttachmentIds`: [String]?

    public init(`attachments`: [WireAttachmentInfo]?, `missingAttachmentIds`: [String]?) {
        self.`attachments` = `attachments`
        self.`missingAttachmentIds` = `missingAttachmentIds`
    }
}

public struct WireInteractionAnswer: Codable, Sendable {
    public let `text`: String?
    public let `boolean`: Bool?
    public let `optionIds`: [String]?
    public let `skipped`: Bool?

    public init(`text`: String?, `boolean`: Bool?, `optionIds`: [String]?, `skipped`: Bool?) {
        self.`text` = `text`
        self.`boolean` = `boolean`
        self.`optionIds` = `optionIds`
        self.`skipped` = `skipped`
    }
}

public struct WireInteractionResponse: Codable, Sendable {
    public let `requestId`: String
    public let `cancelled`: Bool?
    public let `confirmed`: Bool?
    public let `value`: String?
    public let `selectedOptionId`: String?
    public let `answers`: [String: WireInteractionAnswer]?

    public init(`requestId`: String, `cancelled`: Bool?, `confirmed`: Bool?, `value`: String?, `selectedOptionId`: String?, `answers`: [String: WireInteractionAnswer]?) {
        self.`requestId` = `requestId`
        self.`cancelled` = `cancelled`
        self.`confirmed` = `confirmed`
        self.`value` = `value`
        self.`selectedOptionId` = `selectedOptionId`
        self.`answers` = `answers`
    }
}

public struct WireCreateAnnotationInput: Codable, Sendable {
    public let `anchor`: WireAnnotationAnchor
    public let `body`: String

    public init(`anchor`: WireAnnotationAnchor, `body`: String) {
        self.`anchor` = `anchor`
        self.`body` = `body`
    }
}

public struct WireUpdateAnnotationInput: Codable, Sendable {
    public let `annotationId`: UInt64
    public let `body`: String

    public init(`annotationId`: UInt64, `body`: String) {
        self.`annotationId` = `annotationId`
        self.`body` = `body`
    }
}

public struct WireAnnotationPage: Codable, Sendable {
    public let `sessionId`: String
    public let `entries`: [WireAnnotation]?
    public let `nextCursor`: String?

    public init(`sessionId`: String, `entries`: [WireAnnotation]?, `nextCursor`: String?) {
        self.`sessionId` = `sessionId`
        self.`entries` = `entries`
        self.`nextCursor` = `nextCursor`
    }
}

public struct WireDiffTargetMetadata: Codable, Sendable {
    public let `label`: String
    public let `refName`: String?
    public let `baseRefName`: String?
    public let `subject`: String?
    public let `abbreviatedOid`: String?
    public let `committedAt`: Int64?

    public init(`label`: String, `refName`: String?, `baseRefName`: String?, `subject`: String?, `abbreviatedOid`: String?, `committedAt`: Int64?) {
        self.`label` = `label`
        self.`refName` = `refName`
        self.`baseRefName` = `baseRefName`
        self.`subject` = `subject`
        self.`abbreviatedOid` = `abbreviatedOid`
        self.`committedAt` = `committedAt`
    }
}

public struct WireDiffTargetEntry: Codable, Sendable {
    public let `reference`: String
    public let `targetId`: String
    public let `kind`: String
    public let `base`: WireDiffEndpoint
    public let `head`: WireDiffEndpoint
    public let `metadata`: WireDiffTargetMetadata
    public let `annotationCount`: Int?

    public init(`reference`: String, `targetId`: String, `kind`: String, `base`: WireDiffEndpoint, `head`: WireDiffEndpoint, `metadata`: WireDiffTargetMetadata, `annotationCount`: Int?) {
        self.`reference` = `reference`
        self.`targetId` = `targetId`
        self.`kind` = `kind`
        self.`base` = `base`
        self.`head` = `head`
        self.`metadata` = `metadata`
        self.`annotationCount` = `annotationCount`
    }
}

public struct WireDiffTargetDiagnostic: Codable, Sendable {
    public let `reason`: String
    public let `count`: Int

    public init(`reason`: String, `count`: Int) {
        self.`reason` = `reason`
        self.`count` = `count`
    }
}

public struct WireDiffTargetCatalog: Codable, Sendable {
    public let `sessionId`: String
    public let `workspaceId`: String
    public let `targets`: [WireDiffTargetEntry]?
    public let `diagnostics`: [WireDiffTargetDiagnostic]?

    public init(`sessionId`: String, `workspaceId`: String, `targets`: [WireDiffTargetEntry]?, `diagnostics`: [WireDiffTargetDiagnostic]?) {
        self.`sessionId` = `sessionId`
        self.`workspaceId` = `workspaceId`
        self.`targets` = `targets`
        self.`diagnostics` = `diagnostics`
    }
}

public struct WireObserveDiffInput: Codable, Sendable {
    public let `workspaceId`: String
    public let `targetReference`: String
    public let `expectedTargetId`: String
    public let `expectedTargetRevision`: String?
    public let `pageSize`: Int?
    public let `cursor`: String?

    public init(`workspaceId`: String, `targetReference`: String, `expectedTargetId`: String, `expectedTargetRevision`: String?, `pageSize`: Int?, `cursor`: String?) {
        self.`workspaceId` = `workspaceId`
        self.`targetReference` = `targetReference`
        self.`expectedTargetId` = `expectedTargetId`
        self.`expectedTargetRevision` = `expectedTargetRevision`
        self.`pageSize` = `pageSize`
        self.`cursor` = `cursor`
    }
}

public struct WireDiffTarget: Codable, Sendable {
    public let `id`: String
    public let `workspaceId`: String
    public let `kind`: String
    public let `repositoryPath`: String
    public let `base`: WireDiffEndpoint?
    public let `head`: WireDiffEndpoint?

    public init(`id`: String, `workspaceId`: String, `kind`: String, `repositoryPath`: String, `base`: WireDiffEndpoint?, `head`: WireDiffEndpoint?) {
        self.`id` = `id`
        self.`workspaceId` = `workspaceId`
        self.`kind` = `kind`
        self.`repositoryPath` = `repositoryPath`
        self.`base` = `base`
        self.`head` = `head`
    }
}

public struct WireDiffHead: Codable, Sendable {
    public let `state`: String
    public let `oid`: String?

    public init(`state`: String, `oid`: String?) {
        self.`state` = `state`
        self.`oid` = `oid`
    }
}

public struct WireDiffTruncation: Codable, Sendable {
    public let `reason`: String
    public let `count`: Int

    public init(`reason`: String, `count`: Int) {
        self.`reason` = `reason`
        self.`count` = `count`
    }
}

public struct WireDiffOmission: Codable, Sendable {
    public let `reason`: String
    public let `count`: Int

    public init(`reason`: String, `count`: Int) {
        self.`reason` = `reason`
        self.`count` = `count`
    }
}

public struct WireDiffObservation: Codable, Sendable {
    public let `sessionId`: String
    public let `target`: WireDiffTarget
    public let `revision`: String
    public let `head`: WireDiffHead
    public let `indexSummary`: String
    public let `complete`: Bool
    public let `truncation`: WireDiffTruncation?
    public let `omissions`: [WireDiffOmission]?

    public init(`sessionId`: String, `target`: WireDiffTarget, `revision`: String, `head`: WireDiffHead, `indexSummary`: String, `complete`: Bool, `truncation`: WireDiffTruncation?, `omissions`: [WireDiffOmission]?) {
        self.`sessionId` = `sessionId`
        self.`target` = `target`
        self.`revision` = `revision`
        self.`head` = `head`
        self.`indexSummary` = `indexSummary`
        self.`complete` = `complete`
        self.`truncation` = `truncation`
        self.`omissions` = `omissions`
    }
}

public struct WireDiffSide: Codable, Sendable {
    public let `kind`: String
    public let `mode`: UInt32

    public init(`kind`: String, `mode`: UInt32) {
        self.`kind` = `kind`
        self.`mode` = `mode`
    }
}

public struct WireDiffFileSummary: Codable, Sendable {
    public let `path`: String
    public let `fileRevision`: String?
    public let `change`: String
    public let `old`: WireDiffSide
    public let `new`: WireDiffSide
    public let `contentState`: String
    public let `reason`: String?
    public let `additions`: Int?
    public let `deletions`: Int?

    public init(`path`: String, `fileRevision`: String?, `change`: String, `old`: WireDiffSide, `new`: WireDiffSide, `contentState`: String, `reason`: String?, `additions`: Int?, `deletions`: Int?) {
        self.`path` = `path`
        self.`fileRevision` = `fileRevision`
        self.`change` = `change`
        self.`old` = `old`
        self.`new` = `new`
        self.`contentState` = `contentState`
        self.`reason` = `reason`
        self.`additions` = `additions`
        self.`deletions` = `deletions`
    }
}

public struct WireWorkingTreePage: Codable, Sendable {
    public let `observation`: WireDiffObservation
    public let `files`: [WireDiffFileSummary]?
    public let `nextCursor`: String?

    public init(`observation`: WireDiffObservation, `files`: [WireDiffFileSummary]?, `nextCursor`: String?) {
        self.`observation` = `observation`
        self.`files` = `files`
        self.`nextCursor` = `nextCursor`
    }
}

public struct WireReadFileDiffInput: Codable, Sendable {
    public let `targetId`: String
    public let `targetRevision`: String
    public let `path`: String
    public let `expectedFileRevision`: String?
    public let `annotationId`: UInt64?
    public let `pageSize`: Int?
    public let `maxHunks`: Int?
    public let `cursor`: String?

    public init(`targetId`: String, `targetRevision`: String, `path`: String, `expectedFileRevision`: String?, `annotationId`: UInt64?, `pageSize`: Int?, `maxHunks`: Int?, `cursor`: String?) {
        self.`targetId` = `targetId`
        self.`targetRevision` = `targetRevision`
        self.`path` = `path`
        self.`expectedFileRevision` = `expectedFileRevision`
        self.`annotationId` = `annotationId`
        self.`pageSize` = `pageSize`
        self.`maxHunks` = `maxHunks`
        self.`cursor` = `cursor`
    }
}

public struct WireDiffComputation: Codable, Sendable {
    public let `state`: String
    public let `reason`: String?

    public init(`state`: String, `reason`: String?) {
        self.`state` = `state`
        self.`reason` = `reason`
    }
}

public struct WireDiffLine: Codable, Sendable {
    public let `kind`: String
    public let `oldLine`: Int?
    public let `newLine`: Int?
    public let `content`: String
    public let `hasTerminatingLF`: Bool

    public init(`kind`: String, `oldLine`: Int?, `newLine`: Int?, `content`: String, `hasTerminatingLF`: Bool) {
        self.`kind` = `kind`
        self.`oldLine` = `oldLine`
        self.`newLine` = `newLine`
        self.`content` = `content`
        self.`hasTerminatingLF` = `hasTerminatingLF`
    }
}

public struct WireDiffHunk: Codable, Sendable {
    public let `oldStart`: Int
    public let `oldCount`: Int
    public let `newStart`: Int
    public let `newCount`: Int
    public let `continuedBefore`: Bool
    public let `continuedAfter`: Bool
    public let `lines`: [WireDiffLine]?

    public init(`oldStart`: Int, `oldCount`: Int, `newStart`: Int, `newCount`: Int, `continuedBefore`: Bool, `continuedAfter`: Bool, `lines`: [WireDiffLine]?) {
        self.`oldStart` = `oldStart`
        self.`oldCount` = `oldCount`
        self.`newStart` = `newStart`
        self.`newCount` = `newCount`
        self.`continuedBefore` = `continuedBefore`
        self.`continuedAfter` = `continuedAfter`
        self.`lines` = `lines`
    }
}

public struct WireFileDiffPage: Codable, Sendable {
    public let `observation`: WireDiffObservation
    public let `file`: WireDiffFileSummary
    public let `computation`: WireDiffComputation
    public let `hunks`: [WireDiffHunk]?
    public let `nextCursor`: String?

    public init(`observation`: WireDiffObservation, `file`: WireDiffFileSummary, `computation`: WireDiffComputation, `hunks`: [WireDiffHunk]?, `nextCursor`: String?) {
        self.`observation` = `observation`
        self.`file` = `file`
        self.`computation` = `computation`
        self.`hunks` = `hunks`
        self.`nextCursor` = `nextCursor`
    }
}

public enum WireDiffErrorCode: String, Codable, Sendable {
    case value0 = "invalid_path"
    case value1 = "not_repository"
    case value2 = "unsupported_repository"
    case value3 = "stale_workspace"
    case value4 = "stale_target"
    case value5 = "stale_file"
    case value6 = "stale_cursor"
    case value7 = "not_found"
    case value8 = "permission_denied"
    case value9 = "limit_exceeded"
    case value10 = "capacity_exceeded"
    case value11 = "repository_unavailable"
    case value12 = "unavailable"
}

public let kitWireVersion = Operations.ListSessions.Input.Headers.XKitProtocolVersionPayload.allCases[0].rawValue

