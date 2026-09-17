import AppKit
import Foundation
import Testing
@testable import Kit

private let childID = "subagent_00000000000000000000000000000001"
private let taskID = "task_00000000000000000000000000000001"
private func receipt(agent: String = "helper", conversation: String? = nil) throws -> SubagentReceipt {
    let json = """
    {"conversation":{"id":"\(childID)","agentName":"helper","model":"provider/model","thinkingLevel":"medium","state":"running","generation":1,"queuedTasks":0,"updatedAt":"2026-09-15T00:00:00Z"},"task":{"id":"\(taskID)","sequence":1,"state":"running","cancellationGeneration":1,"queuedAt":"2026-09-15T00:00:00Z"}}
    """
    return try SubagentReceipt(JSONDecoder().decode(WireSubagentOperationResult.self, from: Data(json.utf8)), agent: agent, conversation: conversation)
}
private actor MessagingStub: SubagentMessagingClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    let fail: Bool
    var selectors: [String?] = []
    init(fail: Bool = false) { self.fail = fail }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.disconnected }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] { [] }
    func sendSubagent(_ session: String, agent: String, conversation: String?, message: String) async throws -> SubagentReceipt {
        selectors.append(conversation)
        if fail { throw ClientError.disconnected }
        return try receipt(agent: agent, conversation: conversation)
    }
}

@MainActor struct SubagentMessagingTests {
    @Test func startsThenFollowsUpInAcknowledgedConversation() async {
        let state = SubagentSendOperation(), client = MessagingStub()
        var refreshes = 0
        state.draft = "First task"
        await state.send(session: "s", agent: "helper", conversation: nil, client: client) { refreshes += 1 }
        #expect(state.conversationID == childID)
        #expect(state.draft == "")
        state.draft = "Follow up"
        await state.send(session: "s", agent: "helper", conversation: nil, client: client) { refreshes += 1 }
        #expect(await client.selectors == [nil, childID])
        #expect(refreshes == 2)
        #expect(state.pending == false)
    }
    @Test func lostAcknowledgementRefreshesWithoutReplayAndPreservesDraft() async {
        let state = SubagentSendOperation(), client = MessagingStub(fail: true)
        var refreshes = 0
        state.draft = "Keep this draft"
        await state.send(session: "s", agent: "helper", conversation: childID, client: client) { refreshes += 1 }
        #expect(state.draft == "Keep this draft")
        #expect(refreshes == 1)
        #expect(await client.selectors.count == 1)
        #expect(state.error != nil)
        #expect(state.pending == false)
    }
    @Test func refreshFailureDoesNotTurnAcknowledgedMessageIntoRetry() async {
        let state = SubagentSendOperation(), client = MessagingStub()
        state.draft = "Accepted"
        await state.send(session: "s", agent: "helper", conversation: nil, client: client) { throw ClientError.disconnected }
        #expect(state.draft == "")
        #expect(state.conversationID == childID)
        #expect(state.error != nil)
        #expect(await client.selectors.count == 1)
    }
    @Test func replyMustMatchAgentAndConversation() throws {
        #expect(throws: ClientError.self) { try receipt(agent: "different") }
        #expect(throws: ClientError.self) { try receipt(conversation: "subagent_00000000000000000000000000000002") }
    }
    @Test func subagentInputPreservesLiteralShellPrefixes() {
        let editor = ComposerTextView(frame: .zero)
        editor.shellEnabled = false
        editor.setDraftText("!!important instructions")
        #expect(editor.string == "!!important instructions")
        #expect(editor.draftText == "!!important instructions")
    }
    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_SUBAGENT_MESSAGING_LIVE_TEST"] == "1"))
    func liveStartFollowUpAndTranscript() async throws {
        let client = try await HTTPClient.local()
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("kit-subagent-" + UUID().uuidString)
        let agents = directory.appendingPathComponent(".kit/agents")
        try FileManager.default.createDirectory(at: agents, withIntermediateDirectories: true)
        try """
        ---
        name: verification-helper
        description: Isolated macOS messaging verification
        model: openai-codex/gpt-5.6-sol
        ---
        Reply with exactly the token requested by the user. Do not call tools or access files.
        """.write(to: agents.appendingPathComponent("helper.md"), atomically: true, encoding: .utf8)
        let session = "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        _ = try await client.createSession(.init(id: session, cwd: directory.path, name: "macOS subagent API verification", model: "openai-codex/gpt-5.6-sol", thinkingLevel: "off", temporary: false))
        do {
            let first = try await client.sendSubagent(session, agent: "verification-helper", conversation: nil, message: "Reply exactly SUBAGENT_FIRST_VERIFIED")
            for token in ["SUBAGENT_FIRST_VERIFIED", "SUBAGENT_FOLLOWUP_VERIFIED"] {
                if token == "SUBAGENT_FOLLOWUP_VERIFIED" {
                    let next = try await client.sendSubagent(session, agent: "verification-helper", conversation: first.conversationID, message: "Reply exactly " + token)
                    #expect(next.conversationID == first.conversationID)
                    #expect(next.taskID != first.taskID)
                }
                var messages: [TranscriptMessage] = []
                for _ in 0..<90 {
                    do { messages = try await client.subagentTranscript(session: session, conversation: first.conversationID) }
                    catch ClientError.http(409) {
                        // The server can briefly reject reads while constructing a child runtime.
                        try await Task.sleep(for: .milliseconds(100))
                        continue
                    }
                    if messages.contains(where: { $0.role == "assistant" && $0.text.contains(token) }) { break }
                    try await Task.sleep(for: .seconds(1))
                }
                #expect(messages.contains { $0.role == "assistant" && $0.text.contains(token) })
                #expect(try await client.snapshot(session).subagents?.items.first { $0.name == "verification-helper" }?.conversationID == first.conversationID)
            }
            for _ in 0..<60 {
                do { try await client.deleteSession(session); try? FileManager.default.removeItem(at: directory); return }
                catch ClientError.http(409) { try await Task.sleep(for: .seconds(1)) }
            }
            try await client.deleteSession(session)
        } catch {
            // Leave the directory intact if the server still owns a running task.
            if (try? await client.deleteSession(session)) != nil { try? FileManager.default.removeItem(at: directory) }
            throw error
        }
    }
}
