import Foundation
import Observation

/// Scene-scoped connection/catalog; each restored window reconnects independently.
@MainActor @Observable
final class AppModel {
    private(set) var client: (any SessionClient)?
    private(set) var sessions: [SessionExcerpt] = []
    private(set) var connecting = false
    private(set) var error: String?
    private var generation = UUID()

    private var deletedIDs: Set<String> = []

    func sessionDeleted(_ identity: SessionIdentity) {
        guard identity.server == client?.serverID else { return }
        deletedIDs.insert(identity.session)
        sessions.removeAll { $0.id == identity.session }
    }

    func recordCreated(_ session: SessionExcerpt) {
        sessions.removeAll { $0.id == session.id }
        sessions.insert(session, at: 0)
    }

    /// Reuse the established client; overlapping presentation/manual requests coalesce.
    func refreshSessions() async {
        guard !connecting else { return }
        await connectLocal(using: client ?? LocalClient())
    }

    func connectLocal(using candidate: any SessionClient = LocalClient()) async {
        let attempt = UUID(); generation = attempt
        connecting = true; error = nil
        defer { if generation == attempt { connecting = false } }
        do {
            let catalog = try await TemporarySessions.shared.catalog(client: candidate)
            guard generation == attempt, !Task.isCancelled else { return }
            sessions = catalog.filter { !deletedIDs.contains($0.id) }; client = candidate
        } catch {
            guard generation == attempt, !Task.isCancelled else { return }
            self.error = error.localizedDescription
        }
    }

    func connectIfNeeded(to serverID: String, using candidate: any SessionClient = LocalClient()) async {
        guard ["", "local-v2"].contains(serverID), client == nil, !connecting else { return }
        await connectLocal(using: candidate)
    }
}
