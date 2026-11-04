import KitClient
import SwiftUI

/// A file's header: a caret, its folder and name, and its line counts. It
/// pins to the top while its file scrolls, and a tap folds the file.
struct DiffFileHeader: View {
    let file: WireDiffFileSummary
    let collapsed: Bool
    let toggle: () -> Void

    var body: some View {
        let path = ChangesFormat.split(file.path)
        Button(action: toggle) {
            HStack(spacing: 8) {
                Image(systemName: collapsed ? "chevron.right" : "chevron.down")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 12)
                (Text(path.folder).foregroundStyle(Theme.muted)
                    + Text(path.name).fontWeight(.semibold).foregroundStyle(Theme.text))
                    .font(.system(size: 13, design: .monospaced))
                    .lineLimit(1)
                    .truncationMode(.head)
                Spacer(minLength: 8)
                HStack(spacing: 6) {
                    if let additions = file.additions, additions > 0 { Text("+\(additions)").foregroundStyle(Theme.success) }
                    if let deletions = file.deletions, deletions > 0 { Text("−\(deletions)").foregroundStyle(Theme.danger) }
                }
                .font(.system(size: 12, design: .monospaced))
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 40)
            .background(Theme.raised)
            .overlay(alignment: .top) { Rectangle().fill(Theme.border).frame(height: 0.5) }
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.border).frame(height: 0.5) }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Self.accessibilityLabel(file, collapsed: collapsed))
        .accessibilityHint(collapsed ? "Shows the file’s changes" : "Hides the file’s changes")
        .accessibilityIdentifier("diff-file")
    }

    static func accessibilityLabel(_ file: WireDiffFileSummary, collapsed: Bool) -> String {
        var parts = [file.path]
        if let additions = file.additions, additions > 0 { parts.append("\(additions) added") }
        if let deletions = file.deletions, deletions > 0 { parts.append("\(deletions) removed") }
        if collapsed { parts.append("folded") }
        return parts.joined(separator: ", ")
    }
}

/// A file's diff under its header, loading as it comes into view.
struct DiffFileBody: View {
    let file: WireDiffFileSummary
    let diff: DiffState
    let annotations: AnnotationState
    let client: ServerClient
    let session: String

    private var reader: DiffState? { diff.fileStates[file.path] }
    private var textual: Bool { file.contentState == "text" }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !textual {
                DiffNote(text: ChangesFormat.contentNote(file))
            } else if let reader, let document = reader.document, !document.rows.isEmpty {
                CommentableDiff(document: document, wrap: diff.wrapLines, annotations: annotations) { anchor, editor in
                    Task {
                        await annotations.save(client: client, session: session, anchor: anchor,
                                               editing: editor.editing, replacing: editor.replacing)
                    }
                } remove: { note in
                    Task { await annotations.remove(client: client, session: session, id: note.id) }
                } complete: { note in
                    if note.complete { return note }
                    do { try await annotations.refresh(client: client, session: session) } catch {
                        annotations.report(error)
                        return nil
                    }
                    return annotations.records.first { $0.id == note.id && $0.complete }
                }
                if let notice = reader.notice { DiffNote(text: notice) }
                if reader.lineCursor != nil {
                    DiffNote(text: "Loading more…", loading: true)
                        .task(id: "\(reader.lineCursor ?? "")\(reader.loading)") {
                            guard !reader.loading else { return }
                            await reader.read(file, client: client, session: session, more: true)
                        }
                }
            } else if let error = reader?.error {
                HStack(spacing: 12) {
                    DiffNote(text: "Couldn’t load this file. \(error)", warning: true)
                    Button("Try Again") { read() }
                        .font(.system(size: 13, weight: .semibold))
                        .padding(.trailing, 16)
                }
            } else if let notice = reader?.notice, reader?.loading == false {
                DiffNote(text: notice)
            } else {
                DiffNote(text: "Loading…", loading: true)
            }
        }
        .task(id: diff.observation?.revision ?? "") {
            guard textual else { return }
            await diff.appeared(file, client: client, session: session)
        }
    }

    private func read() {
        guard let reader else { return }
        Task { await reader.read(file, client: client, session: session) }
    }
}

/// A file's rows with its comments under their lines, and the one being
/// written under its selection. A tap on a line number selects that line;
/// the handle under the selection drags it longer. While the handle moves,
/// what sits below the selection steps aside, so the rows stay put under
/// the finger.
struct CommentableDiff: View {
    let document: DiffDocument
    let wrap: Bool
    let annotations: AnnotationState
    let save: (WireAnnotationAnchor, AnnotationState.Editor) -> Void
    let remove: (FileAnnotation) -> Void
    /// A comment with its whole text; a snapshot carries only its start.
    let complete: (FileAnnotation) async -> FileAnnotation?
    /// The rows the handle covers while it's dragged.
    @State private var dragged: ClosedRange<Int>?
    @State private var refusal: String?

    /// The comment being written, when it's on this file.
    private var editor: AnnotationState.Editor? {
        guard let editor = annotations.editor, editor.anchor.workingTreeDiff?.path == document.file.path else { return nil }
        return editor
    }
    private var side: String? { editor?.anchor.workingTreeDiff?.side }
    private var selected: ClosedRange<Int>? {
        dragged ?? editor.flatMap { DiffComments.rows(document, $0.anchor) }
    }
    /// The file's comments here, and the rows each sits under. One being
    /// edited shows as the box instead. One whose code changed sits under
    /// the rows that now carry its line numbers.
    private var placed: [(note: FileAnnotation, rows: ClosedRange<Int>)] {
        annotations.records.compactMap { note in
            guard DiffComments.belongs(note, to: document), note.id != editor?.editing,
                  let rows = note.stale ? DiffComments.nearby(document, note.anchor)
                                        : DiffComments.rows(document, note.anchor) else { return nil }
            return (note, rows)
        }
    }
    /// Comments whose code changed and whose lines are gone; they lead the file.
    private var unplaced: [FileAnnotation] {
        annotations.records.filter { note in
            note.stale && DiffComments.belongs(note, to: document) && DiffComments.nearby(document, note.anchor) == nil
        }
    }

    var body: some View {
        let selected = selected
        let placed = placed
        let boxEnd = dragged == nil && editor != nil ? selected?.upperBound : nil
        var breaks = Set(placed.map(\.rows.upperBound))
        if let boxEnd { breaks.insert(boxEnd) }
        if dragged != nil, let start = selected?.lowerBound { breaks = breaks.filter { $0 < start } }
        let segments = DiffComments.segments(count: document.rows.count, after: breaks)
        return VStack(alignment: .leading, spacing: 0) {
            if let refusal { DiffNote(text: refusal, warning: true) }
            ForEach(unplaced) { note in card(note) }
            ForEach(segments, id: \.lowerBound) { segment in
                let last = segment.upperBound - 1
                DiffLines(document: document, rows: segment, wrap: wrap, selected: selected,
                          handle: annotations.pending ? nil : selected?.upperBound,
                          tap: select, drag: drag, drop: drop)
                    // The handle hangs over the top edge of what's below.
                    .zIndex(1)
                if breaks.contains(last) {
                    ForEach(placed.filter { $0.rows.upperBound == last }, id: \.note.id) { item in
                        card(item.note)
                    }
                    if boxEnd == last, let editor, let selected, let side {
                        DiffCommentEditor(annotations: annotations, anchor: editor.anchor) {
                            save(document.anchor(rows: selected, side: side) ?? editor.anchor, editor)
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
                                refusal = nil
                                annotations.replacement = full
                            }
                        },
                        stopPlacing: { annotations.replacement = nil })
    }

    private func select(_ index: Int) {
        let row = document.rows[index]
        guard DiffComments.selectable(row), !annotations.pending else { return }
        guard let anchor = document.anchor(rows: index...index, side: DiffComments.side(row)) else {
            refusal = "This line can’t take a comment."
            return
        }
        refusal = nil
        move(to: anchor)
    }

    private func drag(_ target: Int) {
        guard let start = selected?.lowerBound, let side,
              let anchor = DiffComments.extend(document, from: start, toward: max(start, target), side: side) else { return }
        dragged = document.range(anchor)
    }

    private func drop() {
        defer { dragged = nil }
        guard let dragged, let side, dragged != editor.flatMap({ DiffComments.rows(document, $0.anchor) }),
              let anchor = document.anchor(rows: dragged, side: side) else { return }
        move(to: anchor)
    }

    /// Opens the box on new lines, keeping what was written.
    private func move(to anchor: WireAnnotationAnchor) {
        if annotations.editor != nil { annotations.reselectEditor() }
        annotations.begin(anchor: anchor)
    }
}

/// A file's unified diff rows: a line number and sign, then the line on a
/// green or red wash. Long lines scroll sideways unless they wrap. Selected
/// lines fill their numbers with the accent.
struct DiffLines: View {
    let document: DiffDocument
    var rows: Range<Int>
    let wrap: Bool
    var selected: ClosedRange<Int>? = nil
    /// The row the selection handle hangs under.
    var handle: Int? = nil
    var tap: (Int) -> Void = { _ in }
    var drag: (Int) -> Void = { _ in }
    var drop: () -> Void = {}
    @State private var codeWidth: CGFloat = 0
    /// Where each row ends, measured when lines wrap.
    @State private var bottoms: [Int: CGFloat] = [:]

    init(document: DiffDocument, rows: Range<Int>? = nil, wrap: Bool, selected: ClosedRange<Int>? = nil,
         handle: Int? = nil, tap: @escaping (Int) -> Void = { _ in }, drag: @escaping (Int) -> Void = { _ in },
         drop: @escaping () -> Void = {}) {
        self.document = document
        self.rows = rows ?? document.rows.indices
        self.wrap = wrap
        self.selected = selected
        self.handle = handle
        self.tap = tap
        self.drag = drag
        self.drop = drop
    }

    static let rowHeight: CGFloat = 20
    static let signWidth: CGFloat = 22
    static let font = Font.system(size: 12, design: .monospaced)
    private static let space = "diff-lines"

    var body: some View {
        let numberWidth = Self.numberWidth(document)
        Group {
            if wrap {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(rows, id: \.self) { index in
                        let row = document.rows[index]
                        HStack(alignment: .firstTextBaseline, spacing: 0) {
                            gutter(index, numberWidth: numberWidth)
                            Text(Self.display(row.text))
                                .foregroundStyle(Self.textColor(row))
                                .italic(row.kind == "marker")
                                .padding(.vertical, 2)
                                .padding(.trailing, 12)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .frame(minHeight: Self.rowHeight)
                        .background(Self.wash(row))
                        .onGeometryChange(for: CGFloat.self) { $0.frame(in: .named(Self.space)).maxY } action: {
                            bottoms[index] = $0
                        }
                    }
                }
            } else {
                HStack(alignment: .top, spacing: 0) {
                    VStack(spacing: 0) {
                        ForEach(rows, id: \.self) { index in
                            gutter(index, numberWidth: numberWidth)
                                .frame(height: Self.rowHeight)
                                .background(selected?.contains(index) == true ? Theme.accent : Self.wash(document.rows[index]))
                        }
                    }
                    ScrollView(.horizontal) {
                        VStack(alignment: .leading, spacing: 0) {
                            ForEach(rows, id: \.self) { index in
                                let row = document.rows[index]
                                Text(Self.display(row.text))
                                    .foregroundStyle(Self.textColor(row))
                                    .italic(row.kind == "marker")
                                    .lineLimit(1)
                                    .fixedSize()
                                    .padding(.trailing, 16)
                                    .frame(maxWidth: .infinity, minHeight: Self.rowHeight, maxHeight: Self.rowHeight, alignment: .leading)
                                    .background(Self.wash(row))
                            }
                        }
                        .frame(minWidth: codeWidth, alignment: .leading)
                    }
                    .scrollIndicators(.hidden)
                    .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { codeWidth = $0 }
                }
            }
        }
        .font(Self.font)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Self.accessibilityText(document, rows: rows))
        .accessibilityIdentifier("diff-lines")
        .overlay(alignment: .topLeading) {
            if let handle, rows.contains(handle) {
                SelectionHandle()
                    .offset(x: numberWidth + Self.signWidth - SelectionHandle.size / 2,
                            y: bottom(of: handle) - SelectionHandle.size / 2)
                    .gesture(DragGesture(minimumDistance: 0, coordinateSpace: .named(Self.space))
                        .onChanged { drag(row(at: $0.location.y)) }
                        .onEnded { _ in drop() })
            }
        }
        .coordinateSpace(.named(Self.space))
    }

    private func gutter(_ index: Int, numberWidth: CGFloat) -> some View {
        let row = document.rows[index]
        let chosen = selected?.contains(index) == true
        return HStack(spacing: 0) {
            Text(Self.number(row).map(String.init) ?? "")
                .foregroundStyle(chosen ? Color.white : Theme.muted)
                .frame(width: numberWidth, alignment: .trailing)
            Text(Self.sign(row))
                .foregroundStyle(chosen ? Color.white
                                 : row.kind == "addition" ? Theme.success : row.kind == "deletion" ? Theme.danger : Theme.muted)
                .frame(width: Self.signWidth, alignment: .center)
        }
        .background { if wrap && chosen { Theme.accent } }
        .contentShape(Rectangle())
        .onTapGesture { tap(index) }
    }

    /// The bottom edge of a row in this block.
    private func bottom(of index: Int) -> CGFloat {
        if wrap, let bottom = bottoms[index] { return bottom }
        return CGFloat(index - rows.lowerBound + 1) * Self.rowHeight
    }

    /// The row under a point in this block.
    private func row(at y: CGFloat) -> Int {
        if wrap {
            return rows.first { (bottoms[$0] ?? 0) > y } ?? rows.upperBound - 1
        }
        return min(max(rows.lowerBound + Int(y / Self.rowHeight), rows.lowerBound), rows.upperBound - 1)
    }

    /// The number a row shows: the old line for a removed one, the new
    /// line otherwise.
    static func number(_ row: DiffDisplayRow) -> Int? {
        row.kind == "deletion" ? row.oldLine : row.newLine ?? row.oldLine
    }

    static func sign(_ row: DiffDisplayRow) -> String {
        switch row.kind {
        case "addition": "+"
        case "deletion": "-"
        default: ""
        }
    }

    /// Wide enough for the longest number, with room before it.
    static func numberWidth(_ document: DiffDocument) -> CGFloat {
        let largest = document.rows.compactMap(number).max() ?? 0
        return CGFloat(max(String(largest).count, 2)) * 7.3 + 12
    }

    /// Tabs as four spaces, so lines keep their indentation.
    static func display(_ text: String) -> String {
        text.replacingOccurrences(of: "\t", with: "    ")
    }

    static func wash(_ row: DiffDisplayRow) -> Color {
        switch row.kind {
        case "addition": Theme.diffAdded
        case "deletion": Theme.diffRemoved
        case "hunk": Theme.raised
        default: .clear
        }
    }

    static func textColor(_ row: DiffDisplayRow) -> Color {
        row.kind == "hunk" || row.kind == "marker" ? Theme.muted : Theme.text
    }

    /// The rows read as text, one line each, for VoiceOver.
    static func accessibilityText(_ document: DiffDocument, rows: Range<Int>? = nil) -> String {
        document.rows[rows ?? document.rows.indices].map { row in
            switch row.kind {
            case "addition": "Added: \(row.text)"
            case "deletion": "Removed: \(row.text)"
            case "hunk": "Gap"
            default: row.text
            }
        }.joined(separator: "\n")
    }
}

/// The dot under a selection that drags it longer.
private struct SelectionHandle: View {
    static let size: CGFloat = 14

    var body: some View {
        Circle()
            .fill(Theme.accent)
            .overlay { Circle().strokeBorder(Theme.surface, lineWidth: 2) }
            .frame(width: Self.size, height: Self.size)
            .frame(width: 44, height: 44)
            .contentShape(Rectangle())
            .frame(width: Self.size, height: Self.size)
            .accessibilityLabel("Selection handle")
            .accessibilityIdentifier("diff-selection-handle")
    }
}
