import KitClient
import SwiftUI

/// How a diff's rows become a comment's range. A comment covers lines on
/// one side: removed lines are the old file's, the rest are the new file's.
enum DiffComments {
    /// The side a selection starting on `row` covers.
    static func side(_ row: DiffDisplayRow) -> String {
        row.kind == "deletion" ? "old" : "new"
    }

    /// Whether a row can start a comment: it has a line number.
    static func selectable(_ row: DiffDisplayRow) -> Bool {
        row.number(side(row)) != nil
    }

    /// The anchor for the rows from `start` toward `target`, stopping at
    /// the last row the range can reach: the same side, one hunk, and up to
    /// 200 lines. Removed lines only reach other removed lines. Nil when not even `start` can take a comment.
    static func extend(_ document: DiffDocument, from start: Int, toward target: Int, side: String)
        -> WireAnnotationAnchor? {
        guard document.rows.indices.contains(start) else { return nil }
        var best = document.anchor(rows: start...start, side: side)
        guard best != nil else { return nil }
        let step = target >= start ? 1 : -1
        var index = start
        while index != target {
            index += step
            guard document.rows.indices.contains(index) else { break }
            // The gutter shows old numbers only on removed lines, so those
            // are all a selection of old lines covers.
            if side == "old" && !["deletion", "marker"].contains(document.rows[index].kind) { break }
            guard let anchor = document.anchor(rows: min(start, index)...max(start, index), side: side) else {
                // Lines from the other side pass under it; a gap or the limit ends it.
                let row = document.rows[index]
                if row.kind != "hunk" && row.number(side) == nil { continue }
                break
            }
            best = anchor
        }
        return best
    }

    /// The rows a comment covers. A comment written before the diff's
    /// revision moved still finds its rows while its file is unchanged.
    static func rows(_ document: DiffDocument, _ anchor: WireAnnotationAnchor) -> ClosedRange<Int>? {
        if let rows = document.range(anchor) { return rows }
        guard anchor.workingTreeDiff?.fileRevision == document.file.fileRevision else { return nil }
        return nearby(document, anchor)
    }

    /// Whether a comment was written on this file of this comparison.
    static func belongs(_ note: FileAnnotation, to document: DiffDocument) -> Bool {
        note.diffAnchor?.path == document.file.path && note.diffAnchor?.targetId == document.observation.target.id
    }

    /// The rows that now carry a comment's line numbers, whatever changed
    /// under it: where a comment whose code changed is shown.
    static func nearby(_ document: DiffDocument, _ anchor: WireAnnotationAnchor) -> ClosedRange<Int>? {
        guard let diff = anchor.workingTreeDiff, diff.path == document.file.path,
              diff.targetId == document.observation.target.id else { return nil }
        let lines = diff.startLine...diff.endLine
        let indices = document.rows.indices.filter { document.rows[$0].number(diff.side).map(lines.contains) ?? false }
        guard let first = indices.first, let last = indices.last else { return nil }
        return first...last
    }

    /// The runs of rows between the places something sits under a row.
    static func segments(count: Int, after breaks: Set<Int>) -> [Range<Int>] {
        var segments: [Range<Int>] = [], start = 0
        for end in breaks.sorted() where end >= start && end < count {
            segments.append(start..<(end + 1))
            start = end + 1
        }
        if start < count || segments.isEmpty { segments.append(start..<count) }
        return segments
    }

    /// A saved comment's card, to scroll to.
    static func cardID(_ id: UInt64) -> String { "comment-\(id)" }

    /// The line a saved comment's card leads with.
    static func caption(_ note: FileAnnotation) -> String {
        let lines = note.startLine == note.endLine ? "line \(note.startLine)" : "lines \(note.rangeLabel)"
        let removed = note.diffAnchor?.side == "old" ? "Removed " + lines : lines.prefix(1).uppercased() + lines.dropFirst()
        return removed + " · goes with your next message"
    }

    /// What the comment box says it's about: `Comment on lines 1671–1673`.
    static func title(_ anchor: WireAnnotationAnchor) -> String {
        guard let diff = anchor.workingTreeDiff else { return "Comment" }
        let removed = diff.side == "old" ? "removed " : ""
        if diff.startLine == diff.endLine { return "Comment on \(removed)line \(diff.startLine)" }
        return "Comment on \(removed)lines \(diff.startLine)–\(diff.endLine)"
    }
}

/// The box under selected lines where a comment is written. Cancel drops
/// it; Comment saves it to the session, to go with the next message.
struct DiffCommentEditor: View {
    @Bindable var annotations: AnnotationState
    let anchor: WireAnnotationAnchor
    let save: () -> Void
    @FocusState private var focused: Bool

    private var canSave: Bool { !annotations.pending && FileAnnotation.validBody(annotations.draft) }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(DiffComments.title(anchor))
                .font(.system(size: 13))
                .foregroundStyle(Theme.muted)
            TextField("Add a comment", text: $annotations.draft, axis: .vertical)
                .font(.system(size: 16))
                .foregroundStyle(Theme.text)
                .lineLimit(1...8)
                .focused($focused)
                .disabled(annotations.pending)
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
                .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.cardRadius))
                .overlay {
                    RoundedRectangle(cornerRadius: Theme.cardRadius)
                        .strokeBorder(focused ? Theme.accent : Theme.border, lineWidth: 1)
                }
                .accessibilityIdentifier("diff-comment-field")
            if let error = annotations.error {
                Text(error)
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 8) {
                Spacer()
                Button("Cancel") { annotations.cancelEditor() }
                    .buttonStyle(CommentButtonStyle(prominent: false))
                    .disabled(annotations.pending)
                    .accessibilityIdentifier("diff-comment-cancel")
                Button(action: save) {
                    if annotations.pending { ProgressView().controlSize(.small).tint(Theme.surface) } else { Text("Comment") }
                }
                .buttonStyle(CommentButtonStyle(prominent: true))
                .disabled(!canSave)
                .accessibilityIdentifier("diff-comment-save")
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.surface)
        .onAppear { focused = true }
    }
}

/// Cancel and Comment: capsules, the second filled with the text color.
private struct CommentButtonStyle: ButtonStyle {
    let prominent: Bool
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 15, weight: .semibold))
            .foregroundStyle(prominent ? Theme.surface : Theme.text)
            .padding(.horizontal, 16)
            .frame(minWidth: 72, minHeight: 36)
            .background(prominent ? Theme.text : Theme.hover, in: Capsule())
            .opacity(enabled ? (configuration.isPressed ? 0.7 : 1) : 0.4)
    }
}

/// The comment Changes is bringing into view, and what its card calls once
/// it's shown so Changes can scroll to it.
struct CommentReveal {
    var id: UInt64?
    var shown: (UInt64) -> Void = { _ in }
}

extension EnvironmentValues {
    @Entry var commentReveal = CommentReveal()
}

/// A saved comment under its lines, waiting to go with the next message.
/// Its menu edits it in the box or deletes it.
struct DiffCommentCard: View {
    let note: FileAnnotation
    let enabled: Bool
    /// Whether a tap on a line number places this comment.
    var placing = false
    let edit: () -> Void
    let delete: () -> Void
    var selectNewRange: () -> Void = {}
    var stopPlacing: () -> Void = {}
    @Environment(\.commentReveal) private var reveal

    var body: some View {
        Group {
            if note.stale {
                changed
                    .accessibilityElement(children: .contain)
                    .accessibilityLabel("Diff changed, \(note.body)")
            } else {
                saved.accessibilityElement(children: .combine)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(Theme.raised, in: RoundedRectangle(cornerRadius: Theme.cardRadius))
        .overlay { RoundedRectangle(cornerRadius: Theme.cardRadius).strokeBorder(Theme.border, lineWidth: 0.5) }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .accessibilityIdentifier("diff-comment-card")
        .id(DiffComments.cardID(note.id))
        .onChange(of: reveal.id == note.id, initial: true) { _, revealed in
            if revealed { reveal.shown(note.id) }
        }
    }

    /// The code under it changed: say so, and offer to move or delete it.
    private var changed: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("Diff changed", systemImage: "exclamationmark.triangle.fill")
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(Theme.warning)
            Text(note.body)
                .font(.system(size: 16))
                .foregroundStyle(Theme.muted)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            if placing {
                HStack(spacing: 12) {
                    Text("Tap a line number to place it.")
                        .font(.system(size: 14))
                        .foregroundStyle(Theme.muted)
                    Spacer(minLength: 0)
                    Button("Cancel", action: stopPlacing)
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Theme.text)
                        .accessibilityIdentifier("diff-comment-stop-placing")
                }
            } else {
                HStack(spacing: 20) {
                    Button("Select new range", action: selectNewRange)
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Theme.text)
                        .padding(.horizontal, 14)
                        .frame(minHeight: 36)
                        .overlay { Capsule().strokeBorder(Theme.strongBorder, lineWidth: 1) }
                        .accessibilityIdentifier("diff-comment-select-range")
                    Button("Delete", role: .destructive, action: delete)
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Theme.danger)
                        .accessibilityIdentifier("diff-comment-delete")
                }
                .buttonStyle(.plain)
                .disabled(!enabled)
            }
        }
    }

    private var saved: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: "text.bubble")
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.accent)
                Text(DiffComments.caption(note))
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
                Spacer(minLength: 4)
                Menu {
                    Button("Edit", systemImage: "pencil", action: edit)
                    Button("Delete", systemImage: "trash", role: .destructive, action: delete)
                } label: {
                    Image(systemName: "ellipsis")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundStyle(Theme.muted)
                        .frame(width: 32, height: 24)
                        .contentShape(Rectangle())
                }
                .disabled(!enabled)
                .accessibilityLabel("Comment options")
                .accessibilityIdentifier("diff-comment-menu")
            }
            Text(note.body)
                .font(.system(size: 16))
                .foregroundStyle(Theme.text)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
