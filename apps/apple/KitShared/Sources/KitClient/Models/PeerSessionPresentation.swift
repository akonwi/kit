import Foundation

/// Presentation of the recorded peer tool envelope, not live request tracking.
public struct PeerSessionPresentation {
    public struct Session: Decodable {
        public let id: String
        public let name: String
        public let cwd: String
        public let availability: String
        public var title: String { name.isEmpty ? id : name }

    public init(id: String, name: String, cwd: String, availability: String) {
        self.id = id
        self.name = name
        self.cwd = cwd
        self.availability = availability
    }
    }
    private struct Response: Decodable {
        struct Request: Decodable {
            let recipientSessionId: String
            let state: String
            let result: String?
            let error: String?
        }
        let action: String?
        let sessions: [Session]?
        let request: Request?
        let timedOut: Bool?
        let error: String?
    }
    public let action: String
    public let sessions: [Session]
    public let question: String?
    public let reply: String?
    public let error: String?
    public let recipientID: String?
    public let status: String

    public init(_ tool: ToolActivity) {
        let args = tool.arguments.flatMap { $0.data(using: .utf8) }
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
        let response = tool.output.data(using: .utf8).flatMap { try? JSONDecoder().decode(Response.self, from: $0) }
        func text(_ value: String?) -> String? {
            guard let value, !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
            return value
        }
        action = text(args["action"] as? String) ?? response?.action ?? ""
        sessions = response?.sessions ?? []
        question = action == "send" ? text(args["message"] as? String) : nil
        reply = text(response?.request?.result)
        let id = response?.request?.recipientSessionId ?? args["sessionId"] as? String
        recipientID = (action == "discover" ? nil : id).flatMap { Self.validSessionID($0) ? $0 : nil }
        let plain = tool.output.trimmingCharacters(in: .whitespacesAndNewlines)
        error = text(response?.error) ?? text(response?.request?.error) ?? (tool.failed ?
            (plain.isEmpty || plain.hasPrefix("{") || plain.hasPrefix("[") ? "Peer request failed." : plain) : nil)
        if error != nil {
            status = "Failed"
        } else if let request = response?.request {
            let state = Self.stateLabel(request.state)
            status = response?.timedOut == true ? "Wait timed out · \(state)" : state
        } else if tool.status == "Running…" || tool.status == "Planned" {
            status = "\(titleForAction(action))…"
        } else if action == "discover", response != nil {
            status = sessions.isEmpty ? "No peer sessions available" : "\(sessions.count) \(sessions.count == 1 ? "session" : "sessions")"
        } else {
            status = "Peer result unavailable"
        }
    }

    public var title: String { titleForAction(action) }
    public var summary: String { recipientID ?? status }
    public static func validSessionID(_ id: String) -> Bool {
        id.hasPrefix("session_") && id.dropFirst(8).count == 32 &&
            id.dropFirst(8).allSatisfy { "0123456789abcdef".contains($0) }
    }
    public static func stateLabel(_ state: String) -> String {
        switch state {
        case "queued": return "Queued"
        case "processing": return "Processing"
        case "completed": return "Completed"
        case "failed": return "Failed"
        case "aborted": return "Aborted"
        case "interrupted": return "Interrupted"
        case "recipient_archived": return "Recipient archived"
        case "recipient_unavailable": return "Recipient unavailable"
        default: return "Unknown request state"
        }
    }
}

private func titleForAction(_ action: String) -> String {
    switch action {
    case "discover": return "Find sessions"
    case "send": return "Ask session"
    case "inspect": return "Inspect peer request"
    case "wait": return "Wait for session"
    default: return "Peer session"
    }
}
