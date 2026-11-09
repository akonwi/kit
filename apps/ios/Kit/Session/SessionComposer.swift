import Foundation
import KitClient
import Observation

/// The client a composer sends through: prompts and prompt commands, the
/// follow-up queue, aborts, attachment uploads, comments, and the snapshot
/// that settles an uncertain send.
typealias SessionSendClient = SessionMutationClient & PromptCommandClient & ComposerClient & AnnotationClient

/// The composer under one session's transcript: the draft, and what its one
/// button does with it. Sending, stopping, and queue changes go through the
/// shared `SessionOperations`, so the phone settles uncertain sends and queue
/// edits the way the Mac does.
@MainActor @Observable
final class SessionComposer {
    /// The button at the end of the capsule.
    enum Action: Equatable {
        /// Sends the draft, or queues it behind a running turn.
        case send(enabled: Bool)
        /// Stops the running turn. Shown while one runs and the draft is empty.
        case stop(enabled: Bool)
        /// Waiting on the server: a send whose answer hasn't come, or a stop
        /// whose run hasn't ended.
        case waiting
    }

    let session: String
    let operations = SessionOperations()
    private let client: any SessionSendClient

    var draft = "" {
        didSet { if draft != oldValue { noticeDismissed = true } }
    }
    /// Attachments that came back with queued messages moved into the draft.
    /// They go out again with the next send.
    private(set) var restoredAttachmentIDs: [String] = []
    /// The restored attachments' names, once the server has said.
    private(set) var restoredNames: [String: String] = [:]
    /// Files attached to the draft, each uploading as soon as it's added.
    let uploads = ComposerAttachments()
    /// Comments on the session's changes, sent with the next message.
    let annotations = AnnotationState()
    /// The session's model name, for saying it can't take images.
    var modelName: @MainActor () -> String = { "" }
    /// Whether the session's stream is live. Nothing is sent without it.
    var live = false
    private(set) var latest: SessionExcerpt?
    /// How many messages and prompt commands this composer has sent. The
    /// session screen follows the end when it changes.
    private(set) var sends = 0
    /// Set when the draft changes after a refusal, which hides the refusal.
    private var noticeDismissed = false

    init(session: String, client: any SessionSendClient) {
        self.session = session
        self.client = client
    }

    /// Takes in each snapshot the session's stream delivers.
    func reconcile(_ snapshot: SessionExcerpt) {
        latest = snapshot
        operations.reconcile(snapshot)
        annotations.observe(snapshot.annotations ?? [])
    }

    // MARK: State

    var running: Bool { latest?.activeRunID != nil }
    private var hasContent: Bool {
        !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !restoredAttachmentIDs.isEmpty
            || !uploads.items.isEmpty || !annotations.records.isEmpty
    }
    /// The model's name when the session's model accepts no images.
    var imageRejectingModel: String? {
        guard let inputs = latest?.inputs, !inputs.contains("image") else { return nil }
        return modelName()
    }
    /// Attached images the model can't take. They hold the send until removed.
    var hasRejectedImages: Bool { imageRejectingModel != nil && uploads.items.contains(where: \.isImage) }
    /// How many more files the draft can take.
    var attachmentRoom: Int { max(0, 8 - uploads.items.count - restoredAttachmentIDs.count) }
    /// Whether the queue is held because its first message couldn't start.
    /// The server refuses new prompts until it is edited or cleared.
    var held: Bool { operations.queue?.admissionFailure != nil }
    /// The queue to show above the capsule, when there is one.
    var queue: FollowUpState? { operations.queue.flatMap { $0.count > 0 ? $0 : nil } }

    var action: Action {
        if operations.sending || operations.uncertain { return .waiting }
        if running && !hasContent {
            return operations.abortingRun == nil ? .stop(enabled: live) : .waiting
        }
        return .send(enabled: live && hasContent && !held && !operations.queuePending
                     && uploads.ready && !hasRejectedImages)
    }

    /// Whether the draft can change. It can't while its send is unanswered.
    var editable: Bool { !operations.sending && !operations.uncertain }

    /// Whether the mic shows: the draft has no words and can change.
    var canDictate: Bool { editable && draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    /// Whether the mic takes Send's slot, with nothing to send or stop.
    /// Otherwise it sits beside Stop, or beside Send for attachments and comments.
    var dictatesInSendSlot: Bool { canDictate && !hasContent && !running }

    /// Whether the queue's Edit and Clear can act.
    var queueEnabled: Bool { live && editable && !operations.queuePending }

    /// What went wrong with a comment, while no comment box is open to say.
    var commentNotice: String? { annotations.editor == nil ? annotations.error : nil }

    /// What went wrong with the last send, stop, or queue change, shown in
    /// place of the activity line.
    var notice: String? {
        if case .failed(let reason) = operations.recovery {
            return "Couldn’t check whether the message was sent. \(reason)"
        }
        if !noticeDismissed {
            switch operations.submission {
            case .failed(let reason), .rejected(let reason): return "Didn’t send. \(reason)"
            default: break
            }
        }
        if let error = operations.abortError { return "Couldn’t stop. \(error)" }
        // With a queue, its tray shows the error instead.
        if queue == nil, let error = operations.queueError { return error }
        return nil
    }

    // MARK: Actions

    /// Sends the draft, or a prompt command when it starts with one.
    func send() {
        guard case .send(enabled: true) = action else { return }
        noticeDismissed = false
        let text = draft, id = session
        let attachments = ComposerAttachments.uniqueIDs(restoredAttachmentIDs + uploads.readyIDs)
        guard attachments.count <= 8 else {
            operations.reject("A message can contain up to eight attachments.")
            return
        }
        // Comments go with the message, as on the Mac.
        guard !annotations.pending, !annotations.uncertain, annotations.editor == nil else {
            operations.reject("Finish the comment you’re writing before sending.")
            return
        }
        let notes = annotations.records
        guard notes.count <= 64 else {
            operations.reject("A message can contain up to 64 comments.")
            return
        }
        guard !notes.contains(where: \.stale) else {
            operations.reject("The code under a comment changed. Select its lines again or delete it before sending.")
            return
        }
        let sent = SessionOperations.Draft(text: text, notes: [:], attachmentIDs: attachments)
        let uncertain: @MainActor () -> Void = { [weak self] in self?.recover() }
        let acknowledged: @MainActor () -> Void = { [weak self] in self?.applyAcknowledgedDraft() }
        if let command = PromptCommand.invocation(text) {
            guard (latest?.promptCommands ?? []).contains(where: { $0.name == command.name }) else {
                operations.reject("Unknown prompt command: /" + command.name)
                return
            }
            guard attachments.isEmpty, notes.isEmpty else {
                operations.reject("Prompt commands do not accept attachments or comments.")
                return
            }
            sends += 1
            operations.submitCommand(client: client, session: id, input: .init(name: command.name, args: command.args),
                                     draft: sent, uncertain: uncertain, acknowledged: acknowledged)
            return
        }
        sends += 1
        operations.submit(client: client, session: id,
                          input: WirePromptInput(text: text, attachmentIds: attachments, annotationIds: notes.map(\.id)),
                          draft: sent, uncertain: uncertain, acknowledged: acknowledged)
    }

    /// Sends dictated words with the attachments and comments waiting. The
    /// mic shows only while the draft has no words, so they replace it. If
    /// the message can't go, the words stay in the draft.
    func sendDictation(_ text: String) {
        draft = text
        send()
    }

    /// Attaches a file's bytes; it uploads at once.
    func attach(filename: String, data: Data) {
        uploads.add(filename: filename, data: data, client: client, session: session,
                    imageRejectingModel: { [weak self] in self?.imageRejectingModel })
    }

    /// Attaches a file the user picked; it uploads at once.
    func attach(url: URL) {
        uploads.add(url: url, client: client, session: session,
                    imageRejectingModel: { [weak self] in self?.imageRejectingModel })
    }

    /// Deletes a comment before it's sent.
    func removeComment(_ note: FileAnnotation) {
        let client = client, id = session
        Task { await annotations.remove(client: client, session: id, id: note.id) }
    }

    /// Uploads a file again after it failed.
    func retryUpload(_ id: UUID) {
        uploads.retry(id, client: client, session: session)
    }

    /// Drops a restored attachment from the draft.
    func removeRestored(_ id: String) {
        restoredAttachmentIDs.removeAll { $0 == id }
    }

    /// Asks the server for the restored attachments' names.
    func loadRestoredNames() async {
        let ids = restoredAttachmentIDs.filter { restoredNames[$0] == nil }
        guard !ids.isEmpty, let result = try? await client.resolveAttachments(session, ids: ids) else { return }
        for info in result.attachments ?? [] { restoredNames[info.id] = info.filename }
        for id in result.missingAttachmentIds ?? [] { restoredNames[id] = "Attachment unavailable" }
    }

    /// Stops the running turn.
    func stop() {
        guard case .stop(enabled: true) = action, let run = latest?.activeRunID else { return }
        operations.abort(client: client, session: session, turn: run)
    }

    /// Edits, clears, or sends the queue now. Edit moves the queued messages
    /// in front of the draft.
    func changeQueue(_ change: SessionOperations.QueueAction) {
        guard queueEnabled else { return }
        if change == .promote, !running { return }
        operations.changeQueue(change, client: client, session: session) { [weak self] messages in
            guard let self else { return }
            let text = messages.map(\.text).joined(separator: "\n\n")
            self.draft = [text, self.draft].filter { !$0.isEmpty }.joined(separator: "\n\n")
            var ids = self.restoredAttachmentIDs
            for id in messages.flatMap({ $0.attachmentIds ?? [] }) where !ids.contains(id) { ids.append(id) }
            self.restoredAttachmentIDs = ids
        }
    }

    /// Puts a fork's first message that couldn't start back in the draft,
    /// with the failure shown like any failed send.
    func restore(failedFork failure: ForkFirstTurnFailures.Failure) {
        if draft.isEmpty { draft = failure.message }
        operations.fail(failure.error)
        noticeDismissed = false
    }

    /// Asks again whether an unanswered send reached the server.
    func recover() {
        let client = client, id = session
        operations.recover { try await client.snapshot(id) }
    }

    /// Clears what was sent from the draft, unless it changed meanwhile.
    private func applyAcknowledgedDraft() {
        guard let sent = operations.acknowledgedDraft else { return }
        if draft == sent.text { draft = "" }
        restoredAttachmentIDs.removeAll { sent.attachmentIDs.contains($0) }
        uploads.acknowledge(sent.attachmentIDs)
        operations.consumeAcknowledgedDraft()
    }
}
