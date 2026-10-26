import Foundation

/// Markdown files in file tabs: which files show rendered, and what the
/// file links in one open.
enum MarkdownFiles {
    /// The extensions of files that show rendered: Markdown's common ones.
    static let extensions: Set<String> = ["md", "markdown", "mdown", "mdwn", "mkd", "mkdn", "mkdown"]

    /// Whether a file is Markdown by its name.
    static func isMarkdown(_ path: String) -> Bool {
        let name = (path as NSString).lastPathComponent
        let ext = (name as NSString).pathExtension.lowercased()
        return !ext.isEmpty && name.count > ext.count + 1 && extensions.contains(ext)
    }

    /// The pane a file link in the Markdown file `file` shows. A link names a
    /// file on the same machine as the file it's in. A relative link is
    /// resolved against the file's directory; on the server, a path in the
    /// working directory `cwd` opens as its workspace file, any other as a
    /// host file (ADR 0050). On this Mac, `~` is `home`. Nil when the link
    /// names nothing that opens.
    static func pane(forLink link: String, in file: WorkspacePane, cwd: String?,
                     home: String = NSHomeDirectory()) -> WorkspacePane? {
        guard !link.isEmpty, !link.contains("\0") else { return nil }
        let absolute = link.hasPrefix("/") || link == "~" || link.hasPrefix("~/")
        switch file {
        case .file(let path):
            if absolute { return FilePaths.serverPane(for: link, cwd: cwd) }
            guard let cwd, cwd.hasPrefix("/") else { return nil }
            return FilePaths.serverPane(for: FilePaths.clean(cwd + "/" + directory(of: path) + "/" + link), cwd: cwd)
        case .hostFile(let path):
            if absolute { return FilePaths.serverPane(for: link, cwd: cwd) }
            if path == "~" || path.hasPrefix("~/") {
                // Only the server knows its home, so the path stays under it.
                return .hostFile(homeRelative(directory(of: String(path.dropFirst(min(2, path.count)))) + "/" + link))
            }
            return FilePaths.serverPane(for: FilePaths.clean(directory(of: path) + "/" + link), cwd: cwd)
        case .localFile(let path):
            let target: String
            if link == "~" || link.hasPrefix("~/") { target = home + "/" + link.dropFirst(1) }
            else if link.hasPrefix("/") { target = link }
            else { target = directory(of: path) + "/" + link }
            return .localFile(FilePaths.clean(target))
        default:
            return nil
        }
    }

    /// Whether `source` is within what a file tab renders: at most 1 MiB,
    /// quotes and lists nested at most 64 deep on any line, and at most
    /// 50,000 such layers in all. The terminal client's parser keeps the
    /// same bounds for files; deeper nesting would recurse past the stack.
    static func renderable(_ source: String) -> Bool {
        guard source.utf8.count <= 1 << 20 else { return false }
        var layers = 0
        for line in source.utf8.split(separator: UInt8(ascii: "\n"), omittingEmptySubsequences: false) {
            let depth = containerDepth(line)
            layers += depth
            if depth > 64 || layers > 50_000 { return false }
        }
        return true
    }

    /// The quote and list markers opening a line, counting indentation as
    /// nesting, as the terminal client's parser counts them.
    private static func containerDepth(_ line: Substring.UTF8View) -> Int {
        let bytes = Array(line)
        var depth = 0, offset = 0
        while offset < bytes.count {
            var spaces = 0
            while offset < bytes.count, bytes[offset] == UInt8(ascii: " ") || bytes[offset] == UInt8(ascii: "\t") {
                spaces += bytes[offset] == UInt8(ascii: "\t") ? 4 : 1
                offset += 1
            }
            guard offset < bytes.count else { return depth }
            var consumed = 0
            switch bytes[offset] {
            case UInt8(ascii: ">"): consumed = 1
            case UInt8(ascii: "-"), UInt8(ascii: "+"), UInt8(ascii: "*"):
                if offset + 1 < bytes.count, bytes[offset + 1] == UInt8(ascii: " ") || bytes[offset + 1] == UInt8(ascii: "\t") { consumed = 1 }
            default:
                var end = offset
                while end < bytes.count, end - offset < 10, bytes[end] >= UInt8(ascii: "0"), bytes[end] <= UInt8(ascii: "9") { end += 1 }
                if end > offset, end + 1 < bytes.count, bytes[end] == UInt8(ascii: ".") || bytes[end] == UInt8(ascii: ")"),
                   bytes[end + 1] == UInt8(ascii: " ") || bytes[end + 1] == UInt8(ascii: "\t") {
                    consumed = end - offset + 1
                }
            }
            guard consumed > 0 else { return depth }
            depth += spaces / 2 + 1
            if depth > 64 { return depth }
            offset += consumed
        }
        return depth
    }

    /// The directory holding `path`: everything before its last component.
    private static func directory(of path: String) -> String {
        guard let slash = path.lastIndex(of: "/") else { return "" }
        return String(path[..<slash])
    }

    /// `~/` and `relative` cleaned of empty, `.`, and `..` components; `..`
    /// past the home directory is kept for the server to resolve.
    private static func homeRelative(_ relative: String) -> String {
        var parts: [Substring] = []
        for part in relative.split(separator: "/", omittingEmptySubsequences: true) {
            switch part {
            case ".": continue
            case "..": if let last = parts.last, last != ".." { parts.removeLast() } else { parts.append(part) }
            default: parts.append(part)
            }
        }
        return parts.isEmpty ? "~" : "~/" + parts.joined(separator: "/")
    }
}
