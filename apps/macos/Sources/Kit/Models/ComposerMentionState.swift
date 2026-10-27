import Foundation
import Observation

/// The open inline mention in the composer: `@` for a workspace file or `#`
/// for another session. Ranges use UTF-16 offsets to agree with the native
/// text editor.
@MainActor @Observable
final class ComposerMentionState {
    /// What a mention refers to, named by the character that opens it.
    enum Kind: Equatable {
        case file, session

        /// The UTF-16 code unit that opens the mention.
        var trigger: UInt16 {
            switch self {
            case .file: 64 // @
            case .session: 35 // #
            }
        }
    }

    private(set) var isOpen = false
    private(set) var kind = Kind.file
    private(set) var range = NSRange(location: 0, length: 0)
    private(set) var query = ""
    private(set) var entries: [String] = []
    /// The sessions the open session mention lists, once loaded.
    private(set) var sessions: [SessionMention] = []
    /// Keys of the matching entries, best first: paths, or session IDs.
    private(set) var matches: [String] = []
    private(set) var selected: String?
    private(set) var loading = false
    private(set) var error: String?
    private(set) var revision = 0
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var task: Task<Void, Never>?
    private(set) var indexNotice: String?
    deinit { task?.cancel() }

    var visibleMatches: [String] {
        let index = matches.firstIndex(of: selected ?? "") ?? 0
        let start = max(0, min(index - 5, matches.count - 10))
        return Array(matches.dropFirst(start).prefix(10))
    }

    /// The listed session with this ID.
    func session(_ id: String) -> SessionMention? { sessions.first { $0.id == id } }

    /// The single row shown while nothing matches.
    var emptyText: String {
        switch kind {
        case .file:
            return loading ? "Loading…" : error ?? "No results"
        case .session:
            if loading { return "Loading sessions…" }
            if let error { return "Couldn’t load sessions: \(error)" }
            return sessions.isEmpty ? "No other sessions" : "No results"
        }
    }

    func close() {
        isOpen = false; loading = false; query = ""; selected = nil; matches = []
        generation += 1; revision += 1; task?.cancel(); task = nil
    }
    func reset() { close(); entries = []; sessions = []; indexNotice = nil }

    /// Match the TUI: a contiguous typed `@` or `#` at a whitespace boundary
    /// opens the list, and typing after it is the query. Whitespace or the
    /// trigger in the query, a paste, an edit elsewhere, or removing the
    /// trigger closes it.
    @discardableResult
    func observe(previous: String, next: String, pasted: Bool) -> Bool {
        let old = Array(previous.utf16), new = Array(next.utf16)
        var start = 0, oldEnd = old.count, newEnd = new.count
        while start < oldEnd && start < newEnd && old[start] == new[start] { start += 1 }
        while oldEnd > start && newEnd > start && old[oldEnd - 1] == new[newEnd - 1] { oldEnd -= 1; newEnd -= 1 }
        if isOpen {
            let trigger = kind.trigger
            guard !pasted, start >= range.location, oldEnd == NSMaxRange(range) else { close(); return false }
            range.length += newEnd - oldEnd
            guard range.length > 0, NSMaxRange(range) <= new.count, new[range.location] == trigger else { close(); return false }
            query = String(decoding: new[(range.location + 1)..<NSMaxRange(range)], as: UTF16.self)
            guard !query.utf16.contains(trigger), !query.contains(where: \.isWhitespace) else { close(); return false }
            filter(); return false
        }
        guard !pasted, newEnd - start == 1, start < new.count,
              let opened = [Kind.file, .session].first(where: { $0.trigger == new[start] }) else { return false }
        let prefix = String(decoding: new.prefix(start), as: UTF16.self)
        guard prefix.isEmpty || prefix.last?.isWhitespace == true else { return false }
        isOpen = true; kind = opened; range = NSRange(location: start, length: 1); query = ""; error = nil
        // Each session mention lists the sessions afresh, as the TUI does.
        if opened == .session { sessions = [] }
        filter(); return true
    }
    func load(_ loader: @escaping @Sendable (Bool) async throws -> FileIndex, force: Bool = false) {
        guard isOpen, kind == .file else { return }
        task?.cancel(); generation += 1
        let token = generation
        loading = true; error = nil; revision += 1
        task = Task { [weak self] in
            do {
                let index = try await loader(force)
                try Task.checkCancellation()
                guard let self, self.isOpen, self.generation == token else { return }
                self.entries = index.paths; self.indexNotice = index.notice; self.loading = false; self.filter()
            } catch is CancellationError {} catch {
                guard let self, self.isOpen, self.generation == token else { return }
                self.loading = false; self.error = error.localizedDescription; self.revision += 1
            }
        }
    }
    /// Lists the other sessions for an open session mention, replacing an
    /// earlier list or load.
    func loadSessions(_ loader: @escaping @Sendable () async throws -> [SessionMention]) {
        guard isOpen, kind == .session else { return }
        task?.cancel(); generation += 1
        let token = generation
        loading = true; error = nil; sessions = []; filter()
        task = Task { [weak self] in
            do {
                let sessions = try await loader()
                try Task.checkCancellation()
                guard let self, self.isOpen, self.kind == .session, self.generation == token else { return }
                self.sessions = sessions; self.loading = false; self.filter()
            } catch is CancellationError {} catch {
                guard let self, self.isOpen, self.kind == .session, self.generation == token else { return }
                self.loading = false; self.error = error.localizedDescription; self.revision += 1
            }
        }
    }
    private func filter() {
        switch kind {
        case .file: matches = Self.filtered(query: query, paths: entries)
        case .session: matches = SessionMentions.ranked(sessions, query: query).map(\.id)
        }
        selected = matches.first; revision += 1
    }
    func move(_ delta: Int) {
        guard !matches.isEmpty else { return }
        let index = matches.firstIndex(of: selected ?? "") ?? 0
        selected = matches[(index + delta + matches.count) % matches.count]; revision += 1
    }
    /// The open mention's range and the text that replaces it: `@path ` or
    /// `#[session:<id>] `, for `key` or the selected match.
    func replacement(in text: String, key: String? = nil) -> (range: NSRange, text: String)? {
        guard isOpen, let key = key ?? selected, matches.contains(key), NSMaxRange(range) <= text.utf16.count else { return nil }
        switch kind {
        case .file: return (range, "@" + key + " ")
        case .session: return (range, SessionMentions.token(key) + " ")
        }
    }

    /// Same ranking tiers and tie order as vaxis DefaultFuzzySelectFilter.
    nonisolated static func filtered(query: String, paths: [String]) -> [String] {
        let query = query.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
        guard !query.isEmpty else { return paths }
        return paths.enumerated().compactMap { index, path -> (String, Int, Int)? in
            guard let score = score(path.lowercased(), query) else { return nil }
            return (path, score, index)
        }.sorted { $0.1 == $1.1 ? $0.2 < $1.2 : $0.1 > $1.1 }.map(\.0)
    }
    nonisolated private static func score(_ candidate: String, _ query: String) -> Int? {
        if candidate == query { return 1000 }
        if candidate.hasPrefix(query) { return 900 + query.utf8.count * 4 }
        let text = Array(candidate), needle = Array(query)
        func boundary(_ index: Int) -> Bool { index == 0 || " -_:/ .+#@\t".contains(text[index - 1]) }
        if let range = candidate.range(of: query) {
            let index = candidate.distance(from: candidate.startIndex, to: range.lowerBound)
            return 800 + query.utf8.count * 4 - index + (boundary(index) ? 50 : 0)
        }
        var q = 0, start = -1, last = -1, consecutive = 0, boundaries = 0
        for (i, character) in text.enumerated() where character == needle[q] {
            if start == -1 { start = i }
            if last == i - 1 { consecutive += 1 }
            if boundary(i) { boundaries += 1 }
            last = i; q += 1
            if q == needle.count { return 500 + needle.count * 8 + consecutive * 12 + boundaries * 10 - start * 2 - (last - start + 1) }
        }
        return nil
    }
}
