import AppKit
import Foundation
import KitMermaid
import KitTranscript
import SwiftUI
import Testing
@testable import Kit

/// A server that takes sends, answering each at once.
private actor SendingClient: SessionMutationClient, ComposerClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var calls: [WirePromptInput] = []

    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        var value = SessionExcerpt(id: id, title: "Test", sourceTitle: "Test", model: "m", thinking: "high", workspace: "w",
                                   date: "", messages: [])
        value.followUps = try FollowUpState(WireFollowUpQueue(count: 0, previews: [], annotationIds: nil))
        return value
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(try snapshot(id))
        while !Task.isCancelled { try await Task.sleep(for: .seconds(1)) }
    }
    func submit(_ session: String, input: WirePromptInput) async throws -> WirePromptSubmission {
        calls.append(input)
        return WirePromptSubmission(reservation: WireTurnReservation(sessionId: session, turnId: "turn"), queued: false,
                                    queue: WireFollowUpQueue(count: 0, previews: [], annotationIds: nil))
    }
    func restoreFollowUps(_ session: String) async throws -> WireRestoreFollowUpsResult { throw ClientError.http(404) }
    func promoteFollowUps(_ session: String) async throws -> WirePromoteFollowUpsResult { throw ClientError.http(404) }
    func abort(_ session: String, turn: String) async throws {}
    func models() async throws -> [WireModelCapability] { [] }
    func files(_ session: String) async throws -> [String] { [] }
    func configure(_ session: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult { throw ClientError.http(409) }
    func respond(_ session: String, input: WireInteractionResponse) async throws {}
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution {
        .init(attachments: [], missingAttachmentIds: [])
    }
    func upload(_ session: String, filename: String, data: Data) async throws -> WireAttachmentInfo {
        throw ClientError.http(500)
    }
}

/// Comments on selected text (ADR 0070) in the macOS client.
@MainActor struct TextCommentTests {
    // MARK: Waiting comments

    @Test func commentsAreAddedEditedRemovedAndMarkTheirScope() throws {
        let draft = TextCommentDraft()
        let first = try #require(draft.add(comment: " Back off. ", source: "Assistant reply", quote: "every 2 seconds", scope: "message:1"))
        draft.add(comment: "Same bug.", source: "Tool output · go test", quote: "--- FAIL", scope: nil)
        #expect(draft.items.map(\.annotation.comment) == ["Back off.", "Same bug."])
        #expect(draft.marks(in: "message:1") == [TextCommentMark(id: first, quote: "every 2 seconds")])
        draft.update(first, comment: "Back off exponentially.")
        #expect(draft.item(first)?.annotation.comment == "Back off exponentially.")
        draft.update(first, comment: "  ")
        #expect(draft.items.map(\.annotation.comment) == ["Same bug."])
        #expect(draft.marks(in: "message:1").isEmpty)
        draft.acknowledge(draft.items.map(\.id))
        #expect(draft.isEmpty)
    }

    @Test func aMessageHoldsAtMostSixteenComments() {
        let draft = TextCommentDraft()
        #expect(draft.add(comment: "", source: "Scratchpad", quote: "words", scope: nil) == nil)
        #expect(draft.add(comment: "Why?", source: "Scratchpad", quote: "  \n", scope: nil) == nil)
        for index in 0..<TextAnnotationText.limit {
            #expect(draft.add(comment: "\(index)", source: "Scratchpad", quote: "words", scope: nil) != nil)
        }
        #expect(draft.add(comment: "one more", source: "Scratchpad", quote: "words", scope: nil) == nil)
        #expect(draft.items.count == 16)
        #expect(draft.error == "A message can contain up to 16 comments on selected text.")
    }

    @Test func sendingPutsCommentBlocksLastAndClearsTheChipsOnceTheServerHasIt() async throws {
        let client = SendingClient()
        let session = try await client.snapshot("s")
        let store = SessionStore(fixture: Fixture(sessions: [session]), client: client)
        store.attach()
        defer { store.detach() }
        try await eventually { store.connectionState == .connected }
        store.ui.workspace.diagramAnnotations.add(comment: "Split this.", source: "flowchart TD\nA", part: Part(.node, 0), nonce: "d1")
        let comments = store.ui.workspace.textComments
        comments.add(comment: "Back off.", source: "Assistant reply", quote: "every 2 seconds", scope: "message:1")
        store.ui.draft = "Fix these."
        store.send()
        try await eventually { await !client.calls.isEmpty }
        let call = try #require(await client.calls.first)
        let split = TextAnnotationText.split(call.text)
        #expect(split.annotations == [.init(number: 1, comment: "Back off.", source: "Assistant reply",
                                            quote: "every 2 seconds", cut: false)])
        #expect(DiagramAnnotationText.split(split.text).text == "Fix these.")
        try await eventually { comments.isEmpty && store.ui.draft.isEmpty }
    }

    @Test func commentsAloneMakeAMessage() async throws {
        let client = SendingClient()
        let session = try await client.snapshot("s")
        let store = SessionStore(fixture: Fixture(sessions: [session]), client: client)
        store.attach()
        defer { store.detach() }
        try await eventually { store.connectionState == .connected }
        store.ui.workspace.textComments.add(comment: "Rename.", source: "Scratchpad", quote: "tmp", scope: nil)
        store.send()
        try await eventually { await !client.calls.isEmpty }
        #expect(await client.calls.first?.text.hasPrefix("<text-annotation n=\"1\">\nComment: Rename.\n") == true)
    }

    // MARK: Where a selection's comment goes

    @Test func aSelectionCoversTheLinesItTouches() {
        let content = "zero\none\ntwo\nthree"
        #expect(SelectionLineComments.lines(of: NSRange(location: 1, length: 2), in: content) == 0...0)
        #expect(SelectionLineComments.lines(of: NSRange(location: 6, length: 6), in: content) == 1...2)
        // Through a line's newline, but not into the next line.
        #expect(SelectionLineComments.lines(of: NSRange(location: 5, length: 4), in: content) == 1...1)
        #expect(SelectionLineComments.lines(of: NSRange(location: 0, length: 0), in: content) == nil)
        #expect(SelectionLineComments.lines(of: NSRange(location: 15, length: 9), in: content) == nil)
    }

    @Test func aSelectionInASourceRegionCommentsOnItsText() throws {
        let fixture = try RegionWindow()
        defer { fixture.window.close() }
        fixture.reply.setSelectedRange(NSRange(location: 0, length: 5))
        #expect(fixture.window.makeFirstResponder(fixture.reply))
        let selection = try #require(fixture.coordinator.selection(in: fixture.window))
        guard case .text(let source)? = fixture.coordinator.destination(for: selection, in: fixture.window) else {
            Issue.record("Expected a text comment"); return
        }
        #expect(source == TextCommentSource(label: "Assistant reply", scope: "message:1"))
    }

    @Test func aSelectionOutsideEverySourceRegionOffersNoComment() throws {
        let fixture = try RegionWindow()
        defer { fixture.window.close() }
        fixture.terminal.setSelectedRange(NSRange(location: 0, length: 5))
        #expect(fixture.window.makeFirstResponder(fixture.terminal))
        let selection = try #require(fixture.coordinator.selection(in: fixture.window))
        #expect(fixture.coordinator.destination(for: selection, in: fixture.window) == nil)
    }

    @Test func aSelectionInAnEditorWithLineAnnotationsBeginsOne() throws {
        let fixture = try RegionWindow()
        defer { fixture.window.close() }
        var begun: [NSRange] = []
        SelectionLineComments.register(fixture.terminal) { range in { begun.append(range) } }
        defer { SelectionLineComments.unregister(fixture.terminal) }
        fixture.terminal.setSelectedRange(NSRange(location: 6, length: 4))
        #expect(fixture.window.makeFirstResponder(fixture.terminal))
        let selection = try #require(fixture.coordinator.selection(in: fixture.window))
        guard case .lines(let begin)? = fixture.coordinator.destination(for: selection, in: fixture.window) else {
            Issue.record("Expected a line annotation"); return
        }
        begin()
        #expect(begun == [NSRange(location: 6, length: 4)])
    }

    @Test func theContextMenuOffersCommentFirst() throws {
        let fixture = try RegionWindow()
        defer { fixture.window.close() }
        fixture.reply.setSelectedRange(NSRange(location: 0, length: 5))
        #expect(fixture.window.makeFirstResponder(fixture.reply))
        let event = try #require(NSEvent.mouseEvent(with: .rightMouseDown, location: NSPoint(x: 20, y: 130), modifierFlags: [],
            timestamp: 0, windowNumber: fixture.window.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
        let menu = try #require(fixture.coordinator.commentMenu(for: event, in: fixture.window))
        #expect(menu.items.first?.title == "Comment")
        #expect(menu.items.dropFirst().first?.isSeparatorItem == true)
        #expect(menu.items.contains { $0.action == #selector(NSText.copy(_:)) })
    }

    // MARK: Marks

    @Test func markedWordsTakeAWashAnUnderlineAndALinkToTheirComment() throws {
        let theme = MicaTheme(dark: true)
        let id = UUID()
        let mark = TextCommentMark(id: id, quote: "every 2 seconds")
        let text = AttributedString("Retries every 2 seconds with no backoff.")
        let marked = TextCommentMarking.marked(text, [mark], theme: theme)
        let runs = marked.runs.map { (String(marked[$0.range].characters), $0.link) }
        #expect(runs.map(\.0) == ["Retries ", "every 2 seconds", " with no backoff."])
        #expect(runs.map(\.1) == [nil, mark.url, nil])
        let run = try #require(marked.runs.first { $0.link != nil })
        #expect(run.backgroundColor == theme.accent.opacity(0.24))
        #expect(run.underlineStyle == Text.LineStyle(pattern: .solid, color: theme.accent))
        #expect(TextCommentMark.id(in: mark.url) == id)
    }

    @Test func aMarkHoversWithItsComment() throws {
        let draft = TextCommentDraft()
        let id = try #require(draft.add(comment: "Back off exponentially.", source: "Assistant reply", quote: "every 2 seconds",
                                        scope: "message:1"))
        #expect(TextLinkHover.toolTip(for: TextCommentMark(id: id, quote: "every 2 seconds").url) == "Back off exponentially.")
    }

    // MARK: Sent messages

    @Test func aSentMessagesCommentsAreCountedUnderIt() {
        let one = TextAnnotationText.Sent(number: 1, comment: "Back off.", source: "Assistant reply", quote: "every 2 seconds", cut: false)
        #expect(TranscriptTextComments.title([one]) == "1 comment")
        #expect(TranscriptTextComments.title([one, one]) == "2 comments")
    }
}

/// A window with a reply in a comment source region above a text view in
/// no region, as a terminal is.
@MainActor private struct RegionWindow {
    let window: NSWindow
    let reply: NSTextView
    let terminal: NSTextView
    let coordinator = SelectionActions.Coordinator(theme: MicaTheme(dark: true), comments: TextCommentDraft())

    init() throws {
        _ = NSApplication.shared
        let content = NSView(frame: NSRect(x: 0, y: 0, width: 300, height: 200))
        let region = CommentSourceView(frame: NSRect(x: 0, y: 100, width: 300, height: 100))
        region.source = TextCommentSource(label: "Assistant reply", scope: "message:1")
        reply = NSTextView(frame: NSRect(x: 0, y: 100, width: 300, height: 100))
        reply.string = "alpha beta"
        terminal = NSTextView(frame: NSRect(x: 0, y: 0, width: 300, height: 100))
        terminal.string = "alpha\nbeta\ngamma"
        content.addSubview(region)
        content.addSubview(reply)
        content.addSubview(terminal)
        window = NSWindow(testContentRect: NSRect(x: -10000, y: -10000, width: 300, height: 200))
        window.contentView = content
    }
}
