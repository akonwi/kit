import Foundation
import Observation
import Testing
@testable import Kit

private actor ObservationClient: SessionClient {
    nonisolated let serverID = "observation-test"
    nonisolated let isDemo = false
    private var receiver: (@Sendable (SessionExcerpt) async -> Void)?
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        receiver = receive
        while !Task.isCancelled { try await Task.sleep(for: .seconds(1)) }
    }
    func emit(_ snapshot: SessionExcerpt) async { await receiver?(snapshot) }
    func ready() -> Bool { receiver != nil }
}

@MainActor private final class Invalidations {
    private(set) var count = 0
    private let read: @MainActor () -> Void

    /// Counts invalidations of `read`, re-registering after each change.
    init(_ read: @escaping @MainActor () -> Void) {
        self.read = read
        register()
    }

    private func register() {
        withObservationTracking(read) {
            Task { @MainActor [weak self] in
                self?.count += 1
                self?.register()
            }
        }
    }
}

@MainActor struct SessionObservationTests {
    private func session(title: String = "Observed", messages: [String], activity: String? = nil) -> SessionExcerpt {
        var value = SessionExcerpt(id: "s", title: title, sourceTitle: "", model: "m", thinking: "high",
            workspace: "Test", date: "", messages: messages.map {
                TranscriptMessage(id: $0, role: "assistant", text: "Text " + $0, tools: [])
            })
        value.activity = activity
        return value
    }

    private func setup() async throws -> (SessionStore, ObservationClient) {
        let client = ObservationClient()
        let state = SessionStore(fixture: Fixture(sessions: [session(messages: [])]), client: client)
        state.attach()
        for _ in 0..<100 where !(await client.ready()) { try await Task.sleep(for: .milliseconds(10)) }
        await client.emit(session(messages: ["a"]))
        return (state, client)
    }

    private func track(_ read: @escaping @MainActor () -> Void) -> Invalidations {
        Invalidations(read)
    }

    @Test func streamingTranscriptAndActivityDoNotInvalidateMetadataReaders() async throws {
        let (state, client) = try await setup()
        defer { state.detach() }
        let metadata = track { _ = state.selected?.model; _ = state.selected?.title }
        let catalog = track { _ = state.sessions }
        let transcript = track { _ = state.messages }
        let activity = track { _ = state.activity }

        await client.emit(session(messages: ["a", "b"], activity: "Thinking…"))
        try await Task.sleep(for: .milliseconds(20))
        await client.emit(session(messages: ["a", "b", "c"], activity: "Working…"))
        try await Task.sleep(for: .milliseconds(20))

        #expect(state.messages.map(\.id) == ["a", "b", "c"])
        #expect(state.activity == "Working…")
        #expect(state.selected?.title == "Observed")
        #expect(metadata.count == 0)
        #expect(catalog.count == 0)
        #expect(transcript.count == 2)
        #expect(activity.count == 2)
    }

    @Test func metadataChangesInvalidateMetadataAndCatalogReaders() async throws {
        let (state, client) = try await setup()
        defer { state.detach() }
        let metadata = track { _ = state.selected?.title }
        let catalog = track { _ = state.sessions }
        let transcript = track { _ = state.messages }

        await client.emit(session(title: "Renamed", messages: ["a"]))
        try await Task.sleep(for: .milliseconds(20))

        #expect(state.selected?.title == "Renamed")
        #expect(state.sessions.map(\.title) == ["Renamed"])
        #expect(metadata.count == 1)
        #expect(catalog.count == 1)
        #expect(transcript.count == 0)
    }

    @Test func selectedMetadataOmitsTranscriptWhileSnapshotIsComplete() async throws {
        let (state, client) = try await setup()
        defer { state.detach() }
        await client.emit(session(messages: ["a", "b"], activity: "Working…"))

        #expect(state.selected?.messages.map(\.id) == [])
        #expect(state.selected?.activity == nil)
        #expect(state.selectedSnapshot?.messages.map(\.id) == ["a", "b"])
        #expect(state.selectedSnapshot?.activity == "Working…")
    }
}
