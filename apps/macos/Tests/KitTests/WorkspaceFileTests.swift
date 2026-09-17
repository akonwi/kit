import Foundation
import Testing
@testable import Kit

private final class WorkspaceFileResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let workspace = #"{"sessionId":"session_test","cwd":"/tmp/repo","workspaceId":"workspace_test","state":"ready","limits":{"maxPathBytes":4096,"maxPathComponents":64,"defaultDirectoryPageSize":100,"maxDirectoryPageSize":200,"maxDirectoryEntries":10000,"maxDirectoryResponseBytes":524288,"maxDirectoryObservationBytes":8388608,"maxPreviewBytes":1000000,"maxPreviewLines":5000,"maxActiveRequests":4,"maxPendingRequests":16}}"#
        let body: String, status: Int
        if url.path.hasSuffix("/workspace") {
            #expect(request.httpMethod == "GET")
            body = workspace; status = 200
        } else {
            #expect(url.path == "/v1/sessions/session_test/workspace/files/read")
            #expect(request.httpMethod == "POST")
            status = url.port == 19402 ? 415 : 200
            let path = url.port == 19403 ? "other.swift" : "main.swift"
            body = """
            {"sessionId":"session_test","workspace":\(workspace),"path":"\(path)","revision":"rev_test","size":5,"encoding":"utf-8","content":"hello","returnedBytes":5,"returnedLines":1,"truncated":false}
            """
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: ["Content-Type":"application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct WorkspaceFileTests {
    private func client(_ port: Int = 19401) throws -> HTTPClient {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [WorkspaceFileResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "test", serverID: "test", configuration: config)
    }
    @Test func readsBoundedServerContentAndRejectsMismatchedPath() async throws {
        let file = try await client().readWorkspaceFile("session_test", path: "main.swift")
        #expect(file.content == "hello")
        #expect(file.path == "main.swift")
        #expect(file.workspace.cwd == "/tmp/repo")
        await #expect(throws: WorkspaceFileError.self) { try await client(19402).readWorkspaceFile("session_test", path: "main.swift") }
        await #expect(throws: ClientError.self) { try await client(19403).readWorkspaceFile("session_test", path: "main.swift") }
        #expect(WorkspaceFileError.response(415).errorDescription == "Binary files cannot be previewed.")
    }
    @MainActor @Test func filesCommandOpensAndReusesRetainedTabs() throws {
        let command = try #require(PaletteCommand.catalog(dark: false).first { $0.name == "files" })
        #expect(command.demoOnly == false)
        let workspace = WorkspaceState(demo: false)
        workspace.open(.file("main.swift"))
        workspace.open(.file("other.swift"))
        workspace.open(.file("main.swift"))
        #expect(workspace.selected == .file("main.swift"))
        #expect(workspace.panes == [.conversation, .file("main.swift"), .file("other.swift")])
        #expect(ComposerMentionState.filtered(query: "msw", paths: ["README.md", "main.swift"]) == ["main.swift"])
    }
    @Test(.enabled(if: ProcessInfo.processInfo.environment["KIT_FILES_LIVE_TEST"] == "1"))
    func liveIndexFileCanBeReadThroughSessionWorkspace() async throws {
        let client = try await HTTPClient.local()
        let session = try #require(try await client.sessions().first { $0.cwd?.contains("kit-v2") == true })
        let files = try await client.files(session.id)
        let path = try #require(files.first { $0 == "README.md" })
        let file = try await client.readWorkspaceFile(session.id, path: path)
        #expect(file.path == path)
        #expect(file.workspace.cwd == session.cwd)
        #expect(file.returnedBytes == file.content.utf8.count)
        #expect(file.content.contains("Kit"))
    }
}
