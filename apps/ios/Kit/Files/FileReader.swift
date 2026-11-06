import Foundation
import KitClient

/// One workspace file as last read from the server: its text, or why it
/// can't be shown. Reads again on refresh, keeping the text it has until the
/// new read lands.
@MainActor @Observable
final class FileReader {
    let path: String
    private(set) var file: WireWorkspaceFileRead?
    private(set) var loading = false
    private(set) var error: String?
    private var token = UUID()

    init(path: String) {
        self.path = path
    }

    /// The file's lines as they show, tabs as four spaces. A trailing
    /// newline adds no line.
    private(set) var lines: [String] = []

    /// Why the text stops short, when it does.
    var truncationNotice: String? {
        guard let file, file.truncated else { return nil }
        return "Showing the first \(file.returnedLines.formatted()) lines of \(ByteCountFormatter.string(fromByteCount: file.size, countStyle: .file))."
    }

    /// Reads the file in `session`, whose working directory is `cwd`.
    func load(client: any WorkspaceFileClient, session: String, cwd: String?) async {
        let token = UUID()
        self.token = token
        loading = true
        error = nil
        defer { if self.token == token { loading = false } }
        let expected = file?.revision
        do {
            let read: WireWorkspaceFileRead
            do {
                read = try await client.readWorkspaceFile(session, path: path, expectedRevision: expected)
            } catch WorkspaceFileError.response(409) where expected != nil {
                // The file changed since it was read: read it fresh.
                try Task.checkCancellation()
                read = try await client.readWorkspaceFile(session, path: path, expectedRevision: nil)
            }
            try Task.checkCancellation()
            guard self.token == token else { return }
            guard read.sessionId == session, read.path == path, cwd == nil || read.workspace.cwd == cwd else {
                throw ClientError.invalidPayload
            }
            file = read
            lines = Self.lines(read.content)
        } catch is CancellationError {
        } catch {
            guard self.token == token, !Task.isCancelled else { return }
            self.error = (error as? WorkspaceFileError)?.errorDescription ?? "Couldn’t read this file."
        }
    }

    static func lines(_ content: String) -> [String] {
        guard !content.isEmpty else { return [] }
        var lines = content.components(separatedBy: "\n")
        if lines.last == "" { lines.removeLast() }
        return lines.map { line in
            (line.hasSuffix("\r") ? String(line.dropLast()) : line).replacingOccurrences(of: "\t", with: "    ")
        }
    }
}
