import KitClient
import Testing
@testable import Kit

/// The agent strip lists started subagents beside Main: the ones that need a
/// look, then the ones working, then the idle ones, each group in the order
/// the main agent started them.
struct SubagentStripTests {
    static func agent(_ name: String, _ status: String, started: Bool = true, queued: Int? = nil) -> SubagentRoster.Item {
        SubagentRoster.Item(name: name, description: "", model: "gpt-5.5", status: status,
                            conversationID: started ? "conversation-\(name)" : nil, updatedAt: nil,
                            queuedTasks: queued, generation: started ? 1 : nil)
    }

    /// A tools message whose calls start the named agents in order.
    static func starts(_ names: String...) -> TranscriptMessage { calls("start", names) }

    static func calls(_ action: String, _ names: [String]) -> TranscriptMessage {
        TranscriptMessage(id: "tools", role: "tools", text: "", tools: names.map {
            ToolActivity(id: "call-\($0)", name: "subagent", summary: "", output: "",
                         arguments: #"{"action":"\#(action)","agent":"\#($0)"}"#, failed: false)
        })
    }

    @Test func agentsNeedingALookComeFirstThenWorkingThenIdle() {
        let roster = SubagentRoster(items: [
            Self.agent("reviewer", "idle"),
            Self.agent("tester", "running"),
            Self.agent("scout", "failed"),
            Self.agent("writer", "interrupted"),
            Self.agent("helper", "aborted"),
        ])
        let strip = SubagentStrip(roster: roster, messages: [Self.starts("reviewer", "tester", "scout", "helper", "writer")])
        #expect(strip.agents.map(\.name) == ["scout", "writer", "tester", "reviewer", "helper"])
    }

    @Test func agentsInAGroupKeepTheirStartOrder() {
        let roster = SubagentRoster(items: [
            Self.agent("alpha", "running"),
            Self.agent("zulu", "running"),
            Self.agent("mike", "running"),
        ])
        let strip = SubagentStrip(roster: roster, messages: [Self.starts("zulu"), Self.starts("mike", "alpha", "zulu")])
        #expect(strip.agents.map(\.name) == ["zulu", "mike", "alpha"])
    }

    @Test func onlyStartCallsGiveTheStartOrder() {
        let roster = SubagentRoster(items: [Self.agent("alpha", "idle"), Self.agent("zulu", "idle")])
        let strip = SubagentStrip(roster: roster, messages: [Self.calls("message", ["zulu"]), Self.starts("alpha", "zulu")])
        #expect(strip.agents.map(\.name) == ["alpha", "zulu"])
    }

    @Test func anIdleAgentWithQueuedTasksCountsAsWorking() {
        let roster = SubagentRoster(items: [Self.agent("first", "idle"), Self.agent("second", "idle", queued: 2)])
        let strip = SubagentStrip(roster: roster, messages: [Self.starts("first", "second")])
        #expect(strip.agents.map(\.name) == ["second", "first"])
    }

    @Test func agentsStartedBeforeTheLoadedHistoryFollowByName() {
        let roster = SubagentRoster(items: [
            Self.agent("beta", "idle"),
            Self.agent("alpha", "idle"),
            Self.agent("known", "idle"),
        ])
        let strip = SubagentStrip(roster: roster, messages: [Self.starts("known")])
        #expect(strip.agents.map(\.name) == ["known", "alpha", "beta"])
    }

    @Test func agentsThatNeverStartedAreLeftOut() {
        let roster = SubagentRoster(items: [Self.agent("reviewer", "idle"), Self.agent("unused", "inactive", started: false)])
        let strip = SubagentStrip(roster: roster, messages: [])
        #expect(strip.agents.map(\.name) == ["reviewer"])
        #expect(SubagentStrip(roster: nil, messages: []).isEmpty)
    }
}
