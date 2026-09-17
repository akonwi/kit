import Foundation
import Testing
@testable import Kit

private actor SnapshotClient: SessionNamingClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var catalog: [SessionExcerpt]
    var catalogFailure = false
    var pendingCatalog: CheckedContinuation<[SessionExcerpt], any Error>?
    var holdCatalog = false
    var catalogCalls = 0
    func setCatalog(_ values: [SessionExcerpt], fails: Bool = false) {
        catalog = values; catalogFailure = fails
    }
    func suspendCatalog() { holdCatalog = true }
    func catalogWaiting() -> Bool { pendingCatalog != nil }
    func completeCatalog() { pendingCatalog?.resume(returning: catalog); pendingCatalog = nil; holdCatalog = false }
    var receivers: [String: @Sendable (SessionExcerpt) async -> Void] = [:]
    init(_ catalog: [SessionExcerpt]) { self.catalog = catalog }
    func sessions() async throws -> [SessionExcerpt] {
        catalogCalls += 1
        if catalogFailure { throw ClientError.noDaemon }
        if holdCatalog { return try await withCheckedThrowingContinuation { pendingCatalog = $0 } }
        return catalog
    }
    func snapshot(_ id: String) async throws -> SessionExcerpt { catalog.first { $0.id == id }! }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        receivers[id] = receive
        while !Task.isCancelled { try await Task.sleep(for: .seconds(1)) }
    }
    var renameFails = false
    var renameEvent: String?
    func setRename(fails: Bool = false, event: String? = nil) { renameFails = fails; renameEvent = event }
    func renameSession(_ id: String, name: String) async throws -> SessionExcerpt {
        if renameFails { throw ClientError.http(422) }
        var result = catalog.first { $0.id == id }!
        result.title = name
        if let renameEvent {
            var newer = result
            newer.title = renameEvent
            await receivers[id]?(newer)
        }
        return result
    }
    func ready(_ id: String) -> Bool { receivers[id] != nil }
    func emit(_ value: SessionExcerpt) async { await receivers[value.id]?(value) }
}

@MainActor struct SessionOwnershipTests {
    private func session(_ id: String, text: String = "Original", model: String = "original") -> SessionExcerpt {
        SessionExcerpt(id: id, title: id, sourceTitle: "Test", model: model, thinking: "high", workspace: "Test", date: "",
                       messages: [TranscriptMessage(id: "message", role: "assistant", text: text, tools: [])])
    }
    private func waitForAttachment(_ id: String, client: SnapshotClient) async throws {
        for _ in 0..<100 {
            if await client.ready(id) { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        Issue.record("Session did not attach")
    }

    @Test func snapshotsUpdateServerDataAndPreserveLocalUI() async throws {
        let initial = session("a")
        let client = SnapshotClient([initial])
        let store = SessionStore(fixture: Fixture(sessions: [initial]), client: client)
        store.ui.draft = "My draft"
        store.ui.attachments = ["image.png"]
        store.ui.workspace.scratchpad = "My notes"
        store.ui.palette = true
        store.attach()
        defer { store.detach() }
        try await waitForAttachment("a", client: client)
        await client.emit(session("a", text: "Server update", model: "server-model"))
        #expect(store.messages.map(\.text) == ["Server update"])
        #expect(store.model == "server-model")
        #expect(store.ui.draft == "My draft")
        #expect(store.ui.attachments == ["image.png"])
        #expect(store.ui.workspace.scratchpad == "My notes")
        #expect(store.ui.palette)
        store.previewModel("Demo model")
        store.previewThinking("low")
        store.previewInteractions([.approval()])
        store.send()
        store.replay()
        #expect(store.model == "server-model")
        #expect(store.thinking == "high")
        #expect(store.messages.map(\.text) == ["Server update"])
        #expect(store.ui.draft == "My draft")
        #expect(store.interactions.count == 0)
    }

    @Test func staleCallbacksCannotReplaceSelectedOrDetachedSnapshot() async throws {
        let catalog = [session("a"), session("b")]
        let client = SnapshotClient(catalog)
        let store = SessionStore(fixture: Fixture(sessions: catalog), client: client)
        store.attach()
        try await waitForAttachment("a", client: client)
        store.select("b")
        try await waitForAttachment("b", client: client)
        await client.emit(session("a", text: "Late A"))
        #expect(store.selectedID == "b")
        #expect(store.messages.map(\.text) == ["Original"])
        store.detach()
        await client.emit(session("b", text: "Late B"))
        #expect(store.messages.map(\.text) == ["Original"])
    }

    @Test func catalogRefreshPreservesSelectionTranscriptAndWorkspace() async throws {
        let initial = session("a")
        let client = SnapshotClient([initial])
        let store = SessionStore(fixture: Fixture(sessions: [initial]), client: client)
        store.ui.draft = "Unsent"
        store.ui.workspace.scratchpad = "Notes"
        var renamed = initial
        renamed.title = "New name"
        renamed.messages = []
        await client.setCatalog([session("b"), renamed])
        await store.refreshSessions()
        #expect(store.sessions.map(\.id) == ["b", "a"])
        #expect(store.selectedID == "a")
        #expect(store.selected?.title == "New name")
        #expect(store.messages.map(\.text) == ["Original"])
        #expect(store.ui.draft == "Unsent")
        #expect(store.ui.workspace.scratchpad == "Notes")
        // A missing row must not close the open session or discard its local work.
        await client.setCatalog([session("b")])
        await store.refreshSessions()
        #expect(store.sessions.map(\.id) == ["b"])
        #expect(store.selectedID == "a")
        #expect(store.messages.map(\.text) == ["Original"])
        #expect(store.ui.draft == "Unsent")
        #expect(store.ui.workspace.scratchpad == "Notes")
    }

    @Test func catalogFailureKeepsConnectedTranscriptAndCanRetry() async throws {
        let initial = session("a")
        let client = SnapshotClient([initial])
        let store = SessionStore(fixture: Fixture(sessions: [initial]), client: client)
        store.attach()
        defer { store.detach() }
        try await waitForAttachment("a", client: client)
        await client.emit(initial)
        await client.setCatalog([], fails: true)
        await store.refreshSessions()
        #expect(store.connectionState == .connected)
        #expect(store.sessions.map(\.id) == ["a"])
        #expect(store.catalogError == ClientError.noDaemon.localizedDescription)
        await client.setCatalog([initial, session("b")])
        await store.refreshSessions()
        #expect(store.catalogError == nil)
        #expect(store.sessions.map(\.id) == ["a", "b"])
    }

    @Test func renameDuringRefreshWinsOverOldCatalogAndRequestsCoalesce() async throws {
        let initial = session("a")
        let client = SnapshotClient([initial])
        let store = SessionStore(fixture: Fixture(sessions: [initial]), client: client)
        store.attach()
        defer { store.detach() }
        try await waitForAttachment("a", client: client)
        await client.emit(initial)
        await client.suspendCatalog()
        let refresh = Task { await store.refreshSessions() }
        for _ in 0..<100 {
            if await client.catalogWaiting() { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(await client.catalogWaiting())
        await store.refreshSessions()
        var renamed = initial
        renamed.title = "Live rename"
        await client.emit(renamed)
        await client.completeCatalog()
        await refresh.value
        #expect(store.selected?.title == "Live rename")
        #expect(store.sessions.map(\.title) == ["Live rename"])
        #expect(await client.catalogCalls == 1)
        #expect(store.connectionState == .connected)
    }

    @Test func renameAcknowledgementPreservesTranscriptAndDraft() async throws {
        let initial = session("a")
        let client = SnapshotClient([initial])
        let store = SessionStore(fixture: Fixture(sessions: [initial]), client: client)
        store.ui.draft = "Unsent"
        store.attach()
        defer { store.detach() }
        try await waitForAttachment("a", client: client)
        await client.emit(initial)
        try await store.renameSession("New name")
        #expect(store.selected?.title == "New name")
        #expect(store.sessions.map(\.title) == ["New name"])
        #expect(store.messages.map(\.text) == ["Original"])
        #expect(store.ui.draft == "Unsent")
        await client.setRename(event: "Newer external rename")
        try await store.renameSession("Delayed acknowledgement")
        #expect(store.selected?.title == "Newer external rename")
        #expect(store.sessions.map(\.title) == ["Newer external rename"])
    }

    @Test func rejectedRenamePreservesTitleAndDraft() async throws {
        let initial = session("a")
        let client = SnapshotClient([initial])
        let store = SessionStore(fixture: Fixture(sessions: [initial]), client: client)
        store.ui.draft = "Unsent"
        store.attach()
        defer { store.detach() }
        try await waitForAttachment("a", client: client)
        await client.emit(initial)
        await client.setRename(fails: true)
        do { try await store.renameSession("Rejected"); Issue.record("Expected rejection") }
        catch { }
        #expect(store.selected?.title == "a")
        #expect(store.ui.draft == "Unsent")
    }

    @Test func demoMutationsDoNotChangeRecordedSnapshot() {
        let initial = session("a")
        let store = SessionStore(fixture: Fixture(sessions: [initial]))
        store.ui.draft = "Hello"
        store.send()
        store.previewModel("Preview model")
        #expect(store.messages.map(\.role) == ["assistant", "user", "preview"])
        #expect(store.model == "Preview model")
        #expect(store.selected?.messages.map(\.text) == ["Original"])
        #expect(store.selected?.model == "original")
        #expect(store.ui.draft == "")
    }
}
