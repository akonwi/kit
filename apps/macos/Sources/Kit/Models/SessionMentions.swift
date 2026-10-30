import Foundation
import KitTranscript

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

/// The composer's session mention list. The mention links themselves are
/// in KitTranscript, which both Apple clients share.
extension SessionMentions {
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
}
