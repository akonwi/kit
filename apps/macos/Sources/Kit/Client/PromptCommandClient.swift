import Foundation

protocol PromptCommandClient: SessionClient {
    func runPromptCommand(_ id: String, input: WirePromptCommandInput) async throws -> WireRunReservation
}

extension LocalClient: PromptCommandClient {
    func runPromptCommand(_ id: String, input: WirePromptCommandInput) async throws -> WireRunReservation {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.runPromptCommand(id, input: input)
    }
}

struct PromptCommand: Decodable, Sendable, Equatable, Identifiable {
    let name: String
    let description: String
    let source: String
    let location: String
    var argumentHint: String? = nil
    var id: String { name }

    static func validName(_ name: String) -> Bool {
        !name.isEmpty && name.utf8.count <= 128 && !name.unicodeScalars.contains {
            CharacterSet.whitespacesAndNewlines.contains($0) || CharacterSet.controlCharacters.contains($0) ||
            $0.properties.generalCategory == .format || $0 == "/" || $0 == "\\"
        }
    }

    static func project(_ commands: [WirePromptCommand]) throws -> [Self] {
        guard commands.count <= 128 else { throw ClientError.invalidPayload }
        var seen = Set<String>()
        return try commands.map {
            guard validName($0.name), seen.insert($0.name).inserted,
                  $0.description.utf8.count <= 1024, $0.location.utf8.count <= 4096,
                  PluginCommand.safeText($0.argumentHint ?? "", limit: 1024) else { throw ClientError.invalidPayload }
            return Self(name: $0.name, description: $0.description, source: $0.source, location: $0.location,
                        argumentHint: $0.argumentHint)
        }
    }

    static func invocation(_ text: String) -> (name: String, args: String)? {
        guard text.hasPrefix("/") else { return nil }
        let body = text.dropFirst()
        let end = body.firstIndex(where: { $0.isWhitespace }) ?? body.endIndex
        let name = String(body[..<end])
        guard validName(name) else { return nil }
        return (name, String(body[end...].drop(while: { $0.isWhitespace })))
    }
}
