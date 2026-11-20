import KitDictation
import SwiftUI

/// An annotation's comment field: the composer's editor, so it edits as
/// the composer does (Return adds the comment, Shift-, Option- or
/// ⌘-Return starts a line, Esc cancels), with the mic code comments have
/// inside it. While it records, the strip and the done square take the
/// field's place at its height; Return or done puts the words after the
/// comment with the caret after them, and Esc or the strip's cancel throws
/// the recording away and keeps the field. Return and Esc in the field
/// wait while a recording is going. Comments on selected text, browser
/// elements and diagram parts share it.
struct DictatedCommentField: View {
    @Environment(\.mica) private var theme
    @Binding var text: String
    /// Whether the field has focus; it shows in the field's border.
    @Binding var focused: Bool
    /// Changing it puts focus in the field.
    var focusRequest: Int
    let prompt: String
    let accessibilityLabel: String
    let submit: () -> Void
    let cancel: () -> Void
    /// The arrows, for a field whose empty comment steps through something.
    var up: (() -> Bool)? = nil
    var down: (() -> Bool)? = nil
    /// Focus requests of the field's own, as when the strip goes; each puts
    /// the caret at the end.
    @State private var ownFocus = 0
    /// The field's height before the strip took its place, which the strip keeps.
    @State private var fieldHeight: CGFloat?
    @State private var ownDictation = DictationSlot()
    @Environment(\.dictationSlot) private var givenDictation

    /// The field shows two lines at least, and grows to eight before it scrolls.
    static let minimumLines: CGFloat = 2
    static let maximumLines: CGFloat = 8

    var body: some View {
        HStack(alignment: .top, spacing: 4) {
            if #available(macOS 26, *), dictation.listening {
                DictationStrip(dictation: dictation, done: finishDictation, cancelled: focusAtEnd, size: 13)
                DictationSquare(dictation: dictation, size: 20, done: finishDictation)
            } else {
                GrowingComposerEditor(
                    text: $text, focused: $focused, focusRequest: focusRequest + ownFocus,
                    focusAtEndRequest: ownFocus > 0 ? focusRequest + ownFocus : nil,
                    foreground: theme.text, placeholderColor: theme.muted,
                    attachmentDrop: { _ in false }, attachmentDropTargeted: { _ in },
                    submit: { if !dictating { submit() } },
                    shellEnabled: false, placeholder: prompt, accessibilityLabel: accessibilityLabel,
                    cancel: { if dictating { dictationSlot.reset() } else { cancel() } },
                    theme: theme, maximumHeight: Self.maximumLines * ComposerScrollView.lineHeight,
                    arrow: up == nil && down == nil ? nil : { ($0 ? up : down)?() ?? false },
                    focusFollowsResponder: true)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(minHeight: Self.minimumLines * ComposerScrollView.lineHeight, alignment: .top)
                    .padding(.vertical, 2)
                if #available(macOS 26, *) {
                    DictationMic(dictation: dictation, size: 20, iconSize: 13)
                }
            }
        }
        .frame(height: dictationListening ? fieldHeight : nil, alignment: .top)
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height in
            if !dictationListening { fieldHeight = height }
        }
        .padding(.leading, 10).padding(.trailing, DictationSupport.available ? 4 : 10).padding(.vertical, 6)
        .background(theme.raised, in: RoundedRectangle(cornerRadius: 4))
        .overlay(RoundedRectangle(cornerRadius: 4)
            .strokeBorder(focused || dictating ? theme.accent.opacity(0.6) : theme.border, lineWidth: 1))
        .onChange(of: text) {
            if #available(macOS 26, *) { dictation.dismissProblem() }
        }
        // The editor goes while the strip shows, and may not say it lost focus.
        .onChange(of: dictationListening) { if dictationListening { focused = false } }
    }

    private var dictationSlot: DictationSlot { givenDictation ?? ownDictation }

    @available(macOS 26, *)
    private var dictation: Dictation { dictationSlot.dictation }

    private var dictating: Bool {
        if #available(macOS 26, *) { return dictation.isActive }
        return false
    }

    /// Whether the recording strip stands in the comment's place.
    private var dictationListening: Bool {
        if #available(macOS 26, *) { return dictation.listening }
        return false
    }

    /// Stops recording and puts the words after the comment, with the
    /// field focused and the caret after them.
    private func finishDictation() {
        guard #available(macOS 26, *) else { return }
        let binding = $text
        dictation.finish { words in
            let current = binding.wrappedValue
            binding.wrappedValue = DictationText.join(
                current.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "" : current, words)
            focusAtEnd()
        }
    }

    /// Back to the comment with the caret at its end, as the strip goes.
    /// The editor is remade as the strip goes, and a remade editor takes
    /// only an at-end request.
    private func focusAtEnd() { ownFocus += 1 }
}
