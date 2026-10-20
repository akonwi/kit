import Foundation

/// Endpoints and sessions have independent identities; session IDs are not global.
struct SessionIdentity: Hashable, Codable, Sendable {
    let server: String
    let session: String
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
}

/// A client for one server, named by `serverID`. It resolves the server
/// through its connector for each operation, including after a restart.
struct ServerClient: ScratchpadClient, TranscriptPagingClient, SubagentStreamingClient {
    let serverID: String
    let connector: ServerConnector
    let isDemo = false

    /// The name of the app's own Kit home's server.
    static let localID = "local-v2"

    /// The server of the app's own Kit home.
    static func local() -> ServerClient {
        ServerClient(serverID: localID, connector: .home(DevHomeResolver.resolve()))
    }

    /// Resolves the server and returns a transport for one operation.
    func resolve() async throws -> HTTPClient {
        switch connector {
        case .home(let home): try await HTTPClient.discover(home: home, serverID: serverID)
        }
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
    func history(_ id: String, before: String) async throws -> TranscriptHistoryPage {
        try await resolve().history(id, before: before)
    }
    func snapshot(_ id: String) async throws -> SessionExcerpt { try await resolve().snapshot(id) }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        try await resolve().watch(id, receive: receive)
    }
}
