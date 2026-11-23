import Foundation
import KitClient
import Testing
import UIKit
@testable import Kit

@MainActor
struct WorkspaceFilesTests {
    @Test func searchFindsFilesNotFolders() async {
        let client = FakeFileClient(index: FileIndex(paths: ["src/", "src/main.swift", "README.md"], cwd: "/repo"))
        let files = WorkspaceFiles()
        await files.load(client: client, session: "s")
        #expect(files.search("msw") == ["src/main.swift"])
        #expect(files.search("src") == ["src/main.swift"])
        #expect(files.search("") == ["src/main.swift", "README.md"])
    }

    @Test func loadingAgainReadsOnlyOnRefresh() async {
        let client = FakeFileClient(index: FileIndex(paths: ["a.txt"], cwd: "/repo"))
        let files = WorkspaceFiles()
        await files.load(client: client, session: "s")
        await files.load(client: client, session: "s")
        #expect(client.indexReads == [false])
        client.index = FileIndex(paths: ["a.txt", "b.txt"], cwd: "/repo")
        await files.load(client: client, session: "s", refresh: true)
        #expect(client.indexReads == [false, true])
        #expect(files.files == ["a.txt", "b.txt"])
    }

    @Test func aFailedRefreshKeepsTheFilesItHas() async {
        let client = FakeFileClient(index: FileIndex(paths: ["a.txt"], cwd: "/repo"))
        let files = WorkspaceFiles()
        client.indexError = ClientError.http(503)
        await files.load(client: client, session: "s")
        #expect(files.error == "Couldn’t load the files.")
        #expect(!files.ready)
        client.indexError = nil
        await files.load(client: client, session: "s")
        #expect(files.error == nil)
        client.indexError = ClientError.http(503)
        await files.load(client: client, session: "s", refresh: true)
        #expect(files.error == "Couldn’t refresh the files.")
        #expect(files.files == ["a.txt"])
    }
}

/// A file pane downloads the whole file (ADR 0074).
@MainActor
struct FileReaderTests {
    @Test func downloadsTheFilesLines() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.put("main.swift", "let a = 1\r\nlet b = 2\n")
        let reader = FileReader(path: "main.swift", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.lines == ["let a = 1", "let b = 2"])
        #expect(reader.error == nil)
        #expect(reader.notice == nil)
        #expect(reader.file?.revision == FakeFileClient.revision(1))
        #expect(client.downloads == [.init(target: .workspace(id: FakeFileClient.workspaceID, path: "main.swift"), cachedRevision: nil)])
    }

    @Test func aLargeFileShowsItsStartToALinesEnd() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        let line = String(repeating: "x", count: 1023) + "\n"
        client.put("big.log", "short\n" + String(repeating: line, count: 9 * 1024))
        let reader = FileReader(path: "big.log", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.lines.count == 8 * 1024)
        #expect(reader.lines.last == String(repeating: "x", count: 1023))
        #expect(reader.longest == 1023)
        #expect(reader.notice == "Showing the first 8.4 MB of 9.4 MB.")
    }

    /// A Markdown file's preview is parsed whole, so less of it shows.
    @Test func aLargeMarkdownFileShowsLess() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        let line = String(repeating: "x", count: 1023) + "\n"
        client.put("notes.md", "# Notes\n" + String(repeating: line, count: 3 * 1024))
        let reader = FileReader(path: "notes.md", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.lines.count == 2 * 1024)
        #expect(reader.notice == "Showing the first 2.1 MB of 3.1 MB.")
        #expect(reader.file?.truncated == true)
    }

    @Test func aRefreshOfAnUnchangedFileKeepsItsCopy() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.put("a.txt", "old\n")
        let reader = FileReader(path: "a.txt", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        await reader.load(client: client, session: "s", cwd: "/repo")
        client.put("a.txt", "new\n")
        await reader.load(client: client, session: "s", cwd: "/repo")
        let target = FileDownloadTarget.workspace(id: FakeFileClient.workspaceID, path: "a.txt")
        #expect(client.downloads == [.init(target: target, cachedRevision: nil),
                                     .init(target: target, cachedRevision: FakeFileClient.revision(1)),
                                     .init(target: target, cachedRevision: FakeFileClient.revision(1))])
        #expect(reader.lines == ["new"])
        #expect(reader.file?.revision == FakeFileClient.revision(2))
    }

    @Test func aFileThatChangesWhileItDownloadsIsDownloadedAgain() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.put("a.txt", "first\n")
        client.changes["a.txt"] = [Data("second\n".utf8)]
        let reader = FileReader(path: "a.txt", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.lines == ["second"])
        #expect(reader.file?.revision == FakeFileClient.revision(2))
        #expect(reader.notice == nil)
    }

    /// A copy that changed again can't take comments, since it may not match
    /// any one version of the file.
    @Test func aFileThatKeepsChangingShowsWithoutComments() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.put("a.txt", "first\n")
        client.changes["a.txt"] = [Data("second\n".utf8), Data("third\n".utf8)]
        let reader = FileReader(path: "a.txt", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.lines == ["second"])
        #expect(reader.file == nil)
        #expect(reader.notice == "The file changed while it loaded, so it can’t take comments. Refresh to load it again.")
    }

    /// A server that doesn't know a file's size sends no total to show.
    @Test func aCutFileOfUnknownSizeSaysHowMuchShows() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        let line = String(repeating: "x", count: 1023) + "\n"
        client.put("live.md", String(repeating: line, count: 3 * 1024), sizeKnown: false)
        let reader = FileReader(path: "live.md", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.lines.count == 2 * 1024)
        #expect(reader.notice == "Showing the first 2.1 MB.")
        #expect(reader.file == nil)
    }

    @Test func aFileThatIsntTextSaysWhy() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.put("logo.bin", Data([0x00, 0x01, 0x02]))
        let reader = FileReader(path: "logo.bin", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.error == "Binary files cannot be previewed.")
        #expect(reader.file == nil)
    }

    @Test func aWorkspaceThatMovedIsUnavailable() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.put("a.txt", "x\n")
        let reader = FileReader(path: "a.txt", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/elsewhere")
        #expect(reader.error == "Workspace unavailable.")
        #expect(client.downloads == [])
    }

    @Test func anOlderServerSaysItNeedsAnUpdate() async {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.downloadError = FileDownloadError.serverNeedsUpdate
        let reader = FileReader(path: "a.txt", store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.error == "This server’s Kit needs an update to open files.")
    }
}

/// A file outside the session's folder is downloaded from its server,
/// view-only.
@MainActor
struct HostFileReaderTests {
    @Test func downloadsAFileElsewhereAsText() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.put("~/notes.md", "one\ntwo\n", resolved: "/home/me/notes.md")
        let reader = FileReader(path: "~/notes.md", onServer: true, store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.lines == ["one", "two"])
        #expect(reader.readPath == "/home/me/notes.md")
        #expect(reader.file == nil)
        #expect(client.downloads == [.init(target: .host(path: "~/notes.md"), cachedRevision: nil)])
    }

    @Test func downloadsAnImageElsewhere() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        let png = try #require(UIImage(systemName: "star")?.pngData())
        client.put("/srv/logo.png", png)
        let reader = FileReader(path: "/srv/logo.png", onServer: true, store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.image != nil)
        #expect(reader.text == nil)
        #expect(reader.readPath == "/srv/logo.png")
    }

    @Test func anImageThatKeepsChangingSaysSo() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        let star = try #require(UIImage(systemName: "star")?.pngData())
        let heart = try #require(UIImage(systemName: "heart")?.pngData())
        let circle = try #require(UIImage(systemName: "circle")?.pngData())
        client.put("/srv/logo.png", star)
        client.changes["/srv/logo.png"] = [heart, circle]
        let reader = FileReader(path: "/srv/logo.png", onServer: true, store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.image != nil)
        #expect(reader.notice == "The file changed while it loaded. Refresh to load it again.")
    }

    @Test func aBinaryFileElsewhereSaysWhatShows() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.put("/srv/data.bin", Data([0x00, 0xFF]))
        let reader = FileReader(path: "/srv/data.bin", onServer: true, store: client.store)
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.error == "Kit shows text files and PNG, JPEG, GIF, or WebP images.")
    }

    /// A large image is kept no bigger than the phone needs to zoom into.
    @Test func aLargeImageIsKeptSmaller() throws {
        let context = try #require(CGContext(data: nil, width: 6000, height: 4000, bitsPerComponent: 8, bytesPerRow: 0,
                                             space: CGColorSpaceCreateDeviceRGB(),
                                             bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        let large = try #require(context.makeImage())
        let png = try #require(UIImage(cgImage: large).pngData())
        let image = try #require(FileReader.image(png))
        #expect(image.cgImage?.width == 3072)
        #expect(image.cgImage?.height == 2048)
    }
}

/// A server holding files in memory, each with a revision that changes when
/// its content does.
final class FakeFileClient: FileBrowsingClient, @unchecked Sendable {
    struct Download: Equatable {
        let target: FileDownloadTarget
        let cachedRevision: String?
    }

    struct Stored {
        var data: Data
        /// Nil for a file whose size the server doesn't know, which has no
        /// revision either.
        var revision: String?
        var resolved: String
    }

    static let workspaceID = "workspace_" + String(repeating: "w", count: 43)

    /// The `n`th revision a fake file takes.
    static func revision(_ n: Int) -> String { "file_" + String(format: "%043d", n) }

    let serverID = "studio"
    let isDemo = false
    /// A download store of its own, removed with the client.
    let store: FileDownloadStore
    private let directory: URL
    private let lock = NSLock()
    private var _index: FileIndex
    private var _indexError: (any Error)?
    private var _indexReads: [Bool] = []
    private var _files: [String: Stored] = [:]
    private var _changes: [String: [Data]] = [:]
    private var _downloads: [Download] = []
    private var _downloadError: (any Error)?
    private var revisions = 0

    init(index: FileIndex) {
        _index = index
        directory = FileManager.default.temporaryDirectory.appendingPathComponent("FakeFileClient-\(UUID().uuidString)")
        store = FileDownloadStore(directory: directory, maxTotalBytes: 64 << 20)
    }

    deinit { try? FileManager.default.removeItem(at: directory) }

    var index: FileIndex {
        get { lock.withLock { _index } }
        set { lock.withLock { _index = newValue } }
    }
    var indexError: (any Error)? {
        get { lock.withLock { _indexError } }
        set { lock.withLock { _indexError = newValue } }
    }
    /// Content a file changes to after each download of it, in order.
    var changes: [String: [Data]] {
        get { lock.withLock { _changes } }
        set { lock.withLock { _changes = newValue } }
    }
    var downloadError: (any Error)? {
        get { lock.withLock { _downloadError } }
        set { lock.withLock { _downloadError = newValue } }
    }
    var indexReads: [Bool] { lock.withLock { _indexReads } }
    var downloads: [Download] { lock.withLock { _downloads } }

    /// Stores `content` at `path`, a workspace path or one on the server,
    /// which the server resolves as `resolved`.
    func put(_ path: String, _ content: String, resolved: String? = nil, sizeKnown: Bool = true) {
        put(path, Data(content.utf8), resolved: resolved, sizeKnown: sizeKnown)
    }

    func put(_ path: String, _ data: Data, resolved: String? = nil, sizeKnown: Bool = true) {
        lock.withLock { store(path, data, resolved: resolved, sizeKnown: sizeKnown) }
    }

    private func store(_ path: String, _ data: Data, resolved: String? = nil, sizeKnown: Bool = true) {
        revisions += 1
        _files[path] = Stored(data: data, revision: sizeKnown ? Self.revision(revisions) : nil,
                              resolved: resolved ?? _files[path]?.resolved ?? path)
    }

    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { SessionTesting.session(id) }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}

    func fileIndex(_ session: String, refresh: Bool) async throws -> FileIndex {
        try lock.withLock {
            _indexReads.append(refresh)
            if let _indexError { throw _indexError }
            return _index
        }
    }

    func workspace(_ session: String) async throws -> WireWorkspaceRef {
        let limits: [String: Any] = [
            "maxPathBytes": 4096, "maxPathComponents": 64, "defaultDirectoryPageSize": 100, "maxDirectoryPageSize": 200,
            "maxDirectoryEntries": 10000, "maxDirectoryResponseBytes": 524_288, "maxDirectoryObservationBytes": 8_388_608,
            "maxPreviewBytes": 1_000_000, "maxPreviewLines": 5000, "maxActiveRequests": 1, "maxPendingRequests": 0,
        ]
        let payload: [String: Any] = ["sessionId": session, "cwd": "/repo", "workspaceId": Self.workspaceID, "state": "ready", "limits": limits]
        return try JSONDecoder().decode(WireWorkspaceRef.self, from: JSONSerialization.data(withJSONObject: payload))
    }

    func downloadFile(_ session: String, target: FileDownloadTarget, cachedRevision: String?, maxBytes: Int64?,
                      to destination: URL, progress: (@Sendable (Int64, Int64?) -> Void)?) async throws -> FileDownloadResult {
        let (stored, workspace) = try lock.withLock { () throws -> (Stored, String?) in
            _downloads.append(Download(target: target, cachedRevision: cachedRevision))
            if let _downloadError { throw _downloadError }
            let (path, workspace) = Self.parts(target)
            guard let stored = _files[path] else { throw FileDownloadError.response(404) }
            if var queue = _changes[path], !queue.isEmpty {
                store(path, queue.removeFirst())
                _changes[path] = queue
            }
            return (stored, workspace)
        }
        let info = FileDownloadInfo(session: session, workspace: workspace, path: stored.resolved, revision: stored.revision,
                                    size: stored.revision == nil ? nil : Int64(stored.data.count), modifiedAt: "2026-11-22T12:00:00Z")
        if let revision = stored.revision, cachedRevision == revision {
            return FileDownloadResult(info: info, notModified: true, bytes: 0, truncated: false)
        }
        let written = maxBytes.map { stored.data.prefix(Int($0)) } ?? stored.data
        try written.write(to: destination)
        progress?(Int64(written.count), info.size)
        return FileDownloadResult(info: info, notModified: false, bytes: Int64(written.count), truncated: written.count < stored.data.count)
    }

    func checkFile(_ session: String, target: FileDownloadTarget) async throws -> FileDownloadInfo {
        try lock.withLock {
            let (path, workspace) = Self.parts(target)
            guard let stored = _files[path] else { throw FileDownloadError.response(404) }
            return FileDownloadInfo(session: session, workspace: workspace, path: stored.resolved, revision: stored.revision,
                                    size: stored.revision == nil ? nil : Int64(stored.data.count), modifiedAt: "2026-11-22T12:00:00Z")
        }
    }

    /// A workspace file as comments anchor to it.
    static func file(_ path: String, _ content: String, revision: String = "file_test", session: String = "s") throws -> WireWorkspaceFileRead {
        let limits: [String: Any] = [
            "maxPathBytes": 4096, "maxPathComponents": 64, "defaultDirectoryPageSize": 100, "maxDirectoryPageSize": 200,
            "maxDirectoryEntries": 10000, "maxDirectoryResponseBytes": 524_288, "maxDirectoryObservationBytes": 8_388_608,
            "maxPreviewBytes": 1_000_000, "maxPreviewLines": 5000, "maxActiveRequests": 1, "maxPendingRequests": 0,
        ]
        let payload: [String: Any] = [
            "sessionId": session, "path": path, "revision": revision, "encoding": "utf-8", "content": content,
            "size": content.utf8.count, "returnedBytes": content.utf8.count,
            "returnedLines": content.split(separator: "\n").count, "truncated": false,
            "workspace": ["sessionId": session, "cwd": "/repo", "workspaceId": "workspace_test", "state": "ready", "limits": limits],
        ]
        return try JSONDecoder().decode(WireWorkspaceFileRead.self, from: JSONSerialization.data(withJSONObject: payload))
    }

    private static func parts(_ target: FileDownloadTarget) -> (path: String, workspace: String?) {
        switch target {
        case .workspace(let id, let path): (path, id)
        case .host(let path): (path, nil)
        }
    }
}
