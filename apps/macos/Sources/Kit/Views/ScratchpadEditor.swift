import CodeEditLanguages
@preconcurrency import CodeEditSourceEditor
import SwiftUI

struct ScratchpadEditor: View {
    @Environment(\.mica) private var theme
    @Binding var text: String
    @State private var editorState = SourceEditorState()

    var body: some View {
        SourceEditor(
            $text, language: .markdown,
            configuration: FileEditorTheme.configuration(theme, isEditable: true, wrapLines: true),
            state: $editorState
        )
        .clipped()
        .accessibilityLabel("Scratchpad editor")
    }
}
