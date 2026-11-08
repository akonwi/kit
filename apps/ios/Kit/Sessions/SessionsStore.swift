import Foundation
import KitClient
import Observation
import Synchronization

/// What the Sessions list asks of a server.
protocol SessionsNetwork: Sendable {
    /// The server's sessions, asked for in the attention order. A server that
    /// predates the order lists them unordered and without their activity.
    func sessionsByAttention(on server: SavedServer) async throws -> AttentionListing
    /// Which sessions are working or awaiting a response; the rest are idle.
    /// Asked only of a server that predates the attention order.
    func activity(on server: SavedServer) async throws -> [String: SessionTabStatus]
    func snapshot(_ id: String, on server: SavedServer) async throws -> SessionExcerpt
    /// A client for one server, for screens that watch a session.
    func client(for server: SavedServer) -> ServerClient
}

/// Reaches servers with the device tokens in the Keychain. One client is
/// kept per server, so its verified transport is reused between requests.
final class LiveSessionsNetwork: SessionsNetwork {
    private let tokens: any DeviceTokenStore
    /// Clients by server name and address.
    private let clients = Mutex<[String: ServerClient]>([:])

    init(tokens: any DeviceTokenStore = KeychainDeviceTokens()) {
        self.tokens = tokens
    }

    func client(for server: SavedServer) -> ServerClient {
        let key = server.name + "\u{1F}" + server.address
        return clients.withLock { clients in
            if let client = clients[key] { return client }
            let credential = DeviceCredential(home: ServerStore.credentialHome, name: server.name)
            let client = ServerClient(serverID: server.name, connector: .url(server.address, credential: credential), tokens: tokens)
            clients[key] = client
            return client
        }
    }

    func sessionsByAttention(on server: SavedServer) async throws -> AttentionListing {
        try await client(for: server).sessionsByAttention()
    }

    func activity(on server: SavedServer) async throws -> [String: SessionTabStatus] {
        try await client(for: server).sessionActivity()
    }

    func snapshot(_ id: String, on server: SavedServer) async throws -> SessionExcerpt {
        try await client(for: server).snapshot(id)
    }
}

/// When this iPhone last showed each session, so the list can mark sessions
/// that changed since. A server's sessions all count as seen the first time
/// it's listed, so pairing doesn't mark every old session as new.
final class SeenSessions {
    private let defaults: UserDefaults
    private static let seenKey = "sessions.seen"
    private static let listedKey = "sessions.listedServers"
    /// When each session was last shown, by `server␟session`.
    private var seen: [String: Double]
    private var listed: Set<String>

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        seen = defaults.dictionary(forKey: Self.seenKey) as? [String: Double] ?? [:]
        listed = Set(defaults.stringArray(forKey: Self.listedKey) ?? [])
    }

    private func save() {
        defaults.set(seen, forKey: Self.seenKey)
        defaults.set(listed.sorted(), forKey: Self.listedKey)
    }

    private static func key(_ id: SessionIdentity) -> String { id.server + "\u{1F}" + id.session }

    /// Whether the session changed after it was last shown.
    func isUnseen(_ id: SessionIdentity, changed: Date?) -> Bool {
        guard let changed else { return false }
        guard let shown = seen[Self.key(id)] else { return true }
        return changed.timeIntervalSince1970 > shown
    }

    func markSeen(_ id: SessionIdentity, at date: Date) {
        seen[Self.key(id)] = max(seen[Self.key(id)] ?? 0, date.timeIntervalSince1970)
        save()
    }

    /// Records a server's current list. The first time, every session counts
    /// as seen; afterwards, sessions the server no longer lists are forgotten.
    func listed(_ server: String, sessions: [(id: String, changed: Date?)]) {
        let prefix = server + "\u{1F}"
        let current = Set(sessions.map { prefix + $0.id })
        seen = seen.filter { !$0.key.hasPrefix(prefix) || current.contains($0.key) }
        if !listed.contains(server) {
            for session in sessions {
                seen[prefix + session.id] = (session.changed ?? .distantPast).timeIntervalSince1970
            }
            listed.insert(server)
        }
        save()
    }

    /// Forgets a server that was removed.
    func forget(_ server: String) {
        let prefix = server + "\u{1F}"
        seen = seen.filter { !$0.key.hasPrefix(prefix) }
        listed.remove(server)
        save()
    }
}

/// Every paired server's sessions, merged into one list in the attention
/// order: sessions that need you first, the one waiting longest first; then
/// working sessions; then the rest, latest activity first. A server that
/// can't be reached keeps the list it last gave, ordered as idle.
@MainActor @Observable
final class SessionsStore {
    struct Row: Identifiable, Equatable {
        let session: SessionExcerpt
        let server: String
        let status: SessionTabStatus
        /// The server couldn't be reached, so `status` and `preview` are as last seen.
        let stale: Bool
        let unseen: Bool
        let preview: SessionPreview?
        let changed: Date?
        /// When it started waiting on you, if its server says.
        var waitingSince: Date? = nil

        var id: SessionIdentity { SessionIdentity(server: server, session: session.id) }
        /// Whether the row shows a lane mark and a semibold title.
        var stands: Bool { status != .idle || unseen }
    }

    /// A server whose sessions can't be listed now.
    struct Problem: Identifiable, Equatable {
        let server: String
        let state: ServerState
        /// When its sessions were last listed, if ever.
        let listedAt: Date?
        var id: String { server }
    }

    enum Filter: Hashable {
        case all, needsYou
        case server(String)
    }

    private struct Listing {
        var server: SavedServer
        var sessions: [SessionExcerpt] = []
        var activity: [String: SessionTabStatus] = [:]
        var waitingSince: [String: Date] = [:]
        var listedAt: Date?
        var problem: ServerState?
    }

    private struct CachedPreview {
        let signature: String
        let preview: SessionPreview?
        let fetchedAt: Date
    }

    private var listings: [String: Listing] = [:]
    /// Server names in the order the Servers tab lists them.
    private(set) var servers: [String] = []
    private var previews: [SessionIdentity: CachedPreview] = [:]
    private var fetching: Set<SessionIdentity> = []
    private(set) var loaded = false
    /// Changes whenever a session is marked seen, so rows are read again.
    private var seenRevision = 0

    let network: any SessionsNetwork
    private let seen: SeenSessions
    private let now: () -> Date

    /// At most this many sessions get a preview at once.
    static let previewLimit = 12
    /// How often a working session's preview is fetched again.
    static let workingPreviewInterval: TimeInterval = 15

    init(network: any SessionsNetwork, seen: SeenSessions = SeenSessions(), now: @escaping () -> Date = { Date() }) {
        self.network = network
        self.seen = seen
        self.now = now
    }

    static func live() -> SessionsStore { SessionsStore(network: LiveSessionsNetwork()) }

    // MARK: Reading

    var rows: [Row] {
        _ = seenRevision
        return servers.flatMap { name -> [Row] in
            guard let listing = listings[name] else { return [] }
            let stale = listing.problem != nil
            return listing.sessions.map { session in
                let id = SessionIdentity(server: name, session: session.id)
                let changed = SessionFormat.date(session.lastActivity ?? session.date)
                let status = listing.activity[session.id] ?? .idle
                let unseen = seen.isUnseen(id, changed: changed)
                let preview = (status != .idle || unseen) ? previews[id]?.preview : nil
                return Row(session: session, server: name, status: status, stale: stale, unseen: unseen,
                           preview: preview, changed: changed, waitingSince: listing.waitingSince[session.id])
            }
        }
        .sorted(by: Self.precedes)
    }

    /// The attention order across servers. A stale row's status is only as
    /// last seen, so it's ordered as idle. Sessions with the same ID on two
    /// servers fall back to the server's name.
    static func precedes(_ a: Row, _ b: Row) -> Bool {
        let first = key(a), second = key(b)
        if AttentionOrder.precedes(first, second) { return true }
        if AttentionOrder.precedes(second, first) { return false }
        return a.server < b.server
    }

    private static func key(_ row: Row) -> AttentionOrder.Key {
        let status = row.stale ? .idle : row.status
        return AttentionOrder.Key(status: status, waitingSince: status == .awaitingResponse ? row.waitingSince : nil,
                                  updated: row.changed, id: row.session.id)
    }

    func rows(_ filter: Filter) -> [Row] {
        switch filter {
        case .all: rows
        case .needsYou: rows.filter { $0.status == .awaitingResponse && !$0.stale }
        case .server(let name): rows.filter { $0.server == name }
        }
    }

    var needsYouCount: Int { rows(.needsYou).count }

    var problems: [Problem] {
        servers.compactMap { name in
            guard let listing = listings[name], let problem = listing.problem else { return nil }
            return Problem(server: name, state: problem, listedAt: listing.listedAt)
        }
    }

    func server(named name: String) -> SavedServer? { listings[name]?.server }

    // MARK: Loading

    /// Lists every server's sessions, then fetches the previews that are due.
    func refresh(_ saved: [SavedServer]) async {
        servers = saved.map(\.name)
        for server in saved where listings[server.name]?.server != server {
            listings[server.name] = Listing(server: server)
        }
        for name in listings.keys where !servers.contains(name) {
            listings[name] = nil
            seen.forget(name)
        }
        await withTaskGroup(of: Void.self) { group in
            for server in saved {
                group.addTask { await self.list(server) }
            }
        }
        loaded = true
        await fetchPreviews()
    }

    /// Lists one server's sessions again.
    func retry(_ name: String) async {
        guard let server = listings[name]?.server else { return }
        await list(server)
        await fetchPreviews()
    }

    private func list(_ server: SavedServer) async {
        let network = self.network
        do {
            let list: [SessionExcerpt]
            var statuses: [String: SessionTabStatus] = [:]
            var since: [String: Date] = [:]
            switch try await network.sessionsByAttention(on: server) {
            case .ordered(let listed):
                list = listed.map(\.session)
                for entry in listed where entry.status != .idle {
                    statuses[entry.session.id] = entry.status
                    since[entry.session.id] = entry.waitingSince
                }
            case .unordered(let sessions):
                list = sessions
                statuses = try await network.activity(on: server)
            }
            guard listings[server.name]?.server == server else { return }
            seen.listed(server.name, sessions: list.map { ($0.id, SessionFormat.date($0.lastActivity ?? $0.date)) })
            listings[server.name]?.sessions = list
            listings[server.name]?.activity = statuses
            listings[server.name]?.waitingSince = since
            listings[server.name]?.listedAt = now()
            listings[server.name]?.problem = nil
        } catch {
            guard !isCancellation(error), listings[server.name]?.server == server else { return }
            listings[server.name]?.problem = ServerState.from(error, at: now())
        }
    }

    /// Fetches a snapshot for each standing row whose preview is missing or
    /// out of date, in the list's order, a few at a time.
    private func fetchPreviews() async {
        let date = now()
        let due = rows.filter { $0.stands && !$0.stale }.prefix(Self.previewLimit).filter { row in
            guard !fetching.contains(row.id) else { return false }
            guard let cached = previews[row.id], cached.signature == signature(row) else { return true }
            return row.status == .running && date.timeIntervalSince(cached.fetchedAt) >= Self.workingPreviewInterval
        }
        guard !due.isEmpty else { return }
        fetching.formUnion(due.map(\.id))
        defer { fetching.subtract(due.map(\.id)) }
        let network = self.network
        let requests = due.compactMap { row in listings[row.server].map { (row.id, signature(row), $0.server) } }
        await withTaskGroup(of: (SessionIdentity, String, SessionPreview??).self) { group in
            var pending = requests[...]
            func next() {
                guard let (id, signature, server) = pending.popFirst() else { return }
                group.addTask {
                    let snapshot = try? await network.snapshot(id.session, on: server)
                    return (id, signature, snapshot.map(SessionPreview.from))
                }
            }
            for _ in 0..<3 { next() }
            for await (id, signature, preview) in group {
                if let preview { previews[id] = CachedPreview(signature: signature, preview: preview, fetchedAt: now()) }
                next()
            }
        }
    }

    private func signature(_ row: Row) -> String {
        "\(row.session.lastActivity ?? row.session.date)|\(row.status)"
    }

    // MARK: Seen

    /// Records that a session was shown as of its latest change.
    func markSeen(_ id: SessionIdentity, changed: Date? = nil) {
        let latest = rows.first { $0.id == id }?.changed
        seen.markSeen(id, at: max(changed ?? .distantPast, latest ?? .distantPast))
        seenRevision += 1
    }
}
