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

    @Test func oneFieldDictatesAtATime() async throws {
        guard #available(macOS 26, *) else { return }
        let composer = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(10)))
        let comment = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(10)))
        #expect(await composer.dictation.startAlone())
        #expect(await comment.dictation.startAlone())
        #expect(composer.dictation.phase == .idle)
        #expect(comment.dictation.phase == .recording)
        #expect(await composer.dictation.startAlone())
        #expect(comment.dictation.phase == .idle)
        composer.reset()
    }

    // MARK: Browser annotation

    @Test func aBrowserCommentIsDictatedIntoItsBoxWithTheCaretAfterTheWords() async throws {
        guard #available(macOS 26, *) else { return }
        for dark in [false, true] {
            let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: "Make it quieter.", delay: .milliseconds(10)))
            let page = try await BrowserComment()
            page.session.comment = "Too loud."
            let box = try await Rendered(page.box, slot: slot, dark: dark, width: BrowserAnnotationOverlay.boxWidth + 40)
            defer { box.close() }
            try await eventually { box.commentEditor != nil }
            try box.snapshot("browser-idle")
            let idle = box.height

            #expect(await slot.dictation.start())
            try await box.settle()
            try box.snapshot("browser-listening")
            #expect(box.commentField == nil)
            #expect(box.height == idle)

            // Return finishes the recording; it doesn't save the comment.
            try box.press(.return)
            try await eventually { page.session.comment == "Too loud. Make it quieter." }
            try await box.settle()
            try box.snapshot("browser-filled")
            #expect(page.added == 0)
            let editor = try #require(box.commentEditor)
            #expect(editor.string == "Too loud. Make it quieter.")
            #expect(editor.selectedRange() == NSRange(location: 26, length: 0), "\(editor.selectedRange())")
        }
    }

    @Test func escWhileDictatingABrowserCommentStopsTheRecordingAndKeepsTheBox() async throws {
        guard #available(macOS 26, *) else { return }
        let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(10)))
        let page = try await BrowserComment()
        page.session.comment = "Too loud."
        let box = try await Rendered(page.box, slot: slot, dark: false, width: BrowserAnnotationOverlay.boxWidth + 40)
        defer { box.close() }
        #expect(await slot.dictation.start())
        try await box.settle()
        try box.press(.escape)
        try await eventually { slot.dictation.phase == .idle }
        try await box.settle()
        #expect(page.cancelled == 0)
        #expect(page.session.comment == "Too loud.")
        #expect(box.window.firstResponder === box.commentEditor)
    }

    @Test func keysWhileABrowserCommentGetsDictationReadyActOnDictationNotTheBox() async throws {
        guard #available(macOS 26, *) else { return }
        for key in [Rendered.Key.return, .escape, .up, .down] {
            let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .seconds(2)))
            let page = try await BrowserComment()
            if key == .return { page.session.comment = "Too loud." }
            let box = try await Rendered(page.box, slot: slot, dark: false, width: BrowserAnnotationOverlay.boxWidth + 40)
            defer { box.close() }
            try await eventually { box.window.firstResponder === box.commentEditor }
            let starting = Task { await slot.dictation.start() }
            try await eventually { slot.dictation.isPreparing }
            try box.press(key)
            try await box.settle()
            #expect(page.added == 0)
            #expect(page.cancelled == 0)
            #expect(page.steps == 0)
            #expect(slot.dictation.isPreparing == (key != .escape))
            slot.reset()
            #expect(await starting.value == false)
            if key == .up {
                // Once dictation stops, the arrow steps out to the parent again.
                try box.press(.up)
                try await eventually { page.steps == 1 }
            }
        }
    }

    @Test func theArrowsDontStepWhileTheStripTakesABrowserComment() async throws {
        guard #available(macOS 26, *) else { return }
        let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(10)))
        let page = try await BrowserComment()
        let box = try await Rendered(page.box, slot: slot, dark: false, width: BrowserAnnotationOverlay.boxWidth + 40)
        defer { box.close() }
        #expect(await slot.dictation.start())
        try await box.settle()
        try box.press(.up)
        try box.press(.down)
        try await box.settle()
        #expect(page.steps == 0)
        #expect(slot.dictation.phase == .recording)
        slot.reset()
    }

    @Test func pickingAnotherElementThrowsTheBrowserCommentsRecordingAway() async throws {
        guard #available(macOS 26, *) else { return }
        for finishing in [false, true] {
            let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(200)))
            let page = try await BrowserComment()
            page.session.comment = "Too loud."
            let box = try await Rendered(page.box, slot: slot, dark: false, width: BrowserAnnotationOverlay.boxWidth + 40)
            defer { box.close() }
            #expect(await slot.dictation.start())
            try await box.settle()
            if finishing {
                try box.press(.return)
                try await eventually { slot.dictation.phase == .transcribing }
            }
            page.engine.annotationResults["select"] = BrowserComment.selected("a.link")
            await page.session.click(at: CGPoint(x: 50, y: 50), deep: false)
            try await eventually { slot.dictation.phase == .idle }
            try await Task.sleep(for: .milliseconds(400))
            #expect(page.session.comment == "Too loud.")
        }
    }

    @Test func theBrowserCommentsRecordingGoesWithItsBox() async throws {
        guard #available(macOS 26, *) else { return }
        let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(10)))
        let page = try await BrowserComment()
        page.session.comment = "Too loud."
        let overlay = try await Rendered(page.overlay, slot: slot, dark: false)
        defer { overlay.close() }
        try await eventually { overlay.commentField != nil }
        #expect(await slot.dictation.start())
        try await overlay.settle()
        // Cancel, Save, and Esc let the selection go.
        await page.session.deselect()
        try await overlay.settle()
        #expect(overlay.commentField == nil)
        #expect(slot.dictation.phase == .idle)

        // Annotate mode ends, or its tab closes or is switched away from.
        await page.session.click(at: CGPoint(x: 50, y: 50), deep: false)
        try await eventually { overlay.commentField != nil }
        #expect(await slot.dictation.start())
        try await overlay.settle()
        page.shown = false
        try await overlay.settle()
        #expect(overlay.commentField == nil)
        #expect(slot.dictation.phase == .idle)
    }

    @Test func thePagesKeysLeaveTheSelectionWhileItsCommentIsDictated() async throws {
        guard #available(macOS 26, *) else { return }
        let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(10)))
        let page = try await BrowserComment()
        let overlay = try await Rendered(page.overlay, slot: slot, dark: false)
        defer { overlay.close() }
        try await eventually { overlay.commentField != nil }
        #expect(await slot.dictation.start())
        try await overlay.settle()
        let pointer = try #require(overlay.pointerLayer)
        overlay.window.makeFirstResponder(pointer)
        try overlay.press(.up)
        try overlay.press(.down)
        try overlay.press(.escape)
        try await overlay.settle()
        #expect(page.steps == 0)
        #expect(slot.dictation.phase == .idle)
        #expect(page.session.page.selected != nil)
        // Without a recording, Esc leaves the selection.
        overlay.window.makeFirstResponder(pointer)
        try overlay.press(.escape)
        try await eventually { page.session.page.selected == nil }
    }

    @Test func aClickOnThePageThrowsTheBrowserCommentsRecordingAwayBeforePicking() async throws {
        guard #available(macOS 26, *) else { return }
        let slot = DictationSlot(engine: ScriptedDictationEngine(transcript: Self.said, delay: .milliseconds(10)))
        let page = try await BrowserComment()
        page.session.comment = "Too loud."
        let overlay = try await Rendered(page.overlay, slot: slot, dark: false)
        defer { overlay.close() }
        try await eventually { overlay.commentField != nil }
        #expect(await slot.dictation.start())
        try await overlay.settle()
        let pointer = try #require(overlay.pointerLayer)
        page.engine.annotationResults["select"] = BrowserComment.selected("a.link")
        let point = pointer.convert(NSPoint(x: 300, y: 300), to: nil)
        overlay.window.sendEvent(try #require(NSEvent.mouseEvent(with: .leftMouseDown, location: point, modifierFlags: [],
            timestamp: 0, windowNumber: overlay.window.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1)))
        // Gone as the button goes down, before the page has answered.
        #expect(slot.dictation.phase == .idle)
        #expect(page.session.clicks == 1)
        try await eventually { page.session.page.selected?.label == "a.link" }
        #expect(page.session.comment == "Too loud.")
    }

    private func store(running: Bool = false) -> SessionStore {
        var session = SessionExcerpt(id: "s", title: "Test", sourceTitle: "Test", model: "m", thinking: "high",
                                     workspace: "w", date: "", messages: [])
        session.activeRunID = running ? "run" : nil
        return SessionStore(fixture: Fixture(sessions: [session]))
    }
}

/// A page in Annotate mode with an element selected, and its comment box
/// or whole overlay, counting what they ask of the page.
@MainActor @Observable private final class BrowserComment {
    let engine = FakeBrowserEngine()
    let session: BrowserAnnotationSession
    private(set) var added = 0
    private(set) var cancelled = 0
    /// Whether the overlay shows, as Annotate mode and its tab being shown do.
    var shown = true
    var steps: Int { engine.annotationScripts.filter { $0.operation == "step" }.count }

    static func selected(_ label: String) -> String {
        #"{"selected":{"label":"\#(label)","rect":{"x":40,"y":40,"width":120,"height":32}},"crumbs":["main","\#(label)"],"canParent":true,"canChild":true}"#
    }

    init() async throws {
        engine.annotationResults["select"] = Self.selected("button.cta")
        engine.annotationResults["step"] = Self.selected("main")
        engine.annotationResults["clear"] = "{}"
        let defaults = try #require(UserDefaults(suiteName: "DictationPresentationTests.\(UUID())"))
        session = BrowserAnnotationSession(engine: engine, defaults: defaults)
        await session.click(at: CGPoint(x: 50, y: 50), deep: false)
    }

    var box: BrowserAnnotationCommentBox {
        BrowserAnnotationCommentBox(session: session, add: { [unowned self] in added += 1 },
                                    cancel: { [unowned self] in cancelled += 1 })
    }

    var overlay: some View {
        Overlay(page: self).frame(height: 360)
    }

    private struct Overlay: View {
        let page: BrowserComment

        var body: some View {
            if page.shown {
                BrowserAnnotationOverlay(session: page.session, page: page.engine.view,
                                         attach: { _ in }, done: {})
            }
        }
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

    /// The browser comment box's field.
    var commentField: NSTextField? { Self.views(NSTextField.self, in: host).first { $0.isEditable } }

    /// The browser comment's editor while it has focus.
    var commentEditor: NSTextView? {
        guard let field = commentField, let editor = field.currentEditor() as? NSTextView else { return nil }
        return editor
    }

    /// The Annotate overlay's layer over the page.
    var pointerLayer: NSView? {
        Self.allViews(in: host).first { String(describing: type(of: $0)) == "PointerView" }
    }

    func close() { window.close() }

    enum Key { case `return`, escape, up, down }

    /// Presses a key as the keyboard does, to whatever has focus.
    func press(_ key: Key) throws {
        let (code, characters): (UInt16, String) = switch key {
        case .return: (36, "\r")
        case .escape: (53, "\u{1b}")
        case .up: (126, String(UnicodeScalar(NSUpArrowFunctionKey)!))
        case .down: (125, String(UnicodeScalar(NSDownArrowFunctionKey)!))
        }
        let modifiers: NSEvent.ModifierFlags = code >= 125 ? [.numericPad, .function] : []
        for type in [NSEvent.EventType.keyDown, .keyUp] {
            window.sendEvent(try #require(NSEvent.keyEvent(with: type, location: .zero, modifierFlags: modifiers, timestamp: 0,
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

    private static func allViews(in view: NSView) -> [NSView] {
        [view] + view.subviews.flatMap(allViews)
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
