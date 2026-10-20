import Foundation

/// A pairing link, `https://host/pair#CODE`: a server's HTTP(S) origin and
/// a single-use code, in the fragment so browsers and proxies never send it.
struct PairingLink: Equatable, Sendable {
    /// The server's origin, as `kit servers` saves it.
    let server: String
    let code: String

    static let path = "/pair"

    init(server: String, code: String) {
        self.server = server
        self.code = code
    }

    /// Parses a link as `kit attach` does; nil for anything else.
    init?(_ link: String) {
        guard let parts = URLComponents(string: link.trimmingCharacters(in: .whitespacesAndNewlines)),
              let scheme = parts.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = parts.percentEncodedHost, !host.isEmpty, parts.user == nil, parts.password == nil,
              parts.path == Self.path, parts.query == nil, let code = parts.fragment, Self.validCode(code),
              let server = SavedServer.canonicalURL(scheme + "://" + host + (parts.port.map { ":\($0)" } ?? ""))
        else { return nil }
        self.server = server
        self.code = code
    }

    /// Whether a value looks like a link, to tell it from other locations.
    static func looksLikeLink(_ value: String) -> Bool {
        value.contains(path + "#")
    }

    /// A pairing code: 26 characters of lowercase base32.
    static func validCode(_ code: String) -> Bool {
        code.utf8.count == 26 && code.utf8.allSatisfy { (0x61...0x7A).contains($0) || (0x32...0x37).contains($0) }
    }

    /// The link that joins `server` and `code`.
    static func link(server: String, code: String) -> String {
        (server.hasSuffix("/") ? String(server.dropLast()) : server) + path + "#" + code
    }
}

/// A pairing code this Mac issued for another device.
struct PairingCode: Equatable, Sendable {
    let code: String
    let expiresAt: Date
    /// The origins the server says it is served at, loopback last.
    let urls: [String]

    static func date(_ value: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: value) { return date }
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: value)
    }
}

/// Why redeeming a pairing link failed.
enum PairingError: LocalizedError, Equatable {
    case invalidLink
    case codeRefused
    case rateLimited
    /// The link is for another server than the one being paired again.
    case otherServer(expected: String, link: String)
    /// Another saved server has the link's URL.
    case alreadySaved(name: String, url: String)

    var errorDescription: String? {
        switch self {
        case .invalidLink: "That isn’t a pairing link. It looks like https://host/pair#code."
        case .codeRefused: "This link was already used or has expired. Get a new one and try again."
        case .rateLimited: "Too many pairing attempts. Try again shortly."
        case .otherServer(let expected, let link): "This link is for \(link), not \(expected)."
        case .alreadySaved(let name, let url): "\(url) is already saved as \(name)."
        }
    }
}

enum DeviceName {
    /// How this Mac names itself to the servers it pairs with: its name in
    /// Sharing settings, within the server's limit of 64 bytes.
    static func current() -> String {
        clean(Host.current().localizedName ?? ProcessInfo.processInfo.hostName)
    }

    static func clean(_ value: String) -> String {
        var name = String(value.unicodeScalars.filter { !CharacterSet.controlCharacters.contains($0) && $0.properties.generalCategory != .format })
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if name.hasSuffix(".local") { name = String(name.dropLast(6)) }
        while name.utf8.count > 64 { name.removeLast() }
        name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        // A name shaped like a device ID would shadow one.
        if name.isEmpty || name.hasPrefix("device_") { return "Kit for Mac" }
        return name
    }
}
