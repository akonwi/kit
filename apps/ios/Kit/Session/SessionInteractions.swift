import Foundation
import KitClient
import Observation

/// The requests a session is waiting on, as the interaction dock shows them:
/// the oldest pending one in the composer's place. Requests become shared
/// `InteractionFlow`s, which keep their answers across snapshots, and answers
/// go out as the Mac sends them.
@MainActor @Observable
final class SessionInteractions {
    let session: String
    private let client: any ComposerClient
    /// Pending requests, oldest first.
    private(set) var flows: [InteractionFlow] = []
    /// Keeps an answered request docked while a plugin's next one arrives.
    let hold = InteractionHold()
    /// How long an answered request stays docked after it's sent.
    private let holdWindow: Duration
    /// Whether the session's stream is live. Nothing is sent without it.
    var live = false

    init(session: String, client: any ComposerClient, holdWindow: Duration = InteractionHold.window) {
        self.session = session
        self.client = client
        self.holdWindow = holdWindow
    }

    /// The request in the composer's place: the oldest pending one, or the
    /// answered one still held.
    var docked: InteractionFlow? { flows.first ?? hold.flow }

    /// The request the reader folded away to read the transcript. Another
    /// request arrives open.
    private var collapsedID: UUID?

    /// Whether the docked request shows as a pill instead of in full.
    var collapsed: Bool {
        get { docked != nil && docked?.id == collapsedID }
        set { collapsedID = newValue ? docked?.id : nil }
    }

    /// Takes in each snapshot, keeping the flow, and so the answers, of a
    /// request that is still pending.
    func reconcile(_ snapshot: SessionExcerpt) {
        let previous = flows
        flows = (snapshot.pendingInteractions ?? []).sorted { $0.createdAt < $1.createdAt }.compactMap { request in
            previous.first { $0.request?.id == request.id } ?? (try? InteractionFlow(request: request))
        }
        if !flows.isEmpty { hold.clear() }
    }

    /// Runs an answer step, and sends the request once it is answered.
    func forward(_ flow: InteractionFlow, _ step: (InteractionFlow) -> Bool) {
        guard live, !flow.submitting else { return }
        if step(flow) { respond(flow, cancelled: false) }
    }

    /// Cancels the request, which tells the agent it was declined.
    func cancel(_ flow: InteractionFlow) {
        guard live else { return }
        respond(flow, cancelled: true)
    }

    /// Sends an answer or cancellation. The session's stream reports what the
    /// server did with it; a failed answer stays docked with its error.
    private func respond(_ flow: InteractionFlow, cancelled: Bool) {
        guard !flow.submitting, let input = flow.response(cancelled: cancelled) else { return }
        flow.submitting = true; flow.error = nil
        let client = client, session = session
        Task {
            do {
                try await client.respond(session, input: input)
                // It stays in its sending state until something replaces it.
                hold.hold(flow, for: holdWindow) {}
            } catch {
                flow.error = error.localizedDescription
                flow.submitting = false
            }
        }
    }
}
