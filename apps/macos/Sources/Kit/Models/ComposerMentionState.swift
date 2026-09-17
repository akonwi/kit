import Foundation
import Observation

/// Inline mention ranges use UTF-16 offsets to agree with the native text editor.
@MainActor @Observable
final class ComposerMentionState {
    private(set) var isOpen = false
    private(set) var range = NSRange(location: 0, length: 0)
    private(set) var query = ""
    private(set) var entries: [String] = []
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
    func close() {
        isOpen = false; loading = false; query = ""; selected = nil; matches = []
        generation += 1; revision += 1; task?.cancel(); task = nil
    }
    func reset() { close(); entries = []; indexNotice = nil }

    /// Match the TUI: a contiguous typed @ at a whitespace boundary opens the list.
    @discardableResult
    func observe(previous: String, next: String, pasted: Bool) -> Bool {
        let old = Array(previous.utf16), new = Array(next.utf16)
        var start = 0, oldEnd = old.count, newEnd = new.count
        while start < oldEnd && start < newEnd && old[start] == new[start] { start += 1 }
        while oldEnd > start && newEnd > start && old[oldEnd - 1] == new[newEnd - 1] { oldEnd -= 1; newEnd -= 1 }
        if isOpen {
            guard !pasted, start >= range.location, oldEnd == NSMaxRange(range) else { close(); return false }
            range.length += newEnd - oldEnd
            guard range.length > 0, NSMaxRange(range) <= new.count, new[range.location] == 64 else { close(); return false }
            query = String(decoding: new[(range.location + 1)..<NSMaxRange(range)], as: UTF16.self)
            guard !query.contains(where: { $0 == "@" || $0.isWhitespace }) else { close(); return false }
            filter(); return false
        }
        guard !pasted, newEnd - start == 1, start < new.count, new[start] == 64 else { return false }
        let prefix = String(decoding: new.prefix(start), as: UTF16.self)
        guard prefix.isEmpty || prefix.last?.isWhitespace == true else { return false }
        isOpen = true; range = NSRange(location: start, length: 1); query = ""; error = nil
        filter(); return true
    }
    func load(_ loader: @escaping @Sendable (Bool) async throws -> FileIndex, force: Bool = false) {
        guard isOpen else { return }
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
    private func filter() {
        matches = Self.filtered(query: query, paths: entries)
        selected = matches.first; revision += 1
    }
    func move(_ delta: Int) {
        guard !matches.isEmpty else { return }
        let index = matches.firstIndex(of: selected ?? "") ?? 0
        selected = matches[(index + delta + matches.count) % matches.count]; revision += 1
    }
    func replacement(in text: String, path: String? = nil) -> (range: NSRange, text: String)? {
        guard isOpen, let path = path ?? selected, matches.contains(path), NSMaxRange(range) <= text.utf16.count else { return nil }
        return (range, "@" + path + " ")
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
