import AppKit
@preconcurrency import CodeEditSourceEditor
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

/// Clicking a waiting comment's chip brings its words into view, centred,
/// and opens its comment box at them to edit or delete (ADR 0070).
@MainActor @Suite(.serialized) struct TextCommentRevealTests {
    static let theme = MicaTheme(dark: false)

    @Test func aReplysWordsScrollToTheMiddleAndTheBoxOpensAtThemToEditAndDelete() async throws {
        let fixture = try await TranscriptWindow(messages: Self.conversation(target: "Retries happen every 2 seconds with no backoff."))
        defer { fixture.close() }
        let comments = fixture.workspace.textComments
        let id = try #require(comments.add(comment: "Back off exponentially.", source: "Assistant reply",
                                           quote: "every 2 seconds", scope: "message:m20", pane: .conversation))
        // The transcript starts at the latest message, far below the words.
        let mark = try #require(comments.item(id)).mark
        #expect(TextCommentMarkFinder.find(mark, in: fixture.adapter.table) == nil)

        await fixture.open(id)
        let words = try fixture.wordsInViewport(mark)
        #expect(abs(words.midY - fixture.viewport.midY) <= 2, "words \(words), viewport \(fixture.viewport)")

        // The box opens below the words, lined up with them, with the
        // comment ready to edit.
        let panel = try #require(fixture.popover)
        let screenWords = try fixture.wordsOnScreen(mark)
        #expect(abs(panel.frame.maxY - (screenWords.minY - AnchoredPopover.gap)) <= 1, "\(panel.frame) \(screenWords)")
        #expect(abs(panel.frame.minX - screenWords.minX) <= 1)
        let editor = try await Self.focusedEditor(in: panel)
        #expect(editor.string == "Back off exponentially.")
        #expect(editor.selectedRange() == NSRange(location: 23, length: 0))

        // Save keeps the edit; the box goes, and the words stay marked.
        editor.insertText(" Cap it at 30s.", replacementRange: editor.selectedRange())
        try Self.pressReturn(in: panel)
        try await eventually { comments.item(id)?.annotation.comment == "Back off exponentially. Cap it at 30s." }
        try await eventually { fixture.popover == nil }
        _ = try fixture.wordsInViewport(mark)

        // Delete removes it, and its mark.
        await fixture.open(id)
        let again = try #require(fixture.popover)
        _ = try await Self.focusedEditor(in: again)
        // Delete leads the box's bottom row, in from its corner.
        try Self.click(NSPoint(x: 32, y: 21), in: again)
        try await eventually { comments.item(id) == nil }
        try await eventually { fixture.popover == nil }
        try await eventually { TextCommentMarkFinder.find(mark, in: fixture.adapter.table) == nil }
    }

    @Test func wordsInACollapsedToolGroupOpenItsRowAndScrollItsWell() async throws {
        let output = (1...40).map { $0 == 30 ? "--- FAIL: TestPairRetries (30.02s)" : "ok   line \($0)" }.joined(separator: "\n")
        let tools = (0..<6).map { index in
            ToolActivity(id: "t\(index)", name: "fetch", summary: "go test ./pair/\(index)",
                         output: index == 4 ? output : "ok", failed: false, status: "Completed")
        }
        var messages = Self.conversation(target: "Here are the results.")
        messages.insert(TranscriptMessage(id: "tools", role: "tools", text: "", tools: tools), at: 21)
        let fixture = try await TranscriptWindow(messages: messages)
        defer { fixture.close() }
        let drawer = fixture.presentation.drawer(for: "tools")
        #expect(drawer.disclosure(count: 6) == .group(expanded: false))
        let comments = fixture.workspace.textComments
        let id = try #require(comments.add(comment: "Same bug as the timeout.", source: "Tool output · fetch",
                                           quote: "--- FAIL: TestPairRetries (30.02s)",
                                           scope: TextCommentSource.toolScope("t4"), pane: .conversation))
        let mark = try #require(comments.item(id)).mark

        await fixture.open(id)
        #expect(drawer.disclosure(count: 6) == .group(expanded: true))
        #expect(drawer.selectedTool == "t4")
        let words = try fixture.wordsInViewport(mark)
        #expect(abs(words.midY - fixture.viewport.midY) <= 2, "words \(words), viewport \(fixture.viewport)")
        // The well scrolled to the failing line, below its first fourteen.
        let (field, rect) = try #require(TextCommentMarkFinder.find(mark, in: fixture.adapter.table))
        let well = try #require(field.enclosingScrollView)
        #expect(well !== fixture.adapter.scroll)
        #expect(well.contentView.bounds.minY > 0)
        #expect(well.contentView.bounds.contains(well.contentView.convert(rect, from: field)))
        let panel = try #require(fixture.popover)
        let screenWords = try fixture.wordsOnScreen(mark)
        #expect(abs(panel.frame.maxY - (screenWords.minY - AnchoredPopover.gap)) <= 1)
        let editor = try await Self.focusedEditor(in: panel)
        #expect(editor.string == "Same bug as the timeout.")
    }

    @Test func wordsInHistoryThatsGoneOpenTheBoxAtTheChip() async throws {
        let fixture = try await TranscriptWindow(messages: Self.conversation(target: "Nothing to see."))
        defer { fixture.close() }
        let comments = fixture.workspace.textComments
        let id = try #require(comments.add(comment: "Still true?", source: "Assistant reply", quote: "an old answer",
                                           scope: "message:released", pane: .conversation))
        await fixture.open(id)
        let panel = try #require(fixture.popover)
        // The chip sits low on the screen, so the box opens above it.
        #expect(abs(panel.frame.minY - (fixture.chip.maxY + AnchoredPopover.gap)) <= 1, "\(panel.frame) \(fixture.chip)")
        #expect(abs(panel.frame.minX - fixture.chip.minX) <= 1)
        let editor = try await Self.focusedEditor(in: panel)
        #expect(editor.string == "Still true?")
    }

    @Test func aConversationThatEndedOpensTheBoxAtTheChipWithoutATab() async throws {
        let fixture = try await TranscriptWindow(messages: Self.conversation(target: "Nothing to see."))
        defer { fixture.close() }
        let comments = fixture.workspace.textComments
        let id = try #require(comments.add(comment: "Ask it why.", source: "Assistant reply", quote: "the plan",
                                           scope: "message:sub-1", pane: .agent("reviewer")))
        await fixture.open(id, canOpen: { _ in false })
        #expect(!fixture.workspace.panes.contains(.agent("reviewer")))
        let panel = try #require(fixture.popover)
        #expect(abs(panel.frame.minX - fixture.chip.minX) <= 1)
    }

    @Test func scratchpadWordsOpenItsTabAndScrollToTheMiddle() async throws {
        let workspace = WorkspaceState(demo: false)
        let lines = (1...120).map { $0 == 90 ? "todo: wire the retry budget" : "line \($0)" }
        workspace.scratchpadState.observe(ScratchpadRecord(owner: "root", content: lines.joined(separator: "\n"),
            revision: 1, updatedAt: "2026-09-20T12:00:00Z"))
        let comments = workspace.textComments
        let context = lines.joined(separator: "\n")
        let id = try #require(comments.add(comment: "Do this first.", source: "Scratchpad", quote: "wire the retry budget",
            scope: TextCommentSource.scratchpadScope,
            locator: TextCommentLocator(context: context, occurrence: 0), pane: .scratchpad))
        #expect(!workspace.panes.contains(.scratchpad))
        let host = NSHostingController(rootView: RevealScratchpad(workspace: workspace))
        host.sizingOptions = []
        let window = NSWindow(testContentRect: NSRect(x: 200, y: 200, width: 480, height: 300))
        window.contentViewController = host; window.setContentSize(NSSize(width: 480, height: 300))
        window.orderFront(nil)
        defer { comments.popover.dismiss(); window.close() }
        try await Task.sleep(for: .milliseconds(300))
        await workspace.openTextComment(id, canOpen: { _ in true }, chip: NSRect(x: 220, y: 210, width: 80, height: 20),
                                        window: window, theme: Self.theme)
        #expect(workspace.isVisible(.scratchpad))
        let controller = try #require(Self.editor(host.view))
        let range = (controller.textView.string as NSString).range(of: "wire the retry budget")
        let boxes = controller.textView.layoutManager.rectsFor(range: range)
        let words = try #require(boxes.first)
        let visible = controller.scrollView.contentView.bounds
        #expect(visible.minY > 0)
        #expect(visible.contains(words))
        #expect(abs(words.midY - visible.midY) <= 20, "words \(words), visible \(visible)")
        let panel = try #require(window.childWindows?.first)
        let screenWords = window.convertToScreen(controller.textView.convert(words, to: nil))
        #expect(abs(panel.frame.maxY - (screenWords.minY - AnchoredPopover.gap)) <= 1, "\(panel.frame) \(screenWords)")
        let editor = try await Self.focusedEditor(in: panel)
        #expect(editor.string == "Do this first.")
    }

    // MARK: Helpers

    /// Forty messages, the twentieth an assistant reply saying target.
    static func conversation(target: String) -> [TranscriptMessage] {
        (0..<40).map { index in
            let text = index == 20 ? target
                : "Message \(index). " + String(repeating: "Some words to fill the line. ", count: 6)
            return TranscriptMessage(id: "m\(index)", role: index.isMultiple(of: 2) ? "assistant" : "user", text: text, tools: [])
        }
    }

    static func focusedEditor(in window: NSWindow) async throws -> ComposerTextView {
        try await eventually { window.firstResponder is ComposerTextView }
        return try #require(window.firstResponder as? ComposerTextView)
    }

    static func pressReturn(in window: NSWindow) throws {
        for type in [NSEvent.EventType.keyDown, .keyUp] {
            window.sendEvent(try #require(NSEvent.keyEvent(with: type, location: .zero, modifierFlags: [], timestamp: 0,
                windowNumber: window.windowNumber, context: nil, characters: "\r", charactersIgnoringModifiers: "\r",
                isARepeat: false, keyCode: 36)))
        }
    }

    /// Clicks point, in window's coordinates.
    static func click(_ point: NSPoint, in window: NSWindow) throws {
        for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
            window.sendEvent(try #require(NSEvent.mouseEvent(with: type, location: point, modifierFlags: [], timestamp: 0,
                windowNumber: window.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1)))
        }
    }

    static func editor(_ view: NSView) -> TextViewController? {
        if let value = view.nextResponder as? TextViewController { return value }
        return view.subviews.compactMap(editor).first
    }
}

/// A transcript in a window, scrolled to its latest message, with a chip
/// near the bottom of the screen.
@MainActor private final class TranscriptWindow {
    let workspace = WorkspaceState(demo: false)
    let presentation = TranscriptPresentationState()
    let adapter: NativeTranscriptCoordinator
    let window: NSWindow

    init(messages: [TranscriptMessage]) async throws {
        _ = NSApplication.shared
        let input = NativeTranscript(messages: messages, hasHistory: false, historyLoading: false, historyError: nil,
            active: false, presentation: presentation, workspace: workspace, resumeRequest: 0,
            latestOutOfView: .constant(false), loadHistory: {}, theme: TextCommentRevealTests.theme)
        adapter = NativeTranscriptCoordinator(input)
        window = NSWindow(testContentRect: NSRect(x: 200, y: 200, width: 900, height: 600))
        window.contentView = adapter.scroll
        window.orderFront(nil)
        adapter.receive(input)
        for _ in 0..<10 {
            adapter.scroll.layoutSubtreeIfNeeded()
            try await Task.sleep(for: .milliseconds(30))
        }
    }

    func close() {
        workspace.textComments.popover.dismiss()
        adapter.stop()
        window.close()
    }

    /// The chip, on screen: low in the window.
    var chip: NSRect {
        NSRect(x: window.frame.minX + 40, y: (window.screen?.visibleFrame.minY ?? 0) + 20, width: 120, height: 22)
    }

    func open(_ id: UUID, canOpen: (WorkspacePane) -> Bool = { _ in true }) async {
        await workspace.openTextComment(id, canOpen: canOpen, chip: chip, window: window, theme: TextCommentRevealTests.theme)
    }

    /// The comment box, while it's open.
    var popover: NSWindow? { window.childWindows?.first { $0.isVisible } }

    var viewport: NSRect { adapter.scroll.contentView.bounds }

    /// The marked words, in the transcript's clip view, after checking
    /// they're shown in full.
    func wordsInViewport(_ mark: TextCommentMark) throws -> NSRect {
        let (field, rect) = try #require(TextCommentMarkFinder.find(mark, in: adapter.table))
        let words = adapter.scroll.contentView.convert(rect, from: field)
        #expect(viewport.contains(words), "words \(words), viewport \(viewport)")
        return words
    }

    func wordsOnScreen(_ mark: TextCommentMark) throws -> NSRect {
        let (field, rect) = try #require(TextCommentMarkFinder.find(mark, in: adapter.table))
        return window.convertToScreen(field.convert(rect, to: nil))
    }
}

private struct RevealScratchpad: View {
    @Bindable var workspace: WorkspaceState
    var body: some View {
        ScratchpadEditor(text: $workspace.scratchpad, documentVersion: workspace.scratchpadState.documentVersion,
                         editorState: $workspace.scratchpadPosition)
            .environment(\.textComments, workspace.textComments)
            .environment(\.mica, TextCommentRevealTests.theme)
    }
}
