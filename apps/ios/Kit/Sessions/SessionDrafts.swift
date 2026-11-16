import Foundation
import KitClient
import KitDictation

/// What's typed or dictated in each session's composer, kept on this iPhone
/// so a draft is there when the session opens again, as on the Mac. Only
/// the text is kept; attachments and comments stay with the screen.
@MainActor
final class SessionDrafts {
    private let defaults: UserDefaults
    private static let key = "sessions.drafts"
    /// Each session's draft, by `server␟session`.
    private var drafts: [String: String]
    /// Sessions a listing has shown since launch. A draft for one that
    /// hasn't been listed yet, such as one just started, isn't let go when
    /// a listing that began before it misses it.
    private var listedSessions: Set<String> = []
    /// Drafts saved since launch for sessions no listing has shown yet.
    private var unlisted: Set<String> = []
    private var writeScheduled = false

    /// How long typing settles before drafts are written.
    static let writeDelay: Duration = .milliseconds(500)

    init(defaults: UserDefaults = SharedStorage.defaults) {
        self.defaults = defaults
        drafts = defaults.dictionary(forKey: Self.key) as? [String: String] ?? [:]
    }

    private static func key(_ id: SessionIdentity) -> String { id.server + "\u{1F}" + id.session }

    func text(for id: SessionIdentity) -> String {
        drafts[Self.key(id)] ?? ""
    }

    /// Keeps the session's draft; an empty one is forgotten. Writing waits
    /// for typing to settle.
    func save(_ text: String, for id: SessionIdentity) {
        let key = Self.key(id)
        let kept = text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : text
        guard drafts[key] != kept else { return }
        drafts[key] = kept
        if kept != nil, !listedSessions.contains(key) { unlisted.insert(key) }
        scheduleWrite()
    }

    /// Adds words after the session's draft.
    func append(_ words: String, for id: SessionIdentity) {
        let text = text(for: id)
        save(text.isEmpty ? words : DictationText.join(text, words), for: id)
    }

    /// Forgets drafts for a server's sessions it no longer lists, except
    /// ones saved since launch for sessions no listing has shown yet.
    func listed(_ server: String, sessions: Set<String>) {
        let prefix = server + "\u{1F}"
        let current = Set(sessions.map { prefix + $0 })
        listedSessions.formUnion(current)
        unlisted.subtract(current)
        let kept = drafts.filter { !$0.key.hasPrefix(prefix) || current.contains($0.key) || unlisted.contains($0.key) }
        guard kept.count != drafts.count else { return }
        drafts = kept
        scheduleWrite()
    }

    /// Forgets a removed server's drafts.
    func forget(_ server: String) {
        let prefix = server + "\u{1F}"
        let kept = drafts.filter { !$0.key.hasPrefix(prefix) }
        guard kept.count != drafts.count else { return }
        drafts = kept
        write()
    }

    #if DEBUG
    /// Forgets every draft, for UI tests.
    func forgetAll() {
        drafts = [:]
        write()
    }
    #endif

    /// Writes the drafts now, as when the app is left.
    func write() {
        writeScheduled = false
        if drafts.isEmpty { defaults.removeObject(forKey: Self.key) } else { defaults.set(drafts, forKey: Self.key) }
    }

    private func scheduleWrite() {
        guard !writeScheduled else { return }
        writeScheduled = true
        Task { [weak self] in
            try? await Task.sleep(for: Self.writeDelay)
            guard let self, self.writeScheduled else { return }
            self.write()
        }
    }
}
