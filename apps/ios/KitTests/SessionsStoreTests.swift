import Foundation
import KitClient
import Testing
@testable import Kit

struct SessionPreviewTests {
    @Test func aPendingConfirmationAsksForApproval() {
        var session = SessionTesting.session("a")
        session.pendingInteractions = [SessionTesting.interaction(.value0, title: "git push origin main")]
        #expect(SessionPreview.from(session) == .asking(.approve, title: "git push origin main"))
        #expect(SessionPreview.from(session)?.text(stale: false) == "Approve: git push origin main")
        #expect(SessionPreview.from(session)?.text(stale: true) == "Was asking: git push origin main")
    }

    @Test func otherQuestionsAreQuestions() {
        var session = SessionTesting.session("a")
        session.pendingInteractions = [SessionTesting.interaction(.value2, title: "Keep the old\nCSV export?")]
        #expect(SessionPreview.from(session)?.text(stale: false) == "Question: Keep the old CSV export?")
    }

    @Test func aRunningToolIsTheCurrentStep() {
        var session = SessionTesting.session("a")
        session.activeRunID = "run_1"
        session.messages = [
            TranscriptMessage(id: "u", role: "user", text: "Look", tools: []),
            TranscriptMessage(id: "t", role: "tools", text: "", tools: [
                ToolActivity(id: "1", name: "read", summary: "", output: "x", arguments: #"{"path":"Sources/A.swift"}"#, failed: false, status: "Done"),
                ToolActivity(id: "2", name: "read", summary: "", output: "", arguments: #"{"path":"Client/SessionReplica.swift"}"#, failed: false, status: "Running…"),
            ]),
        ]
        #expect(SessionPreview.from(session) == .working("Reading SessionReplica.swift"))
        #expect(SessionPreview.from(session)?.text(stale: true) == "Was reading SessionReplica.swift")
    }

    @Test func aRunningCommandShowsTheCommand() {
        let tool = ToolActivity(id: "1", name: "bash", summary: "", output: "", arguments: #"{"command":"go test ./..."}"#, failed: false, status: "Running…")
        #expect(SessionPreview.working(tool) == "Running go test ./...")
    }

    @Test func aRunWithoutAToolShowsItsActivity() {
        var session = SessionTesting.session("a")
        session.activeRunID = "run_1"
        session.activity = "Thinking…"
        #expect(SessionPreview.from(session) == .working("Thinking…"))
    }

    @Test func aFinishedSessionShowsTheStartOfItsLastReply() {
        var session = SessionTesting.session("a")
        session.messages = [
            TranscriptMessage(id: "1", role: "assistant", text: "Older", tools: []),
            TranscriptMessage(id: "2", role: "assistant", text: "```swift\nlet a = 1\n```\n\n**Done.** The client\nresumes.", tools: []),
        ]
        #expect(SessionPreview.from(session) == .replied("Done. The client resumes."))
    }

    @Test func anEmptySessionHasNoPreview() {
        #expect(SessionPreview.from(SessionTesting.session("a")) == nil)
    }
}

struct SessionFormatTests {
    let calendar = Calendar(identifier: .gregorian)

    @Test func agesAreShort() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        #expect(SessionFormat.age(now.addingTimeInterval(-20), now: now, calendar: calendar) == "now")
        #expect(SessionFormat.age(now.addingTimeInterval(-240), now: now, calendar: calendar) == "4m")
        let morning = calendar.startOfDay(for: now).addingTimeInterval(3600)
        #expect(SessionFormat.age(morning, now: morning.addingTimeInterval(7200), calendar: calendar) == "2h")
        #expect(SessionFormat.age(morning.addingTimeInterval(-86400), now: morning.addingTimeInterval(7200), calendar: calendar) == "Yesterday")
    }

    @Test func homeDirectoriesAreAbbreviated() {
        #expect(SessionFormat.directory("/Users/akonwi/Developer/agent/kit") == "~/Developer/agent/kit")
        #expect(SessionFormat.directory("/home/kit") == "~")
        #expect(SessionFormat.directory("/root/src") == "~/src")
        #expect(SessionFormat.directory("/srv/kit") == "/srv/kit")
    }
}

@MainActor
struct SessionsStoreTests {
    let studio = SavedServer(name: "studio", kind: .url, address: "https://studio.example.ts.net")
    let vps = SavedServer(name: "vps", kind: .url, address: "https://vps.example.ts.net")

    @Test func sessionsFromEveryServerAreMergedLatestFirst() async {
        let network = FakeSessionsNetwork()
        network.set(studio, sessions: [SessionTesting.session("a", at: 100), SessionTesting.session("c", at: 300)])
        network.set(vps, sessions: [SessionTesting.session("b", at: 200)])
        let store = SessionTesting.store(network)
        await store.refresh([studio, vps])
        #expect(store.rows.map(\.id) == [
            SessionIdentity(server: "studio", session: "c"),
            SessionIdentity(server: "vps", session: "b"),
            SessionIdentity(server: "studio", session: "a"),
        ])
    }

    /// Sessions that need you come first, the one waiting longest first, then
    /// working ones, then the rest, latest first, merged across servers.
    @Test func sessionsAreMergedInAttentionOrder() async {
        let network = FakeSessionsNetwork()
        let base = Date(timeIntervalSince1970: 1_800_000_000)
        network.set(studio, sessions: [SessionTesting.session("idle-new", at: 900), SessionTesting.session("asked-later", at: 500),
                                       SessionTesting.session("working-old", at: 100)],
                    activity: ["asked-later": .awaitingResponse, "working-old": .running],
                    waitingSince: ["asked-later": base.addingTimeInterval(400)])
        network.set(vps, sessions: [SessionTesting.session("asked-first", at: 50), SessionTesting.session("working-new", at: 600),
                                    SessionTesting.session("idle-old", at: 10)],
                    activity: ["asked-first": .awaitingResponse, "working-new": .running],
                    waitingSince: ["asked-first": base.addingTimeInterval(40)])
        let store = SessionTesting.store(network)
        await store.refresh([studio, vps])
        #expect(store.rows.map(\.session.id) == ["asked-first", "asked-later", "working-new", "working-old", "idle-new", "idle-old"])
        #expect(network.activityRequests.isEmpty)
    }

    /// A server that predates the attention order is asked for its activity
    /// besides, and its sessions are ordered here, a waiting one by its last
    /// change.
    @Test func anOlderServersSessionsAreOrderedHere() async {
        let network = FakeSessionsNetwork()
        network.set(vps, sessions: [SessionTesting.session("idle", at: 900), SessionTesting.session("asked", at: 100),
                                    SessionTesting.session("working", at: 200)],
                    activity: ["asked": .awaitingResponse, "working": .running], legacy: true)
        let store = SessionTesting.store(network)
        await store.refresh([vps])
        #expect(store.rows.map(\.session.id) == ["asked", "working", "idle"])
        #expect(network.activityRequests == ["vps"])
    }

    /// An unreachable server's statuses are only as last seen, so its
    /// sessions are ordered as idle among the others.
    @Test func staleSessionsAreOrderedAsIdle() async {
        let network = FakeSessionsNetwork()
        network.set(vps, sessions: [SessionTesting.session("was-asking", at: 100)], activity: ["was-asking": .awaitingResponse],
                    waitingSince: ["was-asking": Date(timeIntervalSince1970: 1_800_000_050)])
        network.set(studio, sessions: [SessionTesting.session("recent", at: 300)])
        let store = SessionTesting.store(network)
        await store.refresh([studio, vps])
        #expect(store.rows.map(\.session.id) == ["was-asking", "recent"])
        network.fail(vps, with: URLError(.cannotConnectToHost))
        await store.refresh([studio, vps])
        #expect(store.rows.map(\.session.id) == ["recent", "was-asking"])
    }

    @Test func sessionsListedTheFirstTimeCountAsSeen() async {
        let network = FakeSessionsNetwork()
        network.set(studio, sessions: [SessionTesting.session("a", at: 100)])
        let store = SessionTesting.store(network)
        await store.refresh([studio])
        #expect(store.rows.map(\.unseen) == [false])
        #expect(network.snapshots.isEmpty)

        network.set(studio, sessions: [SessionTesting.session("a", at: 150), SessionTesting.session("new", at: 160)])
        await store.refresh([studio])
        #expect(store.rows.map(\.unseen) == [true, true])
        #expect(Set(network.snapshots) == ["a", "new"])
    }

    @Test func openingASessionMarksItSeen() async {
        let network = FakeSessionsNetwork()
        network.set(studio, sessions: [SessionTesting.session("a", at: 100)])
        let store = SessionTesting.store(network)
        await store.refresh([studio])
        network.set(studio, sessions: [SessionTesting.session("a", at: 150)])
        await store.refresh([studio])
        let id = SessionIdentity(server: "studio", session: "a")
        #expect(store.rows.first?.unseen == true)
        store.markSeen(id)
        #expect(store.rows.first?.unseen == false)
        #expect(store.rows.first?.preview == nil)
    }

    @Test func sessionsThatNeedYouGetPreviewsAndAFilter() async {
        let network = FakeSessionsNetwork()
        var waiting = SessionTesting.session("w", at: 100)
        waiting.pendingInteractions = [SessionTesting.interaction(.value0, title: "Push?")]
        network.set(studio, sessions: [SessionTesting.session("idle", at: 50), waiting], activity: ["w": .awaitingResponse])
        let store = SessionTesting.store(network)
        await store.refresh([studio])
        #expect(store.needsYouCount == 1)
        #expect(store.rows(.needsYou).map(\.session.id) == ["w"])
        #expect(store.rows(.needsYou).first?.preview == .asking(.approve, title: "Push?"))
        #expect(network.snapshots == ["w"])
    }

    @Test func anUnreachableServerKeepsItsListAsStale() async {
        let network = FakeSessionsNetwork()
        network.set(vps, sessions: [SessionTesting.session("r", at: 100)], activity: ["r": .running])
        let store = SessionTesting.store(network)
        await store.refresh([vps])
        #expect(store.rows.first?.stale == false)

        network.fail(vps, with: URLError(.cannotConnectToHost))
        await store.refresh([vps])
        #expect(store.rows.map(\.session.id) == ["r"])
        #expect(store.rows.first?.stale == true)
        #expect(store.rows(.needsYou).isEmpty)
        #expect(store.problems.map(\.server) == ["vps"])
        guard case .unreachable = store.problems.first?.state else {
            Issue.record("Expected unreachable, got \(String(describing: store.problems.first?.state))")
            return
        }
    }

    @Test func aRemovedServerLeavesTheList() async {
        let network = FakeSessionsNetwork()
        network.set(studio, sessions: [SessionTesting.session("a", at: 100)])
        network.set(vps, sessions: [SessionTesting.session("b", at: 100)])
        let store = SessionTesting.store(network)
        await store.refresh([studio, vps])
        await store.refresh([studio])
        #expect(store.rows.map(\.server) == ["studio"])
        #expect(store.servers == ["studio"])
    }
}

enum SessionTesting {
    static func session(_ id: String, at seconds: TimeInterval = 0) -> SessionExcerpt {
        let date = ISO8601DateFormatter().string(from: Date(timeIntervalSince1970: 1_800_000_000 + seconds))
        return SessionExcerpt(id: id, title: "Session \(id)", sourceTitle: "Kit server", model: "m", thinking: "off",
                              workspace: "kit", cwd: "/Users/me/kit", date: date, lastActivity: date, messages: [])
    }

    static func interaction(_ kind: WireInteractionKind, title: String) -> WireInteractionRequest {
        WireInteractionRequest(id: "i", sessionId: "s", kind: kind, title: title, createdAt: "2026-01-01T00:00:00Z")
    }

    @MainActor static func store(_ network: FakeSessionsNetwork) -> SessionsStore {
        SessionsStore(network: network, seen: SeenSessions(defaults: UserDefaults(suiteName: UUID().uuidString)!))
    }
}

/// Lists sessions as a server would. By default it's a server that orders
/// them by attention; `legacy` makes it one that predates the order, which
/// lists them unordered and reports their activity separately.
final class FakeSessionsNetwork: SessionsNetwork, @unchecked Sendable {
    private struct Listing {
        var sessions: [SessionExcerpt]
        var activity: [String: SessionTabStatus]
        var waitingSince: [String: Date]
        var legacy: Bool
    }

    private let lock = NSLock()
    private var lists: [String: Result<Listing, any Error>] = [:]
    private var _snapshots: [String] = []
    private var _activityRequests: [String] = []

    var snapshots: [String] { lock.withLock { _snapshots } }
    /// The servers asked for their activity separately, in order.
    var activityRequests: [String] { lock.withLock { _activityRequests } }

    func set(_ server: SavedServer, sessions: [SessionExcerpt], activity: [String: SessionTabStatus] = [:],
             waitingSince: [String: Date] = [:], legacy: Bool = false) {
        lock.withLock { lists[server.name] = .success(Listing(sessions: sessions, activity: activity, waitingSince: waitingSince, legacy: legacy)) }
    }

    func fail(_ server: SavedServer, with error: any Error) {
        lock.withLock { lists[server.name] = .failure(error) }
    }

    private func list(_ server: SavedServer) throws -> Listing {
        try lock.withLock { try lists[server.name]?.get() ?? Listing(sessions: [], activity: [:], waitingSince: [:], legacy: false) }
    }

    func sessionsByAttention(on server: SavedServer) async throws -> AttentionListing {
        let listing = try list(server)
        if listing.legacy { return .unordered(listing.sessions) }
        let listed = listing.sessions.map { session in
            let status = listing.activity[session.id] ?? .idle
            return ListedSession(session: session, status: status,
                                 waitingSince: status == .awaitingResponse ? listing.waitingSince[session.id] : nil)
        }
        return .ordered(listed.sorted {
            AttentionOrder.precedes(
                .init(status: $0.status, waitingSince: $0.waitingSince, updated: SessionFormat.date($0.session.lastActivity ?? $0.session.date), id: $0.session.id),
                .init(status: $1.status, waitingSince: $1.waitingSince, updated: SessionFormat.date($1.session.lastActivity ?? $1.session.date), id: $1.session.id))
        })
    }

    func activity(on server: SavedServer) async throws -> [String: SessionTabStatus] {
        lock.withLock { _activityRequests.append(server.name) }
        return try list(server).activity
    }

    func snapshot(_ id: String, on server: SavedServer) async throws -> SessionExcerpt {
        lock.withLock { _snapshots.append(id) }
        guard let session = try list(server).sessions.first(where: { $0.id == id }) else { throw ClientError.missingSession }
        return session
    }

    func client(for server: SavedServer) -> ServerClient {
        ServerClient(serverID: server.name, connector: .url(server.address, credential: DeviceCredential(home: "test", name: server.name)),
                     tokens: MemoryTokens())
    }
}
