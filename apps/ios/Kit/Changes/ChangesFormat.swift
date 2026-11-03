import Foundation
import KitClient

/// How Changes and the session menu describe a diff in words.
enum ChangesFormat {
    /// The line under Changes in the session menu: the counts, or why
    /// there are none.
    @MainActor static func summary(_ diff: DiffState) -> String {
        if let error = diff.error { return error }
        if diff.catalog == nil || (diff.loading && diff.files.isEmpty) { return "Loading…" }
        if diff.files.isEmpty { return empty(diff) }
        return [files(diff.files.count, more: diff.fileCursor != nil), lines(diff.additions, diff.deletions)]
            .compactMap { $0 }.joined(separator: "  ")
    }

    /// What an empty diff says.
    @MainActor static func empty(_ diff: DiffState) -> String {
        if diff.target == nil, let notice = diff.notice { return notice }
        return diff.target.map { $0.kind == "working_tree" } ?? true ? "Working tree is clean" : "No changes"
    }

    /// `1 file`, `4 files`, or `100+ files` while more are to come.
    static func files(_ count: Int, more: Bool) -> String {
        "\(count)\(more ? "+" : "") \(count == 1 && !more ? "file" : "files")"
    }

    /// `+48 −12`, either part left out when the server sent no count.
    static func lines(_ additions: Int?, _ deletions: Int?) -> String? {
        let parts = [additions.map { "+\($0)" }, deletions.map { "−\($0)" }].compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " ")
    }

    /// The target pill's short name.
    static func pill(_ target: WireDiffTargetEntry?) -> String {
        guard let target else { return "Working tree" }
        switch target.kind {
        case "working_tree": return "Uncommitted"
        case "branch": return "This branch"
        case "commit": return target.metadata.abbreviatedOid.flatMap { $0.isEmpty ? nil : $0 } ?? "Commit"
        default: return target.metadata.label
        }
    }

    /// A target's name in the Compare sheet.
    static func title(_ target: WireDiffTargetEntry) -> String {
        switch target.kind {
        case "working_tree": return "Uncommitted changes"
        case "branch": return "This branch"
        case "commit": return target.metadata.subject.flatMap { $0.isEmpty ? nil : $0 } ?? target.metadata.label
        default: return target.metadata.label
        }
    }

    /// The line under a target's name in the Compare sheet.
    static func detail(_ target: WireDiffTargetEntry) -> String? {
        switch target.kind {
        case "branch": return target.metadata.label
        case "commit":
            let age = target.metadata.committedAt.map {
                Date(timeIntervalSince1970: TimeInterval($0)).formatted(.relative(presentation: .named, unitsStyle: .abbreviated))
            }
            return [target.metadata.abbreviatedOid, age].compactMap { $0 }.joined(separator: " · ")
        default: return nil
        }
    }

    /// A path's folder, with its slash, and its name: `Client/` and
    /// `HTTPClient.swift`.
    static func split(_ path: String) -> (folder: String, name: String) {
        guard let slash = path.lastIndex(of: "/") else { return ("", path) }
        return (String(path[...slash]), String(path[path.index(after: slash)...]))
    }

    /// Why a file shows no text.
    static func contentNote(_ file: WireDiffFileSummary) -> String {
        switch file.contentState {
        case "binary": "Binary file changed"
        case "conflict": "File has unresolved conflicts"
        case "intent_to_add": "Intent-to-add file has no textual diff"
        case "unsupported_transform": "File uses an unsupported content transform"
        case "unsupported_kind": "File kind is not available for textual diff"
        case "too_large": "File diff exceeds the configured limit"
        case "unavailable": "File diff is unavailable"
        default: "No textual changes"
        }
    }
}
