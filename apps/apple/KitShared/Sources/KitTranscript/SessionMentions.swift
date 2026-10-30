import Foundation

/// Session mentions: `#[session:<id>]` in a message refers the model to
/// another Kit session on the same server, which it may consult with
/// `peer_session`. The token is plain message text on the wire, as the TUI
/// inserts it.
public enum SessionMentions {
    /// The scheme of transcript links a mention renders as.
    public static let linkScheme = "kit-session"

    /// The text a chosen session inserts, before its trailing space.
    public static func token(_ id: String) -> String { "#[session:\(id)]" }

    /// A mention found in text: its range, in UTF-16 offsets, and the session ID.
    public struct Match: Equatable, Sendable {
        public let range: NSRange
        public let id: String

        public init(range: NSRange, id: String) {
            self.range = range
            self.id = id
        }
    }

    private static let pattern = try! NSRegularExpression(pattern: #"#\[session:([^\]\s]+)\]"#)

    /// The mentions in `text`, in order.
    public static func matches(in text: String) -> [Match] {
        let source = text as NSString
        return pattern.matches(in: text, range: NSRange(location: 0, length: source.length)).map {
            Match(range: $0.range, id: source.substring(with: $0.range(at: 1)))
        }
    }

    /// A link that opens the mentioned session.
    public static func link(_ id: String) -> URL? {
        var components = URLComponents()
        components.scheme = linkScheme
        components.path = id
        return components.url
    }

    /// The session a mention link opens, or nil for any other URL.
    public static func session(in url: URL) -> String? {
        guard url.scheme == linkScheme,
              let id = URLComponents(url: url, resolvingAgainstBaseURL: false)?.path, !id.isEmpty else { return nil }
        return id
    }

    /// `text` with each mention outside code marked as a link to its session.
    /// The token's characters stay, so the text still copies as written; the
    /// view names the session when it draws the link.
    public static func linked(_ text: AttributedString) -> AttributedString {
        let plain = String(text.characters)
        guard plain.contains("#[session:") else { return text }
        var result = text
        for match in matches(in: plain) {
            guard let range = Range(match.range, in: plain), let url = link(match.id) else { continue }
            let start = plain.distance(from: plain.startIndex, to: range.lowerBound)
            let length = plain.distance(from: range.lowerBound, to: range.upperBound)
            let lower = result.characters.index(result.startIndex, offsetBy: start)
            let upper = result.characters.index(lower, offsetBy: length)
            let span = lower..<upper
            // Code keeps its raw text, and a token inside a link stays that link.
            guard !result[span].runs.contains(where: {
                $0.inlinePresentationIntent?.contains(.code) == true || $0.link != nil
            }) else { continue }
            result[span].link = url
        }
        return result
    }

    /// How a mention link reads: `#` and the session's title, or, for a
    /// session the app doesn't know, `#unknown session`.
    public static func label(title: String?) -> String {
        "#" + (title ?? "unknown session")
    }
}
