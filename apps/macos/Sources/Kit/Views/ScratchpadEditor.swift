import CodeEditLanguages
@preconcurrency import CodeEditSourceEditor
import SwiftUI

struct ScratchpadEditor: View {
    @Environment(\.mica) private var theme
    @Environment(\.textComments) private var comments
    @State private var painter = ScratchpadCommentPainter()
    @Binding var text: String
    var documentVersion = 0
    @Binding var editorState: SourceEditorState
    @State private var scrollers = KitScrollerCoordinator(theme: MicaTheme(dark: false))

    var body: some View {
        SourceEditor(
            $text, language: .markdown,
            configuration: FileEditorTheme.configuration(theme, isEditable: true, wrapLines: true),
            state: $editorState,
            coordinators: [painter, scrollers]
        )
        .onChange(of: theme, initial: true) { scrollers.theme = theme }
        .id(documentVersion)
        .onAppear { updateMarks() }
        .onChange(of: marks) { updateMarks() }
        .onChange(of: theme) { updateMarks() }
        .clipped()
        .background(CommentSourceRegion(source: .init(label: "Scratchpad", scope: TextCommentSource.scratchpadScope,
                                                      pane: .scratchpad)))
        .onAppear { comments?.register(painter) }
        .onDisappear { comments?.unregister(painter) }
        .accessibilityLabel("Scratchpad editor")
    }

    private var marks: [TextCommentMark] { comments?.marks(in: TextCommentSource.scratchpadScope) ?? [] }
    private func updateMarks() { painter.update(marks: marks, theme: theme) }
}
