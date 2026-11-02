import Foundation
import KitClient
import Observation

/// A paired server's connection as the app last found it.
enum ServerState: Equatable, Sendable {
    case connecting
    case online(kitVersion: String)
    case unreachable(since: Date)
    case needsPairing(ClientError.PairingNeed)
    /// The server speaks an older protocol than this app.
    case serverNeedsUpdate(kitVersion: String, serverProtocol: Int)
    /// The server speaks a newer protocol than this app.
    case appNeedsUpdate(kitVersion: String, serverProtocol: Int)
    case failed(String)

    /// The state a failed check leaves a server in.
    static func from(_ error: any Error, at date: Date) -> ServerState {
        if let error = error as? ClientError {
            switch error {
            case .needsPairing(let need): return .needsPairing(need)
            case .http(401): return .needsPairing(.refused)
            case .incompatibleDaemon(let version, _, let serverProtocol, let reason):
                return reason == .daemonProtocolOlder
                    ? .serverNeedsUpdate(kitVersion: version, serverProtocol: serverProtocol)
                    : .appNeedsUpdate(kitVersion: version, serverProtocol: serverProtocol)
            default: return .failed(error.localizedDescription)
            }
        }
        if error is URLError || (error as NSError).domain == NSURLErrorDomain { return .unreachable(since: date) }
        return .failed(error.localizedDescription)
    }
}

/// What a server reported when it answered.
struct ServerStatus: Equatable, Sendable {
    let kitVersion: String
    let sessions: Int
}

/// How the app reaches servers, so tests can stand in for the network.
protocol ServerNetwork: Sendable {
    /// Redeems a pairing code for a device token.
    func redeem(server: URL, code: String, deviceName: String) async throws -> String
    /// Verifies a paired server and counts its sessions.
    func status(server: URL, token: String, name: String) async throws -> ServerStatus
}

struct LiveServerNetwork: ServerNetwork {
    func redeem(server: URL, code: String, deviceName: String) async throws -> String {
        try await HTTPClient.redeemPairing(url: server, code: code, deviceName: deviceName)
    }

    func status(server: URL, token: String, name: String) async throws -> ServerStatus {
        let (client, identity) = try await HTTPClient.handshake(url: server, token: token, serverID: name)
        return ServerStatus(kitVersion: identity.kitVersion, sessions: try await client.sessions().count)
    }
}

/// The servers this iPhone is paired with. Each is saved in the app's own
/// saved-server list, with its device token in the Keychain.
@MainActor @Observable
final class ServerStore {
    struct Entry: Identifiable, Equatable {
        let server: SavedServer
        var state: ServerState
        /// The session count from the last time the server answered.
        var sessions: Int?

        var id: String { server.name }
        var url: String { server.address }
    }

    private(set) var entries: [Entry] = []
    /// Why the saved list couldn't be read, if it couldn't.
    private(set) var loadError: String?

    private let list: URL
    private let tokens: any DeviceTokenStore
    private let connector: any ServerNetwork
    private let now: @Sendable () -> Date

    /// The Kit home device tokens are filed under. This app has no Kit home,
    /// so its tokens share one fixed label.
    nonisolated static let credentialHome = "ios"

    init(list: URL, tokens: any DeviceTokenStore, connector: any ServerNetwork,
         now: @escaping @Sendable () -> Date = { Date() }) {
        self.list = list
        self.tokens = tokens
        self.connector = connector
        self.now = now
        reload()
    }

    static func live() -> ServerStore {
        let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return ServerStore(list: support.appendingPathComponent("servers.json"), tokens: KeychainDeviceTokens(),
                           connector: LiveServerNetwork())
    }

    func entry(_ name: String) -> Entry? {
        entries.first { $0.id == name }
    }

    /// Reads the saved list again, keeping what is known about servers
    /// that are still saved at the same URL.
    func reload() {
        let saved: [SavedServer]
        do {
            saved = try SavedServerList.read(list).filter { $0.kind == .url }
            loadError = nil
        } catch {
            saved = []
            loadError = error.localizedDescription
        }
        let known = Dictionary(entries.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        entries = saved.map { server in
            if let entry = known[server.name], entry.server == server { return entry }
            return Entry(server: server, state: .connecting)
        }
    }

    /// Checks every saved server at once.
    func checkAll() async {
        reload()
        await withTaskGroup(of: Void.self) { group in
            for entry in entries {
                group.addTask { await self.check(entry.id) }
            }
        }
    }

    /// Verifies one server and counts its sessions. A server that was
    /// online stays shown as online while it is checked again.
    func check(_ name: String) async {
        guard let entry = entry(name) else { return }
        let server = entry.server
        guard let token = tokens.token(for: credential(name), url: server.address) else {
            update(server) { $0.state = .needsPairing(.missing) }
            return
        }
        guard let url = URL(string: server.address) else { return }
        if case .online = entry.state {} else { update(server) { $0.state = .connecting } }
        do {
            let status = try await connector.status(server: url, token: token, name: name)
            update(server) {
                $0.state = .online(kitVersion: status.kitVersion)
                $0.sessions = status.sessions
            }
        } catch {
            let state = ServerState.from(error, at: now())
            update(server) { $0.state = state }
        }
    }

    /// Redeems a pairing link and saves its server as `name`, then checks
    /// it. The name is claimed before the single-use code is spent and
    /// released if pairing fails.
    func pair(_ link: PairingLink, as name: String, deviceName: String) async throws {
        if let existing = entries.first(where: { $0.url == link.server }) {
            throw PairingError.alreadySaved(name: existing.id, url: link.server)
        }
        try SavedServerList.add(SavedServer(name: name, kind: .url, address: link.server), to: list)
        do {
            try await redeem(link, for: name, deviceName: deviceName)
        } catch {
            try? SavedServerList.remove(name, from: list)
            reload()
            throw error
        }
        reload()
        await check(name)
    }

    /// Pairs again with the saved server `name`, with a link for its URL.
    func pairAgain(_ name: String, link: PairingLink, deviceName: String) async throws {
        guard let entry = entry(name) else { throw SavedServerError.notFound(name) }
        guard entry.url == link.server else { throw PairingError.otherServer(expected: entry.url, link: link.server) }
        try await redeem(link, for: name, deviceName: deviceName)
        await check(name)
    }

    /// Forgets a server and its device token.
    func remove(_ name: String) throws {
        try SavedServerList.remove(name, from: list)
        try? tokens.delete(credential(name))
        reload()
    }

    /// The name a new server is offered: its host's first label, or the
    /// whole address for an IP, made valid and unused.
    func suggestedName(for link: PairingLink) -> String {
        ServerNaming.suggested(host: URLComponents(string: link.server)?.host ?? "", taken: Set(entries.map(\.id)))
    }

    private func redeem(_ link: PairingLink, for name: String, deviceName: String) async throws {
        guard let url = URL(string: link.server) else { throw PairingError.invalidLink }
        let token = try await connector.redeem(server: url, code: link.code, deviceName: deviceName)
        // The server may have been removed while the link was redeemed.
        // Then its token isn't saved: nothing would use or delete it.
        let current = (try? SavedServerList.read(list)) ?? []
        guard current.contains(where: { $0.name == name && $0.kind == .url && $0.address == link.server }) else {
            throw SavedServerError.notFound(name)
        }
        try tokens.save(token, for: credential(name), url: link.server)
    }

    private func credential(_ name: String) -> DeviceCredential {
        DeviceCredential(home: Self.credentialHome, name: name)
    }

    /// Changes a server's entry if it is still saved as it was.
    private func update(_ server: SavedServer, _ change: (inout Entry) -> Void) {
        guard let index = entries.firstIndex(where: { $0.server == server }) else { return }
        change(&entries[index])
    }
}

enum ServerNaming {
    static func suggested(host: String, taken: Set<String>) -> String {
        let trimmed = host.trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        let isAddress = trimmed.contains(":") || trimmed.allSatisfy { $0.isNumber || $0 == "." }
        let base = isAddress ? trimmed : String(trimmed.split(separator: ".").first ?? "")
        var name = String(base.lowercased().unicodeScalars.map { scalar -> Character in
            let allowed = ("a"..."z").contains(scalar) || ("0"..."9").contains(scalar) || ".-_".unicodeScalars.contains(scalar)
            return allowed ? Character(scalar) : "-"
        })
        while let first = name.first, !first.isLetter && !first.isNumber { name.removeFirst() }
        name = String(name.prefix(SavedServer.maximumNameLength))
        if !SavedServer.validName(name), name != SavedServer.localName { name = "server" }
        let taken = taken.union([SavedServer.localName])
        guard taken.contains(name) else { return name }
        for index in 2... {
            let suffix = "-\(index)"
            let candidate = String(name.prefix(SavedServer.maximumNameLength - suffix.count)) + suffix
            if !taken.contains(candidate) { return candidate }
        }
        return name
    }
}
