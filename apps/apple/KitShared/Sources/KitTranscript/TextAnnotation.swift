import Foundation

// Comments on selected text (ADR 0070): the text the model gets for a
// person's comment on words they selected in a reply, tool output, a diff or
// the scratchpad. Each comment is a block after the message's text: the
// comment, then where the words came from and the words themselves, fenced
// as untrusted with a nonce, since they may have come from a reply or a
// tool. The terminal, browser and Apple clients read the same blocks, held
// to the fixtures in `testdata/text-annotations`.

/// One comment in a message: what the person wrote, the words they
/// selected, where those words came from, and the nonce its fence is
/// tagged with.
public struct TextAnnotation: Equatable, Sendable {
    public var comment: String
    /// Where the words came from, as a person reads it: "Assistant reply",
    /// "Tool output · go test ./…", "Scratchpad".
    public var source: String
    public var quote: String
    public var nonce: String

    public init(comment: String, source: String, quote: String,
                nonce: String = UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()) {
        self.comment = comment; self.source = source; self.quote = quote; self.nonce = nonce
    }
}

public enum TextAnnotationText {
    public static let open = "<text-annotation n=\""
    public static let close = "</text-annotation>"
    static let fenceName = "UNTRUSTED_SELECTED_TEXT"
    /// The most of a quote a block holds, in UTF-8 bytes; the rest is cut.
    public static let maxQuoteBytes = 4096
    /// The most comments on selected text one message holds.
    public static let limit = 16

    // MARK: The message's text

    /// text with every kind of line break as "\n": "\r\n", "\r", U+0085,
    /// U+2028 and U+2029.
    public static func normalizedNewlines(_ text: String) -> String {
        var result = String.UnicodeScalarView()
        var scalars = text.unicodeScalars.makeIterator()
        var pending = scalars.next()
        while let scalar = pending {
            pending = scalars.next()
            switch scalar {
            case "\r":
                result.append("\n")
                if pending == "\n" { pending = scalars.next() }
            case "\u{85}", "\u{2028}", "\u{2029}": result.append("\n")
            default: result.append(scalar)
            }
        }
        return String(result)
    }

    /// quote with its line breaks as "\n" and without the blank space around
    /// it, cut to maxQuoteBytes at a Unicode scalar boundary, and whether it
    /// was cut. Every client cuts at the same place.
    public static func trimmedQuote(_ quote: String) -> (text: String, cut: Bool) {
        let text = normalizedNewlines(quote).trimmingCharacters(in: .whitespacesAndNewlines)
        guard text.utf8.count > maxQuoteBytes else { return (text, false) }
        var kept = String.UnicodeScalarView()
        var bytes = 0
        for scalar in text.unicodeScalars {
            let size = String(scalar).utf8.count
            if bytes + size > maxQuoteBytes { break }
            kept.append(scalar)
            bytes += size
        }
        return (String(kept), true)
    }

    /// One comment's block, numbered number.
    public static func block(number: Int, _ item: TextAnnotation) -> String {
        // A comment's later lines are indented, so none can pass for a line
        // of the block.
        let comment = normalizedNewlines(item.comment).trimmingCharacters(in: .whitespacesAndNewlines)
            .split(separator: "\n", omittingEmptySubsequences: false).joined(separator: "\n  ")
        let source = normalizedNewlines(item.source).split(separator: "\n").joined(separator: " ")
        let quote = trimmedQuote(item.quote)
        return [
            "\(open)\(number)\">",
            "Comment: \(comment)",
            "[\(fenceName) nonce=\(item.nonce)]",
            "From: \(source)",
            quote.cut ? "Text, cut to \(maxQuoteBytes / 1024) KiB:" : "Text:",
            quote.text,
            "[/\(fenceName) nonce=\(item.nonce)]",
            close
        ].joined(separator: "\n")
    }

    /// The blocks for a message's comments, numbered from 1, a blank line
    /// apart, or "" for none.
    public static func text(_ items: [TextAnnotation]) -> String {
        items.enumerated().map { block(number: $0.offset + 1, $0.element) }.joined(separator: "\n\n")
    }

    /// The message's text, then the blocks after it.
    public static func join(_ text: String, _ blocks: String) -> String {
        guard !blocks.isEmpty else { return text }
        return text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? blocks : text + "\n\n" + blocks
    }

    // MARK: Sent comments

    /// A sent comment, as a transcript shows it.
    public struct Sent: Equatable, Identifiable, Sendable {
        public var id: Int { number }
        public let number: Int
        public let comment: String
        public let source: String
        public let quote: String
        /// Whether the quote was cut to fit the block.
        public let cut: Bool

        public init(number: Int, comment: String, source: String, quote: String, cut: Bool) {
            self.number = number; self.comment = comment; self.source = source; self.quote = quote; self.cut = cut
        }
    }

    /// Splits a message into its own text and the comment blocks after it:
    /// blocks numbered from 1 that end the message, after its text and a
    /// blank line or alone. A message without them comes back unchanged, as
    /// does one whose blocks are malformed, are more than `limit`, hold a
    /// quote over `maxQuoteBytes`, or are followed by more text.
    public static func split(_ message: String) -> (text: String, annotations: [Sent]) {
        let first = "\(open)1\">\n"
        var from = message.startIndex
        while let found = message.range(of: first, range: from..<message.endIndex) {
            let before = message[..<found.lowerBound]
            // By scalars, since a "\r" before them joins the first "\n" as one
            // character.
            if before.isEmpty || before.unicodeScalars.suffix(2).elementsEqual("\n\n".unicodeScalars),
               let sent = parseBlocks(message[found.lowerBound...]) {
                return (before.trimmingCharacters(in: .whitespacesAndNewlines), sent)
            }
            from = found.upperBound
        }
        return (message, [])
    }

    /// The blocks text holds, numbered from 1 and a blank line apart, when
    /// they run to its end.
    private static func parseBlocks(_ text: Substring) -> [Sent]? {
        var sent: [Sent] = []
        var rest = text
        while true {
            guard sent.count < limit, let (block, end) = parse(rest, number: sent.count + 1) else { return nil }
            sent.append(block)
            rest = rest[end...]
            if rest.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return sent }
            guard rest.unicodeScalars.prefix(2).elementsEqual("\n\n".unicodeScalars) else { return nil }
            rest = rest[rest.unicodeScalars.index(rest.startIndex, offsetBy: 2)...]
        }
    }

    /// The block numbered number at the start of text and where it ends: its
    /// fence closes with its own nonce, so nothing inside can end it early.
    private static func parse(_ text: Substring, number: Int) -> (Sent, Substring.Index)? {
        let head = "\(open)\(number)\">\nComment: "
        guard text.hasPrefix(head) else { return nil }
        var rest = text.dropFirst(head.count)
        let fenceStart = "\n[\(fenceName) nonce="
        guard let fence = rest.range(of: fenceStart) else { return nil }
        let comment = String(rest[..<fence.lowerBound]).replacingOccurrences(of: "\n  ", with: "\n")
        rest = rest[fence.upperBound...]
        guard let nonceEnd = rest.range(of: "]\n") else { return nil }
        let nonce = rest[..<nonceEnd.lowerBound]
        guard !nonce.isEmpty, !nonce.contains("\n") else { return nil }
        let closing = "\n[/\(fenceName) nonce=\(nonce)]\n\(close)"
        let body = rest[nonceEnd.upperBound...]
        guard let end = body.range(of: closing) else { return nil }
        let inside = body[..<end.lowerBound]
        guard inside.hasPrefix("From: "), let sourceEnd = inside.range(of: "\n") else { return nil }
        let source = String(inside[inside.index(inside.startIndex, offsetBy: 6)..<sourceEnd.lowerBound])
        let afterSource = inside[sourceEnd.upperBound...]
        let cutHead = "Text, cut to \(maxQuoteBytes / 1024) KiB:\n"
        let cut: Bool
        let quote: Substring
        if afterSource.hasPrefix("Text:\n") {
            cut = false
            quote = afterSource.dropFirst("Text:\n".count)
        } else if afterSource.hasPrefix(cutHead) {
            cut = true
            quote = afterSource.dropFirst(cutHead.count)
        } else {
            return nil
        }
        guard quote.utf8.count <= maxQuoteBytes else { return nil }
        return (Sent(number: number, comment: comment, source: source, quote: String(quote), cut: cut), end.upperBound)
    }
}
