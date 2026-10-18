import Foundation
import Testing
@testable import Kit

struct SessionProjectionTests {
    private func message(_ id: String, turn: String = "turn", role: String = "assistant",
                         content: [[String: Any]] = [], callID: String? = nil, boundary: String? = nil,
                         source: String? = nil, boundaryID: String? = nil,
                         details: [String: Any]? = nil) throws -> WireTranscriptMessage {
        var value: [String: Any] = ["id": id, "turnId": turn, "sequence": 1, "role": role,
                                    "content": content, "createdAt": ""]
        if let callID { value["toolCallId"] = callID }
        if let boundary { value["boundaryKind"] = boundary }
        if let source { value["boundarySource"] = source }
        if let boundaryID { value["boundaryId"] = boundaryID }
        if let details { value["details"] = details }
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
    @Test func promptCommandMessagesShowTheirInvocation() throws {
        let rows = try SessionProjection.transcript([
            message("command", role: "user", content: [["kind": "promptCommand", "name": "claude-fix", "arguments": "123 high",
                "source": "claude_project", "text": "Pretend to fix issue #123 at high priority."]]),
            message("bare", turn: "next", role: "user", content: [["kind": "promptCommand", "name": "claude-ping",
                "source": "claude_project", "text": "Reply with pong."]])
        ])
        #expect(rows.map(\.role) == ["user", "user"])
        #expect(rows.map(\.text) == ["/claude-fix 123 high", "/claude-ping"])
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

    private func framed(_ kind: String, from source: String, _ text: String) -> [[String: Any]] {
        [["kind": "text", "text": "[\(kind) from \(source)]"], ["kind": "text", "text": text]]
    }

    @Test func contextTheUserCausedBecomesMarkersAndModelContextHasNoRow() throws {
        let fork = "session_" + String(repeating: "a", count: 32)
        let rows = try SessionProjection.transcript([
            message("user", role: "user", content: [["kind": "text", "text": "Review this"]]),
            message("call", content: [call("review")]),
            message("child", role: "context", content: framed("subagent_result", from: "subagent", "Subagent result"),
                    boundary: "subagent_result", source: "subagent", boundaryID: "mailbox_1"),
            message("request", role: "context", content: framed("subagent_request_result", from: "subagent", "Reply"),
                    boundary: "subagent_request_result", source: "subagent", boundaryID: "mailbox_2"),
            message("query", role: "context", content: [["kind": "text", "text": "Peer question"]], boundary: "peer_query", boundaryID: "peer_1"),
            message("result", role: "context", content: [["kind": "text", "text": "Peer reply"]], boundary: "peer_result", boundaryID: "peer_2"),
            message("pr", role: "context", content: framed("pull_request_status", from: "github", "PR #12: checks passed"),
                    boundary: "pull_request_status", source: "github", boundaryID: "pr_1"),
            message("future", role: "context", content: [["kind": "text", "text": "Something new"]], boundary: "future_kind", boundaryID: "future_1"),
            message("compact", role: "context", content: [["kind": "text", "text": "[context summary]\n## Goal\nShip it."]],
                    boundary: "summary", source: "compaction"),
            message("forked", role: "context",
                    content: framed("session_forked", from: "session-manager", "This session was forked into session \(fork)."),
                    boundary: "session_forked", source: "session-manager", boundaryID: "session-fork:" + fork),
            message("moved", role: "context", content: framed("cwd", from: "user", "The user changed this session's working directory."),
                    boundary: "cwd", source: "user", boundaryID: "cwd:mutation_1",
                    details: ["version": 1, "previousCwd": "/repo", "cwd": "/repo-terminal"]),
            message("reply", content: [["kind": "text", "text": "Review complete"]])
        ])
        #expect(rows.map(\.id) == ["user", "tools-review", "compact", "forked", "moved", "reply"])
        #expect(rows.map(\.role) == ["user", "tools", "marker", "marker", "marker", "assistant"])
        #expect(rows.map(\.marker) == [nil, nil, .compaction(summary: "## Goal\nShip it."), .fork(sessionID: fork),
                                       .workingDirectory(path: "/repo-terminal", previous: "/repo"), nil])
    }

    @MainActor @Test func transcriptTableRowsKeepMarkersAndInboxMessages() {
        let marker = TranscriptMessage(id: "moved", role: "marker", text: "", tools: [],
                                       marker: .workingDirectory(path: "/repo-terminal", previous: "/repo"))
        let inbox = SubagentInboxMessage(kind: .reply, from: "tester", text: "Yes.", failed: false)
        let reply = TranscriptMessage(id: "answered", role: "inbox", text: "Yes.", tools: [], inbox: inbox)
        #expect(NativeTranscriptCoordinator.row(from: marker) == TranscriptMessage(id: "message:moved", role: "marker", text: "", tools: [],
                                                                                 marker: .workingDirectory(path: "/repo-terminal", previous: "/repo")))
        #expect(NativeTranscriptCoordinator.row(from: reply) == TranscriptMessage(id: "message:answered", role: "inbox", text: "Yes.", tools: [], inbox: inbox))
    }

    @Test func messagesFromAnotherSubagentNameTheSenderWithoutTheModelInstructions() throws {
        let request = "Subagent request subrequest_1 from reviewer:\nIs shell exit covered?\nUse subagent_reply with this receipt to answer."
        let rows = try SessionProjection.transcript([
            message("asked", turn: "t1", role: "context", content: framed("subagent_inbox", from: "subagent", request),
                    boundary: "subagent_inbox", source: "subagent", boundaryID: "inbox_task_1",
                    details: ["version": 1, "receipt": "subrequest_1", "kind": "request", "from": "reviewer", "message": "Is shell exit covered?"]),
            message("answered", turn: "t2", role: "context", content: framed("subagent_inbox", from: "subagent", "Subagent reply…"),
                    boundary: "subagent_inbox", source: "subagent", boundaryID: "inbox_task_2",
                    details: ["version": 1, "receipt": "subrequest_2", "kind": "reply", "from": "tester", "message": "Yes."]),
            message("failed", turn: "t3", role: "context", content: framed("subagent_inbox", from: "subagent", "Subagent request subrequest_3 failed: request expired"),
                    boundary: "subagent_inbox", source: "subagent", boundaryID: "inbox_task_3",
                    details: ["version": 1, "receipt": "subrequest_3", "kind": "reply", "from": "tester", "failure": "request expired"]),
            message("older", turn: "t4", role: "context", content: framed("subagent_inbox", from: "subagent", "Subagent reply to subrequest_4 from tester:\nDone."),
                    boundary: "subagent_inbox", source: "subagent", boundaryID: "inbox_task_4",
                    details: ["version": 1, "receipt": "subrequest_4", "kind": "reply"])
        ])
        #expect(rows.map(\.role) == ["inbox", "inbox", "inbox", "inbox"])
        #expect(rows.map(\.inbox) == [
            SubagentInboxMessage(kind: .request, from: "reviewer", text: "Is shell exit covered?", failed: false),
            SubagentInboxMessage(kind: .reply, from: "tester", text: "Yes.", failed: false),
            SubagentInboxMessage(kind: .reply, from: "tester", text: "request expired", failed: true),
            SubagentInboxMessage(kind: .reply, from: nil, text: "Subagent reply to subrequest_4 from tester:\nDone.", failed: false)
        ])
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
