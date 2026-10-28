import Foundation

/// Renderer-facing events shared by the main SSE stream and child event pages.
struct TranscriptEvent {
    let kind: String
    let turnId: String
    var runId: String = ""
    var messageId: String?
    var contentIndex: Int?
    var delta: String?
    var text: String?
    var pluginId: String?
    var thinking: String?
    var toolCallId: String?
    var toolName: String?
    var arguments: String?
    var contentTruncated: Bool?
    var content: [WireTranscriptContent]?
    var isError: Bool?
    var sessionName: String?
    var interaction: WireInteractionRequest?
    var interactionId: String?
    var providerRetry: WireProviderRetry?
    var errorMessage: String?
    /// How a finished turn ended, such as "failed" or "aborted".
    var status: String?
    var usage: WireSessionUsage?
    var compactionID: String?
    /// Why a compaction failed, on its compaction.completed and on the
    /// turn.completed of a turn it failed.
    var compactionFailureReason: WireCompactionFailureReason?
    var contextTokens: Int?
    var contextWindow: Int?
    var annotation: WireAnnotation?
    var annotationId: UInt64?
    var annotationIds: [UInt64]?
    var scratchpad: WireScratchpad?
    /// A tool result's structured details as JSON.
    var details: String?
    /// A boundary record's identity, kind, source, structured details, and
    /// creation time; set only for boundary.created.
    var boundaryId: String?
    var boundaryKind: String?
    var boundarySource: String?
    var boundaryDetails: WireJSON?
    var createdAt: String?
    /// When the app received a new event; nil for replayed events, whose time
    /// says nothing about when the work happened.
    var receivedAt: Date?

    init(_ event: WireSessionEvent) {
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

    init?(_ event: WireSubagentLiveEvent) {
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
    var boundaryRecord: WireTranscriptMessage? {
        guard kind == "boundary.created", let id = boundaryId, !id.isEmpty,
              let boundaryKind, !boundaryKind.isEmpty, let createdAt else { return nil }
        return WireTranscriptMessage(id: id, turnId: "", sequence: 0, role: "context", content: content ?? [],
            stopReason: nil, errorMessage: nil, toolCallId: nil, toolName: nil, boundaryId: id,
            boundaryKind: boundaryKind, boundarySource: boundarySource, details: boundaryDetails, isError: nil, createdAt: createdAt)
    }

    static func toolID(turn: String, call: String) -> String { "\(turn.utf8.count):\(turn)\(call)" }
}
