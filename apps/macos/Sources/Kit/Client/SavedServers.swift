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

    /// The origin of an HTTP(S) server URL, `scheme://host[:port]` in lower
    /// case, as `kit servers` saves it; nil for anything else.
    static func canonicalURL(_ value: String) -> String? {
        guard let parts = URLComponents(string: value.trimmingCharacters(in: .whitespacesAndNewlines)),
              let scheme = parts.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = parts.percentEncodedHost, !host.isEmpty,
              parts.user == nil, parts.password == nil, parts.path.isEmpty || parts.path == "/",
              parts.query == nil, parts.fragment == nil else { return nil }
        let port = parts.port.map { ":\($0)" } ?? ""
        return (scheme + "://" + host + port).lowercased()
    }

    /// Whether ssh can be given `destination`: `[user@]host`, a configured
    /// alias, or an `ssh://` URL, in the characters `kit servers` accepts.
    static func validDestination(_ destination: String) -> Bool {
        guard !destination.isEmpty, !destination.hasPrefix("-"), destination.utf8.count <= 1024 else { return false }
        return destination.unicodeScalars.allSatisfy { character in
            character.isASCII && (CharacterSet.alphanumerics.contains(character) || "._-@:/[]%+".unicodeScalars.contains(character))
        }
    }

    /// A clean absolute path, as `kit servers add` saves.
    static func validHome(_ path: String) -> Bool {
        path.hasPrefix("/") && URL(fileURLWithPath: path).standardizedFileURL.path == path
    }

    /// Whether the entry can be saved as it is.
    var isValid: Bool {
        guard Self.validName(name) else { return false }
        switch kind {
        case .home: return Self.validHome(address)
        case .url: return Self.canonicalURL(address) == address
        case .ssh: return Self.validDestination(address)
        }
    }
}

/// Why the saved-server list couldn't be changed.
enum SavedServerError: LocalizedError, Equatable {
    case invalid(SavedServer)
    case exists(String)
    case notFound(String)
    /// The list isn't one this app can change without losing entries.
    case unreadable(String)
    case io(String)

    var errorDescription: String? {
        switch self {
        case .invalid(let server):
            if !SavedServer.validName(server.name) {
                server.name == SavedServer.localName
                    ? "“local” is this Mac’s own server."
                    : "Use up to 32 lowercase letters, digits, “.”, “-”, or “_”, starting with a letter or digit."
            } else {
                switch server.kind {
                case .home: "A Kit home must be an absolute path."
                case .url: "A server URL must be an HTTP(S) address such as https://kit.example.com."
                case .ssh: "An SSH host may contain only letters, digits, and . _ - @ : / [ ] % +."
                }
            }
        case .exists(let name): "A server named “\(name)” is already saved."
        case .notFound(let name): "No server named “\(name)” is saved."
        case .unreadable(let reason): "The saved-server list can’t be changed: \(reason)"
        case .io(let reason): "The saved-server list couldn’t be written: \(reason)"
        }
    }
}

/// Reads and changes the saved-server list. Changes hold the lock beside the
/// list that `kit servers` holds, keep entries and fields this app doesn't
/// understand, and replace the file atomically, readable only by its owner.
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
            // Entries saved before servers had kinds are Kit homes.
            let kind = entry.kind ?? (entry.home == nil ? nil : SavedServer.Kind.home.rawValue)
            guard let name = entry.name, SavedServer.validName(name), seen.insert(name).inserted,
                  let kind = kind.flatMap(SavedServer.Kind.init(rawValue:)) else { return nil }
            switch kind {
            case .home:
                guard let home = entry.home, SavedServer.validHome(home) else { return nil }
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

    /// Saves a server under a new name.
    static func add(_ server: SavedServer, to url: URL) throws {
        guard server.isValid else { throw SavedServerError.invalid(server) }
        try mutate(url) { entries in
            guard !entries.contains(where: { $0.name == server.name }) else { throw SavedServerError.exists(server.name) }
            let field = switch server.kind {
            case .home: "home"
            case .url: "url"
            case .ssh: "destination"
            }
            entries.append(RawEntry(name: server.name, fields: ["name": server.name, "kind": server.kind.rawValue, field: server.address]))
        }
    }

    /// Forgets the server saved under `name`.
    static func remove(_ name: String, from url: URL) throws {
        try mutate(url) { entries in
            guard let index = entries.firstIndex(where: { $0.name == name }) else { throw SavedServerError.notFound(name) }
            entries.remove(at: index)
        }
    }

    /// Saves the server saved under `name` under `newName` instead, keeping
    /// every other field of its entry.
    static func rename(_ name: String, to newName: String, in url: URL) throws {
        guard SavedServer.validName(newName) else { throw SavedServerError.invalid(SavedServer(name: newName, kind: .home, address: "/")) }
        try mutate(url) { entries in
            guard let index = entries.firstIndex(where: { $0.name == name }) else { throw SavedServerError.notFound(name) }
            guard !entries.contains(where: { $0.name == newName }) else { throw SavedServerError.exists(newName) }
            var fields = entries[index].fields
            fields["name"] = newName
            entries[index] = RawEntry(name: newName, fields: fields)
        }
    }

    /// One entry as the file holds it, with the fields this app doesn't know.
    private struct RawEntry {
        let name: String
        var fields: [String: Any]
    }

    private static func mutate(_ url: URL, _ change: (inout [RawEntry]) throws -> Void) throws {
        try PrivateFile.withLock(url) {
            var entries = try rawEntries(url)
            try change(&entries)
            entries.sort { $0.name < $1.name }
            let document: [String: Any] = ["servers": entries.map(\.fields)]
            try PrivateFile.replace(url, with: PrivateFile.encode(document))
        }
    }

    /// The entries of the list at `url`, refusing a list that `kit servers`
    /// would also refuse, so a change never drops what it can't read.
    private static func rawEntries(_ url: URL) throws -> [RawEntry] {
        let data: Data
        do { data = try Data(contentsOf: url) }
        catch let error as CocoaError where error.code == .fileReadNoSuchFile { return [] }
        catch { throw SavedServerError.io(error.localizedDescription) }
        guard data.count <= maximumSize else { throw SavedServerError.unreadable("it is larger than 1 MB.") }
        guard let document = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw SavedServerError.unreadable("it isn’t valid JSON.")
        }
        let list: [Any]
        switch document["servers"] {
        case nil, is NSNull: list = []
        case let servers as [Any]: list = servers
        default: throw SavedServerError.unreadable("its servers aren’t a list.")
        }
        var names = Set<String>()
        return try list.map { item in
            guard let fields = item as? [String: Any], let name = fields["name"] as? String else {
                throw SavedServerError.unreadable("an entry has no name.")
            }
            guard names.insert(name).inserted else { throw SavedServerError.unreadable("“\(name)” is listed twice.") }
            return RawEntry(name: name, fields: fields)
        }
    }
}

/// Files Kit's Go side also writes, changed the way it changes them: under an
/// exclusive lock on a sibling `.lock` file and replaced atomically, readable
/// only by their owner.
enum PrivateFile {
    /// Runs `body` holding the lock for `url`, creating its directory.
    static func withLock<T>(_ url: URL, _ body: () throws -> T) throws -> T {
        let directory = url.deletingLastPathComponent()
        do { try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700]) }
        catch { throw SavedServerError.io(error.localizedDescription) }
        let lock = open(url.path + ".lock", O_CREAT | O_RDWR | O_CLOEXEC, 0o600)
        guard lock >= 0 else { throw SavedServerError.io(String(cString: strerror(errno))) }
        defer { close(lock) }
        guard flock(lock, LOCK_EX) == 0 else { throw SavedServerError.io(String(cString: strerror(errno))) }
        defer { flock(lock, LOCK_UN) }
        return try body()
    }

    /// A JSON document as Kit writes it, ending with a newline.
    static func encode(_ document: [String: Any]) throws -> Data {
        var data: Data
        do { data = try JSONSerialization.data(withJSONObject: document, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]) }
        catch { throw SavedServerError.io(error.localizedDescription) }
        data.append(0x0A)
        return data
    }

    /// Replaces `url` with `data` atomically, readable only by its owner.
    static func replace(_ url: URL, with data: Data) throws {
        let temporary = url.deletingLastPathComponent().appendingPathComponent(".\(url.lastPathComponent)-\(UUID().uuidString)")
        let descriptor = open(temporary.path, O_CREAT | O_EXCL | O_WRONLY | O_CLOEXEC, 0o600)
        guard descriptor >= 0 else { throw SavedServerError.io(String(cString: strerror(errno))) }
        defer { unlink(temporary.path) }
        let written = data.withUnsafeBytes { buffer in write(descriptor, buffer.baseAddress, buffer.count) }
        let synced = fsync(descriptor) == 0
        close(descriptor)
        guard written == data.count, synced, Darwin.rename(temporary.path, url.path) == 0 else {
            throw SavedServerError.io(String(cString: strerror(errno)))
        }
    }
}
