import Foundation
import Observation

protocol SessionVCSClient: SessionClient {
    func vcs(_ session: String) async throws -> WireSessionVCSStatus
    /// Blocks on the server-pushed repository stream until cancellation or
    /// failure. Reconnection starts fresh; there is no cursor or replay.
    func watchVCS(_ session: String, receive: @escaping @Sendable (WireSessionVCSStatus) async -> Void) async throws
}

protocol SessionDirectoryClient: SessionVCSClient {
    func changeDirectory(_ session: String, input: WireChangeCWDInput) async throws -> SessionExcerpt
}

extension LocalClient: SessionDirectoryClient {
    func changeDirectory(_ session: String, input: WireChangeCWDInput) async throws -> SessionExcerpt {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.changeDirectory(session, input: input)
    }
    func vcs(_ session: String) async throws -> WireSessionVCSStatus {
        try await HTTPClient.local().vcs(session)
    }
    func watchVCS(_ session: String, receive: @escaping @Sendable (WireSessionVCSStatus) async -> Void) async throws {
        try await HTTPClient.local().watchVCS(session, receive: receive)
    }
}

/// Retain an unresolved relative-path mutation across dialog dismissal and retry.
@MainActor @Observable
final class DirectoryChangeOperation {
    private(set) var request: WireChangeCWDInput?
    private(set) var acknowledged = false
    private(set) var pending = false
    private(set) var error: String?

    static func path(_ text: String) -> String? {
        let path = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !path.isEmpty, path.utf8.count <= 4096,
              !path.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) }) else { return nil }
        return path
    }

    func perform(path: String, session: String, client: any SessionDirectoryClient,
                 refresh: @MainActor () async throws -> Void) async -> Bool {
        guard !pending, let path = Self.path(path) else { return false }
        if let request, request.path != path {
            error = "Resolve the previous directory change before choosing another path."
            return false
        }
        if request == nil {
            request = WireChangeCWDInput(mutationId: "cwd_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased(), path: path)
        }
        pending = true; error = nil
        defer { pending = false }
        do {
            if !acknowledged {
                _ = try await client.changeDirectory(session, input: request!)
                acknowledged = true
            }
            try await refresh()
            request = nil; acknowledged = false
            return true
        } catch {
            if !acknowledged {
                if error is MutationNotSent { request = nil }
                else if case ClientError.http(let code) = error, code < 500 { request = nil }
            }
            if case ClientError.http(409) = error {
                self.error = "The session has active work. Wait for it to finish before changing directories."
            } else {
                self.error = acknowledged ? "Directory changed, but workspace refresh failed. Retry to reload it. " + error.localizedDescription : error.localizedDescription
            }
            return false
        }
    }
}
