import Foundation
import Synchronization
import Testing
@testable import Kit

/// Downloads of whole files (ADR 0074): the client's checks, the store's
/// revalidation, and the panes' fallback to a preview.
@Suite(.serialized)
struct FileDownloadTests {
    static let session = "session_test"
    static let workspace = "workspace_" + String(repeating: "A", count: 43)
    static let revision = "file_" + String(repeating: "B", count: 43)
    static let changed = "file_" + String(repeating: "C", count: 43)
    static let target = FileDownloadTarget.workspace(id: workspace, path: "notes.txt")

    /// One raw HTTP response.
    static func response(status: String = "200 OK", headers: [String: String] = [:], body: String = "", length: Int? = nil) -> Data {
        var fields = headers
        fields["Content-Length"] = String(length ?? body.utf8.count)
        fields["Connection"] = "close"
        let lines = ["HTTP/1.1 " + status] + fields.sorted { $0.key < $1.key }.map { $0.key + ": " + $0.value }
        return Data((lines.joined(separator: "\r\n") + "\r\n\r\n" + body).utf8)
    }

    /// The identity headers of notes.txt at `revision`.
    static func identity(revision: String? = FileDownloadTests.revision, session: String = FileDownloadTests.session,
                         path: String = "notes.txt") -> [String: String] {
        var headers = ["X-Kit-Session-ID": session, "X-Kit-Workspace-ID": workspace, "X-Kit-File-Path": path,
                       "X-Kit-File-Modified-At": "2026-11-18T12:00:00.123456789Z", "Content-Type": "application/octet-stream",
                       "Cache-Control": "private, no-cache"]
        if let revision { headers["ETag"] = "\"" + revision + "\"" }
        return headers
    }

    /// A server answering its requests in turn with `responses`, then 500.
    final class Server: @unchecked Sendable {
        let http: LoopbackHTTPServer
        let url: URL
        init(_ responses: [Data], delay: TimeInterval = 0) async throws {
            let remaining = Mutex(responses)
            http = try LoopbackHTTPServer { _ in
                if delay > 0 { Thread.sleep(forTimeInterval: delay) }
                return remaining.withLock { $0.isEmpty ? FileDownloadTests.response(status: "500 Internal Server Error") : $0.removeFirst() }
            }
            url = try await http.start()
        }
        func client() throws -> HTTPClient {
            try HTTPClient(endpoint: url, token: "test", instance: "instance", serverID: "server_test")
        }
        deinit { http.stop() }
    }

    static func destination() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("kit-download-test-" + UUID().uuidString)
    }

    @Test func downloadsAWholeFileAndChecksItsIdentity() async throws {
        let server = try await Server([Self.response(headers: Self.identity(), body: "0123456789")])
        let destination = Self.destination()
        defer { try? FileManager.default.removeItem(at: destination) }
        let result = try await server.client().downloadFile(Self.session, target: Self.target, cachedRevision: nil, maxBytes: nil,
                                                            to: destination, progress: nil)
        #expect(result == FileDownloadResult(
            info: FileDownloadInfo(session: Self.session, workspace: Self.workspace, path: "notes.txt", revision: Self.revision,
                                   size: 10, modifiedAt: "2026-11-18T12:00:00.123456789Z"),
            notModified: false, bytes: 10, truncated: false))
        #expect(try String(contentsOf: destination, encoding: .utf8) == "0123456789")
        let permissions = try FileManager.default.attributesOfItem(atPath: destination.path)[.posixPermissions] as? Int
        #expect(permissions == 0o600)
        #expect(server.http.requests.first?.contains("GET /v1/sessions/session_test/files/content?workspaceId=\(Self.workspace)&path=notes.txt ") == true)
    }

    @Test(arguments: [
        ("another session", FileDownloadTests.identity(session: "session_other")),
        ("another path", FileDownloadTests.identity(path: "other.txt")),
        ("a host revision", FileDownloadTests.identity(revision: "hostfile_" + String(repeating: "B", count: 43))),
        ("a malformed ETag", FileDownloadTests.identity(revision: "file_short")),
    ])
    func rejectsAResponseForAnotherFile(name: String, headers: [String: String]) async throws {
        let server = try await Server([Self.response(headers: headers, body: "0123456789")])
        let destination = Self.destination()
        defer { try? FileManager.default.removeItem(at: destination) }
        await #expect(throws: ClientError.self, "\(name)") {
            try await server.client().downloadFile(Self.session, target: Self.target, cachedRevision: nil, maxBytes: nil,
                                                   to: destination, progress: nil)
        }
    }

    @Test func rejectsABodyShorterThanItsLength() async throws {
        let server = try await Server([Self.response(headers: Self.identity(), body: "01234", length: 10)])
        let destination = Self.destination()
        defer { try? FileManager.default.removeItem(at: destination) }
        await #expect(throws: (any Error).self) {
            try await server.client().downloadFile(Self.session, target: Self.target, cachedRevision: nil, maxBytes: nil,
                                                   to: destination, progress: nil)
        }
    }

    @Test func cancellationStopsADownload() async throws {
        let server = try await Server([Self.response(headers: Self.identity(), body: "0123456789")], delay: 2)
        let client = try server.client()
        let destination = Self.destination()
        defer { try? FileManager.default.removeItem(at: destination) }
        let task = Task {
            try await client.downloadFile(Self.session, target: Self.target, cachedRevision: nil, maxBytes: nil, to: destination, progress: nil)
        }
        try await Task.sleep(for: .milliseconds(100))
        task.cancel()
        let started = ContinuousClock.now
        do {
            _ = try await task.value
            Issue.record("a cancelled download finished")
        } catch {
            #expect(isCancellation(error), "error = \(error)")
        }
        #expect(started.duration(to: .now) < .seconds(1))
    }

    @Test func aLimitAsksForTheFilesStart() async throws {
        var headers = Self.identity()
        headers["Content-Range"] = "bytes 0-3/10"
        let server = try await Server([Self.response(status: "206 Partial Content", headers: headers, body: "0123")])
        let destination = Self.destination()
        defer { try? FileManager.default.removeItem(at: destination) }
        let result = try await server.client().downloadFile(Self.session, target: Self.target, cachedRevision: nil, maxBytes: 4,
                                                            to: destination, progress: nil)
        #expect(result.bytes == 4 && result.truncated && result.info.size == 10)
        #expect(server.http.requests.first?.contains("Range: bytes=0-3") == true)
    }

    @Test func theStoreRevalidatesAndReplacesAChangedFile() async throws {
        var notModified = Self.identity()
        notModified.removeValue(forKey: "Content-Type")
        let server = try await Server([
            Self.response(headers: Self.identity(), body: "first"),
            Self.response(status: "304 Not Modified", headers: notModified),
            Self.response(headers: Self.identity(revision: Self.changed), body: "second"),
        ])
        let directory = Self.destination()
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = FileDownloadStore(directory: directory, maxTotalBytes: 1 << 20)
        let key = FileDownloadStore.Key(server: "server_test", session: Self.session, target: Self.target, maxBytes: nil)
        let client = try server.client()

        let first = try await store.file(key, client: client)
        #expect(first.info.revision == Self.revision)
        #expect(first.data == Data("first".utf8))
        let directoryPermissions = try FileManager.default.attributesOfItem(atPath: directory.path)[.posixPermissions] as? Int
        #expect(directoryPermissions == 0o700)
        let kept = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        #expect(kept.count == 1)

        let revalidated = try await store.file(key, client: client)
        #expect(revalidated.data == Data("first".utf8) && revalidated.info.revision == Self.revision)
        #expect(server.http.requests[1].contains("If-None-Match: \"\(Self.revision)\""))
        #expect(try FileManager.default.contentsOfDirectory(atPath: directory.path) == kept)

        let replaced = try await store.file(key, client: client)
        #expect(replaced.info.revision == Self.changed)
        #expect(replaced.data == Data("second".utf8))
        let now = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        #expect(now.count == 1 && now != kept)
        // The replaced copy's bytes stay readable to whoever holds them.
        #expect(first.data == Data("first".utf8))
    }

    @Test func theStoreEvictsTheLeastRecentlyUsedFile() async throws {
        let server = try await Server([
            Self.response(headers: Self.identity(), body: "0123456789"),
            Self.response(headers: Self.identity(path: "other.txt"), body: "0123456789"),
        ])
        let directory = Self.destination()
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = FileDownloadStore(directory: directory, maxTotalBytes: 15)
        let client = try server.client()
        let first = try await store.file(.init(server: "server_test", session: Self.session, target: Self.target, maxBytes: nil), client: client)
        let kept = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        let second = try await store.file(.init(server: "server_test", session: Self.session,
                                                target: .workspace(id: Self.workspace, path: "other.txt"), maxBytes: nil), client: client)
        let now = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        #expect(kept.count == 1 && now.count == 1 && now != kept)
        #expect(first.data == Data("0123456789".utf8) && second.data == Data("0123456789".utf8))
    }

    @Test func theStoreDoesNotKeepAFileLargerThanItsBound() async throws {
        let server = try await Server([
            Self.response(headers: Self.identity(), body: "small"),
            Self.response(headers: Self.identity(path: "other.txt"), body: "0123456789"),
        ])
        let directory = Self.destination()
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = FileDownloadStore(directory: directory, maxTotalBytes: 8)
        let client = try server.client()
        _ = try await store.file(.init(server: "server_test", session: Self.session, target: Self.target, maxBytes: nil), client: client)
        let kept = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        let large = try await store.file(.init(server: "server_test", session: Self.session,
                                               target: .workspace(id: Self.workspace, path: "other.txt"), maxBytes: nil), client: client)
        #expect(large.data == Data("0123456789".utf8))
        #expect(kept.count == 1)
        #expect(try FileManager.default.contentsOfDirectory(atPath: directory.path) == kept)
    }

    /// A server whose answers arrive in the order a test releases them.
    actor ScriptedClient: FileDownloadClient {
        nonisolated let serverID = "server_test"
        nonisolated let isDemo = false
        /// An answer: a body and revision, or nil for 304 Not Modified.
        typealias Answer = (body: String, revision: String)?
        private var waiting: [CheckedContinuation<Answer, Never>] = []
        private(set) var revalidated: [String?] = []
        func sessions() async throws -> [SessionExcerpt] { [] }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
        var pending: Int { waiting.count }
        /// Answers the `index`th request still waiting.
        func answer(_ index: Int, with answer: Answer) { waiting.remove(at: index).resume(returning: answer) }
        func downloadFile(_ session: String, target: FileDownloadTarget, cachedRevision: String?, maxBytes: Int64?,
                          to destination: URL, progress: (@Sendable (Int64, Int64?) -> Void)?) async throws -> FileDownloadResult {
            revalidated.append(cachedRevision)
            let answer = await withCheckedContinuation { waiting.append($0) }
            guard let (body, revision) = answer else {
                let info = FileDownloadInfo(session: session, workspace: FileDownloadTests.workspace, path: "notes.txt",
                                            revision: cachedRevision, size: nil, modifiedAt: "2026-11-18T12:00:00Z")
                return FileDownloadResult(info: info, notModified: true, bytes: 0, truncated: false)
            }
            try Data(body.utf8).write(to: destination)
            let info = FileDownloadInfo(session: session, workspace: FileDownloadTests.workspace, path: "notes.txt",
                                        revision: revision, size: Int64(body.utf8.count), modifiedAt: "2026-11-18T12:00:00Z")
            return FileDownloadResult(info: info, notModified: false, bytes: Int64(body.utf8.count), truncated: false)
        }
        func waitForRequests(_ count: Int) async {
            while waiting.count < count { await Task.yield() }
        }
    }

    static func keyed() -> (FileDownloadStore, FileDownloadStore.Key, URL) {
        let directory = destination()
        return (FileDownloadStore(directory: directory, maxTotalBytes: 1 << 20),
                FileDownloadStore.Key(server: "server_test", session: session, target: target, maxBytes: nil), directory)
    }

    @Test func aSlowerOlderAnswerDoesNotReplaceANewerOne() async throws {
        let (store, key, directory) = Self.keyed()
        defer { try? FileManager.default.removeItem(at: directory) }
        let client = ScriptedClient()
        let older = Task { try await store.file(key, client: client) }
        await client.waitForRequests(1)
        let newer = Task { try await store.file(key, client: client) }
        await client.waitForRequests(2)
        await client.answer(1, with: ("newer", Self.changed))
        #expect(try await newer.value.data == Data("newer".utf8))
        await client.answer(0, with: ("older", Self.revision))
        #expect(try await older.value.data == Data("older".utf8))

        // The store still holds the newer copy, so it revalidates that one.
        let again = Task { try await store.file(key, client: client) }
        await client.waitForRequests(1)
        await client.answer(0, with: nil)
        #expect(try await again.value.data == Data("newer".utf8))
        #expect(await client.revalidated == [nil, nil, Self.changed])
    }

    @Test func aRevalidationOvertakenByAChangeFetchesAgainWithoutLosingIt() async throws {
        let (store, key, directory) = Self.keyed()
        defer { try? FileManager.default.removeItem(at: directory) }
        let client = ScriptedClient()
        let first = Task { try await store.file(key, client: client) }
        await client.waitForRequests(1)
        await client.answer(0, with: ("first", Self.revision))
        _ = try await first.value

        let revalidating = Task { try await store.file(key, client: client) }
        await client.waitForRequests(1)
        let changing = Task { try await store.file(key, client: client) }
        await client.waitForRequests(2)
        await client.answer(1, with: ("second", Self.changed))
        #expect(try await changing.value.data == Data("second".utf8))
        // The 304 names a copy the store replaced; it asks again for the
        // current one rather than dropping it.
        await client.answer(0, with: nil)
        await client.waitForRequests(1)
        await client.answer(0, with: nil)
        #expect(try await revalidating.value.data == Data("second".utf8))
        #expect(await client.revalidated == [nil, Self.revision, Self.revision, Self.changed])
    }

    @Test func concurrentRequestsForOneFileEachGetItsBytes() async throws {
        let server = try await Server([
            Self.response(headers: Self.identity(), body: "first"),
            Self.response(headers: Self.identity(revision: Self.changed), body: "second"),
        ], delay: 0.1)
        let directory = Self.destination()
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = FileDownloadStore(directory: directory, maxTotalBytes: 1 << 20)
        let key = FileDownloadStore.Key(server: "server_test", session: Self.session, target: Self.target, maxBytes: nil)
        let client = try server.client()
        async let one = store.file(key, client: client)
        async let two = store.file(key, client: client)
        let bodies = Set(try await [one, two].map { String(decoding: $0.data, as: UTF8.self) })
        #expect(bodies == ["first", "second"])
        #expect(try FileManager.default.contentsOfDirectory(atPath: directory.path).count == 1)
    }

    @Test func anOlderServerLeavesThePreview() async throws {
        let server = try await Server([Self.response(status: "404 Not Found", headers: ["Content-Type": "text/plain"], body: "404 page not found\n")])
        let directory = Self.destination()
        defer { try? FileManager.default.removeItem(at: directory) }
        let download = await WholeFileDownload()
        let text = await download.load(Self.target, session: Self.session, client: try server.client(),
                                       store: FileDownloadStore(directory: directory, maxTotalBytes: 1 << 20))
        #expect(text == nil)
        #expect(await download.state == .idle)
    }

    @Test func aDeclaredMissingFileIsAFailureNotAnOlderServer() async throws {
        let server = try await Server([Self.response(status: "404 Not Found", headers: ["Content-Type": "application/json"],
                                                     body: #"{"error":{"code":"not_found","message":"file does not exist"}}"#)])
        let destination = Self.destination()
        defer { try? FileManager.default.removeItem(at: destination) }
        await #expect(throws: FileDownloadError.response(404)) {
            try await server.client().downloadFile(Self.session, target: Self.target, cachedRevision: nil, maxBytes: nil,
                                                   to: destination, progress: nil)
        }
    }

    @Test func aDownloadReplacesAPreviewWithTheWholeText() async throws {
        let server = try await Server([Self.response(headers: Self.identity(revision: Self.changed), body: "line one\nline two\n")])
        let directory = Self.destination()
        defer { try? FileManager.default.removeItem(at: directory) }
        let download = await WholeFileDownload()
        let text = await download.load(Self.target, session: Self.session, client: try server.client(),
                                       store: FileDownloadStore(directory: directory, maxTotalBytes: 1 << 20))
        #expect(text == WholeFileDownload.Text(content: "line one\nline two\n", revision: Self.changed, size: 18, limited: false))
        #expect(await download.state == .idle)
    }

    @Test func decodingKeepsCompleteLinesAndRefusesBinary() {
        #expect(WholeFileDownload.decode(Data("one\ntwo\nthr".utf8), limited: true) == "one\ntwo\n")
        #expect(WholeFileDownload.decode(Data("one\ntwo\nthr".utf8), limited: false) == "one\ntwo\nthr")
        // A limit inside a character cuts back to the last line.
        #expect(WholeFileDownload.decode(Data("one\nwe\u{301}".utf8).dropLast(), limited: true) == "one\n")
        // A cut-off file with no line end keeps its preview.
        #expect(WholeFileDownload.decode(Data("one line".utf8), limited: true) == nil)
        #expect(WholeFileDownload.decode(Data([0x61, 0x00, 0x62]), limited: false) == nil)
        #expect(WholeFileDownload.decode(Data([0x61, 0xFF, 0x62]), limited: false) == nil)
    }

    @Test func hostPathsHaveOneSpelling() {
        #expect(FilePaths.normalizedHost("~") == "~")
        #expect(FilePaths.normalizedHost("~/") == "~")
        #expect(FilePaths.normalizedHost("~//src/./a/../notes.txt") == "~/src/notes.txt")
        #expect(FilePaths.normalizedHost("~/a/../../b") == "~/../b")
        #expect(FilePaths.normalizedHost("~/../../b") == "~/../../b")
        #expect(FilePaths.normalizedHost("/var//log/./app.log") == "/var/log/app.log")
        #expect(FilePaths.normalizedHost("notes.txt") == nil)
        #expect(FileDownloadStore.Key(server: "s", session: "t", target: .host(path: "~/./src//notes.txt"), maxBytes: nil)
                == FileDownloadStore.Key(server: "s", session: "t", target: .host(path: "~/src/notes.txt"), maxBytes: nil))
    }

    @Test func aHostFileAnswerMustNameTheRequestedFile() {
        #expect(FilePaths.hostPath("/var/log/app.log", answers: "/var/log/../log/app.log"))
        #expect(!FilePaths.hostPath("/var/log/other.log", answers: "/var/log/app.log"))
        #expect(FilePaths.hostPath("/Users/me/notes.txt", answers: "~/notes.txt"))
        #expect(FilePaths.hostPath("/Users/me/src/notes.txt", answers: "~/src/./notes.txt"))
        #expect(!FilePaths.hostPath("/Users/me/other.txt", answers: "~/notes.txt"))
        #expect(!FilePaths.hostPath("/Users/me/../me/notes.txt", answers: "~/notes.txt"))
        #expect(!FilePaths.hostPath("notes.txt", answers: "~/notes.txt"))
        #expect(FilePaths.hostPath("/Users/me", answers: "~"))
        #expect(FilePaths.hostPath("/etc/hosts", answers: "~/../../etc/hosts"))
        #expect(!FilePaths.hostPath("/Users/me/other", answers: "~/..notes"))
        #expect(FilePaths.hostPath("/Users/me/notes.txt", answers: "~/./src/../notes.txt"))
    }

    @Test func aMismatchedHomeFileIsRefused() async throws {
        var headers = Self.identity(revision: "hostfile_" + String(repeating: "B", count: 43), path: "/Users/me/other.txt")
        headers.removeValue(forKey: "X-Kit-Workspace-ID")
        let server = try await Server([Self.response(headers: headers, body: "x")])
        let destination = Self.destination()
        defer { try? FileManager.default.removeItem(at: destination) }
        let error = await #expect(throws: ClientError.self) {
            try await server.client().downloadFile(Self.session, target: .host(path: "~/notes.txt"), cachedRevision: nil,
                                                   maxBytes: nil, to: destination, progress: nil)
        }
        guard case .invalidPayload = error else { Issue.record("expected invalidPayload, got \(String(describing: error))"); return }
    }

    @MainActor @Test func theFooterSaysWhatThePaneShows() {
        #expect(FileFooter.truncation(previewTruncated: false, whole: nil, download: nil) == "")
        #expect(FileFooter.truncation(previewTruncated: true, whole: nil, download: nil) == " · Preview truncated")
        let limited = WholeFileDownload.Text(content: String(repeating: "x\n", count: 1024), revision: Self.revision, size: 1 << 30, limited: true)
        #expect(WholeFileDownload.note(.idle, text: limited) == "Showing the first 2 KB")
        #expect(WholeFileDownload.note(.downloading(written: 1 << 20, size: 4 << 20), text: nil) == "Loading the whole file… 25%")
        #expect(WholeFileDownload.note(.cancelled, text: nil) == "Preview truncated")
    }

    @MainActor @Test func aHostFileTabShowsTheWholeFileAndKeepsItOnReload() async throws {
        let hostRevision = "hostfile_" + String(repeating: "B", count: 43)
        var headers = Self.identity(revision: hostRevision, path: "/var/log/app.log")
        headers.removeValue(forKey: "X-Kit-Workspace-ID")
        var notModified = headers
        notModified.removeValue(forKey: "Content-Type")
        let server = try await Server([
            Self.response(headers: headers, body: "one\ntwo\nthree\n"),
            Self.response(status: "304 Not Modified", headers: notModified),
        ])
        let directory = Self.destination()
        defer { try? FileManager.default.removeItem(at: directory) }
        let loader = ExternalFileLoader(downloadStore: FileDownloadStore(directory: directory, maxTotalBytes: 1 << 20))
        let preview = ViewedFile(path: "/var/log/app.log", size: 14, content: .text("one\n", truncated: true))
        let client = try server.client()
        await loader.load(.init(attempt: 0, activity: 0)) { preview }
        await loader.loadWhole(session: Self.session, client: client)
        #expect(loader.phase.file == ViewedFile(path: "/var/log/app.log", size: 14, content: .text("one\ntwo\nthree\n", truncated: false)))

        // A reload whose preview agrees keeps the whole file while it revalidates.
        await loader.load(.init(attempt: 1, activity: 0)) { preview }
        #expect(loader.phase.file?.content == .text("one\ntwo\nthree\n", truncated: false))
        await loader.loadWhole(session: Self.session, client: client)
        #expect(loader.phase.file?.content == .text("one\ntwo\nthree\n", truncated: false))
        #expect(server.http.requests.count == 2)
    }
}
