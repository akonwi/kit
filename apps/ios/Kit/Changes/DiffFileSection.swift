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
    let client: ServerClient
    let session: String

    private var reader: DiffState? { diff.fileStates[file.path] }
    private var textual: Bool { file.contentState == "text" }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !textual {
                DiffNote(text: ChangesFormat.contentNote(file))
            } else if let reader, let document = reader.document, !document.rows.isEmpty {
                DiffLines(document: document, wrap: diff.wrapLines)
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

/// A file's unified diff rows: a line number and sign, then the line on a
/// green or red wash. Long lines scroll sideways unless they wrap.
struct DiffLines: View {
    let document: DiffDocument
    let wrap: Bool
    @State private var codeWidth: CGFloat = 0

    static let rowHeight: CGFloat = 20
    static let font = Font.system(size: 12, design: .monospaced)

    var body: some View {
        let numberWidth = Self.numberWidth(document)
        Group {
            if wrap {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(document.rows.enumerated()), id: \.offset) { _, row in
                        HStack(alignment: .firstTextBaseline, spacing: 0) {
                            gutter(row, numberWidth: numberWidth)
                            Text(Self.display(row.text))
                                .foregroundStyle(Self.textColor(row))
                                .italic(row.kind == "marker")
                                .padding(.vertical, 2)
                                .padding(.trailing, 12)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .frame(minHeight: Self.rowHeight)
                        .background(Self.wash(row))
                    }
                }
            } else {
                HStack(alignment: .top, spacing: 0) {
                    VStack(spacing: 0) {
                        ForEach(Array(document.rows.enumerated()), id: \.offset) { _, row in
                            gutter(row, numberWidth: numberWidth)
                                .frame(height: Self.rowHeight)
                                .background(Self.wash(row))
                        }
                    }
                    ScrollView(.horizontal) {
                        VStack(alignment: .leading, spacing: 0) {
                            ForEach(Array(document.rows.enumerated()), id: \.offset) { _, row in
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
        .accessibilityLabel(Self.accessibilityText(document))
        .accessibilityIdentifier("diff-lines")
    }

    private func gutter(_ row: DiffDisplayRow, numberWidth: CGFloat) -> some View {
        HStack(spacing: 0) {
            Text(Self.number(row).map(String.init) ?? "")
                .foregroundStyle(Theme.muted)
                .frame(width: numberWidth, alignment: .trailing)
            Text(Self.sign(row))
                .foregroundStyle(row.kind == "addition" ? Theme.success : row.kind == "deletion" ? Theme.danger : Theme.muted)
                .frame(width: 22, alignment: .center)
        }
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
    static func accessibilityText(_ document: DiffDocument) -> String {
        document.rows.map { row in
            switch row.kind {
            case "addition": "Added: \(row.text)"
            case "deletion": "Removed: \(row.text)"
            case "hunk": "Gap"
            default: row.text
            }
        }.joined(separator: "\n")
    }
}
