import Foundation

protocol PluginNotificationClient: SessionClient {
    func watchPluginNotifications(_ session: String, receive: @escaping @Sendable (PluginNotification) async -> Void) async throws
}

extension LocalClient: PluginNotificationClient {
    func watchPluginNotifications(_ session: String, receive: @escaping @Sendable (PluginNotification) async -> Void) async throws {
        try await HTTPClient.local().watchPluginNotifications(session, receive: receive)
    }
}

/// Live-only plugin metadata, projected before reaching the alert surface.
struct PluginNotification: Sendable {
    let pluginID: String
    let title: String
    let detail: String
    let variant: String
    let persistent: Bool

    // Foundation JSON decoders collapse duplicate keys. Inspect string tokens
    // before decoding this flat object so escaped spellings cannot bypass checks.
    private static func uniqueMembers(_ data: Data) -> Bool {
        let bytes = Array(data)
        var index = 0, seen = Set<String>()
        while index < bytes.count {
            guard bytes[index] == 34 else { index += 1; continue }
            let start = index
            index += 1
            while index < bytes.count && bytes[index] != 34 {
                index += bytes[index] == 92 ? 2 : 1
            }
            guard index < bytes.count else { return false }
            index += 1
            let end = index
            while index < bytes.count && [9, 10, 13, 32].contains(bytes[index]) { index += 1 }
            if index < bytes.count && bytes[index] == 58 {
                guard let key = try? JSONDecoder().decode(String.self, from: Data(bytes[start..<end])),
                      seen.insert(key).inserted else { return false }
            }
        }
        return true
    }

    static func decode(_ data: Data) throws -> Self {
        guard data.count < 32 * 1024, String(data: data, encoding: .utf8) != nil, uniqueMembers(data),
              let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              Set(object.keys).isSubset(of: ["pluginId", "instance", "title", "subtitle", "variant", "persistent"])
        else { throw ClientError.invalidPayload }
        struct Payload: Decodable {
            let pluginId: String; let instance: String; let title: String
            let subtitle: String?; let variant: String; let persistent: Bool?
        }
        let value = try JSONDecoder().decode(Payload.self, from: data)
        let detail = value.subtitle ?? ""
        guard PluginCommand.matches(value.pluginId, "^[a-z][a-z0-9-]{0,31}$"),
              PluginCommand.matches(value.instance, "^[a-zA-Z0-9_.:-]{1,128}$"),
              PluginCommand.safeText(value.title, limit: 1024),
              !value.title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              detail.utf8.count <= 4096,
              !detail.unicodeScalars.contains(where: {
                  (CharacterSet.controlCharacters.contains($0) && $0 != "\n" && $0 != "\t") || $0.properties.generalCategory == .format
              }), ["info", "warning", "error"].contains(value.variant)
        else { throw ClientError.invalidPayload }
        return Self(pluginID: value.pluginId, title: value.title, detail: detail, variant: value.variant, persistent: value.persistent ?? false)
    }
}
