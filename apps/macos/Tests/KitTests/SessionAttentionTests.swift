import Testing
@testable import Kit

@MainActor
struct SessionAttentionTests {
    private func session(_ id: String = "s", run: String? = nil, generation: String = "watch") -> SessionExcerpt {
        var value = SessionExcerpt(id: id, title: "Test", sourceTitle: "Test", model: "m", thinking: "high",
                                   workspace: "w", date: "", messages: [])
        value.activeRunID = run
        value.watchGeneration = generation
        return value
    }

    @Test func completionBouncesOnceAndReconnectEstablishesBaseline() {
        var bounces = 0
        var time = 0.0
        let feedback = SessionAttention(isActive: { false }, now: { time }, bounce: { bounces += 1 })
        feedback.observe(session(run: "r1"), server: "a")
        #expect(bounces == 0)
        feedback.observe(session(), server: "a")
        #expect(bounces == 1)
        time = 3
        feedback.observe(session(), server: "a")
        feedback.observe(session(run: "r1"), server: "a")
        feedback.observe(session(), server: "a")
        #expect(bounces == 1)
        feedback.observe(session(run: "r2"), server: "a")
        feedback.observe(session(generation: "reconnected"), server: "a")
        #expect(bounces == 1)
    }

    @Test func foregroundIsQuietAndConcurrentCompletionsAreCoalesced() {
        var active = true
        var time = 0.0
        var bounces = 0
        let feedback = SessionAttention(isActive: { active }, now: { time }, bounce: { bounces += 1 })
        feedback.observe(session(run: "r1"), server: "a")
        feedback.observe(session(), server: "a")
        #expect(bounces == 0)
        active = false
        feedback.observe(session(), server: "a")
        #expect(bounces == 0)
        for id in ["one", "two"] {
            feedback.observe(session(id, run: "r2"), server: "a")
            feedback.observe(session(id), server: "a")
        }
        #expect(bounces == 1)
        time = 3
        feedback.observe(session("two", run: "r3"), server: "a")
        var failed = session("two")
        failed.terminalError = "Provider failed"
        feedback.observe(failed, server: "a")
        #expect(bounces == 2)
    }

    @Test func newInteractionRequestsAttentionOnce() {
        var bounces = 0
        let feedback = SessionAttention(isActive: { false }, bounce: { bounces += 1 })
        var value = session(run: "r1")
        feedback.observe(value, server: "a")
        value.pendingInteractions = [WireInteractionRequest(id: "question", sessionId: "s", runId: "r1",
            toolCallId: "tool", kind: .value0, title: "Continue?", detail: nil, options: nil, questions: nil, createdAt: "")]
        feedback.observe(value, server: "a")
        feedback.observe(value, server: "a")
        #expect(bounces == 1)
    }
}
