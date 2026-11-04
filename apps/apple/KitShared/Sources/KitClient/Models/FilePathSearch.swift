import Foundation

/// Fuzzy search over a session's file paths, as the file pickers and `@`
/// mentions rank them.
public enum FilePathSearch {
    /// The paths matching `query`, best first. Same ranking tiers and tie
    /// order as vaxis DefaultFuzzySelectFilter: an exact match, then a
    /// prefix, then a substring, then the letters in order; ties keep the
    /// paths' order. An empty query returns every path.
    public static func filtered(query: String, paths: [String]) -> [String] {
        let query = query.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
        guard !query.isEmpty else { return paths }
        return paths.enumerated().compactMap { index, path -> (String, Int, Int)? in
            guard let score = score(path.lowercased(), query) else { return nil }
            return (path, score, index)
        }.sorted { $0.1 == $1.1 ? $0.2 < $1.2 : $0.1 > $1.1 }.map(\.0)
    }

    private static func score(_ candidate: String, _ query: String) -> Int? {
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
