import Foundation

extension FilePaths {
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
}
