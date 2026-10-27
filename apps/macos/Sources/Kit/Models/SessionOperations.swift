import Foundation
import Observation

@MainActor @Observable
final class SessionOperations {
    enum Submission: Equatable {
        case idle, pending, acknowledged(String), uncertain
        /// The server, or a fork's first turn, didn't start the turn. Shown
        /// in the activity card with Retry.
        case failed(String)
        /// The composer's input can't be sent as it is. Shown at the
        /// composer, where it can be fixed.
        case rejected(String)
    }
    enum Recovery: Equatable { case idle, refreshing, ready, failed(String) }
    private(set) var recovery: Recovery = .idle
    @ObservationIgnored private var recoveryTask: Task<Void, Never>?

    struct Draft {
        let text: String
        let notes: [String: String]
        let attachmentIDs: [String]
    }
    private(set) var submission: Submission = .idle
    private(set) var acknowledgedDraft: Draft?
    private(set) var receiptTurn: String?
    private(set) var queue: FollowUpState? {
        didSet {
            if queue?.count == 0, submission == .acknowledged("Queued") { submission = .idle }
        }
    }
    private(set) var queuePending = false
    private(set) var queueError: String?
    private(set) var abortingRun: String?
    private(set) var abortError: String?
    private var abortGeneration = UUID()
    /// Turns observed while a submission's receipt is outstanding. The server
    /// reserves the receipt turn at submission, so earlier turns never match.
    private var observedTurns = Set<String>()
    private var awaitingReceipt: Bool { submission == .pending || submission == .acknowledged("Queued") }
    private var currentRun: String?
    @ObservationIgnored private var submitTask: Task<Void, Never>?
    @ObservationIgnored private var queueTask: Task<Void, Never>?
    @ObservationIgnored private var abortTask: Task<Void, Never>?

    deinit { submitTask?.cancel(); queueTask?.cancel(); abortTask?.cancel(); recoveryTask?.cancel() }
    var sending: Bool { submission == .pending }
    /// Why the last send didn't start a turn, if it failed.
    var startFailure: String? { if case .failed(let message) = submission { message } else { nil } }
    var uncertain: Bool { if case .uncertain = submission { true } else { false } }

    /// Refuses the composer's input before it is sent.
    func reject(_ message: String) { submission = .rejected(message) }
    /// Reports a turn that didn't start, such as a fork's first message.
    func fail(_ message: String) { submission = .failed(message) }
    func clearAcknowledgement() {
        if case .acknowledged = submission { submission = .idle; receiptTurn = nil }
    }
    func recover(using refresh: @escaping @MainActor () async throws -> SessionExcerpt) {
        guard uncertain, recovery != .refreshing else { return }
        recovery = .refreshing
        recoveryTask = Task { [weak self] in
            do {
                let snapshot = try await refresh()
                try Task.checkCancellation()
                guard let self else { return }
                guard let queue = snapshot.followUps else { throw ClientError.invalidPayload }
                self.queue = queue
                self.recovery = .ready
                self.submission = .idle
                self.recoveryTask = nil
            } catch {
                guard !Task.isCancelled else { return }
                self?.recovery = .failed(error.localizedDescription)
                self?.recoveryTask = nil
            }
        }
    }
    func consumeAcknowledgedDraft() { acknowledgedDraft = nil }

    func reconcile(_ session: SessionExcerpt) {
        currentRun = session.activeRunID
        if awaitingReceipt { observedTurns.formUnion(session.observedTurns ?? []) }
        if let receiptTurn, observedTurns.contains(receiptTurn) {
            submission = .acknowledged("Sent")
            observedTurns = []
        }
        // The server announces every queue change, by any client, on the
        // session's stream, in order. A queue operation's response may be
        // older than the stream by the time it arrives, so only the session
        // sets the queue.
        if let live = session.followUps, live != queue { queue = live }
        if let abortingRun, abortingRun != currentRun { self.abortingRun = nil }
    }

    func submit(client: any SessionMutationClient, session: String, input: WirePromptInput,
                draft: Draft, uncertain: @escaping @MainActor () -> Void,
                acknowledged: @escaping @MainActor () -> Void) {
        performSubmission(draft: draft, uncertain: uncertain, acknowledged: acknowledged) {
            let result = try await client.submit(session, input: input)
            return SubmissionReceipt(turn: result.reservation?.turnId, queued: result.queued)
        }
    }

    func submitCommand(client: any PromptCommandClient, session: String, input: WirePromptCommandInput,
                       draft: Draft, uncertain: @escaping @MainActor () -> Void,
                       acknowledged: @escaping @MainActor () -> Void) {
        performSubmission(draft: draft, uncertain: uncertain, acknowledged: acknowledged) {
            let result = try await client.submitPromptCommand(session, input: input)
            return SubmissionReceipt(turn: result.reservation?.turnId, queued: result.queued)
        }
    }

    private struct SubmissionReceipt: Sendable {
        let turn: String?
        let queued: Bool
    }

    private func performSubmission(draft: Draft, uncertain: @escaping @MainActor () -> Void,
                                   acknowledged: @escaping @MainActor () -> Void,
                                   request: @escaping @Sendable () async throws -> SubmissionReceipt) {
        guard !sending, !self.uncertain else { return }
        submission = .pending
        receiptTurn = nil
        observedTurns = []
        submitTask = Task { [weak self] in
            do {
                let result = try await request()
                guard !Task.isCancelled, let self else { return }
                self.receiptTurn = result.turn
                self.acknowledgedDraft = draft
                self.submission = .acknowledged(result.queued ? "Queued" : "Sent")
                self.submitTask = nil
                acknowledged()
            } catch {
                guard !Task.isCancelled, let self else { return }
                self.submitTask = nil
                if Self.definitelyRejected(error) { self.submission = .failed(self.refusal(error)) }
                else {
                    self.submission = .uncertain
                    uncertain()
                }
            }
        }
    }

    func abort(client: any SessionMutationClient, session: String, turn: String) {
        guard abortingRun == nil else { return }
        let generation = UUID()
        abortGeneration = generation
        abortingRun = turn
        currentRun = turn
        abortError = nil
        abortTask = Task { [weak self] in
            do {
                try await client.abort(session, turn: turn)
                guard !Task.isCancelled, let self, self.abortGeneration == generation else { return }
                if self.currentRun != turn { self.abortingRun = nil }
                self.abortTask = nil
            } catch {
                guard !Task.isCancelled, let self, self.abortGeneration == generation else { return }
                self.abortError = self.currentRun == turn ? error.localizedDescription : nil
                self.abortingRun = nil
                self.abortTask = nil
            }
        }
    }

    enum QueueAction { case edit, remove, promote }
    func changeQueue(_ action: QueueAction, client: any SessionMutationClient, session: String,
                     restored: @escaping @MainActor ([WirePromptInput]) -> Void) {
        guard !queuePending, !sending, !uncertain else { return }
        queuePending = true; queueError = nil
        queueTask = Task { [weak self] in
            do {
                switch action {
                case .promote:
                    _ = try await client.promoteFollowUps(session)
                    guard !Task.isCancelled else { return }
                case .edit, .remove:
                    let result = try await client.restoreFollowUps(session)
                    guard !Task.isCancelled else { return }
                    if action == .edit { restored(result.messages ?? []) }
                }
                self?.queuePending = false; self?.queueTask = nil
            } catch {
                guard !Task.isCancelled else { return }
                self?.queuePending = false
                self?.queueError = error.localizedDescription
                self?.queueTask = nil
            }
        }
    }

    /// Why the server refused a prompt. An idle session with queued messages
    /// starts nothing else until they are edited or cleared.
    private func refusal(_ error: Error) -> String {
        if case ClientError.http(409) = error, (queue?.count ?? 0) > 0, currentRun == nil {
            return "Edit or clear the queued messages before sending a new one."
        }
        return error.localizedDescription
    }

    private static func definitelyRejected(_ error: Error) -> Bool {
        if error is MutationNotSent { return true }
        if case ClientError.http(let status) = error { return [400, 401, 403, 404, 409, 413, 422, 429].contains(status) }
        return false
    }
}
