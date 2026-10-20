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

    private static func identity(instance: String = "instance-1") -> ServerIdentity {
        ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: instance, kitVersion: "0.48.0",
                       protocolVersion: kitWireVersion, machine: String(repeating: "b", count: 64), home: "/Users/kit")
    }

    private static let fast = ServerConnection.Schedule(firstRetry: .milliseconds(20), maximumRetry: .milliseconds(40),
                                                       check: .seconds(60))

    private func connection(_ script: Script, name: String = "devbox", schedule: ServerConnection.Schedule = fast) -> ServerConnection {
        let client = ServerClient(serverID: name, connector: .home(URL(fileURLWithPath: "/tmp/\(name)")))
        return ServerConnection(client: client, catalogClient: FixtureClient(fixture: Fixture(sessions: [])), schedule: schedule) {
            try await script.next()
        }
    }

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
        let slow = ServerConnection.Schedule(firstRetry: .seconds(60), maximumRetry: .seconds(60), check: .seconds(60))
        let script = Script([.success(Self.identity()), .failure(ClientError.noDaemon), .success(Self.identity())])
        let server = connection(script, schedule: slow)
        server.start()
        defer { server.stop() }
        try await eventually { server.status.isConnected && server.catalog.loaded }
        server.checkNow()
        try await eventually { if case .reconnecting = server.status { true } else { false } }
        server.checkNow()
        try await eventually { server.status == .connected(Self.identity()) }
        #expect(await script.calls == 3)
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
        let script = Script([.success(Self.identity(instance: "first")), .success(Self.identity(instance: "second"))])
        let server = connection(script)
        server.start()
        defer { server.stop() }
        try await eventually { server.status.identity?.instance == "first" }
        server.checkNow()
        try await eventually { server.status.identity?.instance == "second" }
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

    /// The local server and devbox; devbox answers with `devbox`.
    private func servers(devbox: @escaping @Sendable () async throws -> ServerIdentity) -> ServerDirectory {
        ServerDirectory(home: URL(fileURLWithPath: "/tmp/kit-home"),
                        saved: { [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox")] }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector),
                             catalogClient: FixtureClient(fixture: Fixture(sessions: []))) {
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
        let servers = servers { Self.identity }
        servers.local.catalog.recordCreated(excerpt("a", "Local A", date: "2026-01-01T00:00:00Z"))
        servers.connection(named: "devbox")?.catalog.recordCreated(excerpt("a", "Devbox A", date: "2026-01-02T00:00:00Z"))
        #expect(SessionPickerView.entries(servers.connections).isEmpty)
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        try await eventually { servers.connections.allSatisfy(\.status.isConnected) }
        let entries = SessionPickerView.entries(servers.connections)
        #expect(entries.map(\.id) == ["a", "devbox/a"])
        #expect(entries.map(\.title) == ["Local A", "Devbox A"])
        #expect(entries.map { SessionWindowModel.identity(treeKey: $0.id).serverLabel } == [nil, "devbox"])
    }
}
