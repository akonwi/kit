import Foundation
import Observation

public protocol SessionReloadClient: SessionClient {
    func reloadSession(_ id: String) async throws -> WireReloadSessionResult
}

extension ServerClient: SessionReloadClient {
    public func reloadSession(_ id: String) async throws -> WireReloadSessionResult {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.reloadSession(id)
    }
}

/// Reload is explicit; recovery refreshes never silently repeat the mutation.
@MainActor @Observable
public final class SessionReloadOperation {
    public init() {}

    public private(set) var pending = false
    public private(set) var result: WireReloadSessionResult?
    public private(set) var error: String?
    public private(set) var refreshError: String?
    public var progressLabel: String {
        result != nil || error != nil ? "Refreshing session…" : "Reloading session context…"
    }

    public func perform(session: String, client: any SessionReloadClient,
                 refresh: @MainActor () async throws -> Void) async {
        guard !pending else { return }
        pending = true
        result = nil; error = nil; refreshError = nil
        defer { pending = false }
        do { result = try await client.reloadSession(session) }
        catch { self.error = error.localizedDescription }
        await refreshState(refresh)
    }

    public func refreshOnly(_ refresh: @MainActor () async throws -> Void) async {
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
