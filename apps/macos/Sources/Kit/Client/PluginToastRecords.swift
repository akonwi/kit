import Foundation
import OpenAPIRuntime

/// Strictly decodes the live `plugin.toast` records declared by the contract.
enum PluginToastRecords {
    static let recordName = "plugin.toast"

    static func decode(_ event: ServerSentEvent) throws -> PluginNotification? {
        if event.event == nil, event.data == nil, event.id == nil, event.retry == nil { return nil }
        guard event.event == recordName, event.id == nil, event.retry == nil,
              let data = event.data, !data.contains("\n") else { throw ClientError.invalidPayload }
        let bytes = Data(data.utf8)
        guard StrictJSON.uniqueMembers(bytes) else { throw ClientError.invalidPayload }
        do { _ = try JSONDecoder().decode(Components.Schemas.PluginToast.self, from: bytes) }
        catch { throw ClientError.invalidPayload }
        return try PluginNotification.decode(bytes)
    }
}
