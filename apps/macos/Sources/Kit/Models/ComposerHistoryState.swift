import Foundation
import Observation

@MainActor @Observable final class ComposerHistoryState {
    enum Mode { case messages, bash }
    enum Entry: Identifiable, Equatable {
        case message(ComposerMessageHistoryEntry)
        case bash(ComposerBashHistoryEntry)
        var id: String { switch self { case .message(let value): value.id; case .bash(let value): value.id } }
        var text: String { switch self { case .message(let value): value.text; case .bash(let value): value.command } }
    }

    private(set) var mode: Mode?
    private(set) var loading = false
    private(set) var error: String?
    private(set) var query = ""
    private(set) var entries: [Entry] = []
    private(set) var selection = 0
    private(set) var hasMore = false
    private var nextCursor: String?
    private var identity = ""
    private var task: Task<Void, Never>?
    var isOpen: Bool { mode != nil }
    var matches: [Entry] {
        entries.filter { query.isEmpty || $0.text.localizedCaseInsensitiveContains(query) }
    }
    var selected: Entry? { matches.indices.contains(selection) ? matches[selection] : nil }
    var visibleMatches: [Entry] {
        let first = max(0, selection - 5)
        return Array(matches.dropFirst(first).prefix(10))
    }

    func reset(identity: String) {
        guard self.identity != identity else { return }
        self.identity = identity
        close()
    }
    func close() {
        guard mode != nil || task != nil || loading || error != nil else { return }
        task?.cancel(); task = nil; mode = nil; loading = false; error = nil
        query = ""; entries = []; selection = 0; hasMore = false; nextCursor = nil
    }
    func openMessages(session: String, client: (any ComposerHistoryClient)?) {
        guard mode == nil, client != nil else { return }
        mode = .messages; error = nil; query = ""; entries = []; selection = 0
        loadOlderMessages(session: session, client: client)
    }
    func openBash(session: String, query: String, client: (any ComposerHistoryClient)?) {
        guard let client else { return }
        if mode == .bash { setQuery(query); return }
        close(); mode = .bash; self.query = query; loading = true
        task = Task { [weak self] in
            do {
                let page = try await client.bashHistory(session, before: nil, limit: 10)
                guard !Task.isCancelled, let self, self.mode == .bash else { return }
                self.entries = page.entries.map(Entry.bash); self.hasMore = page.hasMore
                self.nextCursor = page.nextCursor; self.loading = false; self.task = nil;
                if self.matches.isEmpty, self.hasMore { self.loadOlderBash(session: session, client: client) }
            } catch {
                guard !Task.isCancelled, let self, self.mode == .bash else { return }
                self.loading = false; self.error = error.localizedDescription; self.task = nil;
            }
        }
    }
    func setQuery(_ value: String) {
        guard query != value else { return }
        query = value; selection = 0
    }
    func appendQuery(_ value: String) { setQuery(query + value) }
    func deleteQueryBackward() { if !query.isEmpty { setQuery(String(query.dropLast())) } }
    func move(_ offset: Int, session: String, client: (any ComposerHistoryClient)?) {
        guard !matches.isEmpty else {
            if hasMore, !loading { loadOlder(session: session, client: client) }
            return
        }
        if offset > 0, selection == matches.count - 1, hasMore {
            if !loading { loadOlder(session: session, client: client, advancingFrom: matches[selection].id) }
            return
        }
        selection = (selection + offset + matches.count) % matches.count
    }
    func retry(session: String, client: (any ComposerHistoryClient)?) {
        let mode = mode, query = query
        close()
        if mode == .messages { openMessages(session: session, client: client) }
        else if mode == .bash { openBash(session: session, query: query, client: client) }
    }

    private func loadOlder(session: String, client: (any ComposerHistoryClient)?, advancingFrom anchorID: String? = nil) {
        if mode == .messages { loadOlderMessages(session: session, client: client, advancingFrom: anchorID) }
        else if mode == .bash { loadOlderBash(session: session, client: client, advancingFrom: anchorID) }
    }

    private func loadOlderMessages(session: String, client: (any ComposerHistoryClient)?, advancingFrom anchorID: String? = nil) {
        guard let client else { return }
        let cursor = nextCursor
        let requestedQuery = query
        loading = true
        task = Task { [weak self] in
            do {
                let page = try await client.messageHistory(session, before: cursor)
                guard !Task.isCancelled, let self, self.mode == .messages else { return }
                var seen = Set(self.entries.map(\.text))
                self.entries += page.entries.filter { seen.insert($0.text).inserted }.map(Entry.message)
                self.hasMore = page.hasMore; self.nextCursor = page.nextCursor
                self.loading = false; self.task = nil
                if self.query == requestedQuery, let anchorID,
                   let anchor = self.matches.firstIndex(where: { $0.id == anchorID }),
                   anchor + 1 < self.matches.count { self.selection = anchor + 1 }
                if self.matches.isEmpty, self.hasMore { self.loadOlderMessages(session: session, client: client) }
            } catch {
                guard !Task.isCancelled, let self, self.mode == .messages else { return }
                self.loading = false; self.error = error.localizedDescription; self.task = nil
            }
        }
    }

    private func loadOlderBash(session: String, client: (any ComposerHistoryClient)?, advancingFrom anchorID: String? = nil) {
        guard let client, let cursor = nextCursor else { hasMore = false; return }
        let requestedQuery = query
        loading = true
        task = Task { [weak self] in
            do {
                let page = try await client.bashHistory(session, before: cursor, limit: 100)
                guard !Task.isCancelled, let self, self.mode == .bash else { return }
                let known = Set(self.entries.map(\.id))
                self.entries += page.entries.filter { !known.contains($0.id) }.map(Entry.bash)
                self.hasMore = page.hasMore; self.nextCursor = page.nextCursor
                self.loading = false; self.task = nil
                if self.query == requestedQuery, let anchorID,
                   let anchor = self.matches.firstIndex(where: { $0.id == anchorID }),
                   anchor + 1 < self.matches.count {
                    self.selection = anchor + 1
                }

                if self.matches.isEmpty, self.hasMore { self.loadOlderBash(session: session, client: client) }
            } catch {
                guard !Task.isCancelled, let self, self.mode == .bash else { return }
                self.loading = false; self.hasMore = false; self.error = error.localizedDescription; self.task = nil;
            }
        }
    }
}
