import Testing
@testable import Kit

@MainActor
struct WorkspaceTests {
    @Test func tabsDeduplicateAndKeepDrafts() {
        let workspace = WorkspaceState()
        workspace.scratchpad = "Keep this note"
        workspace.open(.agent("reviewer"))
        workspace.open(.scratchpad)
        workspace.open(.agent("reviewer"))
        #expect(workspace.panes == [.conversation, .agent("reviewer"), .scratchpad])
        #expect(workspace.scratchpad == "Keep this note")
        workspace.close(.agent("reviewer"))
        #expect(workspace.selected == .scratchpad)
        workspace.close(.scratchpad)
        #expect(workspace.panes == [.conversation])
    }

    @Test func reviewNotesSurviveTabsAndAreConsumedBySend() {
        let state = SessionStore(fixture: Fixture(sessions: []))
        state.ui.workspace.selectedFile = "View.swift"
        state.ui.workspace.editNote(line: 8)
        state.ui.workspace.noteDraft = "Keep the reopen control."
        state.ui.workspace.saveNote()
        state.ui.workspace.open(.scratchpad)
        state.ui.workspace.open(.review)
        #expect(state.ui.workspace.notes["View.swift:8"] == "Keep the reopen control.")
        state.send()
        #expect(state.messages.first?.text.contains("Keep the reopen control.") == true)
        #expect(state.ui.workspace.notes.isEmpty)
        #expect(state.ui.workspace.visible)
    }

    @Test func mentionReplacesOnlyTrailingToken() {
        let state = SessionStore(fixture: Fixture(sessions: []))
        state.ui.draft = "Check @old.swift and @View"
        state.showFiles()
        state.chooseFile("Sources/View.swift")
        #expect(state.ui.draft == "Check @old.swift and @Sources/View.swift ")
        #expect(!state.ui.filePicker)
        state.showFiles(intent: "open")
        state.chooseFile("Sources/View.swift")
        #expect(state.ui.workspace.selected == .file("Sources/View.swift"))
        #expect(state.ui.draft == "Check @old.swift and @Sources/View.swift ")
    }
}
