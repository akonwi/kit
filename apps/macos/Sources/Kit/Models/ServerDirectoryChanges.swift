import Foundation

/// Changes to the saved servers, made from Settings and from tabs whose
/// server needs pairing. Each writes the shared list, as `kit servers` does,
/// then reads it again so every window follows.
extension ServerDirectory {
    /// Saves a Kit home or SSH host under a new name.
    func add(_ server: SavedServer) throws {
        var directory: ObjCBool = false
        if server.kind == .home, !(FileManager.default.fileExists(atPath: server.address, isDirectory: &directory) && directory.boolValue) {
            throw ServerLocationError.noHome(server.address)
        }
        try SavedServerList.add(server, to: list)
        refresh()
    }

    /// Redeems a pairing link and saves its server under `name` with the
    /// device token it issued. The name is claimed before the link's
    /// single-use code is spent, and released if pairing fails.
    func add(name: String, link: PairingLink, deviceName: String = DeviceName.current(),
             redeem: (URL, String, String) async throws -> String = HTTPClient.redeemPairing) async throws {
        if let existing = savedServers.first(where: { $0.kind == .url && $0.address == link.server }) {
            throw PairingError.alreadySaved(name: existing.name, url: link.server)
        }
        let server = SavedServer(name: name, kind: .url, address: link.server)
        try SavedServerList.add(server, to: list)
        do {
            try await pair(name: name, link: link, deviceName: deviceName, redeem: redeem)
        } catch {
            try? SavedServerList.remove(name, from: list)
            refresh()
            throw error
        }
        refresh()
    }

    /// Pairs this Mac with the saved served server `name` again, with a link
    /// for its URL, and checks it now.
    func pairAgain(_ name: String, link: PairingLink, deviceName: String = DeviceName.current(),
                   redeem: (URL, String, String) async throws -> String = HTTPClient.redeemPairing) async throws {
        guard let server = savedServers.first(where: { $0.name == name }), server.kind == .url else {
            throw SavedServerError.notFound(name)
        }
        guard server.address == link.server else { throw PairingError.otherServer(expected: server.address, link: link.server) }
        try await pair(name: name, link: link, deviceName: deviceName, redeem: redeem)
        connection(named: name)?.checkNow()
    }

    private func pair(name: String, link: PairingLink, deviceName: String,
                      redeem: (URL, String, String) async throws -> String) async throws {
        guard let url = URL(string: link.server) else { throw PairingError.invalidLink }
        let token = try await redeem(url, link.code, deviceName)
        // The server may have been removed or renamed while the link was
        // redeemed. Then its token isn't saved: nothing would use or delete
        // it, and it could replace the token of another server saved under
        // the name meanwhile.
        let current = (try? SavedServerList.read(list)) ?? []
        guard current.contains(where: { $0.name == name && $0.kind == .url && $0.address == link.server }) else {
            throw SavedServerError.notFound(name)
        }
        try tokens.save(token, for: DeviceCredential(home: home.path, name: name), url: link.server)
    }

    /// Saves the server `name` under `newName`. Its device token moves with
    /// it, and windows move its tabs.
    func rename(_ name: String, to newName: String) throws {
        let server = savedServers.first { $0.name == name }
        try SavedServerList.rename(name, to: newName, in: list)
        if let server {
            if server.kind == .url {
                try? tokens.rename(DeviceCredential(home: home.path, name: name), to: newName, url: server.address)
                try? TerminalDeviceTokens.rename(name, to: newName, home: home.path)
            }
            // Clients some views still hold for the old name reach the
            // server as it's now saved, with its token under the new name.
            let renamed = SavedServer(name: newName, kind: server.kind, address: server.address)
            connection(named: name)?.client.address.repoint(to: .saved(renamed, home: home))
        }
        refresh()
        if launcherServer == name { launcherServer = newName }
        TemporarySessions.shared.serverRenamed(from: name, to: newName)
        if SettingsRoute.shared.server == name { SettingsRoute.shared.server = newName }
        serverRenamed(name, newName)
    }

    /// Forgets the server `name` and its device token; windows close its tabs.
    func remove(_ name: String) throws {
        let server = savedServers.first { $0.name == name }
        try SavedServerList.remove(name, from: list)
        if server?.kind == .url {
            try? tokens.delete(DeviceCredential(home: home.path, name: name))
            try? TerminalDeviceTokens.delete(name, home: home.path)
        }
        refresh()
        if launcherServer == name { launcherServer = ServerClient.localID }
        if SettingsRoute.shared.server == name { SettingsRoute.shared.server = ServerClient.localID }
        serverRemoved(name)
    }

    /// The name a new server is offered, from its location: a pairing link's
    /// or URL's host, an SSH host, or a Kit home's folder, made valid and
    /// unused.
    func suggestedName(for location: ServerLocation) -> String {
        let base: String
        switch location {
        case .link(let link): base = Self.hostName(URLComponents(string: link.server)?.host ?? "")
        case .ssh(let destination):
            var host = destination.hasPrefix("ssh://") ? String(destination.dropFirst(6)) : destination
            if let at = host.lastIndex(of: "@") { host = String(host[host.index(after: at)...]) }
            if let colon = host.firstIndex(of: ":") { host = String(host[..<colon]) }
            if let slash = host.firstIndex(of: "/") { host = String(host[..<slash]) }
            base = Self.hostName(host)
        case .home(let path):
            let folder = URL(fileURLWithPath: path).lastPathComponent
            base = folder == ".kit" ? URL(fileURLWithPath: path).deletingLastPathComponent().lastPathComponent : folder
        }
        var name = String(base.lowercased().unicodeScalars.map { scalar -> Character in
            let allowed = ("a"..."z").contains(scalar) || ("0"..."9").contains(scalar) || ".-_".unicodeScalars.contains(scalar)
            return allowed ? Character(scalar) : "-"
        })
        while let first = name.first, !first.isLetter && !first.isNumber { name.removeFirst() }
        name = String(name.prefix(SavedServer.maximumNameLength))
        if !SavedServer.validName(name), name != SavedServer.localName { name = "server" }
        let taken = Set(savedServers.map(\.name)).union([SavedServer.localName])
        guard taken.contains(name) else { return name }
        for index in 2... {
            let suffix = "-\(index)"
            let candidate = String(name.prefix(SavedServer.maximumNameLength - suffix.count)) + suffix
            if !taken.contains(candidate) { return candidate }
        }
        return name
    }

    /// A host's first label, unless the host is an IP address.
    private static func hostName(_ host: String) -> String {
        let trimmed = host.trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        if trimmed.contains(":") || trimmed.allSatisfy({ $0.isNumber || $0 == "." }) { return trimmed }
        return String(trimmed.split(separator: ".").first ?? "")
    }
}

enum ServerLocationError: LocalizedError, Equatable {
    case noHome(String)

    var errorDescription: String? {
        switch self {
        case .noHome(let path): "There’s no folder at \(ServerStatePresentation.abbreviated(path))."
        }
    }
}

/// What Add Server found in a location the user typed.
enum ServerLocation: Equatable {
    /// A pairing link for a served server.
    case link(PairingLink)
    /// An SSH host: `user@host` or an `ssh://` URL.
    case ssh(String)
    /// The absolute path of a Kit home on this Mac.
    case home(String)

    /// Reads a location as `kit servers add` does: a pairing link, an
    /// `ssh://` URL or `user@host`, or a path. Anything else is nil.
    init?(_ value: String, userHome: String = FileManager.default.homeDirectoryForCurrentUser.path) {
        let value = value.trimmingCharacters(in: .whitespacesAndNewlines)
        if PairingLink.looksLikeLink(value) {
            guard let link = PairingLink(value) else { return nil }
            self = .link(link)
        } else if value.hasPrefix("http://") || value.hasPrefix("https://") {
            return nil
        } else if value.hasPrefix("/") || value.hasPrefix("~") {
            var path = value
            if path == "~" || path.hasPrefix("~/") { path = userHome + path.dropFirst() }
            guard path.hasPrefix("/") else { return nil }
            self = .home(URL(fileURLWithPath: path).standardizedFileURL.path)
        } else {
            // Like `kit servers add`, anything else is an SSH destination:
            // user@host, an ssh:// URL, or a host or alias.
            guard SavedServer.validDestination(value) else { return nil }
            self = .ssh(value)
        }
    }
}
