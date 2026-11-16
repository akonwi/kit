import Foundation
import KitClient
import KitDictation

/// A session's kept draft: its words, and the attachments already uploaded
/// for it, by their IDs on the server.
struct KeptDraft: Equatable, Sendable {
    var text = ""
    var attachmentIDs: [String] = []

    var isEmpty: Bool { text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && attachmentIDs.isEmpty }
}

/// What's typed, dictated, or attached in each session's composer, kept on
/// this iPhone so a draft is there when the session opens again, as on the
/// Mac. Words and uploaded attachments are kept; comments and uploads still
/// on their way stay with the screen.
@MainActor
final class SessionDrafts {
    private let defaults: UserDefaults
    private static let key = "sessions.drafts"
    private static let receivedKey = "sessions.receivedShares"
    /// Each session's draft, by `server␟session`.
    private var drafts: [String: KeptDraft]
    /// Sessions a listing has shown since launch. A draft for one that
    /// hasn't been listed yet, such as one just started, isn't let go when
    /// a listing that began before it misses it.
    private var listedSessions: Set<String> = []
    /// Drafts saved since launch for sessions no listing has shown yet.
    private var unlisted: Set<String> = []
    private var writeScheduled = false
    /// Shares from other apps already in their drafts, by ID, written with
    /// the drafts so a share isn't added twice when its file outlives them.
    private(set) var receivedShares: Set<String>

    /// How long typing settles before drafts are written.
    static let writeDelay: Duration = .milliseconds(500)

    init(defaults: UserDefaults = SharedStorage.defaults) {
        self.defaults = defaults
        drafts = Self.read(defaults.dictionary(forKey: Self.key) ?? [:])
        receivedShares = Set(defaults.stringArray(forKey: Self.receivedKey) ?? [])
    }

    /// Drafts as written: a dictionary of text and attachment IDs, or just
    /// the text, as drafts were first kept.
    private static func read(_ stored: [String: Any]) -> [String: KeptDraft] {
        stored.compactMapValues { value in
            if let text = value as? String { return KeptDraft(text: text) }
            guard let fields = value as? [String: Any] else { return nil }
            return KeptDraft(text: fields["text"] as? String ?? "", attachmentIDs: fields["attachments"] as? [String] ?? [])
        }
    }

    private var stored: [String: Any] {
        drafts.mapValues { ["text": $0.text, "attachments": $0.attachmentIDs] as [String: Any] }
    }

    private static func key(_ id: SessionIdentity) -> String { id.server + "\u{1F}" + id.session }

    func draft(for id: SessionIdentity) -> KeptDraft {
        drafts[Self.key(id)] ?? KeptDraft()
    }

    func text(for id: SessionIdentity) -> String {
        draft(for: id).text
    }

    /// Keeps the session's draft; an empty one is forgotten. Writing waits
    /// for typing to settle.
    func save(_ draft: KeptDraft, for id: SessionIdentity) {
        let key = Self.key(id)
        let kept = draft.isEmpty ? nil : draft
        guard drafts[key] != kept else { return }
        drafts[key] = kept
        if kept != nil, !listedSessions.contains(key) { unlisted.insert(key) }
        scheduleWrite()
    }

    /// Keeps the session's draft words, with its attachments.
    func save(_ text: String, for id: SessionIdentity) {
        var kept = draft(for: id)
        kept.text = text
        save(kept, for: id)
    }

    /// Adds words after the session's draft.
    func append(_ words: String, for id: SessionIdentity) {
        let text = text(for: id)
        save(text.isEmpty ? words : DictationText.join(text, words), for: id)
    }

    /// Notes that a share is in its draft. It's written with the drafts.
    func received(share id: UUID) {
        receivedShares.insert(id.uuidString)
    }

    /// Forgets shares that no longer wait, keeping the ones still waiting.
    func keepReceived(_ waiting: Set<String>) {
        receivedShares.formIntersection(waiting)
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
        if drafts.isEmpty { defaults.removeObject(forKey: Self.key) } else { defaults.set(stored, forKey: Self.key) }
        if receivedShares.isEmpty { defaults.removeObject(forKey: Self.receivedKey) }
        else { defaults.set(receivedShares.sorted(), forKey: Self.receivedKey) }
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
