import Foundation

/// Which file a path names and where it opens (ADR 0050). Paths in the
/// transcript and paths typed into Open File name files on the session's
/// server: one in the working directory opens as a workspace file, any other
/// as a host file. Files on this Mac open only from the system open panel.
enum FilePaths {
    /// The scheme of transcript links that open a file on the server.
    static let linkScheme = "kit-file"

    /// The pane that shows `path` from the session's server: its workspace
    /// file when it lies in `cwd`, otherwise a host file. A relative path is
    /// resolved against `cwd`. Nil when `path` names nothing that opens.
    static func serverPane(for path: String, cwd: String?) -> WorkspacePane? {
        guard !path.isEmpty, !path.contains("\0") else { return nil }
        if path == "~" || path.hasPrefix("~/") { return .hostFile(path) }
        let absolute: String
        if path.hasPrefix("/") {
            absolute = clean(path)
        } else {
            guard let cwd, cwd.hasPrefix("/") else { return nil }
            absolute = clean(cwd + "/" + path)
        }
        if let file = ToolPresentation.workspacePath(absolute, cwd: cwd) { return .file(file) }
        return .hostFile(absolute)
    }

    /// The path in text typed or pasted into Open File, without surrounding
    /// quotes, shell escapes, or a `file://` scheme. Nil unless it is
    /// absolute or home-relative; other text searches workspace files.
    static func typed(_ text: String) -> String? {
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

    /// A transcript link that opens `path` on the server.
    static func link(_ path: String) -> URL? {
        var components = URLComponents()
        components.scheme = linkScheme
        components.path = path
        return components.url
    }

    /// The path a transcript link opens, or nil for any other URL.
    static func path(fromLink url: URL) -> String? {
        guard url.scheme == linkScheme, let path = URLComponents(url: url, resolvingAgainstBaseURL: false)?.path,
              !path.isEmpty else { return nil }
        return path
    }

    /// The path a Markdown link destination names, or nil for a web or other
    /// URL. Absolute, home-relative, `file://`, and relative destinations
    /// name files; a trailing `#L12` line anchor is dropped.
    static func linkedPath(destination: String) -> String? {
        var value = destination.trimmingCharacters(in: .whitespaces)
        if let range = value.range(of: #"#L\d+(-L?\d+)?$"#, options: .regularExpression) { value.removeSubrange(range) }
        guard !value.isEmpty, !value.hasPrefix("#") else { return nil }
        if value.lowercased().hasPrefix("file:") { return typed(value) }
        if let url = URL(string: value), url.scheme != nil { return nil }
        if value.hasPrefix("/") || value == "~" || value.hasPrefix("~/") { return value.removingPercentEncoding ?? value }
        guard !value.hasPrefix("//") else { return nil }
        return value.removingPercentEncoding ?? value
    }

    /// The path an inline code span is, when it is exactly an absolute or
    /// home-relative file path; nil for code that merely contains one, or
    /// that reads as a pattern, template, URL route, or command.
    static func codePath(_ code: String) -> String? {
        guard code.count > 1, code.count <= 1024, code.hasPrefix("/") || code.hasPrefix("~/"), !code.hasSuffix("/"),
              !code.hasPrefix("//"), !code.contains("://"),
              code.rangeOfCharacter(from: .whitespacesAndNewlines) == nil,
              code.rangeOfCharacter(from: CharacterSet(charactersIn: "{}<>*?$|`\"'\\[]()")) == nil else { return nil }
        return code
    }

    /// An absolute path without empty, `.`, or `..` components, as Go's
    /// `filepath.Clean` makes one. Symbolic links are left alone: the path
    /// may name a file on another machine.
    static func clean(_ path: String) -> String {
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
