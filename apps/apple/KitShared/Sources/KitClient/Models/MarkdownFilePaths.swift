import Foundation

/// Which files are Markdown, by name: the files that show rendered.
public enum MarkdownFilePaths {
    /// Markdown's common extensions.
    public static let extensions: Set<String> = ["md", "markdown", "mdown", "mdwn", "mkd", "mkdn", "mkdown"]

    /// Whether the file at `path` is Markdown by its extension. A name that
    /// is only an extension, such as `.md`, isn't.
    public static func isMarkdown(_ path: String) -> Bool {
        let name = (path as NSString).lastPathComponent
        let ext = (name as NSString).pathExtension.lowercased()
        return !ext.isEmpty && name.count > ext.count + 1 && extensions.contains(ext)
    }
}
