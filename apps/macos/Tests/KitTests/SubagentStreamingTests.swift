import Foundation
import Testing
@testable import Kit

struct SubagentStreamingTests {
    private func page(_ events: [[String: Any]], stream: String = "stream", first: Int = 1, last: Int? = nil, resync: Bool = false) throws -> WireSubagentLiveEventPage {
        try JSONDecoder().decode(WireSubagentLiveEventPage.self, from: JSONSerialization.data(withJSONObject:
            ["streamId": stream, "firstSequence": first, "lastSequence": last ?? events.last?["sequence"] as? Int ?? 0,
             "resyncRequired": resync, "events": events]))
    }
    private func event(_ sequence: Int, _ kind: String, turn: String = "turn", fields: [String: Any] = [:]) -> [String: Any] {
        var result: [String: Any] = ["sequence": sequence, "kind": kind, "turnId": turn]
        result.merge(fields) { _, value in value }
        return result
    }
    private func history(_ messages: [[String: Any]] = []) throws -> WireSubagentTranscript {
        try JSONDecoder().decode(WireSubagentTranscript.self, from: JSONSerialization.data(withJSONObject:
            ["conversationId": "child", "messages": messages]))
    }
    private func message(_ id: String, turn: String = "turn", sequence: Int = 1, role: String = "assistant", content: [[String: Any]], call: String? = nil) -> [String: Any] {
        var value: [String: Any] = ["id": id, "turnId": turn, "sequence": sequence, "role": role,
            "content": content, "createdAt": "", "stopReason": "endTurn"]
        if let call { value["toolCallId"] = call; value["toolName"] = "bash" }
        return value
    }

    @Test func thinkingAndToolsUpdateBeforeTurnSettlesAndReplayDoesNotDuplicate() throws {
        var state = SubagentStreamProjection()
        let events = [event(1, "turn.started"),
            event(2, "message.thinking.delta", fields: ["messageId": "m", "delta": "Checking ownership"]),
            event(3, "tool.started", fields: ["toolCallId": "call", "toolName": "bash"]),
            event(4, "tool.updated", fields: ["toolCallId": "call", "toolName": "bash", "text": "first"])]
        #expect(try state.accept(page(events)))
        let running = try state.project(history())
        #expect(running.messages.map(\.role) == ["tools"])
        #expect(running.messages[0].tools[0].output == "first")
        #expect(running.messages[0].tools[0].thinking == "Checking ownership")
        #expect(running.messages[0].tools[0].status == "Running…")
        #expect(running.activity == "Working…")
        #expect(try state.accept(page(events)))
        #expect(try state.project(history()).messages == running.messages)
        #expect(try state.accept(page([event(5, "tool.updated", fields: ["toolCallId": "call", "toolName": "bash", "text": " second"])])))
        #expect(try state.project(history()).messages[0].tools[0].output == "first second")
        #expect(try state.accept(page([event(6, "tool.completed", fields: ["toolCallId": "call", "toolName": "bash", "text": "final"]), event(7, "turn.settled") ])))
        let final = try state.project(history())
        #expect(final.messages[0].tools[0].output == "final")
        #expect(final.messages[0].tools[0].status == "Completed")
        #expect(final.activity == nil)
    }

    @Test func proseIsBufferedAndDurableCompletionWins() throws {
        var state = SubagentStreamProjection()
        #expect(try state.accept(page([event(1, "message.text.delta", fields: ["messageId": "m", "delta": "Hello"]) ])))
        #expect(try state.project(history()).messages.isEmpty)
        #expect(try state.accept(page([event(2, "message.completed", fields: ["messageId": "m"])])))
        #expect(try state.project(history()).messages.map(\.text) == ["Hello"])
        let stored = try history([message("m", content: [["kind": "text", "text": "Hello world"]])])
        #expect(try state.project(stored).messages.map(\.text) == ["Hello world"])
    }

    @Test func repeatedToolIDsInDifferentTurnsRemainIndependent() throws {
        var state = SubagentStreamProjection()
        let stored = try history([
            message("a", content: [["kind": "toolCall", "toolCallId": "call", "toolName": "bash", "arguments": "{}"]]),
            message("b", sequence: 2, role: "tool", content: [["kind": "text", "text": "old"]], call: "call")])
        #expect(try state.accept(page([event(1, "tool.started", turn: "next", fields: ["toolCallId": "call", "toolName": "bash"]),
            event(2, "tool.updated", turn: "next", fields: ["toolCallId": "call", "toolName": "bash", "text": "new"]) ])))
        let result = try state.project(stored)
        #expect(result.messages.count == 2)
        #expect(result.messages.flatMap(\.tools).map(\.output) == ["old", "new"])
        #expect(Set(result.messages.flatMap(\.tools).map(\.id)).count == 2)
    }

    @Test func cursorResetsOnGapsReplacementAndExpiredRetention() throws {
        var state = SubagentStreamProjection()
        #expect(try state.accept(page([event(10, "turn.started")], first: 10)))
        #expect(state.cursor == 10)
        #expect(try state.accept(page([event(12, "turn.started")], first: 10)) == false)
        #expect(state.cursor == 0)
        #expect(try state.accept(page([event(12, "turn.started")], first: 12)))
        #expect(try state.accept(page([], stream: "replacement", first: 1, last: 2, resync: true)) == false)
        #expect(state.stream == "")
        #expect(try state.accept(page([event(1, "turn.started")], stream: "replacement")))
    }
}
