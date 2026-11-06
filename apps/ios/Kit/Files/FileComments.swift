import KitClient
import SwiftUI

/// How a file's lines become a comment's range, and where its comments sit.
enum FileComments {
    /// The anchor for `lines` of the file as read.
    static func anchor(_ file: WireWorkspaceFileRead, lines: ClosedRange<Int>) -> WireAnnotationAnchor {
        WireAnnotationAnchor(kind: .value0, workspaceFile: .init(
            workspaceId: file.workspace.workspaceId, path: file.path, fileRevision: file.revision,
            startLine: lines.lowerBound, endLine: lines.upperBound), workingTreeDiff: nil)
    }

    /// The lines from `start` toward `target`: never above `start`, and no
    /// further than 200 lines or the file's last line.
    static func extend(from start: Int, toward target: Int, count: Int) -> ClosedRange<Int> {
        let end = min(max(start, target), start + AnnotationSelection.maximumLines - 1, max(count, start))
        return start...end
    }

    /// The lines an anchor covers, when it's on `path`.
    static func lines(_ anchor: WireAnnotationAnchor, path: String) -> ClosedRange<Int>? {
        guard let file = anchor.workspaceFile, file.path == path else { return nil }
        return file.startLine...file.endLine
    }

    /// Where the file's comments sit, by the line each goes under; those
    /// whose lines are past the end lead the file, under line 0. Comments
    /// on a diff or another file aren't here.
    static func placement(_ notes: [FileAnnotation], path: String, count: Int, except editing: UInt64? = nil)
        -> [Int: [FileAnnotation]] {
        var placed: [Int: [FileAnnotation]] = [:]
        for note in notes where !note.isDiff && note.path == path && note.id != editing {
            placed[note.endLine <= count ? note.endLine : 0, default: []].append(note)
        }
        return placed
    }
}

/// A file's lines with its comments under them, and the one being written
/// under its selection. A tap on a line number selects that line; the
/// handle under the selection drags it longer. While the handle moves,
/// what sits below the selection steps aside, so the lines stay put under
/// the finger.
struct CommentableFile: View {
    let file: WireWorkspaceFileRead
    let lines: [String]
    var colored: [AttributedString]?
    let wrap: Bool
    var marked: Int?
    let annotations: AnnotationState
    let save: (WireAnnotationAnchor, AnnotationState.Editor) -> Void
    let remove: (FileAnnotation) -> Void
    /// A comment with its whole text; a snapshot carries only its start.
    let complete: (FileAnnotation) async -> FileAnnotation?
    /// The lines the handle covers while it's dragged.
    @State private var dragged: ClosedRange<Int>?

    /// The comment being written, when it's on this file.
    private var editor: AnnotationState.Editor? {
        guard let editor = annotations.editor, FileComments.lines(editor.anchor, path: file.path) != nil else { return nil }
        return editor
    }
    private var selected: ClosedRange<Int>? {
        dragged ?? editor.flatMap { FileComments.lines($0.anchor, path: file.path) }
    }

    var body: some View {
        let selected = selected
        let placed = FileComments.placement(annotations.records, path: file.path, count: lines.count,
                                            except: editor?.editing)
        FileLines(lines: lines, colored: colored, wrap: wrap, marked: marked, selected: selected,
                  handle: annotations.pending || editor == nil ? nil : selected?.upperBound,
                  tap: select, drag: drag, drop: drop) { line in
            // While the handle moves, nothing sits under the selection.
            if dragged == nil || line < (selected?.lowerBound ?? 0) {
                VStack(spacing: 0) {
                    ForEach(placed[line] ?? []) { note in card(note) }
                    if dragged == nil, let editor, let selected, selected.upperBound == line {
                        DiffCommentEditor(annotations: annotations, anchor: editor.anchor) {
                            save(FileComments.anchor(file, lines: selected), editor)
                        }
                    }
                }
            }
        }
    }

    private func card(_ note: FileAnnotation) -> some View {
        DiffCommentCard(note: note, enabled: !annotations.pending && annotations.editor == nil,
                        placing: annotations.replacement?.id == note.id,
                        edit: {
                            Task {
                                guard let full = await complete(note) else { return }
                                annotations.begin(anchor: full.anchor, editing: full)
                            }
                        },
                        delete: { remove(note) },
                        selectNewRange: {
                            Task {
                                guard let full = await complete(note) else { return }
                                annotations.replacement = full
                            }
                        },
                        stopPlacing: { annotations.replacement = nil })
    }

    private func select(_ line: Int) {
        guard !annotations.pending, lines.indices.contains(line - 1) else { return }
        move(to: FileComments.anchor(file, lines: line...line))
    }

    private func drag(_ target: Int) {
        guard let start = selected?.lowerBound else { return }
        dragged = FileComments.extend(from: start, toward: target, count: lines.count)
    }

    private func drop() {
        defer { dragged = nil }
        guard let dragged, dragged != editor.flatMap({ FileComments.lines($0.anchor, path: file.path) }) else { return }
        move(to: FileComments.anchor(file, lines: dragged))
    }

    /// Opens the box on new lines, keeping what was written.
    private func move(to anchor: WireAnnotationAnchor) {
        if annotations.editor != nil { annotations.reselectEditor() }
        annotations.begin(anchor: anchor)
    }
}
