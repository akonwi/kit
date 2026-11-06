import KitClient
import SwiftUI

extension EnvironmentValues {
    /// Opens a sent comment where it was written: its file at its lines, or
    /// Changes for a comment on a diff.
    @Entry var openComment: ((FileAnnotation) -> Void)? = nil
}

/// The comments a message carried, a card each under its text: the file and
/// lines, the comment, and the first lines it was written on. A tap opens
/// the comment where it was written.
struct SentComments: View {
    let notes: [FileAnnotation]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(notes) { SentCommentCard(note: $0) }
        }
    }
}

struct SentCommentCard: View {
    let note: FileAnnotation
    @Environment(\.openComment) private var open

    /// The most lines of code a card quotes.
    nonisolated static let quotedLines = 3

    var body: some View {
        Button { open?(note) } label: {
            VStack(alignment: .leading, spacing: 6) {
                Label {
                    Text(note.label)
                        .font(.system(size: 13, design: .monospaced))
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                        .truncationMode(.middle)
                } icon: {
                    Image(systemName: "text.bubble")
                        .font(.system(size: 12))
                        .foregroundStyle(Theme.accent)
                }
                .labelStyle(.titleAndIcon)
                Text(note.body)
                    .font(.system(size: 16))
                    .foregroundStyle(Theme.text)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                quote
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.cardRadius - 2))
            .contentShape(RoundedRectangle(cornerRadius: Theme.cardRadius - 2))
        }
        .buttonStyle(.plain)
        .disabled(open == nil)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Comment on \(note.label), \(note.body)")
        .accessibilityHint(open == nil ? "" : note.isDiff ? "Shows it in Changes" : "Opens the file at these lines")
        .accessibilityAddTraits(.isButton)
        .accessibilityIdentifier("sent-comment")
    }

    /// The first lines the comment was written on. A comment on removed
    /// lines covers only removed lines, so they're tinted as removed.
    @ViewBuilder private var quote: some View {
        let lines = Self.lines(note.source)
        let removed = note.diffAnchor?.side == "old"
        if !lines.isEmpty {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                    Text(removed ? "- " + line : line)
                        .font(.system(size: 13, design: .monospaced))
                        .foregroundStyle(removed ? Theme.danger : Theme.text)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 6)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(removed ? Theme.diffRemoved : Theme.raised, in: RoundedRectangle(cornerRadius: Theme.controlRadius))
            .overlay {
                if !removed {
                    RoundedRectangle(cornerRadius: Theme.controlRadius).strokeBorder(Theme.border, lineWidth: 0.5)
                }
            }
        }
    }

    /// Up to `quotedLines` lines of `source`, tabs as four spaces, less
    /// the indent they all share.
    nonisolated static func lines(_ source: String) -> [String] {
        guard !source.isEmpty else { return [] }
        let lines = source.split(separator: "\n", omittingEmptySubsequences: false)
            .prefix(quotedLines)
            .map { $0.replacingOccurrences(of: "\t", with: "    ") }
        let indent = lines.filter { !$0.allSatisfy(\.isWhitespace) }
            .map { $0.prefix(while: { $0 == " " }).count }
            .min() ?? 0
        return lines.map { String($0.dropFirst(min(indent, $0.prefix(while: { $0 == " " }).count))) }
    }
}
