import Testing
@testable import Kit

@MainActor struct TranscriptPresentationTests {
    @Test func drawerStateSurvivesSessionSwitchingAndScopesByServer() {
        let ui = SessionUIState(demo: false)
        let first = SessionIdentity(server: "one", session: "session")
        let other = SessionIdentity(server: "two", session: "session")
        let drawer = ui.transcript.drawer(for: "tools-turn")
        drawer.expanded = true
        drawer.selectedTool = "read-file"
        ui.switchSession(from: first, to: other, demo: false)
        let otherDrawer = ui.transcript.drawer(for: "tools-turn")
        otherDrawer.expanded = true
        otherDrawer.selectedTool = "run-tests"
        ui.switchSession(from: other, to: first, demo: false)
        #expect(ui.transcript.drawer(for: "tools-turn") === drawer)
        #expect(drawer.expanded == true)
        #expect(drawer.selectedTool == "read-file")
        ui.switchSession(from: first, to: other, demo: false)
        #expect(ui.transcript.drawer(for: "tools-turn") === otherDrawer)
        #expect(otherDrawer.selectedTool == "run-tests")
    }
}
