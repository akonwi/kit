import Foundation
import KitClient
import Testing
@testable import Kit

@MainActor
struct SessionComposerTests {
    nonisolated static func snapshot(running: String? = nil, queue: WireFollowUpQueue? = nil,
                         commands: [PromptCommand]? = nil, observed: [String]? = nil) -> SessionExcerpt {
        var session = SessionTesting.session("s")
        session.activeRunID = running
        session.observedTurns = observed
        session.followUps = try? FollowUpState(queue ?? WireFollowUpQueue(count: 0, previews: [], annotationIds: nil))
        session.promptCommands = commands
        return session
    }

    static func composer(_ client: FakeComposerClient = FakeComposerClient(), live: Bool = true, snapshot: SessionExcerpt = snapshot()) -> SessionComposer {
        let composer = SessionComposer(session: "s", client: client)
        composer.live = live
        composer.reconcile(snapshot)
        return composer
    }

    /// Lets the operation's task finish.
    static func settle() async {
        for _ in 0..<20 { await Task.yield() }
        try? await Task.sleep(for: .milliseconds(20))
    }

    @Test func anEmptyDraftCanNotBeSent() {
        let composer = Self.composer()
        #expect(composer.action == .send(enabled: false))
        composer.draft = "  \n"
        #expect(composer.action == .send(enabled: false))
        composer.draft = "Run the tests"
        #expect(composer.action == .send(enabled: true))
    }

    @Test func aRunningTurnShowsStopUntilThereIsText() {
        let composer = Self.composer(snapshot: Self.snapshot(running: "t1"))
        #expect(composer.action == .stop(enabled: true))
        composer.draft = "Also check the 409 path"
        #expect(composer.action == .send(enabled: true))
        composer.draft = ""
        #expect(composer.action == .stop(enabled: true))
    }

    @Test func nothingIsSentWhileTheStreamIsDown() {
        let composer = Self.composer(live: false, snapshot: Self.snapshot(running: "t1"))
        #expect(composer.action == .stop(enabled: false))
        composer.draft = "Hello"
        #expect(composer.action == .send(enabled: false))
    }

    @Test func aSentDraftClearsOnceTheServerAcceptsIt() async {
        let client = FakeComposerClient()
        let composer = Self.composer(client)
        composer.draft = "Run the tests"
        composer.send()
        #expect(composer.action == .waiting)
        #expect(!composer.editable)
        await Self.settle()
        #expect(client.submitted.map(\.text) == ["Run the tests"])
        #expect(composer.draft == "")
        #expect(composer.editable)
    }

    @Test func aRefusalKeepsTheDraftUntilItIsEdited() async {
        let client = FakeComposerClient()
        client.submitError = ClientError.http(429)
        let composer = Self.composer(client)
        composer.draft = "Run the tests"
        composer.send()
        await Self.settle()
        #expect(composer.draft == "Run the tests")
        #expect(composer.notice?.hasPrefix("Didn’t send. ") == true)
        #expect(composer.action == .send(enabled: true))
        composer.draft = "Run the tests again"
        #expect(composer.notice == nil)
    }

    @Test func aLostAnswerChecksTheSessionBeforeAnotherSend() async {
        let client = FakeComposerClient()
        client.submitError = ClientError.disconnected
        let composer = Self.composer(client)
        composer.draft = "Run the tests"
        composer.send()
        await Self.settle()
        #expect(client.snapshotReads == 1)
        #expect(composer.action == .send(enabled: true))
        #expect(composer.draft == "Run the tests")
    }

    @Test func stopAbortsTheRunningTurn() async {
        let client = FakeComposerClient()
        let composer = Self.composer(client, snapshot: Self.snapshot(running: "t1"))
        composer.stop()
        #expect(composer.action == .waiting)
        await Self.settle()
        #expect(client.aborted == ["t1"])
        // The run ends on the stream.
        composer.reconcile(Self.snapshot())
        #expect(composer.action == .send(enabled: false))
    }

    @Test func aHeldQueueBlocksSendingUntilItIsChanged() throws {
        let held = WireFollowUpQueue(count: 2, previews: ["a", "b"], annotationIds: nil,
                                     admissionFailure: WireFollowUpAdmissionFailure(message: "the provider rejected the API key"))
        let composer = Self.composer(snapshot: Self.snapshot(queue: held))
        composer.draft = "Hello"
        #expect(composer.held)
        #expect(composer.queue?.count == 2)
        #expect(composer.action == .send(enabled: false))
    }

    @Test func editMovesQueuedMessagesInFrontOfTheDraft() async {
        let client = FakeComposerClient()
        client.restored = [WirePromptInput(text: "first", attachmentIds: ["att-1"], annotationIds: nil),
                           WirePromptInput(text: "second", attachmentIds: nil, annotationIds: nil)]
        let queued = WireFollowUpQueue(count: 2, previews: ["first", "second"], annotationIds: nil)
        let composer = Self.composer(client, snapshot: Self.snapshot(running: "t1", queue: queued))
        composer.draft = "third"
        composer.changeQueue(.edit)
        await Self.settle()
        #expect(composer.draft == "first\n\nsecond\n\nthird")
        #expect(composer.restoredAttachmentIDs == ["att-1"])
    }

    @Test func sendNowNeedsARunningTurn() async {
        let client = FakeComposerClient()
        let queued = WireFollowUpQueue(count: 1, previews: ["first"], annotationIds: nil)
        let composer = Self.composer(client, snapshot: Self.snapshot(queue: queued))
        composer.changeQueue(.promote)
        await Self.settle()
        #expect(client.promotions == 0)
        composer.reconcile(Self.snapshot(running: "t1", queue: queued))
        composer.changeQueue(.promote)
        await Self.settle()
        #expect(client.promotions == 1)
    }

    @Test func aKnownPromptCommandIsSentAsACommand() async throws {
        let client = FakeComposerClient()
        let review = try JSONDecoder().decode(PromptCommand.self, from: Data(
            #"{"name":"review","description":"Review the diff","source":"project","location":"/r.md"}"#.utf8))
        let composer = Self.composer(client, snapshot: Self.snapshot(commands: [review]))
        composer.draft = "/review the cursor change"
        composer.send()
        await Self.settle()
        #expect(client.commands.map(\.name) == ["review"])
        #expect(client.commands.first?.args == "the cursor change")
        #expect(client.submitted.isEmpty)

        composer.draft = "/unknown"
        composer.send()
        #expect(composer.notice == "Didn’t send. Unknown prompt command: /unknown")
    }
    static func comment(_ id: UInt64, stale: Bool = false) throws -> FileAnnotation {
        try FileAnnotation(id: id, anchor: .init(kind: .value1, workspaceFile: nil, workingTreeDiff: .init(
            targetId: "difftarget_test", targetRevision: "diffrev_test", path: "client.go",
            fileRevision: "diff_file_test", side: "new", startLine: 4, endLine: 6)),
            body: "Why?", source: "", stale: stale, complete: true)
    }

    static func commented(_ notes: [FileAnnotation]) -> SessionExcerpt {
        var session = snapshot()
        session.annotations = notes
        return session
    }

    @Test func commentsGoWithTheNextMessage() async throws {
        let client = FakeComposerClient()
        let composer = Self.composer(client, snapshot: Self.commented([try Self.comment(3), try Self.comment(5)]))
        // Comments alone are enough to send.
        #expect(composer.action == .send(enabled: true))
        composer.draft = "Look at these two"
        composer.send()
        await Self.settle()
        #expect(client.submitted.map(\.text) == ["Look at these two"])
        #expect(client.submitted.map(\.annotationIds) == [[3, 5]])
    }

    @Test func aCommentWhoseCodeChangedHoldsTheSend() async throws {
        let client = FakeComposerClient()
        let composer = Self.composer(client, snapshot: Self.commented([try Self.comment(3, stale: true)]))
        composer.draft = "Look at this"
        composer.send()
        await Self.settle()
        #expect(client.submitted.isEmpty)
        #expect(composer.notice == "Didn’t send. The code under a comment changed. Select its lines again or delete it before sending.")
    }

    @Test func aCommentBeingWrittenHoldsTheSend() async throws {
        let client = FakeComposerClient()
        let note = try Self.comment(3)
        let composer = Self.composer(client, snapshot: Self.commented([note]))
        composer.annotations.begin(anchor: note.anchor, editing: note)
        composer.draft = "Look at this"
        composer.send()
        await Self.settle()
        #expect(client.submitted.isEmpty)
        #expect(composer.notice == "Didn’t send. Finish the comment you’re writing before sending.")
    }

    @Test func aPromptCommandTakesNoComments() async throws {
        let client = FakeComposerClient()
        var session = Self.commented([try Self.comment(3)])
        session.promptCommands = [try JSONDecoder().decode(PromptCommand.self, from: Data(
            #"{"name":"review","description":"Review the diff","source":"project","location":"/r.md"}"#.utf8))]
        let composer = Self.composer(client, snapshot: session)
        composer.draft = "/review"
        composer.send()
        await Self.settle()
        #expect(client.commands.isEmpty)
        #expect(composer.notice == "Didn’t send. Prompt commands do not accept attachments or comments.")
    }

    @Test func aCommentCanBeDeletedFromTheComposer() async throws {
        let client = FakeComposerClient()
        let composer = Self.composer(client, snapshot: Self.commented([try Self.comment(3)]))
        composer.removeComment(try Self.comment(3))
        await Self.settle()
        #expect(client.deletedAnnotations == [3])
    }
}

final class FakeComposerClient: SessionSendClient, @unchecked Sendable {
    let serverID = "studio"
    let isDemo = false
    private let lock = NSLock()
    private var _submitted: [WirePromptInput] = []
    private var _commands: [WirePromptCommandInput] = []
    private var _aborted: [String] = []
    private var _promotions = 0
    private var _snapshotReads = 0
    var submitError: (any Error)?
    var restored: [WirePromptInput] = []

    var submitted: [WirePromptInput] { lock.withLock { _submitted } }
    var commands: [WirePromptCommandInput] { lock.withLock { _commands } }
    var aborted: [String] { lock.withLock { _aborted } }
    var promotions: Int { lock.withLock { _promotions } }
    var snapshotReads: Int { lock.withLock { _snapshotReads } }

    private static let emptyQueue = WireFollowUpQueue(count: 0, previews: [], annotationIds: nil)

    func sessions() async throws -> [SessionExcerpt] { [] }
    func snapshot(_ id: String) async throws -> SessionExcerpt {
        lock.withLock { _snapshotReads += 1 }
        return SessionComposerTests.snapshot()
    }
    func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}

    func submit(_ session: String, input: WirePromptInput) async throws -> WirePromptSubmission {
        if let submitError { throw submitError }
        lock.withLock { _submitted.append(input) }
        return WirePromptSubmission(reservation: WireTurnReservation(sessionId: session, turnId: "t9"), queued: false, queue: Self.emptyQueue)
    }
    func submitPromptCommand(_ id: String, input: WirePromptCommandInput) async throws -> WirePromptSubmission {
        lock.withLock { _commands.append(input) }
        return WirePromptSubmission(reservation: WireTurnReservation(sessionId: id, turnId: "t9"), queued: false, queue: Self.emptyQueue)
    }
    func restoreFollowUps(_ session: String) async throws -> WireRestoreFollowUpsResult {
        WireRestoreFollowUpsResult(messages: restored, queue: Self.emptyQueue)
    }
    func promoteFollowUps(_ session: String) async throws -> WirePromoteFollowUpsResult {
        lock.withLock { _promotions += 1 }
        return WirePromoteFollowUpsResult(promoted: 1, queue: Self.emptyQueue)
    }
    func abort(_ session: String, turn: String) async throws {
        lock.withLock { _aborted.append(turn) }
    }

    private var _uploaded: [String] = []
    var uploadError: (any Error)?
    var uploaded: [String] { lock.withLock { _uploaded } }

    func models() async throws -> [WireModelCapability] { [] }
    func configure(_ session: String, input: WireConfigureSessionInput) async throws -> WireConfigureSessionResult {
        throw ClientError.http(409)
    }
    func files(_ session: String) async throws -> [String] { [] }
    func upload(_ session: String, filename: String, data: Data) async throws -> WireAttachmentInfo {
        if let uploadError { throw uploadError }
        let id = lock.withLock { _uploaded.append(filename); return "attachment_\(_uploaded.count)" }
        return WireAttachmentInfo(id: id, sessionId: session, filename: filename, mediaType: "text/plain",
                                  size: Int64(data.count), sha256: String(repeating: "0", count: 64),
                                  createdAt: "2026-10-31T00:00:00Z", width: nil, height: nil)
    }
    func resolveAttachments(_ session: String, ids: [String]) async throws -> WireAttachmentResolution {
        .init(attachments: [], missingAttachmentIds: ids)
    }
    func respond(_ session: String, input: WireInteractionResponse) async throws {}

    private var _deletedAnnotations: [UInt64] = []
    var deletedAnnotations: [UInt64] { lock.withLock { _deletedAnnotations } }
    func annotations(_ session: String) async throws -> [FileAnnotation] { [] }
    func createAnnotation(_ session: String, input: WireCreateAnnotationInput) async throws -> FileAnnotation {
        throw ClientError.http(409)
    }
    func updateAnnotation(_ session: String, input: WireUpdateAnnotationInput) async throws -> FileAnnotation {
        throw ClientError.http(409)
    }
    func deleteAnnotation(_ session: String, id: UInt64) async throws {
        lock.withLock { _deletedAnnotations.append(id) }
    }
}
