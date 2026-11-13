import KitDictation
import SwiftUI

/// An annotation's comment field with the mic that code comments have,
/// inside it. While it records, the strip and the done square take the
/// field's place at its height; Return or done puts the words after the
/// comment with the caret after them, and Esc or the strip's cancel throws
/// the recording away and keeps the field. Return and Esc in the field
/// wait while a recording is going. The browser's and diagrams' Annotate
/// modes share it.
struct DictatedCommentField: View {
    @Environment(\.mica) private var theme
    @Binding var text: String
    var focused: FocusState<Bool>.Binding
    let prompt: String
    let accessibilityLabel: String
    let submit: () -> Void
    let cancel: () -> Void
    /// The arrows, for a field whose empty comment steps through something.
    var up: (() -> KeyPress.Result)? = nil
    var down: (() -> KeyPress.Result)? = nil
    @State private var selection: TextSelection?
    @State private var caretToEnd = false
    /// The field's height before the strip took its place, which the strip keeps.
    @State private var fieldHeight: CGFloat?
    @State private var ownDictation = DictationSlot()
    @Environment(\.dictationSlot) private var givenDictation

    var body: some View {
        HStack(alignment: .top, spacing: 4) {
            if #available(macOS 26, *), dictation.listening {
                DictationStrip(dictation: dictation, done: finishDictation, cancelled: focusAtEnd, size: 13)
                DictationSquare(dictation: dictation, size: 20, done: finishDictation)
            } else {
                TextField("", text: $text, selection: $selection,
                          prompt: Text(prompt).foregroundStyle(theme.muted), axis: .vertical)
                    .textFieldStyle(.plain)
                    .font(.kit(size: 13))
                    .foregroundStyle(theme.text)
                    .lineLimit(2...8)
                    .focused(focused)
                    .onSubmit { if !dictating { submit() } }
                    .onExitCommand { if dictating { dictationSlot.reset() } else { cancel() } }
                    .onKeyPress(.upArrow) { up?() ?? .ignored }
                    .onKeyPress(.downArrow) { down?() ?? .ignored }
                    .padding(.vertical, 2)
                    .accessibilityLabel(accessibilityLabel)
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
            .strokeBorder(focused.wrappedValue || dictating ? theme.accent.opacity(0.6) : theme.border, lineWidth: 1))
        .onChange(of: focused.wrappedValue) {
            guard focused.wrappedValue, caretToEnd else { return }
            caretToEnd = false
            selection = TextSelection(insertionPoint: text.endIndex)
        }
        .onChange(of: text) {
            if #available(macOS 26, *) { dictation.dismissProblem() }
        }
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
    /// Focus selects the whole comment, so the caret moves once it has.
    private func focusAtEnd() {
        caretToEnd = true
        focused.wrappedValue = true
    }
}
