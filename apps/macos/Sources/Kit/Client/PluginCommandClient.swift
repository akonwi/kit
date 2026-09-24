import Foundation

protocol PluginCommandClient: SessionClient {
    func executePluginCommand(_ session: String, input: WirePluginCommandInput) async throws
}

extension LocalClient: PluginCommandClient {
    func executePluginCommand(_ session: String, input: WirePluginCommandInput) async throws {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        try await transport.executePluginCommand(session, input: input)
    }
}

/// Executable contributions are not prompt templates. Retain the generation
/// with every selection; never retarget an invocation after a catalog refresh.
struct PluginCommand: Decodable, Sendable, Equatable, Identifiable {
    let id: String
    let instance: String
    let description: String
    let argName: String?
    var name: String { id }
    var selectionID: String { "plugin:" + id + ":" + instance }

    static func safeText(_ value: String, limit: Int) -> Bool {
        value.utf8.count <= limit && !value.unicodeScalars.contains {
            CharacterSet.controlCharacters.contains($0) || $0.properties.generalCategory == .format
        }
    }
    static func matches(_ value: String, _ pattern: String) -> Bool {
        guard let range = value.range(of: pattern, options: .regularExpression) else { return false }
        return range == value.startIndex..<value.endIndex
    }
    static func validSelection(id: String, instance: String) -> Bool {
        let parts = id.split(separator: ".", maxSplits: 1, omittingEmptySubsequences: false)
        return parts.count == 2 && matches(String(parts[0]), "^[a-z][a-z0-9-]{0,31}$") && parts[1].utf8.count <= 128
            && matches(String(parts[1]), "^[a-z][a-z0-9-]*(\\.[a-z][a-z0-9-]*)*$")
            && matches(instance, "^[a-zA-Z0-9_.:-]{1,128}$")
            && !id.contains("\n") && !instance.contains("\n")
    }
    static func project(_ values: [WirePluginCommand]) throws -> [Self] {
        guard values.count <= 256 else { throw ClientError.invalidPayload }
        var seen = Set<String>()
        return try values.map {
            guard validSelection(id: $0.id, instance: $0.instance),
                  matches($0.pluginId, "^[a-z][a-z0-9-]{0,31}$"), !$0.pluginId.contains("\n"),
                  $0.id == $0.pluginId + "." + $0.localId,
                  seen.insert($0.id).inserted, safeText($0.description, limit: 1024),
                  !$0.description.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  safeText($0.argName ?? "", limit: 128), safeText($0.category ?? "", limit: 128)
            else { throw ClientError.invalidPayload }
            return Self(id: $0.id, instance: $0.instance, description: $0.description, argName: $0.argName)
        }
    }
}

struct PluginCommandFailure: Error, LocalizedError, Sendable {
    let unavailable: Bool
    var errorDescription: String? {
        unavailable ? "Plugin command is no longer available. Reselect it from the refreshed catalog."
            : "Plugin command failed; effects may have partially completed. It was not retried."
    }
}
