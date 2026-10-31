import Foundation

/// What a new session form needs to decide before and after it asks a server
/// to create a session.
public enum SessionCreation {
    /// A fresh session ID, chosen by the client so that a create whose
    /// answer was lost can be looked up again.
    public static func newID() -> String {
        "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
    }

    /// The absolute folder a typed path names on a server whose user's home
    /// is `home`: `~` is that home, and a relative path starts from it. Nil
    /// for an empty path.
    public static func folder(_ typed: String, home: String) -> String? {
        let path = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !path.isEmpty else { return nil }
        if path.hasPrefix("/") { return path }
        let base = home.hasSuffix("/") ? String(home.dropLast()) : home
        if path == "~" { return base.isEmpty ? "/" : base }
        let relative = path.hasPrefix("~/") ? String(path.dropFirst(2)) : path
        return base + "/" + relative
    }

    /// The thinking level a new session starts on: medium when the model
    /// offers it, otherwise its first level, or none.
    public static func defaultThinking(_ levels: [String]) -> String {
        levels.contains("medium") ? "medium" : levels.first ?? ""
    }

    /// Whether a failed create certainly didn't create the session: it was
    /// never sent, or the server refused it. Any other failure may have
    /// lost the server's answer to a create that happened.
    public static func definitelyRejected(_ error: any Error) -> Bool {
        if error is MutationNotSent { return true }
        if case ClientError.http(let code) = error { return [400, 401, 403, 404, 409, 422].contains(code) }
        return false
    }
}
