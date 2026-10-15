import SwiftUI

struct AnnotationInput: View {
    @Environment(\.mica) private var theme
    @Bindable var annotations: AnnotationState
    let client: any AnnotationClient
    let session: String
    let cancel: () -> Void
    var sizeChanged: () -> Void = {}
    @State private var focused = false
    @State private var focusRequest = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            GrowingComposerEditor(text: $annotations.draft, focused: $focused, focusRequest: focusRequest,
                foreground: theme.text, placeholderColor: theme.muted,
                attachmentDrop: { _ in false }, attachmentDropTargeted: { _ in }, submit: save,
                shellEnabled: false, placeholder: "Add a comment…", accessibilityLabel: "Annotation comment",
                cancel: cancel, isolatedUndo: true, theme: theme)
                .frame(minHeight: 20)
            if annotations.draft.utf8.count > 16 * 1024 {
                Text("Comments can contain up to 16 KiB.").font(.kit(size: 11)).foregroundStyle(theme.muted)
            }
            HStack(spacing: 8) {
                if annotations.pending { KitSpinner() }
                Spacer()
                Button("Cancel", action: cancel).buttonStyle(SmallButtonStyle(bordered: true))
                Button(annotations.editor?.editing == nil ? "Comment" : "Save", action: save)
                    .buttonStyle(SmallButtonStyle(prominent: true))
                    .disabled(!FileAnnotation.validBody(annotations.draft) || annotations.uncertain)
            }
        }.padding(.horizontal, 12).padding(.vertical, 10)
            .background(theme.raised, in: RoundedRectangle(cornerRadius: 4))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(focused ? theme.accent.opacity(0.6) : theme.strongBorder, lineWidth: 1))
            .disabled(annotations.pending)
            .onAppear { focusRequest += 1 }
            .onChange(of: annotations.draft) { sizeChanged() }
    }

    private func save() {
        guard let editor = annotations.editor else { return }
        Task {
            let saved = await annotations.save(client: client, session: session, anchor: editor.anchor,
                editing: editor.editing, replacing: editor.replacing)
            if saved { cancel() }
        }
    }
}
