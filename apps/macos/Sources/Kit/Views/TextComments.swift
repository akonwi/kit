import AppKit
import KitDictation
import KitTranscript
import SwiftUI

/// What the comment box at a selection holds while open: the comment
/// being written, and the waiting comment it edits, if any.
@MainActor @Observable final class TextCommentEditor {
    var comment: String
    let editing: UUID?

    init(comment: String = "", editing: UUID? = nil) {
        self.comment = comment; self.editing = editing
    }
}

/// The comment box at selected text (ADR 0070): the words, the comment,
/// which can be dictated, and Comment to add it to the message. Editing a
/// waiting comment offers Save and Delete instead.
struct TextCommentBox: View {
    @Environment(\.mica) private var theme
    @Bindable var editor: TextCommentEditor
    let quote: String
    let submit: () -> Void
    let cancel: () -> Void
    var delete: (() -> Void)? = nil
    @State private var focused = false
    @State private var focusRequest = 0
    @State private var dictationSlot = DictationSlot()

    static let width: CGFloat = 340

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            TextCommentQuote(text: quote, lineLimit: 2)
            DictatedCommentField(text: $editor.comment, focused: $focused, focusRequest: focusRequest, prompt: "Add a comment…",
                                 accessibilityLabel: "Comment on the selected text", submit: add, cancel: cancel)
                .environment(\.dictationSlot, dictationSlot)
            if #available(macOS 26, *), dictation.hasNotice { DictationNotice(dictation: dictation) }
            HStack(spacing: 8) {
                if let delete {
                    Button("Delete", action: delete).buttonStyle(.plain)
                        .font(.kit(size: 12)).foregroundStyle(theme.danger)
                        .padding(.horizontal, 8)
                }
                Spacer(minLength: 4)
                Button("Cancel", action: cancel).buttonStyle(SmallButtonStyle(bordered: true))
                Button(editor.editing == nil ? "Comment" : "Save", action: add)
                    .buttonStyle(SmallButtonStyle(prominent: true))
                    .disabled(empty || dictating)
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 10)
        .frame(width: Self.width)
        .background(theme.surface)
        .onAppear { focusRequest += 1 }
        .onDisappear { dictationSlot.reset() }
    }

    private var empty: Bool { editor.comment.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    private func add() {
        guard !empty, !dictating else { return }
        submit()
    }

    @available(macOS 26, *)
    private var dictation: Dictation { dictationSlot.dictation }

    private var dictating: Bool {
        if #available(macOS 26, *) { return dictation.isActive }
        return false
    }
}

/// Selected words as a comment shows them: muted, after a rule, in the
/// face they were read in.
struct TextCommentQuote: View {
    @Environment(\.mica) private var theme
    let text: String
    var monospaced = false
    var lineLimit: Int? = nil

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Rectangle().fill(theme.border).frame(width: 2)
            Text(text.trimmingCharacters(in: .whitespacesAndNewlines))
                .font(.kit(size: 12, design: monospaced ? .monospaced : .default))
                .foregroundStyle(theme.muted)
                .lineLimit(lineLimit).truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
        }
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// A comment on selected text waiting in the composer: its words and the
/// comment. Clicking it reopens the comment; its × removes it.
struct TextCommentChip: View {
    @Environment(\.mica) private var theme
    let item: TextCommentDraft.Item
    let draft: TextCommentDraft
    @State private var editing = false
    @State private var editor = TextCommentEditor()

    var body: some View {
        CommentChipFrame(help: item.annotation.comment,
                         label: "Comment on “\(Self.excerpt(item.annotation.quote))”: \(item.annotation.comment)") {
            Button {
                editor = TextCommentEditor(comment: item.annotation.comment, editing: item.id)
                editing = true
            } label: {
                HStack(spacing: 6) {
                    IconView(.quotes, size: 12).foregroundStyle(theme.muted)
                    Text(Self.excerpt(item.annotation.quote))
                        .font(.kit(size: 11)).foregroundStyle(theme.muted)
                        .lineLimit(1).truncationMode(.tail).frame(maxWidth: 120, alignment: .leading).fixedSize()
                    Text(item.annotation.comment).lineLimit(1).truncationMode(.tail)
                        .frame(maxWidth: 180, alignment: .leading)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .popover(isPresented: $editing, arrowEdge: .top) {
                TextCommentBox(editor: editor, quote: item.annotation.quote, submit: {
                    draft.update(item.id, comment: editor.comment); editing = false
                }, cancel: { editing = false }, delete: { draft.remove(item.id); editing = false })
            }
        } remove: {
            draft.remove(item.id)
        }
    }

    /// The words' first line, for a chip.
    static func excerpt(_ quote: String) -> String {
        let line = quote.split(whereSeparator: \.isNewline).first.map(String.init) ?? quote
        return line.trimmingCharacters(in: .whitespaces)
    }
}

/// The comments on selected text a sent message carried, collapsed under a
/// count. Open, each shows where its words came from, the words, and the
/// comment. Whether it's open lives with the transcript, so it survives
/// the row being rebuilt, and opening it measures the row again.
struct TranscriptTextComments: View {
    @Environment(\.mica) private var theme
    let comments: [TextAnnotationText.Sent]
    @Bindable var state: ToolDrawerState
    var onExpand: () -> Void = {}

    var body: some View {
        if !comments.isEmpty {
            let open = state.expanded ?? false
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 6) {
                    IconView(open ? .caretDown : .caretRight, size: 10).frame(width: 10)
                    IconView(.quotes, size: 12)
                    Text(Self.title(comments))
                }
                .font(.kit(size: 12)).foregroundStyle(theme.text)
                .frame(height: 20)
                .contentShape(Rectangle())
                .onTapGesture { toggle(open) }
                .accessibilityElement(children: .combine)
                .accessibilityLabel(Self.title(comments))
                .accessibilityAddTraits(.isButton)
                .accessibilityValue(open ? "Expanded" : "Collapsed")
                .accessibilityAction { toggle(open) }
                if open {
                    VStack(alignment: .leading, spacing: 16) {
                        ForEach(comments) { comment in
                            VStack(alignment: .leading, spacing: 6) {
                                Text(comment.source)
                                    .font(.kit(size: 11)).foregroundStyle(theme.muted)
                                    .lineLimit(1).textSelection(.enabled)
                                TextCommentQuote(text: comment.cut ? comment.quote + "…" : comment.quote,
                                                 monospaced: comment.source.hasPrefix("Tool output"), lineLimit: 6)
                                    .textSelection(.enabled)
                                Text(comment.comment).font(.kit(size: 14)).foregroundStyle(theme.text)
                                    .textSelection(.enabled)
                            }
                        }
                    }
                    .padding(.top, 10).padding(.leading, 16)
                }
            }
        }
    }

    private func toggle(_ open: Bool) {
        state.expanded = !open
        if !open { onExpand() }
    }

    static func title(_ comments: [TextAnnotationText.Sent]) -> String {
        "\(comments.count) \(comments.count == 1 ? "comment" : "comments")"
    }
}
