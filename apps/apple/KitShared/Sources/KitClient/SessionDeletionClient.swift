import Foundation

public protocol SessionDisposalClient: SessionClient {
    func disposeSession(_ id: String) async throws
}

extension ServerClient: SessionDisposalClient {
    public func disposeSession(_ id: String) async throws {
        try await resolve().disposeSession(id)
    }
}

public protocol SessionDeletionClient: SessionClient {
    func deleteSession(_ id: String) async throws
}

extension ServerClient: SessionDeletionClient {
    public func deleteSession(_ id: String) async throws {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        try await transport.deleteSession(id)
    }
}

extension Notification.Name {
    public static let kitSessionDeleted = Notification.Name("kitSessionDeleted")
}

/// A lost acknowledgement is resolved by reading authoritative state, never retrying DELETE.
public enum SessionDeletion {
    public static func perform(_ id: String, client: any SessionDeletionClient) async throws {
        do { try await client.deleteSession(id) }
        catch {
            let failure = error
            if case ClientError.http(404) = failure { return }
            do { _ = try await client.snapshot(id) }
            catch ClientError.http(404) { return }
            catch ClientError.missingSession { return }
            catch { throw failure }
            throw failure
        }
    }
}
