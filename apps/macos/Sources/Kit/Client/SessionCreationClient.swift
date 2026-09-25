import Foundation

protocol SessionCreationClient: SessionClient {
    func models() async throws -> [WireModelCapability]
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt
}

extension LocalClient: SessionCreationClient {
    func models() async throws -> [WireModelCapability] { try await HTTPClient.local().models() }
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.createSession(input)
    }
}
