import Foundation

protocol SessionCreationClient: SessionClient {
    func models() async throws -> [WireModelCapability]
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt
}

extension ServerClient: SessionCreationClient {
    func models() async throws -> [WireModelCapability] { try await resolve().models() }
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.createSession(input)
    }
}
