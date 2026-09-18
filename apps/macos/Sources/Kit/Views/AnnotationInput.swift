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
            HStack(spacing: 12) {
                if annotations.pending { KitSpinner() }
                Spacer()
                Button("Cancel", action: cancel)
                Button("Save", action: save)
                    .disabled(!FileAnnotation.validBody(annotations.draft) || annotations.uncertain)
            }.font(.kit(size: 11)).buttonStyle(.plain)
        }.padding(12).background(theme.raised, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).stroke(theme.accent.opacity(focused ? 0.5 : 0), lineWidth: 1))
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
