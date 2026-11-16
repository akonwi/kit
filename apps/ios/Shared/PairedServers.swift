import Foundation
import KitClient

/// The servers this iPhone is paired with, as the app and its share
/// extension read them: the saved list in the app group, and each server's
/// device token in the Keychain group they share.
enum PairedServers {
    /// The Kit home device tokens are filed under. The app has no Kit home,
    /// so its tokens share one fixed label.
    static let credentialHome = "ios"

    /// The saved-server list.
    static var list: URL { SharedStorage.directory.appendingPathComponent("servers.json") }

    static func credential(_ name: String) -> DeviceCredential {
        DeviceCredential(home: credentialHome, name: name)
    }

    /// The saved servers reached by URL, the only kind the iPhone pairs with.
    static func saved() throws -> [SavedServer] {
        try SavedServerList.read(list).filter { $0.kind == .url }
    }

    /// A client for a saved server, with its device token.
    static func client(for server: SavedServer, tokens: any DeviceTokenStore = KeychainDeviceTokens()) -> ServerClient {
        ServerClient(serverID: server.name, connector: .url(server.address, credential: credential(server.name)), tokens: tokens)
    }
}
