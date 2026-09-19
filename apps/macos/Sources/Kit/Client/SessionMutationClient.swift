import Foundation

struct FollowUpState: Decodable, Sendable {
    let count: Int
    let previews: [String]
    init(_ wire: WireFollowUpQueue) throws {
        guard wire.count >= 0, wire.count <= 64, (wire.previews?.count ?? 0) <= wire.count,
              (wire.previews ?? []).allSatisfy({ !$0.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && $0.utf8.count <= 1024 }) else { throw ClientError.invalidPayload }
        count = wire.count; previews = wire.previews ?? []
    }
}

protocol SessionMutationClient: SessionClient {
    func followUps(_ session: String) async throws -> FollowUpState
    func submit(_ session: String, input: WirePromptInput) async throws -> WirePromptSubmission
    func restoreFollowUps(_ session: String) async throws -> WireRestoreFollowUpsResult
    func promoteFollowUps(_ session: String) async throws -> WirePromoteFollowUpsResult
    func abort(_ session: String, run: String) async throws
}

/// Discovery failures occur before a mutation was sent; transport failures after that are ambiguous.
struct MutationNotSent: LocalizedError {
    let reason: String
    var errorDescription: String? { reason }
}

extension SessionMutationClient {
    func followUps(_ session: String) async throws -> FollowUpState {
        try await snapshot(session).followUps ?? FollowUpState(WireFollowUpQueue(count: 0, previews: [], annotationIds: nil))
    }
}

extension LocalClient: SessionMutationClient {
    func followUps(_ session: String) async throws -> FollowUpState { try await HTTPClient.local().followUps(session) }
    private func mutationTransport() async throws -> HTTPClient {
        do { return try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
    }
    func submit(_ session: String, input: WirePromptInput) async throws -> WirePromptSubmission {
        try await mutationTransport().submit(session, input: input)
    }
    func restoreFollowUps(_ session: String) async throws -> WireRestoreFollowUpsResult {
        try await mutationTransport().restoreFollowUps(session)
    }
    func promoteFollowUps(_ session: String) async throws -> WirePromoteFollowUpsResult {
        try await mutationTransport().promoteFollowUps(session)
    }
    func abort(_ session: String, run: String) async throws {
        try await mutationTransport().abort(session, run: run)
    }
}
