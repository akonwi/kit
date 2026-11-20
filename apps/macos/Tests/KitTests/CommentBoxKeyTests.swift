import AppKit
import KitMermaid
import KitMermaidScene
import SwiftUI
import Testing
@testable import Kit

/// Every comment box edits as the composer does: Return adds the comment,
/// and Shift-, Option- and ⌘-Return start a new line in it.
@MainActor @Suite(.serialized) struct CommentBoxKeyTests {
    @Test func theTextCommentBoxAddsOnReturnAndBreaksLinesOtherwise() async throws {
        let editor = TextCommentEditor()
        var added = 0, cancelled = 0
        let box = try await HostedBox(TextCommentBox(editor: editor, quote: "every 2 seconds", submit: { added += 1 },
                                                     cancel: { cancelled += 1 }), width: TextCommentBox.width)
        defer { box.close() }
        try await box.checkLineBreaks(comment: { editor.comment }, added: { added })
        try box.press(.escape)
        #expect(cancelled == 1)
    }

    @Test func theDiagramCommentBoxAddsOnReturnAndBreaksLinesOtherwise() async throws {
        let annotator = DiagramAnnotator()
        annotator.start()
        annotator.click(Part(.node, 2))
        var added = 0
        let box = try await HostedBox(DiagramCommentBox(annotator: annotator, summary: "node C \"Reject\" (box)",
                                                        add: { added += 1 }), width: DiagramCommentBox.width)
        defer { box.close() }
        try await box.checkLineBreaks(comment: { annotator.comment }, added: { added })
    }

    @Test func theBrowserCommentBoxAddsOnReturnAndBreaksLinesOtherwise() async throws {
        let page = try await BrowserSelection()
        var added = 0
        let box = try await HostedBox(BrowserAnnotationCommentBox(session: page.session, add: { added += 1 }, cancel: {}),
                                      width: BrowserAnnotationOverlay.boxWidth)
        defer { box.close() }
        try await box.checkLineBreaks(comment: { page.session.comment }, added: { added })
    }

    @Test func theBrowserCommentBoxStepsThroughAncestorsWithArrowsOnlyWhileEmpty() async throws {
        let page = try await BrowserSelection()
        let box = try await HostedBox(BrowserAnnotationCommentBox(session: page.session, add: {}, cancel: {}),
                                      width: BrowserAnnotationOverlay.boxWidth)
        defer { box.close() }
        let editor = try await box.focusedEditor()
        try box.press(.up)
        try await eventually { page.steps == 1 }
        try await eventually { page.session.page.selected?.label == "main" }
        // With a comment, the arrows move the caret instead.
        editor.insertText("One\nTwo", replacementRange: editor.selectedRange())
        try box.press(.up)
        try await Task.sleep(for: .milliseconds(100))
        #expect(page.steps == 1)
        #expect(editor.selectedRange() == NSRange(location: 3, length: 0))
    }
}

/// A comment box alone in a window.
@MainActor private final class HostedBox {
    let window: NSWindow
    let host: NSView

    init(_ content: some View, width: CGFloat) async throws {
        _ = NSApplication.shared
        let theme = ThemeConfiguration.decode("").theme(dark: false)
        let view = content.environment(\.mica, theme).foregroundStyle(theme.text)
            .frame(width: width).fixedSize(horizontal: false, vertical: true)
        host = NSHostingView(rootView: view)
        window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: width, height: 360))
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        try await settle()
    }

    func close() { window.close() }

    func settle() async throws {
        for _ in 0..<4 {
            host.layoutSubtreeIfNeeded()
            try await Task.sleep(for: .milliseconds(30))
        }
    }

    /// The comment's editor, once it has focus.
    func focusedEditor() async throws -> ComposerTextView {
        try await eventually {
            self.editor.map { self.window.firstResponder === $0 } ?? false
        }
        return try #require(editor)
    }

    var editor: ComposerTextView? { Self.views(ComposerTextView.self, in: host).first }

    /// Types four lines, breaking them with Shift-, Option- and ⌘-Return,
    /// and checks that they all show and nothing is added until Return.
    func checkLineBreaks(comment: () -> String, added: () -> Int) async throws {
        let editor = try await focusedEditor()
        let lineHeight = ComposerScrollView.lineHeight
        editor.insertText("First", replacementRange: editor.selectedRange())
        try press(.return, [.shift])
        editor.insertText("Second", replacementRange: editor.selectedRange())
        try press(.return, [.option])
        editor.insertText("Third", replacementRange: editor.selectedRange())
        try press(.return, [.command])
        editor.insertText("Fourth", replacementRange: editor.selectedRange())
        try await settle()
        #expect(comment() == "First\nSecond\nThird\nFourth")
        #expect(editor.string == "First\nSecond\nThird\nFourth")
        #expect(added() == 0)
        // Every line shows; the field grew to hold them.
        #expect(abs(editor.enclosingScrollView!.frame.height - 4 * lineHeight) <= 1,
                "\(editor.enclosingScrollView!.frame.height)")
        #expect(editor.enclosingScrollView!.contentView.bounds.minY == 0)
        try press(.return)
        try await eventually { added() == 1 }
        #expect(comment() == "First\nSecond\nThird\nFourth")
    }

    enum Key { case `return`, escape, up }

    /// Presses a key as the keyboard does, to whatever has focus.
    func press(_ key: Key, _ modifiers: NSEvent.ModifierFlags = []) throws {
        let (code, characters): (UInt16, String) = switch key {
        case .return: (36, "\r")
        case .escape: (53, "\u{1b}")
        case .up: (126, String(UnicodeScalar(NSUpArrowFunctionKey)!))
        }
        let flags = key == .up ? modifiers.union([.numericPad, .function]) : modifiers
        for type in [NSEvent.EventType.keyDown, .keyUp] {
            window.sendEvent(try #require(NSEvent.keyEvent(with: type, location: .zero, modifierFlags: flags, timestamp: 0,
                windowNumber: window.windowNumber, context: nil, characters: characters,
                charactersIgnoringModifiers: characters, isARepeat: false, keyCode: code)))
        }
    }

    private static func views<T: NSView>(_ type: T.Type, in view: NSView) -> [T] {
        if let match = view as? T { return [match] }
        return view.subviews.flatMap { views(type, in: $0) }
    }
}

/// A page in Annotate mode with an element selected.
@MainActor private final class BrowserSelection {
    let engine = FakeBrowserEngine()
    let session: BrowserAnnotationSession
    var steps: Int { engine.annotationScripts.filter { $0.operation == "step" }.count }

    init() async throws {
        let selected = { (label: String) in
            #"{"selected":{"label":"\#(label)","rect":{"x":40,"y":40,"width":120,"height":32}},"crumbs":["main","\#(label)"],"canParent":true,"canChild":true}"#
        }
        engine.annotationResults["select"] = selected("button.cta")
        engine.annotationResults["step"] = selected("main")
        engine.annotationResults["clear"] = "{}"
        let defaults = try #require(UserDefaults(suiteName: "CommentBoxKeyTests.\(UUID())"))
        session = BrowserAnnotationSession(engine: engine, defaults: defaults)
        await session.click(at: CGPoint(x: 50, y: 50), deep: false)
    }
}
