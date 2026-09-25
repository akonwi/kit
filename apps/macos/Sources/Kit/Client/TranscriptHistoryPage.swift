import Foundation

/// Server-selected, complete-turn history preceding an exclusive cursor.
struct TranscriptHistoryPage: Sendable {
    let sessionID: String
    let messages: [TranscriptMessage]
    let previousCursor: String?
}

protocol TranscriptPagingClient: SessionClient {
    func history(_ id: String, before: String) async throws -> TranscriptHistoryPage
}

extension TranscriptHistoryPage {
    init(_ page: WireTranscriptPage, sessionID: String, before: String) throws {
        guard page.sessionId == sessionID, let boundary = UInt64(before), boundary > 0 else {
            throw ClientError.invalidPayload
        }
        let source = page.messages ?? []
        var previous: Int64 = -1
        var ids = Set<String>()
        var closedTurns = Set<String>()
        var turn: String?
        var calls: [String: String] = [:]
        var results = Set<String>()
        for message in source {
            guard !message.id.isEmpty, ids.insert(message.id).inserted,
                  message.sequence > previous, message.sequence >= 0,
                  UInt64(message.sequence) < boundary else { throw ClientError.invalidPayload }
            if message.role != "bash", message.turnId != turn {
                guard !message.turnId.isEmpty, !closedTurns.contains(message.turnId) else { throw ClientError.invalidPayload }
                if let turn { closedTurns.insert(turn) }
                turn = message.turnId
                calls = [:]; results = []
            }
            if message.role == "assistant" {
                for block in message.content ?? [] where block.kind.rawValue == "toolCall" {
                    guard let id = block.toolCallId, let name = block.toolName,
                          calls.updateValue(name, forKey: id) == nil else { throw ClientError.invalidPayload }
                }
            }
            if message.role == "tool" {
                guard let id = message.toolCallId, let name = message.toolName,
                      calls[id] == name, results.insert(id).inserted else { throw ClientError.invalidPayload }
            }
            previous = message.sequence
        }
        self.sessionID = sessionID
        messages = try SessionProjection.transcript(source)
        previousCursor = try Self.cursor(page.previousMessageCursor, hasMore: page.hasMoreMessages,
                                         firstSequence: source.first?.sequence)
        if let previousCursor {
            guard let value = UInt64(previousCursor), value < boundary else { throw ClientError.invalidPayload }
        }
    }

    static func cursor(_ cursor: String?, hasMore: Bool?, firstSequence: Int64?) throws -> String? {
        if hasMore == true {
            guard let cursor, let number = UInt64(cursor), number > 0,
                  let firstSequence, firstSequence >= 0, number == UInt64(firstSequence) else {
                throw ClientError.invalidPayload
            }
            return cursor
        }
        guard cursor == nil || cursor == "" else { throw ClientError.invalidPayload }
        return nil
    }
}
