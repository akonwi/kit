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
    case noDaemon, invalidEndpoint, incompatible, invalidPayload, missingSession, http(Int), disconnected, oversized
    var errorDescription: String? {
        switch self {
        case .discovery(let reason): "Couldn’t read Kit’s server discovery files. \(reason)"
        case .noDaemon: "No running Kit v2 server found. Start it with the Kit v2 CLI, then retry."
        case .invalidEndpoint: "The server endpoint is invalid. Local discovery must use a loopback address."
        case .incompatible: "The server uses an incompatible protocol. Update Kit and the server together."
        case .invalidPayload: "The server returned an invalid session response."
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

/// Resolve discovery afresh for each attachment, including after daemon restart.
struct LocalClient: ScratchpadClient, TranscriptPagingClient, SubagentStreamingClient {
    func scratchpad(session: String) async throws -> ScratchpadRecord {
        try await HTTPClient.local().scratchpad(session: session)
    }
    func updateScratchpad(session: String, content: String, expectedRevision: Int64) async throws -> ScratchpadRecord {
        try await HTTPClient.local().updateScratchpad(session: session, content: content, expectedRevision: expectedRevision)
    }
    func subagentTranscript(session: String, conversation: String) async throws -> [TranscriptMessage] {
        try await HTTPClient.local().subagentTranscript(session: session, conversation: conversation)
    }
    func watchSubagent(session: String, conversation: String,
                       receive: @escaping @Sendable (SubagentTranscriptUpdate) async -> Void) async throws {
        try await HTTPClient.local().watchSubagent(session: session, conversation: conversation, receive: receive)
    }
    let serverID = "local-v2"
    let isDemo = false
    func sessions() async throws -> [SessionExcerpt] { try await HTTPClient.local().sessions() }
    func history(_ id: String, before: String) async throws -> TranscriptHistoryPage {
        try await HTTPClient.local().history(id, before: before)
    }
    func snapshot(_ id: String) async throws -> SessionExcerpt { try await HTTPClient.local().snapshot(id) }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        try await HTTPClient.local().watch(id, receive: receive)
    }
}
