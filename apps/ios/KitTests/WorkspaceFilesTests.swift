import Foundation
import KitClient
import Testing
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

@MainActor
struct FileReaderTests {
    @Test func readsTheFilesLines() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.reads["main.swift"] = [.success(try FakeFileClient.file("main.swift", "let a = 1\r\nlet b = 2\n"))]
        let reader = FileReader(path: "main.swift")
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.lines == ["let a = 1", "let b = 2"])
        #expect(reader.error == nil)
        #expect(reader.truncationNotice == nil)
    }

    @Test func aTruncatedFileSaysHowMuchShows() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.reads["big.log"] = [.success(try FakeFileClient.file("big.log", "one\ntwo\n", size: 3_000_000, truncated: true))]
        let reader = FileReader(path: "big.log")
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.truncationNotice == "Showing the first 2 lines of 3 MB.")
    }

    @Test func aRefreshSendsTheRevisionAndReadsFreshWhenItChanged() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.reads["a.txt"] = [
            .success(try FakeFileClient.file("a.txt", "old\n", revision: "r1")),
            .failure(WorkspaceFileError.response(409)),
            .success(try FakeFileClient.file("a.txt", "new\n", revision: "r2")),
        ]
        let reader = FileReader(path: "a.txt")
        await reader.load(client: client, session: "s", cwd: "/repo")
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(client.fileReads == [.init(path: "a.txt", revision: nil), .init(path: "a.txt", revision: "r1"), .init(path: "a.txt", revision: nil)])
        #expect(reader.lines == ["new"])
    }

    @Test func aFileThatCantBeShownSaysWhy() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.reads["logo.png"] = [.failure(WorkspaceFileError.response(415))]
        let reader = FileReader(path: "logo.png")
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.error == "Binary files cannot be previewed.")
        #expect(reader.file == nil)
    }

    @Test func aReadOfAnotherFileOrFolderIsRefused() async throws {
        let client = FakeFileClient(index: FileIndex(paths: []))
        client.reads["a.txt"] = [.success(try FakeFileClient.file("b.txt", "x\n"))]
        let reader = FileReader(path: "a.txt")
        await reader.load(client: client, session: "s", cwd: "/repo")
        #expect(reader.file == nil)
        #expect(reader.error == "Couldn’t read this file.")
    }
}

final class FakeFileClient: FileBrowsingClient, @unchecked Sendable {
    struct Read: Equatable {
        let path: String
        let revision: String?
    }

    let serverID = "studio"
    let isDemo = false
    private let lock = NSLock()
    private var _index: FileIndex
    private var _indexError: (any Error)?
    private var _indexReads: [Bool] = []
    private var _reads: [String: [Result<WireWorkspaceFileRead, any Error>]] = [:]
    private var _fileReads: [Read] = []

    init(index: FileIndex) {
        _index = index
    }

    var index: FileIndex {
        get { lock.withLock { _index } }
        set { lock.withLock { _index = newValue } }
    }
    var indexError: (any Error)? {
        get { lock.withLock { _indexError } }
        set { lock.withLock { _indexError = newValue } }
    }
    var reads: [String: [Result<WireWorkspaceFileRead, any Error>]] {
        get { lock.withLock { _reads } }
        set { lock.withLock { _reads = newValue } }
    }
    var indexReads: [Bool] { lock.withLock { _indexReads } }
    var fileReads: [Read] { lock.withLock { _fileReads } }

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

    func readWorkspaceFile(_ session: String, path: String, expectedRevision: String?) async throws -> WireWorkspaceFileRead {
        try lock.withLock {
            _fileReads.append(Read(path: path, revision: expectedRevision))
            guard var queue = _reads[path], !queue.isEmpty else { throw WorkspaceFileError.response(404) }
            let next = queue.removeFirst()
            _reads[path] = queue
            return try next.get()
        }
    }

    static func file(_ path: String, _ content: String, size: Int? = nil, truncated: Bool = false,
                     revision: String = "file_test", session: String = "s", cwd: String = "/repo") throws -> WireWorkspaceFileRead {
        let limits: [String: Any] = [
            "maxPathBytes": 4096, "maxPathComponents": 64, "defaultDirectoryPageSize": 100, "maxDirectoryPageSize": 200,
            "maxDirectoryEntries": 10000, "maxDirectoryResponseBytes": 524_288, "maxDirectoryObservationBytes": 8_388_608,
            "maxPreviewBytes": 1_000_000, "maxPreviewLines": 5000, "maxActiveRequests": 1, "maxPendingRequests": 0,
        ]
        let payload: [String: Any] = [
            "sessionId": session, "path": path, "revision": revision, "encoding": "utf-8", "content": content,
            "size": size ?? content.utf8.count, "returnedBytes": content.utf8.count,
            "returnedLines": content.split(separator: "\n").count, "truncated": truncated,
            "workspace": ["sessionId": session, "cwd": cwd, "workspaceId": "workspace_test", "state": "ready", "limits": limits],
        ]
        return try JSONDecoder().decode(WireWorkspaceFileRead.self, from: JSONSerialization.data(withJSONObject: payload))
    }
}
