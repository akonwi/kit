import Foundation

protocol WorkspaceFileClient: SessionClient {
    func readWorkspaceFile(_ session: String, path: String, expectedRevision: String?) async throws -> WireWorkspaceFileRead
}

extension WorkspaceFileClient {
    func readWorkspaceFile(_ session: String, path: String) async throws -> WireWorkspaceFileRead {
        try await readWorkspaceFile(session, path: path, expectedRevision: nil)
    }
}

/// One active read process-wide; bounded waiters keep multiple windows from
/// exceeding even the smallest advertised server concurrency limit.
actor WorkspaceReadGate {
    static let shared = WorkspaceReadGate()
    private var active = false
    private var waiting = 0
    func read<T: Sendable>(_ operation: @Sendable () async throws -> T) async throws -> T {
        guard waiting < 16 else { throw WorkspaceFileError.response(429) }
        waiting += 1
        do {
            while active { try await Task.sleep(for: .milliseconds(25)) }
            try Task.checkCancellation()
        } catch { waiting -= 1; throw error }
        waiting -= 1
        active = true
        defer { active = false }
        return try await operation()
    }
}

extension LocalClient: WorkspaceFileClient {
    func readWorkspaceFile(_ session: String, path: String, expectedRevision: String?) async throws -> WireWorkspaceFileRead {
        try await HTTPClient.local().readWorkspaceFile(session, path: path, expectedRevision: expectedRevision)
    }
}

enum WorkspaceFileError: LocalizedError {
    case unavailable, response(Int)
    var errorDescription: String? {
        switch self {
        case .unavailable: "Workspace unavailable."
        case .response(404): "This file no longer exists."
        case .response(403): "This file cannot be read from this workspace."
        case .response(415): "Binary files cannot be previewed."
        case .response(409): "The workspace or file changed. Refresh to read the latest version."
        case .response(413): "This file exceeds the server’s preview limits."
        case .response(429): "The workspace is busy. Try again shortly."
        case .response: "Couldn’t read this file."
        }
    }
}
