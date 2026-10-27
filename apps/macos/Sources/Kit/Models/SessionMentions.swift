import Foundation

/// A session a `#` mention can reference: another session on the attached
/// session's server, as its row in the composer's mention list shows it.
struct SessionMention: Identifiable, Equatable, Sendable {
    let id: String
    let title: String
    /// The session's working directory on its server.
    let cwd: String
    /// The last component of `cwd`, shown in the row.
    let folder: String
    let lastActivity: Date?
    var status: SessionTabStatus = .idle

    /// The row's right column: what the session is doing, or how long ago it
    /// was active, as the session launcher shows it.
    func trailing(now: Date = .now) -> String {
        switch status {
        case .running: "running"
        case .awaitingResponse: "waiting"
        case .idle: lastActivity.map { SessionLauncher.age(of: $0, now: now) } ?? ""
        }
    }
}

/// Session mentions: `#[session:<id>]` in a message refers the model to
/// another Kit session on the same server, which it may consult with
/// `peer_session`. The token is plain message text on the wire, as the TUI
/// inserts it.
enum SessionMentions {
    /// The scheme of transcript links a mention renders as.
    static let linkScheme = "kit-session"

    /// The text a chosen session inserts, before its trailing space.
    static func token(_ id: String) -> String { "#[session:\(id)]" }

    /// The sessions a mention lists: every session but the attached one, most
    /// recently active first, with what each is doing.
    static func candidates(_ sessions: [SessionExcerpt], activity: [String: SessionTabStatus],
                           excluding attached: String) -> [SessionMention] {
        SessionLauncher.recent(sessions.filter { $0.id != attached }).map { session in
            let cwd = session.cwd ?? session.workspace
            return SessionMention(id: session.id, title: session.title, cwd: cwd,
                                  folder: session.workspace,
                                  lastActivity: SessionLauncher.timestamp(session.lastActivity ?? session.date),
                                  status: activity[session.id] ?? .idle)
        }
    }

    /// The candidates matching `query`, best first, ties in list order. As in
    /// the TUI, the title weighs most, then the session ID and full working
    /// directory; the directory as shown also matches. A blank query keeps
    /// every candidate.
    static func ranked(_ candidates: [SessionMention], query: String) -> [SessionMention] {
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !query.isEmpty else { return candidates }
        return candidates.enumerated().compactMap { position, session -> (SessionMention, Int, Int)? in
            let score = score(session, query: query)
            return score > 0 ? (session, score, position) : nil
        }
        .sorted { $0.1 == $1.1 ? $0.2 < $1.2 : $0.1 > $1.1 }
        .map(\.0)
    }

    private static func score(_ session: SessionMention, query: String) -> Int {
        PaletteCommand.score(PaletteCommand(id: session.id, name: session.title,
                                            description: WorkspaceLocation.homeRelative(session.cwd),
                                            searchTerms: [session.id, session.cwd]), query: query)
    }

    /// A mention found in text: its range, in UTF-16 offsets, and the session ID.
    struct Match: Equatable {
        let range: NSRange
        let id: String
    }

    private static let pattern = try! NSRegularExpression(pattern: #"#\[session:([^\]\s]+)\]"#)

    /// The mentions in `text`, in order.
    static func matches(in text: String) -> [Match] {
        let source = text as NSString
        return pattern.matches(in: text, range: NSRange(location: 0, length: source.length)).map {
            Match(range: $0.range, id: source.substring(with: $0.range(at: 1)))
        }
    }

    /// A link that opens the mentioned session.
    static func link(_ id: String) -> URL? {
        var components = URLComponents()
        components.scheme = linkScheme
        components.path = id
        return components.url
    }

    /// The session a mention link opens, or nil for any other URL.
    static func session(in url: URL) -> String? {
        guard url.scheme == linkScheme,
              let id = URLComponents(url: url, resolvingAgainstBaseURL: false)?.path, !id.isEmpty else { return nil }
        return id
    }

    /// `text` with each mention outside code marked as a link to its session.
    /// The token's characters stay, so the text still copies as written; the
    /// view names the session when it draws the link.
    static func linked(_ text: AttributedString) -> AttributedString {
        let plain = String(text.characters)
        guard plain.contains("#[session:") else { return text }
        var result = text
        for match in matches(in: plain) {
            guard let range = Range(match.range, in: plain), let url = link(match.id) else { continue }
            let start = plain.distance(from: plain.startIndex, to: range.lowerBound)
            let length = plain.distance(from: range.lowerBound, to: range.upperBound)
            let lower = result.characters.index(result.startIndex, offsetBy: start)
            let upper = result.characters.index(lower, offsetBy: length)
            let span = lower..<upper
            // Code keeps its raw text, and a token inside a link stays that link.
            guard !result[span].runs.contains(where: {
                $0.inlinePresentationIntent?.contains(.code) == true || $0.link != nil
            }) else { continue }
            result[span].link = url
        }
        return result
    }

    /// How a mention link reads: `#` and the session's title, or, for a
    /// session the app doesn't know, `#unknown session`.
    static func label(title: String?) -> String {
        "#" + (title ?? "unknown session")
    }
}
