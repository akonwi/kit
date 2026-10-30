import Foundation

public struct FollowUpState: Decodable, Sendable, Equatable {
    public let count: Int
    public let previews: [String]
    /// Why the first queued message could not start. The server holds the
    /// queue, and refuses new prompts, until a client restores it.
    public let admissionFailure: String?
    public init(_ wire: WireFollowUpQueue) throws {
        guard wire.count >= 0, wire.count <= 64, (wire.previews?.count ?? 0) <= wire.count,
              (wire.previews ?? []).allSatisfy({ !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && $0.utf8.count <= 1024 }) else { throw ClientError.invalidPayload }
        if let failure = wire.admissionFailure?.message {
            guard wire.count > 0, !failure.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  failure.utf8.count <= 1024 else { throw ClientError.invalidPayload }
        }
        count = wire.count; previews = wire.previews ?? []; admissionFailure = wire.admissionFailure?.message
    }
}

public protocol SessionMutationClient: SessionClient {
    func submit(_ session: String, input: WirePromptInput) async throws -> WirePromptSubmission
    func restoreFollowUps(_ session: String) async throws -> WireRestoreFollowUpsResult
    func promoteFollowUps(_ session: String) async throws -> WirePromoteFollowUpsResult
    func abort(_ session: String, turn: String) async throws
}

/// Discovery failures occur before a mutation was sent; transport failures after that are ambiguous.
public struct MutationNotSent: LocalizedError {
    public let reason: String
    public var errorDescription: String? { reason }

    public init(reason: String) {
        self.reason = reason
    }
}

extension ServerClient: SessionMutationClient {
    private func mutationTransport() async throws -> HTTPClient {
        do { return try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
    }
    public func submit(_ session: String, input: WirePromptInput) async throws -> WirePromptSubmission {
        try await mutationTransport().submit(session, input: input)
    }
    public func restoreFollowUps(_ session: String) async throws -> WireRestoreFollowUpsResult {
        try await mutationTransport().restoreFollowUps(session)
    }
    public func promoteFollowUps(_ session: String) async throws -> WirePromoteFollowUpsResult {
        try await mutationTransport().promoteFollowUps(session)
    }
    public func abort(_ session: String, turn: String) async throws {
        try await mutationTransport().abort(session, turn: turn)
    }
}
