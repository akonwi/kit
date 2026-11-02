import Foundation
import KitClient
import KitTranscript

/// The line under a session's title in the Sessions list: what it's asking,
/// what it's doing, or how its last reply began.
enum SessionPreview: Equatable, Sendable {
    /// The session waits for an answer to `title`.
    case asking(Ask, title: String)
    /// The session is working; `text` names the current step.
    case working(String)
    /// The session finished; `text` is the start of its last reply.
    case replied(String)

    enum Ask: Equatable, Sendable {
        case approve, question, signIn

        var label: String {
            switch self {
            case .approve: "Approve"
            case .question: "Question"
            case .signIn: "Sign in"
            }
        }
    }

    /// The line as shown. A stale preview, from a server that can't be
    /// reached, says what the session was doing when last seen.
    func text(stale: Bool) -> String {
        switch self {
        case .asking(let ask, let title): stale ? "Was asking: \(title)" : "\(ask.label): \(title)"
        case .working(let text): stale ? "Was " + text.prefix(1).lowercased() + text.dropFirst() : text
        case .replied(let text): text
        }
    }

    /// The preview a session's snapshot gives, or nil when it has nothing to say.
    static func from(_ session: SessionExcerpt) -> SessionPreview? {
        if let request = session.pendingInteractions?.first {
            let ask: Ask = switch request.kind {
            case .value0: .approve
            case .value4: .signIn
            default: .question
            }
            return .asking(ask, title: oneLine(request.title))
        }
        if session.activeRunID != nil || session.activity != nil {
            if let tool = session.messages.last(where: { $0.role == "tools" })?.tools.last(where: \.running) {
                return .working(working(tool))
            }
            if let activity = session.activity { return .working(activity) }
            return .working(LiveActivity.working)
        }
        if let reply = session.messages.last(where: { $0.role == "assistant" && !$0.text.isEmpty }),
           let text = plain(reply.text) {
            return .replied(text)
        }
        return nil
    }

    /// A running tool call as a short step: "Reading HTTPClient.swift".
    static func working(_ tool: ToolActivity) -> String {
        let presentation = ToolPresentation(tool)
        if tool.bashCommand != nil { return "Running " + presentation.summary }
        let path = presentation.string("path").map { ($0 as NSString).lastPathComponent }
        let step: (verb: String, object: String?)? = switch tool.name.lowercased() {
        case "read": ("Reading", path)
        case "write": ("Writing", path)
        case "edit": ("Editing", path)
        case "ls": ("Listing", path)
        case "grep": ("Searching for", presentation.string("pattern"))
        case "find", "glob": ("Finding", presentation.string("pattern"))
        default: nil
        }
        if let step, let object = step.object, !object.isEmpty { return "\(step.verb) \(oneLine(object))" }
        return presentation.chip.map { "\(presentation.title) · \($0)" } ?? presentation.title
    }

    /// The first block of Markdown that reads as text, on one line.
    static func plain(_ markdown: String) -> String? {
        for block in MarkdownContent(source: markdown).blocks {
            if let text = text(of: block) { return text }
        }
        return nil
    }

    private static func text(of block: MarkdownContent.Block) -> String? {
        switch block {
        case .paragraph(let value), .heading(_, let value):
            let line = oneLine(String(value.characters))
            return line.isEmpty ? nil : line
        case .quote(let blocks): return blocks.lazy.compactMap(text(of:)).first
        case .callout(let callout): return callout.blocks.lazy.compactMap(text(of:)).first
        case .list(let items): return items.lazy.flatMap(\.blocks).compactMap(text(of:)).first
        default: return nil
        }
    }

    private static func oneLine(_ value: String) -> String {
        value.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }
}

/// How the Sessions list writes times and places.
enum SessionFormat {
    /// How long ago a session was active: "now", "4m", "2h", "Yesterday",
    /// a weekday, then a date.
    static func age(_ date: Date, now: Date, calendar: Calendar = .current) -> String {
        let seconds = max(0, now.timeIntervalSince(date))
        if seconds < 60 { return "now" }
        if seconds < 3600 { return "\(Int(seconds / 60))m" }
        let days = calendar.dateComponents([.day], from: calendar.startOfDay(for: date),
                                           to: calendar.startOfDay(for: now)).day ?? 0
        if days == 0 { return "\(Int(seconds / 3600))h" }
        if days == 1 { return "Yesterday" }
        if days < 7 { return date.formatted(.dateTime.weekday(.abbreviated)) }
        let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
        return date.formatted(sameYear ? .dateTime.month(.abbreviated).day() : .dateTime.month(.abbreviated).day().year())
    }

    /// A working directory with the server user's home written `~`. The app
    /// doesn't know the server's home, so it recognizes the usual ones.
    static func directory(_ path: String) -> String {
        let parts = path.split(separator: "/", omittingEmptySubsequences: true)
        if path == "/root" || path.hasPrefix("/root/") { return "~" + path.dropFirst("/root".count) }
        guard parts.count >= 2, ["Users", "home"].contains(parts[0]) else { return path }
        let rest = parts.dropFirst(2).joined(separator: "/")
        return rest.isEmpty ? "~" : "~/" + rest
    }

    /// A server timestamp.
    static func date(_ value: String) -> Date? {
        let parser = ISO8601DateFormatter()
        parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return parser.date(from: value) ?? ISO8601DateFormatter().date(from: value)
    }
}
