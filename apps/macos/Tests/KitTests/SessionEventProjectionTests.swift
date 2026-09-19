import Foundation
import Testing
@testable import Kit

struct SessionEventProjectionTests {
    private func snapshot() throws -> WireSessionSnapshot {
        let json = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0}}"#
        return try JSONDecoder().decode(WireSessionSnapshot.self, from: Data(json.utf8))
    }
    private func event(_ kind: String, _ fields: [String: Any] = [:]) throws -> WireSessionEvent {
        var json: [String: Any] = ["streamId": "stream", "sequence": 1, "sessionId": "s", "turnId": "t", "runId": "t", "kind": kind]
        json.merge(fields) { _, new in new }
        return try JSONDecoder().decode(WireSessionEvent.self, from: JSONSerialization.data(withJSONObject: json))
    }
    @Test func cumulativeUsageReplacesTotalsWithoutDependingOnLoadedMessages() throws {
        var state = try SessionEventProjection(snapshot())
        #expect(state.session.usage?.formattedCost == "$0.00")
        let usage: [String: Any] = ["input": 1200, "output": 340, "cacheRead": 5000,
            "cacheWrite": 20, "reasoning": 100, "totalTokens": 6560,
            "cost": ["input": 0.001, "output": 0.002, "cacheRead": 0.001, "cacheWrite": 0.0001, "total": 0.0041]]
        let update = try event("usage.updated", ["usage": usage])
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
        json["activeCompaction"] = ["id": "compact_a", "runId": "t"]
        json["activeRunId"] = "t"
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

    @Test func contextTracksSnapshotUpdatesAndCompaction() throws {
        let data = try JSONEncoder().encode(snapshot())
        var json = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
        json["contextTokens"] = 82_000
        json["contextWindow"] = 200_000
        let initial = try JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json))
        var state = try SessionEventProjection(initial)
        #expect(state.session.contextTokens == 82_000)
        #expect(state.session.contextWindow == 200_000)
        try state.apply(event("context.updated", ["contextTokens": 180_000, "contextWindow": 200_000]))
        #expect(SessionContext(tokens: state.session.contextTokens, capacity: state.session.contextWindow)?.percentage == 90)
        try state.apply(event("context.updated", ["contextTokens": 20_000, "contextWindow": 100_000]))
        #expect(SessionContext(tokens: state.session.contextTokens, capacity: state.session.contextWindow)?.percentage == 20)
    }

    @Test func liveImageResultsRetainAttachmentIdentityAndTruncation() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("run.started"))
        try state.apply(event("tool.completed", ["toolCallId": "image", "toolName": "show_image",
            "contentTruncated": true, "content": [["kind": "image", "filename": "live.png",
                "mediaType": "image/png", "attachmentId": "attachment_live"]]]))
        let tool = try #require(state.session.messages.last?.tools.first)
        #expect(tool.attachments?.first?.id == "attachment_live")
        #expect(tool.contentTruncated == true)
    }

    @Test func runIdentityRetryAndTerminalFailureAreProjected() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("run.started"))
        #expect(state.session.activeRunID == "t")
        try state.apply(event("message.user", ["text": "Hello"]))
        #expect(state.session.observedTurns == ["t"])
        try state.apply(event("provider.retry.scheduled", ["providerRetry": ["count": 2, "retryAt": "2026-09-13T18:00:00Z"]]))
        #expect(state.session.providerRetryCount == 2)
        #expect(state.session.providerRetryAt == "2026-09-13T18:00:00Z")
        try state.apply(event("provider.retry.started"))
        #expect(state.session.providerRetryAt == nil)
        try state.apply(event("run.finished", ["errorMessage": "Provider unavailable"]))
        #expect(state.session.activeRunID == nil)
        #expect(state.session.terminalError == "Provider unavailable")
        let refreshed = try SessionEventProjection(snapshot(), terminalError: state.session.terminalError)
        #expect(refreshed.session.terminalError == "Provider unavailable")
    }

    @Test func renameUpdatesTitleWithoutChangingIdentity() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("session.renamed", ["sessionName": "New title"]))
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
            try state.apply(event("tool.updated", ["toolCallId": "a", "toolName": "bash", "content": [["kind": "text", "text": value]]]))
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
    @Test func thinkingStreamsButProseAppearsAtomically() throws {
        var state = try SessionEventProjection(snapshot())
        try state.apply(event("assistant.started", ["messageId": "m"]))
        try state.apply(event("assistant.thinking.delta", ["messageId": "m", "delta": "Inspecting files", "contentIndex": 0]))
        #expect(state.session.activity == "Inspecting files")
        try state.apply(event("assistant.text.delta", ["messageId": "m", "delta": "Hello", "contentIndex": 1]))
        #expect(state.session.messages.count == 0)
        try state.apply(event("assistant.completed", ["messageId": "m", "text": "Hello world"]))
        #expect(state.session.messages.map(\.text) == ["Hello world"])
        try state.apply(event("run.finished", ["status": "completed"]))
        #expect(state.session.activity == nil)
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
        try state.apply(event("tool.completed", ["toolCallId": "a", "toolName": "read", "content": [["kind": "text", "text": "Persisted result"]]]))
        #expect(state.session.messages.count == 1)
        #expect(state.session.messages[0].tools.map(\.output) == ["Persisted result"])
    }

    @Test func pendingSnapshotProseStaysBufferedOnResume() throws {
        var json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(snapshot())) as! [String: Any]
        json["activeRunId"] = "t"
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
        try state.apply(event("run.finished", ["status": "completed"]))
        #expect(state.session.messages[0].tools[0].thinking == "**Inspect** the files carefully")
    }

    @Test func tabStatusTracksRunAndMultiplePendingRequests() throws {
        var state = try SessionEventProjection(snapshot())
        #expect(state.session.tabStatus == .idle)
        try state.apply(event("run.started"))
        #expect(state.session.tabStatus == .running)
        let request: [String: Any] = ["id": "one", "sessionId": "s", "runId": "t", "toolCallId": "tool",
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
        try state.apply(event("run.finished", ["status": "completed"]))
        #expect(state.session.tabStatus == .idle)

        var json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(snapshot())) as! [String: Any]
        json["activeRunId"] = "t"
        json["pendingInteractions"] = [request]
        let resumed = try SessionEventProjection(JSONDecoder().decode(WireSessionSnapshot.self, from: JSONSerialization.data(withJSONObject: json)))
        #expect(resumed.session.tabStatus == .awaitingResponse)
    }

}
