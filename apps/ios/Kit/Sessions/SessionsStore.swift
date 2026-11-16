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
/// that changed since, and which sessions were last seen working, so one
/// whose turn ends while it isn't on screen is marked finished, as a Mac tab
/// is. A server's sessions all count as seen the first time it's listed, so
/// pairing doesn't mark every old session as new.
final class SeenSessions {
    private let defaults: UserDefaults
    private static let seenKey = "sessions.seen"
    private static let listedKey = "sessions.listedServers"
    private static let workingKey = "sessions.working"
    /// When each finished session was noticed finishing.
    private static let finishedKey = "sessions.finishedAt"
    /// Finished sessions as kept before their times were.
    private static let finishedSetKey = "sessions.finished"
    /// When each session was last shown, by `server␟session`.
    private var seen: [String: Double]
    private var listed: Set<String>
    /// Sessions last listed working or waiting on the user.
    private var working: Set<String>
    /// Sessions whose turn ended while they weren't on screen, with when
    /// this iPhone noticed; zero for ones kept before the time was.
    private var finished: [String: Double]

    init(defaults: UserDefaults = SharedStorage.defaults) {
        self.defaults = defaults
        seen = defaults.dictionary(forKey: Self.seenKey) as? [String: Double] ?? [:]
        listed = Set(defaults.stringArray(forKey: Self.listedKey) ?? [])
        working = Set(defaults.stringArray(forKey: Self.workingKey) ?? [])
        if let kept = defaults.dictionary(forKey: Self.finishedKey) as? [String: Double] {
            finished = kept
        } else {
            finished = Dictionary((defaults.stringArray(forKey: Self.finishedSetKey) ?? []).map { ($0, 0) }) { first, _ in first }
            // Kept in the new form before the old one is let go.
            defaults.set(finished, forKey: Self.finishedKey)
            defaults.removeObject(forKey: Self.finishedSetKey)
        }
    }

    private func save() {
        defaults.set(seen, forKey: Self.seenKey)
        defaults.set(listed.sorted(), forKey: Self.listedKey)
        defaults.set(working.sorted(), forKey: Self.workingKey)
        defaults.set(finished, forKey: Self.finishedKey)
    }

    private static func key(_ id: SessionIdentity) -> String { id.server + "\u{1F}" + id.session }

    /// Whether the session finished, or changed, after it was last shown.
    func isUnseen(_ id: SessionIdentity, changed: Date?) -> Bool {
        if finished[Self.key(id)] != nil { return true }
        guard let changed else { return false }
        guard let shown = seen[Self.key(id)] else { return true }
        return changed.timeIntervalSince1970 > shown
    }

    /// When this iPhone noticed the session's turn end, if it's marked
    /// finished and the time is known.
    func finishedAt(_ id: SessionIdentity) -> Date? {
        guard let time = finished[Self.key(id)], time > 0 else { return nil }
        return Date(timeIntervalSince1970: time)
    }

    /// Records that a session was shown as of `date`. `working` is whether
    /// it was working or waiting on the user when it left the screen, if
    /// known, so a turn that ends afterwards marks it finished.
    func markSeen(_ id: SessionIdentity, at date: Date, working isWorking: Bool? = nil) {
        let key = Self.key(id)
        seen[key] = max(seen[key] ?? 0, date.timeIntervalSince1970)
        finished[key] = nil
        if let isWorking {
            if isWorking { working.insert(key) } else { working.remove(key) }
        }
        save()
    }

    /// Records a server's current list. The first time, every session counts
    /// as seen; afterwards, sessions the server no longer lists are forgotten.
    /// A session listed idle that was last listed working is marked finished
    /// as of `now`, unless it's on screen.
    func listed(_ server: String, sessions: [(id: String, changed: Date?, working: Bool)], at now: Date = Date(),
                showing: (String) -> Bool = { _ in false }) {
        let prefix = server + "\u{1F}"
        let current = Set(sessions.map { prefix + $0.id })
        let stays = { (key: String) in !key.hasPrefix(prefix) || current.contains(key) }
        seen = seen.filter { stays($0.key) }
        working = working.filter(stays)
        finished = finished.filter { stays($0.key) }
        if !listed.contains(server) {
            for session in sessions {
                seen[prefix + session.id] = (session.changed ?? .distantPast).timeIntervalSince1970
            }
            listed.insert(server)
        }
        for session in sessions {
            let key = prefix + session.id
            if session.working {
                working.insert(key)
                finished[key] = nil
            } else if working.remove(key) != nil, !showing(session.id) {
                finished[key] = now.timeIntervalSince1970
            }
        }
        save()
    }

    /// Forgets a server that was removed.
    func forget(_ server: String) {
        let prefix = server + "\u{1F}"
        let stays = { (key: String) in !key.hasPrefix(prefix) }
        seen = seen.filter { stays($0.key) }
        working = working.filter(stays)
        finished = finished.filter { stays($0.key) }
        listed.remove(server)
        save()
    }
}

/// Every paired server's sessions, merged into one list: sessions that need
/// you first, the one waiting longest first; then finished replies and other
/// changes you haven't seen, the oldest first; then working sessions; then
/// the rest, latest activity first. A server that can't be reached keeps the
/// list it last gave, ordered as idle.
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
        /// When this iPhone noticed its turn end, if it's marked finished.
        var finishedAt: Date? = nil

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
        var sessions: [SessionExcerpt] = [] {
            didSet { titles = Dictionary(sessions.map { ($0.id, $0.title) }) { first, _ in first } }
        }
        /// Each session's title by its ID, for links that name sessions.
        private(set) var titles: [String: String] = [:]
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
    /// The session on screen, if one is. Its turn ending doesn't mark it
    /// finished, even while its changes, a file, or its scratchpad is open.
    @ObservationIgnored var onScreen: SessionIdentity?
    /// Each server's latest listing request, so an older one that answers
    /// later doesn't replace what a newer one listed.
    @ObservationIgnored private var listing: [String: Int] = [:]

    let network: any SessionsNetwork
    private let seen: SeenSessions
    /// Each session's composer draft, kept while its server lists it.
    let drafts: SessionDrafts
    private let now: () -> Date

    /// At most this many sessions get a preview at once.
    static let previewLimit = 12
    /// How often a working session's preview is fetched again.
    static let workingPreviewInterval: TimeInterval = 15

    init(network: any SessionsNetwork, seen: SeenSessions = SeenSessions(), drafts: SessionDrafts = SessionDrafts(),
         now: @escaping () -> Date = { Date() }) {
        self.network = network
        self.seen = seen
        self.drafts = drafts
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
                           preview: preview, changed: changed, waitingSince: listing.waitingSince[session.id],
                           finishedAt: seen.finishedAt(id))
            }
        }
        .sorted(by: Self.precedes)
    }

    /// The order across servers: sessions waiting on you, the one waiting
    /// longest first; then idle ones with something you haven't seen,
    /// finished replies first, the oldest first; then working ones; then
    /// the rest, latest activity first. A stale row's status is only as last
    /// seen, so it's ordered as idle. Sessions with the same ID on two
    /// servers fall back to the server's name.
    static func precedes(_ a: Row, _ b: Row) -> Bool {
        let first = group(a), second = group(b)
        if first != second { return first < second }
        if first == .unseen {
            // Finished replies by when this iPhone noticed them, then other
            // changes by the server's time, the oldest first, as sessions
            // waiting on you are; the two clocks aren't compared.
            switch (a.finishedAt, b.finishedAt) {
            case let (aTime?, bTime?) where aTime != bTime: return aTime < bTime
            case (.some, nil): return true
            case (nil, .some): return false
            default:
                let aTime = a.changed ?? .distantPast, bTime = b.changed ?? .distantPast
                if aTime != bTime { return aTime < bTime }
                if a.session.id != b.session.id { return a.session.id < b.session.id }
                return a.server < b.server
            }
        }
        let aKey = key(a), bKey = key(b)
        if AttentionOrder.precedes(aKey, bKey) { return true }
        if AttentionOrder.precedes(bKey, aKey) { return false }
        return a.server < b.server
    }

    /// The groups the list is ordered in, first to last.
    enum Group: Int, Comparable {
        case waiting, unseen, working, idle
        static func < (a: Group, b: Group) -> Bool { a.rawValue < b.rawValue }
    }

    static func group(_ row: Row) -> Group {
        switch row.stale ? .idle : row.status {
        case .awaitingResponse: .waiting
        case .running: .working
        case .idle: row.unseen ? .unseen : .idle
        }
    }

    private static func key(_ row: Row) -> AttentionOrder.Key {
        let status = row.stale ? .idle : row.status
        return AttentionOrder.Key(status: status, waitingSince: status == .awaitingResponse ? row.waitingSince : nil,
                                  updated: row.changed, id: row.session.id)
    }

    /// Every row, latest activity first, for what follows recent work
    /// rather than what needs you, such as a new session's server and folder.
    var recentRows: [Row] {
        rows.sorted { ($0.changed ?? .distantPast) > ($1.changed ?? .distantPast) }
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

    /// The title of a session its server lists, or nil for one it doesn't.
    func title(of id: SessionIdentity) -> String? {
        listings[id.server]?.titles[id.session]
    }

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
            drafts.forget(name)
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
        let request = listing[server.name, default: 0] + 1
        listing[server.name] = request
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
            guard listings[server.name]?.server == server, listing[server.name] == request else { return }
            let name = server.name
            seen.listed(name, sessions: list.map { ($0.id, SessionFormat.date($0.lastActivity ?? $0.date), statuses[$0.id, default: .idle] != .idle) },
                        at: now(), showing: { self.onScreen == SessionIdentity(server: name, session: $0) })
            drafts.listed(name, sessions: Set(list.map(\.id)))
            listings[server.name]?.sessions = list
            listings[server.name]?.activity = statuses
            listings[server.name]?.waitingSince = since
            listings[server.name]?.listedAt = now()
            listings[server.name]?.problem = nil
        } catch {
            guard !isCancellation(error), listings[server.name]?.server == server, listing[server.name] == request else { return }
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

    /// Records that a session was shown as of its latest change. `working`
    /// is whether the screen saw it working or waiting on the user, if known.
    /// It counts only when the screen is ahead of the list, as when a message
    /// was just sent from it; otherwise what the list last saw stands, since
    /// a screen that hasn't caught up could undo a turn the list saw start
    /// or end.
    func markSeen(_ id: SessionIdentity, changed: Date? = nil, working: Bool? = nil) {
        let latest = rows.first { $0.id == id }?.changed
        let ahead = changed.map { shown in latest.map { shown > $0 } ?? true } ?? false
        seen.markSeen(id, at: max(changed ?? .distantPast, latest ?? .distantPast), working: ahead ? working : nil)
        seenRevision += 1
    }
}
