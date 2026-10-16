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

    /// Sessions that are working or awaiting a response lead, in recency
    /// order; the rest follow as recent sessions.
    func sections(in sessions: [SessionExcerpt], activity: [String: SessionTabStatus])
        -> (active: [SessionExcerpt], recent: [SessionExcerpt]) {
        let ordered = results(in: sessions)
        let active = ordered.filter { (activity[$0.id] ?? .idle) != .idle }
        let ids = Set(active.map(\.id))
        return (active, ordered.filter { !ids.contains($0.id) })
    }

    mutating func move(_ offset: Int, in sessions: [SessionExcerpt]) {
        guard !sessions.isEmpty else { highlightedID = nil; return }
        let current = sessions.firstIndex { $0.id == highlightedID } ?? (offset > 0 ? -1 : sessions.count)
        highlightedID = sessions[max(0, min(sessions.count - 1, current + offset))].id
    }

    /// How long ago a session was last active, as the launcher shows it.
    static func age(of date: Date, now: Date = .now, calendar: Calendar = .current) -> String {
        let seconds = max(0, now.timeIntervalSince(date))
        if seconds < 60 { return "just now" }
        if seconds < 3600 { return "\(Int(seconds / 60)) min ago" }
        if seconds < 86400 { return "\(Int(seconds / 3600)) hr ago" }
        let days = calendar.dateComponents([.day], from: calendar.startOfDay(for: date),
                                           to: calendar.startOfDay(for: now)).day ?? 0
        if days <= 1 { return "yesterday" }
        if days < 7 { return "\(days) days ago" }
        if days < 14 { return "last week" }
        if days < 60 { return "\(days / 7) weeks ago" }
        let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
        return date.formatted(sameYear ? .dateTime.month(.abbreviated).day() : .dateTime.month(.abbreviated).day().year())
    }

    static func timestamp(_ value: String) -> Date? {
        let parser = ISO8601DateFormatter()
        parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return parser.date(from: value) ?? ISO8601DateFormatter().date(from: value)
    }
}
