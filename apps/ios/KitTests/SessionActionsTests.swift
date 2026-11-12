import Foundation
import KitClient
import Testing
@testable import Kit

@MainActor
struct SessionActionsTests {
    static let identity = SessionIdentity(server: "studio", session: "s")

    nonisolated static func snapshot(model: String = "openai/gpt-5.5", thinking: String = "high", running: String? = nil,
                                     compacting: String? = nil, temporary: Bool = false) -> SessionExcerpt {
        var session = SessionTesting.session("s")
        session.model = model
        session.thinking = thinking
        session.activeRunID = running
        session.activeCompactionID = compacting
        session.isTemporary = temporary
        session.configurationRevision = 4
        return session
    }

    nonisolated static func model(_ id: String, provider: String, levels: [WireThinkingLevel]) -> WireModelCapability {
        WireModelCapability(id: id, name: id.split(separator: "/").last.map(String.init) ?? id, provider: provider, api: "responses",
                            contextWindow: 200_000, maxInputTokens: nil, maxOutputTokens: nil, thinkingLevels: levels,
                            inputs: nil, available: true)
    }

    static func actions(_ client: FakeActionsClient = FakeActionsClient(), live: Bool = true,
                        snapshot: SessionExcerpt = snapshot()) async -> SessionActions {
        let actions = SessionActions(identity: identity, client: client)
        actions.live = live
        actions.reconcile(snapshot)
        await actions.loadModels()
        return actions
    }

    @Test func aRunningTurnLocksTheModelButNotTheThinkingLevel() async {
        let actions = await Self.actions(snapshot: Self.snapshot(running: "t1"))
        #expect(actions.modelLockedReason == "Changes when the turn ends")
        #expect(actions.thinkingLockedReason == nil)
        #expect(actions.compactionUnavailableReason == "Available when the turn ends")
    }

    @Test func nothingChangesWhileTheStreamIsDown() async {
        let actions = await Self.actions(live: false)
        #expect(actions.modelLockedReason == "Reconnect to change")
        #expect(actions.thinkingLockedReason == "Reconnect to change")
        #expect(actions.compactionUnavailableReason == "Reconnect to compact")
        #expect(actions.forkUnavailableReason == "Reconnect to fork")
    }

    @Test func modelsAreGroupedByProviderInCatalogOrder() async {
        let actions = await Self.actions()
        #expect(actions.modelGroups.map(\.provider) == ["openai", "anthropic"])
        #expect(actions.modelGroups.map { $0.models.map(\.id) } == [["openai/gpt-5.5", "openai/gpt-5-mini"], ["anthropic/claude-opus-4"]])
        #expect(actions.modelName == "gpt-5.5")
        #expect(actions.thinkingLevels == ["low", "medium", "high"])
    }

    @Test func switchingModelsKeepsTheThinkingLevelWhenTheNewModelOffersIt() async {
        let client = FakeActionsClient()
        let actions = await Self.actions(client)
        actions.changeModel("anthropic/claude-opus-4")
        await SessionComposerTests.settle()
        actions.changeModel("openai/gpt-5-mini")
        await SessionComposerTests.settle()
        #expect(client.configured.map(\.model) == ["anthropic/claude-opus-4", "openai/gpt-5-mini"])
        #expect(client.configured.map(\.thinkingLevel) == [.value4, .value0])
        #expect(client.configured.map(\.expectedRevision) == [4, 4])
    }

    @Test func aRefusedChangeSaysSoAndReadsTheSessionAgain() async {
        let client = FakeActionsClient()
        var delivered: [SessionExcerpt] = []
        let actions = await Self.actions(client)
        actions.deliver = { delivered.append($0) }
        actions.changeThinking("low")
        await SessionComposerTests.settle()
        #expect(client.configured.map(\.thinkingLevel) == [.value2])
        #expect(actions.notice == .init(text: "Couldn’t change the model. \(ClientError.http(409).localizedDescription)", tone: .danger))
        #expect(delivered.count == 1)
        actions.dismissNotice()
        #expect(actions.notice == nil)
    }

    @Test func aCompactionSaysHowItEnded() async {
        let client = FakeActionsClient()
        let actions = await Self.actions(client)
        actions.compact()
        await SessionComposerTests.settle()
        #expect(client.compactions.count == 1)
        #expect(actions.notice == .init(text: "Context compacted.", tone: .info))

        client.compacted = false
        actions.compact()
        await SessionComposerTests.settle()
        #expect(client.compactions.count == 2)
        #expect(Set(client.compactions).count == 2)
        #expect(actions.notice == .init(text: "Not enough turns to compact yet.", tone: .info))
    }

    @Test func aFailedCompactionRetriesTheSameOperation() async {
        let client = FakeActionsClient()
        client.compactionError = ClientError.disconnected
        let actions = await Self.actions(client)
        actions.compact()
        await SessionComposerTests.settle()
        #expect(actions.notice?.tone == .danger)
        #expect(actions.notice?.text.hasPrefix("Couldn’t compact. ") == true)

        client.compactionError = nil
        actions.compact()
        await SessionComposerTests.settle()
        #expect(client.compactions.count == 2)
        #expect(Set(client.compactions).count == 1)
        #expect(actions.notice == .init(text: "Context compacted.", tone: .info))
    }

    @Test func compactionElsewhereShowsAsCompacting() async {
        let actions = await Self.actions(snapshot: Self.snapshot(compacting: "compact_1"))
        #expect(actions.compacting)
        #expect(actions.compactionUnavailableReason == "Compacting…")
        #expect(actions.modelLockedReason == "Wait for compaction")
    }

    @Test func aRenameNeedsAUsableName() async {
        let client = FakeActionsClient()
        let actions = await Self.actions(client)
        #expect(await actions.rename(to: "  ") == false)
        #expect(actions.renameError == "Use a name without control characters, up to 256 bytes.")
        #expect(client.renamed.isEmpty)
        #expect(await actions.rename(to: " Fix SSE reconnect ") == true)
        #expect(client.renamed == ["Fix SSE reconnect"])
        #expect(actions.renameError == nil)
    }

    @Test func aForkWhoseFirstMessageDidNotStartHandsItToTheFork() async {
        let client = FakeActionsClient()
        client.firstTurnError = "Provider is rate limited"
        let actions = await Self.actions(client)
        let child = await actions.fork(name: "Try the other fix", message: " Retry with backoff ")
        #expect(child == SessionIdentity(server: "studio", session: "child"))
        #expect(client.forks.map(\.name) == ["Try the other fix"])
        let failure = ForkFirstTurnFailures.shared.take(SessionIdentity(server: "studio", session: "child"))
        #expect(failure == .init(message: "Retry with backoff", error: "Provider is rate limited"))
    }

    @Test func temporarySessionsCanNotBeForkedAndAreDisposed() async {
        let client = FakeActionsClient()
        let actions = await Self.actions(client, snapshot: Self.snapshot(temporary: true))
        #expect(actions.forkUnavailableReason == "Temporary sessions can’t be forked")
        #expect(await actions.delete())
        #expect(client.disposed == ["s"])
        #expect(client.deleted.isEmpty)
    }

    @Test func deletingAsksInTheSessionsOwnWords() async {
        var named = Self.snapshot()
        named.title = "Fix SSE reconnect"
        #expect(await Self.actions(snapshot: named).deleteConfirmation == .init(
            item: "Delete…", title: "Delete session?",
            message: "“Fix SSE reconnect” and its saved history will be permanently deleted. This can’t be undone.",
            button: "Delete session"))

        var temporary = Self.snapshot(temporary: true)
        temporary.title = "Scratch"
        #expect(await Self.actions(snapshot: temporary).deleteConfirmation == .init(
            item: "Dispose…", title: "Dispose temporary session?",
            message: "“Scratch” will be removed from the server’s memory and any active work will be cancelled. This can’t be undone.",
            button: "Dispose session"))
    }

    @Test func aSessionInUseIsNotDeleted() async {
        let client = FakeActionsClient()
        client.deleteError = ClientError.http(409)
        let actions = await Self.actions(client)
        #expect(await actions.delete() == false)
        #expect(actions.deleteError == "This session is in use and can’t be deleted. Stop its work and try again.")
    }

    @Test func aSessionAlreadyGoneCountsAsDeleted() async {
        let client = FakeActionsClient()
        client.deleteError = ClientError.http(404)
        let actions = await Self.actions(client)
        #expect(await actions.delete())
        #expect(actions.deleteError == nil)
    }

    @Test func tokenCountsAreWrittenShort() {
        #expect(SessionContext.short(950) == "950")
        #expect(SessionContext.short(82_000) == "82k")
        #expect(SessionContext.short(48_200) == "48.2k")
        #expect(SessionContext.short(200_000) == "200k")
        #expect(SessionContext.short(999_700) == "1M")
        #expect(SessionContext.short(1_240_000) == "1.24M")
        var session = Self.snapshot()
        session.contextTokens = 82_000
        session.contextWindow = 200_000
        #expect(SessionContext(session)?.tokensLabel == "82k / 200k")
        #expect(SessionContext(session)?.percent == 41)
    }
}

final class FakeActionsClient: SessionActionsClient, @unchecked Sendable {
    let serverID = "studio"
    let isDemo = false
    private let lock = NSLock()
    private var _configured: [WireConfigureSessionInput] = []
    private var _compactions: [String] = []
    private var _renamed: [String] = []
    private var _forks: [WireForkSessionInput] = []
    private var _deleted: [String] = []
    private var _disposed: [String] = []
    private var _responses: [WireInteractionResponse] = []
    var respondError: (any Error)?
    var compacted = true
    var compactionError: (any Error)?
    var firstTurnError: String?
    var deleteError: (any Error)?

    var configured: [WireConfigureSessionInput] { lock.withLock { _configured } }
    var compactions: [String] { lock.withLock { _compactions } }
    var renamed: [String] { lock.withLock { _renamed } }
    var forks: [WireForkSessionInput] { lock.withLock { _forks } }
    var deleted: [String] { lock.withLock { _deleted } }
    var disposed: [String] { lock.withLock { _disposed } }
    var responses: [WireInteractionResponse] { lock.withLock { _responses } }

    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt { SessionActionsTests.snapshot() }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}

    func models() async throws -> [WireModelCapability] {
        [SessionActionsTests.model("openai/gpt-5.5", provider: "openai", levels: [.value2, .value3, .value4]),
         SessionActionsTests.model("anthropic/claude-opus-4", provider: "anthropic", levels: [.value0, .value4]),
         SessionActionsTests.model("openai/gpt-5-mini", provider: "openai", levels: [])]
    }
    /// Records the change, then refuses it: a result needs a full session.
    func configure(_ session: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult {
        lock.withLock { _configured.append(input) }
        throw ClientError.http(409)
    }
    func files(_ session: String) async throws -> [String] { [] }
    func upload(_ session: String, filename: String, data: Data) async throws -> WireAttachmentInfo { throw ClientError.http(503) }
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution {
        .init(attachments: [], missingAttachmentIds: ids)
    }
    func respond(_ session: String, input: WireInteractionResponse) async throws {
        if let respondError { throw respondError }
        lock.withLock { _responses.append(input) }
    }

    func compactSession(_ id: String, input: WireCompactSessionInput) async throws -> WireCompactSessionResult {
        lock.withLock { _compactions.append(input.operationId) }
        if let compactionError { throw compactionError }
        return WireCompactSessionResult(operationId: input.operationId, compacted: compacted, checkpointId: nil, eventStreamId: "e")
    }
    func renameSession(_ id: String, name: String) async throws -> SessionExcerpt {
        lock.withLock { _renamed.append(name) }
        var session = SessionActionsTests.snapshot()
        session.title = name
        return session
    }
    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> ForkedSession {
        lock.withLock { _forks.append(input) }
        var child = SessionTesting.session("child")
        child.parentSessionID = id
        return ForkedSession(session: child, firstTurnError: firstTurnError)
    }
    func deleteSession(_ id: String) async throws {
        if let deleteError { throw deleteError }
        lock.withLock { _deleted.append(id) }
    }
    func disposeSession(_ id: String) async throws {
        lock.withLock { _disposed.append(id) }
    }

    private var _reloads: [String] = []
    private var _plugins: [WirePluginCommandInput] = []
    var reloads: [String] { lock.withLock { _reloads } }
    var plugins: [WirePluginCommandInput] { lock.withLock { _plugins } }
    var reloadResult = WireReloadSessionResult(sessionId: "s", eventStreamId: "e", sources: nil, diagnostics: nil, warnings: nil)
    var reloadError: (any Error)?
    var pluginError: (any Error)?
    /// Holds plugin runs until released, when set.
    var pluginGate: AsyncStream<Void>?

    private var _folderChanges: [WireChangeCWDInput] = []
    var folderChanges: [WireChangeCWDInput] { lock.withLock { _folderChanges } }
    /// Thrown by the next folder changes, one each, before they succeed.
    var folderErrors: [any Error] = []

    func changeDirectory(_ session: String, input: WireChangeCWDInput) async throws -> SessionExcerpt {
        let error: (any Error)? = lock.withLock {
            _folderChanges.append(input)
            return folderErrors.isEmpty ? nil : folderErrors.removeFirst()
        }
        if let error { throw error }
        return SessionActionsTests.snapshot()
    }
    func vcs(_ session: String) async throws -> WireSessionVCSStatus { throw ClientError.http(404) }
    func watchVCS(_ session: String, receive: @escaping @Sendable (WireSessionVCSStatus) async -> Void) async throws {}

    func reloadSession(_ id: String) async throws -> WireReloadSessionResult {
        lock.withLock { _reloads.append(id) }
        if let reloadError { throw reloadError }
        return reloadResult
    }
    func executePluginCommand(_ session: String, input: WirePluginCommandInput) async throws {
        lock.withLock { _plugins.append(input) }
        if let pluginGate { for await _ in pluginGate { break } }
        if let pluginError { throw pluginError }
    }
}

struct SelectTextTests {
    @Test func inlineMarkdownReadsAsItsText() {
        let markdown = "Use **`lastEventID`** to [resume](https://example.com).\n\n- keep it\n- in *memory*"
        #expect(SelectTextSheet.plainText(markdown) == "Use lastEventID to resume.\n\n- keep it\n- in memory")
    }

    @Test func fencedCodeKeepsItsLines() {
        let markdown = "Try **this**:\n\n```swift\nlet `id` = *last*\n    resume(id)\n```\nDone."
        #expect(SelectTextSheet.plainText(markdown) == "Try this:\n\nlet `id` = *last*\n    resume(id)\nDone.")
        #expect(SelectTextSheet.segments(markdown) == [
            .init(text: "Try this:\n", code: false),
            .init(text: "let `id` = *last*\n    resume(id)", code: true, language: "swift"),
            .init(text: "Done.", code: false),
        ])
    }
}
