import KitTranscript
import SwiftUI

// Comments on selected text a sent message carried (ADR 0070), as on the
// Mac: closed under a count, and open, each comment's source, the words it
// answered, and the comment.

/// A sent message's comments on selected text, closed under a count.
struct SentTextComments: View {
    let comments: [TextAnnotationText.Sent]
    @State private var open = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button {
                withAnimation(.snappy) { open.toggle() }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 11, weight: .semibold))
                        .rotationEffect(.degrees(open ? 90 : 0))
                        .foregroundStyle(Theme.muted)
                    Image(systemName: "quote.opening")
                        .font(.system(size: 12))
                        .foregroundStyle(Theme.accent)
                    Text(Self.title(comments))
                        .font(.system(size: 14))
                        .foregroundStyle(Theme.text)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(Self.title(comments))
            .accessibilityValue(open ? "Expanded" : "Collapsed")
            if open {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(comments) { comment in
                        SentTextCommentCard(comment: comment)
                    }
                }
            }
        }
        .accessibilityIdentifier("sent-text-comments")
    }

    /// "1 comment", "2 comments".
    static func title(_ comments: [TextAnnotationText.Sent]) -> String {
        "\(comments.count) \(comments.count == 1 ? "comment" : "comments")"
    }
}

/// One sent comment on selected text: where the words came from, the words
/// after a rule, and the comment.
private struct SentTextCommentCard: View {
    let comment: TextAnnotationText.Sent

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(comment.source)
                .font(.system(size: 12))
                .foregroundStyle(Theme.muted)
                .lineLimit(1)
                .truncationMode(.tail)
            HStack(alignment: .top, spacing: 8) {
                Rectangle().fill(Theme.border).frame(width: 2)
                Text(comment.cut ? comment.quote + "…" : comment.quote)
                    .font(.system(size: 13, design: comment.source.hasPrefix("Tool output") ? .monospaced : .default))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(6)
                    .truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .fixedSize(horizontal: false, vertical: true)
            Text(comment.comment)
                .font(.system(size: 16))
                .foregroundStyle(Theme.text)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.cardRadius - 2))
        .accessibilityElement(children: .combine)
    }
}
