import AppKit
import KitDictation
import SwiftUI
import Testing
@testable import Kit

/// Dictating into the composer: where the mic sits, the recording strip in
/// the editor's place, and the words landing in the draft. Set
/// KIT_SNAPSHOT_DIR to write the renders there for review.
@MainActor @Suite(.serialized) struct DictationPresentationTests {
    private static let said = "Run the reconnect test 20 times in a row."

    @Test func theMicTakesSendsSquareOnlyWithNothingToSendOrStop() {
        typealias Context = ComposerMic.Context
        #expect(ComposerMic.placement(Context()) == .sendSlot)
        #expect(ComposerMic.placement(Context(draft: "  \n")) == .sendSlot)
        #expect(ComposerMic.placement(Context(stoppable: true)) == .beside)
        #expect(ComposerMic.placement(Context(hasOtherContent: true)) == .beside)
        #expect(ComposerMic.placement(Context(draft: "Run the tests")) == .hidden)
        #expect(ComposerMic.placement(Context(shell: true)) == .hidden)
        #expect(ComposerMic.placement(Context(unavailable: true)) == .hidden)
        // A recording keeps the square, whatever happens to the draft or the turn meanwhile.
        #expect(ComposerMic.placement(Context(dictating: true, draft: "Quoted", stoppable: true)) == .sendSlot)
    }

    @Test func dictatedWordsJoinTheDraftAndFocusTheEditorAtTheEnd() {
        let ui = SessionUIState(demo: true)
        ui.takeDictation("Run the tests.")
        #expect(ui.draft == "Run the tests.")
        #expect(ui.composerFocus == 1)
        #expect(ui.composerFocusAtEndRequest == 1)
        ui.takeDictation("Then the linters.")
        #expect(ui.draft == "Run the tests. Then the linters.")
        #expect(ui.composerFocusAtEndRequest == 2)
    }

    @Test func theStripKeepsTheComposersHeightAndStoppingPutsTheWordsInTheEditor() async throws {
        guard #available(macOS 26, *) else { return }
        for dark in [false, true] {
            let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(10)))
            let state = store()
            let composer = try await Rendered(state, slot: slot, dark: dark)
            defer { composer.close() }
            try composer.snapshot("idle")
            let idle = composer.height
            #expect(composer.editor != nil)

            #expect(await slot.dictation.start())
            try await composer.settle()
            try composer.snapshot("listening")
            #expect(composer.editor == nil)
            #expect(composer.height == idle, "\(composer.height) vs \(idle)")

            try composer.press(.return)
            try await eventually { slot.dictation.phase == .idle && !state.ui.draft.isEmpty }
            try await composer.settle()
            try composer.snapshot("filled")
            let editor = try #require(composer.editor)
            #expect(editor.string == Self.said)
            #expect(composer.window.firstResponder === editor)
            #expect(editor.selectedRange() == NSRange(location: Self.said.utf16.count, length: 0))
        }
    }

    @Test func aProblemShowsOnALineAboveTheEditor() async throws {
        guard #available(macOS 26, *) else { return }
        for dark in [false, true] {
            let refused = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, access: .denied))
            #expect(await !refused.dictation.start())
            #expect(refused.dictation.problem == .notAllowed)
            let silent = DictationSlot(engine: ScriptedDictationEngine(transcript: " ", delay: .milliseconds(10)))
            #expect(await silent.dictation.start())
            #expect(await silent.dictation.finish() == nil)
            #expect(silent.dictation.problem == .nothingHeard)

            let plain = try await Rendered(store(), slot: DictationSlot(engine: ScriptedDictationEngine(transcript: "")), dark: dark)
            let notAllowed = try await Rendered(store(), slot: refused, dark: dark)
            let nothingHeard = try await Rendered(store(), slot: silent, dark: dark)
            defer { [plain, notAllowed, nothingHeard].forEach { $0.close() } }
            try notAllowed.snapshot("not-allowed")
            try nothingHeard.snapshot("nothing-heard")
            // One 12-point line and the 10-point gap below it.
            #expect(notAllowed.height - plain.height == nothingHeard.height - plain.height)
            #expect(nothingHeard.height - plain.height >= 24)
        }
    }

    @Test func typingClearsWhatDictationLastSaid() async throws {
        guard #available(macOS 26, *) else { return }
        let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: " ", delay: .milliseconds(10)))
        #expect(await slot.dictation.start())
        #expect(await slot.dictation.finish() == nil)
        let state = store()
        let composer = try await Rendered(state, slot: slot, dark: false)
        defer { composer.close() }
        state.ui.draft = "R"
        try await eventually { slot.dictation.problem == nil }
    }

    @Test func returnAndEscWhileGettingReadyActOnDictationNotTheAnswer() async throws {
        guard #available(macOS 26, *) else { return }
        for key in [Rendered.Key.return, .escape] {
            let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .seconds(2)))
            let flow = InteractionFlow(title: "Release notes", questions: [
                .init(prompt: "What should the release note say?", kind: .text),
            ])
            flow.answers[0].text = "Kit"
            var finished: [Bool] = []
            let dock = try await Rendered(InteractionDock(flow: flow, pendingCount: 1, finish: { finished.append($0) }),
                                          slot: slot, dark: false, width: 440)
            defer { dock.close() }
            let field = try #require(dock.answerField)
            try await eventually { field.currentEditor() != nil }
            let starting = Task { await slot.dictation.start() }
            try await eventually { slot.dictation.isPreparing }
            try dock.press(key)
            try await dock.settle()
            #expect(finished == [])
            #expect(flow.submitting == false)
            #expect(slot.dictation.isPreparing == (key == .return))
            slot.reset()
            #expect(await starting.value == false)
        }
    }

    @Test func aNewQuestionClearsWhatDictationLastSaid() async throws {
        guard #available(macOS 26, *) else { return }
        let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, access: .denied))
        let flow = InteractionFlow(title: "Release notes", guided: true, questions: [
            .init(prompt: "Title?", kind: .text, required: false),
            .init(prompt: "Summary?", kind: .text, required: false),
        ])
        let dock = try await Rendered(InteractionDock(flow: flow, pendingCount: 1, finish: { _ in }),
                                      slot: slot, dark: false, width: 440)
        defer { dock.close() }
        #expect(await !slot.dictation.start())
        #expect(slot.dictation.problem == .notAllowed)
        #expect(!flow.next())
        #expect(flow.step == 1)
        try await eventually { slot.dictation.problem == nil }
    }

    @Test func aDockAnswerIsDictatedIntoItsFieldWithTheCaretAfterTheWords() async throws {
        guard #available(macOS 26, *) else { return }
        for dark in [false, true] {
            let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: "Fixed the reconnect loop.", delay: .milliseconds(10)))
            let flow = InteractionFlow(title: "Release notes", questions: [
                .init(prompt: "What should the release note say?", kind: .text),
            ])
            flow.answers[0].text = "Kit:"
            let dock = try await Rendered(InteractionDock(flow: flow, pendingCount: 1, finish: { _ in }),
                                          slot: slot, dark: dark, width: 440)
            defer { dock.close() }
            try dock.snapshot("dock-idle")
            let idle = dock.height
            #expect(dock.answerField != nil)

            #expect(await slot.dictation.start())
            try await dock.settle()
            try dock.snapshot("dock-listening")
            #expect(dock.answerField == nil)
            #expect(dock.height == idle)

            try dock.press(.return)
            try await eventually { flow.answers[0].text == "Kit: Fixed the reconnect loop." }
            try await dock.settle()
            try dock.snapshot("dock-filled")
            let field = try #require(dock.answerField)
            #expect(field.stringValue == "Kit: Fixed the reconnect loop.")
            #expect(field.currentEditor()?.selectedRange == NSRange(location: 30, length: 0))
        }
    }

    @Test func aReviewCommentIsDictatedIntoItsEditor() async throws {
        guard #available(macOS 26, *) else { return }
        for dark in [false, true] {
            let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: "Keep the draft on failure.", delay: .milliseconds(10)))
            let annotations = AnnotationState()
            annotations.begin(anchor: WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: "w",
                path: "main.swift", fileRevision: "file", startLine: 1, endLine: 1), workingTreeDiff: nil))
            let input = AnnotationInput(annotations: annotations, client: PreviewAnnotationClient(), session: "s", cancel: {})
            let comment = try await Rendered(input, slot: slot, dark: dark, width: 400)
            defer { comment.close() }
            try comment.snapshot("comment-idle")
            let idle = comment.height

            #expect(await slot.dictation.start())
            try await comment.settle()
            try comment.snapshot("comment-listening")
            #expect(comment.editor == nil)
            #expect(comment.height == idle)

            try comment.press(.return)
            try await eventually { annotations.draft == "Keep the draft on failure." }
            try await comment.settle()
            let editor = try #require(comment.editor)
            #expect(editor.string == "Keep the draft on failure.")
            #expect(comment.window.firstResponder === editor)
            #expect(editor.selectedRange() == NSRange(location: 26, length: 0))
        }
    }

    @Test func escThrowsTheRecordingAwayAndTheEditorTakesFocusBack() async throws {
        guard #available(macOS 26, *) else { return }
        let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(10)))
        let state = store()
        let composer = try await Rendered(state, slot: slot, dark: false)
        defer { composer.close() }
        #expect(await slot.dictation.start())
        try await composer.settle()
        try composer.press(.escape)
        try await eventually { slot.dictation.phase == .idle }
        try await composer.settle()
        let editor = try #require(composer.editor)
        #expect(state.ui.draft == "")
        #expect(composer.window.firstResponder === editor)
    }

    private func store(running: Bool = false) -> SessionStore {
        var session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "Test", model: "m", thinking: "high",
                                     workspace: "w", date: "", messages: [])
        session.activeRunID = running ? "run" : nil
        return SessionStore(fixture: Fixture(sessions: [session]))
    }
}

/// A view laid out in its own window, 20 points in from each side.
@MainActor private final class Rendered {
    let window: NSWindow
    let host: NSView
    let dark: Bool

    init(_ content: some View, slot: DictationSlot, dark: Bool, width: CGFloat = 560) async throws {
        _ = NSApplication.shared
        self.dark = dark
        let theme = ThemeConfiguration.decode("").theme(dark: dark)
        let view = content.padding(20)
            .environment(\.mica, theme).environment(\.colorScheme, dark ? .dark : .light)
            .environment(\.dictationSlot, slot)
            .foregroundStyle(theme.text).background(theme.surface)
            .frame(width: width).fixedSize(horizontal: false, vertical: true)
        host = NSHostingView(rootView: view)
        window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: width, height: 320))
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        try await settle()
    }

    /// The composer at 520 points wide.
    convenience init(_ state: SessionStore, slot: DictationSlot, dark: Bool) async throws {
        try await self.init(ComposerView(state: state), slot: slot, dark: dark)
    }

    func settle() async throws {
        for _ in 0..<6 {
            host.layoutSubtreeIfNeeded()
            try await Task.sleep(for: .milliseconds(30))
        }
    }

    var height: CGFloat { host.fittingSize.height }

    var editor: ComposerTextView? { Self.views(ComposerTextView.self, in: host).first }

    var answerField: NSTextField? { Self.views(InteractionInputBox.self, in: host).first?.field }

    func close() { window.close() }

    enum Key { case `return`, escape }

    /// Presses a key as the keyboard does, to whatever has focus.
    func press(_ key: Key) throws {
        let (code, characters): (UInt16, String) = switch key {
        case .return: (36, "\r")
        case .escape: (53, "\u{1b}")
        }
        for type in [NSEvent.EventType.keyDown, .keyUp] {
            window.sendEvent(try #require(NSEvent.keyEvent(with: type, location: .zero, modifierFlags: [], timestamp: 0,
                windowNumber: window.windowNumber, context: nil, characters: characters,
                charactersIgnoringModifiers: characters, isARepeat: false, keyCode: code)))
        }
    }

    func snapshot(_ name: String) throws {
        guard let directory = ProcessInfo.processInfo.environment["KIT_SNAPSHOT_DIR"] else { return }
        let bitmap = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: bitmap)
        try #require(bitmap.representation(using: .png, properties: [:])).write(to: URL(fileURLWithPath: directory)
            .appendingPathComponent("dictation-\(name)-\(dark ? "dark" : "light").png"))
    }

    private static func views<T: NSView>(_ type: T.Type, in view: NSView) -> [T] {
        if let match = view as? T { return [match] }
        return view.subviews.flatMap { views(type, in: $0) }
    }
}

private struct PreviewAnnotationClient: AnnotationClient {
    let serverID = "preview"
    let isDemo = true
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func annotations(_ session: String) async throws -> [FileAnnotation] { [] }
    func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation { throw ClientError.http(503) }
    func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation { throw ClientError.http(503) }
    func deleteAnnotation(_ session: String, id: UInt64) async throws {}
}
