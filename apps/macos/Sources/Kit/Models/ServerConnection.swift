import Foundation
import Observation

/// Why a reachable server can't serve this app: the two speak different
/// session protocols.
struct ServerMismatch: Equatable, Sendable {
    let serverKitVersion: String
    let serverProtocol: Int
    let appProtocol: Int
    let reason: DaemonCompatibility.Mismatch

    /// The server is older than this app and needs updating.
    var serverIsOlder: Bool { reason == .daemonProtocolOlder }
}

/// What the app currently knows about reaching a server.
enum ServerStatus: Equatable, Sendable {
    /// The first check hasn't finished.
    case connecting
    case connected(ServerIdentity)
    /// The server couldn't be reached or verified. The next check is at
    /// `retry`, or is underway when it is nil.
    case unreachable(reason: String, retry: Date?)
    /// The server speaks another protocol. It is checked again occasionally,
    /// so restarting it with a compatible Kit resumes its tabs.
    case incompatible(ServerMismatch)

    var identity: ServerIdentity? {
        if case .connected(let identity) = self { identity } else { nil }
    }
    var isConnected: Bool { identity != nil }
}

/// The app's one connection to a server, shared by every window. It checks
/// the server when the app opens and keeps checking, with backoff while the
/// server can't be reached. It never starts, stops, or restarts a server.
@MainActor @Observable
final class ServerConnection: Identifiable {
    /// How often the server is checked.
    struct Schedule: Sendable {
        /// The first retry after a failure; each later one doubles, up to `maximum`.
        var firstRetry: Duration = .seconds(2)
        var maximumRetry: Duration = .seconds(30)
        /// How often a connected or incompatible server is checked again.
        var check: Duration = .seconds(30)
    }

    typealias Handshake = @Sendable () async throws -> ServerIdentity

    /// The server's name: `local`, or its saved name.
    let name: String
    let connector: ServerConnector
    let client: ServerClient
    let catalog: SessionCatalog
    private(set) var status: ServerStatus = .connecting

    @ObservationIgnored private let handshake: Handshake
    @ObservationIgnored private let schedule: Schedule
    @ObservationIgnored private var monitor: Task<Void, Never>?

    nonisolated var id: String { name }
    var isLocal: Bool { name == ServerClient.localID }

    /// Creates a connection that checks the server through its connector.
    convenience init(name: String, connector: ServerConnector, schedule: Schedule = Schedule()) {
        let signal = ServerSignal()
        let client = ServerClient(serverID: name, connector: connector, unreachable: signal)
        self.init(client: client, catalogClient: client, schedule: schedule) {
            try await connector.handshake(serverID: name).identity
        }
        signal.connect { [weak self] in
            Task { @MainActor in self?.operationFailed() }
        }
    }

    /// Creates a connection with an explicit handshake, as tests do.
    init(client: ServerClient, catalogClient: (any SessionClient)? = nil, schedule: Schedule = Schedule(),
         handshake: @escaping Handshake) {
        name = client.serverID
        connector = client.connector
        self.client = client
        catalog = SessionCatalog(client: catalogClient ?? client)
        self.schedule = schedule
        self.handshake = handshake
    }

    /// Starts checking the server, if it isn't already.
    func start() {
        guard monitor == nil else { return }
        monitor = Task { [weak self] in await self?.run() }
    }

    /// Stops checking the server.
    func stop() {
        monitor?.cancel()
        monitor = nil
    }

    /// Checks the server now, as Try Now and returning to the app do.
    func checkNow() {
        if case .unreachable(let reason, _) = status { status = .unreachable(reason: reason, retry: nil) }
        stop()
        start()
    }

    /// An operation couldn't reach a server believed connected; check it now
    /// rather than at the next scheduled check. A server already known to be
    /// unreachable keeps its backoff.
    private func operationFailed() {
        guard status.isConnected else { return }
        checkNow()
    }

    private func run() async {
        var delay = schedule.firstRetry
        while !Task.isCancelled {
            let wait: Duration
            do {
                let identity = try await handshake()
                guard !Task.isCancelled else { return }
                let restarted = status.identity?.instance != identity.instance
                status = .connected(identity)
                delay = schedule.firstRetry
                wait = schedule.check
                if restarted || !catalog.loaded {
                    let catalog = catalog
                    Task { await catalog.load() }
                }
            } catch {
                guard !Task.isCancelled, !(error is CancellationError) else { return }
                if case let ClientError.incompatibleDaemon(version, appProtocol, serverProtocol, reason) = error {
                    status = .incompatible(ServerMismatch(serverKitVersion: version, serverProtocol: serverProtocol,
                                                          appProtocol: appProtocol, reason: reason))
                    wait = schedule.check
                } else {
                    status = .unreachable(reason: error.localizedDescription, retry: Date.now.addingTimeInterval(delay.seconds))
                    wait = delay
                    delay = min(delay * 2, schedule.maximumRetry)
                }
            }
            do { try await Task.sleep(for: wait) } catch { return }
        }
    }
}

private extension Duration {
    var seconds: TimeInterval {
        let parts = components
        return TimeInterval(parts.seconds) + TimeInterval(parts.attoseconds) / 1e18
    }
}

/// Every server the app knows: this app's own Kit home's server, then the
/// saved servers it can reach, in name order. Each has one connection that
/// every window shares.
@MainActor @Observable
final class ServerDirectory {
    private(set) var connections: [ServerConnection]
    /// The app's own Kit home.
    let home: URL
    @ObservationIgnored private let saved: () -> [SavedServer]
    @ObservationIgnored private let makeConnection: (String, ServerConnector) -> ServerConnection
    @ObservationIgnored private var started = false

    /// Reads the saved servers of `home` and connects to them through their connectors.
    convenience init(home: URL = DevHomeResolver.resolve()) {
        let list = home.appendingPathComponent("servers.json")
        self.init(home: home, saved: { (try? SavedServerList.read(list)) ?? [] }) { name, connector in
            ServerConnection(name: name, connector: connector)
        }
    }

    /// Creates a directory from explicit parts, as tests do.
    init(home: URL, saved: @escaping () -> [SavedServer],
         makeConnection: @escaping (String, ServerConnector) -> ServerConnection) {
        self.home = URL(fileURLWithPath: home.path).standardizedFileURL
        self.saved = saved
        self.makeConnection = makeConnection
        connections = [makeConnection(ServerClient.localID, .home(path: self.home.path))]
        connections += Self.reachable(saved(), home: self.home).map { makeConnection($0.name, $0.connector) }
    }

    /// A directory that knows only an idle local server, for views and models
    /// created without the app's directory.
    static func inert() -> ServerDirectory {
        ServerDirectory(home: DevHomeResolver.resolve(), saved: { [] }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector)) { throw ClientError.noDaemon }
        }
    }

    var local: ServerConnection { connections[0] }

    func connection(named name: String) -> ServerConnection? {
        connections.first { $0.name == name }
    }

    /// The client of the named server, for operations that pick a server.
    func client(for name: String) -> ServerClient? {
        connection(named: name)?.client
    }

    /// Starts checking every server.
    func start() {
        started = true
        for connection in connections { connection.start() }
    }

    /// Reads the saved servers again: new ones connect, removed ones
    /// disconnect, and changed ones reconnect. Then every server is checked
    /// now, as when the app becomes active.
    func refresh() {
        var next = [local]
        for entry in Self.reachable(saved(), home: home) {
            if let existing = connection(named: entry.name), existing.connector == entry.connector {
                next.append(existing)
            } else {
                let connection = makeConnection(entry.name, entry.connector)
                if started { connection.start() }
                next.append(connection)
            }
        }
        let kept = Set(next.map(ObjectIdentifier.init))
        for connection in connections where !kept.contains(ObjectIdentifier(connection)) { connection.stop() }
        if next.map(ObjectIdentifier.init) != connections.map(ObjectIdentifier.init) { connections = next }
        if started { for connection in connections { connection.checkNow() } }
    }

    /// The saved servers this app can reach. Served and SSH servers arrive
    /// with their connectors; a saved home that is the app's own is `local`.
    private static func reachable(_ servers: [SavedServer], home: URL) -> [(name: String, connector: ServerConnector)] {
        servers.compactMap { server in
            guard server.kind == .home else { return nil }
            let connector = ServerConnector.home(path: server.address)
            guard connector != .home(path: home.path) else { return nil }
            return (server.name, connector)
        }
    }
}
