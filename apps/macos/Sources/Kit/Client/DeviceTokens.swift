import Foundation
import Security

/// Where a served server's device token is kept: under the server's saved
/// name in one Kit home's list.
struct DeviceCredential: Hashable, Sendable {
    /// The Kit home whose saved-server list names the server.
    let home: String
    let name: String
}

/// Keeps the device tokens this Mac was issued by served servers. Each token
/// is bound to the URL it was issued for and is returned only for that URL,
/// so it is never sent to another server saved under the name later.
protocol DeviceTokenStore: Sendable {
    func token(for credential: DeviceCredential, url: String) -> String?
    func save(_ token: String, for credential: DeviceCredential, url: String) throws
    func delete(_ credential: DeviceCredential) throws
}

extension DeviceTokenStore {
    /// Keeps a server's token under its new name.
    func rename(_ credential: DeviceCredential, to name: String, url: String) throws {
        guard let token = token(for: credential, url: url) else { return }
        try save(token, for: DeviceCredential(home: credential.home, name: name), url: url)
        try delete(credential)
    }
}

/// Whether a value has the form of a device token: `kit_device_` and 43
/// unpadded base64url characters.
func isDeviceToken(_ value: String) -> Bool {
    let prefix = "kit_device_"
    guard value.hasPrefix(prefix) else { return false }
    let body = value.utf8.dropFirst(prefix.utf8.count)
    return body.count == 43 && body.allSatisfy { byte in
        (0x41...0x5A).contains(byte) || (0x61...0x7A).contains(byte) || (0x30...0x39).contains(byte) || byte == 0x2D || byte == 0x5F
    }
}

/// The app's device tokens: in the Keychain, falling back to the tokens the
/// terminal client saved beside the saved-server list, which are only read.
struct DeviceTokens: DeviceTokenStore {
    static let standard = DeviceTokens(keychain: KeychainDeviceTokens())

    let keychain: any DeviceTokenStore

    func token(for credential: DeviceCredential, url: String) -> String? {
        keychain.token(for: credential, url: url) ?? TerminalDeviceTokens.token(for: credential, url: url)
    }

    func save(_ token: String, for credential: DeviceCredential, url: String) throws {
        try keychain.save(token, for: credential, url: url)
    }

    func delete(_ credential: DeviceCredential) throws {
        try keychain.delete(credential)
    }
}

/// Why a device token couldn't be kept.
struct DeviceTokenError: LocalizedError, Equatable {
    let status: OSStatus

    var errorDescription: String? {
        let reason = SecCopyErrorMessageString(status, nil) as String? ?? "error \(status)"
        return "The device token couldn’t be saved in the Keychain: \(reason)"
    }
}

/// Device tokens as generic passwords in the login Keychain, one per Kit
/// home and server name, holding the token and the URL it was issued for.
struct KeychainDeviceTokens: DeviceTokenStore {
    static let service = "com.akonwi.kit.device-token"

    private struct Item: Codable {
        let url: String
        let token: String
    }

    private func query(_ credential: DeviceCredential) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: Self.service,
         kSecAttrAccount as String: credential.home + "\n" + credential.name]
    }

    func token(for credential: DeviceCredential, url: String) -> String? {
        var search = query(credential)
        search[kSecReturnData as String] = true
        search[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        guard SecItemCopyMatching(search as CFDictionary, &result) == errSecSuccess, let data = result as? Data,
              let item = try? JSONDecoder().decode(Item.self, from: data),
              item.url == url, isDeviceToken(item.token) else { return nil }
        return item.token
    }

    func save(_ token: String, for credential: DeviceCredential, url: String) throws {
        let data = try JSONEncoder().encode(Item(url: url, token: token))
        let update: [String: Any] = [kSecValueData as String: data]
        var status = SecItemUpdate(query(credential) as CFDictionary, update as CFDictionary)
        if status == errSecItemNotFound {
            var item = query(credential)
            item[kSecValueData as String] = data
            item[kSecAttrLabel as String] = "Kit device token for \(credential.name)"
            item[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            status = SecItemAdd(item as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw DeviceTokenError(status: status) }
    }

    func delete(_ credential: DeviceCredential) throws {
        let status = SecItemDelete(query(credential) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw DeviceTokenError(status: status) }
    }
}

/// The device tokens the terminal client keeps in `server-tokens.json`
/// beside the saved-server list. The app only reads them, so a server paired
/// with `kit attach` connects here too.
enum TerminalDeviceTokens {
    private struct Document: Decodable {
        struct Entry: Decodable {
            let url: String
            let token: String
        }
        let tokens: [String: Entry]?
    }

    static func token(for credential: DeviceCredential, url: String) -> String? {
        let path = URL(fileURLWithPath: credential.home).appendingPathComponent("server-tokens.json").path
        // Like the terminal client, trust only a file no one else can read.
        guard let attributes = try? FileManager.default.attributesOfItem(atPath: path),
              let permissions = attributes[.posixPermissions] as? Int, permissions & 0o077 == 0,
              let size = attributes[.size] as? Int, size <= SavedServerList.maximumSize,
              let data = FileManager.default.contents(atPath: path),
              let document = try? JSONDecoder().decode(Document.self, from: data),
              let entry = document.tokens?[credential.name],
              entry.url == url, isDeviceToken(entry.token) else { return nil }
        return entry.token
    }
}

/// Device tokens held in memory, for tests and previews.
final class MemoryDeviceTokens: DeviceTokenStore, @unchecked Sendable {
    private let lock = NSLock()
    private var tokens: [DeviceCredential: (url: String, token: String)] = [:]

    init(_ tokens: [DeviceCredential: (url: String, token: String)] = [:]) {
        self.tokens = tokens
    }

    func token(for credential: DeviceCredential, url: String) -> String? {
        lock.withLock { tokens[credential].flatMap { $0.url == url ? $0.token : nil } }
    }

    func save(_ token: String, for credential: DeviceCredential, url: String) throws {
        lock.withLock { tokens[credential] = (url, token) }
    }

    func delete(_ credential: DeviceCredential) throws {
        lock.withLock { tokens[credential] = nil }
    }
}
