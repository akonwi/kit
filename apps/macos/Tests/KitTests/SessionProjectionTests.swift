import Foundation
import Testing
@testable import Kit

struct SessionProjectionTests {
    private func message(_ id: String, turn: String = "turn", role: String = "assistant",
                         content: [[String: Any]] = [], callID: String? = nil, boundary: String? = nil,
                         source: String? = nil) throws -> WireTranscriptMessage {
        var value: [String: Any] = ["id": id, "turnId": turn, "sequence": 1, "role": role,
                                    "content": content, "createdAt": ""]
        if let callID { value["toolCallId"] = callID }
        if let boundary { value["boundaryKind"] = boundary }
        if let source { value["boundarySource"] = source }
        return try JSONDecoder().decode(WireTranscriptMessage.self, from: JSONSerialization.data(withJSONObject: value))
    }
    private func call(_ id: String) -> [String: Any] {
        ["kind": "toolCall", "toolCallId": id, "toolName": "read", "arguments": "{}"]
    }
    @Test func sessionInputsProjectWhenPresent() throws {
        func info(_ inputs: [String]?) throws -> WireSessionInfo {
            var value: [String: Any] = ["id": "session_test", "cwd": "/repo", "model": "test/echo", "thinkingLevel": "off",
                                        "configurationRevision": 1, "createdAt": "", "updatedAt": ""]
            if let inputs { value["inputs"] = inputs }
            return try JSONDecoder().decode(WireSessionInfo.self, from: JSONSerialization.data(withJSONObject: value))
        }
        #expect(try SessionProjection.summary(info(["text"])).inputs == ["text"])
        #expect(try SessionProjection.summary(info(["text", "image"])).inputs == ["text", "image"])
        #expect(try SessionProjection.summary(info(nil)).inputs == nil)
    }
    @Test func consecutiveCallsShareSummaryInCallOrder() throws {
        let rows = try SessionProjection.transcript([
            message("a", content: [call("one"), call("two")]),
            message("r2", role: "tool", content: [["kind": "text", "text": "Second"]], callID: "two"),
            message("r1", role: "tool", content: [["kind": "text", "text": "First"]], callID: "one"),
            message("b", content: [call("three")]),
            message("final", content: [["kind": "text", "text": "Done"]])
        ])
        #expect(rows.map(\.role) == ["tools", "assistant"])
        #expect(rows[0].id == "tools-one")
        #expect(rows[0].tools.map(\.id) == ["one", "two", "three"])
        #expect(rows[0].tools.map(\.output) == ["First", "Second", ""])
        #expect(rows[0].tools[0].arguments == "{}")
        #expect(rows[1].text == "Done")
    }
    @Test func textAndTurnBoundariesPreserveOrder() throws {
        let rows = try SessionProjection.transcript([
            message("a", content: [call("one"), ["kind": "text", "text": "Next step"], call("two")]),
            message("b", turn: "next", content: [call("three")]),
            message("user", turn: "next", role: "user", content: [["kind": "text", "text": "Continue"]]),
            message("c", turn: "next", content: [call("four")])
        ])
        #expect(rows.map(\.role) == ["tools", "assistant", "tools", "tools", "user", "tools"])
        #expect(rows.map { $0.tools.map(\.id) } == [["one"], [], ["two"], ["three"], [], ["four"]])
        #expect(rows[1].text == "Next step")
        #expect(rows[4].text == "Continue")
    }
    @Test func retainedResultWithoutCallHasSummary() throws {
        let rows = try SessionProjection.transcript([
            message("result", role: "tool", content: [["kind": "text", "text": "Retained output"]], callID: "old")
        ])
        #expect(rows.map(\.role) == ["tools"])
        #expect(rows[0].tools[0].output == "Retained output")
    }
    @Test func historicalThinkingBelongsToFollowingTool() throws {
        let rows = try SessionProjection.transcript([
            message("a", content: [["kind": "thinking", "text": "Inspect first"], call("one"),
                                   ["kind": "thinking", "text": "Inspect next"], call("two")])
        ])
        #expect(rows.map(\.role) == ["tools"])
        #expect(rows[0].tools.compactMap(\.thinking) == ["Inspect first", "Inspect next"])
    }

    @Test func agentContextRecordsLeaveOnlyUserFacingTranscriptRows() throws {
        let rows = try SessionProjection.transcript([
            message("user", role: "user", content: [["kind": "text", "text": "Review this"]]),
            message("call", content: [call("review")]),
            message("child", role: "context", content: [["kind": "text", "text": "Subagent result"]], boundary: "subagent_result"),
            message("query", role: "context", content: [["kind": "text", "text": "Peer question"]], boundary: "peer_query"),
            message("result", role: "context", content: [["kind": "text", "text": "Peer reply"]], boundary: "peer_result"),
            message("compact", role: "context", content: [["kind": "text", "text": "Compaction summary"]], boundary: "compaction"),
            message("reply", content: [["kind": "text", "text": "Review complete"]])
        ])
        #expect(rows.map(\.id) == ["user", "tools-review", "compact", "reply"])
        #expect(rows.map(\.text) == ["Review this", "", "Compaction summary", "Review complete"])
    }

    @Test func pluginMessagesProjectAsAttributedPluginRows() throws {
        let rows = try SessionProjection.transcript([
            message("pluginmsg_a", turn: "turn_plugin", role: "context",
                    content: [["kind": "text", "text": "Continue the autoresearch loop.\n\nLog each result."]],
                    boundary: "plugin_message", source: "autoresearch"),
            message("call", turn: "turn_plugin", content: [call("read")]),
            message("reply", turn: "turn_plugin", content: [["kind": "text", "text": "Experiment 3 kept."]])
        ])
        #expect(rows.map(\.id) == ["pluginmsg_a", "tools-read", "reply"])
        #expect(rows.map(\.role) == ["plugin", "tools", "assistant"])
        #expect(rows[0].text == "Continue the autoresearch loop.\n\nLog each result.")
        #expect(rows[0].plugin == PluginMessageOrigin(pluginID: "autoresearch", turnID: "turn_plugin"))
        #expect(PluginMessageRow.summary(rows[0].text) == "Continue the autoresearch loop.")
        #expect(PluginMessageRow.summary("\n## Next step\n\nRead `autoresearch.md` first.") == "Next step")
        #expect(PluginMessageRow.summary("Read `autoresearch.md` and **log** it.") == "Read autoresearch.md and log it.")
        #expect(throws: ClientError.self) {
            try SessionProjection.transcript([message("pluginmsg_b", role: "context",
                content: [["kind": "text", "text": "Unattributed"]], boundary: "plugin_message")])
        }
    }

}
