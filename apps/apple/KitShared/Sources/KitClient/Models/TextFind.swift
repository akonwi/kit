import Foundation

/// Finds a plain-text query in a file's text the way every Kit client's find
/// bar does. `testdata/find` at the repository root holds the rules and the
/// cases each client's matcher agrees on; `internal/textfind` is the
/// reference. Ranges are UTF-16 offsets, as `NSString` and TextKit count.
public enum TextFind {
    /// How many matches `find` counts unless told otherwise.
    public static let defaultLimit = 10_000

    /// The find bar's options.
    public struct Options: Equatable, Sendable {
        /// Compare code points exactly instead of by simple case folding.
        public var matchCase: Bool
        /// Keep only matches that don't join a word character on either side.
        public var wholeWord: Bool
        /// How many matches to count; zero or less means `defaultLimit`.
        public var limit: Int

        public init(matchCase: Bool = false, wholeWord: Bool = false, limit: Int = TextFind.defaultLimit) {
            self.matchCase = matchCase
            self.wholeWord = wholeWord
            self.limit = limit
        }
    }

    /// What `find` found.
    public struct Result: Equatable, Sendable {
        /// Half-open UTF-16 ranges in text order; they never overlap.
        public var matches: [Range<Int>]
        /// Whether another match exists past the limit.
        public var capped: Bool

        public init(matches: [Range<Int>] = [], capped: Bool = false) {
            self.matches = matches
            self.capped = capped
        }
    }

    /// The matches of `query` in `text`, scanning left to right. An empty
    /// query, or one with a line feed or carriage return, matches nothing.
    public static func find(_ query: String, in text: String, options: Options = Options()) -> Result {
        let needle = query.unicodeScalars.map { options.matchCase ? $0.value : fold($0) }
        guard let first = needle.first, !needle.contains(0x0A), !needle.contains(0x0D) else { return Result() }
        let haystack = Haystack(text, matchCase: options.matchCase)
        let scalars = haystack.scalars
        let limit = options.limit > 0 ? options.limit : defaultLimit
        var result = Result()
        var start = 0
        while start + needle.count <= scalars.count {
            guard scalars[start] == first, matches(needle, in: scalars, at: start),
                  !options.wholeWord || haystack.isWholeWord(start, start + needle.count) else {
                start += 1
                continue
            }
            if result.matches.count == limit {
                result.capped = true
                break
            }
            result.matches.append(haystack.offsets[start]..<haystack.offsets[start + needle.count])
            start += needle.count
        }
        return result
    }

    /// The index of the first match that starts at or after `offset`, the
    /// first match when none does, or nil when there are none.
    public static func following(_ matches: [Range<Int>], offset: Int) -> Int? {
        guard !matches.isEmpty else { return nil }
        var low = 0, high = matches.count
        while low < high {
            let middle = (low + high) / 2
            if matches[middle].lowerBound < offset { low = middle + 1 } else { high = middle }
        }
        return low == matches.count ? 0 : low
    }

    /// The count a find bar shows: `current` is the one-based index of the
    /// current match, `total` the matches counted, `capped` whether more
    /// exist, and `searchedLines` the lines a cut-short file arrived with,
    /// or 0 when the whole file arrived.
    public static func label(current: Int, total: Int, capped: Bool = false, searchedLines: Int = 0) -> String {
        var label = total > 0 ? "\(grouped(current)) of \(grouped(total))\(capped ? "+" : "")" : "No matches"
        if searchedLines > 0 { label += " in the first \(grouped(searchedLines)) lines" }
        return label
    }

    private static func matches(_ needle: [UInt32], in scalars: [UInt32], at start: Int) -> Bool {
        for index in 1..<needle.count where scalars[start + index] != needle[index] { return false }
        return true
    }

    /// Commas between groups of three digits, whatever the locale, so every
    /// client's count reads the same.
    private static func grouped(_ number: Int) -> String {
        let digits = Array(String(number))
        var text = ""
        for (index, digit) in digits.enumerated() {
            if index > 0, (digits.count - index) % 3 == 0 { text.append(",") }
            text.append(digit)
        }
        return text
    }

    /// The text as code points, folded unless matching case, with the UTF-16
    /// offset each starts at and one past the end.
    private struct Haystack {
        var original: [Unicode.Scalar] = []
        var scalars: [UInt32] = []
        var offsets: [Int] = []

        init(_ text: String, matchCase: Bool) {
            let count = text.unicodeScalars.count
            original.reserveCapacity(count)
            scalars.reserveCapacity(count)
            offsets.reserveCapacity(count + 1)
            var cache: [UInt32: UInt32] = [:]
            var offset = 0
            for scalar in text.unicodeScalars {
                original.append(scalar)
                offsets.append(offset)
                offset += scalar.utf16.count
                if matchCase {
                    scalars.append(scalar.value)
                } else if scalar.isASCII {
                    scalars.append(TextFind.fold(scalar))
                } else if let folded = cache[scalar.value] {
                    scalars.append(folded)
                } else {
                    let folded = TextFind.fold(scalar)
                    cache[scalar.value] = folded
                    scalars.append(folded)
                }
            }
            offsets.append(offset)
        }

        /// Whether the code points from `start` to `end` join no word
        /// character before or after them.
        func isWholeWord(_ start: Int, _ end: Int) -> Bool {
            if start > 0, TextFind.joins(original[start - 1], original[start]) { return false }
            if end < original.count, TextFind.joins(original[end - 1], original[end]) { return false }
            return true
        }
    }

    /// One member of `scalar`'s simple case folding orbit, the same for each
    /// member. Swift has no simple case folding, so this folds by the
    /// single-scalar case mappings and corrects the four code points where
    /// that differs from Unicode's simple folding.
    static func fold(_ scalar: Unicode.Scalar) -> UInt32 {
        if scalar.isASCII {
            let value = scalar.value
            return value >= 0x41 && value <= 0x5A ? value + 0x20 : value
        }
        if let exception = foldExceptions[scalar.value] { return exception }
        let upper = single(scalar.properties.uppercaseMapping, scalar)
        return single(upper.properties.lowercaseMapping, upper).value
    }

    /// `ı` folds to nothing else, and three pairs fold together though
    /// uppercasing one of each pair gives several code points.
    private static let foldExceptions: [UInt32: UInt32] = [0x0131: 0x0131, 0x1FD3: 0x0390, 0x1FE3: 0x03B0, 0xFB05: 0xFB06]

    private static func single(_ mapping: String, _ scalar: Unicode.Scalar) -> Unicode.Scalar {
        let scalars = mapping.unicodeScalars
        return scalars.count == 1 ? scalars.first! : scalar
    }

    /// Whether two adjacent code points belong to one word.
    static func joins(_ a: Unicode.Scalar, _ b: Unicode.Scalar) -> Bool {
        isWordCharacter(a) && isWordCharacter(b) && !isOwnWord(a) && !isOwnWord(b)
    }

    private static func isWordCharacter(_ scalar: Unicode.Scalar) -> Bool {
        if scalar.isASCII {
            let value = scalar.value
            return value == 0x5F || (0x30...0x39).contains(value) || (0x41...0x5A).contains(value) || (0x61...0x7A).contains(value)
        }
        switch scalar.properties.generalCategory {
        case .uppercaseLetter, .lowercaseLetter, .titlecaseLetter, .modifierLetter, .otherLetter,
             .nonspacingMark, .spacingMark, .enclosingMark, .decimalNumber:
            return true
        default:
            return false
        }
    }

    /// Ideographs and kana are each a word of their own.
    private static func isOwnWord(_ scalar: Unicode.Scalar) -> Bool {
        let value = scalar.value
        return (0x3040...0x30FF).contains(value) || (0x31F0...0x31FF).contains(value) || (0xFF66...0xFF9F).contains(value)
            || scalar.properties.isIdeographic
    }
}
