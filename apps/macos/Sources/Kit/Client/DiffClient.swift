import Foundation

protocol DiffClient: SessionClient {
    func diffTargets(_ session: String) async throws -> WireDiffTargetCatalog
    func observeDiff(_ session: String, input: WireObserveDiffInput) async throws -> WireWorkingTreePage
    func readDiff(_ session: String, input: WireReadFileDiffInput) async throws -> WireFileDiffPage
}

extension ServerClient: DiffClient {
    func diffTargets(_ session: String) async throws -> WireDiffTargetCatalog {
        try await resolve().diffTargets(session)
    }
    func observeDiff(_ session: String, input: WireObserveDiffInput) async throws -> WireWorkingTreePage {
        try await resolve().observeDiff(session, input: input)
    }
    func readDiff(_ session: String, input: WireReadFileDiffInput) async throws -> WireFileDiffPage {
        try await resolve().readDiff(session, input: input)
    }
}

/// A diff request the server refused, named in the TUI's words.
struct DiffReadError: LocalizedError {
    let code: String
    var errorDescription: String? {
        switch code {
        case "not_repository": "Not a git repository"
        case "unsupported_repository": "This repository is not supported"
        case "repository_unavailable": "The repository is unavailable"
        case "stale_workspace", "stale_target", "stale_file", "stale_cursor": "The diff changed"
        case "permission_denied": "Permission was denied"
        case "limit_exceeded": "The diff is too large"
        case "capacity_exceeded": "The workspace is busy"
        case "not_found": "This diff is no longer available"
        default: "The diff is unavailable"
        }
    }
    /// The `error.code` a generated diff response carries, found by
    /// reflection so every status's typed payload maps the same way.
    static func code(in output: Any) -> String {
        func search(_ value: Any, depth: Int) -> String? {
            guard depth < 8 else { return nil }
            for child in Mirror(reflecting: value).children {
                if child.label == "code" {
                    if let text = child.value as? String { return text }
                    if let raw = child.value as? any RawRepresentable, let text = raw.rawValue as? String { return text }
                }
                if let found = search(child.value, depth: depth + 1) { return found }
            }
            return nil
        }
        return search(output, depth: 0) ?? "unavailable"
    }
    /// A line under the title when the reason needs one.
    var detail: String? {
        switch code {
        case "not_repository": "Kit can only show diffs inside a git repository."
        case "stale_workspace", "stale_target", "stale_file", "stale_cursor": "The current version loads on the next refresh."
        default: nil
        }
    }
}
