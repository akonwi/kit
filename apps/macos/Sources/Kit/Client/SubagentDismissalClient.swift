import Foundation

protocol SubagentDismissalClient: SubagentClient {
    func dismissSubagent(_ session: String, conversation: String, generation: UInt64) async throws
}

extension ServerClient: SubagentDismissalClient {
    func dismissSubagent(_ session: String, conversation: String, generation: UInt64) async throws {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        try await transport.dismissSubagent(session, conversation: conversation, generation: generation)
    }
}
