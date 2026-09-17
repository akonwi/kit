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
    var usage: WireSessionUsage?
    var compactionID: String?
    var contextTokens: Int?
    var contextWindow: Int?

    init(_ event: WireSessionEvent) {
        kind = event.kind.rawValue; turnId = event.turnId; runId = event.runId
        messageId = event.messageId; contentIndex = event.contentIndex
        delta = event.delta; text = event.text; thinking = event.thinking
        toolCallId = event.toolCallId; toolName = event.toolName; arguments = event.arguments
        contentTruncated = event.contentTruncated
        content = event.content; isError = event.isError; sessionName = event.sessionName
        providerRetry = event.providerRetry; errorMessage = event.errorMessage
        compactionID = event.compactionId
        usage = event.usage
        contextTokens = event.contextTokens; contextWindow = event.contextWindow
        interaction = event.interaction; interactionId = event.interactionId
    }

    init?(_ event: WireSubagentLiveEvent) {
        switch event.kind {
        case "message.text.delta": kind = "assistant.text.delta"
        case "message.thinking.delta": kind = "assistant.thinking.delta"
        case "message.completed":
            guard event.messageId != nil else { return nil }
            kind = "assistant.completed"
        case "turn.started", "execution.started": kind = "run.started"
        case "turn.settled", "execution.settled": kind = "run.finished"
        case "tool.planned", "tool.started", "tool.updated", "tool.completed": kind = event.kind
        default: return nil
        }
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

    static func toolID(turn: String, call: String) -> String { "\(turn.utf8.count):\(turn)\(call)" }
}
