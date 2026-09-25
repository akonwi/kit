import Foundation

protocol SessionNamingClient: SessionClient {
    func renameSession(_ id: String, name: String) async throws -> SessionExcerpt
}

extension LocalClient: SessionNamingClient {
    func renameSession(_ id: String, name: String) async throws -> SessionExcerpt {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.renameSession(id, name: name)
    }
}

enum SessionName {
    static func normalized(_ value: String) -> String? {
        let name = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, name.utf8.count <= 256,
              !name.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) }) else { return nil }
        return name
    }
}
