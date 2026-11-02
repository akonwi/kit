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

final class FakeSessionsNetwork: SessionsNetwork, @unchecked Sendable {
    private let lock = NSLock()
    private var lists: [String: Result<([SessionExcerpt], [String: SessionTabStatus]), any Error>] = [:]
    private var _snapshots: [String] = []

    var snapshots: [String] { lock.withLock { _snapshots } }

    func set(_ server: SavedServer, sessions: [SessionExcerpt], activity: [String: SessionTabStatus] = [:]) {
        lock.withLock { lists[server.name] = .success((sessions, activity)) }
    }

    func fail(_ server: SavedServer, with error: any Error) {
        lock.withLock { lists[server.name] = .failure(error) }
    }

    private func list(_ server: SavedServer) throws -> ([SessionExcerpt], [String: SessionTabStatus]) {
        try lock.withLock { try lists[server.name]?.get() ?? ([], [:]) }
    }

    func sessions(on server: SavedServer) async throws -> [SessionExcerpt] { try list(server).0 }
    func activity(on server: SavedServer) async throws -> [String: SessionTabStatus] { try list(server).1 }

    func snapshot(_ id: String, on server: SavedServer) async throws -> SessionExcerpt {
        lock.withLock { _snapshots.append(id) }
        guard let session = try list(server).0.first(where: { $0.id == id }) else { throw ClientError.missingSession }
        return session
    }

    func client(for server: SavedServer) -> ServerClient {
        ServerClient(serverID: server.name, connector: .url(server.address, credential: DeviceCredential(home: "test", name: server.name)),
                     tokens: MemoryTokens())
    }
}
