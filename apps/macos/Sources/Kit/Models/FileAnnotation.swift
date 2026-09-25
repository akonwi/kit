import Foundation

/// Session-scoped evidence. Summary text must never be used to replace a full body.
struct FileAnnotation: Codable, Identifiable, Sendable, Equatable {
    let id: UInt64
    let workspaceID: String
    let path: String
    let revision: String
    let startLine: Int
    let endLine: Int
    let body: String
    let source: String
    let stale: Bool
    let complete: Bool
    let truncated: Bool
    let diffAnchor: WireWorkingTreeDiffAnnotationAnchor?
    let diffTarget: WirePinnedDiffTarget?
    let validationDeferred: Bool
    var isDiff: Bool { diffAnchor != nil }
    var statusLabel: String? {
        stale ? (isDiff ? "Diff changed" : "File changed") : validationDeferred ? "Evidence validation pending" : nil
    }
    var diffContext: String? {
        guard let diffAnchor else { return nil }
        return "\(diffAnchor.side) side · \(diffAnchor.targetId) · \(diffAnchor.targetRevision)"
    }

    var label: String {
        (path as NSString).lastPathComponent + ":" + rangeLabel
    }
    var rangeLabel: String { startLine == endLine ? "\(startLine)" : "\(startLine)–\(endLine)" }
    var anchor: WireAnnotationAnchor {
        if let diffAnchor { return .init(kind: .value1, workspaceFile: nil, workingTreeDiff: diffAnchor) }
        return .init(kind: .value0, workspaceFile: .init(workspaceId: workspaceID, path: path,
            fileRevision: revision, startLine: startLine, endLine: endLine), workingTreeDiff: nil)
    }

    init(id: UInt64, anchor: WireAnnotationAnchor, body: String, source: String,
         stale: Bool, complete: Bool, truncated: Bool = false,
         diffTarget: WirePinnedDiffTarget? = nil, validationDeferred: Bool = false) throws {
        let path: String, revision: String, workspaceID: String, start: Int, end: Int
        switch anchor.kind {
        case .value0:
            guard let file = anchor.workspaceFile, anchor.workingTreeDiff == nil, diffTarget == nil,
                  file.workspaceId.hasPrefix("workspace_"), file.fileRevision.hasPrefix("file_") else { throw ClientError.invalidPayload }
            path = file.path; revision = file.fileRevision; workspaceID = file.workspaceId
            start = file.startLine; end = file.endLine
        case .value1:
            guard let diff = anchor.workingTreeDiff, anchor.workspaceFile == nil,
                  diff.targetId.hasPrefix("difftarget_"), diff.targetRevision.hasPrefix("diffrev_"),
                  diff.fileRevision.hasPrefix("diff_file_"), ["old", "new"].contains(diff.side) else { throw ClientError.invalidPayload }
            if let diffTarget {
                func validEndpoint(_ endpoint: WireDiffEndpoint) -> Bool {
                    if endpoint.kind == "empty_tree" { return endpoint.oid == nil || endpoint.oid == "" }
                    guard endpoint.kind == "commit", let oid = endpoint.oid, [40, 64].contains(oid.count) else { return false }
                    return oid.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) }
                }
                guard diffTarget.workspaceId.hasPrefix("workspace_"), ["commit", "branch"].contains(diffTarget.kind),
                      validEndpoint(diffTarget.base), validEndpoint(diffTarget.head), diffTarget.head.kind == "commit",
                      diffTarget.kind != "branch" || diffTarget.base.kind == "commit" else { throw ClientError.invalidPayload }
            }
            path = diff.path; revision = diff.fileRevision; workspaceID = diffTarget?.workspaceId ?? ""
            start = diff.startLine; end = diff.endLine
        }
        guard id > 0, !path.isEmpty, !path.hasPrefix("/"), !path.split(separator: "/").contains(".."),
              start > 0, end >= start, end - start < 200, !(stale && validationDeferred),
              Self.validBody(body), source.utf8.count <= 16 * 1024 else { throw ClientError.invalidPayload }
        self.id = id; self.workspaceID = workspaceID; self.path = path; self.revision = revision
        startLine = start; endLine = end; self.body = body; self.source = source
        self.stale = stale; self.complete = complete; self.truncated = truncated
        diffAnchor = anchor.workingTreeDiff; self.diffTarget = diffTarget; self.validationDeferred = validationDeferred
    }

    init(_ value: WireAnnotation, session: String) throws {
        guard value.sessionId == session,
              (value.stale == true) == (value.staleReason != nil),
              value.staleReason?.rawValue != "validation_deferred" else { throw ClientError.invalidPayload }
        try self.init(id: value.id, anchor: value.anchor, body: value.body, source: value.preview.text,
            stale: value.stale == true, complete: true, truncated: value.preview.truncated == true,
            diffTarget: value.diffTarget, validationDeferred: value.validationDeferred == true)
        guard value.preview.startLine == startLine, value.preview.endLine == endLine else { throw ClientError.invalidPayload }
    }

    init(_ value: WireAnnotationSummary) throws {
        guard value.bodyPreview.utf8.count <= 256, value.preview.utf8.count <= 256,
              (value.stale == true) == (value.staleReason != nil),
              value.staleReason?.rawValue != "validation_deferred" else { throw ClientError.invalidPayload }
        try self.init(id: value.id, anchor: value.anchor, body: value.bodyPreview, source: value.preview,
            stale: value.stale == true, complete: false, diffTarget: value.diffTarget,
            validationDeferred: value.validationDeferred == true)
    }

    init(_ value: WireSubmittedAnnotation) throws {
        try self.init(id: value.originalAnnotationId, anchor: value.anchor, body: value.body,
            source: value.preview.text, stale: false, complete: true, truncated: value.preview.truncated == true,
            diffTarget: value.diffTarget)
        guard value.preview.startLine == startLine, value.preview.endLine == endLine else { throw ClientError.invalidPayload }
    }

    static func validBody(_ value: String) -> Bool {
        !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && value.utf8.count <= 16 * 1024 &&
        !value.unicodeScalars.contains { scalar in
            scalar != "\n" && scalar != "\t" &&
            (scalar.properties.generalCategory == .control || scalar.properties.generalCategory == .format)
        }
    }
}

/// Native selections use UTF-16; protocol ranges are inclusive, one-based LF lines.
enum AnnotationSelection {
    static func lines(in source: String, selection: NSRange) -> ClosedRange<Int>? {
        let units = Array(source.utf16)
        guard selection.location != NSNotFound, selection.location >= 0, selection.length >= 0,
              selection.location <= units.count, selection.length <= units.count - selection.location,
              !units.isEmpty else { return nil }
        let first = units.prefix(selection.location).filter { $0 == 10 }.count + 1
        let lastOffset = selection.length == 0 ? selection.location : selection.location + selection.length - 1
        let last = units.prefix(lastOffset).filter { $0 == 10 }.count + 1
        // The server does not count the empty line after a final newline.
        let count = units.filter { $0 == 10 }.count + (units.last == 10 ? 0 : 1)
        guard first <= count, last <= count, last - first < 200 else { return nil }
        return first...last
    }
}
