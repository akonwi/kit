import Testing
@testable import Kit

@MainActor struct ProgressiveToolGroupTests {
    @Test func automaticExpansionCollapsesAtSixAndOnCompletion() {
        let state = ToolDrawerState()
        #expect((1...7).map { state.isExpanded(count: $0, inProgress: true) } == [true, true, true, true, true, false, false])
        #expect(state.isExpanded(count: 3, inProgress: false) == false)
        #expect(state.isExpanded(count: 0, inProgress: true) == false)
    }
    @Test func explicitChoicesSurviveThresholdAndCompletion() {
        let state = ToolDrawerState()
        state.expanded = false
        #expect(state.isExpanded(count: 2, inProgress: true) == false)
        state.expanded = true
        #expect(state.isExpanded(count: 6, inProgress: true))
        #expect(state.isExpanded(count: 9, inProgress: true))
        #expect(state.isExpanded(count: 9, inProgress: false))
    }
    @Test func latestGroupStaysLiveBetweenCallsWithoutRevivingPreviousTurn() {
        let tool = ToolActivity(id: "call", name: "read", summary: "file.go", output: "contents", failed: false, status: "Completed")
        let first = TranscriptMessage(id: "first", role: "tools", text: "", tools: [tool])
        let prose = TranscriptMessage(id: "prose", role: "assistant", text: "Next step", tools: [])
        let second = TranscriptMessage(id: "second", role: "tools", text: "", tools: [tool])
        #expect(ToolGroupActivity.liveGroups(in: [first, prose], active: true) == ["first"])
        #expect(ToolGroupActivity.liveGroups(in: [first, prose, second], active: true) == ["second"])
        #expect(ToolGroupActivity.liveGroups(in: [first, prose, second], active: false) == [])
        let user = TranscriptMessage(id: "user", role: "user", text: "Next turn", tools: [])
        #expect(ToolGroupActivity.liveGroups(in: [first, prose, second, user], active: true) == [])
    }
}
