import KitDictation
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
    @State private var focusAtEndRequest: Int?
    @State private var ownDictation = DictationSlot()
    @Environment(\.dictationSlot) private var givenDictation

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if #available(macOS 26, *), dictation.listening {
                HStack(spacing: 8) {
                    DictationStrip(dictation: dictation, done: finishDictation, cancelled: focusEditorAtEnd, size: 13)
                    DictationSquare(dictation: dictation, size: 20, done: finishDictation)
                }
            } else {
                HStack(alignment: .top, spacing: 4) {
                    GrowingComposerEditor(text: $annotations.draft, focused: $focused, focusRequest: focusRequest,
                        focusAtEndRequest: focusAtEndRequest,
                        foreground: theme.text, placeholderColor: theme.muted,
                        attachmentDrop: { _ in false }, attachmentDropTargeted: { _ in }, submit: { if !dictating { save() } },
                        shellEnabled: false, placeholder: "Add a comment…", accessibilityLabel: "Annotation comment",
                        cancel: { if dictating { dictationSlot.reset() } else { cancel() } }, theme: theme)
                        .frame(minHeight: 20)
                    if #available(macOS 26, *) {
                        DictationMic(dictation: dictation, size: 20, iconSize: 13)
                    }
                }
            }
            if #available(macOS 26, *), dictation.hasNotice { DictationNotice(dictation: dictation) }
            if annotations.draft.utf8.count > 16 * 1024 {
                Text("Comments can contain up to 16 KiB.").font(.kit(size: 11)).foregroundStyle(theme.muted)
            }
            HStack(spacing: 8) {
                if annotations.pending { KitSpinner() }
                Spacer()
                Button("Cancel", action: cancel).buttonStyle(SmallButtonStyle(bordered: true))
                Button(annotations.editor?.editing == nil ? "Comment" : "Save", action: save)
                    .buttonStyle(SmallButtonStyle(prominent: true))
                    .disabled(!FileAnnotation.validBody(annotations.draft) || annotations.uncertain || dictating)
            }
        }.padding(.horizontal, 12).padding(.vertical, 10)
            .background(theme.raised, in: RoundedRectangle(cornerRadius: 4))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(focused || dictating ? theme.accent.opacity(0.6) : theme.strongBorder, lineWidth: 1))
            .disabled(annotations.pending)
            .onAppear { focusRequest += 1 }
            .onDisappear { dictationSlot.reset() }
            .onChange(of: annotations.draft) {
                sizeChanged()
                if #available(macOS 26, *) { dictation.dismissProblem() }
            }
            .onChange(of: dictating) {
                if dictating { focused = false }
                sizeChanged()
            }
            .onChange(of: dictationProblem) { sizeChanged() }
    }

    private var dictationSlot: DictationSlot { givenDictation ?? ownDictation }

    @available(macOS 26, *)
    private var dictation: Dictation { dictationSlot.dictation }

    private var dictating: Bool {
        if #available(macOS 26, *) { return dictation.isActive }
        return false
    }

    private var dictationProblem: DictationProblem? {
        if #available(macOS 26, *) { return dictation.problem }
        return nil
    }

    /// Stops recording and puts the words after the comment, with the
    /// editor focused and the caret after them.
    private func finishDictation() {
        guard #available(macOS 26, *) else { return }
        dictation.finish { text in
            let current = annotations.draft
            annotations.draft = DictationText.join(
                current.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "" : current, text)
            focusEditorAtEnd()
        }
    }

    /// The editor is remade as the strip goes, and a remade editor takes only an at-end request.
    private func focusEditorAtEnd() {
        focusRequest += 1
        focusAtEndRequest = focusRequest
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
