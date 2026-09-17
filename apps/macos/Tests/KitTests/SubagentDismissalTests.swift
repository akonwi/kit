import Foundation
import Testing
@testable import Kit

private actor DismissalStub: SubagentDismissalClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    let failure: ClientError?
    var generations: [UInt64] = []
    init(failure: ClientError? = nil) { self.failure = failure }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.disconnected }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] { [] }
    func dismissSubagent(_ session: String, conversation: String, generation: UInt64) async throws {
        generations.append(generation)
        if let failure { throw failure }
    }
}

private actor DismissalSessionStub: SubagentDismissalClient {
    nonisolated let serverID = "dismiss-test"
    nonisolated let isDemo = false
    let before: SessionExcerpt
    let after: SessionExcerpt
    var dismissed = false
    init(before: SessionExcerpt, after: SessionExcerpt) { self.before = before; self.after = after }
    func sessions() async throws -> [SessionExcerpt] { [dismissed ? after : before] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { dismissed ? after : before }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(dismissed ? after : before)
    }
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] { [] }
    func dismissSubagent(_ session: String, conversation: String, generation: UInt64) async throws { dismissed = true }
}

@MainActor struct SubagentDismissalTests {
    @Test func acknowledgedDismissalRefreshesRoster() async {
        let operation = SubagentDismissalOperation(), client = DismissalStub()
        var reads = 0
        let dismissed = await operation.dismiss(session: "s", conversation: "c", generation: 4, client: client) { reads += 1 }
        #expect(dismissed)
        #expect(reads == 1)
        #expect(await client.generations == [4])
        #expect(operation.pending == false)
        #expect(operation.error == nil)
    }
    @Test func staleDismissalRefreshesWithoutReplayingNewGeneration() async {
        let operation = SubagentDismissalOperation(), client = DismissalStub(failure: .http(409))
        var reads = 0
        let dismissed = await operation.dismiss(session: "s", conversation: "c", generation: 4, client: client) { reads += 1 }
        #expect(dismissed == false)
        #expect(reads == 1)
        #expect(await client.generations == [4])
        #expect(operation.error == "This conversation changed. Review its current state and try again.")
    }
    @Test func uncertainDismissalRefreshesWithoutClaimingSuccess() async {
        let operation = SubagentDismissalOperation(), client = DismissalStub(failure: .disconnected)
        var reads = 0
        let dismissed = await operation.dismiss(session: "s", conversation: "c", generation: 4, client: client) { reads += 1 }
        #expect(dismissed == false)
        #expect(reads == 1)
        #expect(await client.generations == [4])
    }
    @Test func confirmationDependsOnOutstandingWork() {
        var agent = SubagentRoster.Item(name: "helper", description: "", model: "m", status: "idle", conversationID: "c", updatedAt: nil, generation: 1)
        #expect(agent.canDismiss)
        #expect(agent.hasOutstandingWork == false)
        agent.queuedTasks = 1
        #expect(agent.hasOutstandingWork)
        agent.queuedTasks = 0
        agent.activeTaskID = "task"
        #expect(agent.hasOutstandingWork)
    }

    @Test(arguments: [false, true])
    func closesDismissedPaneButPreservesReplacementConversation(replacement: Bool) async throws {
        func session(_ conversation: String?) throws -> SessionExcerpt {
            var result = SessionExcerpt(id: "s", title: "Test", sourceTitle: "test", model: "m", thinking: "off", workspace: "tmp", date: "", messages: [])
            let item: [String: Any] = ["name": "helper", "description": "Helper", "model": "m",
                "status": conversation == nil ? "inactive" : "idle", "conversationID": conversation as Any? ?? NSNull(), "generation": 1]
            result.subagents = try JSONDecoder().decode(SubagentRoster.self,
                from: JSONSerialization.data(withJSONObject: ["items": [item], "diagnostics": []]))
            result.followUps = try FollowUpState(.init(count: 0, previews: nil))
            return result
        }
        let before = try session("old"), after = try session(replacement ? "new" : nil)
        let client = DismissalSessionStub(before: before, after: after)
        let store = SessionStore(fixture: .init(sessions: [before]), client: client)
        store.ui.workspace.open(.agent("helper"))
        store.attach()
        for _ in 0..<100 where store.connectionState != .connected { try await Task.sleep(for: .milliseconds(10)) }
        let agent = try #require(before.subagents?.items.first)
        await store.dismissSubagent(agent, session: "s")
        #expect(store.ui.workspace.panes.contains(.agent("helper")) == replacement)
        #expect(store.selected?.subagents?.items.first?.conversationID == (replacement ? "new" : nil))
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_SUBAGENT_DISMISS_LIVE_TEST"] == "1"))
    func liveDismissalRejectsStaleGenerationAndRetiresConversation() async throws {
        let client = try await HTTPClient.local()
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("kit-dismiss-" + UUID().uuidString)
        let agents = directory.appendingPathComponent(".kit/agents")
        try FileManager.default.createDirectory(at: agents, withIntermediateDirectories: true)
        try """
        ---
        name: dismissal-helper
        description: Isolated dismissal verification
        model: openai-codex/gpt-5.6-sol
        ---
        Reply with exactly READY. Do not call tools or access files.
        """.write(to: agents.appendingPathComponent("helper.md"), atomically: true, encoding: .utf8)
        let session = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        _ = try await client.createSession(.init(id: session, cwd: directory.path, name: "macOS dismissal verification", model: "openai-codex/gpt-5.6-sol", thinkingLevel: "off", temporary: false))
        do {
            let receipt = try await client.sendSubagent(session, agent: "dismissal-helper", conversation: nil, message: "Reply READY")
            let roster = try await client.snapshot(session).subagents
            let agent = try #require(roster?.items.first { $0.name == "dismissal-helper" })
            let generation = try #require(agent.generation)
            do {
                try await client.dismissSubagent(session, conversation: receipt.conversationID, generation: generation + 100)
                Issue.record("Stale dismissal was accepted")
            } catch ClientError.http(409) { }
            let refreshed = try await client.snapshot(session).subagents
            let current = try #require(refreshed?.items.first { $0.name == "dismissal-helper" })
            #expect(current.conversationID == receipt.conversationID)
            try await client.dismissSubagent(session, conversation: receipt.conversationID, generation: #require(current.generation))
            let final = try await client.snapshot(session).subagents
            let inactive = try #require(final?.items.first { $0.name == "dismissal-helper" })
            #expect(inactive.status == "inactive")
            #expect(inactive.conversationID == nil)
            for _ in 0..<60 {
                do {
                    try await client.deleteSession(session)
                    try? FileManager.default.removeItem(at: directory)
                    return
                } catch ClientError.http(409) { try await Task.sleep(for: .seconds(1)) }
            }
            Issue.record("Test session remained busy after dismissal")
        } catch {
            if (try? await client.deleteSession(session)) != nil { try? FileManager.default.removeItem(at: directory) }
            throw error
        }
    }
}
