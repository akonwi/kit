import AppKit
import Testing
@testable import Kit

@MainActor
struct ComposerKeyboardTests {
    private func key(_ modifiers: NSEvent.ModifierFlags = []) throws -> NSEvent {
        try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: modifiers,
            timestamp: 0, windowNumber: 0, context: nil, characters: "\r",
            charactersIgnoringModifiers: "\r", isARepeat: false, keyCode: 36))
    }

    @Test func enterSubmitsWithoutChangingTheDraft() throws {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        editor.string = "Send this message"
        var submitted = ""
        editor.submit = { submitted = editor.string }
        editor.keyDown(with: try key())
        #expect(submitted == "Send this message")
        #expect(editor.string == "Send this message")
    }

    @Test func commandEnterInsertsNewlineAtSelection() throws {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        editor.string = "FirstSecond"
        editor.setSelectedRange(NSRange(location: 5, length: 0))
        editor.submit = { Issue.record("Command-Enter must insert a newline") }
        editor.keyDown(with: try key(.command))
        #expect(editor.string == "First\nSecond")
        #expect(editor.selectedRange().location == 6)
    }

    @Test func markedTextUsesNativeConfirmationBeforeSubmission() throws {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        editor.setMarkedText("候補", selectedRange: NSRange(location: 2, length: 0), replacementRange: NSRange(location: NSNotFound, length: 0))
        editor.submit = { Issue.record("Enter must confirm marked text through the input method") }
        editor.keyDown(with: try key())
        #expect(editor.string.contains("候補"))
    }
}
