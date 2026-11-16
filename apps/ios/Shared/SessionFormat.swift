import Foundation

/// How the Sessions list writes times and places.
enum SessionFormat {
    /// How long ago a session was active: "now", "4m", "2h", "Yesterday",
    /// a weekday, then a date.
    static func age(_ date: Date, now: Date, calendar: Calendar = .current) -> String {
        let seconds = max(0, now.timeIntervalSince(date))
        if seconds < 60 { return "now" }
        if seconds < 3600 { return "\(Int(seconds / 60))m" }
        let days = calendar.dateComponents([.day], from: calendar.startOfDay(for: date),
                                           to: calendar.startOfDay(for: now)).day ?? 0
        if days == 0 { return "\(Int(seconds / 3600))h" }
        if days == 1 { return "Yesterday" }
        if days < 7 { return date.formatted(.dateTime.weekday(.abbreviated)) }
        let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
        return date.formatted(sameYear ? .dateTime.month(.abbreviated).day() : .dateTime.month(.abbreviated).day().year())
    }

    /// A working directory with the server user's home written `~`. The app
    /// doesn't know the server's home, so it recognizes the usual ones.
    static func directory(_ path: String) -> String {
        let parts = path.split(separator: "/", omittingEmptySubsequences: true)
        if path == "/root" || path.hasPrefix("/root/") { return "~" + path.dropFirst("/root".count) }
        guard parts.count >= 2, ["Users", "home"].contains(parts[0]) else { return path }
        let rest = parts.dropFirst(2).joined(separator: "/")
        return rest.isEmpty ? "~" : "~/" + rest
    }

    /// A server timestamp.
    static func date(_ value: String) -> Date? {
        let parser = ISO8601DateFormatter()
        parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return parser.date(from: value) ?? ISO8601DateFormatter().date(from: value)
    }
}
