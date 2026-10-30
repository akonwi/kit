import Foundation

public protocol PromptCommandClient: SessionClient {
    func submitPromptCommand(_ id: String, input: WirePromptCommandInput) async throws -> WirePromptSubmission
}

extension ServerClient: PromptCommandClient {
    public func submitPromptCommand(_ id: String, input: WirePromptCommandInput) async throws -> WirePromptSubmission {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.submitPromptCommand(id, input: input)
    }
}

public struct PromptCommand: Decodable, Sendable, Equatable, Identifiable {
    public let name: String
    public let description: String
    public let source: String
    public let location: String
    public var argumentHint: String? = nil
    public var id: String { name }
    /// The presented source: Claude Code project commands show as "claude".
    public var sourceLabel: String { source == "claude_project" ? "claude" : source }

    public static func validName(_ name: String) -> Bool {
        !name.isEmpty && name.utf8.count <= 128 && !name.unicodeScalars.contains {
            CharacterSet.whitespacesAndNewlines.contains($0) || CharacterSet.controlCharacters.contains($0) ||
            $0.properties.generalCategory == .format || $0 == "/" || $0 == "\\"
        }
    }

    public static func project(_ commands: [WirePromptCommand]) throws -> [Self] {
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

    public static func invocation(_ text: String) -> (name: String, args: String)? {
        guard text.hasPrefix("/") else { return nil }
        let body = text.dropFirst()
        let end = body.firstIndex(where: { $0.isWhitespace }) ?? body.endIndex
        let name = String(body[..<end])
        guard validName(name) else { return nil }
        return (name, String(body[end...].drop(while: { $0.isWhitespace })))
    }

    public init(name: String, description: String, source: String, location: String, argumentHint: String? = nil) {
        self.name = name
        self.description = description
        self.source = source
        self.location = location
        self.argumentHint = argumentHint
    }
}
