import AppKit
import SwiftUI
import Testing
@testable import Kit

/// Shows the session composer and the subagent composer while `shown` holds,
/// as a session tab and its Agent pane do.
@MainActor @Observable private final class ComposerUndoModel {
    var shown = true
    var draft = ""
    var subagentDraft = ""
}

private struct ComposerUndoHarness: View {
    @Bindable var model: ComposerUndoModel

    var body: some View {
        VStack {
            if model.shown {
                editor($model.draft, label: "Message composer")
                editor($model.subagentDraft, label: "Subagent message")
            } else {
                Text("Another session")
            }
        }.frame(width: 400, height: 200)
    }

    private func editor(_ text: Binding<String>, label: String) -> some View {
        GrowingComposerEditor(text: text, focused: .constant(false), focusRequest: 0,
            foreground: .black, placeholderColor: .gray,
            attachmentDrop: { _ in false }, attachmentDropTargeted: { _ in }, submit: {},
            focusEnabled: false, accessibilityLabel: label)
    }
}

@MainActor @Suite(.serialized) struct ComposerUndoTests {
    static let undo = Selector(("undo:"))
    static let redo = Selector(("redo:"))
    private let undo = Self.undo
    private let redo = Self.redo

    private static func editors(in view: NSView) -> [ComposerTextView] {
        if let editor = view as? ComposerTextView { return [editor] }
        return view.subviews.flatMap { editors(in: $0) }
    }

    private static func editor(_ label: String, in view: NSView) throws -> ComposerTextView {
        try #require(editors(in: view).first { $0.accessibilityLabel() == label })
    }

    /// Types as the keyboard does, through the first responder's key handling.
    static func type(_ text: String, in window: NSWindow) throws {
        for character in text.map(String.init) {
            window.firstResponder?.keyDown(with: try #require(NSEvent.keyEvent(with: .keyDown, location: .zero,
                modifierFlags: [], timestamp: 0, windowNumber: window.windowNumber, context: nil,
                characters: character, charactersIgnoringModifiers: character, isARepeat: false, keyCode: 0)))
        }
    }

    /// Sends an Edit menu action the way its menu item does: up the
    /// responder chain from the window's first responder, after validation.
    static func perform(_ action: Selector, in window: NSWindow) throws -> (enabled: Bool, title: String) {
        let item = NSMenuItem(title: "", action: action, keyEquivalent: "")
        var responder = window.firstResponder
        while let current = responder, !current.responds(to: action) { responder = current.nextResponder }
        let target = try #require(responder)
        let enabled = (target as? NSUserInterfaceValidations)?.validateUserInterfaceItem(item) ?? true
        if enabled { target.perform(action, with: item) }
        return (enabled, item.title)
    }

    private static func window(_ model: ComposerUndoModel) -> (NSWindow, NSView) {
        let host = NSHostingView(rootView: ComposerUndoHarness(model: model))
        let window = FocusIsolatedWindow(testContentRect: NSRect(x: 0, y: 0, width: 400, height: 200), styleMask: [.titled])
        window.contentView = host
        window.orderFront(nil)
        host.layoutSubtreeIfNeeded()
        return (window, host)
    }

    @Test func undoInTheComposerUndoesAndRedoesItsTyping() throws {
        let model = ComposerUndoModel()
        let (window, host) = Self.window(model)
        defer { window.close() }
        let composer = try Self.editor("Message composer", in: host)
        #expect(window.makeFirstResponder(composer))
        try Self.type("hello", in: window)
        #expect(composer.string == "hello")
        #expect(model.draft == "hello")

        #expect(try Self.perform(undo, in: window) == (true, "Undo Typing"))
        #expect(composer.string == "")
        #expect(model.draft == "")
        #expect(try Self.perform(undo, in: window) == (false, "Undo"))

        #expect(try Self.perform(redo, in: window) == (true, "Redo Typing"))
        #expect(composer.string == "hello")
        #expect(model.draft == "hello")
    }

    @Test func undoInOneComposerLeavesAnotherComposerAndTheWindowAlone() throws {
        let model = ComposerUndoModel()
        let (window, host) = Self.window(model)
        defer { window.close() }
        let composer = try Self.editor("Message composer", in: host)
        let subagent = try Self.editor("Subagent message", in: host)
        #expect(window.makeFirstResponder(composer))
        try Self.type("session", in: window)
        #expect(window.makeFirstResponder(subagent))
        try Self.type("subagent", in: window)
        #expect(window.undoManager?.canUndo == false)

        #expect(window.makeFirstResponder(composer))
        #expect(try Self.perform(undo, in: window) == (true, "Undo Typing"))
        #expect(model.draft == "")
        #expect(model.subagentDraft == "subagent")
        #expect(subagent.string == "subagent")
    }

    /// Text undo actions don't retain the text they edit. Undo after a
    /// composer is gone, as after its session tab closes or an interaction
    /// replaces it, must not reach that composer's freed text.
    @Test func undoAfterAComposerIsRemovedDoesNotReachItsText() async throws {
        let model = ComposerUndoModel()
        let (window, host) = Self.window(model)
        defer { window.close() }
        weak var storage: NSTextStorage?
        try autoreleasepool {
            let composer = try Self.editor("Message composer", in: host)
            storage = composer.textStorage
            #expect(window.makeFirstResponder(composer))
            try Self.type("removed draft", in: window)
            #expect(model.draft == "removed draft")
        }
        model.shown = false
        try await eventually { Self.editors(in: host).isEmpty }
        window.makeFirstResponder(nil)
        try await eventually { storage == nil }

        // Before the fix the window kept the removed composer's typing, and
        // undoing it messaged the freed text storage.
        try #require(window.undoManager?.canUndo == false)
        #expect(try Self.perform(undo, in: window) == (false, "Undo"))
        #expect(model.draft == "removed draft")
    }
}
