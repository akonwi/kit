import Foundation

/// The server-applied configuration of one durable child conversation.
public struct SubagentConfigurationResult: Equatable, Sendable {
    public let model: String
    public let thinkingLevel: String
    public let warnings: [String]

    public init(model: String, thinkingLevel: String, warnings: [String]) {
        self.model = model
        self.thinkingLevel = thinkingLevel
        self.warnings = warnings
    }
}

/// The server rejected a child configuration with a declared, renderer-safe reason.
public struct SubagentConfigurationRejected: LocalizedError, Equatable {
    public let message: String
    public var errorDescription: String? { message }

    public init(message: String) {
        self.message = message
    }
}

/// Changes one child conversation's model and/or thinking level. A nil field
/// preserves the server's authoritative value.
public protocol SubagentConfigurationClient: SubagentClient {
    func configureSubagent(_ session: String, conversation: String, generation: UInt64,
                           model: String?, thinking: String?) async throws -> SubagentConfigurationResult
}

extension ServerClient: SubagentConfigurationClient {
    public func configureSubagent(_ session: String, conversation: String, generation: UInt64,
                           model: String?, thinking: String?) async throws -> SubagentConfigurationResult {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.configureSubagent(session, conversation: conversation, generation: generation,
            model: model, thinking: thinking)
    }
}
