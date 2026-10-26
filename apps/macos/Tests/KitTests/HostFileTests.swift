import AppKit
import Foundation
import Testing
@testable import Kit

/// Serves `readHostFile` by port: each port answers one way.
private final class HostFileResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(url.path == "/v1/sessions/session_test/host-files/read")
        #expect(request.httpMethod == "POST")
        let body: String, status: Int, type: String
        switch url.port {
        case 19501:
            status = 200; type = "application/json"
            body = #"{"kind":"text","sessionId":"session_test","path":"/var/log/app.log","size":12,"encoding":"utf-8","content":"one\ntwo\n","returnedBytes":8,"returnedLines":2,"truncated":true,"truncationReason":"byte_limit"}"#
        case 19502:
            status = 200; type = "application/json"
            body = #"{"kind":"image","sessionId":"session_test","path":"/Users/me/shot.png","size":3,"mediaType":"image/png","width":2,"height":1,"data":"AQID"}"#
        case 19503:
            status = 415; type = "application/json"
            body = #"{"error":{"code":"binary_file","message":"only text files and PNG, JPEG, GIF, or WebP images can be shown"}}"#
        case 19504:
            status = 413; type = "application/json"
            body = #"{"error":{"code":"limit_exceeded","message":"image exceeds the size Kit shows","details":{"limit":"image_bytes"}}}"#
        case 19505:
            status = 404; type = "text/plain"
            body = "404 page not found\n"
        case 19506:
            status = 404; type = "application/json"
            body = #"{"error":{"code":"not_found","message":"file does not exist"}}"#
        case 19507:
            status = 400; type = "application/json"
            body = #"{"error":{"code":"not_file","message":"path is not a regular file"}}"#
        default:
            status = 200; type = "application/json"
            body = #"{"kind":"text","sessionId":"session_test","path":"/etc/other","encoding":"utf-8"}"#
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": type])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct HostFileClientTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [HostFileResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "test", serverID: "test", configuration: config)
    }

    @Test func readsTextAndImagesFromTheServer() async throws {
        let text = try await client(19501).readHostFile("session_test", path: "/var/log/app.log")
        #expect(text == ViewedFile(path: "/var/log/app.log", size: 12, content: .text("one\ntwo\n", truncated: true)))
        let image = try await client(19502).readHostFile("session_test", path: "/Users/me/shot.png")
        #expect(image == ViewedFile(path: "/Users/me/shot.png", size: 3, content: .image(Data([1, 2, 3]), mediaType: "image/png", width: 2, height: 1)))
    }

    @Test func mapsDeclaredFailuresAndAnOlderServer() async throws {
        await #expect(throws: ViewedFileError.unsupported) { try await client(19503).readHostFile("session_test", path: "/bin/ls") }
        await #expect(throws: ViewedFileError.imageTooLarge) { try await client(19504).readHostFile("session_test", path: "/big.png") }
        await #expect(throws: ViewedFileError.serverNeedsUpdate) { try await client(19505).readHostFile("session_test", path: "/etc/hosts") }
        await #expect(throws: ViewedFileError.notFound) { try await client(19506).readHostFile("session_test", path: "/missing") }
        await #expect(throws: ViewedFileError.notFile) { try await client(19507).readHostFile("session_test", path: "/tmp") }
        #expect(ViewedFileError.serverNeedsUpdate.errorDescription == "This server’s Kit is too old to open files outside the working directory.")
        #expect(ViewedFileError.unsupported.errorDescription == "Kit shows text files and PNG, JPEG, GIF, or WebP images.")
    }

    @Test func rejectsAReadOfAnotherPathAndPathsThatAreNotAbsolute() async throws {
        await #expect(throws: ClientError.self) { try await client(19508).readHostFile("session_test", path: "/etc/hosts") }
        await #expect(throws: ViewedFileError.invalidPath) { try await client(19501).readHostFile("session_test", path: "relative.txt") }
    }

    @Test func aHomePathMayResolveToTheServersAbsolutePath() throws {
        let read = Components.Schemas.HostFileRead.text(.init(encoding: "utf-8", kind: .text, path: "/home/me/.profile", sessionId: "s"))
        #expect(try ViewedFile.project(read, session: "s", requested: "~/.profile").path == "/home/me/.profile")
        #expect(throws: ClientError.self) { try ViewedFile.project(read, session: "s", requested: "/home/me/.bashrc") }
        #expect(throws: ClientError.self) { try ViewedFile.project(read, session: "other", requested: "~/.profile") }
    }
}

struct LocalFileReadTests {
    private func file(_ name: String, _ data: Data) throws -> String {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let url = directory.appendingPathComponent(name)
        try data.write(to: url)
        return url.path
    }

    @Test func readsTextWithTheServersBounds() throws {
        let path = try file("notes.md", Data("# Notes\nsecond\n".utf8))
        #expect(try ViewedFile.readLocal(path: path) == ViewedFile(path: path, size: 15, content: .text("# Notes\nsecond\n", truncated: false)))
        let long = try file("long.log", Data(String(repeating: "x\n", count: ViewedFile.maxTextLines + 3).utf8))
        let read = try ViewedFile.readLocal(path: long)
        #expect(read.content == .text(String(repeating: "x\n", count: ViewedFile.maxTextLines), truncated: true))
        #expect(read.size == Int64(2 * (ViewedFile.maxTextLines + 3)))
    }

    @Test func readsSupportedImagesWithTheirSize() throws {
        let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 3, pixelsHigh: 2, bitsPerSample: 8, samplesPerPixel: 4,
                                      hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
        let png = try #require(bitmap.representation(using: .png, properties: [:]))
        let path = try file("shot.png", png)
        #expect(try ViewedFile.readLocal(path: path) == ViewedFile(path: path, size: Int64(png.count), content: .image(png, mediaType: "image/png", width: 3, height: 2)))
    }

    @Test func refusesWhatItCannotShow() throws {
        #expect(throws: ViewedFileError.unsupported) { try ViewedFile.readLocal(path: try file("a.bin", Data([0x7F, 0x45, 0x00, 0x01]))) }
        #expect(throws: ViewedFileError.unsupported) { try ViewedFile.readLocal(path: try file("bad.txt", Data([0xC3, 0x28]))) }
        let directory = (try file("x", Data()) as NSString).deletingLastPathComponent
        #expect(throws: ViewedFileError.notFile) { try ViewedFile.readLocal(path: directory) }
        #expect(throws: ViewedFileError.notFound) { try ViewedFile.readLocal(path: directory + "/missing.txt") }
        #expect(throws: ViewedFileError.invalidPath) { try ViewedFile.readLocal(path: "relative.txt") }
        let fifo = directory + "/pipe"
        #expect(mkfifo(fifo, 0o600) == 0)
        #expect(throws: ViewedFileError.notFile) { try ViewedFile.readLocal(path: fifo) }
    }

    @Test func cutsAPreviewAtACharacterBoundary() throws {
        let text = String(repeating: "a", count: ViewedFile.maxTextBytes - 1) + "é"
        let (preview, truncated) = try ViewedFile.preview(Data(text.utf8), complete: false)
        #expect(preview == String(repeating: "a", count: ViewedFile.maxTextBytes - 1))
        #expect(truncated)
    }
}

/// Which pane a path opens, and the links the transcript makes of paths.
@MainActor
struct FilePathsTests {
    @Test func serverPathsOpenWorkspaceFilesInsideTheDirectoryAndHostFilesOutside() {
        #expect(FilePaths.serverPane(for: "/repo/Sources/a.swift", cwd: "/repo") == .file("Sources/a.swift"))
        #expect(FilePaths.serverPane(for: "Sources/a.swift", cwd: "/repo") == .file("Sources/a.swift"))
        #expect(FilePaths.serverPane(for: "/var/log/system.log", cwd: "/repo") == .hostFile("/var/log/system.log"))
        #expect(FilePaths.serverPane(for: "../other/b.go", cwd: "/repo") == .hostFile("/other/b.go"))
        #expect(FilePaths.serverPane(for: "/repo/../etc//hosts", cwd: "/repo") == .hostFile("/etc/hosts"))
        #expect(FilePaths.serverPane(for: "~/notes.md", cwd: "/repo") == .hostFile("~/notes.md"))
        #expect(FilePaths.serverPane(for: "/etc/hosts", cwd: nil) == .hostFile("/etc/hosts"))
        #expect(FilePaths.serverPane(for: "a.txt", cwd: nil) == nil)
        #expect(FilePaths.serverPane(for: "", cwd: "/repo") == nil)
    }

    @Test func typedPathsLoseQuotesEscapesAndFileSchemes() {
        #expect(FilePaths.typed("  /var/log/app.log\n") == "/var/log/app.log")
        #expect(FilePaths.typed("'/Users/me/My File.txt'") == "/Users/me/My File.txt")
        #expect(FilePaths.typed(#"/Users/me/My\ File\ (1).txt"#) == "/Users/me/My File (1).txt")
        #expect(FilePaths.typed("file:///Users/me/My%20File.txt") == "/Users/me/My File.txt")
        #expect(FilePaths.typed("~/notes.md") == "~/notes.md")
        #expect(FilePaths.typed("~") == "~")
        #expect(FilePaths.typed("Sources/a.swift") == nil)
        #expect(FilePaths.typed("main") == nil)
        #expect(FilePaths.typed("file://server/share/a.txt") == nil)
    }

    @Test func linksRoundTripPathsWithSpaces() throws {
        let url = try #require(FilePaths.link("/Users/me/My File #2.txt"))
        #expect(url.scheme == "kit-file")
        #expect(FilePaths.path(fromLink: url) == "/Users/me/My File #2.txt")
        #expect(FilePaths.path(fromLink: URL(string: "https://example.com/a")!) == nil)
    }

    @Test func codeSpansThatAreExactlyAPathBecomeLinks() {
        #expect(FilePaths.codePath("/var/log/system.log") == "/var/log/system.log")
        #expect(FilePaths.codePath("~/.zshrc") == "~/.zshrc")
        for code in ["/", "/usr/local/", "/v1/sessions/{id}", "/tmp/*.log", "ls /tmp", "https://a.b/c", "//network", "src/a.go", "$HOME/a"] {
            #expect(FilePaths.codePath(code) == nil, "\(code)")
        }
    }

    @Test func markdownLinkDestinationsNameFiles() {
        #expect(FilePaths.linkedPath(destination: "/Users/me/a.md") == "/Users/me/a.md")
        #expect(FilePaths.linkedPath(destination: "docs/adrs/0050.md#L12") == "docs/adrs/0050.md")
        #expect(FilePaths.linkedPath(destination: "file:///etc/hosts") == "/etc/hosts")
        #expect(FilePaths.linkedPath(destination: "My%20Notes.md") == "My Notes.md")
        #expect(FilePaths.linkedPath(destination: "https://example.com") == nil)
        #expect(FilePaths.linkedPath(destination: "#section") == nil)
    }

    @Test func markdownTurnsPathsIntoFileLinks() throws {
        let content = MarkdownContent(source: "See `/var/log/app.log`, `/v1/sessions/{id}`, [notes](/Users/me/notes.md), and [site](https://example.com).")
        guard case .paragraph(let text)? = content.blocks.first else { Issue.record("no paragraph"); return }
        let links = text.runs.compactMap { run in run.link.map { (String(text[run.range].characters), $0) } }
        #expect(links.map(\.0) == ["/var/log/app.log", "notes", "site"])
        #expect(links.map(\.1) == [FilePaths.link("/var/log/app.log")!, FilePaths.link("/Users/me/notes.md")!, URL(string: "https://example.com")!])
    }

    @Test func aFileLinkOpensBesideTheTranscriptOnTheServer() {
        let workspace = WorkspaceState()
        workspace.currentDirectory = { "/repo" }
        #expect(workspace.openLink(FilePaths.link("/etc/hosts")!, beside: .conversation))
        #expect(workspace.selected == .hostFile("/etc/hosts"))
        #expect(workspace.openLink(FilePaths.link("/repo/README.md")!, beside: .conversation, externally: true))
        #expect(workspace.selected == .file("README.md"))
        #expect(workspace.panes == [.conversation, .hostFile("/etc/hosts"), .file("README.md")])
    }

    @Test func externalFilePanesKeepTheirPathsThroughRestoration() {
        #expect(WorkspacePane.hostFile("/var/log/a: b.log").id == "host-file:/var/log/a: b.log")
        #expect(WorkspacePane(id: "host-file:/var/log/a: b.log") == .hostFile("/var/log/a: b.log"))
        #expect(WorkspacePane(id: "local-file:/Users/me/shot.png") == .localFile("/Users/me/shot.png"))
        #expect(WorkspacePane(id: "host-file:") == nil)
        #expect(WorkspacePane.hostFile("/var/log/app.log").title == "app.log")
        #expect(WorkspacePane.hostFile("~").title == "~")
        #expect(WorkspacePane.localFile("/Users/me/shot.png").title == "shot.png")
    }

    @Test func imageToolCallsLinkTheirFiles() {
        let call = ToolActivity(id: "tool", name: "show_image", summary: "show_image", output: "", arguments: #"{"path":"/tmp/shot.png"}"#,
                                failed: false, status: "Completed", details: nil)
        #expect(ToolPresentation(call).link == .file("/tmp/shot.png"))
    }

    @Test func externalFileFooterNamesTheSizeAndTruncation() {
        #expect(ExternalFilePhase.footer(size: 12, truncated: false) == "Read-only · 12 bytes")
        #expect(ExternalFilePhase.footer(size: 2_048, truncated: true) == "Read-only · 2 KB · Preview truncated")
    }

    @Test func typedPathsOpenTheirPaneAndCloseThePicker() throws {
        let store = SessionStore(fixture: try Fixture.load())
        store.showFiles(intent: "open")
        store.openTypedPath("/etc/hosts")
        #expect(store.ui.workspace.selected == .hostFile("/etc/hosts"))
        #expect(store.ui.filePicker == false)
        #expect(FileMentionPicker.typedPathDetail == "Opens on the session’s server · Open File on This Mac… (⇧⌘O) opens a file here")
    }
}
