import Foundation
import Testing
@testable import Kit

private actor ComposerMock: ComposerClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    var uploads = 0
    var failUpload = true
    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.disconnected }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    func models() async throws -> [WireModelCapability] { [] }
    func files(_ session: String) async throws -> [String] { ["README.md"] }
    func configure(_ session: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult { throw ClientError.http(409) }
    func respond(_ session: String, input: WireInteractionResponse) async throws {}
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution { .init(attachments: [], missingAttachmentIds: ids) }
    func upload(_ session: String, filename: String, data: Data) async throws -> WireAttachmentInfo {
        uploads += 1
        if failUpload { failUpload = false; throw ClientError.http(503) }
        try await Task.sleep(for: .milliseconds(30))
        return .init(id: "attachment_\(uploads)", sessionId: session, filename: filename, mediaType: "text/plain",
            size: Int64(data.count), sha256: String(repeating: "a", count: 64), createdAt: "", width: nil, height: nil)
    }
}

@MainActor struct ComposerClientTests {
    private func wait(_ condition: @MainActor () -> Bool) async throws {
        for _ in 0..<100 where !condition() { try await Task.sleep(for: .milliseconds(10)) }
        #expect(condition())
    }
    @Test func attachmentsRetainIdentityAndBytesAcrossFailureRetryAndSessionSwitch() async throws {
        let client = ComposerMock(), ui = SessionUIState(demo: false)
        let first = SessionIdentity(server: "test", session: "first"), second = SessionIdentity(server: "test", session: "second")
        let uploads = ui.uploads
        uploads.add(filename: "note.txt", data: Data("hello".utf8), client: client, session: "first")
        try await wait { uploads.items.first?.error != nil }
        let id = try #require(uploads.items.first?.id)
        #expect(uploads.items.first?.data == Data("hello".utf8))
        ui.switchSession(from: first, to: second, demo: false)
        #expect(ui.uploads.items.isEmpty)
        uploads.retry(id, client: client, session: "first")
        try await wait { uploads.ready }
        #expect(uploads.items.first?.uploaded?.sessionId == "first")
        ui.switchSession(from: second, to: first, demo: false)
        #expect(ui.uploads.items.first?.id == id)
        ui.uploads.acknowledge(["attachment_2"])
        #expect(ui.uploads.items.isEmpty)
    }
    @Test func removingAnUploadPreventsLateCompletionFromRestoringIt() async throws {
        let client = ComposerMock(), uploads = ComposerAttachments()
        uploads.add(filename: "note.txt", data: Data("hello".utf8), client: client, session: "first")
        try await wait { uploads.items.first?.error != nil }
        let id = try #require(uploads.items.first?.id)
        uploads.retry(id, client: client, session: "first")
        uploads.remove(id)
        try await Task.sleep(for: .milliseconds(60))
        #expect(uploads.items.isEmpty)
        #expect(uploads.readyIDs == [])
    }
    @Test func guidedResponsesUseOptionIdentitiesEvenWhenLabelsMatch() throws {
        let request = WireInteractionRequest(id: "interaction_test", sessionId: "s", runId: "r", toolCallId: "t",
            kind: try #require(WireInteractionKind(rawValue: "guided")), title: "Plan", detail: nil, options: nil,
            questions: [.init(id: "question_a", prompt: "Choose", detail: nil,
                kind: try #require(WireInteractionQuestionKind(rawValue: "select")), required: true,
                options: [.init(id: "option_a", label: "Same label", detail: nil), .init(id: "option_b", label: "Same label", detail: nil)]),
                .init(id: "question_b", prompt: "More?", detail: nil, kind: try #require(WireInteractionQuestionKind(rawValue: "text")), required: false, options: nil)],
            createdAt: "2026-09-13T00:00:00Z")
        let flow = try InteractionFlow(request: request)
        #expect(flow.submitStep(option: "option_b") == false)
        #expect(flow.advance(skip: true))
        let response = try #require(flow.response(cancelled: false))
        #expect(response.requestId == "interaction_test")
        #expect(response.answers?["question_a"]?.optionIds == ["option_b"])
        #expect(response.answers?["question_b"]?.skipped == true)
        let cancellation = try #require(flow.response(cancelled: true))
        #expect(cancellation.cancelled == true)
        #expect(cancellation.answers == nil)
    }
    @Test func attachmentIdentityDeduplicationPreservesPresentationOrder() {
        #expect(ComposerAttachments.uniqueIDs(["attachment_z", "attachment_a", "attachment_z", "attachment_b"]) ==
            ["attachment_z", "attachment_a", "attachment_b"])
    }
    @Test func configurationConflictRefreshesAuthoritativeMetadata() async throws {
        let configuration = ComposerConfiguration()
        var session = try Fixture.load().sessions[0]
        session.configurationRevision = 7
        var refreshes = 0
        await configuration.change(client: ComposerMock(), session: session, model: "provider/model", thinking: "medium") { refreshes += 1 }
        #expect(refreshes == 1)
        #expect(configuration.error != nil)
        #expect(configuration.changing == false)
    }
}
