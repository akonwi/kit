import AppKit
import SwiftUI
import Testing
@testable import Kit

private actor HistoryClient: TranscriptPagingClient {
    nonisolated let serverID = "history-test"
    nonisolated let isDemo = false
    var receiver: (@Sendable (SessionExcerpt) async -> Void)?
    var pending: CheckedContinuation<TranscriptHistoryPage, any Error>?
    var requests: [String] = []
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        receiver = receive
        while !Task.isCancelled { try await Task.sleep(for: .seconds(1)) }
    }
    func history(_ id: String, before: String) async throws -> TranscriptHistoryPage {
        requests.append(before)
        return try await withCheckedThrowingContinuation { pending = $0 }
    }
    func emit(_ snapshot: SessionExcerpt) async { await receiver?(snapshot) }
    func ready() -> Bool { receiver != nil }
    func waiting() -> Bool { pending != nil }
    func finish(_ page: TranscriptHistoryPage) { pending?.resume(returning: page); pending = nil }
    func fail(_ error: ClientError = .disconnected) { pending?.resume(throwing: error); pending = nil }
}

@MainActor struct TranscriptHistoryTests {
    private func snapshot(_ range: Range<Int>) -> SessionExcerpt {
        SessionExcerpt(id: "s", title: "History", sourceTitle: "", model: "m", thinking: "high",
            workspace: "Test", date: "", messages: range.map {
                TranscriptMessage(id: "m\($0)", role: "user", text: "Message \($0)", tools: [])
            }, historyCursor: String(range.lowerBound), historyStart: Int64(range.lowerBound))
    }
    private func wait(_ predicate: () async -> Bool) async throws {
        for _ in 0..<100 {
            if await predicate() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        Issue.record("History operation did not reach expected state")
    }
    private func setup() async throws -> (SessionStore, HistoryClient) {
        let client = HistoryClient()
        let state = SessionStore(fixture: Fixture(sessions: [snapshot(20..<40)]), client: client)
        state.attach()
        try await wait { await client.ready() }
        await client.emit(snapshot(20..<40))
        return (state, client)
    }

    @Test func paginationPreservesLiveTailAndStopsAtBeginning() async throws {
        let (state, client) = try await setup()
        defer { state.detach() }
        state.loadHistory(); state.loadHistory()
        try await wait { await client.waiting() }
        await client.emit(snapshot(20..<41))
        await client.finish(TranscriptHistoryPage(sessionID: "s", messages: snapshot(1..<20).messages, previousCursor: nil))
        try await wait { !state.historyLoading }
        #expect(state.messages.map(\.id) == (1..<41).map { "m\($0)" })
        #expect(await client.requests == ["20"])
        #expect(state.hasEarlierHistory == false)
        await client.emit(snapshot(20..<42))
        #expect(state.messages.map(\.id) == (1..<42).map { "m\($0)" })
        // A newer bounded snapshot retains the connected older prefix.
        await client.emit(snapshot(30..<43))
        #expect(state.messages.map(\.id) == (1..<43).map { "m\($0)" })
        #expect(state.hasEarlierHistory == false)
    }

    @Test func failureRetriesSameCursorAndDetachedResponseIsIgnored() async throws {
        let (state, client) = try await setup()
        state.loadHistory()
        try await wait { await client.waiting() }
        await client.fail()
        try await wait { !state.historyLoading }
        #expect(state.historyError == ClientError.disconnected.localizedDescription)
        #expect(state.messages.map(\.id) == (20..<40).map { "m\($0)" })
        #expect(state.connectionState == .connected)
        state.loadHistory()
        try await wait { await client.waiting() }
        state.detach()
        await client.finish(TranscriptHistoryPage(sessionID: "s", messages: snapshot(1..<20).messages, previousCursor: nil))
        await Task.yield()
        #expect(state.messages.map(\.id) == (20..<40).map { "m\($0)" })
        #expect(await client.requests == ["20", "20"])
    }

    @Test func expiredCursorRestartsFromAuthoritativeSnapshot() async throws {
        let (state, client) = try await setup()
        defer { state.detach() }
        state.ui.draft = "Keep this"
        state.loadHistory()
        try await wait { await client.waiting() }
        await client.fail(.http(409))
        try await Task.sleep(for: .milliseconds(50))
        await client.emit(snapshot(100..<120))
        #expect(state.messages.map { $0.id } == (100..<120).map { "m\($0)" })
        #expect(state.selected?.historyCursor == "100")
        #expect(state.ui.draft == "Keep this")
        #expect(state.historyError == nil)
    }

    @Test func validatesExclusiveCursorAndPageIdentity() throws {
        func page(sequence: Int = 10, cursor: String = "10", id: String = "s") throws -> WireTranscriptPage {
            let json = """
            {"sessionId":"\(id)","hasMoreMessages":true,"previousMessageCursor":"\(cursor)",
             "messages":[{"id":"m","turnId":"t","sequence":\(sequence),"role":"user",
                          "createdAt":"","content":[{"kind":"text","text":"Older"}]}]}
            """
            return try JSONDecoder().decode(WireTranscriptPage.self, from: Data(json.utf8))
        }
        let valid = try TranscriptHistoryPage(page(), sessionID: "s", before: "20")
        #expect(valid.messages.map(\.text) == ["Older"])
        #expect(valid.previousCursor == "10")
        #expect(throws: (any Error).self) { try TranscriptHistoryPage(page(sequence: 20), sessionID: "s", before: "20") }
        #expect(throws: (any Error).self) { try TranscriptHistoryPage(page(cursor: "9"), sessionID: "s", before: "20") }
        #expect(throws: (any Error).self) { try TranscriptHistoryPage(page(id: "other"), sessionID: "s", before: "20") }
    }

    @Test func prependPreservesRenderedReadingPosition() async throws {
        let (state, client) = try await setup()
        defer { state.detach() }
        let host = NSHostingView(rootView: SessionView(state: state))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 700),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        defer { window.close() }
        try await Task.sleep(for: .milliseconds(250))
        func scrollViews(_ view: NSView) -> [NSScrollView] {
            (view as? NSScrollView).map { [$0] } ?? view.subviews.flatMap(scrollViews)
        }
        let scroll = try #require(scrollViews(host).max { $0.frame.height < $1.frame.height })
        let document = try #require(scroll.documentView)
        // A partial row offset tests preservation beyond simply restoring its ID.
        scroll.contentView.scroll(to: NSPoint(x: 0, y: 140))
        scroll.reflectScrolledClipView(scroll.contentView)
        for phase: Int64 in [1, 2, 4] {
            let wheel = try #require(CGEvent(scrollWheelEvent2Source: nil, units: .pixel,
                                             wheelCount: 1, wheel1: 1, wheel2: 0, wheel3: 0))
            wheel.setIntegerValueField(.scrollWheelEventScrollPhase, value: phase)
            scroll.scrollWheel(with: try #require(NSEvent(cgEvent: wheel)))
            try await Task.sleep(for: .milliseconds(30))
        }
        try await Task.sleep(for: .milliseconds(100))
        let beforeHeight = document.frame.height
        let beforeOffset = scroll.contentView.bounds.minY
        try await wait { await client.waiting() }
        await client.finish(TranscriptHistoryPage(sessionID: "s", messages: snapshot(1..<20).messages, previousCursor: nil))
        try await Task.sleep(for: .milliseconds(250))
        let expected = beforeOffset + document.frame.height - beforeHeight
        #expect(abs(scroll.contentView.bounds.minY - expected) < 2)
    }
}
