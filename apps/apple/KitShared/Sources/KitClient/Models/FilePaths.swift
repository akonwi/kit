import Foundation

/// Which file a path names and where it opens (ADR 0050). Paths in the
/// transcript and paths typed into Open File name files on the session's
/// server: one in the working directory opens as a workspace file, any other
/// as a host file. Files on this Mac open only from the system open panel.
public enum FilePaths {
    /// The scheme of transcript links that open a file on the server.
    public static let linkScheme = "kit-file"

    /// The path in text typed or pasted into Open File, without surrounding
    /// quotes, shell escapes, or a `file://` scheme. Nil unless it is
    /// absolute or home-relative; other text searches workspace files.
    public static func typed(_ text: String) -> String? {
        var value = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if value.count >= 2, let first = value.first, first == value.last, first == "'" || first == "\"" {
            value = String(value.dropFirst().dropLast())
        }
        if value.lowercased().hasPrefix("file://") {
            guard let url = URL(string: value), url.isFileURL, url.host == nil || url.host == "" || url.host == "localhost" else { return nil }
            value = url.path
        } else if value.contains("\\") {
            var unescaped = ""
            var escaping = false
            for character in value {
                if escaping || character != "\\" { unescaped.append(character); escaping = false } else { escaping = true }
            }
            value = unescaped
        }
        guard value == "~" || value.hasPrefix("~/") || value.hasPrefix("/"), !value.contains("\0"), !value.contains("\n") else { return nil }
        return value
    }

    /// A transcript link that opens `path` on the server, at `lines` when
    /// given, which the link carries as a `#L42` or `#L42-L50` fragment.
    public static func link(_ path: String, lines: FileLineRange? = nil) -> URL? {
        var components = URLComponents()
        components.scheme = linkScheme
        components.path = path
        components.fragment = lines?.fragment
        return components.url
    }

    /// The path a transcript link opens, or nil for any other URL. Its
    /// lines, if any, are `lines(fromLink:)`.
    public static func path(fromLink url: URL) -> String? {
        guard url.scheme == linkScheme, let path = URLComponents(url: url, resolvingAgainstBaseURL: false)?.path,
              !path.isEmpty else { return nil }
        return path
    }

    /// The lines a transcript link opens its file at, or nil for a link to
    /// the whole file or any other URL.
    public static func lines(fromLink url: URL) -> FileLineRange? {
        guard path(fromLink: url) != nil, let fragment = URLComponents(url: url, resolvingAgainstBaseURL: false)?.fragment else { return nil }
        return FileLineRange(fragment: fragment)
    }

    /// The path a Markdown link destination names, or nil for a web or other
    /// URL. See `linkedLocation(destination:)`.
    public static func linkedPath(destination: String) -> String? {
        linkedLocation(destination: destination)?.path
    }

    /// The file and lines a Markdown link destination names, or nil for a
    /// web or other URL. Absolute, home-relative, `file://`, and relative
    /// destinations name files. Lines follow the path as GitHub writes
    /// them: `#L42`, `#L42-L50`, or `#L42-50`. Lines that can't be, such as
    /// `#L0` or `#L50-L42`, open the whole file. Any other fragment, such as
    /// a heading's `#usage`, is dropped.
    public static func linkedLocation(destination: String) -> FileLocation? {
        var value = destination.trimmingCharacters(in: .whitespaces)
        var lines: FileLineRange?
        if let range = value.range(of: #"#L\d+(-L?\d+)?$"#, options: .regularExpression) {
            lines = FileLineRange(fragment: String(value[range].dropFirst()))
            value.removeSubrange(range)
        }
        return linkedFile(value).map { FileLocation(path: $0, lines: lines) }
    }

    /// The path a destination without its lines names.
    private static func linkedFile(_ destination: String) -> String? {
        var value = destination
        guard !value.isEmpty, !value.hasPrefix("#") else { return nil }
        if value.lowercased().hasPrefix("file:") { return typed(value) }
        // A literal # in a file name is percent-encoded, so one here starts a fragment.
        if let hash = value.firstIndex(of: "#") { value = String(value[..<hash]) }
        if let url = URL(string: value), url.scheme != nil { return nil }
        // A network path, as `//host/a`, names no file here.
        guard !value.hasPrefix("//"), !value.isEmpty else { return nil }
        return value.removingPercentEncoding ?? value
    }

    /// The path an inline code span is, when it is exactly an absolute or
    /// home-relative file path; nil for code that merely contains one, or
    /// that reads as a pattern, template, URL route, or command.
    public static func codePath(_ code: String) -> String? {
        guard code.count > 1, code.count <= 1024, code.hasPrefix("/") || code.hasPrefix("~/"), !code.hasSuffix("/"),
              !code.hasPrefix("//"), !code.contains("://"),
              code.rangeOfCharacter(from: .whitespacesAndNewlines) == nil,
              code.rangeOfCharacter(from: CharacterSet(charactersIn: "{}<>*?$|`\"'\\[]()")) == nil else { return nil }
        return code
    }

    /// An absolute path without empty, `.`, or `..` components, as Go's
    /// `filepath.Clean` makes one. Symbolic links are left alone: the path
    /// may name a file on another machine.
    public static func clean(_ path: String) -> String {
        var parts: [Substring] = []
        for part in path.split(separator: "/", omittingEmptySubsequences: true) {
            switch part {
            case ".": continue
            case "..": if !parts.isEmpty { parts.removeLast() }
            default: parts.append(part)
            }
        }
        return "/" + parts.joined(separator: "/")
    }
}

/// A file a link names and the lines it points at, if any.
public struct FileLocation: Equatable, Sendable {
    public var path: String
    public var lines: FileLineRange?

    public init(path: String, lines: FileLineRange? = nil) {
        self.path = path
        self.lines = lines
    }

    /// The path with its lines as a link names them, as `src/server.go#L42`
    /// or `src/server.go#L42-L50`.
    public var withLines: String { path + (lines.map { "#" + $0.fragment } ?? "") }
}

/// A line, or a range of lines, in a file: 1-based and inclusive.
public struct FileLineRange: Hashable, Sendable, CustomStringConvertible {
    public let start: Int
    public let end: Int

    /// The highest line a link names, as every client reads them.
    public static let maximum = 999_999_999

    /// Lines `start` through `end`; nil unless `1 <= start <= end <= maximum`.
    public init?(start: Int, end: Int? = nil) {
        let end = end ?? start
        guard start >= 1, end >= start, end <= Self.maximum else { return nil }
        self.start = start
        self.end = end
    }

    /// The lines a GitHub-style fragment names: `L42`, `L42-L50`, or `L42-50`.
    public init?(fragment: String) {
        guard let match = fragment.wholeMatch(of: /L(\d+)(?:-L?(\d+))?/), let start = Int(match.1) else { return nil }
        guard let end = match.2 else { self.init(start: start); return }
        guard let last = Int(end) else { return nil }
        self.init(start: start, end: last)
    }

    /// The fragment a link carries them in: `L42` or `L42-L50`.
    public var fragment: String { start == end ? "L\(start)" : "L\(start)-L\(end)" }

    /// `42` or `42-50`.
    public var description: String { start == end ? "\(start)" : "\(start)-\(end)" }
}
