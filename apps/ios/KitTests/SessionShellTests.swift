import Foundation
import KitClient
import Testing
@testable import Kit

/// The phone shows every client's shell commands from the session stream, and
/// skips events of kinds it doesn't know (ADR 0072).
struct SessionShellTests {
    private func projection() throws -> SessionEventProjection {
        let json = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0}}"#
        return try SessionEventProjection(JSONDecoder().decode(WireSessionSnapshot.self, from: Data(json.utf8)))
    }

    private func batch(_ events: String) throws -> WireSessionEventBatch {
        let json = #"{"streamId":"stream","firstSequence":5,"lastSequence":7,"events":["# + events + "]}"
        return try JSONDecoder().decode(WireSessionEventBatch.self, from: Data(json.utf8))
    }

    @Test func anotherClientsExcludedCommandShowsAsItStartsAndSettles() throws {
        var state = try projection()
        let events = try batch(
            #"{"streamId":"stream","sequence":5,"sessionId":"s","kind":"bash.started","bashExecution":{"id":"bash_1","sessionId":"s","sequence":1,"command":"ls","status":"running","excludeFromContext":true,"startedAt":"2026-11-01T00:00:00Z"}},"# +
            #"{"streamId":"stream","sequence":6,"sessionId":"s","kind":"widget.spun","status":"spinning","widget":{"speed":3}},"# +
            #"{"streamId":"stream","sequence":7,"sessionId":"s","kind":"bash.completed","bashExecution":{"id":"bash_1","sessionId":"s","sequence":1,"command":"ls","status":"completed","output":"a.swift","exitCode":0,"excludeFromContext":true,"startedAt":"2026-11-01T00:00:00Z","completedAt":"2026-11-01T00:00:01Z"}}"#)
        #expect(try EventCursor.accept(events, session: "s", stream: "stream", cursor: 4) == 7)
        try state.apply(events.events![0])
        #expect(state.session.activeBashID == "bash_1")
        #expect(state.session.messages.last?.bash?.running == true)
        try state.apply(events.events![1])
        try state.apply(events.events![2])
        #expect(state.session.activeBashID == nil)
        #expect(state.session.messages.map(\.id) == ["bash_1"])
        #expect(state.session.messages.last?.bash?.output == "a.swift")
        #expect(state.session.messages.last?.bash?.excluded == true)
    }
}
