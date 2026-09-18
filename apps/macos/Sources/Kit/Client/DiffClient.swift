import Foundation

protocol DiffClient: SessionClient {
    func diffTargets(_ session: String) async throws -> WireDiffTargetCatalog
    func observeDiff(_ session: String, input: WireObserveDiffInput) async throws -> WireWorkingTreePage
    func readDiff(_ session: String, input: WireReadFileDiffInput) async throws -> WireFileDiffPage
}

extension LocalClient: DiffClient {
    func diffTargets(_ session: String) async throws -> WireDiffTargetCatalog {
        try await HTTPClient.local().diffTargets(session)
    }
    func observeDiff(_ session: String, input: WireObserveDiffInput) async throws -> WireWorkingTreePage {
        try await HTTPClient.local().observeDiff(session, input: input)
    }
    func readDiff(_ session: String, input: WireReadFileDiffInput) async throws -> WireFileDiffPage {
        try await HTTPClient.local().readDiff(session, input: input)
    }
}

struct DiffReadError: LocalizedError {
    let code: String
    var errorDescription: String? {
        switch code {
        case "not_repository": "This workspace is not a Git repository."
        case "unsupported_repository": "This repository configuration is not supported by the diff server."
        case "stale_workspace", "stale_target", "stale_file", "stale_cursor": "The diff changed. Its current revision will load on the next update."
        case "permission_denied": "The server cannot read this diff."
        case "limit_exceeded": "This diff exceeds the server’s bounded read limits."
        case "capacity_exceeded": "The diff server is busy. Try again shortly."
        case "not_found": "This diff is no longer available."
        default: "The diff is unavailable."
        }
    }
}

struct DiffErrorEnvelope: Decodable { let error: WireDiffError }
