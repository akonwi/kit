import Foundation

/// The create_session tool returns the same bounded JSON envelope in its text and details.
struct CreatedSessionPresentation {
    private struct Response: Decodable {
        struct Session: Decodable {
            let id: String
            let cwd: String
            let name: String?
            let runId: String?
        }
        let session: Session?
        let error: String?
    }
    let name: String
    let directory: String
    let prompt: String?
    let sessionID: String?
    let result: String
    let failed: Bool

    init(_ tool: ToolActivity) {
        let args = tool.arguments.flatMap { $0.data(using: .utf8) }
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
        let response = tool.output.data(using: .utf8).flatMap { try? JSONDecoder().decode(Response.self, from: $0) }
        let session = response?.session
        func text(_ value: String?) -> String? {
            guard let value, !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
            return value
        }
        failed = tool.failed || text(response?.error) != nil
        name = text(session?.name) ?? text(args["name"] as? String) ?? "New session"
        directory = text(session?.cwd) ?? text(args["cwd"] as? String) ?? ""
        prompt = text(args["prompt"] as? String)
        let id = session?.id ?? ""
        let validID = id.hasPrefix("session_") && id.dropFirst(8).count == 32 &&
            id.dropFirst(8).allSatisfy { "0123456789abcdef".contains($0) }
        sessionID = !failed && validID ? id : nil
        if failed {
            // Plain-text transport errors are useful; malformed JSON is not a UI.
            let plain = tool.output.trimmingCharacters(in: .whitespacesAndNewlines)
            result = text(response?.error) ?? (plain.isEmpty || plain.hasPrefix("{") || plain.hasPrefix("[") ? "Couldn’t create session." : plain)
        } else if sessionID != nil {
            result = text(session?.runId) == nil ? "Session created" : "Session created · Initial prompt started"
        } else if tool.status == "Planned" || tool.status == "Running…" {
            result = "Creating session…"
        } else {
            result = "Creation result unavailable"
        }
    }

    var summary: String { directory.isEmpty ? name : name + " · " + directory }
}
