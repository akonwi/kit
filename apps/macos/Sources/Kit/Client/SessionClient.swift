import Foundation
import OpenAPIRuntime
import Synchronization

/// Endpoints and sessions have independent identities; session IDs are not global.
struct SessionIdentity: Hashable, Codable, Sendable {
    let server: String
    let session: String

    init(server: String, session: String) {
        self.server = server
        self.session = session
    }

    /// The name earlier builds saved for the local server.
    static let legacyLocalServer = "local-v2"

    private enum CodingKeys: String, CodingKey { case server, session }

    init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let server = try container.decode(String.self, forKey: .server)
        self.server = server == Self.legacyLocalServer ? ServerClient.localID : server
        session = try container.decode(String.self, forKey: .session)
    }
}

protocol SessionClient: Sendable {
    var serverID: String { get }
    var isDemo: Bool { get }
    func sessions() async throws -> [SessionExcerpt]
    func snapshot(_ id: String) async throws -> SessionExcerpt
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws
}

struct FixtureClient: SessionClient {
    let serverID = "demo"
    let isDemo = true
    let fixture: Fixture
    func sessions() async throws -> [SessionExcerpt] { fixture.sessions }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        guard let value = fixture.sessions.first(where: { $0.id == id }) else { throw ClientError.missingSession }
        return value
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(try await snapshot(id))
    }
}

enum ClientError: LocalizedError {
    case discovery(String)
    case noDaemon, invalidEndpoint, incompatible, invalidPayload, daemonNotReady, missingSession, http(Int), disconnected, oversized
    case incompatibleDaemon(daemonVersion: String, appProtocol: Int, daemonProtocol: Int,
                            reason: DaemonCompatibility.Mismatch)
    /// A served server needs this Mac to pair with it again.
    case needsPairing(PairingNeed)
    /// An SSH host needs an answer, such as a password, that only the user
    /// can give, so a background connection stopped (S9).
    case sshSignInRequired
    /// An SSH host's key no longer matches the one this Mac saved (S10).
    case sshHostKeyChanged(SSHHostKeyChange)
    /// An SSH host was reached, but its Kit server isn't running (S16).
    case sshServerStopped
    /// ssh, or the Kit on an SSH host, couldn't connect, saying why.
    case ssh(String)

    /// Why a served server needs pairing.
    enum PairingNeed: Equatable, Sendable {
        /// This Mac has no device token for the server.
        case missing
        /// The server says the device token expired.
        case expired
        /// The server refused the token, as when it was revoked.
        case refused
    }

    var errorDescription: String? {
        switch self {
        case .discovery(let reason): "Couldn’t read Kit’s server discovery files. \(reason)"
        case .noDaemon: "No running Kit server found. Start a compatible Kit server, then retry."
        case .invalidEndpoint: "The server endpoint is invalid. Local discovery must use a loopback address."
        case .incompatible: "The server registry is not supported by this app. Update to compatible releases."
        case .incompatibleDaemon(let daemon, let appProtocol, let daemonProtocol, let reason):
            "Kit app \(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev") (protocol \(appProtocol)) cannot attach to the running server \(daemon) (protocol \(daemonProtocol)): \(reason.rawValue). \(reason.recovery) The server was not changed."
        case .daemonNotReady: "The running Kit server's database is not ready. Retry when it is ready; the server was not changed."
        case .invalidPayload: "The server identity or response does not match local discovery."
        case .missingSession: "This session is no longer available."
        case .http(401), .http(403): "Server authentication failed. Reconnect to refresh credentials."
        case .http(let code): "Server request failed (HTTP \(code))."
        case .disconnected: "The server connection closed."
        case .oversized: "The server response exceeded the client limit."
        case .needsPairing(.missing): "This Mac isn’t paired with the server."
        case .needsPairing(.expired): "This Mac’s pairing with the server expired."
        case .needsPairing(.refused): "The server no longer accepts this Mac’s pairing."
        case .sshSignInRequired: "The SSH host needs you to sign in."
        case .sshHostKeyChanged(let change): "The host key for \(change.host) changed, so Kit stopped connecting."
        case .sshServerStopped: "The Kit server on the SSH host isn’t running."
        case .ssh(let reason): reason
        }
    }
}

/// Preserve arbitrary JSON without interpreting tool-specific payloads as UI state.
indirect enum WireJSON: Codable, Sendable {
    case object([String: WireJSON]), array([WireJSON]), string(String), number(Double), bool(Bool), null
    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let x = try? c.decode(Bool.self) { self = .bool(x) }
        else if let x = try? c.decode(String.self) { self = .string(x) }
        else if let x = try? c.decode(Double.self) { self = .number(x) }
        else if let x = try? c.decode([String: WireJSON].self) { self = .object(x) }
        else { self = .array(try c.decode([WireJSON].self)) }
    }
    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .object(let x): try c.encode(x)
        case .array(let x): try c.encode(x)
        case .string(let x): try c.encode(x)
        case .number(let x): try c.encode(x)
        case .bool(let x): try c.encode(x)
        case .null: try c.encodeNil()
        }
    }
}

/// How the app reaches a server. A connector resolves the server afresh for
/// every operation, so a restarted server is found again.
enum ServerConnector: Hashable, Sendable {
    /// The server of a Kit home on this Mac, found through its discovery files.
    case home(URL)
    /// A server served at an HTTP(S) origin, reached with the device token
    /// kept for `credential`.
    case url(String, credential: DeviceCredential)
    /// The server of a host's default Kit home, reached over SSH.
    case ssh(String)

    /// The server of the Kit home at `path`, normalized so equal paths are equal connectors.
    static func home(path: String) -> ServerConnector {
        .home(URL(fileURLWithPath: path).standardizedFileURL)
    }

    /// How the saved server `server` of the Kit home `home` is reached.
    static func saved(_ server: SavedServer, home: URL) -> ServerConnector {
        switch server.kind {
        case .home: .home(path: server.address)
        case .url: .url(server.address, credential: DeviceCredential(home: home.path, name: server.name))
        case .ssh: .ssh(server.address)
        }
    }

    /// Resolves and verifies the server, reporting who it is.
    func handshake(serverID: String, tokens: any DeviceTokenStore = DeviceTokens.standard,
                   hosts: SSHHosts = .shared) async throws -> (client: HTTPClient, identity: ServerIdentity) {
        switch self {
        case .home(let home):
            return try await HTTPClient.handshake(home: home, serverID: serverID)
        case .url(let origin, let credential):
            guard let url = URL(string: origin) else { throw ClientError.invalidEndpoint }
            guard let token = tokens.token(for: credential, url: origin) else { throw ClientError.needsPairing(.missing) }
            return try await HTTPClient.handshake(url: url, token: token, serverID: serverID)
        case .ssh(let destination):
            // Only the user's sign-in asks questions; this never does.
            let host = hosts.host(destination)
            let endpoint = try await host.resolve()
            do { return try await HTTPClient.handshake(tunnel: endpoint, serverID: serverID) }
            catch {
                // A forward to a server that changed or stopped answering is
                // dropped, so the next check finds the server again.
                if !isCancellation(error) { await host.invalidate(endpoint) }
                throw error
            }
        }
    }
}

/// Whether `error` means the operation was cancelled, as when its view went
/// away, rather than that it failed. Cancellation says nothing about the server.
func isCancellation(_ error: any Error) -> Bool {
    if Task.isCancelled || error is CancellationError || (error as? URLError)?.code == .cancelled { return true }
    if let wrapped = error as? OpenAPIRuntime.ClientError { return isCancellation(wrapped.underlyingError) }
    return false
}

/// Tells a server's connection that an operation couldn't reach the server,
/// so it checks the server now rather than at its next scheduled check.
final class ServerSignal: Sendable {
    private let action = Mutex<(@Sendable () -> Void)?>(nil)

    func connect(_ action: @escaping @Sendable () -> Void) {
        self.action.withLock { $0 = action }
    }

    func fire() {
        action.withLock { $0 }?()
    }
}

/// Where a server's clients reach it. Every client of one server shares its
/// address, so when the server's saved name is pointed elsewhere their next
/// operations go there too, and open sessions keep their drafts and shells.
final class ServerAddress: Sendable {
    private struct Verified {
        let connector: ServerConnector
        let client: HTTPClient
        let at: ContinuousClock.Instant
    }

    private let current: Mutex<ServerConnector>
    private let verified = Mutex<Verified?>(nil)
    /// How long a served server's verified transport is reused without
    /// verifying the server again, when nothing reports it changed sooner.
    static let verifiedLifetime: Duration = .seconds(60)

    init(_ connector: ServerConnector) {
        current = Mutex(connector)
    }

    var connector: ServerConnector { current.withLock { $0 } }

    func repoint(to connector: ServerConnector) {
        current.withLock { $0 = connector }
        verified.withLock { $0 = nil }
    }

    /// The transport last verified for `connector`, while it is fresh.
    func verifiedClient(for connector: ServerConnector) -> HTTPClient? {
        verified.withLock { entry in
            guard let entry, entry.connector == connector, ContinuousClock.now - entry.at < Self.verifiedLifetime else { return nil }
            return entry.client
        }
    }

    func remember(_ client: HTTPClient, for connector: ServerConnector) {
        verified.withLock { $0 = Verified(connector: connector, client: client, at: .now) }
    }

    /// Forgets the verified transport, or only `client` when given and still remembered.
    func forget(_ client: ObjectIdentifier? = nil) {
        verified.withLock { entry in
            if client == nil || entry.map({ ObjectIdentifier($0.client) }) == client { entry = nil }
        }
    }
}

/// A client for one server, named by `serverID`. It resolves the server
/// through its address for each operation, including after a restart.
struct ServerClient: ScratchpadClient, TranscriptPagingClient, SubagentStreamingClient {
    let serverID: String
    let address: ServerAddress
    let isDemo = false
    /// Fired when resolving the server fails.
    var unreachable: ServerSignal? = nil
    /// Where a served server's device token is kept.
    let tokens: any DeviceTokenStore
    /// The SSH hosts an SSH server is reached through.
    let hosts: SSHHosts

    init(serverID: String, connector: ServerConnector, tokens: any DeviceTokenStore = DeviceTokens.standard,
         hosts: SSHHosts = .shared, unreachable: ServerSignal? = nil) {
        self.serverID = serverID
        address = ServerAddress(connector)
        self.tokens = tokens
        self.hosts = hosts
        self.unreachable = unreachable
    }

    var connector: ServerConnector { address.connector }

    /// The name of the app's own Kit home's server.
    static let localID = SavedServer.localName

    /// The server of the app's own Kit home.
    static func local() -> ServerClient {
        ServerClient(serverID: localID, connector: .home(DevHomeResolver.resolve()))
    }

    /// Resolves the server and returns a transport for one operation. A
    /// served server's verified transport is reused, so its operations skip
    /// the Keychain and the handshake, until a request suggests the server
    /// changed, its connection checks it again, or the transport ages out.
    func resolve() async throws -> HTTPClient {
        let connector = self.connector
        if let client = address.verifiedClient(for: connector) { return client }
        do { return try await verify(connector).client }
        catch {
            if !isCancellation(error) { unreachable?.fire() }
            throw error
        }
    }

    /// Reaches and verifies the server afresh, as its connection's checks do.
    /// A served server's transport is kept for later operations; a failure
    /// forgets the one kept.
    func verify(_ connector: ServerConnector? = nil) async throws -> (client: HTTPClient, identity: ServerIdentity) {
        let connector = connector ?? self.connector
        let result: (client: HTTPClient, identity: ServerIdentity)
        do { result = try await connector.handshake(serverID: serverID, tokens: tokens, hosts: hosts) }
        catch {
            address.forget()
            throw error
        }
        // A Kit home's discovery files are cheap to read again; a served or
        // SSH server's verified transport is kept.
        if case .home = connector { return result }
        // The address keeps the transport, so the transport mustn't keep the address.
        let unreachable = self.unreachable, id = ObjectIdentifier(result.client)
        result.client.failures.connect { [weak address] failure in
            address?.forget(id)
            if failure != .changed { unreachable?.fire() }
        }
        address.remember(result.client, for: connector)
        return result
    }

    func scratchpad(session: String) async throws -> ScratchpadRecord {
        try await resolve().scratchpad(session: session)
    }
    func updateScratchpad(session: String, content: String, expectedRevision: Int64) async throws -> ScratchpadRecord {
        try await resolve().updateScratchpad(session: session, content: content, expectedRevision: expectedRevision)
    }
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] {
        try await resolve().subagentTranscript(session: session, conversation: conversation)
    }
    func watchSubagent(session: String, conversation: String,
                       receive: @escaping @Sendable (SubagentTranscriptUpdate) async -> Void) async throws {
        try await resolve().watchSubagent(session: session, conversation: conversation, receive: receive)
    }
    func sessions() async throws -> [SessionExcerpt] { try await resolve().sessions() }
    /// Issues a pairing code for another device.
    func createPairing() async throws -> PairingCode { try await resolve().createPairing() }
    func history(_ id: String, before: String) async throws -> TranscriptHistoryPage {
        try await resolve().history(id, before: before)
    }
    func snapshot(_ id: String) async throws -> SessionExcerpt { try await resolve().snapshot(id) }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        try await resolve().watch(id, receive: receive)
    }
}
