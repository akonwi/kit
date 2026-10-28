import Foundation
import Testing
@testable import Kit

/// A session server that streams one session and records interaction responses.
private actor RespondingClient: ComposerClient {
    nonisolated let serverID = "test"
    nonisolated let isDemo = false
    let catalog: [SessionExcerpt]
    private(set) var responses: [String] = []
    /// How many times the session's stream has been opened.
    private(set) var watches = 0
    private(set) var snapshots = 0
    /// Failures the next responses meet, in order.
    private var failures: [Error] = []
    /// A stream opened while this is set delivers it and then stops for good.
    private var breakingWith: SessionExcerpt?
    private var receiver: (@Sendable (SessionExcerpt) async -> Void)?
    init(_ catalog: [SessionExcerpt]) { self.catalog = catalog }
    func failNextResponse(with error: Error) { failures.append(error) }
    func breakNextWatch(after value: SessionExcerpt) { breakingWith = value }
    func sessions() async throws -> [SessionExcerpt] { catalog }
    func snapshot(_ id: String) async throws -> SessionExcerpt { snapshots += 1; return catalog.first { $0.id == id }! }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
        watches += 1
        if let value = breakingWith {
            breakingWith = nil
            await receive(value)
            throw ClientError.invalidPayload
        }
        receiver = receive
        while !Task.isCancelled { try await Task.sleep(for: .seconds(1)) }
    }
    func ready() -> Bool { receiver != nil }
    func emit(_ value: SessionExcerpt) async { await receiver?(value) }
    func models() async throws -> [WireModelCapability] { [] }
    func files(_ session: String) async throws -> [String] { [] }
    func configure(_ session: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult { throw ClientError.http(409) }
    func upload(_ session: String, filename: String, data: Data) async throws -> WireAttachmentInfo { throw ClientError.http(503) }
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution { .init(attachments: [], missingAttachmentIds: ids) }
    func respond(_ session: String, input: WireInteractionResponse) async throws {
        if !failures.isEmpty { throw failures.removeFirst() }
        responses.append(input.value ?? "")
    }
}

@MainActor struct InteractionResponseTests {
    private func session(asking requests: [String] = []) -> SessionExcerpt {
        var session = SessionExcerpt(id: "a", title: "a", sourceTitle: "Test", model: "model", thinking: "high", workspace: "Test", date: "",
                                     messages: [TranscriptMessage(id: "message", role: "assistant", text: "Asking", tools: [])])
        session.pendingInteractions = requests.enumerated().map { index, id in
            WireInteractionRequest(id: id, sessionId: "a", turnId: "t", toolCallId: "c\(index)", kind: .value1,
                                   title: "Your name?", createdAt: "2026-10-12T00:00:0\(index)Z")
        }
        return session
    }

    /// A store watching session "a", which asks for `requests`.
    private func asking(_ requests: [String]) async throws -> (SessionStore, RespondingClient) {
        let client = RespondingClient([session()])
        let store = SessionStore(fixture: Fixture(sessions: [session()]), client: client)
        store.attach()
        try await eventually { await client.ready() }
        await client.emit(session(asking: requests))
        try await eventually { store.dockedInteraction?.request?.id == requests.first }
        return (store, client)
    }

    private func answer(_ store: SessionStore, with text: String) throws {
        let flow = try #require(store.dockedInteraction)
        flow.answers[0].text = text
        store.finishInteraction(cancelled: false)
    }

    @Test func anAnswerKeepsTheSessionConnected() async throws {
        let (store, client) = try await asking(["interaction_name"])
        defer { store.detach() }
        let flow = try #require(store.dockedInteraction)

        try answer(store, with: "Ada")
        try await eventually { await client.responses == ["Ada"] }

        // The answered request shows that it was sent while the session's
        // stream, still open, reports what happens next.
        #expect(store.dockedInteraction === flow)
        #expect(flow.submitting)
        #expect(store.connectionState == .connected)
        #expect(await client.watches == 1)
        #expect(await client.snapshots == 0)

        // The stream settles it, and the composer returns.
        await client.emit(session())
        try await eventually { store.dockedInteraction == nil }
        #expect(store.connectionState == .connected)
        #expect(await client.watches == 1)
    }

    @Test func theNextRequestOnTheStreamReplacesTheAnsweredOne() async throws {
        let (store, client) = try await asking(["interaction_name"])
        defer { store.detach() }

        try answer(store, with: "Ada")
        try await eventually { await client.responses == ["Ada"] }
        await client.emit(session(asking: ["interaction_follow_up"]))
        try await eventually { store.dockedInteraction?.request?.id == "interaction_follow_up" }
        let next = try #require(store.dockedInteraction)
        #expect(!next.submitting)

        try answer(store, with: "Lovelace")
        try await eventually { await client.responses == ["Ada", "Lovelace"] }
        #expect(store.connectionState == .connected)
        #expect(await client.watches == 1)
    }

    @Test func aFailedAnswerStaysDockedToSendAgain() async throws {
        let (store, client) = try await asking(["interaction_name"])
        defer { store.detach() }
        let flow = try #require(store.dockedInteraction)
        await client.failNextResponse(with: ClientError.http(503))

        try answer(store, with: "Ada")
        try await eventually { flow.error != nil }
        #expect(store.dockedInteraction === flow)
        #expect(!flow.submitting)
        #expect(flow.answers[0].text == "Ada")
        #expect(store.connectionState == .connected)
        #expect(await client.watches == 1)

        store.finishInteraction(cancelled: false)
        try await eventually { await client.responses == ["Ada"] }
        #expect(flow.error == nil)
        #expect(await client.watches == 1)
    }

    @Test func anAnswerReconnectsAStreamThatGaveUp() async throws {
        let client = RespondingClient([session()])
        let store = SessionStore(fixture: Fixture(sessions: [session()]), client: client)
        defer { store.detach() }
        await client.breakNextWatch(after: session(asking: ["interaction_name"]))
        store.attach()
        try await eventually { store.watchStopped }
        #expect(store.dockedInteraction?.request?.id == "interaction_name")

        // Its resolution can only arrive on a stream, so answering reopens one.
        try answer(store, with: "Ada")
        try await eventually { await client.responses == ["Ada"] }
        try await eventually { await client.ready() }
        #expect(await client.watches == 2)
        await client.emit(session())
        try await eventually { store.dockedInteraction == nil }
        #expect(store.connectionState == .connected)
    }
}
