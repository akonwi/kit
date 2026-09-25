import Foundation

/// Child pages are replayable, runtime-local evidence over durable conversation history.
struct SubagentStreamProjection {
    private(set) var stream = ""
    private(set) var cursor: Int64 = 0
    private var events: [WireSubagentLiveEvent] = []
    private var bytes = 0
    private var projection: SessionEventProjection?
    private var appliedCursor: Int64 = 0

    /// False requests a fresh baseline; an expired or replaced journal must never be spliced in.
    mutating func accept(_ page: WireSubagentLiveEventPage) throws -> Bool {
        let first = page.firstSequence ?? 0, last = page.lastSequence ?? 0
        let incoming = page.events ?? []
        guard first >= 0, last >= first, (first == 0) == (last == 0), incoming.count <= 128,
              first == 0 || !(page.streamId ?? "").isEmpty else { throw ClientError.invalidPayload }
        if page.resyncRequired == true || (!stream.isEmpty && page.streamId != stream) {
            guard page.resyncRequired != true || incoming.isEmpty else { throw ClientError.invalidPayload }
            self = Self()
            return false
        }
        var previous: Int64?
        for event in incoming {
            guard event.sequence >= first, event.sequence <= last, event.sequence > 0,
                  previous == nil || event.sequence == previous! + 1,
                  (event.contentIndex ?? 0) >= 0,
                  (event.delta?.utf8.count ?? 0) + (event.text?.utf8.count ?? 0) <= 16 * 1024 else { throw ClientError.invalidPayload }
            switch event.kind {
            case "message.text.delta", "message.thinking.delta":
                guard !(event.messageId ?? "").isEmpty, !(event.delta ?? "").isEmpty,
                      event.toolCallId == nil, event.toolName == nil, event.text == nil else { throw ClientError.invalidPayload }
            case "tool.planned", "tool.started", "tool.updated", "tool.completed":
                guard !(event.toolCallId ?? "").isEmpty, !(event.toolName ?? "").isEmpty,
                      event.delta == nil else { throw ClientError.invalidPayload }
            default:
                guard event.kind.contains("."), event.delta == nil, event.text == nil,
                      event.toolCallId == nil, event.toolName == nil else { throw ClientError.invalidPayload }
            }
            previous = event.sequence
            guard event.sequence > cursor else { continue }
            guard cursor == 0 || event.sequence == cursor + 1 else { self = Self(); return false }
            events.append(event)
            bytes += Self.size(event)
            guard bytes <= 8 * 1024 * 1024 else { throw ClientError.oversized }
            cursor = event.sequence
        }
        guard last == 0 || cursor == last else { self = Self(); return false }
        stream = page.streamId ?? ""
        return true
    }

    mutating func project(_ transcript: WireSubagentTranscript, refresh: Bool = true) throws -> SubagentTranscriptUpdate {
        let raw = transcript.messages ?? []
        if refresh || projection == nil {
            guard !transcript.conversationId.isEmpty, raw.count <= 1000,
                  zip(raw, raw.dropFirst()).allSatisfy({ $0.sequence < $1.sequence }) else { throw ClientError.invalidPayload }
            let source = raw.map(Self.scoped)
            let excerpt = SessionExcerpt(id: transcript.conversationId, title: "", sourceTitle: "Kit server",
                model: "", thinking: "off", workspace: "", date: "", messages: try SessionProjection.transcript(source))
            projection = try SessionEventProjection(session: excerpt, source: source)
            appliedCursor = 0
        }
        for event in events where event.sequence > appliedCursor {
            if let normalized = TranscriptEvent(event) { try projection?.apply(normalized) }
            appliedCursor = event.sequence
        }
        // Completed turns are authoritative in durable history. Retain only the active tail.
        let durableTurns = Set(raw.filter { $0.role == "assistant" && $0.stopReason != nil }.map(\.turnId))
        if let settled = events.lastIndex(where: {
            ["turn.settled", "execution.settled"].contains($0.kind) && durableTurns.contains($0.turnId ?? "")
        }) {
            events.removeFirst(settled + 1)
            bytes = events.reduce(0) { $0 + Self.size($1) }
        }
        return SubagentTranscriptUpdate(messages: projection!.session.messages, activity: projection!.session.activity)
    }

    private static func size(_ event: WireSubagentLiveEvent) -> Int {
        [event.kind, event.turnId, event.messageId, event.toolCallId, event.toolName, event.delta, event.text]
            .reduce(128) { $0 + ($1?.utf8.count ?? 0) }
    }

    private static func scoped(_ message: WireTranscriptMessage) -> WireTranscriptMessage {
        func id(_ call: String?) -> String? { call.map { TranscriptEvent.toolID(turn: message.turnId, call: $0) } }
        let content = message.content?.map {
            WireTranscriptContent(kind: $0.kind, text: $0.text, toolCallId: id($0.toolCallId), toolName: $0.toolName,
                arguments: $0.arguments, argumentsTruncated: $0.argumentsTruncated, filename: $0.filename, mediaType: $0.mediaType, attachmentId: $0.attachmentId, annotations: $0.annotations)
        }
        return WireTranscriptMessage(id: message.id, turnId: message.turnId, sequence: message.sequence,
            role: message.role, content: content, bash: message.bash, stopReason: message.stopReason,
            errorMessage: message.errorMessage, toolCallId: id(message.toolCallId), toolName: message.toolName,
            boundaryId: message.boundaryId, boundaryKind: message.boundaryKind, boundarySource: message.boundarySource,
            details: message.details, isError: message.isError, createdAt: message.createdAt)
    }
}
