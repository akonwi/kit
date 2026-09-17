import Foundation
import Testing
@testable import Kit

private actor AvailabilityClient: SessionClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    let value = SessionExcerpt(id: "a", title: "Saved session", sourceTitle: "Test", model: "m",
        thinking: "high", workspace: "Test", date: "",
        messages: [TranscriptMessage(id: "m", role: "assistant", text: "Retained transcript", tools: [])])
    var available = true
    var failure: Int?
    func set(available: Bool, failure: Int? = nil) { self.available = available; self.failure = failure }
    func sessions() async throws -> [SessionExcerpt] { available ? [value] : [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        if let failure { throw ClientError.http(failure) }
        guard available else { throw ClientError.http(404) }
        return value
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        await receive(try await snapshot(id))
        try await Task.sleep(for: .seconds(60))
    }
}

@MainActor struct SessionUnavailableTests {
    private func wait(_ predicate: () -> Bool) async throws {
        for _ in 0..<100 {
            if predicate() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        Issue.record("Expected session state was not reached")
    }

    @Test func missingCatalogRetainsLocalWorkAndRecoversInPlace() async throws {
        let client = AvailabilityClient()
        let store = SessionStore(fixture: Fixture(sessions: [await client.value]), client: client)
        store.ui.draft = "Keep my draft"
        store.ui.workspace.scratchpad = "Keep notes"
        store.ui.workspace.open(.file("notes.md"))
        let workspace = store.ui.workspace
        store.attach()
        defer { store.detach() }
        try await wait { store.connectionState == .connected }
        await client.set(available: false)
        await store.refreshSessions()
        #expect(store.connectionState == .unavailable)
        #expect(store.unavailable)
        #expect(store.messages.map(\.text) == ["Retained transcript"])
        #expect(store.ui.draft == "Keep my draft")
        #expect(store.ui.workspace === workspace)
        #expect(store.ui.workspace.selected == .file("notes.md"))
        await client.set(available: true)
        await store.refreshSessions()
        try await wait { store.connectionState == .connected }
        #expect(store.unavailable == false)
        #expect(store.selectedID == "a")
        #expect(store.ui.draft == "Keep my draft")
        #expect(store.ui.workspace.scratchpad == "Keep notes")
    }

    @Test func missingWatchCanRetryWithoutDiscardingTranscript() async throws {
        let client = AvailabilityClient()
        let store = SessionStore(fixture: Fixture(sessions: [await client.value]), client: client)
        await client.set(available: false)
        store.attach()
        defer { store.detach() }
        try await wait { store.connectionState == .unavailable }
        #expect(store.messages.map(\.text) == ["Retained transcript"])
        await client.set(available: true)
        store.attach()
        try await wait { store.connectionState == .connected }
        #expect(store.unavailable == false)
    }

    @Test func authenticationFailureIsNotMissingSession() async throws {
        let client = AvailabilityClient()
        let replica = SessionReplica(sessions: [await client.value], client: client)
        await client.set(available: true, failure: 401)
        replica.attach()
        defer { replica.detach() }
        try await wait { replica.connectionState == .authenticationFailed }
        #expect(replica.unavailable == false)
    }

    @Test func resynchronization404RetainsSnapshot() async throws {
        let client = AvailabilityClient()
        let replica = SessionReplica(sessions: [await client.value], client: client)
        await client.set(available: false)
        do { _ = try await replica.resynchronize(); Issue.record("Expected missing session") }
        catch { #expect(replica.connectionState == .unavailable) }
        #expect(replica.snapshot?.messages.map(\.text) == ["Retained transcript"])
    }
}
