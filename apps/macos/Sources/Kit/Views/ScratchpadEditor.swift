import CodeEditLanguages
@preconcurrency import CodeEditSourceEditor
import SwiftUI

struct ScratchpadEditor: View {
    @Environment(\.mica) private var theme
    @Binding var text: String
    var documentVersion = 0
    @Binding var editorState: SourceEditorState

    var body: some View {
        SourceEditor(
            $text, language: .markdown,
            configuration: FileEditorTheme.configuration(theme, isEditable: true, wrapLines: true),
            state: $editorState
        )
        .id(documentVersion)
        .clipped()
        .accessibilityLabel("Scratchpad editor")
    }
}
