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
    /// A connected server missed one check. Its open sessions stay readable
    /// until the next check, which is at `retry`, also fails.
    case reconnecting(ServerIdentity, retry: Date)
    /// The server couldn't be reached or verified. The next check is at
    /// `retry`, or is underway when it is nil.
    case unreachable(reason: String, retry: Date?)
    /// The server speaks another protocol. It is checked again occasionally,
    /// so restarting it with a compatible Kit resumes its tabs.
    case incompatible(ServerMismatch)
    /// A served server needs this Mac to pair with it again. It is checked
    /// again occasionally, so pairing from a terminal resumes its tabs.
    case needsPairing(ClientError.PairingNeed)

    /// The server last verified, while connected or reconnecting.
    var identity: ServerIdentity? {
        switch self {
        case .connected(let identity), .reconnecting(let identity, _): identity
        case .connecting, .unreachable, .incompatible, .needsPairing: nil
        }
    }
    var isConnected: Bool {
        if case .connected = self { true } else { false }
    }
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

        /// The retry after one that waited `wait`.
        func retry(after wait: Duration) -> Duration { min(wait * 2, maximumRetry) }
    }

    typealias Handshake = @Sendable () async throws -> ServerIdentity

    /// The server's name: `local`, or its saved name.
    let name: String
    /// How the server is reached; see `repoint(to:)`.
    private(set) var connector: ServerConnector
    /// Counts repointings, so sessions attached by an earlier route reattach.
    private(set) var route = 0
    let client: ServerClient
    let catalog: SessionCatalog
    private(set) var status: ServerStatus = .connecting
    /// This Mac's Kit-scoped machine identifiers, which the directory sets
    /// (`LocalMachine.identifiers`); empty while unknown.
    var localMachines: Set<String> = []
    /// The machine identifier the server last verified. It is kept while the
    /// server can't be reached or verified, and forgotten when the server is
    /// reached another way.
    private(set) var machine: String?

    /// Whether the server runs on this Mac: the machine identifier it last
    /// verified is this Mac's. Nil until it verifies, or while this Mac's
    /// identifiers are unknown; features that need this Mac stay unavailable
    /// then. Never judged by the address.
    var isThisMac: Bool? {
        guard let machine, !localMachines.isEmpty else { return nil }
        return localMachines.contains(machine)
    }

    @ObservationIgnored private let handshake: Handshake
    /// A handshake is underway.
    @ObservationIgnored private var checking = false
    @ObservationIgnored private(set) var retired = false
    @ObservationIgnored private let schedule: Schedule
    @ObservationIgnored private var monitor: Task<Void, Never>?

    nonisolated var id: String { name }
    var isLocal: Bool { name == ServerClient.localID }

    /// Creates a connection that checks the server through its connector.
    convenience init(name: String, connector: ServerConnector, tokens: any DeviceTokenStore = DeviceTokens.standard,
                     schedule: Schedule = Schedule()) {
        let signal = ServerSignal()
        let client = ServerClient(serverID: name, connector: connector, tokens: tokens, unreachable: signal)
        self.init(client: client, catalogClient: client, schedule: schedule, signal: signal) {
            try await client.verify().identity
        }
    }

    /// Creates a connection with an explicit handshake, as tests do. Firing
    /// `signal` reports a failed operation.
    init(client: ServerClient, catalogClient: (any SessionClient)? = nil, schedule: Schedule = Schedule(),
         signal: ServerSignal? = nil, handshake: @escaping Handshake) {
        name = client.serverID
        connector = client.connector
        self.client = client
        catalog = SessionCatalog(client: catalogClient ?? client)
        self.schedule = schedule
        self.handshake = handshake
        signal?.connect { [weak self] in
            Task { @MainActor in self?.operationFailed() }
        }
    }

    /// Starts checking the server, if it isn't already and the connection
    /// hasn't been retired.
    func start() {
        guard monitor == nil, !retired else { return }
        monitor = Task { [weak self] in await self?.run() }
    }

    /// Stops checking the server.
    func stop() {
        monitor?.cancel()
        monitor = nil
        checking = false
    }

    /// Stops checking the server for good, as when it is no longer saved
    /// under this name or its connector changed. Operations of sessions still
    /// using it can't revive it.
    func retire() {
        retired = true
        stop()
        client.address.forget()
    }

    /// Reaches the server another way, as when its saved name is pointed at
    /// another home. Its sessions' clients follow, and it is checked afresh.
    func repoint(to connector: ServerConnector) {
        guard !retired, connector != self.connector else { return }
        client.address.repoint(to: connector)
        self.connector = connector
        route += 1
        status = .connecting
        machine = nil
        catalog.reset()
        if monitor != nil { checkNow() }
    }

    /// Checks the server now, as Try Now and returning to the app do.
    func checkNow() {
        guard !retired else { return }
        if case .unreachable(let reason, _) = status { status = .unreachable(reason: reason, retry: nil) }
        stop()
        start()
    }

    /// An operation couldn't reach a server believed connected; check it now
    /// rather than at the next scheduled check. A server already known to be
    /// unreachable keeps its backoff, and a check already underway answers
    /// for every operation that failed meanwhile. That check may have begun
    /// before the failure and still succeed; the next failed operation fires
    /// again, so the server is checked then.
    private func operationFailed() {
        guard status.isConnected, !checking else { return }
        checkNow()
    }

    private func run() async {
        var delay = schedule.firstRetry
        while !Task.isCancelled {
            let wait: Duration
            checking = true
            let result: Result<ServerIdentity, any Error>
            do { result = .success(try await handshake()) } catch { result = .failure(error) }
            guard !Task.isCancelled else { return }
            checking = false
            do {
                let identity = try result.get()
                let restarted = status.identity?.instance != identity.instance
                status = .connected(identity)
                if machine != identity.machine { machine = identity.machine }
                delay = schedule.firstRetry
                wait = schedule.check
                if restarted || !catalog.loaded {
                    let catalog = catalog
                    Task { await catalog.load() }
                }
            } catch {
                // A handshake that ends early, uncancelled, is a failure like
                // any other, so checks keep running.
                if case let ClientError.incompatibleDaemon(version, appProtocol, serverProtocol, reason) = error {
                    status = .incompatible(ServerMismatch(serverKitVersion: version, serverProtocol: serverProtocol,
                                                          appProtocol: appProtocol, reason: reason))
                    wait = schedule.check
                } else if case ClientError.needsPairing(let need) = error {
                    status = .needsPairing(need)
                    wait = schedule.check
                } else if case .connected(let identity) = status {
                    status = .reconnecting(identity, retry: Date.now.addingTimeInterval(delay.seconds))
                    wait = delay
                    delay = schedule.retry(after: delay)
                } else {
                    status = .unreachable(reason: error.localizedDescription, retry: Date.now.addingTimeInterval(delay.seconds))
                    wait = delay
                    delay = schedule.retry(after: delay)
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
    /// This Mac's Kit-scoped machine identifiers, given to every connection.
    private(set) var localMachines: Set<String>
    /// Reads this Mac's machine identifiers again, as on `refresh()`.
    @ObservationIgnored private let readLocalMachines: () -> Set<String>
    /// The saved servers, as last read, including those this app can't reach yet.
    private(set) var savedServers: [SavedServer] = []
    /// The saved-server list this directory changes.
    let list: URL
    /// Where served servers' device tokens are kept.
    @ObservationIgnored let tokens: any DeviceTokenStore
    /// Called after a saved server is renamed, so windows move its tabs.
    @ObservationIgnored var serverRenamed: (_ from: String, _ to: String) -> Void = { _, _ in }
    /// Called after a saved server is removed, so windows close its tabs.
    @ObservationIgnored var serverRemoved: (_ name: String) -> Void = { _ in }
    /// How many session tabs are open on a server across windows.
    @ObservationIgnored var openTabs: (_ name: String) -> Int = { _ in 0 }
    /// How many terminals in a server's tabs are running commands, across windows.
    @ObservationIgnored var busyTerminals: (_ name: String) -> Int = { _ in 0 }
    @ObservationIgnored private let saved: () -> [SavedServer]
    @ObservationIgnored private let makeConnection: (String, ServerConnector) -> ServerConnection
    @ObservationIgnored private var started = false
    /// The server the launcher last listed, which new launchers open on.
    var launcherServer: String {
        didSet { if launcherServer != oldValue { defaults?.set(launcherServer, forKey: Self.launcherServerKey) } }
    }
    /// Where the launcher's server is kept; none, as in tests, keeps it in memory.
    @ObservationIgnored private let defaults: UserDefaults?
    static let launcherServerKey = "launcherServer"

    /// Reads the saved servers of `home` and connects to them through their connectors.
    convenience init(home: URL = DevHomeResolver.resolve(), tokens: any DeviceTokenStore = DeviceTokens.standard) {
        let list = home.appendingPathComponent("servers.json")
        self.init(home: home, tokens: tokens, defaults: .standard, localMachines: { LocalMachine.identifiers(home: home) },
                  saved: { (try? SavedServerList.read(list)) ?? [] }) { name, connector in
            ServerConnection(name: name, connector: connector, tokens: tokens)
        }
    }

    /// Creates a directory from explicit parts, as tests do. Changes are
    /// written to `home`'s list, which `saved` should read.
    init(home: URL, tokens: any DeviceTokenStore = MemoryDeviceTokens(), defaults: UserDefaults? = nil,
         localMachines: @escaping () -> Set<String> = { [] }, saved: @escaping () -> [SavedServer],
         makeConnection: @escaping (String, ServerConnector) -> ServerConnection) {
        self.defaults = defaults
        readLocalMachines = localMachines
        self.localMachines = localMachines()
        launcherServer = defaults?.string(forKey: Self.launcherServerKey) ?? ServerClient.localID
        self.home = URL(fileURLWithPath: home.path).standardizedFileURL
        list = self.home.appendingPathComponent("servers.json")
        self.tokens = tokens
        self.saved = saved
        self.makeConnection = makeConnection
        let servers = saved()
        savedServers = servers
        connections = []
        connections = [connect(ServerClient.localID, .home(path: self.home.path))]
            + Self.reachable(servers, home: self.home).map { connect($0.name, $0.connector) }
    }

    /// A new connection to a server, which knows this Mac's machine.
    private func connect(_ name: String, _ connector: ServerConnector) -> ServerConnection {
        let connection = makeConnection(name, connector)
        connection.localMachines = localMachines
        return connection
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

    /// Whether the named server runs on this Mac; nil while it is unknown or
    /// unverified. See `ServerConnection.isThisMac`.
    func isThisMac(_ name: String) -> Bool? {
        connection(named: name)?.isThisMac
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

    /// Reads the saved servers again: new ones connect, removed ones retire,
    /// and changed ones are repointed. Then every server is checked now, as
    /// when the app becomes active.
    func refresh() {
        // A server may have written the home's machine value since launch.
        let machines = readLocalMachines()
        if machines != localMachines {
            localMachines = machines
            for connection in connections { connection.localMachines = machines }
        }
        let servers = saved()
        if servers != savedServers { savedServers = servers }
        var next = [local]
        for entry in Self.reachable(servers, home: home) {
            if let existing = connection(named: entry.name) {
                existing.repoint(to: entry.connector)
                next.append(existing)
            } else {
                let connection = connect(entry.name, entry.connector)
                if started { connection.start() }
                next.append(connection)
            }
        }
        let kept = Set(next.map(ObjectIdentifier.init))
        for connection in connections where !kept.contains(ObjectIdentifier(connection)) { connection.retire() }
        if next.map(ObjectIdentifier.init) != connections.map(ObjectIdentifier.init) { connections = next }
        if started { for connection in connections { connection.checkNow() } }
    }

    /// The saved servers, with how each is reached. A saved home that is
    /// the app's own is `local`, and a home saved under two names connects
    /// once, under the first.
    private static func reachable(_ servers: [SavedServer], home: URL) -> [(name: String, connector: ServerConnector)] {
        var seen: Set<ServerConnector> = [.home(path: home.path)]
        return servers.compactMap { server in
            let connector = ServerConnector.saved(server, home: home)
            if case .home = connector, !seen.insert(connector).inserted { return nil }
            return (server.name, connector)
        }
    }
}
