import Foundation
import CryptoKit
import Testing
@testable import Kit

private final class AttachmentResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        #expect(request.url?.path == "/v1/sessions/session_test/attachments/attachment_test")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test")
        let mode = request.url!.port!
        let headers = ["Content-Type": "text/plain", "X-Kit-Attachment-ID": "attachment_test",
                       "X-Kit-Session-ID": mode == 19402 ? "session_other" : "session_test"]
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: headers)!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data((mode == 19403 ? "wrong" : mode == 19404 ? "hello extra" : "hello").utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct TranscriptAttachmentTests {
    @Test func downloadsAuthenticateAndValidateIdentitySizeAndDigest() async throws {
        let info = WireAttachmentInfo(id: "attachment_test", sessionId: "session_test", filename: "note.txt",
            mediaType: "text/plain", size: 5, sha256: SHA256.hash(data: Data("hello".utf8)).map { String(format: "%02x", $0) }.joined(),
            createdAt: "2026-09-13T00:00:00Z", width: nil, height: nil)
        for port in 19401...19404 {
            let config = URLSessionConfiguration.ephemeral; config.protocolClasses = [AttachmentResponse.self]
            let client = try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "test", serverID: "test", configuration: config)
            if port == 19401 { #expect(try await client.attachmentContent("session_test", info: info) == Data("hello".utf8)) }
            else { await #expect(throws: ClientError.self) { try await client.attachmentContent("session_test", info: info) } }
        }
    }
    @Test func snapshotRetainsUserAndShowImageAttachments() throws {
        let raw = #"[{"id":"user","turnId":"turn","sequence":1,"role":"user","content":[{"kind":"text","text":"Look"},{"kind":"image","filename":"photo.png","mediaType":"image/png","attachmentId":"attachment_image"}],"createdAt":"now"},{"id":"assistant","turnId":"turn","sequence":2,"role":"assistant","content":[{"kind":"toolCall","toolCallId":"call","toolName":"show_image"}],"createdAt":"now"},{"id":"result","turnId":"turn","sequence":3,"role":"tool","toolCallId":"call","toolName":"show_image","content":[{"kind":"image","filename":"result.png","mediaType":"image/png","attachmentId":"attachment_result"}],"createdAt":"now"}]"#
        let messages = try SessionProjection.transcript(JSONDecoder().decode([WireTranscriptMessage].self, from: Data(raw.utf8)))
        #expect(messages.count == 2)
        #expect(messages[0].text == "Look")
        #expect(messages[0].attachments?.first?.id == "attachment_image")
        #expect(messages[1].tools[0].attachments?.first?.filename == "result.png")
    }
    @Test func imageOnlyMessagesAndMissingIdentitiesRemainVisible() throws {
        let raw = #"[{"id":"user","turnId":"turn","sequence":1,"role":"user","content":[{"kind":"image","filename":"missing.png"}],"createdAt":"now"}]"#
        let messages = try SessionProjection.transcript(JSONDecoder().decode([WireTranscriptMessage].self, from: Data(raw.utf8)))
        #expect(messages.count == 1)
        #expect(messages[0].attachments == [TranscriptAttachment(id: nil, filename: "missing.png", mediaType: nil, isImage: true)])
    }
}

private actor AttachmentFixtureClient: AttachmentClient {
    nonisolated let serverID = "attachment-fixture"
    nonisolated let isDemo = false
    var reads = 0
    let data = Data(String(repeating: "content\n", count: 3000).utf8)
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution {
        if ids == ["missing"] { return .init(attachments: [], missingAttachmentIds: ids) }
        return .init(attachments: [.init(id: ids[0], sessionId: session, filename: "long.txt", mediaType: "text/plain",
            size: Int64(data.count), sha256: "", createdAt: "now", width: nil, height: nil)], missingAttachmentIds: [])
    }
    func attachmentContent(_ session: String, info: WireAttachmentInfo) async throws -> Data {
        #expect(info.sessionId == session)
        reads += 1
        return data
    }
}

extension TranscriptAttachmentTests {
    @MainActor @Test func cachePreservesFullDownloadsAndBoundsTextPreviews() async throws {
        let client = AttachmentFixtureClient()
        let store = TranscriptAttachmentStore(client: client, session: "session_a")
        let reference = TranscriptAttachment(id: "file", filename: "long.txt", mediaType: "text/plain", isImage: false)
        let content = try await store.load(reference)
        #expect(content.text?.count == 16_384)
        #expect(content.truncated)
        #expect(content.data.count == 24_000)
        _ = try await store.load(reference)
        #expect(await client.reads == 1)
        let other = TranscriptAttachmentStore(client: client, session: "session_b")
        #expect(try await other.load(reference).info.sessionId == "session_b")
        #expect(await client.reads == 2)
        await #expect(throws: TranscriptAttachmentStore.Failure.self) {
            _ = try await store.load(.init(id: "missing", filename: "gone.png", mediaType: "image/png", isImage: true))
        }
    }
}
