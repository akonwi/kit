import Foundation
import Testing
@testable import Kit

struct SessionEventProjectionTests {
    private func snapshot() throws -> WireSessionSnapshot {
        let json = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0}}"#
        return try JSONDecoder().decode(WireSessionSnapshot.self, from: Data(json.utf8))
    }
    private func event(_ kind: String, _ fields: [String: Any] = [:]) throws -> WireSessionEvent {
        var json: [String: Any] = ["streamId": "stream", "sequence": 1, "sessionId": "s", "turnId": "t", "kind": kind]
        json.merge(fields) { _, new in new }
        return try JSONDecoder().decode(WireSessionEvent.self, from: JSONSerialization.data(withJSONObject: json))
    }
    @Test func pluginDialogsSurviveModelRunBoundariesAndAreAnswerableWhileIdle() throws {
        var state = try SessionEventProjection(snapshot())
        let request: [String: Any] = ["id": "interaction_plugin", "sessionId": "s", "plugin": ["pluginId": "demo", "instance": "host:1"],
            "kind": "input", "title": "Plugin note", "initialValue": "hello", "createdAt": "2026-09-21T00:00:00Z"]
        try state.apply(event("interaction.requested", ["turnId": "", "interaction": request]))
        #expect(state.session.pendingInteractions?.map(\.id) == ["interaction_plugin"])
        #expect(state.session.tabStatus == .awaitingResponse)
        try state.apply(event("turn.started"))
        #expect(state.session.pendingInteractions?.map(\.id) == ["interaction_plugin"])
        try state.apply(event("turn.completed"))
        #expect(state.session.pendingInteractions?.map(\.id) == ["interaction_plugin"])
        #expect(state.session.tabStatus == .awaitingResponse)
        try state.apply(event("interaction.resolved", ["turnId": "", "interactionId": "interaction_plugin"]))
        #expect(state.session.pendingInteractions?.count == 0)
        #expect(state.session.tabStatus == .idle)
    }

    @Test func livePluginMessageAppearsOnceAndDefersToThePersistedRow() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("turn.started", ["status": "running"]))
        let added = try event("plugin.message.added", ["pluginId": "autoresearch", "text": "Continue the autoresearch loop."])
        try state.apply(added)
        try state.apply(added)
        #expect(state.session.messages.map(\.id) == ["live-plugin-t"])
        #expect(state.session.messages.map(\.role) == ["plugin"])
        #expect(state.session.messages.first?.text == "Continue the autoresearch loop.")
        #expect(state.session.messages.first?.plugin == PluginMessageOrigin(pluginID: "autoresearch", turnID: "t"))
        #expect(throws: ClientError.self) { try state.apply(event("plugin.message.added", ["text": "Unattributed"])) }

        var json = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(snapshot())) as? [String: Any])
        json["activeTurnId"] = "t"
        json["messages"] = [["id": "pluginmsg_a", "turnId": "t", "sequence": 1, "role": "context", "createdAt": "",
                             "boundaryId": "pluginmsg_a", "boundaryKind": "plugin_message", "boundarySource": "autoresearch",
                             "details": ["version": 1, "pluginId": "autoresearch"],
                             "content": [["kind": "text", "text": "Continue the autoresearch loop."]]]]
        var refreshed = try SessionEventProjection(JSONDecoder().decode(WireSessionSnapshot.self,
            from: JSONSerialization.data(withJSONObject: json)))
        try refreshed.apply(added)
        #expect(refreshed.session.messages.map(\.id) == ["pluginmsg_a"])
        #expect(refreshed.session.messages.first?.plugin == PluginMessageOrigin(pluginID: "autoresearch", turnID: "t"))
    }

    @Test func cumulativeUsageReplacesTotalsWithoutDependingOnLoadedMessages() throws {
        var state = try SessionEventProjection(snapshot())
        #expect(state.session.usage?.formattedCost == "$0.00")
        let usage: [String: Any] = ["input": 1200, "output": 340, "cacheRead": 5000,
            "cacheWrite": 20, "reasoning": 100, "totalTokens": 6560,
            "cost": ["input": 0.001, "output": 0.002, "cacheRead": 0.001, "cacheWrite": 0.0001, "total": 0.0041]]
        let update = try event("usage.changed", ["usage": usage])
        try state.apply(update)
        try state.apply(update)
        let totals = try #require(state.session.usage)
        #expect(totals.rows.map { $0.1 } == ["1,200", "340", "5,000", "20", "100", "6,560", "$0.004100"])
        #expect(totals.total == 6560)
        var json = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(snapshot())) as? [String: Any])
        json["usage"] = usage
        let refreshed = try SessionProjection.snapshot(JSONDecoder().decode(WireSessionSnapshot.self,
            from: JSONSerialization.data(withJSONObject: json)))
        #expect(refreshed.usage?.total == totals.total)
        #expect(refreshed.usage?.formattedCost == totals.formattedCost)
    }

    @Test func compactionProgressUsesSnapshotAndMatchingEvents() throws {
        var json = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(snapshot())) as? [String: Any])
        json["activeCompaction"] = ["id": "compact_a", "turnId": "t"]
        json["activeTurnId"] = "t"
        let initial = try JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json))
        var state = try SessionEventProjection(initial)
        #expect(state.session.activeCompactionID == "compact_a")
        #expect(state.session.activity == "Compacting context…")
        try state.apply(event("compaction.completed", ["compactionId": "older"]))
        #expect(state.session.activeCompactionID == "compact_a")
        try state.apply(event("compaction.completed", ["compactionId": "compact_a"]))
        #expect(state.session.activeCompactionID == nil)
        #expect(state.session.compactionOutcome?.id == "compact_a")
        #expect(state.session.compactionOutcome?.failed == false)
        #expect(state.session.activity == "Working…")
    }

    @MainActor @Test func failedCompactionAlwaysProvidesToastDetail() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("compaction.started", ["compactionId": "compact_a"]))
        try state.apply(event("compaction.completed", ["compactionId": "compact_a", "isError": true]))
        #expect(state.session.compactionOutcome?.failed == true)
        #expect(state.session.compactionOutcome?.detail == "Context compaction failed")
        let feedback = SessionFeedback()
        feedback.observe(state.session)
        #expect(feedback.notices.first?.detail == "Context compaction failed")

        try state.apply(event("compaction.started", ["compactionId": "compact_b"]))
        try state.apply(event("compaction.completed", ["compactionId": "compact_b", "errorMessage": "  ", "isError": true]))
        #expect(state.session.compactionOutcome?.detail == "Context compaction failed")

        try state.apply(event("compaction.started", ["compactionId": "compact_c"]))
        try state.apply(event("compaction.completed", ["compactionId": "compact_c", "errorMessage": "Provider unavailable", "isError": true]))
        #expect(state.session.compactionOutcome?.detail == "Provider unavailable")
    }

    @Test func contextTracksSnapshotUpdatesAndCompaction() throws {
        let data = try JSONEncoder().encode(snapshot())
        var json = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
        json["contextTokens"] = 82_000
        json["contextWindow"] = 200_000
        let initial = try JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json))
        var state = try SessionEventProjection(initial)
        #expect(state.session.contextTokens == 82_000)
        #expect(state.session.contextWindow == 200_000)
        try state.apply(event("context.changed", ["contextTokens": 180_000, "contextWindow": 200_000]))
        #expect(SessionContext(tokens: state.session.contextTokens, capacity: state.session.contextWindow)?.percentage == 90)
        try state.apply(event("context.changed", ["contextTokens": 20_000, "contextWindow": 100_000]))
        #expect(SessionContext(tokens: state.session.contextTokens, capacity: state.session.contextWindow)?.percentage == 20)
    }

    @Test func liveImageResultsRetainAttachmentIdentityAndTruncation() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("turn.started"))
        try state.apply(event("tool.output.delta", ["toolCallId": "image", "toolName": "show_image",
            "contentTruncated": true, "content": [["kind": "image", "filename": "live.png",
                "mediaType": "image/png", "attachmentId": "attachment_live"]]]))
        let tool = try #require(state.session.messages.last?.tools.first)
        #expect(tool.attachments?.first?.id == "attachment_live")
        #expect(tool.contentTruncated == true)
    }

    @Test func annotationOnlyLivePreviewWaitsForPersistedEvidence() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("turn.started"))
        try state.apply(event("user.message.added", ["text": "Annotations"]))
        #expect(state.session.observedTurns == ["t"])
        #expect(state.session.messages.map(\.text) == ["Annotations"])
        try state.apply(event("annotation.submitted", ["annotationIds": [5]]))
        #expect(state.session.messages.map(\.text) == ["Annotations"])

        let evidence: [String: Any] = ["kind": "annotations", "annotations": [[
            "originalAnnotationId": 5, "anchor": ["kind": "workspace_file", "workspaceFile": [
                "workspaceId": "workspace_test", "path": "README.md", "fileRevision": "file_test",
                "startLine": 7, "endLine": 7]], "body": "testing annotations",
            "preview": ["startLine": 7, "endLine": 7, "text": "Kit is implemented in Go."]]]]
        let canonical: [String: Any] = ["id": "message_0123456789abcdef0123456789abcdef",
            "turnId": "t", "sequence": 1, "role": "user", "createdAt": "2026-09-17T00:00:00Z",
            "content": [evidence]]
        let accepted = try JSONDecoder().decode(WireTranscriptMessage.self, from: JSONSerialization.data(withJSONObject: canonical))
        try state.acceptAnnotationMessage(accepted, ids: [5])
        #expect(state.session.messages.map(\.id) == ["message_0123456789abcdef0123456789abcdef"])
        #expect(state.session.messages.first?.annotations?.map(\.body) == ["testing annotations"])
        #expect(state.session.messages.first?.annotations?.map(\.source) == ["Kit is implemented in Go."])

        var withText = try SessionEventProjection(snapshot())
        try withText.apply(event("user.message.added", ["text": "Please review this"]))
        var textAndEvidence = canonical
        textAndEvidence["content"] = [["kind": "text", "text": "Please review this"], evidence]
        let acceptedWithText = try JSONDecoder().decode(WireTranscriptMessage.self,
            from: JSONSerialization.data(withJSONObject: textAndEvidence))
        try withText.acceptAnnotationMessage(acceptedWithText, ids: [5])
        #expect(withText.session.messages.map(\.text) == ["Please review this"])
        #expect(withText.session.messages.first?.annotations?.map(\.body) == ["testing annotations"])

        var failedLookup = try SessionEventProjection(snapshot())
        try failedLookup.apply(event("user.message.added", ["text": "Annotations"]))
        try failedLookup.apply(event("annotation.submitted", ["annotationIds": [5]]))
        failedLookup.discardAnnotationPreview()
        #expect(failedLookup.session.messages.isEmpty)

        var literal = try SessionEventProjection(snapshot())
        try literal.apply(event("user.message.added", ["text": "Annotations"]))
        try literal.apply(event("turn.completed"))
        literal.clearAnnotationPreviewCandidate()
        literal.discardAnnotationPreview()
        #expect(literal.session.messages.map(\.text) == ["Annotations"])
    }

    @Test func runIdentityRetryAndTerminalFailureAreProjected() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("turn.started"))
        #expect(state.session.activeRunID == "t")
        try state.apply(event("user.message.added", ["text": "Hello"]))
        #expect(state.session.observedTurns == ["t"])
        try state.apply(event("provider.retry.scheduled", ["providerRetry": ["count": 2, "retryAt": "2026-09-13T18:00:00Z"]]))
        #expect(state.session.providerRetryCount == 2)
        #expect(state.session.providerRetryAt == "2026-09-13T18:00:00Z")
        try state.apply(event("provider.retry.started"))
        #expect(state.session.providerRetryAt == nil)
        try state.apply(event("turn.completed", ["errorMessage": "Provider unavailable"]))
        #expect(state.session.activeRunID == nil)
        #expect(state.session.terminalError == "Provider unavailable")
        let refreshed = try SessionEventProjection(snapshot(), terminalError: state.session.terminalError)
        #expect(refreshed.session.terminalError == "Provider unavailable")
    }

    @Test func renameUpdatesTitleWithoutChangingIdentity() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("session.name.changed", ["sessionName": "New title"]))
        #expect(state.session.id == "s")
        #expect(state.session.title == "New title")
    }

    @Test func toolsAppearAndUpdateBeforeTurnFinishes() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("tool.planned", ["toolCallId": "a", "toolName": "bash", "arguments": "{}", "messageId": "m"]))
        #expect(state.session.messages[0].tools[0].status == "Planned")
        try state.apply(event("tool.started", ["toolCallId": "a", "toolName": "bash"]))
        #expect(state.session.messages[0].tools[0].status == "Running…")
        for value in ["first", " second"] {
            try state.apply(event("tool.output.delta", ["toolCallId": "a", "toolName": "bash", "content": [["kind": "text", "text": value]]]))
        }
        #expect(state.session.messages[0].tools[0].output == "first second")
        try state.apply(event("tool.completed", ["toolCallId": "a", "toolName": "bash", "content": [["kind": "text", "text": "Final"]], "isError": true]))
        try state.apply(event("tool.started", ["toolCallId": "b", "toolName": "read"]))
        #expect(state.session.messages.count == 1)
        #expect(state.session.messages[0].tools.map(\.id) == ["a", "b"])
        #expect(state.session.messages[0].tools[0].output == "Final")
        #expect(state.session.messages[0].tools[0].failed)
        #expect(state.session.messages[0].tools[0].arguments == "{}")
    }
    @Test func unfinishedSnapshotToolCallReceivesLiveUpdatesInPlace() throws {
        var json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(snapshot())) as! [String: Any]
        json["activeTurnId"] = "t"
        json["messages"] = [
            ["id": "question", "turnId": "t", "sequence": 1, "role": "user", "createdAt": "",
             "content": [["kind": "text", "text": "Read it"]]],
            ["id": "assistant", "turnId": "t", "sequence": 2, "role": "assistant", "createdAt": "", "stopReason": "toolUse",
             "content": [["kind": "toolCall", "toolCallId": "a", "toolName": "read"]]]
        ]
        var state = try SessionEventProjection(JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json)))
        try state.apply(event("tool.completed", ["toolCallId": "a", "toolName": "read", "content": [["kind": "text", "text": "Contents"]]]))
        #expect(state.session.messages.map(\.id) == ["question", "tools-a"])
        #expect(state.session.messages[1].tools.map(\.id) == ["a"])
        #expect(state.session.messages[1].tools[0].status == "Completed")
        #expect(state.session.messages[1].tools[0].output == "Contents")
    }

    @Test func toolUpdatesFollowRowsAfterEarlierRowsAreRemoved() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("user.message.added", ["text": "Annotations"]))
        try state.apply(event("tool.started", ["toolCallId": "a", "toolName": "read"]))
        #expect(state.session.messages.map(\.id) == ["live-user-t", "tools-a"])
        state.discardAnnotationPreview()
        try state.apply(event("tool.completed", ["toolCallId": "a", "toolName": "read", "content": [["kind": "text", "text": "Done"]]]))
        #expect(state.session.messages.map(\.id) == ["tools-a"])
        #expect(state.session.messages[0].tools.map(\.status) == ["Completed"])
        #expect(state.session.messages[0].tools.map(\.output) == ["Done"])
    }

    private func limited(_ limit: Int) throws -> SessionEventProjection {
        try SessionEventProjection(session: SessionProjection.snapshot(snapshot()), source: [], retainedLiveLimit: limit)
    }

    @Test func releasedLiveContentDoesNotCountTowardTheLimit() throws {
        var state = try limited(1024)
        try state.apply(event("turn.started"))
        let chunk = String(repeating: "x", count: 800)
        for index in 0..<8 {
            let message = "m\(index)", tool = "tool\(index)"
            try state.apply(event("assistant.started", ["messageId": message]))
            try state.apply(event("assistant.text.delta", ["messageId": message, "delta": chunk, "contentIndex": 0]))
            try state.apply(event("assistant.completed", ["messageId": message, "text": "Reply \(index)"]))
            try state.apply(event("tool.output.delta", ["toolCallId": tool, "toolName": "bash", "content": [["kind": "text", "text": chunk]]]))
            try state.apply(event("tool.completed", ["toolCallId": tool, "toolName": "bash", "content": [["kind": "text", "text": "done"]]]))
        }
        #expect(state.liveContentSuspended == false)
        #expect(state.session.messages.filter { $0.role == "assistant" }.map(\.text) == (0..<8).map { "Reply \($0)" })
        #expect(state.session.messages.flatMap(\.tools).map(\.output) == Array(repeating: "done", count: 8))
        try state.apply(event("assistant.started", ["messageId": "streaming"]))
        try state.apply(event("assistant.thinking.delta", ["messageId": "streaming", "delta": "Still streaming", "contentIndex": 0]))
        #expect(state.session.activity == "Still streaming")
    }

    @Test func liveContentAboveTheLimitIsSuspendedUntilTheNextTurn() throws {
        var state = try limited(1024)
        try state.apply(event("turn.started"))
        let chunk = String(repeating: "x", count: 600)
        try state.apply(event("assistant.started", ["messageId": "m"]))
        try state.apply(event("assistant.thinking.delta", ["messageId": "m", "delta": "Planning", "contentIndex": 0]))
        try state.apply(event("assistant.text.delta", ["messageId": "m", "delta": chunk, "contentIndex": 1]))
        try state.apply(event("tool.output.delta", ["messageId": "m", "toolCallId": "a", "toolName": "bash", "content": [["kind": "text", "text": chunk]]]))
        #expect(state.liveContentSuspended)
        #expect(state.session.activity == "Working…")

        // Suspended deltas neither accumulate nor end the stream.
        try state.apply(event("tool.output.delta", ["toolCallId": "a", "toolName": "bash", "content": [["kind": "text", "text": chunk]]]))
        try state.apply(event("assistant.thinking.delta", ["messageId": "m", "delta": " more", "contentIndex": 0]))
        #expect(state.session.messages.flatMap(\.tools).map(\.output.count) == [600])
        #expect(state.session.activity == "Working…")
        try state.apply(event("tool.completed", ["toolCallId": "a", "toolName": "bash", "content": [["kind": "text", "text": "Final"]]]))
        try state.apply(event("assistant.completed", ["messageId": "m", "text": "Answer"]))
        #expect(state.session.messages.map(\.role) == ["tools", "assistant"])
        #expect(state.session.messages.flatMap(\.tools).map(\.status) == ["Completed"])
        #expect(state.session.messages.flatMap(\.tools).map(\.output) == ["Final"])
        #expect(state.session.messages.last?.text == "Answer")

        try state.apply(event("turn.completed", ["status": "completed"]))
        try state.apply(event("turn.started", ["turnId": "t2"]))
        #expect(state.liveContentSuspended == false)
        try state.apply(event("assistant.started", ["turnId": "t2", "messageId": "n"]))
        try state.apply(event("assistant.thinking.delta", ["turnId": "t2", "messageId": "n", "delta": "Next turn", "contentIndex": 0]))
        #expect(state.session.activity == "Next turn")
    }

    @Test func thinkingStreamsButProseAppearsAtomically() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("assistant.started", ["messageId": "m"]))
        try state.apply(event("assistant.thinking.delta", ["messageId": "m", "delta": "Inspecting files", "contentIndex": 0]))
        #expect(state.session.activity == "Inspecting files")
        try state.apply(event("assistant.text.delta", ["messageId": "m", "delta": "Hello", "contentIndex": 1]))
        #expect(state.session.messages.count == 0)
        try state.apply(event("assistant.completed", ["messageId": "m", "text": "Hello world"]))
        #expect(state.session.messages.map(\.text) == ["Hello world"])
        try state.apply(event("turn.completed", ["status": "completed"]))
        #expect(state.session.activity == nil)
    }
    @Test func repeatedLiveCompletionUpdatesResponseInPlace() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("assistant.completed", ["messageId": "m", "text": "First response"]))
        try state.apply(event("assistant.completed", ["messageId": "later", "text": "Later response"]))
        try state.apply(event("assistant.completed", ["messageId": "m", "text": "Final response"]))
        try state.apply(event("assistant.completed", ["messageId": "m", "text": "Final response"]))
        #expect(state.session.messages.map(\.id) == ["m", "later"])
        #expect(state.session.messages.map(\.text) == ["Final response", "Later response"])
    }

    @Test func replayKeepsPersistedCallsAndMessagesUnique() throws {
        let base = try snapshot()
        var json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(base)) as! [String: Any]
        json["messages"] = [
            ["id": "assistant", "turnId": "t", "sequence": 1, "role": "assistant", "createdAt": "", "stopReason": "toolUse",
             "content": [["kind": "toolCall", "toolCallId": "a", "toolName": "read"]]],
            ["id": "result", "turnId": "t", "sequence": 2, "role": "tool", "createdAt": "", "toolCallId": "a",
             "content": [["kind": "text", "text": "Persisted result"]]]
        ]
        var state = try SessionEventProjection(JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json)))
        try state.apply(event("tool.started", ["toolCallId": "a", "toolName": "read"]))
        try state.apply(event("tool.output.delta", ["toolCallId": "a", "toolName": "read", "content": [["kind": "text", "text": "Persisted result"]]]))
        #expect(state.session.messages.count == 1)
        #expect(state.session.messages[0].tools.map(\.output) == ["Persisted result"])
    }

    @Test func pendingSnapshotProseStaysBufferedOnResume() throws {
        var json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(snapshot())) as! [String: Any]
        json["activeTurnId"] = "t"
        json["messages"] = [["id": "m", "turnId": "t", "sequence": 1, "role": "assistant", "createdAt": "",
                              "content": [["kind": "text", "text": "Hello"]]]]
        var state = try SessionEventProjection(JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json)))
        #expect(state.session.messages.count == 0)
        try state.apply(event("assistant.text.delta", ["messageId": "m", "delta": " world", "contentIndex": 0]))
        try state.apply(event("assistant.completed", ["messageId": "m"]))
        #expect(state.session.messages.map(\.text) == ["Hello world"])
    }

    @Test func toolStreamRetainsThinkingEvidence() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("assistant.started", ["messageId": "m"]))
        try state.apply(event("assistant.thinking.delta", ["messageId": "m", "delta": "**Inspect** the files", "contentIndex": 0]))
        #expect(state.session.messages.count == 0)
        #expect(state.session.activity == "**Inspect** the files")
        try state.apply(event("tool.planned", ["messageId": "m", "toolCallId": "a", "toolName": "read"]))
        #expect(state.session.messages[0].tools[0].thinking == "**Inspect** the files")
        try state.apply(event("assistant.completed", ["messageId": "m", "thinking": "**Inspect** the files carefully"]))
        try state.apply(event("tool.started", ["toolCallId": "a", "toolName": "read"]))
        try state.apply(event("tool.started", ["toolCallId": "b", "toolName": "read"]))
        #expect(state.session.messages[0].tools.compactMap(\.thinking) == ["**Inspect** the files carefully"])
        try state.apply(event("turn.completed", ["status": "completed"]))
        #expect(state.session.messages[0].tools[0].thinking == "**Inspect** the files carefully")
    }

    @Test func tabStatusTracksRunAndMultiplePendingRequests() throws {
        var state = try SessionEventProjection(snapshot())
        #expect(state.session.tabStatus == .idle)
        try state.apply(event("turn.started"))
        #expect(state.session.tabStatus == .running)
        let request: [String: Any] = ["id": "one", "sessionId": "s", "turnId": "t", "toolCallId": "tool",
                                      "kind": "confirm", "title": "Continue?", "createdAt": ""]
        var second = request
        second["id"] = "two"
        try state.apply(event("interaction.requested", ["interaction": request]))
        try state.apply(event("interaction.requested", ["interaction": second]))
        try state.apply(event("interaction.requested", ["interaction": request]))
        #expect(state.session.tabStatus == .awaitingResponse)
        try state.apply(event("interaction.resolved", ["interactionId": "one"]))
        #expect(state.session.tabStatus == .awaitingResponse)
        try state.apply(event("interaction.resolved", ["interactionId": "two"]))
        #expect(state.session.tabStatus == .running)
        try state.apply(event("turn.completed", ["status": "completed"]))
        #expect(state.session.tabStatus == .idle)

        var json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(snapshot())) as! [String: Any]
        json["activeTurnId"] = "t"
        json["pendingInteractions"] = [request]
        let resumed = try SessionEventProjection(JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json)))
        #expect(resumed.session.tabStatus == .awaitingResponse)
    }

}
