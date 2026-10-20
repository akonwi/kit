import Foundation
import Testing
@testable import Kit

/// Waits for a condition the main actor reaches asynchronously.
@MainActor func eventually(_ condition: @MainActor () -> Bool) async throws {
    for _ in 0..<300 {
        if condition() { return }
        try await Task.sleep(for: .milliseconds(10))
    }
    Issue.record("Condition was not reached in time")
}

/// Waits for a condition that has to ask an actor.
@MainActor func eventually(awaiting condition: @MainActor () async -> Bool) async throws {
    for _ in 0..<300 {
        if await condition() { return }
        try await Task.sleep(for: .milliseconds(10))
    }
    Issue.record("Condition was not reached in time")
}

@MainActor struct SessionCatalogTests {
    private actor CatalogClient: SessionClient {
        nonisolated let serverID = "local"
        nonisolated let isDemo = false
        var titles = ["First"]
        func changeCatalog() { titles = ["Renamed", "New session"] }
        func sessions() async throws -> [SessionExcerpt] {
            titles.enumerated().map { index, title in
                SessionExcerpt(id: String(index), title: title, sourceTitle: "", model: "",
                               thinking: "", workspace: "", date: "", messages: [])
            }
        }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    }

    @Test func refreshLoadsTheServersCurrentCatalog() async {
        let client = CatalogClient()
        let catalog = SessionCatalog(client: client)
        await catalog.load()
        #expect(catalog.sessions.map(\.title) == ["First"])
        await client.changeCatalog()
        await catalog.refresh()
        #expect(catalog.sessions.map(\.title) == ["Renamed", "New session"])
        #expect(catalog.loaded)
        #expect(catalog.loading == false)
    }

    private actor SlowCatalogClient: SessionClient {
        nonisolated let serverID = "local"
        nonisolated let isDemo = false
        private(set) var calls = 0
        private(set) var cancelled = false
        func sessions() async throws -> [SessionExcerpt] {
            calls += 1
            if calls == 1 { return [] }
            do { try await Task.sleep(for: .seconds(60)) }
            catch { cancelled = true; throw error }
            return []
        }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    }

    @Test func cancellingARefreshReleasesTheCatalog() async throws {
        let client = SlowCatalogClient()
        let catalog = SessionCatalog(client: client)
        await catalog.load()
        let request = Task { await catalog.refresh() }
        for _ in 0..<100 {
            if await client.calls == 2 { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(await client.calls == 2)
        request.cancel()
        await request.value
        for _ in 0..<100 {
            if await client.cancelled { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(await client.cancelled)
        #expect(catalog.loading == false)
        #expect(catalog.error == nil)
    }
}

@MainActor struct ServerConnectionTests {
    /// Answers handshakes from a script, then repeats its last answer.
    private actor Script {
        private var answers: [Result<ServerIdentity, any Error>]
        private(set) var calls = 0
        init(_ answers: [Result<ServerIdentity, any Error>]) { self.answers = answers }
        func next() throws -> ServerIdentity {
            calls += 1
            let answer = answers.count > 1 ? answers.removeFirst() : answers[0]
            return try answer.get()
        }
    }

    nonisolated private static func identity(instance: String = "instance-1") -> ServerIdentity {
        ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: instance, kitVersion: "0.48.0",
                       protocolVersion: kitWireVersion, machine: String(repeating: "b", count: 64), home: "/Users/kit")
    }

    private static let fast = ServerConnection.Schedule(firstRetry: .milliseconds(20), maximumRetry: .milliseconds(40),
                                                       check: .seconds(60))

    /// A catalog that counts how often it is loaded.
    private actor CountingCatalog: SessionClient {
        nonisolated let serverID = "devbox"
        nonisolated let isDemo = false
        private(set) var loads = 0
        func sessions() async throws -> [SessionExcerpt] { loads += 1; return [] }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {}
    }

    private func connection(_ script: Script, name: String = "devbox", schedule: ServerConnection.Schedule = fast,
                            catalog: (any SessionClient)? = nil, signal: ServerSignal? = nil) -> ServerConnection {
        let client = ServerClient(serverID: name, connector: .home(URL(fileURLWithPath: "/tmp/\(name)")))
        return ServerConnection(client: client, catalogClient: catalog ?? FixtureClient(fixture: Fixture(sessions: [])),
                                schedule: schedule, signal: signal) {
            try await script.next()
        }
    }

    private static let slow = ServerConnection.Schedule(firstRetry: .seconds(60), maximumRetry: .seconds(60), check: .seconds(60))

    @Test func aReachableServerConnectsAndLoadsItsCatalog() async throws {
        let server = connection(Script([.success(Self.identity())]))
        #expect(server.status == .connecting)
        server.start()
        defer { server.stop() }
        try await eventually { server.status == .connected(Self.identity()) && server.catalog.loaded }
    }

    @Test func anUnreachableServerRetriesWithBackoffAndResumes() async throws {
        let script = Script([.failure(ClientError.noDaemon), .failure(ClientError.noDaemon), .success(Self.identity())])
        let server = connection(script)
        let before = Date()
        server.start()
        defer { server.stop() }
        try await eventually {
            if case .unreachable(let reason, let retry) = server.status {
                return reason == ClientError.noDaemon.localizedDescription && retry.map { $0 > before } == true
            }
            return false
        }
        try await eventually { server.status.isConnected }
        #expect(await script.calls == 3)
    }

    @Test func aConnectedServerReconnectsBeforeItIsUnreachable() async throws {
        let slow = ServerConnection.Schedule(firstRetry: .seconds(60), maximumRetry: .seconds(60), check: .seconds(60))
        let script = Script([.success(Self.identity()), .failure(ClientError.noDaemon), .failure(ClientError.noDaemon)])
        let server = connection(script, schedule: slow)
        server.start()
        defer { server.stop() }
        try await eventually { server.status.isConnected }
        server.checkNow()
        try await eventually { if case .reconnecting(Self.identity(), _) = server.status { true } else { false } }
        #expect(!server.status.isConnected)
        #expect(server.status.identity == Self.identity())
        server.checkNow()
        try await eventually {
            if case .unreachable(ClientError.noDaemon.localizedDescription, _) = server.status { true } else { false }
        }
    }

    @Test func aServerBackWhileReconnectingResumesWithoutReloading() async throws {
        let script = Script([.success(Self.identity()), .failure(ClientError.noDaemon), .success(Self.identity())])
        let catalog = CountingCatalog()
        let server = connection(script, schedule: Self.slow, catalog: catalog)
        server.start()
        defer { server.stop() }
        try await eventually { server.status.isConnected && server.catalog.loaded }
        server.checkNow()
        try await eventually { if case .reconnecting = server.status { true } else { false } }
        server.checkNow()
        try await eventually { server.status == .connected(Self.identity()) }
        #expect(await script.calls == 3)
        #expect(await catalog.loads == 1)
    }

    @Test func retriesBackOffByDoublingUpToTheMaximum() {
        let schedule = ServerConnection.Schedule()
        var waits = [schedule.firstRetry]
        for _ in 0..<5 { waits.append(schedule.retry(after: waits.last!)) }
        #expect(waits == [.seconds(2), .seconds(4), .seconds(8), .seconds(16), .seconds(30), .seconds(30)])
    }

    @Test func aFailedOperationChecksAConnectedServerOnce() async throws {
        let signal = ServerSignal()
        let gate = AsyncStream<Void>.makeStream()
        actor Calls { var count = 0; func add() -> Int { count += 1; return count } }
        let calls = Calls()
        let client = ServerClient(serverID: "devbox", connector: .home(URL(fileURLWithPath: "/tmp/devbox")))
        let server = ServerConnection(client: client, catalogClient: FixtureClient(fixture: Fixture(sessions: [])),
                                      schedule: Self.slow, signal: signal) {
            // The second check waits until released, standing for a slow handshake.
            if await calls.add() == 2 { for await _ in gate.stream { break } }
            return Self.identity()
        }
        server.start()
        defer { server.stop() }
        try await eventually { server.status.isConnected }
        signal.fire()
        try await eventually(awaiting: { await calls.count == 2 })
        // Failures meanwhile are answered by the check underway.
        signal.fire()
        signal.fire()
        try await Task.sleep(for: .milliseconds(50))
        gate.continuation.yield()
        try await eventually { server.status.isConnected }
        try await Task.sleep(for: .milliseconds(50))
        #expect(await calls.count == 2)
    }

    @Test func aRetiredConnectionStaysDown() async throws {
        let signal = ServerSignal()
        let script = Script([.success(Self.identity())])
        let server = connection(script, schedule: Self.slow, signal: signal)
        server.start()
        try await eventually { server.status.isConnected }
        server.retire()
        signal.fire()
        server.checkNow()
        server.start()
        try await Task.sleep(for: .milliseconds(50))
        #expect(await script.calls == 1)
    }

    @Test func aServerOfAnotherProtocolIsIncompatible() async throws {
        let older = ClientError.incompatibleDaemon(daemonVersion: "0.40.0", appProtocol: kitWireVersion,
                                                   daemonProtocol: kitWireVersion - 1, reason: .daemonProtocolOlder)
        let server = connection(Script([.failure(older)]))
        server.start()
        defer { server.stop() }
        try await eventually {
            server.status == .incompatible(ServerMismatch(serverKitVersion: "0.40.0", serverProtocol: kitWireVersion - 1,
                                                          appProtocol: kitWireVersion, reason: .daemonProtocolOlder))
        }
    }

    @Test func tryNowChecksAgainWithoutWaitingForTheRetry() async throws {
        let slow = ServerConnection.Schedule(firstRetry: .seconds(60), maximumRetry: .seconds(60), check: .seconds(60))
        let script = Script([.failure(ClientError.noDaemon), .success(Self.identity())])
        let server = connection(script, schedule: slow)
        server.start()
        defer { server.stop() }
        try await eventually { if case .unreachable = server.status { true } else { false } }
        server.checkNow()
        try await eventually { server.status.isConnected }
    }

    @Test func aRestartedServerReloadsItsCatalog() async throws {
        let script = Script([.success(Self.identity(instance: "first")), .success(Self.identity(instance: "first")),
                             .success(Self.identity(instance: "second"))])
        let catalog = CountingCatalog()
        let server = connection(script, catalog: catalog)
        server.start()
        defer { server.stop() }
        try await eventually { server.status.identity?.instance == "first" }
        try await eventually(awaiting: { await catalog.loads == 1 })
        server.checkNow()
        try await eventually(awaiting: { await script.calls == 2 })
        server.checkNow()
        try await eventually { server.status.identity?.instance == "second" }
        try await eventually(awaiting: { await catalog.loads == 2 })
    }
}

@MainActor struct ServerDirectoryTests {
    private final class Saved {
        var servers: [SavedServer] = []
    }

    private func directory(_ saved: Saved, home: String = "/Users/kit/.kit") -> ServerDirectory {
        ServerDirectory(home: URL(fileURLWithPath: home), saved: { saved.servers }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector)) { throw ClientError.noDaemon }
        }
    }

    @Test func localComesFirstThenSavedHomesByName() {
        let saved = Saved()
        saved.servers = [
            SavedServer(name: "beta", kind: .home, address: "/work/beta/home"),
            SavedServer(name: "alpha", kind: .home, address: "/work/alpha/home"),
            SavedServer(name: "vps", kind: .url, address: "https://kit.example.com"),
            SavedServer(name: "mini", kind: .ssh, address: "akonwi@mini"),
            SavedServer(name: "self", kind: .home, address: "/Users/kit/.kit"),
        ].sorted { $0.name < $1.name }
        let servers = directory(saved)
        #expect(servers.connections.map(\.name) == ["local", "alpha", "beta"])
        #expect(servers.local.connector == .home(URL(fileURLWithPath: "/Users/kit/.kit")))
        #expect(servers.connection(named: "beta")?.connector == .home(URL(fileURLWithPath: "/work/beta/home")))
        #expect(servers.client(for: "alpha")?.serverID == "alpha")
        #expect(servers.connection(named: "vps") == nil)
    }

    @Test func refreshKeepsUnchangedServersAndFollowsTheSavedList() {
        let saved = Saved()
        saved.servers = [SavedServer(name: "alpha", kind: .home, address: "/work/alpha/home"),
                         SavedServer(name: "beta", kind: .home, address: "/work/beta/home")]
        let servers = directory(saved)
        let alpha = servers.connection(named: "alpha")
        let beta = servers.connection(named: "beta")
        saved.servers = [SavedServer(name: "alpha", kind: .home, address: "/work/alpha/home"),
                         SavedServer(name: "beta", kind: .home, address: "/work/beta2/home"),
                         SavedServer(name: "gamma", kind: .home, address: "/work/gamma/home")]
        servers.refresh()
        #expect(servers.connections.map(\.name) == ["local", "alpha", "beta", "gamma"])
        #expect(servers.connection(named: "alpha") === alpha)
        #expect(servers.connection(named: "beta") !== beta)
        #expect(servers.connection(named: "beta")?.connector == .home(URL(fileURLWithPath: "/work/beta2/home")))
        saved.servers = []
        servers.refresh()
        #expect(servers.connections.map(\.name) == ["local"])
        #expect(alpha?.retired == true)
        #expect(beta?.retired == true)
    }

    @Test func aServerSavedUnderTwoNamesConnectsOnce() {
        let saved = Saved()
        saved.servers = [SavedServer(name: "alpha", kind: .home, address: "/work/shared/home"),
                         SavedServer(name: "beta", kind: .home, address: "/work/shared/home")]
        #expect(directory(saved).connections.map(\.name) == ["local", "alpha"])
    }
}

struct SavedServerListTests {
    @Test func readsKnownEntriesAndSkipsTheRest() {
        let data = Data("""
        {"servers":[
          {"name":"vps","kind":"url","url":"https://kit.example.com"},
          {"name":"devbox","kind":"home","home":"/work/devbox/home","future":"kept by kit"},
          {"name":"mini","kind":"ssh","destination":"akonwi@mini.local"},
          {"name":"cloud","kind":"carrier-pigeon","coop":"roof"},
          {"name":"Upper","kind":"home","home":"/work/upper"},
          {"name":"local","kind":"home","home":"/work/local"},
          {"name":"relative","kind":"home","home":"work/relative"},
          {"name":"devbox","kind":"home","home":"/work/second"}
        ]}
        """.utf8)
        #expect(SavedServerList.decode(data) == [
            SavedServer(name: "devbox", kind: .home, address: "/work/devbox/home"),
            SavedServer(name: "mini", kind: .ssh, address: "akonwi@mini.local"),
            SavedServer(name: "vps", kind: .url, address: "https://kit.example.com"),
        ])
    }

    @Test func aMissingOrUnreadableListHasNoServers() throws {
        let missing = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".json")
        #expect(try SavedServerList.read(missing) == [])
        #expect(SavedServerList.decode(Data("not json".utf8)) == [])
    }

    @Test func namesFollowKitServersAdd() {
        for name in ["a", "devbox", "dev-box_2.local", String(repeating: "a", count: 32)] { #expect(SavedServer.validName(name)) }
        for name in ["", "local", "Dev", "-dev", ".dev", "dev box", "dev/box", String(repeating: "a", count: 33)] {
            #expect(!SavedServer.validName(name))
        }
    }
}

struct ServerStatePresentationTests {
    private let home = URL(fileURLWithPath: "/work/devbox/home")

    @Test func anUnreachableServerShowsItsErrorAndTheNextTry() {
        let retry = Date(timeIntervalSince1970: 100)
        let presentation = ServerStatePresentation(name: "devbox", connector: .home(home),
                                                   status: .unreachable(reason: "No running Kit server found.", retry: retry),
                                                   sessionTab: true, appVersion: "Kit for Mac 0.5.0")
        #expect(presentation.title == "Can’t connect to devbox")
        #expect(presentation.message == "Kit couldn’t reach devbox. It keeps trying, and this tab picks up where it left off when devbox is back.")
        #expect(presentation.source == "home · /work/devbox/home")
        #expect(presentation.reason == "No running Kit server found.")
        #expect(presentation.versions == [])
        #expect(presentation.action == .retry(retry))
        #expect(presentation.footnote == "Your draft and layout stay in this tab. Tabs on other servers keep working.")
    }

    @Test func theLocalServerGoesUnnamed() {
        let presentation = ServerStatePresentation(name: "local", connector: .home(DevHomeResolver.defaultHome),
                                                   status: .unreachable(reason: "Refused.", retry: nil),
                                                   sessionTab: false, appVersion: "Kit for Mac 0.5.0")
        #expect(presentation.title == "Can’t connect to Kit")
        #expect(presentation.source == "local · ~/.kit")
        #expect(presentation.footnote == "Tabs on other servers keep working.")
    }

    @Test func anOlderServerGetsUpdateThenRestartSteps() {
        let mismatch = ServerMismatch(serverKitVersion: "0.45.0", serverProtocol: 46, appProtocol: 47, reason: .daemonProtocolOlder)
        let presentation = ServerStatePresentation(name: "devbox", connector: .home(home), status: .incompatible(mismatch),
                                                   sessionTab: true, appVersion: "Kit for Mac 0.5.0")
        #expect(presentation.title == "devbox needs a newer Kit")
        #expect(presentation.message == "devbox runs an older Kit than this app can use. Update kit, then restart the server. Restarting stops work running there, so Kit leaves it to you.")
        #expect(presentation.source == nil)
        #expect(presentation.reason == nil)
        #expect(presentation.versions == [.init(label: "This app", value: "Kit for Mac 0.5.0 · protocol 47", outdated: false),
                                          .init(label: "devbox", value: "Kit 0.45.0 · protocol 46", outdated: true)])
        #expect(presentation.action == .restart(command: "KIT_HOME=/work/devbox/home kit server restart", prompt: "After updating kit, run:"))
    }

    @Test func restartingTheDefaultHomeNeedsNoHome() {
        #expect(ServerStatePresentation.restartCommand(connector: .home(DevHomeResolver.defaultHome)) == "kit server restart")
        #expect(ServerStatePresentation.restartCommand(connector: .home(URL(fileURLWithPath: "/work/my kit/home")))
                == "KIT_HOME='/work/my kit/home' kit server restart")
    }

    @Test func anOlderAppLinksToTheReleases() {
        let mismatch = ServerMismatch(serverKitVersion: "0.49.0", serverProtocol: 49, appProtocol: 48, reason: .clientProtocolOlder)
        let presentation = ServerStatePresentation(name: "vps", connector: .home(home), status: .incompatible(mismatch),
                                                   sessionTab: true, appVersion: "Kit for Mac 0.5.0")
        #expect(presentation.title == "Update Kit to use vps")
        #expect(presentation.message == "vps runs a newer Kit than this app. Install the latest Kit for Mac; vps and its work keep running.")
        #expect(presentation.versions == [.init(label: "This app", value: "Kit for Mac 0.5.0 · protocol 48", outdated: true),
                                          .init(label: "vps", value: "Kit 0.49.0 · protocol 49", outdated: false)])
        #expect(presentation.action == .update)
    }
}

@MainActor struct ServerTabTests {
    nonisolated private static let identity = ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: "i",
                                                 kitVersion: "0.48.0", protocolVersion: kitWireVersion,
                                                 machine: String(repeating: "b", count: 64), home: "/Users/kit")

    /// The local server and devbox, listing `sessions` by server name;
    /// devbox answers with `devbox`.
    private func servers(sessions: [String: [SessionExcerpt]] = [:],
                         devbox: @escaping @Sendable () async throws -> ServerIdentity) -> ServerDirectory {
        ServerDirectory(home: URL(fileURLWithPath: "/tmp/kit-home"),
                        saved: { [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox")] }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector),
                             catalogClient: FixtureClient(fixture: Fixture(sessions: sessions[name] ?? []))) {
                if name == "devbox" { return try await devbox() }
                return Self.identity
            }
        }
    }

    private func excerpt(_ id: String, _ title: String, date: String) -> SessionExcerpt {
        SessionExcerpt(id: id, title: title, sourceTitle: "", model: "m", thinking: "high", workspace: "/work",
                       date: date, messages: [])
    }

    @Test func aTabWhoseServerCantServeItAsksForAttention() async throws {
        let servers = servers { throw ClientError.noDaemon }
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        let model = SessionWindowModel(id: UUID(), servers: servers,
                                       sessions: [SessionIdentity(server: "local", session: "a"),
                                                  SessionIdentity(server: "devbox", session: "b"),
                                                  SessionIdentity(server: "gone", session: "c")])
        try await eventually { model.tabs[1].serverIssue != nil && servers.local.status.isConnected }
        #expect(model.tabs.map(\.serverLabel) == [nil, "devbox", "gone"])
        #expect(model.tabs.map(\.serverIssue) == [nil, "Can’t connect", "Server isn’t saved"])
        #expect(model.tabs.map(\.statusLabel) == [nil, "Can’t connect", "Server isn’t saved"])
        #expect(model.tabs.map(\.needsAttention) == [false, true, true])
    }

    @Test func openSessionListsEveryConnectedServer() async throws {
        let servers = servers(sessions: ["local": [excerpt("a", "Local A", date: "2026-01-01T00:00:00Z")],
                                         "devbox": [excerpt("a", "Devbox A", date: "2026-01-02T00:00:00Z")]]) { Self.identity }
        #expect(SessionPickerView.entries(servers.connections).isEmpty)
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        try await eventually { servers.connections.allSatisfy { $0.status.isConnected && $0.catalog.loaded } }
        let entries = SessionPickerView.entries(servers.connections)
        #expect(entries.map(\.id) == ["a", "devbox/a"])
        #expect(entries.map(\.title) == ["Local A", "Devbox A"])
        #expect(entries.map { SessionWindowModel.identity(treeKey: $0.id).serverLabel } == [nil, "devbox"])
    }

    @Test func aServerNamePointedElsewhereGetsAFreshStore() async throws {
        var saved = [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox")]
        let servers = ServerDirectory(home: URL(fileURLWithPath: "/tmp/kit-home"), saved: { saved }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector),
                             catalogClient: FixtureClient(fixture: Fixture(sessions: []))) { Self.identity }
        }
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        let model = SessionWindowModel(id: UUID(), servers: servers, sessions: [SessionIdentity(server: "devbox", session: "a")])
        let tab = model.tabs[0]
        try await eventually { servers.connection(named: "devbox")?.status.isConnected == true }
        let first = try #require(model.store(for: tab))
        #expect(first.connector == .home(path: "/tmp/devbox"))

        saved = [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox2")]
        servers.refresh()
        try await eventually { servers.connection(named: "devbox")?.status.isConnected == true }
        let second = try #require(model.store(for: tab))
        #expect(second !== first)
        #expect(second.connector == .home(path: "/tmp/devbox2"))
        model.closeAll()
    }

    @Test func aStoreWhoseWatchGaveUpResumesWhenItsServerIsBack() async throws {
        // A registry this app can't read stops the session's watch for good.
        let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        let run = home.appendingPathComponent("run")
        try FileManager.default.createDirectory(at: run, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }
        try Data(#"{"registryVersion":2}"#.utf8).write(to: run.appendingPathComponent("server.json"))
        try Data("token".utf8).write(to: run.appendingPathComponent("server.token"))
        let servers = ServerDirectory(home: URL(fileURLWithPath: "/tmp/kit-home"),
                                      saved: { [SavedServer(name: "devbox", kind: .home, address: home.path)] }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector),
                             catalogClient: FixtureClient(fixture: Fixture(sessions: []))) { Self.identity }
        }
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        let model = SessionWindowModel(id: UUID(), servers: servers, sessions: [SessionIdentity(server: "devbox", session: "a")])
        let tab = model.tabs[0]
        try await eventually { servers.connection(named: "devbox")?.status.isConnected == true }
        let store = try #require(model.store(for: tab))
        try await eventually { store.watchStopped }
        #expect(model.store(for: tab) === store)
        // Attaching again starts a new watch, which reaches the server again.
        #expect(store.connectionState == .disconnected)
        try await eventually { store.watchStopped }
        // A watch still starting is left to run.
        model.store(for: tab)
        #expect(model.store(for: tab) === store)
        #expect(store.connectionState == .disconnected)
        model.closeAll()
    }
}
