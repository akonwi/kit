import Foundation
import Observation

/// Client-owned references only. Conversation content remains exclusively in daemon memory.
@MainActor @Observable
final class TemporarySessions {
    static let shared = TemporarySessions()
    private let defaults: UserDefaults
    private let key = "temporarySessionIdentities"
    private(set) var identities: Set<SessionIdentity>

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        identities = defaults.data(forKey: key).flatMap {
            try? JSONDecoder().decode(Set<SessionIdentity>.self, from: $0)
        } ?? []
    }

    func contains(server: String, session: String) -> Bool {
        identities.contains(SessionIdentity(server: server, session: session))
    }

    func remember(server: String, session: String) {
        identities.insert(SessionIdentity(server: server, session: session))
        save()
    }

    func forget(_ identity: SessionIdentity) {
        identities.remove(identity)
        save()
    }

    private func save() {
        if let data = try? JSONEncoder().encode(identities) { defaults.set(data, forKey: key) }
    }

    func catalog(client: any SessionClient) async throws -> [SessionExcerpt] {
        var result = try await client.sessions()
        for identity in identities.filter({ $0.server == client.serverID }).sorted(by: { $0.session < $1.session }) {
            do {
                var session = try await client.snapshot(identity.session)
                session.isTemporary = true
                // Catalogs carry metadata only, not another copy of the transcript.
                session.messages = []
                result.removeAll { $0.id == identity.session }
                result.append(session)
            } catch ClientError.http(404) { forget(identity) }
            catch ClientError.missingSession { forget(identity) }
        }
        return result
    }
}
