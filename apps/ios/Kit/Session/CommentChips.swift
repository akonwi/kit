import KitClient
import SwiftUI

/// The session's comments above the composer, a chip each, to go with the
/// next message. A tap shows the comment in Changes or its file; × deletes it.
struct CommentChips: View {
    let composer: SessionComposer
    let reveal: ((FileAnnotation) -> Void)?
    /// Which comments a tap can show from here.
    var revealable: (FileAnnotation) -> Bool = { $0.isDiff }

    var body: some View {
        let notes = composer.annotations.records
        if !notes.isEmpty {
            ScrollView(.horizontal) {
                HStack(spacing: 8) {
                    ForEach(notes) { note in
                        CommentChip(note: note, enabled: !composer.annotations.pending,
                                    reveal: revealable(note) ? reveal.map { reveal in { reveal(note) } } : nil,
                                    remove: { composer.removeComment(note) })
                    }
                }
                .padding(.horizontal, 4)
                .padding(.vertical, 4)
            }
            .scrollIndicators(.hidden)
            .accessibilityIdentifier("comment-chips")
        }
    }
}

/// One comment: its file and lines, and × to delete it. A comment whose
/// code changed shows a warning instead of the bubble.
private struct CommentChip: View {
    let note: FileAnnotation
    let enabled: Bool
    let reveal: (() -> Void)?
    let remove: () -> Void

    var body: some View {
        HStack(spacing: 0) {
            Button { reveal?() } label: {
                HStack(spacing: 6) {
                    Image(systemName: note.stale ? "exclamationmark.triangle.fill" : "text.bubble")
                        .font(.system(size: 13))
                        .foregroundStyle(note.stale ? Theme.warning : Theme.accent)
                    Text(note.label)
                        .font(.system(size: 13, design: .monospaced))
                        .foregroundStyle(Theme.text)
                        .lineLimit(1)
                }
                .padding(.leading, 12)
                .padding(.trailing, 4)
                .frame(minHeight: 36)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(reveal == nil)
            .accessibilityLabel(Self.accessibilityLabel(note))
            .accessibilityHint(reveal == nil ? "" : note.isDiff ? "Shows the comment in Changes" : "Shows the comment in its file")
            .accessibilityIdentifier("comment-chip")
            Button(action: remove) {
                Image(systemName: "xmark")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 32, height: 36)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(!enabled)
            .accessibilityLabel("Delete comment on \(note.label)")
            .accessibilityIdentifier("comment-chip-delete")
        }
        .padding(.trailing, 4)
        .background(Theme.surface, in: Capsule())
        .overlay { Capsule().strokeBorder(Theme.border, lineWidth: 0.5) }
    }

    static func accessibilityLabel(_ note: FileAnnotation) -> String {
        ["Comment on \(note.label)", note.statusLabel, note.body].compactMap { $0 }.joined(separator: ", ")
    }
}
