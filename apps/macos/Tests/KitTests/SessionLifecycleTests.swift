import Foundation
import Testing
@testable import Kit

private actor LifecycleClient: SessionClient {
    nonisolated let serverID = "lifecycle"
    nonisolated let isDemo = false
    private(set) var started = 0
    private(set) var stopped = 0
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        started += 1
        defer { stopped += 1 }
        while !Task.isCancelled { try await Task.sleep(for: .seconds(60)) }
    }
}

@MainActor struct SessionLifecycleTests {
    private var fixture: Fixture {
        Fixture(sessions: ["a", "b"].map {
            SessionExcerpt(id: $0, title: $0, sourceTitle: "", model: "m", thinking: "high",
                           workspace: "", date: "", messages: [])
        })
    }

    @Test func constructingSessionViewsDoesNotOpenStreams() async throws {
        let client = LifecycleClient()
        let registry = SessionWindowRegistry(restoration: WindowRestorationStore(
            url: FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)))
        // The non-first selection exercised the old initializer's select/attach path.
        for _ in 0..<200 {
            _ = SessionScreen(fixture: fixture, sessionID: "b", windows: registry, client: client)
        }
        try await Task.sleep(for: .milliseconds(50))
        #expect(await client.started == 0)
        let state = SessionStore(fixture: fixture, client: client, sessionID: "b")
        #expect(state.selectedID == "b")
        #expect(state.selected?.title == "b")
    }

    @Test func releasingSessionStopsItsStream() async throws {
        let client = LifecycleClient()
        var state: SessionStore? = SessionStore(fixture: fixture, client: client)
        weak var released = state
        state?.attach()
        for _ in 0..<100 {
            if await client.started == 1 { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(await client.started == 1)
        state = nil
        #expect(released == nil)
        for _ in 0..<100 {
            if await client.stopped == 1 { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(await client.stopped == 1)
    }
}
