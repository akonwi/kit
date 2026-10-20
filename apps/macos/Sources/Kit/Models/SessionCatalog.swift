import Foundation
import Observation

/// The sessions one server lists. Every window shares a server's catalog.
@MainActor @Observable
final class SessionCatalog {
    let client: any SessionClient
    private(set) var sessions: [SessionExcerpt] = []
    /// Whether the catalog has loaded at least once.
    private(set) var loaded = false
    private(set) var loading = false
    private(set) var error: String?
    private var generation = UUID()
    private var deletedIDs: Set<String> = []

    init(client: any SessionClient) {
        self.client = client
    }

    var serverID: String { client.serverID }

    func sessionDeleted(_ identity: SessionIdentity) {
        guard identity.server == client.serverID else { return }
        deletedIDs.insert(identity.session)
        sessions.removeAll { $0.id == identity.session }
    }

    func recordCreated(_ session: SessionExcerpt) {
        sessions.removeAll { $0.id == session.id }
        sessions.insert(session, at: 0)
    }

    /// Loads the catalog. Overlapping requests coalesce.
    func refresh() async {
        guard !loading else { return }
        await load()
    }

    /// Loads the catalog, superseding a load in progress.
    func load() async {
        let attempt = UUID(); generation = attempt
        loading = true; error = nil
        defer { if generation == attempt { loading = false } }
        do {
            let catalog = try await TemporarySessions.shared.catalog(client: client)
            guard generation == attempt, !Task.isCancelled else { return }
            sessions = catalog.filter { !deletedIDs.contains($0.id) }
            loaded = true
        } catch {
            guard generation == attempt, !Task.isCancelled else { return }
            self.error = error.localizedDescription
        }
    }
}
