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

    private func slash(_ modifiers: NSEvent.ModifierFlags = []) throws -> NSEvent {
        try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: modifiers,
            timestamp: 0, windowNumber: 0, context: nil, characters: "/",
            charactersIgnoringModifiers: "/", isARepeat: false, keyCode: 44))
    }

    @Test func slashOpensPaletteOnlyFromAnEmptyMessageDraft() throws {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        let window = NSWindow(contentRect: editor.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = editor
        window.orderFront(nil)
        defer { window.close() }
        window.makeFirstResponder(editor)
        var opens = 0
        editor.openPalette = { opens += 1 }
        editor.keyDown(with: try slash())
        #expect(opens == 1)
        #expect(editor.string == "")
        editor.string = "Existing draft"
        editor.setSelectedRange(NSRange(location: editor.string.utf16.count, length: 0))
        editor.keyDown(with: try slash())
        #expect(opens == 1)
        #expect(editor.string == "Existing draft/")
        editor.setDraftText("!echo test")
        editor.keyDown(with: try slash())
        #expect(opens == 1)
        #expect(editor.draftText == "!echo test/")
    }

    @Test func insertedOrPastedSlashDoesNotOpenPalette() {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        var opens = 0
        editor.openPalette = { opens += 1 }
        editor.insertText("/", replacementRange: NSRange(location: 0, length: 0))
        #expect(editor.string == "/")
        #expect(opens == 0)
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
