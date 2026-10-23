import Foundation

/// A server that reports which of its sessions are working or awaiting a
/// response, as of the request.
protocol SessionActivityClient: SessionClient {
    /// The status of each session that isn't idle, by session ID. A session
    /// it omits is idle.
    func sessionActivity() async throws -> [String: SessionTabStatus]
}

extension ServerClient: SessionActivityClient {
    func sessionActivity() async throws -> [String: SessionTabStatus] {
        try await resolve().sessionActivity()
    }
}
