import Foundation

/// The server-applied configuration of one durable child conversation.
struct SubagentConfigurationResult: Equatable, Sendable {
    let model: String
    let thinkingLevel: String
    let warnings: [String]
}

/// The server rejected a child configuration with a declared, renderer-safe reason.
struct SubagentConfigurationRejected: LocalizedError, Equatable {
    let message: String
    var errorDescription: String? { message }
}

/// Changes one child conversation's model and/or thinking level. A nil field
/// preserves the server's authoritative value.
protocol SubagentConfigurationClient: SubagentClient {
    func configureSubagent(_ session: String, conversation: String, generation: UInt64,
                           model: String?, thinking: String?) async throws -> SubagentConfigurationResult
}

extension LocalClient: SubagentConfigurationClient {
    func configureSubagent(_ session: String, conversation: String, generation: UInt64,
                           model: String?, thinking: String?) async throws -> SubagentConfigurationResult {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.configureSubagent(session, conversation: conversation, generation: generation,
            model: model, thinking: thinking)
    }
}
