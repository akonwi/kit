import Foundation
import KitClient
import Testing
@testable import Kit

@MainActor
struct SessionFeedTests {
    let id = SessionIdentity(server: "studio", session: "s")

    static func message(_ id: String, _ text: String = "") -> TranscriptMessage {
        TranscriptMessage(id: id, role: "assistant", text: text.isEmpty ? id : text, tools: [])
    }

    static func snapshot(_ ids: [String], cursor: String? = nil, start: Int64? = nil) -> SessionExcerpt {
        var session = SessionTesting.session("s")
        session.messages = ids.map { message($0) }
        session.historyCursor = cursor
        session.historyStart = start
        return session
    }

    @Test func aSnapshotMakesTheFeedLive() {
        let feed = SessionFeed(identity: id, client: FakeTranscriptClient())
        #expect(feed.connection == .connecting)
        feed.receive(Self.snapshot(["a", "b"]))
        #expect(feed.connection == .live)
        #expect(feed.session?.messages.map(\.id) == ["a", "b"])
    }

    @Test func earlierPagesStayInFrontOfLaterSnapshots() async {
        let client = FakeTranscriptClient()
        client.pages["10"] = TranscriptHistoryPage(sessionID: "s", messages: [Self.message("y"), Self.message("z")], previousCursor: "5")
        let feed = SessionFeed(identity: id, client: client)
        feed.receive(Self.snapshot(["a", "b"], cursor: "10", start: 10))
        #expect(feed.hasEarlier)

        await feed.loadEarlier()
        #expect(feed.session?.messages.map(\.id) == ["y", "z", "a", "b"])
        #expect(feed.session?.historyCursor == "5")

        // A live update with the same boundary keeps the loaded page.
        feed.receive(Self.snapshot(["a", "b", "c"], cursor: "10", start: 10))
        #expect(feed.session?.messages.map(\.id) == ["y", "z", "a", "b", "c"])
        #expect(feed.session?.historyCursor == "5")
    }

    @Test func aMovedBoundaryKeepsHistoryJoinedToTheNewSnapshot() async {
        let client = FakeTranscriptClient()
        client.pages["10"] = TranscriptHistoryPage(sessionID: "s", messages: [Self.message("z")], previousCursor: nil)
        let feed = SessionFeed(identity: id, client: client)
        feed.receive(Self.snapshot(["a", "b"], cursor: "10", start: 10))
        await feed.loadEarlier()
        // The server's recent window now starts at b.
        feed.receive(Self.snapshot(["b", "c"], cursor: "12", start: 12))
        #expect(feed.session?.messages.map(\.id) == ["z", "a", "b", "c"])
        // A window that shares nothing starts afresh.
        feed.receive(Self.snapshot(["x"], cursor: "20", start: 20))
        #expect(feed.session?.messages.map(\.id) == ["x"])
        #expect(feed.session?.historyCursor == "20")
    }

    @Test func aCursorTheServerForgotStartsAgainFromTheSnapshot() async {
        let client = FakeTranscriptClient()
        client.historyError = ClientError.http(409)
        let feed = SessionFeed(identity: id, client: client)
        feed.receive(Self.snapshot(["a"], cursor: "10", start: 10))
        await feed.loadEarlier()
        #expect(feed.session?.messages.map(\.id) == ["a"])
        #expect(feed.historyError != nil)
        #expect(!feed.historyLoading)
    }

    @Test func aMissingSessionStopsReconnecting() async {
        let client = FakeTranscriptClient()
        client.watchError = ClientError.missingSession
        let feed = SessionFeed(identity: id, client: client)
        await feed.run()
        #expect(feed.connection == .unavailable("This session is no longer on the server."))
    }

    @Test func aDroppedStreamKeepsItsTranscriptWhileReconnecting() async throws {
        let client = FakeTranscriptClient()
        client.watchError = URLError(.networkConnectionLost)
        client.deliver = Self.snapshot(["a"])
        let feed = SessionFeed(identity: id, client: client)
        let task = Task { await feed.run() }
        defer { task.cancel() }
        // Waits for the drop rather than a fixed time, which a busy machine may overrun.
        for _ in 0..<500 {
            if case .reconnecting = feed.connection { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        guard case .reconnecting = feed.connection else {
            Issue.record("Expected reconnecting, got \(feed.connection)")
            return
        }
        #expect(feed.session?.messages.map(\.id) == ["a"])
        task.cancel()
        await task.value
        // Cancelled during the first delay, before connecting again.
        #expect(client.watches == 1)
    }

    @Test func headerDirectoriesKeepTheirEnds() {
        #expect(SessionScreen.shortDirectory("/Users/me/Developer/agent/kit") == "~/…/agent/kit")
        #expect(SessionScreen.shortDirectory("/Users/me/kit") == "~/kit")
    }
}

final class FakeTranscriptClient: TranscriptPagingClient, @unchecked Sendable {
    let serverID = "studio"
    let isDemo = false
    private let lock = NSLock()
    var pages: [String: TranscriptHistoryPage] = [:]
    var historyError: (any Error)?
    var watchError: (any Error)?
    var deliver: SessionExcerpt?
    private var _watches = 0
    var watches: Int { lock.withLock { _watches } }

    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }

    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        lock.withLock { _watches += 1 }
        if let deliver { await receive(deliver) }
        if let watchError { throw watchError }
    }

    func history(_ id: String, before: String) async throws -> TranscriptHistoryPage {
        if let historyError { throw historyError }
        guard let page = pages[before] else { throw ClientError.invalidPayload }
        return page
    }
}
