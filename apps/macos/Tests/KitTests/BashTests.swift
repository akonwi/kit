import Foundation
import Testing
@testable import Kit

private func execution(_ id: String = "bash_test", status: WireBashExecutionStatus = .value0, excluded: Bool = false, output: String = "") throws -> BashExecution {
    try BashExecution(.init(id: id, sessionId: "s", sequence: 0, command: "printf test", status: status,
        output: output, exitCode: status == .value0 ? nil : 0, excludeFromContext: excluded, truncated: false,
        timedOut: false, errorMessage: nil, startedAt: "2026-09-15T12:00:00Z",
        completedAt: status == .value0 ? nil : "2026-09-15T12:00:01Z"), session: "s")
}
private actor ShellClient: BashClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var inputs: [WireBashExecutionInput] = []
    var lookupFailures: Int
    init(lookupFailures: Int = 0) { self.lookupFailures = lookupFailures }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.disconnected }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func startBash(_ session: String, input: WireBashExecutionInput) async throws -> BashExecution {
        inputs.append(input)
        throw ClientError.disconnected
    }
    func bash(_ session: String, id: String) async throws -> BashExecution {
        if lookupFailures > 0 { lookupFailures -= 1; throw ClientError.http(404) }
        return try execution(id, status: .value1, output: "test")
    }
    func abortBash(_ session: String, id: String) async throws {}
}
private actor ShellResults {
    var latest: SessionExcerpt?
    func accept(_ value: SessionExcerpt) { latest = value }
}
@MainActor struct BashTests {
    @Test func prefixAndAcknowledgementLoss() async throws {
        #expect(BashDraft("! printf hello")?.command == "printf hello")
        #expect(BashDraft("!!printf hello")?.excluded == true)
        #expect(BashDraft("!printf hello")?.excluded == false)
        #expect(BashDraft("hello") == nil)
        let operation = BashOperation(), client = ShellClient()
        var acknowledged = ""
        await operation.submit("!printf test", session: "s", client: client) { acknowledged = $0 }
        #expect(acknowledged == "!printf test")
        #expect(await client.inputs.count == 1)
        #expect(operation.executions.first?.output == "test")
        #expect(operation.executions.first?.statusLabel == "Completed")
        #expect(operation.unresolved == false)
    }
    @Test func explicitRetryKeepsIdentityAndPosition() async throws {
        let operation = BashOperation(), client = ShellClient(lookupFailures: 1)
        await operation.submit("!printf test", session: "s", client: client, after: "before") { _ in }
        #expect(operation.unresolved)
        await operation.retry(session: "s", client: client) { _ in }
        let ids = await client.inputs.map(\.executionId)
        #expect(ids.count == 2)
        #expect(ids[0] == ids[1])
        let rows = operation.merge(into: [
            .init(id: "before", role: "assistant", text: "Before shell", tools: []),
            .init(id: "after", role: "assistant", text: "After shell", tools: [])])
        #expect(rows.map(\.id) == ["before", ids[0], "after"])
    }
    @Test func statusTextShowsOnlyUnsuccessfulOutcomes() throws {
        func settled(_ status: WireBashExecutionStatus, exitCode: Int? = 0, timedOut: Bool = false) throws -> BashExecution {
            try BashExecution(.init(id: "bash_test", sessionId: "s", sequence: 0, command: "printf test", status: status,
                output: "", exitCode: exitCode, excludeFromContext: false, truncated: false,
                timedOut: timedOut, errorMessage: status == .value1 ? nil : "Stopped", startedAt: "2026-09-15T12:00:00Z",
                completedAt: "2026-09-15T12:00:01Z"), session: "s")
        }
        let cases: [(BashExecution, String?)] = [
            (try execution(), nil),
            (try settled(.value1), nil),
            (try settled(.value1, exitCode: 2), "Exit 2"),
            (try settled(.value2, timedOut: true), "Timed out"),
            (try settled(.value3, exitCode: nil), "Aborted"),
            (try settled(.value4, exitCode: nil), "Interrupted")
        ]
        #expect(cases.map { BashExecutionView.status(of: $0.0) } == cases.map(\.1))
    }

    @Test func submittedCommandsKeepTheirPlaceOnlyWhileItIsShown() async throws {
        let operation = BashOperation(), client = ShellClient()
        await operation.submit("!printf test", session: "s", client: client, after: "before") { _ in }
        let id = try #require(operation.executions.first?.id)
        func row(_ id: String) -> TranscriptMessage { .init(id: id, role: "assistant", text: id, tools: []) }
        // Settled before its record arrives, the command follows its anchor.
        #expect(operation.merge(into: [row("before"), row("after")]).map(\.id) == ["before", id, "after"])
        let record = try execution(id, status: .value1, output: "test").message
        #expect(operation.merge(into: [row("before"), record, row("after")]).map(\.id) == ["before", id, "after"])
        // Once its record and anchor leave the shown rows, it is not appended.
        #expect(operation.merge(into: [row("later")]).map(\.id) == ["later"])

        // An excluded command never has a record; it is shown only after its anchor.
        let excluded = try execution("bash_excluded", status: .value1, excluded: true, output: "private")
        let unrecorded = BashExecution.merge([excluded], anchors: ["bash_excluded": "before"], into: [row("before"), row("after")])
        #expect(unrecorded.map(\.id) == ["before", "bash_excluded", "after"])
        #expect(BashExecution.merge([excluded], anchors: ["bash_excluded": "before"], into: [row("later")]).map(\.id) == ["later"])
        // A running command is current work, so it follows a transcript that no
        // longer shows its anchor.
        let running = try execution("bash_running")
        #expect(BashExecution.merge([running], anchors: ["bash_running": "before"], into: [row("later")]).map(\.id) == ["later", "bash_running"])
    }

    private static let snapshotJSON = #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0}"#
    private func snapshot(_ members: String = "") throws -> WireSessionSnapshot {
        try JSONDecoder().decode(WireSessionSnapshot.self, from: Data((Self.snapshotJSON + members + "}").utf8))
    }
    private func wire(_ id: String, sequence: Int = 1, running: Bool = false, excluded: Bool = true, startedAt: String = "2026-09-15T12:00:02Z") -> [String: Any] {
        var value: [String: Any] = ["id": id, "sessionId": "s", "sequence": sequence, "command": "ls",
            "status": running ? "running" : "completed", "excludeFromContext": excluded, "startedAt": startedAt]
        if !running { value["output"] = "a.swift"; value["exitCode"] = 0; value["completedAt"] = "2026-09-15T12:00:03Z" }
        return value
    }
    private func batch(_ events: [[String: Any]], first: Int = 1) throws -> WireSessionEventBatch {
        let numbered = events.enumerated().map { index, event in
            event.merging(["streamId": "stream", "sequence": first + index, "sessionId": "s"]) { current, _ in current }
        }
        let json: [String: Any] = ["streamId": "stream", "firstSequence": first, "lastSequence": first + events.count - 1, "events": numbered]
        return try JSONDecoder().decode(WireSessionEventBatch.self, from: JSONSerialization.data(withJSONObject: json))
    }

    @Test func anotherClientsCommandStartsAndSettlesFromTheStream() throws {
        var state = try SessionEventProjection(snapshot(#","messages":[{"id":"a","turnId":"t","sequence":1,"role":"assistant","content":[{"kind":"text","text":"Before"}],"stopReason":"stop","createdAt":"2026-09-15T12:00:01Z"}]"#))
        let events = try batch([
            ["kind": "bash.started", "bashExecution": wire("bash_1", running: true)],
            ["kind": "bash.completed", "bashExecution": wire("bash_1")],
            // A late copy of the start leaves the command settled.
            ["kind": "bash.started", "bashExecution": wire("bash_1", running: true)],
        ])
        try state.apply(events.events![0])
        #expect(state.session.messages.map(\.id) == ["a", "bash_1"])
        #expect(state.session.messages.last?.bash?.running == true)
        #expect(state.session.messages.last?.bash?.excluded == true)
        #expect(state.session.activeBashID == "bash_1")
        try state.apply(events.events![1])
        try state.apply(events.events![2])
        #expect(state.session.messages.map(\.id) == ["a", "bash_1"])
        #expect(state.session.messages.last?.bash?.output == "a.swift")
        #expect(state.session.messages.last?.bash?.running == false)
        #expect(state.session.activeBashID == nil)
    }

    @Test func aSnapshotPlacesTheCommandsItListsWhereTheyStarted() throws {
        let message = { (id: String, at: String) in #"{"id":"\#(id)","turnId":"t\#(id)","sequence":1,"role":"user","content":[{"kind":"text","text":"\#(id)"}],"createdAt":"\#(at)"}"# }
        let executions = try String(data: JSONSerialization.data(withJSONObject: [
            wire("bash_0", sequence: 0, startedAt: "2026-09-15T12:00:00Z"),
            wire("bash_1", sequence: 1, startedAt: "2026-09-15T12:00:02Z"),
            wire("bash_2", sequence: 2, running: true, startedAt: "2026-09-15T12:00:04Z")]), encoding: .utf8)!
        let state = try SessionEventProjection(snapshot(#","activeBashExecutionId":"bash_2","messages":["# +
            message("a", "2026-09-15T12:00:01Z") + "," + message("b", "2026-09-15T12:00:03Z") + #"],"bashExecutions":"# + executions))
        #expect(state.session.messages.map(\.id) == ["bash_0", "a", "bash_1", "b", "bash_2"])
        #expect(state.session.messages.last?.bash?.running == true)
        #expect(state.session.activeBashID == "bash_2")
        // A running command must be the active one.
        #expect(throws: ClientError.self) { try SessionEventProjection(snapshot(#","bashExecutions":"# + executions)) }
    }

    @Test func eventsOfUnknownKindsAreSkippedAndAdvanceTheCursor() throws {
        var state = try SessionEventProjection(snapshot())
        let events = try batch([
            ["kind": "widget.spun", "status": "spinning", "widget": ["speed": 3]],
            ["kind": "bash.started", "bashExecution": wire("bash_1", running: true)],
        ], first: 5)
        #expect(events.events?.first?.kind == .unrecognized)
        #expect(try EventCursor.accept(events, session: "s", stream: "stream", cursor: 4) == 6)
        for event in events.events ?? [] { try state.apply(event) }
        #expect(state.session.messages.map(\.id) == ["bash_1"])
        // A kind that breaks the event grammar is still malformed.
        #expect(throws: (any Error).self) { try batch([["kind": "Widget Spun"]]) }
    }

    @Test func aSnapshotIgnoresMembersItDoesNotKnow() throws {
        let data = Data((Self.snapshotJSON + #","messages":[],"widgets":[{"speed":3}]}"#).utf8)
        let generated = try JSONDecoder().decode(Components.Schemas.SessionSnapshot.self, from: data)
        #expect(generated.session.id == "s")
        #expect(try snapshot(#","widgets":[{"speed":3}]"#).session.id == "s")
        // A member it knows is still decoded strictly.
        let nested = Data(Self.snapshotJSON.replacingOccurrences(of: #""session":{"#, with: #""session":{"widget":1,"#).appending(#","messages":[]}"#).utf8)
        #expect(throws: (any Error).self) { try JSONDecoder().decode(Components.Schemas.SessionSnapshot.self, from: nested) }
    }
    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_BASH_LIVE_TEST"] == "1"))
    func liveExecutionContextAndAbort() async throws {
        let client = try await HTTPClient.local()
        let session = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        _ = try await client.createSession(.init(id: session, cwd: "/tmp", name: "macOS shell verification", model: "openai-codex/gpt-5.6-sol", thinkingLevel: "off", temporary: true))
        do {
            for excluded in [false, true] {
                let id = "bash_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
                let input = WireBashExecutionInput(executionId: id, command: "printf KIT_SHELL_VERIFIED", excludeFromContext: excluded)
                _ = try await client.startBash(session, input: input)
                var result = try await client.bash(session, id: id)
                for _ in 0..<40 where result.running {
                    try await Task.sleep(for: .milliseconds(100)); result = try await client.bash(session, id: id)
                }
                #expect(result.output == "KIT_SHELL_VERIFIED")
                #expect(result.excluded == excluded)
                #expect(result.statusLabel == "Completed")
                let duplicate = try await client.startBash(session, input: input)
                #expect(duplicate.id == result.id)
                let snapshot = try await client.snapshot(session)
                #expect(snapshot.messages.contains { $0.bash?.id == id } == !excluded)
            }
            // The watch learns of executions from its snapshots; commands started
            // by this window are tracked by its BashOperation instead.
            let id = "bash_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
            _ = try await client.startBash(session, input: .init(executionId: id, command: "sleep 30", excludeFromContext: true))
            #expect(try await client.snapshot(session).activeBashID == id)
            let observed = ShellResults()
            let watch = Task { try await client.watch(session) { await observed.accept($0) } }
            defer { watch.cancel() }
            try await eventually { await observed.latest?.messages.contains(where: { $0.bash?.id == id && $0.bash?.running == true }) == true }
            #expect(await observed.latest?.messages.contains { $0.bash?.id == id && $0.bash?.running == true } == true)
            try await client.abortBash(session, id: id)
            var result = try await client.bash(session, id: id)
            for _ in 0..<40 where result.running {
                try await Task.sleep(for: .milliseconds(100)); result = try await client.bash(session, id: id)
            }
            #expect(result.status == "aborted")
            try await eventually { await observed.latest?.messages.contains(where: { $0.bash?.id == id && $0.bash?.status == "aborted" }) == true }
            #expect(await observed.latest?.messages.contains { $0.bash?.id == id && $0.bash?.status == "aborted" } == true)
            watch.cancel()
            try await client.disposeSession(session)
        } catch { try? await client.disposeSession(session); throw error }
    }
}
