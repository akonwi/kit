import Foundation
import Testing
@testable import Kit

private actor SiblingClient: SessionCreationClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var requests: [WireCreateSessionInput] = []
    var failure: (any Error)?
    func fail(_ error: any Error) { failure = error }
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func models() async throws -> [WireModelCapability] { [] }
    func createSession(_ input: WireCreateSessionInput) async throws -> SessionExcerpt {
        requests.append(input)
        if let failure { throw failure }
        return SessionExcerpt(id: "session_sibling", title: "New session", sourceTitle: "New session", model: input.model,
            thinking: input.thinkingLevel ?? "", workspace: "project", cwd: input.cwd, date: "", messages: [])
    }
}

@MainActor struct NewSessionCommandTests {
    private static let source = SessionExcerpt(id: "session_source", title: "Source", sourceTitle: "Source",
        model: "provider/model", thinking: "high", workspace: "project", cwd: "/tmp/project", date: "", messages: [])

    @Test func newSessionReusesTheActiveSessionsDirectoryModelAndThinking() async throws {
        let client = SiblingClient()
        let state = SessionStore(fixture: Fixture(sessions: [Self.source]), client: client, sessionID: Self.source.id)
        #expect(state.canCreateSibling)
        let created = try #require(await state.createSibling())
        #expect(created.id == "session_sibling")
        let requests = await client.requests
        #expect(requests.count == 1)
        #expect(requests[0].cwd == "/tmp/project")
        #expect(requests[0].model == "provider/model")
        #expect(requests[0].thinkingLevel == "high")
        #expect(requests[0].temporary == false)
        #expect(requests[0].name == nil)
        #expect(state.siblingCreationPending == false)
    }

    @Test func failedCreationReportsFeedback() async throws {
        let client = SiblingClient()
        await client.fail(ClientError.disconnected)
        let state = SessionStore(fixture: Fixture(sessions: [Self.source]), client: client, sessionID: Self.source.id)
        #expect(await state.createSibling() == nil)
        #expect(state.feedback.items.map(\.title) == ["New session failed"])
        #expect(state.feedback.items.map(\.detail) == [ClientError.disconnected.localizedDescription])
    }
}
