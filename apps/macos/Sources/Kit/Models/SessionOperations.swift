import Foundation
import Observation

@MainActor @Observable
final class SessionOperations {
    enum Submission: Equatable {
        case idle, pending, acknowledged(String), failed(String), uncertain
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
    private var queueRevision = 0
    private var abortGeneration = UUID()
    private var observedTurns = Set<String>()
    private var currentRun: String?
    @ObservationIgnored private var submitTask: Task<Void, Never>?
    @ObservationIgnored private var queueTask: Task<Void, Never>?
    @ObservationIgnored private var abortTask: Task<Void, Never>?

    deinit { submitTask?.cancel(); queueTask?.cancel(); abortTask?.cancel(); recoveryTask?.cancel() }
    var sending: Bool { submission == .pending }
    var uncertain: Bool { if case .uncertain = submission { true } else { false } }

    func reject(_ message: String) { submission = .failed(message) }
    func clearAcknowledgement() {
        if case .acknowledged = submission { submission = .idle; receiptTurn = nil }
    }
    func recover(using refresh: @escaping @MainActor () async throws -> SessionExcerpt) {
        guard uncertain, recovery != .refreshing else { return }
        recovery = .refreshing
        queueRevision += 1
        recoveryTask = Task { [weak self] in
            do {
                let snapshot = try await refresh()
                try Task.checkCancellation()
                guard let self else { return }
                guard let queue = snapshot.followUps else { throw ClientError.invalidPayload }
                self.queue = queue
                self.queueRevision += 1
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
        observedTurns.formUnion(session.observedTurns ?? [])
        if let receiptTurn, observedTurns.contains(receiptTurn) { submission = .acknowledged("Sent") }
        if queue == nil { queue = session.followUps }
        if let abortingRun, abortingRun != currentRun { self.abortingRun = nil }
    }

    func submit(client: any SessionMutationClient, session: String, input: WirePromptInput,
                draft: Draft, uncertain: @escaping @MainActor () -> Void,
                acknowledged: @escaping @MainActor () -> Void) {
        performSubmission(draft: draft, uncertain: uncertain, acknowledged: acknowledged) {
            let result = try await client.submit(session, input: input)
            return SubmissionReceipt(turn: result.reservation?.turnId, queued: result.queued, queue: result.queue)
        }
    }

    func submitCommand(client: any PromptCommandClient, session: String, input: WirePromptCommandInput,
                       draft: Draft, uncertain: @escaping @MainActor () -> Void,
                       acknowledged: @escaping @MainActor () -> Void) {
        performSubmission(draft: draft, uncertain: uncertain, acknowledged: acknowledged) {
            let result = try await client.runPromptCommand(session, input: input)
            return SubmissionReceipt(turn: result.turnId, queued: false, queue: nil)
        }
    }

    private struct SubmissionReceipt: Sendable {
        let turn: String?
        let queued: Bool
        let queue: WireFollowUpQueue?
    }

    private func performSubmission(draft: Draft, uncertain: @escaping @MainActor () -> Void,
                                   acknowledged: @escaping @MainActor () -> Void,
                                   request: @escaping @Sendable () async throws -> SubmissionReceipt) {
        guard !sending, !self.uncertain else { return }
        queueRevision += 1
        submission = .pending
        receiptTurn = nil
        submitTask = Task { [weak self] in
            do {
                let result = try await request()
                guard !Task.isCancelled, let self else { return }
                self.queueRevision += 1
                if let queue = result.queue { self.queue = try FollowUpState(queue) }
                self.receiptTurn = result.turn
                self.acknowledgedDraft = draft
                self.submission = .acknowledged(result.queued ? "Queued" : "Sent")
                self.submitTask = nil
                acknowledged()
            } catch {
                guard !Task.isCancelled, let self else { return }
                self.submitTask = nil
                if Self.definitelyRejected(error) { self.submission = .failed(error.localizedDescription) }
                else {
                    self.submission = .uncertain
                    uncertain()
                }
            }
        }
    }

    func abort(client: any SessionMutationClient, session: String, run: String) {
        guard abortingRun == nil else { return }
        let generation = UUID()
        abortGeneration = generation
        abortingRun = run
        currentRun = run
        abortError = nil
        abortTask = Task { [weak self] in
            do {
                try await client.abort(session, run: run)
                guard !Task.isCancelled, let self, self.abortGeneration == generation else { return }
                if self.currentRun != run { self.abortingRun = nil }
                self.abortTask = nil
            } catch {
                guard !Task.isCancelled, let self, self.abortGeneration == generation else { return }
                self.abortError = self.currentRun == run ? error.localizedDescription : nil
                self.abortingRun = nil
                self.abortTask = nil
            }
        }
    }

    enum QueueAction { case edit, remove, promote }
    func changeQueue(_ action: QueueAction, client: any SessionMutationClient, session: String,
                     restored: @escaping @MainActor ([WirePromptInput]) -> Void) {
        guard !queuePending, !sending, !uncertain else { return }
        queueRevision += 1
        queuePending = true; queueError = nil
        queueTask = Task { [weak self] in
            do {
                switch action {
                case .promote:
                    let result = try await client.promoteFollowUps(session)
                    guard !Task.isCancelled else { return }
                    self?.queue = try FollowUpState(result.queue)
                case .edit, .remove:
                    let result = try await client.restoreFollowUps(session)
                    guard !Task.isCancelled else { return }
                    self?.queue = try FollowUpState(result.queue)
                    if action == .edit { restored(result.messages ?? []) }
                }
                self?.queueRevision += 1
                self?.queuePending = false; self?.queueTask = nil
            } catch {
                guard !Task.isCancelled else { return }
                self?.queuePending = false
                self?.queueError = error.localizedDescription
                self?.queueTask = nil
            }
        }
    }

    func monitorQueue(client: any SessionMutationClient, session: String) async {
        while !Task.isCancelled {
            let revision = queueRevision
            do {
                let latest = try await client.followUps(session)
                try Task.checkCancellation()
                if revision == queueRevision && !queuePending && !sending && recovery != .refreshing { queue = latest }
            } catch {
                if Task.isCancelled { return }
            }
            do { try await Task.sleep(for: .seconds(2)) } catch { return }
        }
    }

    private static func definitelyRejected(_ error: Error) -> Bool {
        if error is MutationNotSent { return true }
        if case ClientError.http(let status) = error { return [400, 401, 403, 404, 409, 413, 422, 429].contains(status) }
        return false
    }
}
