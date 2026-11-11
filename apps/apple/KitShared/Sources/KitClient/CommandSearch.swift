import Foundation

/// Ranks commands against what was typed, the way the TUI's palette does, so
/// every client lists the same command first for the same query.
public enum CommandSearch {
    /// The best match among the name (weighted highest), the description,
    /// and the search terms. Zero means no match. `query` is lowercased.
    public static func score(name: String, description: String, terms: [String] = [], query: String) -> Int {
        var best = 0
        let named = stringScore(name.lowercased(), query)
        if named > 0 { best = named + 40 }
        best = max(best, stringScore(description.lowercased(), query))
        for term in terms where !term.isEmpty {
            let alias = stringScore(term.lowercased(), query)
            if alias > 0 { best = max(best, alias + 15) }
        }
        return best
    }

    /// Ranks an exact match, then a prefix, then a substring (higher at a word
    /// boundary), then the query's characters in order with gaps.
    public static func stringScore(_ candidate: String, _ query: String) -> Int {
        let chars = Array(candidate), needle = Array(query)
        guard !needle.isEmpty else { return 0 }
        if candidate == query { return 1000 }
        if candidate.hasPrefix(query) { return 900 + needle.count * 4 }
        if let index = find(chars, needle) {
            return 800 + needle.count * 4 - index + (wordBoundary(chars, index) ? 50 : 0)
        }
        return subsequenceScore(chars, needle)
    }

    private static func find(_ chars: [Character], _ needle: [Character]) -> Int? {
        guard needle.count <= chars.count else { return nil }
        for start in 0...(chars.count - needle.count) where Array(chars[start..<start + needle.count]) == needle {
            return start
        }
        return nil
    }

    /// Rewards consecutive characters and word starts, and penalizes a late
    /// start and a wide span.
    private static func subsequenceScore(_ chars: [Character], _ needle: [Character]) -> Int {
        var matched = 0, start = -1, last = -1, consecutive = 0, boundaries = 0
        for (index, char) in chars.enumerated() where char == needle[matched] {
            if start == -1 { start = index }
            if last == index - 1 { consecutive += 1 }
            if wordBoundary(chars, index) { boundaries += 1 }
            last = index
            matched += 1
            if matched == needle.count {
                let span = last - start + 1
                let score = 500 + needle.count * 8 + consecutive * 12 + boundaries * 10 - start * 2 - span
                return max(0, score)
            }
        }
        return 0
    }

    private static func wordBoundary(_ chars: [Character], _ index: Int) -> Bool {
        index <= 0 || " -_:/.+#@\t".contains(chars[index - 1])
    }
}
