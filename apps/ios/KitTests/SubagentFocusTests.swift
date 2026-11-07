import Foundation
import KitClient
import Testing
@testable import Kit

/// The agent switcher: which transcript shows, and what the agent bar's
/// Model, Thinking, and Dismiss do with the subagent in view.
@MainActor
struct SubagentFocusTests {
    static func agent(_ name: String, _ status: String = "idle", conversation: String? = nil, queued: Int? = nil, generation: UInt64 = 3) -> SubagentRoster.Item {
        SubagentRoster.Item(name: name, description: "", model: "openai/gpt-5.5", status: status,
                            conversationID: conversation ?? "conversation-\(name)", updatedAt: nil,
                            thinkingLevel: "high", queuedTasks: queued, generation: generation)
    }

    static func snapshot(_ agents: SubagentRoster.Item...) -> SessionExcerpt {
        var session = SessionTesting.session("s")
        session.subagents = SubagentRoster(items: agents)
        return session
    }

    static func focus(_ client: FakeSubagentClient = FakeSubagentClient(), _ agents: SubagentRoster.Item...) -> SubagentFocus {
        let focus = SubagentFocus(session: "s", client: client)
        focus.live = true
        focus.reconcile(SessionExcerpt.with(agents))
        focus.refresh = { [weak focus] in focus?.reconcile(try await client.snapshot("s")) }
        return focus
    }

    @Test func showingAnAgentSwapsTheTranscriptAndRemembersIt() {
        let focus = Self.focus(FakeSubagentClient(), Self.agent("reviewer"), Self.agent("tester"))
        #expect(focus.focus == nil)
        focus.show("tester")
        #expect(focus.focused?.name == "tester")
        focus.show("reviewer")
        focus.show(nil)
        #expect(focus.focus == nil)
        #expect(focus.visited == ["tester", "reviewer"])
    }

    @Test func anAgentNotInTheStripCantBeShown() {
        let focus = Self.focus(FakeSubagentClient(), Self.agent("reviewer"))
        focus.show("ghost")
        #expect(focus.focus == nil)
        #expect(focus.visited.isEmpty)
    }

    @Test func mainReturnsWhenTheAgentInViewLeavesTheRoster() {
        let focus = Self.focus(FakeSubagentClient(), Self.agent("reviewer"), Self.agent("tester"))
        focus.show("tester")
        focus.show("reviewer")
        focus.reconcile(Self.snapshot(Self.agent("tester")))
        #expect(focus.focus == nil)
        #expect(focus.visited == ["tester"])
    }

    @Test func theModelIsLockedWhileTheAgentHasWork() {
        let focus = Self.focus(FakeSubagentClient(), Self.agent("reviewer", "running"), Self.agent("tester", queued: 1), Self.agent("idle"))
        #expect(focus.modelLockedReason(Self.agent("reviewer", "running")) == "Can change once reviewer’s work finishes")
        #expect(focus.modelLockedReason(Self.agent("tester", queued: 1)) == "Can change once tester’s work finishes")
        #expect(focus.thinkingLockedReason(Self.agent("reviewer", "running")) == nil)
        #expect(focus.modelLockedReason(Self.agent("idle")) == nil)
        focus.live = false
        #expect(focus.modelLockedReason(Self.agent("idle")) == "Reconnect to change")
    }

    @Test func changingThinkingSendsOnlyThinkingAndRefreshes() async throws {
        let client = FakeSubagentClient()
        let reviewer = Self.agent("reviewer", "running")
        let focus = Self.focus(client, reviewer)
        client.next = Self.snapshot(reviewer)
        focus.changeThinking(reviewer, to: "low")
        try await until { client.snapshots == 1 }
        #expect(client.configured.count == 1)
        #expect(client.configured.first?.model == nil)
        #expect(client.configured.first?.thinking == "low")
        #expect(client.configured.first?.generation == 3)
    }

    @Test func aLockedModelChangeIsNotSent() async throws {
        let client = FakeSubagentClient()
        let reviewer = Self.agent("reviewer", "running")
        let focus = Self.focus(client, reviewer)
        focus.changeModel(reviewer, to: "anthropic/claude")
        try await Task.sleep(for: .milliseconds(50))
        #expect(client.configured.isEmpty)
    }

    @Test func dismissingTheAgentInViewReturnsToMain() async {
        let client = FakeSubagentClient()
        let reviewer = Self.agent("reviewer")
        let focus = Self.focus(client, reviewer, Self.agent("tester"))
        focus.show("reviewer")
        client.next = Self.snapshot(Self.agent("tester"))
        await focus.dismiss(reviewer)
        #expect(client.dismissed == ["conversation-reviewer@3"])
        #expect(focus.focus == nil)
        #expect(focus.strip.agents.map(\.name) == ["tester"])
        #expect(focus.notice == nil)
    }

    @Test func aFailedDismissalKeepsTheAgentAndSaysWhy() async {
        let client = FakeSubagentClient()
        client.dismissError = ClientError.http(409)
        let reviewer = Self.agent("reviewer")
        let focus = Self.focus(client, reviewer)
        focus.show("reviewer")
        client.next = Self.snapshot(reviewer)
        await focus.dismiss(reviewer)
        #expect(focus.focus == "reviewer")
        #expect(focus.notice == .init(text: "Couldn’t dismiss. This conversation changed. Review its current state and try again.", tone: .danger))
        focus.dismissNotice()
        #expect(focus.notice == nil)
    }

    @Test func aDismissalErrorStaysWithItsAgent() async {
        let client = FakeSubagentClient()
        client.dismissError = ClientError.http(409)
        let reviewer = Self.agent("reviewer")
        let focus = Self.focus(client, reviewer, Self.agent("tester"))
        focus.show("reviewer")
        client.next = Self.snapshot(reviewer, Self.agent("tester"))
        await focus.dismiss(reviewer)
        #expect(focus.notice != nil)
        focus.show("tester")
        #expect(focus.notice == nil)
    }

    @Test func aConversationThatLeavesTheRosterIsForgotten() {
        let reviewer = Self.agent("reviewer")
        let focus = Self.focus(FakeSubagentClient(), reviewer)
        let state = focus.conversation(reviewer)
        focus.reconcile(Self.snapshot(reviewer))
        #expect(focus.conversation(reviewer) === state)
        focus.reconcile(Self.snapshot())
        focus.reconcile(Self.snapshot(reviewer))
        #expect(focus.conversation(reviewer) !== state)
    }

    @Test func anAgentStartedAgainUnderTheNameStaysInView() async {
        let client = FakeSubagentClient()
        let reviewer = Self.agent("reviewer")
        let focus = Self.focus(client, reviewer)
        focus.show("reviewer")
        client.next = Self.snapshot(Self.agent("reviewer", conversation: "conversation-again"))
        await focus.dismiss(reviewer)
        #expect(focus.focus == "reviewer")
        #expect(focus.focused?.conversationID == "conversation-again")
    }

    private func until(_ condition: @MainActor () -> Bool) async throws {
        for _ in 0..<100 where !condition() { try await Task.sleep(for: .milliseconds(10)) }
        #expect(condition())
    }
}

private extension SessionExcerpt {
    static func with(_ agents: [SubagentRoster.Item]) -> SessionExcerpt {
        var session = SessionTesting.session("s")
        session.subagents = SubagentRoster(items: agents)
        return session
    }
}

final class FakeSubagentClient: SubagentFocusClient, @unchecked Sendable {
    struct Configured: Equatable {
        let conversation: String
        let generation: UInt64
        let model: String?
        let thinking: String?
    }

    let serverID = "studio"
    let isDemo = false
    private let lock = NSLock()
    private var _configured: [Configured] = []
    private var _dismissed: [String] = []
    private var _snapshots = 0
    private var _next: SessionExcerpt?
    var dismissError: (any Error)?

    var configured: [Configured] { lock.withLock { _configured } }
    var dismissed: [String] { lock.withLock { _dismissed } }
    var snapshots: Int { lock.withLock { _snapshots } }
    /// The snapshot a refresh returns.
    var next: SessionExcerpt? {
        get { lock.withLock { _next } }
        set { lock.withLock { _next = newValue } }
    }

    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        lock.withLock { _snapshots += 1 }
        guard let next else { throw ClientError.missingSession }
        return next
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] { [] }

    func configureSubagent(_ session: String, conversation: String, generation: UInt64,
                           model: String?, thinking: String?) async throws -> SubagentConfigurationResult {
        lock.withLock { _configured.append(.init(conversation: conversation, generation: generation, model: model, thinking: thinking)) }
        return SubagentConfigurationResult(model: model ?? "", thinkingLevel: thinking ?? "", warnings: [])
    }

    func dismissSubagent(_ session: String, conversation: String, generation: UInt64) async throws {
        if let dismissError { throw dismissError }
        lock.withLock { _dismissed.append("\(conversation)@\(generation)") }
    }
}
