import Foundation

/// A client-hosted plugin instance a session watch carries (ADR 0051): the
/// instance, its declaration, and where its call events go.
struct ClientPluginHosting: Sendable {
    let instanceID: String
    let declaration: WireAttachClientPluginInput
    /// Receives call events from the stream bound to the instance.
    let receive: @Sendable (ClientPluginCallEvent) async -> Void
    /// Told when the server refuses the instance; the session is watched
    /// without it until the next connection.
    var refused: @Sendable (any Error) -> Void = { _ in }

    /// A new instance identifier: "clientplugin_" and 32 lowercase hex digits.
    static func newInstanceID() -> String {
        "clientplugin_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
    }

    static func validInstanceID(_ id: String) -> Bool { valid(id, prefix: "clientplugin_", lowercase: true) }
    static func validCallID(_ id: String) -> Bool { valid(id, prefix: "clientcall_", lowercase: false) }

    private static func valid(_ id: String, prefix: String, lowercase: Bool) -> Bool {
        guard id.hasPrefix(prefix) else { return false }
        let digits = id.dropFirst(prefix.count)
        return digits.count == 32 && digits.allSatisfy { $0.isHexDigit && (!lowercase || !$0.isUppercase) }
    }
}

/// A call event addressed to a hosted instance, validated on this side of
/// the boundary.
enum ClientPluginCallEvent: Sendable {
    /// The instance should run `call` and answer it once.
    case requested(id: String, call: ClientPluginCallRequest)
    /// The call ended; the instance should stop any work on it.
    case resolved(id: String, resolution: String)

    /// The event as one for `instance`, or nil when it is another kind,
    /// for another instance, or carries no call (as on a replay).
    init?(_ event: WireSessionEvent, instance: String) {
        guard let id = event.clientPluginCallId, ClientPluginHosting.validCallID(id),
              event.clientPluginInstanceId == instance else { return nil }
        switch event.kind {
        case .value31:
            guard let wire = event.clientPluginCall, let call = ClientPluginCallRequest(wire) else { return nil }
            self = .requested(id: id, call: call)
        case .value32:
            guard let resolution = event.clientPluginCallResolution,
                  ["completed", "failed", "timed_out", "withdrawn", "unavailable"].contains(resolution) else { return nil }
            self = .resolved(id: id, resolution: resolution)
        default:
            return nil
        }
    }
}

/// One tool call for a hosted instance.
struct ClientPluginCallRequest: Sendable {
    let pluginID: String
    let toolID: String
    let toolCallID: String
    /// The subagent conversation that made the call, if not the primary agent.
    let subagentConversationID: String?
    let input: WireJSON

    init?(_ wire: WireClientPluginCall) {
        guard wire.pluginId.range(of: "^[a-z][a-z0-9-]{0,31}$", options: .regularExpression) != nil,
              wire.toolId.range(of: "^[a-z][a-z0-9_]{0,63}$", options: .regularExpression) != nil,
              !wire.toolCallId.isEmpty, wire.toolCallId.utf8.count <= 256 else { return nil }
        pluginID = wire.pluginId
        toolID = wire.toolId
        toolCallID = wire.toolCallId
        subagentConversationID = wire.subagentConversationId.flatMap { $0.isEmpty ? nil : $0 }
        input = wire.input
    }

    init(pluginID: String, toolID: String, toolCallID: String = "call", subagentConversationID: String? = nil, input: WireJSON) {
        self.pluginID = pluginID
        self.toolID = toolID
        self.toolCallID = toolCallID
        self.subagentConversationID = subagentConversationID
        self.input = input
    }
}

/// A client that can host plugin instances on a session's watch.
protocol ClientPluginHostingClient: SessionClient {
    func watch(_ id: String, hosting: ClientPluginHosting?,
               receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws
    func detachClientPlugin(session id: String, instance: String) async throws
    func completeClientPluginCall(session id: String, instance: String, call: String,
                                  result: WireCompleteClientPluginCallInput) async throws
}

extension ServerClient: ClientPluginHostingClient {
    func watch(_ id: String, hosting: ClientPluginHosting?,
               receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        try await resolve().watch(id, hosting: hosting, receive: receive)
    }
    func detachClientPlugin(session id: String, instance: String) async throws {
        try await resolve().detachClientPlugin(session: id, instance: instance)
    }
    func completeClientPluginCall(session id: String, instance: String, call: String,
                                  result: WireCompleteClientPluginCallInput) async throws {
        try await resolve().completeClientPluginCall(session: id, instance: instance, call: call, result: result)
    }
}
