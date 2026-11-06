import Foundation
import KitClient
import Testing
@testable import Kit

/// A session whose turn has started with an image alone. The stream says
/// only "Attachment"; the stored message, read beside it, has the image.
/// The turn never ends here, so no turn-end refresh brings it.
private final class ImageTurn: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let url = request.url!
        if url.path.hasSuffix("/events/stream") {
            let frame = { (sequence: Int, fields: String) in
                "event: session.events\ndata: {\"streamId\":\"stream\",\"firstSequence\":\(sequence),\"lastSequence\":\(sequence),\"events\":[{\"streamId\":\"stream\",\"sequence\":\(sequence),\"sessionId\":\"s\",\"turnId\":\"t\",\(fields)}]}\n\n"
            }
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil,
                headerFields: ["Content-Type": "text/event-stream"])!, cacheStoragePolicy: .notAllowed)
            // Stays open: the turn is still running.
            client?.urlProtocol(self, didLoad: Data((frame(1, #""kind":"turn.started","status":"running""#)
                + frame(2, #""kind":"user.message.added","text":"Attachment""#)).utf8))
            return
        }
        let body = url.path.hasSuffix("/messages")
            ? #"{"SessionID":"s","HasMore":false,"Messages":[{"id":"message_0123456789abcdef0123456789abcdef","turnId":"t","sequence":1,"role":"user","createdAt":"2026-11-05T00:00:00Z","content":[{"kind":"image","attachmentId":"attachment_0123456789abcdef0123456789abcdef","filename":"photo.jpg","mediaType":"image/jpeg"}]}]}"#
            : #"{"session":{"id":"s","name":"Test","cwd":"/tmp","model":"m","thinkingLevel":"high","configurationRevision":0,"createdAt":"","updatedAt":""},"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"followUps":{"count":0},"messages":[],"eventStreamId":"stream","eventCursor":0}"#
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil,
            headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

@MainActor
struct LiveImageTests {
    @Test func aPhotoSentAloneShowsWhileItsTurnRuns() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ImageTurn.self]
        let client = try HTTPClient(endpoint: URL(string: "https://studio.example.ts.net")!, token: "test", instance: "test",
                                    serverID: "studio", remote: true, configuration: configuration)
        let feed = SessionFeed(identity: SessionIdentity(server: "studio", session: "s"), client: client)
        let viewing = Task { await feed.view() }
        defer { viewing.cancel() }

        let deadline = ContinuousClock.now + .seconds(60)
        while feed.session?.messages.last?.attachments?.isEmpty != false, ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        // The message shows the photo, and no placeholder text, while the
        // turn is still running.
        let user = try #require(feed.session?.messages.last)
        #expect(user.role == "user")
        #expect(user.text == "")
        #expect(user.attachments == [TranscriptAttachment(id: "attachment_0123456789abcdef0123456789abcdef",
                                                         filename: "photo.jpg", mediaType: "image/jpeg", isImage: true)])
        #expect(feed.session?.activeRunID == "t")
    }
}
