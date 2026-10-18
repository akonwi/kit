import Foundation

/// Context Kit added to a conversation that the user should see as an event
/// in the timeline rather than as a message.
enum TranscriptMarker: Decodable, Sendable, Equatable {
    /// Earlier context was replaced by this summary.
    case compaction(summary: String)
    /// The session was forked into the session with this ID.
    case fork(sessionID: String)
    /// The user changed the session's working directory.
    case workingDirectory(path: String, previous: String?)
}

/// A request or reply that another subagent sent to this one.
struct SubagentInboxMessage: Decodable, Sendable, Equatable {
    enum Kind: String, Decodable, Sendable { case request, reply }
    let kind: Kind
    /// The other agent, when the server named it.
    let from: String?
    /// The message, or why the request failed.
    let text: String
    let failed: Bool
}

extension SessionProjection {
    /// The row for a context message other than a composer command or a
    /// plugin message, or nil when the context is only for the model. The
    /// row's ID is the boundary ID, so a record keeps its row from creation.
    static func contextRow(_ message: WireTranscriptMessage) -> TranscriptMessage? {
        guard let kind = message.boundaryKind else { return nil }
        let content = boundaryContent(message.content ?? [], kind: kind, source: message.boundarySource)
        // A boundary keeps one row from creation, before and after it joins a
        // turn's history under a new message ID.
        let id = message.boundaryId.flatMap { $0.isEmpty ? nil : $0 } ?? message.id
        let details = message.details.flatMap { try? JSONEncoder().encode($0) }
        switch kind {
        case "summary":
            var summary = visibleText(content)
            if summary.hasPrefix(compactionPrefix) { summary.removeFirst(compactionPrefix.count) }
            summary = summary.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !summary.isEmpty else { return nil }
            return TranscriptMessage(id: id, role: "marker", text: "", tools: [], marker: .compaction(summary: summary))
        case "session_forked":
            guard let id = message.boundaryId, id.hasPrefix(forkPrefix) else { return nil }
            let session = String(id.dropFirst(forkPrefix.count))
            guard PeerSessionPresentation.validSessionID(session) else { return nil }
            return TranscriptMessage(id: id, role: "marker", text: "", tools: [], marker: .fork(sessionID: session))
        case "cwd":
            struct Details: Decodable { let version: Int; let previousCwd: String?; let cwd: String }
            guard let details, let value = try? JSONDecoder().decode(Details.self, from: details),
                  value.version == 1, !value.cwd.isEmpty else { return nil }
            return TranscriptMessage(id: id, role: "marker", text: "", tools: [],
                                     marker: .workingDirectory(path: value.cwd, previous: value.previousCwd))
        case "subagent_inbox":
            struct Details: Decodable {
                let version: Int
                let kind: String
                let from: String?
                let message: String?
                let failure: String?
            }
            guard let details, let value = try? JSONDecoder().decode(Details.self, from: details),
                  value.version == 1, let inboxKind = SubagentInboxMessage.Kind(rawValue: value.kind) else { return nil }
            let from = value.from.flatMap { $0.isEmpty ? nil : $0 }
            let inbox: SubagentInboxMessage
            if let failure = value.failure, !failure.isEmpty {
                inbox = .init(kind: inboxKind, from: from, text: failure, failed: true)
            } else if let body = value.message, !body.isEmpty {
                inbox = .init(kind: inboxKind, from: from, text: body, failed: false)
            } else {
                // Older deliveries carry only the model-facing text.
                let text = visibleText(content)
                guard !text.isEmpty else { return nil }
                inbox = .init(kind: inboxKind, from: from, text: text, failed: false)
            }
            return TranscriptMessage(id: id, role: "inbox", text: inbox.text, tools: [], inbox: inbox)
        default:
            return nil
        }
    }

    /// Content without the model-facing "[kind from source]" line that leads
    /// a boundary delivered into the conversation.
    static func boundaryContent(_ content: [WireTranscriptContent], kind: String, source: String?) -> [WireTranscriptContent] {
        var framing = "[" + kind
        if let source, !source.isEmpty { framing += " from " + source }
        framing += "]"
        guard let first = content.first, first.kind.rawValue == "text", first.text == framing else { return content }
        return Array(content.dropFirst())
    }

    private static let compactionPrefix = "[context summary]"
    private static let forkPrefix = "session-fork:"
}
