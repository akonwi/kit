import Foundation

// The string operations the reference renderer makes through Go's `strings`
// package, with the same semantics: offsets and lengths are UTF-8 bytes,
// comparisons are bytewise, and whitespace is Unicode White_Space.

extension String {
    /// The string's length in UTF-8 bytes, as Ard's `Str.size()`.
    var size: Int { utf8.count }

    var bytes: [UInt8] { Array(utf8) }

    init(bytes: some Collection<UInt8>) {
        self = String(decoding: bytes, as: UTF8.self)
    }

    /// The bytes from start up to end, or nil when the range is out of
    /// bounds, as Ard's `Str.slice`.
    func slice(start: Int = 0, end: Int? = nil) -> String? {
        let all = bytes
        let stop = end ?? all.count
        guard start >= 0, start <= stop, stop <= all.count else { return nil }
        return String(bytes: all[start..<stop])
    }
}

enum Go {
    static func isSpace(_ scalar: Unicode.Scalar) -> Bool {
        scalar.properties.isWhitespace
    }

    static func trimSpace(_ value: String) -> String {
        let scalars = value.unicodeScalars
        guard let first = scalars.firstIndex(where: { !isSpace($0) }),
              let last = scalars.lastIndex(where: { !isSpace($0) }) else { return "" }
        return String(scalars[first...last])
    }

    static func fields(_ value: String) -> [String] {
        var result: [String] = []
        var current = String.UnicodeScalarView()
        for scalar in value.unicodeScalars {
            if isSpace(scalar) {
                if !current.isEmpty { result.append(String(current)); current = .init() }
            } else {
                current.append(scalar)
            }
        }
        if !current.isEmpty { result.append(String(current)) }
        return result
    }

    /// Lowercases rune by rune, keeping runes without a single-rune mapping,
    /// as Go's simple case mapping does.
    static func toLower(_ value: String) -> String {
        map(value) { $0.properties.lowercaseMapping }
    }

    static func toUpper(_ value: String) -> String {
        map(value) { $0.properties.uppercaseMapping }
    }

    private static func map(_ value: String, _ mapping: (Unicode.Scalar) -> String) -> String {
        var result = String.UnicodeScalarView()
        for scalar in value.unicodeScalars {
            let mapped = mapping(scalar).unicodeScalars
            if mapped.count == 1, let only = mapped.first { result.append(only) } else { result.append(scalar) }
        }
        return String(result)
    }

    static func index(_ value: String, _ needle: String) -> Int {
        index(value.bytes, needle.bytes)
    }

    /// The absolute offset of needle in haystack at or after from, or -1.
    static func index(_ haystack: [UInt8], _ needle: [UInt8], from: Int = 0) -> Int {
        if needle.isEmpty { return from }
        guard needle.count <= haystack.count else { return -1 }
        let first = needle[0]
        var start = from
        while start + needle.count <= haystack.count {
            if haystack[start] == first {
                var matched = true
                var offset = 1
                while offset < needle.count {
                    if haystack[start + offset] != needle[offset] { matched = false; break }
                    offset += 1
                }
                if matched { return start }
            }
            start += 1
        }
        return -1
    }

    /// The byte offset of the first rune of value that is in chars, or -1.
    static func indexAny(_ value: String, _ chars: String) -> Int {
        let set = Set(chars.unicodeScalars)
        var offset = 0
        for scalar in value.unicodeScalars {
            if set.contains(scalar) { return offset }
            offset += String(scalar).utf8.count
        }
        return -1
    }

    static func containsAny(_ value: String, _ chars: String) -> Bool {
        indexAny(value, chars) >= 0
    }

    static func contains(_ value: String, _ needle: String) -> Bool {
        index(value, needle) >= 0
    }

    static func hasPrefix(_ value: String, _ prefix: String) -> Bool {
        value.utf8.starts(with: prefix.utf8)
    }

    static func hasSuffix(_ value: String, _ suffix: String) -> Bool {
        value.utf8.reversed().starts(with: suffix.utf8.reversed())
    }

    static func trimPrefix(_ value: String, _ prefix: String) -> String {
        hasPrefix(value, prefix) ? String(bytes: value.bytes.dropFirst(prefix.size)) : value
    }

    static func trimSuffix(_ value: String, _ suffix: String) -> String {
        hasSuffix(value, suffix) ? String(bytes: value.bytes.dropLast(suffix.size)) : value
    }

    /// Removes leading and trailing runes contained in cutset.
    static func trim(_ value: String, _ cutset: String) -> String {
        trimRight(trimLeft(value, cutset), cutset)
    }

    static func trimLeft(_ value: String, _ cutset: String) -> String {
        let set = Set(cutset.unicodeScalars)
        let scalars = value.unicodeScalars
        guard let first = scalars.firstIndex(where: { !set.contains($0) }) else { return "" }
        return String(scalars[first...])
    }

    static func trimRight(_ value: String, _ cutset: String) -> String {
        let set = Set(cutset.unicodeScalars)
        let scalars = value.unicodeScalars
        guard let last = scalars.lastIndex(where: { !set.contains($0) }) else { return "" }
        return String(scalars[...last])
    }

    static func replaceAll(_ value: String, _ old: String, _ new: String) -> String {
        let haystack = value.bytes, needle = old.bytes, replacement = new.bytes
        guard !needle.isEmpty else { return value }
        var result: [UInt8] = []
        result.reserveCapacity(haystack.count)
        var start = 0
        while start < haystack.count {
            let found = index(haystack, needle, from: start)
            if found < 0 { break }
            result.append(contentsOf: haystack[start..<found])
            result.append(contentsOf: replacement)
            start = found + needle.count
        }
        result.append(contentsOf: haystack[min(start, haystack.count)...])
        return String(bytes: result)
    }

    /// Splits value around each sep; an empty value is one empty field.
    static func split(_ value: String, _ separator: String) -> [String] {
        let haystack = value.bytes, needle = separator.bytes
        var result: [String] = []
        var start = 0
        while needle.count > 0 {
            let found = index(haystack, needle, from: start)
            if found < 0 { break }
            result.append(String(bytes: haystack[start..<found]))
            start = found + needle.count
        }
        result.append(String(bytes: haystack[start...]))
        return result
    }

    static func repeated(_ value: String, _ count: Int) -> String {
        String(repeating: value, count: max(count, 0))
    }

    /// Parses a decimal integer with an optional sign, as Go's strconv.Atoi:
    /// digits only, within Int's range.
    static func atoi(_ value: String) -> Int? {
        Int(value)
    }
}
