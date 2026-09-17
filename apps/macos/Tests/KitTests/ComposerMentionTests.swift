import AppKit
import Testing
@testable import Kit

@MainActor struct ComposerMentionTests {
    @Test func typedMentionsRespectBoundariesAndPaste() {
        for (previous, next, pasted, expected) in [
            ("", "@", false, true), ("see ", "see @", false, true),
            ("see\n", "see\n@", false, true), ("mail", "mail@", false, false),
            ("", "@", true, false), ("", "@src", true, false)
        ] {
            let state = ComposerMentionState()
            #expect(state.observe(previous: previous, next: next, pasted: pasted) == expected)
            #expect(state.isOpen == expected)
        }
    }
    @Test func inlineEditsTrackOnlyTheActiveToken() {
        let state = ComposerMentionState()
        state.observe(previous: "🙂 before  after", next: "🙂 before @ after", pasted: false)
        #expect(state.range == NSRange(location: 10, length: 1))
        state.observe(previous: "🙂 before @ after", next: "🙂 before @src after", pasted: false)
        #expect(state.query == "src")
        #expect(state.range == NSRange(location: 10, length: 4))
        state.observe(previous: "🙂 before @src after", next: "🙂 before @sr after", pasted: false)
        #expect(state.query == "sr")
        state.observe(previous: "🙂 before @sr after", next: "🙂 before @sr  after", pasted: false)
        #expect(state.isOpen == false)
    }
    @Test func fuzzyRankingMatchesTuiAndIncludesDirectories() {
        #expect(ComposerMentionState.filtered(query: "src", paths: ["other/src.go", "src/main.go", "src/", "src", "scratch"]) ==
            ["src", "src/main.go", "src/", "other/src.go", "scratch"])
        #expect(ComposerMentionState.filtered(query: "tsh", paths: ["internal/tui/app.go", "internal/tui/shell.go"]) == ["internal/tui/shell.go"])
    }
    @Test func arrowsWrapAndEnterReplacesTheTokenWithoutSubmitting() async throws {
        let state = ComposerMentionState()
        state.observe(previous: "see  later", next: "see @ later", pasted: false)
        state.load { _ in FileIndex(paths: ["docs/", "src/main.swift"]) }
        for _ in 0..<100 where state.loading { try await Task.sleep(for: .milliseconds(5)) }
        #expect(state.selected == "docs/")
        state.move(-1)
        #expect(state.selected == "src/main.swift")
        state.move(1)
        #expect(state.selected == "docs/")
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        editor.string = "see @ later"; editor.mentions = state
        editor.setSelectedRange(NSRange(location: 5, length: 0))
        editor.submit = { Issue.record("Mention completion must not submit the prompt") }
        let enter = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: 0, context: nil, characters: "\r", charactersIgnoringModifiers: "\r", isARepeat: false, keyCode: 36))
        editor.keyDown(with: enter)
        #expect(editor.string == "see @docs/  later")
        #expect(editor.selectedRange().location == 11)
        #expect(state.isOpen == false)
    }
    @Test func closedPickerIgnoresLateFileIndexResults() async throws {
        let state = ComposerMentionState()
        state.observe(previous: "", next: "@", pasted: false)
        state.load { _ in try? await Task.sleep(for: .milliseconds(30)); return FileIndex(paths: ["stale.swift"]) }
        state.reset()
        try await Task.sleep(for: .milliseconds(60))
        #expect(state.isOpen == false)
        #expect(state.entries == [])
    }
}
