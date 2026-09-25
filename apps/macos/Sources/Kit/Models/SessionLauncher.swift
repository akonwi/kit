import Foundation

/// Launcher selection is independent of the active session. Highlighting a row
/// never creates a session store or changes restoration intent.
struct SessionLauncher {
    var query = ""
    var highlightedID: String?

    func results(in sessions: [SessionExcerpt]) -> [SessionExcerpt] {
        let terms = query.split(whereSeparator: \.isWhitespace).map(String.init)
        return sessions.filter { session in
            let text = [session.title, session.cwd ?? session.workspace].joined(separator: " ")
            return terms.allSatisfy { text.localizedCaseInsensitiveContains($0) }
        }.sorted {
            let left = Self.timestamp($0.lastActivity ?? $0.date) ?? .distantPast
            let right = Self.timestamp($1.lastActivity ?? $1.date) ?? .distantPast
            if left != right { return left > right }
            return $0.id < $1.id
        }
    }

    mutating func move(_ offset: Int, in sessions: [SessionExcerpt]) {
        guard !sessions.isEmpty else { highlightedID = nil; return }
        let current = sessions.firstIndex { $0.id == highlightedID } ?? (offset > 0 ? -1 : sessions.count)
        highlightedID = sessions[max(0, min(sessions.count - 1, current + offset))].id
    }

    static func timestamp(_ value: String) -> Date? {
        let parser = ISO8601DateFormatter()
        parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return parser.date(from: value) ?? ISO8601DateFormatter().date(from: value)
    }
}
