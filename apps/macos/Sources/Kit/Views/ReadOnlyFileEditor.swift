import AppKit
import CodeEditLanguages
@preconcurrency import CodeEditSourceEditor
import SwiftUI

/// Replace the document on content changes while retaining native selection
/// and scroll state outside that document identity.
struct ReadOnlyFileEditor: View {
    let path: String
    let source: String
    var position: Binding<SourceEditorState>? = nil
    @State private var retainedState = SourceEditorState()
    private struct Document: Hashable { let path: String; let source: String }

    var body: some View {
        FileEditorDocument(path: path, source: source, editorState: position ?? $retainedState)
            .id(Document(path: path, source: source))
            // AppKit's gutter can extend past the representable during scrolling.
            // The file editor owns only the area below the workspace headers.
            .clipped()
    }

    static func language(for path: String) -> CodeLanguage {
        let filename = (path as NSString).lastPathComponent
        let ext = (filename as NSString).pathExtension.lowercased()
        return CodeLanguage.allLanguages.first { $0.extensions.contains(filename) }
            ?? CodeLanguage.allLanguages.first { $0.extensions.contains(ext) }
            ?? .default
    }
}

private struct FileEditorDocument: View {
    @Environment(\.mica) private var theme
    let path: String
    let source: String
    @Binding var editorState: SourceEditorState

    static func clamped(_ state: SourceEditorState, source: String) -> SourceEditorState {
        var result = state
        let length = source.utf16.count
        result.cursorPositions = state.cursorPositions?.map { cursor in
            let start = min(max(0, cursor.range.location), length)
            return CursorPosition(range: NSRange(location: start, length: min(max(0, cursor.range.length), length - start)))
        }
        return result
    }

    var body: some View {
        SourceEditor(
            .constant(source), language: ReadOnlyFileEditor.language(for: path),
            configuration: FileEditorTheme.configuration(theme), state: Binding(
                get: { Self.clamped(editorState, source: source) }, set: { editorState = $0 })
        )
        .accessibilityLabel("Read-only file: \(path)")
    }
}

@MainActor
enum FileEditorTheme {
    static func configuration(_ theme: MicaTheme, isEditable: Bool = false, wrapLines: Bool = false) -> SourceEditorConfiguration {
        .init(
            appearance: .init(theme: colors(theme), font: Typography.shared.font(size: 12, mono: true),
                              wrapLines: wrapLines, bracketPairEmphasis: nil),
            behavior: .init(isEditable: isEditable, isSelectable: true),
            layout: .init(editorOverscroll: 0),
            peripherals: .init(showGutter: true, showMinimap: false, showReformattingGuide: false, showFoldingRibbon: false)
        )
    }

    static func colors(_ theme: MicaTheme) -> EditorTheme {
        // The editor computes RGB brightness even with its minimap hidden.
        func rgb(_ color: Color) -> NSColor {
            NSColor(color).usingColorSpace(.sRGB) ?? NSColor(srgbRed: 0, green: 0, blue: 0, alpha: 1)
        }
        func attribute(_ name: String, _ fallback: Color) -> EditorTheme.Attribute {
            .init(color: rgb(theme.syntax(name, fallback: fallback)))
        }
        return EditorTheme(
            text: attribute("text", theme.text), insertionPoint: rgb(theme.accent),
            invisibles: .init(color: rgb(theme.muted)), background: rgb(theme.surface),
            lineHighlight: rgb(theme.raised), selection: rgb(theme.accent.opacity(0.2)),
            keywords: attribute("keyword", theme.accent), commands: attribute("function", theme.accent),
            types: attribute("type", theme.accent), attributes: attribute("attribute", theme.accent),
            variables: attribute("variable", theme.text), values: attribute("builtin", theme.accent),
            numbers: attribute("number", theme.accent), strings: attribute("string", theme.success),
            characters: attribute("escape", theme.success), comments: attribute("comment", theme.muted)
        )
    }
}
