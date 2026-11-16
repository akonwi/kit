import Foundation
import KitClient

/// A session something can be shared to, as the share sheet lists it.
struct ShareDestination: Identifiable, Hashable, Sendable {
    let server: String
    let session: String
    let title: String
    /// Where it runs, with the server user's home written `~`.
    let folder: String
    /// When it was last active.
    let changed: Date?

    var id: String { server + "\u{1F}" + session }

    init(server: String, session: String, title: String, folder: String, changed: Date?) {
        self.server = server
        self.session = session
        self.title = title
        self.folder = folder
        self.changed = changed
    }

    init(server: String, excerpt: SessionExcerpt) {
        self.init(server: server, session: excerpt.id, title: excerpt.title,
                  folder: SessionFormat.directory(excerpt.cwd ?? excerpt.workspace),
                  changed: SessionFormat.date(excerpt.lastActivity ?? excerpt.date))
    }

    /// A server's sessions, from either kind of listing.
    static func listed(_ listing: AttentionListing, on server: String) -> [ShareDestination] {
        switch listing {
        case .ordered(let sessions): sessions.map { ShareDestination(server: server, excerpt: $0.session) }
        case .unordered(let sessions): sessions.map { ShareDestination(server: server, excerpt: $0) }
        }
    }

    /// Every server's sessions together, the most recently active first,
    /// since a share most often goes to a session just used.
    static func ordered(_ destinations: [ShareDestination]) -> [ShareDestination] {
        destinations.sorted {
            let first = $0.changed ?? .distantPast, second = $1.changed ?? .distantPast
            return first != second ? first > second : $0.id < $1.id
        }
    }

    /// Whether a search finds it: every word in its title, folder, or server.
    func matches(_ search: String) -> Bool {
        let words = search.split(whereSeparator: \.isWhitespace)
        guard !words.isEmpty else { return true }
        let haystack = "\(title) \(folder) \(server)"
        return words.allSatisfy { haystack.localizedStandardContains($0) }
    }
}
