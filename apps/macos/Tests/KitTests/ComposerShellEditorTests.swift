import AppKit
import Testing
import SwiftUI
@testable import Kit

@MainActor struct ComposerShellEditorTests {
    @Test func backspaceAtStartIncludesCommandAndOtherwiseDeletesText() {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 500, height: 100))
        for command in ["", "printf hello"] {
            editor.setDraftText("!!" + command)
            editor.setSelectedRange(NSRange(location: 0, length: 0))
            editor.deleteBackward(nil)
            #expect(editor.draftText == "!" + command)
            #expect(editor.string == command)
            #expect(editor.selectedRange() == NSRange(location: 0, length: 0))
        }
        editor.setDraftText("!!printf hello")
        editor.setSelectedRange(NSRange(location: editor.string.utf16.count, length: 0))
        editor.deleteBackward(nil)
        #expect(editor.draftText == "!!printf hell")
        editor.setSelectedRange(NSRange(location: 0, length: 6))
        editor.deleteBackward(nil)
        #expect(editor.draftText == "!! hell")
    }
    @Test func shellLayoutDoesNotRepeatedlyInvalidateClosedCompletionState() {
        let mentions = ComposerMentionState()
        let parent = GrowingComposerEditor(text: .constant("!"), focused: .constant(false), focusRequest: 0,
            foreground: .primary, placeholderColor: .secondary, attachmentDrop: { _ in false },
            attachmentDropTargeted: { _ in }, submit: {}, mentions: mentions)
        let coordinator = parent.makeCoordinator()
        let editor = ComposerTextView(frame: .zero)
        editor.setDraftText("!")
        let revision = mentions.revision
        coordinator.renderMentions(editor)
        coordinator.renderMentions(editor)
        #expect(mentions.revision == revision)
    }
    @Test func prefixesAreHiddenWithoutTrimmingCommandOrMovingItsCaret() {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 500, height: 100))
        editor.string = "!printf 'héllo'\n  pwd"
        editor.setSelectedRange(NSRange(location: editor.string.utf16.count, length: 0))
        editor.setDraftText(editor.draftText)
        #expect(editor.string == "printf 'héllo'\n  pwd")
        #expect(editor.shellPrefix == "!")
        #expect(editor.draftText == "!printf 'héllo'\n  pwd")
        #expect(editor.selectedRange().location == editor.string.utf16.count)
        editor.setDraftText("!!" + editor.string)
        #expect(editor.string == "printf 'héllo'\n  pwd")
        #expect(editor.shellPrefix == "!!")
        editor.setDraftText(editor.string)
        #expect(editor.shellPrefix == "")
        #expect(editor.string == "printf 'héllo'\n  pwd")
    }
    @Test func secondBangSelectsExclusionAndEscapeExits() throws {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 500, height: 100))
        editor.setDraftText("!")
        editor.string = "!"
        editor.setDraftText(editor.draftText)
        #expect(editor.string == "")
        #expect(editor.draftText == "!!")
        var exited = false
        editor.exitShell = { exited = true }
        let escape = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: 0, context: nil, characters: "\u{1b}", charactersIgnoringModifiers: "\u{1b}", isARepeat: false, keyCode: 53))
        editor.keyDown(with: escape)
        #expect(exited)
    }
    @Test func bashHighlightingUsesTemporaryColorsAndPreservesSelection() async throws {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 500, height: 100))
        editor.setDraftText("!printf 'hello'")
        editor.setSelectedRange(NSRange(location: 3, length: 2))
        let spans = try await CodeSyntaxHighlighter.shared.spans(for: .init(source: editor.string, language: "bash"))
        let stringSpan = try #require(spans.first { $0.role == "string" })
        let theme = MicaTheme(dark: false)
        editor.applyShellHighlighting(spans, theme: theme)
        let color = editor.layoutManager?.temporaryAttribute(.foregroundColor, atCharacterIndex: stringSpan.range.location, effectiveRange: nil) as? NSColor
        #expect(color == NSColor(theme.syntax("string", fallback: theme.success)))
        #expect(editor.draftText == "!printf 'hello'")
        #expect(editor.selectedRange() == NSRange(location: 3, length: 2))
    }
}
