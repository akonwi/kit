import Foundation
import Testing
@testable import Kit

private final class AnnotationConflictResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        let reading = request.httpMethod == "GET"
        let codes = [19201: "stale_file", 19202: "stale_workspace", 19203: "stale_target", 19204: "other"]
        let code = codes[url.port ?? 0] ?? "other"
        let body = reading ? #"{"sessionId":"session_test","entries":[]}"# :
            "{\"error\":{\"code\":\"\(code)\",\"message\":\"stale evidence\"}}"
        #expect(url.path == "/v1/sessions/session_test/annotations")
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: reading ? 200 : 409,
            httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@MainActor struct AnnotationConflictTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [AnnotationConflictResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test",
            instance: "test", serverID: "test", configuration: configuration)
    }
    private var anchor: WireAnnotationAnchor {
        .init(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test", path: "main.swift",
            fileRevision: "file_old", startLine: 1, endLine: 1), workingTreeDiff: nil)
    }

    @Test func staleFilePreservesCommentAndShowsRecovery() async throws {
        let state = AnnotationState()
        state.begin(anchor: anchor)
        state.draft = "Keep my comment"
        let transport = try client(19201)
        #expect(await state.save(client: transport, session: "session_test", anchor: anchor) == false)
        #expect(state.draft == "Keep my comment")
        #expect(state.editor == nil)
        #expect(state.selectionDraft?.path == "main.swift")
        #expect(state.selectionDraft?.body == "Keep my comment")
        #expect(state.evidenceConflict == .staleFile)
        #expect(state.error == "Couldn’t save the annotation. Your comment is preserved. The file changed. Refresh it and select the range again.")
        #expect(!state.uncertain)
        let fresh = WireAnnotationAnchor(kind: .value0, workspaceFile: .init(workspaceId: "workspace_test",
            path: "main.swift", fileRevision: "file_fresh", startLine: 2, endLine: 2), workingTreeDiff: nil)
        state.begin(anchor: fresh)
        #expect(state.editor?.anchor.workspaceFile?.fileRevision == "file_fresh")
        #expect(state.draft == "Keep my comment")
        #expect(state.evidenceConflict == nil)
    }

    @Test(arguments: [
        (19202, AnnotationEvidenceConflict.staleWorkspace),
        (19203, AnnotationEvidenceConflict.staleTarget)
    ]) func otherEvidenceConflictsAreDistinguished(port: Int, expected: AnnotationEvidenceConflict) async throws {
        do {
            _ = try await client(port).createAnnotation("session_test", input: .init(anchor: anchor, body: "Comment"))
            Issue.record("Expected evidence conflict")
        } catch let conflict as AnnotationEvidenceConflict {
            #expect(conflict == expected)
        }
    }

    @Test func unknownConflictRetainsHTTPFallback() async throws {
        do {
            _ = try await client(19204).createAnnotation("session_test", input: .init(anchor: anchor, body: "Comment"))
            Issue.record("Expected HTTP conflict")
        } catch ClientError.http(let status) {
            #expect(status == 409)
        }
    }
}
