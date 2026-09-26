import AppKit
import Foundation
import SwiftUI
import Testing
@testable import Kit

private struct HistoryTestClient: ComposerHistoryClient {
    let serverID = "test"
    let isDemo = false
    var messages: [ComposerMessageHistoryEntry] = []
    var olderMessages: ComposerMessageHistoryPage?
    var firstBash: ComposerBashHistoryPage = .init(entries: [], nextCursor: nil, hasMore: false)
    var olderBash: ComposerBashHistoryPage = .init(entries: [], nextCursor: nil, hasMore: false)
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func messageHistory(_ session: String, before: String?) async throws -> ComposerMessageHistoryPage {
        if before != nil { return olderMessages ?? .init(entries: [], nextCursor: nil, hasMore: false) }
        return .init(entries: messages, nextCursor: olderMessages == nil ? nil : "2", hasMore: olderMessages != nil)
    }
    func bashHistory(_ session: String, before: String?, limit: Int) async throws -> ComposerBashHistoryPage {
        before == nil ? firstBash : olderBash
    }
}

@MainActor
struct ComposerHistoryTests {
    private func settle(_ condition: @escaping @MainActor () -> Bool) async throws {
        for _ in 0..<100 {
            if condition() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        Issue.record("history operation did not settle")
    }

    @Test func messageHistoryFiltersAndInsertsWithoutSubmitting() async throws {
        let client = HistoryTestClient(messages: [
            .init(id: "new", text: "newest prompt"), .init(id: "old", text: "older prompt")
        ])
        let state = ComposerHistoryState()
        state.openMessages(session: "session_1", client: client)
        try await settle { !state.loading }
        state.appendQuery("older")
        #expect(state.selected == .message(.init(id: "old", text: "older prompt")))

        let editor = ComposerTextView(frame: .zero)
        editor.history = state
        var submitted = false
        editor.submit = { submitted = true }
        editor.insertHistory()
        #expect(editor.draftText == "older prompt")
        #expect(!submitted)
        #expect(!state.isOpen)
    }

    @Test func messageHistoryPagesAndDeduplicatesExactText() async throws {
        let client = HistoryTestClient(messages: [.init(id: "new", text: "same"), .init(id: "next", text: "different")],
            olderMessages: .init(entries: [.init(id: "duplicate", text: "same"), .init(id: "old", text: "same ")],
                nextCursor: nil, hasMore: false))
        let state = ComposerHistoryState()
        state.openMessages(session: "session_1", client: client)
        try await settle { !state.loading }
        state.move(1, session: "session_1", client: client)
        state.move(1, session: "session_1", client: client)
        try await settle { !state.loading && state.entries.count == 3 }
        #expect(state.selected == .message(.init(id: "old", text: "same ")))
        #expect(state.entries.count == 3)
    }

    @Test func bashHistoryRestoresOriginalContextModeAndLoadsOlderPage() async throws {
        let client = HistoryTestClient(
            firstBash: .init(entries: [.init(id: "new", sequence: 2, command: "pwd", excluded: false)], nextCursor: "2", hasMore: true),
            olderBash: .init(entries: [.init(id: "old", sequence: 1, command: "echo secret", excluded: true)], nextCursor: nil, hasMore: false)
        )
        let state = ComposerHistoryState()
        state.openBash(session: "session_1", query: "", client: client)
        try await settle { !state.loading }
        state.move(1, session: "session_1", client: client)
        try await settle { !state.loading && state.entries.count == 2 }
        #expect(state.selected == .bash(.init(id: "old", sequence: 1, command: "echo secret", excluded: true)))

        let editor = ComposerTextView(frame: .zero)
        editor.history = state
        editor.insertHistory()
        #expect(editor.draftText == "!!echo secret")
        #expect(editor.shellPrefix == "!!")
        #expect(editor.string == "echo secret")
    }

    @Test func filteredBashHistoryPagesUntilAnOlderMatchAppears() async throws {
        let client = HistoryTestClient(
            firstBash: .init(entries: [.init(id: "new", sequence: 3, command: "pwd", excluded: false)], nextCursor: "3", hasMore: true),
            olderBash: .init(entries: [
                .init(id: "first", sequence: 2, command: "echo first secret", excluded: false),
                .init(id: "second", sequence: 1, command: "echo second secret", excluded: true)
            ], nextCursor: nil, hasMore: false)
        )
        let state = ComposerHistoryState()
        state.openBash(session: "session_1", query: "secret", client: client)
        try await settle { !state.loading && state.entries.count == 3 }
        #expect(state.selected == .bash(.init(id: "first", sequence: 2, command: "echo first secret", excluded: false)))
    }

    @Test func openHistoryDoesNotRepeatedlyInvalidateClosedCompletionState() {
        let mentions = ComposerMentionState()
        let history = ComposerHistoryState()
        history.openMessages(session: "session_1", client: HistoryTestClient())
        let parent = GrowingComposerEditor(text: .constant(""), focused: .constant(false), focusRequest: 0,
            foreground: .primary, placeholderColor: .secondary, attachmentDrop: { _ in false },
            attachmentDropTargeted: { _ in }, submit: {}, mentions: mentions, history: history)
        let coordinator = parent.makeCoordinator()
        let editor = ComposerTextView(frame: .zero)
        let revision = mentions.revision
        coordinator.renderMentions(editor)
        coordinator.renderMentions(editor)
        #expect(mentions.revision == revision)
    }

    @Test func shiftedTypingFiltersMessageHistoryWithoutEditingTheDraft() throws {
        let editor = ComposerTextView(frame: .zero)
        let state = ComposerHistoryState()
        editor.history = state
        state.openMessages(session: "session_1", client: HistoryTestClient())
        let shiftedA = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: .shift,
            timestamp: 0, windowNumber: 0, context: nil, characters: "A", charactersIgnoringModifiers: "a",
            isARepeat: false, keyCode: 0))
        editor.keyDown(with: shiftedA)
        #expect(state.query == "A")
        #expect(editor.string.isEmpty)
    }

    @Test func optionInputClosesMessageHistoryBeforeEditingTheDraft() throws {
        let editor = ComposerTextView(frame: .zero)
        let state = ComposerHistoryState()
        editor.history = state
        state.openMessages(session: "session_1", client: HistoryTestClient())
        let optionE = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: .option,
            timestamp: 0, windowNumber: 0, context: nil, characters: "´", charactersIgnoringModifiers: "e",
            isARepeat: false, keyCode: 14))
        editor.keyDown(with: optionE)
        #expect(!state.isOpen)
        #expect(state.query.isEmpty)
    }

    @Test func escapeDismissesHistoryWithoutExitingShellOrSubmitting() throws {
        let editor = ComposerTextView(frame: .zero)
        let state = ComposerHistoryState()
        editor.history = state; editor.historySession = "session_1"; editor.historyClient = HistoryTestClient()
        editor.setDraftText("!!echo keep")
        state.openBash(session: "session_1", query: "echo keep", client: editor.historyClient)
        var exited = false, submitted = false
        editor.exitShell = { exited = true }; editor.submit = { submitted = true }
        let escape = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: 0, context: nil, characters: "\u{1b}", charactersIgnoringModifiers: "\u{1b}", isARepeat: false, keyCode: 53))
        editor.keyDown(with: escape)
        #expect(!state.isOpen)
        #expect(editor.draftText == "!!echo keep")
        #expect(!exited && !submitted)
    }

    @Test func queuedFollowUpTakesPrecedenceOverMessageHistory() throws {
        let editor = ComposerTextView(frame: .zero)
        let state = ComposerHistoryState()
        editor.history = state; editor.historySession = "session_1"; editor.historyClient = HistoryTestClient()
        var recalled = false
        editor.recallQueued = { recalled = true; return true }
        let up = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: 0, context: nil, characters: "", charactersIgnoringModifiers: "", isARepeat: false, keyCode: 126))
        editor.keyDown(with: up)
        #expect(recalled)
        #expect(!state.isOpen)
    }

    @Test func arrowsWithinMultilineShellDraftKeepNativeCaretMovement() throws {
        let editor = ComposerTextView(frame: NSRect(x: 0, y: 0, width: 400, height: 100))
        let state = ComposerHistoryState()
        editor.history = state; editor.historySession = "session_1"; editor.historyClient = HistoryTestClient()
        editor.setDraftText("!first\nsecond")
        editor.setSelectedRange(NSRange(location: 7, length: 0))
        let up = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: 0, context: nil, characters: "", charactersIgnoringModifiers: "", isARepeat: false, keyCode: 126))
        editor.keyDown(with: up)
        #expect(!state.isOpen)
        editor.setSelectedRange(NSRange(location: 2, length: 0))
        let down = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: 0, context: nil, characters: "", charactersIgnoringModifiers: "", isARepeat: false, keyCode: 125))
        editor.keyDown(with: down)
        #expect(!state.isOpen)
    }

    @Test func upOnEmptyComposerOpensMessageHistoryAndShellArrowOpensBashHistory() throws {
        let client = HistoryTestClient()
        let editor = ComposerTextView(frame: .zero)
        let state = ComposerHistoryState()
        editor.history = state; editor.historySession = "session_1"; editor.historyClient = client
        let up = try #require(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: 0,
            windowNumber: 0, context: nil, characters: "", charactersIgnoringModifiers: "", isARepeat: false, keyCode: 126))
        editor.keyDown(with: up)
        #expect(state.mode == .messages)
        state.close()
        editor.setDraftText("!git status")
        editor.keyDown(with: up)
        #expect(state.mode == .bash)
        #expect(state.query == "git status")
    }
}
