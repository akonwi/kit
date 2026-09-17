import AppKit
import CodeEditLanguages
@preconcurrency import CodeEditSourceEditor
import Testing
import SwiftUI
import Observation
@testable import Kit

@MainActor
@Suite(.serialized)
struct FileEditorTests {
    @Test func accessibilityRejectsInvalidRangesWithoutCrashing() {
        _ = NSApplication.shared
        EditorAccessibility.install()
        let controller = TextViewController(string: "let value = 1\n", language: .swift,
            configuration: FileEditorTheme.configuration(MicaTheme(dark: false)), cursorPositions: [])
        _ = controller.view
        #expect(controller.textView.accessibilityFrame(for: NSRange(location: NSNotFound, length: 1)) == .zero)
        #expect(controller.textView.accessibilityFrame(for: NSRange(location: Int.max - 1, length: 5)) == .zero)
        #expect(controller.textView.accessibilityFrame(for: NSRange(location: 0, length: 0)) == .zero)
        #expect(controller.textView.accessibilityString(for: NSRange(location: 0, length: 3)) == "let")
    }

    @Test func scratchpadSupportsEditingAndUndo() {
        _ = NSApplication.shared
        let source = "# Notes\n"
        let controller = TextViewController(string: source, language: .markdown,
            configuration: FileEditorTheme.configuration(MicaTheme(dark: false), isEditable: true, wrapLines: true),
            cursorPositions: [])
        _ = controller.view
        controller.textView.selectAll(nil)
        controller.textView.insertText("# Updated notes\n", replacementRange: controller.textView.selectedRange())
        #expect(controller.textView.string == "# Updated notes\n")
        controller.textView.undoManager?.undo()
        #expect(controller.textView.string == source)
    }

    @Test func readOnlyEditorRetainsSelectionAndRejectsTyping() {
        _ = NSApplication.shared
        let source = "let greeting = \"Hello 🌿\"\n"
        let controller = TextViewController(string: source, language: .swift,
            configuration: FileEditorTheme.configuration(MicaTheme(dark: false)), cursorPositions: [])
        _ = controller.view
        controller.textView.selectAll(nil)
        #expect(controller.textView.selectedRange() == NSRange(location: 0, length: source.utf16.count))
        controller.textView.insertText("replacement", replacementRange: controller.textView.selectedRange())
        #expect(controller.textView.string == source)
        #expect(controller.textView.isSelectable)
    }

    @Test func changedContentPreservesRenderedSelectionAndScroll() async throws {
        let document = RefreshDocument()
        let host = NSHostingController(rootView: RefreshHarness(document: document))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 500, height: 250), styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false; window.contentViewController = host; window.orderFront(nil)
        defer { window.close() }
        func editor(_ controller: NSViewController) -> TextViewController? {
            func find(_ view: NSView) -> TextViewController? {
                if let value = view.nextResponder as? TextViewController { return value }
                return view.subviews.compactMap { find($0) }.first
            }
            return find(controller.view)
        }
        try await Task.sleep(for: .milliseconds(250))
        let original = try #require(editor(host))
        original.setCursorPositions([CursorPosition(range: NSRange(location: 30, length: 5))])
        original.scrollView.contentView.scroll(to: CGPoint(x: 0, y: 400))
        original.scrollView.reflectScrolledClipView(original.scrollView.contentView)
        try await Task.sleep(for: .milliseconds(150))
        // Capture the same state the editor reports during ordinary selection/scroll.
        document.position = SourceEditorState(cursorPositions: original.cursorPositions,
            scrollPosition: original.scrollView.contentView.bounds.origin)
        let saved = document.position.scrollPosition!
        document.source += "\n// Updated file"
        try await Task.sleep(for: .milliseconds(300))
        let refreshed = try #require(editor(host))
        #expect(refreshed.textView.string.hasSuffix("// Updated file"))
        #expect(refreshed.textView.selectedRange() == NSRange(location: 30, length: 5))
        #expect(abs(refreshed.scrollView.contentView.bounds.origin.y - saved.y) < 2)
        document.source = "short"
        try await Task.sleep(for: .milliseconds(200))
        #expect(try #require(editor(host)).textView.selectedRange() == NSRange(location: 5, length: 0))
    }

    @Test func languagesFollowFilenameAndExtension() {
        #expect(ReadOnlyFileEditor.language(for: "main.go").id == CodeLanguage.go.id)
        #expect(ReadOnlyFileEditor.language(for: "View.swift").id == CodeLanguage.swift.id)
        #expect(ReadOnlyFileEditor.language(for: "Component.tsx").id == CodeLanguage.tsx.id)
        #expect(ReadOnlyFileEditor.language(for: "notes.unknown").id == CodeLanguage.default.id)
    }

    @Test func themeUsesImportedSyntaxAndRGBColors() {
        let theme = MicaTheme(dark: true, overrides: ["bg": "#102030"], syntaxOverrides: ["keyword": "#ff0000"])
        let result = FileEditorTheme.colors(theme)
        #expect(abs((result.background.usingColorSpace(.sRGB)?.redComponent ?? -1) - 16.0 / 255.0) < 0.00001)
        #expect(result.keywords.color.usingColorSpace(.sRGB)?.redComponent == 1)
        #expect(FileEditorTheme.colors(MicaTheme(dark: false)).background.colorSpace.colorSpaceModel == .rgb)
    }
}

@MainActor @Observable private final class RefreshDocument {
    var source = (0..<200).map { "let value\($0) = \($0)" }.joined(separator: "\n")
    var position = SourceEditorState()
}

private struct RefreshHarness: View {
    @Bindable var document: RefreshDocument
    var body: some View {
        ReadOnlyFileEditor(path: "test.swift", source: document.source, position: $document.position)
    }
}
