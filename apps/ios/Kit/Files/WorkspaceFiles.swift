import Foundation
import KitClient

/// What a session's files are read through: its file index and its
/// workspace files.
protocol FileBrowsingClient: WorkspaceFileClient {
    func fileIndex(_ session: String, refresh: Bool) async throws -> FileIndex
}

extension ServerClient: FileBrowsingClient {}

/// A session's files, to find one by name: every file in its working
/// directory, from the server's file index. The index skips ignored files
/// and may stop short in a very large project.
@MainActor @Observable
final class WorkspaceFiles {
    /// The working directory the index lists, once read.
    private(set) var cwd: String?
    /// Whether the server stopped listing before the end.
    private(set) var truncated = false
    private(set) var loading = false
    private(set) var error: String?
    /// Every file, in the index's order.
    private(set) var files: [String] = []
    /// Whether the index has been read.
    private(set) var ready = false
    private var token = UUID()

    /// Reads the index, unless it has been read and `refresh` is false. A
    /// refresh asks the server to list the files again.
    func load(client: any FileBrowsingClient, session: String, refresh: Bool = false) async {
        if ready && !refresh { return }
        let token = UUID()
        self.token = token
        loading = true
        error = nil
        defer { if self.token == token { loading = false } }
        do {
            let index = try await client.fileIndex(session, refresh: refresh)
            guard self.token == token else { return }
            files = index.paths.filter { !$0.hasSuffix("/") }
            cwd = index.cwd
            truncated = index.truncated
            ready = true
        } catch is CancellationError {
        } catch {
            guard self.token == token, !Task.isCancelled else { return }
            self.error = ready ? "Couldn’t refresh the files." : "Couldn’t load the files."
        }
    }

    /// The files matching `query`, best first, up to `limit`.
    func search(_ query: String, limit: Int = 200) -> [String] {
        Array(FilePathSearch.filtered(query: query, paths: files).prefix(limit))
    }
}
