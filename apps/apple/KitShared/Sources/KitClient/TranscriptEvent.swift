import Foundation

/// Renderer-facing events shared by the main SSE stream and child event pages.
public struct TranscriptEvent {
    public let kind: String
    public let turnId: String
    public var runId: String = ""
    public var messageId: String?
    public var contentIndex: Int?
    public var delta: String?
    public var text: String?
    public var pluginId: String?
    public var thinking: String?
    public var toolCallId: String?
    public var toolName: String?
    public var arguments: String?
    public var contentTruncated: Bool?
    public var content: [WireTranscriptContent]?
    public var isError: Bool?
    public var sessionName: String?
    public var interaction: WireInteractionRequest?
    public var interactionId: String?
    public var providerRetry: WireProviderRetry?
    public var errorMessage: String?
    /// How a finished turn ended, such as "failed" or "aborted".
    public var status: String?
    public var usage: WireSessionUsage?
    public var compactionID: String?
    /// Why a compaction failed, on its compaction.completed and on the
    /// turn.completed of a turn it failed.
    public var compactionFailureReason: WireCompactionFailureReason?
    public var contextTokens: Int?
    public var contextWindow: Int?
    public var annotation: WireAnnotation?
    public var annotationId: UInt64?
    public var annotationIds: [UInt64]?
    public var scratchpad: WireScratchpad?
    /// A tool result's structured details as JSON.
    public var details: String?
    /// A boundary record's identity, kind, source, structured details, and
    /// creation time; set only for boundary.created.
    public var boundaryId: String?
    public var boundaryKind: String?
    public var boundarySource: String?
    public var boundaryDetails: WireJSON?
    public var createdAt: String?
    /// When the app received a new event; nil for replayed events, whose time
    /// says nothing about when the work happened.
    public var receivedAt: Date?

    public init(_ event: WireSessionEvent) {
        annotation = event.annotation; annotationId = event.annotationId; annotationIds = event.annotationIds
        scratchpad = event.scratchpad
        kind = event.kind.rawValue; turnId = event.turnId ?? ""; runId = event.turnId ?? ""
        messageId = event.messageId; contentIndex = event.contentIndex
        delta = event.delta; text = event.text; pluginId = event.pluginId; thinking = event.thinking
        toolCallId = event.toolCallId; toolName = event.toolName; arguments = event.arguments
        contentTruncated = event.contentTruncated
        content = event.content; isError = event.isError; sessionName = event.sessionName
        details = SessionProjection.json(event.details)
        providerRetry = event.providerRetry; errorMessage = event.errorMessage; status = event.status?.rawValue
        compactionID = event.compactionId; compactionFailureReason = event.compactionFailureReason
        usage = event.usage
        contextTokens = event.contextTokens; contextWindow = event.contextWindow
        interaction = event.interaction; interactionId = event.interactionId
        boundaryId = event.boundaryId; boundaryKind = event.boundaryKind; boundarySource = event.boundarySource
        boundaryDetails = event.kind == .value29 ? event.details : nil; createdAt = event.createdAt
    }

    public init?(_ event: WireSubagentLiveEvent) {
        switch event.kind {
        case "message.text.delta": kind = "assistant.text.delta"
        case "message.thinking.delta": kind = "assistant.thinking.delta"
        case "message.completed":
            guard event.messageId != nil else { return nil }
            kind = "assistant.completed"
        case "turn.started", "execution.started": kind = "turn.started"
        case "turn.settled", "execution.settled": kind = "turn.completed"
        case "tool.planned", "tool.started", "tool.output.delta", "tool.completed", "boundary.created": kind = event.kind
        default: return nil
        }
        boundaryId = event.boundaryId; boundaryKind = event.boundaryKind; boundarySource = event.boundarySource
        boundaryDetails = event.details; createdAt = event.createdAt
        turnId = event.turnId ?? ""
        runId = turnId
        messageId = event.messageId; contentIndex = event.contentIndex; delta = event.delta
        toolCallId = event.toolCallId.map { Self.toolID(turn: turnId, call: $0) }
        toolName = event.toolName; isError = event.isError
        if let output = event.text {
            content = [WireTranscriptContent(kind: .value0, text: output, toolCallId: nil, toolName: nil,
                arguments: nil, argumentsTruncated: nil, filename: nil, mediaType: nil, attachmentId: nil, annotations: nil)]
        }
    }

    /// The context record a boundary.created event carries, shaped as the
    /// snapshot lists a boundary that has not joined a completed turn.
    public var boundaryRecord: WireTranscriptMessage? {
        guard kind == "boundary.created", let id = boundaryId, !id.isEmpty,
              let boundaryKind, !boundaryKind.isEmpty, let createdAt else { return nil }
        return WireTranscriptMessage(id: id, turnId: "", sequence: 0, role: "context", content: content ?? [],
            stopReason: nil, errorMessage: nil, toolCallId: nil, toolName: nil, boundaryId: id,
            boundaryKind: boundaryKind, boundarySource: boundarySource, details: boundaryDetails, isError: nil, createdAt: createdAt)
    }

    public static func toolID(turn: String, call: String) -> String { "\(turn.utf8.count):\(turn)\(call)" }
}
