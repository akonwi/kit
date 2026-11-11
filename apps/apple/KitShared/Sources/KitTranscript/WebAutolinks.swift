import Foundation

/// Bare web addresses in Markdown prose, found as GitHub finds them (the
/// GFM autolink extension): an `http://` or `https://` URL at the start of
/// text or after whitespace, `*`, `_`, `~`, or `(`, running to whitespace or
/// `<`, without trailing punctuation, an unbalanced `)`, or an entity-like
/// `&name;`. Code spans and fences aren't prose, so they're never searched.
enum WebAutolinks {
    /// The addresses in `text`, as ranges of its characters and their URLs.
    /// `previous` is the character shown before `text`, nil at the start of
    /// a block.
    static func matches(in text: String, after previous: Character? = nil) -> [(range: Range<String.Index>, url: URL)] {
        var found: [(Range<String.Index>, URL)] = []
        var index = text.startIndex
        while let scheme = text.range(of: "http", options: [.caseInsensitive], range: index..<text.endIndex) {
            index = text.index(after: scheme.lowerBound)
            let before = scheme.lowerBound > text.startIndex ? text[text.index(before: scheme.lowerBound)] : previous
            if let before, !(before.isWhitespace || "*_~(".contains(before)) { continue }
            var start = scheme.upperBound
            if start < text.endIndex, text[start] == "s" || text[start] == "S" { start = text.index(after: start) }
            guard text[start...].hasPrefix("://") else { continue }
            let host = text.index(start, offsetBy: 3)
            // The host is checked before the rest is read, so text full of
            // addresses that aren't is read about once.
            var hostEnd = host
            while hostEnd < text.endIndex, isDomain(text[hostEnd]) { hostEnd = text.index(after: hostEnd) }
            // A period may end the sentence, not the host.
            var named = hostEnd
            while named > host, text[text.index(before: named)] == "." { named = text.index(before: named) }
            guard validDomain(text[host..<named]) else { continue }
            var end = hostEnd
            while end < text.endIndex, !text[end].isWhitespace, text[end] != "<" { end = text.index(after: end) }
            let candidateEnd = end
            end = trimmed(text, from: host, to: end)
            guard end > host, validDomain(text[host..<min(named, end)]),
                  let url = URL(string: String(text[scheme.lowerBound..<end])),
                  let name = url.host(), !name.isEmpty else {
                // An address that doesn't parse spoils what follows it up to
                // the next space too.
                index = max(index, candidateEnd)
                continue
            }
            found.append((scheme.lowerBound..<end, url))
            index = end
        }
        return found
    }

    /// `text` with each address in it linked; `nil` where none is.
    static func linked(_ text: String, after previous: Character? = nil) -> AttributedString? {
        let matches = matches(in: text, after: previous)
        guard !matches.isEmpty else { return nil }
        var result = AttributedString()
        var last = text.startIndex
        for match in matches {
            result += AttributedString(String(text[last..<match.range.lowerBound]))
            var link = AttributedString(String(text[match.range]))
            link.link = match.url
            result += link
            last = match.range.upperBound
        }
        result += AttributedString(String(text[last...]))
        return result
    }

    /// Where an address ending at `end` really ends: before trailing
    /// punctuation, a `)` without its `(`, or an entity reference.
    private static func trimmed(_ text: String, from start: String.Index, to end: String.Index) -> String.Index {
        var end = end
        var opening = 0, closing = 0
        for character in text[start..<end] {
            if character == "(" { opening += 1 } else if character == ")" { closing += 1 }
        }
        while end > start {
            let last = text[text.index(before: end)]
            if "?!.,:*_~'\"".contains(last) {
                end = text.index(before: end)
            } else if last == ")", closing > opening {
                closing -= 1
                end = text.index(before: end)
            } else if last == ";" {
                // `&name;` reads as an entity, which isn't part of the address.
                var ampersand = text.index(before: end)
                while ampersand > start, text[text.index(before: ampersand)].isLetter || text[text.index(before: ampersand)].isNumber {
                    ampersand = text.index(before: ampersand)
                }
                guard ampersand > start, text[text.index(before: ampersand)] == "&" else { break }
                end = text.index(before: ampersand)
            } else {
                break
            }
        }
        return end
    }

    private static func isDomain(_ character: Character) -> Bool {
        character.isLetter || character.isNumber || character == "-" || character == "_" || character == "."
    }

    /// Whether the address's host is a valid domain: letters, digits, `_` and
    /// `-` in segments split by periods, without an underscore in its last two.
    private static func validDomain(_ host: Substring) -> Bool {
        guard !host.isEmpty else { return false }
        let segments = host.split(separator: ".", omittingEmptySubsequences: false)
        guard segments.allSatisfy({ !$0.isEmpty && $0.allSatisfy { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" } }) else {
            return false
        }
        return !segments.suffix(2).contains { $0.contains("_") }
    }
}
