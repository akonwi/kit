import KitClient
import KitTranscript
import SwiftUI

/// The sessions a transcript can link to: others on its server, as `#`
/// mentions and session tool rows name them.
struct SessionLinks {
    /// The transcript's own session, which a link doesn't open.
    let current: String
    /// The title of a session on the transcript's server, when it's listed.
    let title: @MainActor (String) -> String?
    /// Opens a session over the transcript, so Back returns to it.
    let open: @MainActor (String) -> Void
}

extension EnvironmentValues {
    /// Set on a session's transcripts. Without it, as in a Markdown file,
    /// a mention stays as written.
    @Entry var sessionLinks: SessionLinks? = nil
}

extension SessionMentions {
    /// `text` with each mention link drawn as `#` and its session's title in
    /// accent on an accent tint, as the Mac draws it. The transcript's own
    /// session reads as its title, not a link. A session the server doesn't
    /// list reads `#unknown session`, muted and not a link. Without `links`,
    /// the token stays as written.
    @MainActor static func presented(_ text: AttributedString, links: SessionLinks?) -> AttributedString {
        var result = text
        // Replace from the end, by offset, so earlier spans stay where they were.
        for (url, original) in text.runs[\.link].reversed() {
            guard let url, let id = session(in: url) else { continue }
            let start = text.characters.distance(from: text.startIndex, to: original.lowerBound)
            let length = text.characters.distance(from: original.lowerBound, to: original.upperBound)
            let lower = result.characters.index(result.startIndex, offsetBy: start)
            let range = lower..<result.characters.index(lower, offsetBy: length)
            guard let links else { result[range].link = nil; continue }
            let title = links.title(id)
            var piece = AttributedString(label(title: title))
            piece.inlinePresentationIntent = text[original].runs.first?.inlinePresentationIntent
            if title != nil, id == links.current {
                piece.foregroundColor = Theme.text
                piece.backgroundColor = Theme.raised
            } else if title != nil {
                piece.link = url
                piece.foregroundColor = Theme.accent
                piece.backgroundColor = Theme.accent.opacity(0.14)
            } else {
                piece.foregroundColor = Theme.muted
                piece.backgroundColor = Theme.raised
            }
            result.replaceSubrange(range, with: piece)
        }
        return result
    }

    /// Plain text, such as a user's message, with each mention outside a
    /// backtick code span marked as a link to its session.
    static func linked(plain text: String) -> AttributedString {
        var result = AttributedString(text)
        guard text.contains("#[session:") else { return result }
        let source = text as NSString
        let code = codeSpans.matches(in: text, range: NSRange(location: 0, length: source.length)).map(\.range)
        for match in matches(in: text) where !code.contains(where: { NSIntersectionRange($0, match.range).length > 0 }) {
            guard let range = Range(match.range, in: text), let url = link(match.id),
                  let lower = AttributedString.Index(range.lowerBound, within: result),
                  let upper = AttributedString.Index(range.upperBound, within: result) else { continue }
            result[lower..<upper].link = url
        }
        return result
    }

    /// Fenced blocks, then inline spans of one or more backticks.
    private static let codeSpans = try! NSRegularExpression(pattern: #"```[\s\S]*?(```|$)|(`+)[^`]*?\2"#)
}
