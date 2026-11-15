import Foundation
import KitClient
import KitDictation

/// What's typed or dictated in each session's composer, kept on this iPhone
/// so a draft is there when the session opens again, as on the Mac. Only
/// the text is kept; attachments and comments stay with the screen.
final class SessionDrafts {
    private let defaults: UserDefaults
    private static let key = "sessions.drafts"
    /// Each session's draft, by `server␟session`.
    private var drafts: [String: String]

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        drafts = defaults.dictionary(forKey: Self.key) as? [String: String] ?? [:]
    }

    private static func key(_ id: SessionIdentity) -> String { id.server + "\u{1F}" + id.session }

    func text(for id: SessionIdentity) -> String {
        drafts[Self.key(id)] ?? ""
    }

    /// Keeps the session's draft; an empty one is forgotten.
    func save(_ text: String, for id: SessionIdentity) {
        let key = Self.key(id)
        let kept = text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : text
        guard drafts[key] != kept else { return }
        drafts[key] = kept
        defaults.set(drafts, forKey: Self.key)
    }

    /// Adds words after the session's draft.
    func append(_ words: String, for id: SessionIdentity) {
        let text = text(for: id)
        save(text.isEmpty ? words : DictationText.join(text, words), for: id)
    }

    /// Forgets drafts for a server's sessions it no longer lists.
    func listed(_ server: String, sessions: Set<String>) {
        let prefix = server + "\u{1F}"
        let kept = drafts.filter { !$0.key.hasPrefix(prefix) || sessions.contains(String($0.key.dropFirst(prefix.count))) }
        guard kept.count != drafts.count else { return }
        drafts = kept
        defaults.set(drafts, forKey: Self.key)
    }

    #if DEBUG
    /// Forgets every draft, for UI tests.
    func forgetAll() {
        drafts = [:]
        defaults.removeObject(forKey: Self.key)
    }
    #endif

    /// Forgets a removed server's drafts.
    func forget(_ server: String) {
        let prefix = server + "\u{1F}"
        let kept = drafts.filter { !$0.key.hasPrefix(prefix) }
        guard kept.count != drafts.count else { return }
        drafts = kept
        defaults.set(drafts, forKey: Self.key)
    }
}
