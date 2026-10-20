import Foundation

/// One entry of the saved-server list (`<home>/servers.json`), which the
/// terminal client and the app share. It holds only how to reach a server;
/// secrets live elsewhere.
struct SavedServer: Equatable, Sendable {
    enum Kind: String, Sendable {
        /// The server of a Kit home on this Mac.
        case home
        /// A server served at an HTTP(S) URL, reached with a device token.
        case url
        /// The server of a host's default Kit home, reached over SSH.
        case ssh
    }

    let name: String
    let kind: Kind
    /// The Kit home path, URL, or SSH destination.
    let address: String

    /// The name of the app's own Kit home's server. It is reserved and never saved.
    static let localName = "local"
    static let maximumNameLength = 32

    /// Whether a name can be saved: 1 to 32 lowercase letters, digits, '.',
    /// '-', or '_', starting with a letter or digit, and not `local`.
    static func validName(_ name: String) -> Bool {
        guard !name.isEmpty, name.utf8.count <= maximumNameLength, name != localName else { return false }
        return name.unicodeScalars.enumerated().allSatisfy { index, character in
            let alphanumeric = ("a"..."z").contains(character) || ("0"..."9").contains(character)
            return alphanumeric || (index > 0 && ".-_".unicodeScalars.contains(character))
        }
    }
}

/// Reads the saved-server list. Changes are made by `kit servers`, which
/// keeps entries this app doesn't understand.
enum SavedServerList {
    private struct Document: Decodable {
        let servers: [Entry]?
    }

    private struct Entry: Decodable {
        let name: String?
        let kind: String?
        let home: String?
        let url: String?
        let destination: String?
    }

    static let maximumSize = 1 << 20

    /// The servers saved in `url`, in name order. A missing file lists none.
    /// Entries of unknown kinds, invalid entries, and repeated names are
    /// skipped, so one bad entry doesn't hide the rest.
    static func read(_ url: URL) throws -> [SavedServer] {
        let data: Data
        do { data = try Data(contentsOf: url) }
        catch let error as CocoaError where error.code == .fileReadNoSuchFile { return [] }
        guard data.count <= maximumSize else { throw ClientError.oversized }
        return decode(data)
    }

    static func decode(_ data: Data) -> [SavedServer] {
        guard let document = try? JSONDecoder().decode(Document.self, from: data) else { return [] }
        var seen = Set<String>()
        let servers = (document.servers ?? []).compactMap { entry -> SavedServer? in
            guard let name = entry.name, SavedServer.validName(name), seen.insert(name).inserted,
                  let kind = entry.kind.flatMap(SavedServer.Kind.init(rawValue:)) else { return nil }
            switch kind {
            case .home:
                guard let home = entry.home, validHome(home) else { return nil }
                return SavedServer(name: name, kind: kind, address: home)
            case .url:
                guard let url = entry.url, !url.isEmpty else { return nil }
                return SavedServer(name: name, kind: kind, address: url)
            case .ssh:
                guard let destination = entry.destination, !destination.isEmpty else { return nil }
                return SavedServer(name: name, kind: kind, address: destination)
            }
        }
        return servers.sorted { $0.name < $1.name }
    }

    /// A clean absolute path, as `kit servers add` saves.
    private static func validHome(_ path: String) -> Bool {
        path.hasPrefix("/") && URL(fileURLWithPath: path).standardizedFileURL.path == path
    }
}
