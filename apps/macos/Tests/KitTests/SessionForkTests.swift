import Foundation
import Testing
@testable import Kit

private actor ForkClient: SessionForkClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var requests: [WireForkSessionInput] = []
    var failNext = false
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws { }
    func failNextFork() { failNext = true }
    var firstTurnError: String?
    func failFirstTurn(_ error: String) { firstTurnError = error }
    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> ForkedSession {
        requests.append(input)
        if failNext { failNext = false; throw ClientError.disconnected }
        return ForkedSession(session: SessionExcerpt(id: "session_child\(requests.count)", title: input.name ?? "fork: Test", parentSessionID: id,
            sourceTitle: "Test", model: "m", thinking: "high", workspace: "Test", date: "", messages: []), firstTurnError: firstTurnError)
    }
}

@MainActor struct SessionForkTests {
    @Test func firstMessageIsSentWithTheFork() async throws {
        let client = ForkClient(), operation = SessionForkOperation()
        let child = try #require(await operation.submit(client: client, source: "parent", name: "  Child  ", message: "  Explore the other approach\n"))
        let requests = await client.requests
        #expect(requests.count == 1)
        #expect(requests[0].name == "Child")
        #expect(requests[0].prompt?.text == "Explore the other approach")
        #expect(requests[0].prompt?.attachmentIds == nil)
        #expect(requests[0].prompt?.annotationIds == nil)
        #expect(child.session.parentSessionID == "parent")
        #expect(child.firstTurnError == nil)
        #expect(operation.pending == false)
    }

    @Test func blankNameAndMessageForkWithServerDefaults() async throws {
        let client = ForkClient(), operation = SessionForkOperation()
        _ = try #require(await operation.submit(client: client, source: "parent", name: " ", message: " \n "))
        let requests = await client.requests
        #expect(requests.count == 1)
        #expect(requests[0].name == nil)
        #expect(requests[0].prompt == nil)
    }

    @Test func failedForkReportsErrorAndSubmitsEditedInputAgain() async throws {
        let client = ForkClient(), operation = SessionForkOperation()
        await client.failNextFork()
        #expect(await operation.submit(client: client, source: "parent", name: "Child", message: "First") == nil)
        #expect(operation.error == ClientError.disconnected.localizedDescription)
        _ = try #require(await operation.submit(client: client, source: "parent", name: "Child", message: "Edited"))
        let requests = await client.requests
        #expect(requests.map { $0.prompt?.text } == ["First", "Edited"])
        #expect(operation.error == nil)
    }

    @Test func firstTurnErrorStillReturnsThePublishedFork() async throws {
        let client = ForkClient(), operation = SessionForkOperation()
        await client.failFirstTurn("session is unavailable")
        let forked = try #require(await operation.submit(client: client, source: "parent", name: "", message: "Explore"))
        #expect(forked.session.id == "session_child1")
        #expect(forked.firstTurnError == "session is unavailable")
        #expect(operation.error == nil)
    }

    @Test func childWindowPresentsFirstTurnErrorAsFailedSubmission() {
        let child = SessionExcerpt(id: "session_child1", title: "fork: Test", parentSessionID: "parent",
            sourceTitle: "Test", model: "m", thinking: "high", workspace: "Test", date: "", messages: [])
        let state = SessionStore(fixture: Fixture(sessions: [child]), client: ForkClient(), sessionID: child.id)
        ForkFirstTurnFailures.shared.record(.init(message: "Explore the other approach", error: "session is unavailable"),
                                            for: SessionIdentity(server: state.serverID, session: child.id))
        state.attach()
        defer { state.detach() }
        #expect(state.ui.draft == "Explore the other approach")
        #expect(state.operations.submission == .failed("session is unavailable"))
        #expect(ForkFirstTurnFailures.shared.take(SessionIdentity(server: state.serverID, session: child.id)) == nil)
    }

    @Test func oversizedFirstMessageIsRejectedWithoutRequest() async {
        let client = ForkClient(), operation = SessionForkOperation()
        _ = await operation.submit(client: client, source: "parent", name: "", message: String(repeating: "x", count: SessionForkOperation.maxMessageBytes + 1))
        #expect(operation.error == "Use a message without NUL characters, up to 128 KiB.")
        #expect(await client.requests.isEmpty)
    }

    @Test func invalidNameIsRejectedWithoutRequest() async {
        let client = ForkClient(), operation = SessionForkOperation()
        _ = await operation.submit(client: client, source: "parent", name: String(repeating: "x", count: 257))
        #expect(operation.error == "Use a name without control characters, up to 256 bytes.")
        #expect(await client.requests.isEmpty)
    }
}
