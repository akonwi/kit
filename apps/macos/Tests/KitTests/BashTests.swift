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
    @Test func pollingPreservesLiveMessagesAndTerminalState() async throws {
        let sink = ShellResults()
        let projection = BashWatchProjection { await sink.accept($0) }
        let session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "server", model: "model", thinking: "off", workspace: "tmp", date: "", messages: [.init(id: "assistant", role: "assistant", text: "Live response", tools: [])])
        await projection.stream(session)
        await projection.poll(session, active: try execution(), settled: [])
        #expect(await sink.latest?.activeBashID == "bash_test")
        await projection.poll(session, active: nil, settled: [try execution(status: .value1, output: "test")])
        await projection.stream(session)
        let rows = await sink.latest?.messages
        #expect(rows?.map(\.id) == ["assistant", "bash_test"])
        #expect(rows?.first?.text == "Live response")
        #expect(rows?.last?.bash?.statusLabel == "Completed")
        #expect(await sink.latest?.activeBashID == nil)
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
            let observed = ShellResults()
            let watch = Task { try await client.watch(session) { await observed.accept($0) } }
            defer { watch.cancel() }
            let id = "bash_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
            _ = try await client.startBash(session, input: .init(executionId: id, command: "sleep 30", excludeFromContext: true))
            #expect(try await client.snapshot(session).activeBashID == id)
            for _ in 0..<60 {
                if await observed.latest?.messages.contains(where: { $0.bash?.id == id && $0.bash?.running == true }) == true { break }
                try await Task.sleep(for: .milliseconds(100))
            }
            #expect(await observed.latest?.messages.contains { $0.bash?.id == id && $0.bash?.running == true } == true)
            try await client.abortBash(session, id: id)
            var result = try await client.bash(session, id: id)
            for _ in 0..<40 where result.running {
                try await Task.sleep(for: .milliseconds(100)); result = try await client.bash(session, id: id)
            }
            #expect(result.status == "aborted")
            for _ in 0..<60 {
                if await observed.latest?.messages.contains(where: { $0.bash?.id == id && $0.bash?.status == "aborted" }) == true { break }
                try await Task.sleep(for: .milliseconds(100))
            }
            #expect(await observed.latest?.messages.contains { $0.bash?.id == id && $0.bash?.status == "aborted" } == true)
            watch.cancel()
            try await client.disposeSession(session)
        } catch { try? await client.disposeSession(session); throw error }
    }
}
