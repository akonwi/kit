import Foundation
import KitClient

/// The delete_session tool returns the same bounded JSON envelope in its text and details.
public struct DeletedSessionsPresentation: Sendable {
    private struct Response: Decodable {
        struct Session: Decodable {
            let id: String
            let name: String?
        }
        struct Failure: Decodable {
            let id: String
            let name: String?
            let error: String
        }
        let deleted: [Session]?
        let failed: [Failure]?
        let declined: Bool?
        let message: String?
        let error: String?
    }

    /// A session the call did not delete, and why.
    public struct Failure: Sendable, Equatable {
        public let name: String
        public let reason: String

        public init(name: String, reason: String) {
            self.name = name
            self.reason = reason
        }
    }

    public let title: String
    /// The deleted sessions' names, deepest first.
    public let deleted: [String]
    public let failures: [Failure]
    public let declined: Bool
    /// The result's message or error, if it has one.
    public let result: String?
    public let failed: Bool
    private let requested: Int

    public init(_ tool: ToolActivity) {
        let args = tool.arguments.flatMap { $0.data(using: .utf8) }
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
        requested = (args["sessionIds"] as? [Any])?.count ?? 0
        title = requested == 1 ? "Delete session" : "Delete sessions"
        let response = tool.output.data(using: .utf8).flatMap { try? JSONDecoder().decode(Response.self, from: $0) }
        func name(_ value: String?) -> String {
            let words = (value ?? "").split(whereSeparator: \.isWhitespace).joined(separator: " ")
            return words.isEmpty ? "Untitled session" : words
        }
        func text(_ value: String?) -> String? {
            guard let value, !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
            return value
        }
        deleted = (response?.deleted ?? []).map { name($0.name) }
        // A session the call could not resolve has no name; its ID is all there is.
        failures = (response?.failed ?? []).map { Failure(name: text($0.name).map { name($0) } ?? $0.id, reason: $0.error) }
        declined = response?.declined == true
        failed = tool.failed || text(response?.error) != nil
        if let error = text(response?.error) {
            result = error
        } else if let message = text(response?.message) {
            result = message
        } else if tool.failed {
            // Plain-text transport errors are useful; malformed JSON is not a UI.
            let plain = tool.output.trimmingCharacters(in: .whitespacesAndNewlines)
            result = plain.isEmpty || plain.hasPrefix("{") || plain.hasPrefix("[") ? "Couldn’t delete sessions." : plain
        } else {
            result = nil
        }
    }

    /// The deleted sessions once the result names them, then whether the user
    /// declined, and otherwise how many sessions the call names.
    public var summary: String {
        if declined { return "declined" }
        if !deleted.isEmpty { return deleted.joined(separator: ", ") }
        if requested == 0 { return "" }
        return requested == 1 ? "1 session" : "\(requested) sessions"
    }
}
