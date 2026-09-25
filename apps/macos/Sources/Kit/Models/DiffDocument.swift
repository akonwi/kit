import Foundation

struct DiffDisplayRow: Sendable {
    let text: String
    let kind: String
    let oldLine: Int?
    let newLine: Int?
    func number(_ side: String) -> Int? { side == "old" ? oldLine : newLine }
}

struct DiffDocument: Sendable {
    let observation: WireDiffObservation
    let file: WireDiffFileSummary
    let rows: [DiffDisplayRow]
    let side: String?
    private let oldRows: [Int: Int]
    private let newRows: [Int: Int]
    var identity: String { observation.target.id + observation.revision + file.path + (file.fileRevision ?? "") + String(rows.count) + (side ?? "unified") }
    var content: String { rows.map(\.text).joined(separator: "\n") + "\n" }

    init(observation: WireDiffObservation, file: WireDiffFileSummary, hunks: [WireDiffHunk]) {
        self.observation = observation; self.file = file; side = nil
        var displayed: [DiffDisplayRow] = []
        for hunk in hunks {
            if !hunk.continuedBefore || displayed.isEmpty {
                displayed.append(DiffDisplayRow(text: "@@ −\(hunk.oldStart),\(hunk.oldCount) +\(hunk.newStart),\(hunk.newCount) @@" + (hunk.continuedBefore ? " (continued)" : ""),
                    kind: "hunk", oldLine: nil, newLine: nil))
            }
            for line in hunk.lines ?? [] {
                displayed.append(DiffDisplayRow(text: line.content, kind: line.kind, oldLine: line.oldLine, newLine: line.newLine))
                if !line.hasTerminatingLF {
                    displayed.append(DiffDisplayRow(text: "\\ No newline at end of file", kind: "marker", oldLine: nil, newLine: nil))
                }
            }
        }
        rows = displayed
        oldRows = Dictionary(displayed.enumerated().compactMap { index, row in row.oldLine.map { ($0, index) } }, uniquingKeysWith: min)
        newRows = Dictionary(displayed.enumerated().compactMap { index, row in row.newLine.map { ($0, index) } }, uniquingKeysWith: min)
    }

    private init(source: DiffDocument, rows: [DiffDisplayRow], side: String) {
        observation = source.observation; file = source.file; self.rows = rows; self.side = side
        oldRows = Dictionary(rows.enumerated().compactMap { index, row in row.oldLine.map { ($0, index) } }, uniquingKeysWith: min)
        newRows = Dictionary(rows.enumerated().compactMap { index, row in row.newLine.map { ($0, index) } }, uniquingKeysWith: min)
    }

    /// Pair replacement runs, padding the shorter side without inventing source coordinates.
    func split() -> (old: DiffDocument, new: DiffDocument) {
        var old: [DiffDisplayRow] = [], new: [DiffDisplayRow] = []
        var removed: [DiffDisplayRow] = [], added: [DiffDisplayRow] = []
        let blank = DiffDisplayRow(text: "", kind: "placeholder", oldLine: nil, newLine: nil)
        func project(_ row: DiffDisplayRow, old: Bool) -> DiffDisplayRow {
            DiffDisplayRow(text: row.text, kind: row.kind, oldLine: old ? row.oldLine : nil, newLine: old ? nil : row.newLine)
        }
        func flush() {
            for index in 0..<max(removed.count, added.count) {
                old.append(index < removed.count ? project(removed[index], old: true) : blank)
                new.append(index < added.count ? project(added[index], old: false) : blank)
            }
            removed = []; added = []
        }
        var precedingKind = ""
        for row in rows {
            if row.kind == "deletion" { removed.append(row) }
            else if row.kind == "addition" { added.append(row) }
            else if row.kind == "marker", precedingKind == "deletion" { removed.append(row) }
            else if row.kind == "marker", precedingKind == "addition" { added.append(row) }
            else {
                flush()
                old.append(project(row, old: true)); new.append(project(row, old: false))
            }
            if row.kind != "marker" { precedingKind = row.kind }
        }
        flush()
        return (DiffDocument(source: self, rows: old, side: "old"), DiffDocument(source: self, rows: new, side: "new"))
    }

    func anchor(rows selection: ClosedRange<Int>, side: String) -> WireAnnotationAnchor? {
        guard self.side == nil || self.side == side else { return nil }
        guard rows.indices.contains(selection.lowerBound), rows.indices.contains(selection.upperBound),
              let revision = file.fileRevision else { return nil }
        let selected = rows[selection]
        guard !selected.contains(where: { $0.kind == "hunk" }),
              let first = selected.first?.number(side), let last = selected.last?.number(side),
              first <= last, last - first < 200 else { return nil }
        let numbers = selected.compactMap { $0.number(side) }
        guard numbers == Array(first...last) else { return nil }
        return .init(kind: .value1, workspaceFile: nil, workingTreeDiff: .init(targetId: observation.target.id,
            targetRevision: observation.revision, path: file.path, fileRevision: revision,
            side: side, startLine: first, endLine: last))
    }

    func range(_ anchor: WireAnnotationAnchor) -> ClosedRange<Int>? {
        guard let diff = anchor.workingTreeDiff, side == nil || side == diff.side, diff.targetId == observation.target.id,
              diff.targetRevision == observation.revision, diff.path == file.path, diff.fileRevision == file.fileRevision,
              let start = (diff.side == "old" ? oldRows : newRows)[diff.startLine],
              let end = (diff.side == "old" ? oldRows : newRows)[diff.endLine], start <= end else { return nil }
        return start...end
    }
}

struct AnnotationDocument {
    let content: String
    let path: String
    let revision: String
    let workspaceID: String
    let diff: DiffDocument?
    init(file: WireWorkspaceFileRead) {
        content = file.content; path = file.path; revision = file.revision
        workspaceID = file.workspace.workspaceId; diff = nil
    }
    init(diff: DiffDocument) {
        self.diff = diff; content = diff.content; path = diff.file.path
        revision = diff.file.fileRevision ?? ""; workspaceID = diff.observation.target.workspaceId
    }
    func range(_ anchor: WireAnnotationAnchor) -> ClosedRange<Int>? {
        if let diff { return diff.range(anchor) }
        guard let file = anchor.workspaceFile, file.workspaceId == workspaceID,
              file.path == path, file.fileRevision == revision else { return nil }
        return (file.startLine - 1)...(file.endLine - 1)
    }
    func anchor(_ lines: ClosedRange<Int>, side: String?) -> WireAnnotationAnchor? {
        if let diff { return diff.anchor(rows: lines, side: side ?? "new") }
        return .init(kind: .value0, workspaceFile: .init(workspaceId: workspaceID, path: path,
            fileRevision: revision, startLine: lines.lowerBound + 1, endLine: lines.upperBound + 1), workingTreeDiff: nil)
    }
}

enum DiffValidation {
    static func cursor(_ value: String?) throws {
        guard (value?.utf8.count ?? 0) <= 1024 else { throw ClientError.invalidPayload }
    }
    static func observation(_ value: WireDiffObservation, session: String, target: String, revision: String?) throws {
        guard value.sessionId == session, value.target.id == target, value.target.workspaceId.hasPrefix("workspace_"),
              value.revision.hasPrefix("diffrev_"), revision == nil || revision == value.revision,
              ["working_tree", "branch", "commit"].contains(value.target.kind) else { throw ClientError.invalidPayload }
    }
    static func file(_ value: WireDiffFileSummary) throws {
        guard !value.path.isEmpty, !value.path.hasPrefix("/"), !value.path.split(separator: "/").contains(".."),
              value.path.utf8.count <= 4096, value.fileRevision == nil || value.fileRevision!.hasPrefix("diff_file_"),
              ["added", "deleted", "modified", "mode_changed", "unknown"].contains(value.change),
              ["text", "binary", "conflict", "intent_to_add", "unsupported_transform", "unsupported_kind", "too_large", "unavailable"].contains(value.contentState),
              (value.additions ?? 0) >= 0, (value.deletions ?? 0) >= 0 else { throw ClientError.invalidPayload }
    }
    static func hunks(_ page: WireFileDiffPage) throws {
        guard let hunks = page.hunks, hunks.count <= 20, ["complete", "too_complex"].contains(page.computation.state),
              hunks.reduce(0, { $0 + ($1.lines?.count ?? 0) }) <= 1000 else { throw ClientError.invalidPayload }
        for hunk in hunks {
            guard hunk.oldStart >= 0, hunk.newStart >= 0, hunk.oldCount >= 0, hunk.newCount >= 0 else { throw ClientError.invalidPayload }
            for line in hunk.lines ?? [] {
                guard line.content.utf8.count <= 64 * 1024, !line.content.contains("\n"),
                      (line.oldLine ?? 1) > 0, (line.newLine ?? 1) > 0 else { throw ClientError.invalidPayload }
                switch line.kind {
                case "context": guard line.oldLine != nil, line.newLine != nil else { throw ClientError.invalidPayload }
                case "addition": guard line.oldLine == nil, line.newLine != nil else { throw ClientError.invalidPayload }
                case "deletion": guard line.oldLine != nil, line.newLine == nil else { throw ClientError.invalidPayload }
                default: throw ClientError.invalidPayload
                }
            }
        }
    }
}
