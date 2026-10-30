import Foundation
import Observation

protocol SessionReloadClient: SessionClient {
    func reloadSession(_ id: String) async throws -> WireReloadSessionResult
}

extension ServerClient: SessionReloadClient {
    func reloadSession(_ id: String) async throws -> WireReloadSessionResult {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.reloadSession(id)
    }
}

/// Reload is explicit; recovery refreshes never silently repeat the mutation.
@MainActor @Observable
final class SessionReloadOperation {
    private(set) var pending = false
    private(set) var result: WireReloadSessionResult?
    private(set) var error: String?
    private(set) var refreshError: String?
    var progressLabel: String {
        result != nil || error != nil ? "Refreshing session…" : "Reloading session context…"
    }

    func perform(session: String, client: any SessionReloadClient,
                 refresh: @MainActor () async throws -> Void) async {
        guard !pending else { return }
        pending = true
        result = nil; error = nil; refreshError = nil
        defer { pending = false }
        do { result = try await client.reloadSession(session) }
        catch { self.error = error.localizedDescription }
        await refreshState(refresh)
    }

    func refreshOnly(_ refresh: @MainActor () async throws -> Void) async {
        guard !pending else { return }
        pending = true
        defer { pending = false }
        await refreshState(refresh)
    }

    private func refreshState(_ refresh: @MainActor () async throws -> Void) async {
        refreshError = nil
        do { try await refresh() }
        catch is CancellationError { refreshError = "Session changed before its local refresh completed" }
        catch { refreshError = error.localizedDescription }
    }
}
