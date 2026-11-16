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

    @Test func hiddenPanesAreBuiltOnceShownAndStayBuilt() {
        let workspace = WorkspaceState()
        workspace.open(.scratchpad)
        let terminal = workspace.openTerminal(directory: "/tmp")
        workspace.open(.file("View.swift"))
        // Only the file is on screen. The scratchpad hasn't been, so it
        // waits; the conversation and the terminal are always built.
        #expect(workspace.builtPanes() == [.conversation, .terminal(terminal.id), .file("View.swift")])

        workspace.select(.scratchpad)
        #expect(workspace.builtPanes() == [.conversation, .scratchpad, .terminal(terminal.id), .file("View.swift")])
        workspace.select(.conversation)
        #expect(workspace.builtPanes() == [.conversation, .scratchpad, .terminal(terminal.id), .file("View.swift")])

        // A pane closed is built again when it next opens.
        workspace.close(.scratchpad)
        #expect(workspace.builtPanes() == [.conversation, .terminal(terminal.id), .file("View.swift")])
        workspace.open(.scratchpad)
        #expect(workspace.builtPanes() == [.conversation, .terminal(terminal.id), .file("View.swift"), .scratchpad])
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
        #expect(state.ui.workspace.panes == [.conversation, .scratchpad, .review])
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
