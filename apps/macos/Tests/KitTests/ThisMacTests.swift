import AppKit
import Foundation
import Synchronization
import Testing
@testable import Kit

/// This Mac's machine identifier, derived as a Kit server derives the
/// `machineId` its health reports. The expected values are the Go server's
/// (`internal/serveridentity`) for the same inputs.
struct LocalMachineTests {
    private func home() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("kit-machine-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    @Test func hashesTheHardwareUUIDAsTheServerDoes() throws {
        let home = try home()
        defer { try? FileManager.default.removeItem(at: home) }
        #expect(LocalMachine.identifiers(home: home) { "4C1E7F7A-0000-4000-8000-1234567890AB" }
                == ["ad2a367531a7e696b1abf4c27c8546746e2a69e2a8eb356e369f242709105f0c"])
    }

    @Test func theHomesMachineValueIsThisMacToo() throws {
        let home = try home()
        defer { try? FileManager.default.removeItem(at: home) }
        let file = home.appendingPathComponent("machine-id")
        // A home whose server never needed a value has none, and reading
        // doesn't create one.
        #expect(LocalMachine.identifiers(home: home) { nil } == [])
        #expect(!FileManager.default.fileExists(atPath: file.path))

        // A server that couldn't read the hardware UUID reports the home's
        // value, so both are this Mac's.
        try (String(repeating: "0123456789abcdef", count: 4) + "\n").write(to: file, atomically: true, encoding: .utf8)
        #expect(LocalMachine.identifiers(home: home) { nil }
                == ["a8d7cb180f049965e92d42a5cf76f003d7f8423f7e97ae42d92a3cc2381aaee4"])
        #expect(LocalMachine.identifiers(home: home) { "4C1E7F7A-0000-4000-8000-1234567890AB" }
                == ["ad2a367531a7e696b1abf4c27c8546746e2a69e2a8eb356e369f242709105f0c",
                    "a8d7cb180f049965e92d42a5cf76f003d7f8423f7e97ae42d92a3cc2381aaee4"])

        try "not a machine value\n".write(to: file, atomically: true, encoding: .utf8)
        #expect(LocalMachine.identifiers(home: home) { nil } == [])
    }

    @Test func readsThisMacsHardwareUUID() throws {
        let uuid = try #require(LocalMachine.platformUUID())
        #expect(UUID(uuidString: uuid) != nil)
    }
}

@MainActor struct ThisMacConnectionTests {
    nonisolated private static let thisMac = String(repeating: "b", count: 64)
    nonisolated private static let elsewhere = String(repeating: "c", count: 64)

    /// Session `a`, in `/work/kit`, which every server lists.
    nonisolated private static let session = SessionExcerpt(id: "a", title: "A", sourceTitle: "", model: "m", thinking: "high",
                                                            workspace: "kit", cwd: "/work/kit", date: "", messages: [])

    nonisolated private static func identity(machine: String) -> ServerIdentity {
        ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: "i", kitVersion: "0.48.0",
                       protocolVersion: kitWireVersion, machine: machine, home: "/home/kit")
    }

    /// Answers a handshake with whatever machine it is set to, or fails.
    private final class Machine: Sendable {
        private let state: Mutex<Result<String, ClientError>>
        init(_ machine: String) { state = Mutex(.success(machine)) }
        var answer: Result<String, ClientError> {
            get { state.withLock { $0 } }
            set { state.withLock { $0 = newValue } }
        }
    }

    /// The local server on this Mac and devbox, answering as `devbox` says.
    private func servers(localMachines: @escaping () -> Set<String> = { [thisMac] }, devbox: Machine,
                         saved: @escaping () -> [SavedServer] = { [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox")] })
        -> ServerDirectory {
        ServerDirectory(home: URL(fileURLWithPath: "/tmp/kit-home"), localMachines: localMachines, saved: saved) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector),
                             catalogClient: FixtureClient(fixture: Fixture(sessions: [Self.session])),
                             schedule: .init(firstRetry: .milliseconds(20), maximumRetry: .milliseconds(40), check: .milliseconds(30))) {
                if name == "devbox" { return Self.identity(machine: try devbox.answer.get()) }
                return Self.identity(machine: Self.thisMac)
            }
        }
    }

    @Test func aServerIsOnThisMacOnlyWhenItsVerifiedMachineMatches() async throws {
        let servers = servers(devbox: Machine(Self.elsewhere))
        let devbox = try #require(servers.connection(named: "devbox"))
        // Nothing is known before a server verifies, whatever its address.
        #expect(servers.local.isThisMac == nil)
        #expect(devbox.isThisMac == nil)
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        try await eventually { servers.local.status.isConnected && devbox.status.isConnected }
        #expect(servers.isThisMac("local") == true)
        #expect(servers.isThisMac("devbox") == false)
        #expect(servers.isThisMac("gone") == nil)
    }

    @Test func untilThisMacsMachineIsKnownNoServerIsOnThisMac() async throws {
        var machines: Set<String> = []
        let servers = servers(localMachines: { machines }, devbox: Machine(Self.thisMac))
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        try await eventually { servers.connections.allSatisfy(\.status.isConnected) }
        #expect(servers.connections.map(\.isThisMac) == [nil, nil])
        // A server may write the home's machine value after the app opens.
        machines = [Self.thisMac]
        servers.refresh()
        #expect(servers.connections.map(\.isThisMac) == [true, true])
    }

    @Test func theVerifiedMachineOutlastsAnOutageButNotARepoint() async throws {
        let answer = Machine(Self.thisMac)
        var saved = [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox")]
        let servers = servers(devbox: answer, saved: { saved })
        let devbox = try #require(servers.connection(named: "devbox"))
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        try await eventually { devbox.status.isConnected }
        #expect(devbox.isThisMac == true)

        answer.answer = .failure(.noDaemon)
        devbox.checkNow()
        try await eventually { if case .unreachable = devbox.status { true } else { false } }
        #expect(devbox.isThisMac == true)

        // Reached another way, it may be another machine until it verifies.
        saved = [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox2")]
        servers.refresh()
        #expect(devbox.isThisMac == nil)
        answer.answer = .success(Self.elsewhere)
        try await eventually { devbox.status.isConnected }
        #expect(devbox.isThisMac == false)
    }

    @Test func aServerSavedLaterKnowsThisMac() async throws {
        var saved: [SavedServer] = []
        let servers = servers(devbox: Machine(Self.thisMac), saved: { saved })
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        saved = [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox")]
        servers.refresh()
        let devbox = try #require(servers.connection(named: "devbox"))
        #expect(devbox.localMachines == [Self.thisMac])
        try await eventually { devbox.status.isConnected }
        #expect(devbox.isThisMac == true)
    }

    @Test func aNewSessionChoosesItsFolderOnlyOnThisMac() async throws {
        let servers = servers(devbox: Machine(Self.elsewhere))
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        try await eventually { servers.connections.allSatisfy(\.status.isConnected) }
        #expect(NewSessionPlace(servers.local) == .thisMac("local"))
        #expect(NewSessionPlace(try #require(servers.connection(named: "devbox"))) == .elsewhere("devbox", home: "/home/kit"))
    }

    // MARK: Session tabs

    /// A server's session client that keeps its watch open.
    private actor TabClient: SessionClient {
        nonisolated let serverID: String
        nonisolated let isDemo = false
        init(_ serverID: String) { self.serverID = serverID }
        func sessions() async throws -> [SessionExcerpt] { [] }
        func snapshot(_ id: String) async throws -> SessionExcerpt { throw ClientError.missingSession }
        func watch(_ id: String, receive: @escaping @Sendable (SessionExcerpt) async -> Void) async throws {
            while !Task.isCancelled { try await Task.sleep(for: .seconds(60)) }
        }
    }

    /// A tab's store for session `a` on `server`.
    private func store(on server: String, servers: ServerDirectory) throws -> (SessionWindowModel, SessionStore) {
        let model = SessionWindowModel(id: UUID(), servers: servers, sessions: [SessionIdentity(server: server, session: "a")])
        let store = try #require(model.store(for: model.tabs[0], client: TabClient(server)))
        return (model, store)
    }

    @Test func aTabOnThisMacOpensTerminalsInItsFolder() async throws {
        let servers = servers(devbox: Machine(Self.thisMac))
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        try await eventually { servers.connections.allSatisfy { $0.status.isConnected && $0.catalog.loaded } }
        // Another Kit home on this Mac is this Mac too.
        let (model, store) = try store(on: "devbox", servers: servers)
        defer { model.closeAll() }
        #expect(store.thisMacGate == .available)
        #expect(store.terminalDirectory == "/work/kit")
        let items = TileAddButton.items(state: store, workspace: store.ui.workspace)
        #expect(items.first == TileAddMenu.Item(choice: .terminal, title: "Terminal", icon: .terminalWindow))
    }

    @Test func aTabOnAnotherMachineDimsTerminalWithTheReason() async throws {
        let servers = servers(devbox: Machine(Self.elsewhere))
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        try await eventually { servers.connections.allSatisfy { $0.status.isConnected && $0.catalog.loaded } }
        let (model, store) = try store(on: "devbox", servers: servers)
        defer { model.closeAll() }
        #expect(store.offersTerminal)
        #expect(store.thisMacGate == .needsThisMac)
        #expect(store.terminalDirectory == nil)
        let items = TileAddButton.items(state: store, workspace: store.ui.workspace)
        #expect(items.first == TileAddMenu.Item(choice: .terminal, title: "Terminal", icon: .terminalWindow,
                                                unavailableReason: "needs this Mac"))
        #expect(items.map(\.choice) == [.terminal, .browser, .file])
        #expect(PaletteCommand.builtIns(terminal: store.offersTerminal).contains { $0.id == "terminal" })
        #expect(store.terminalUnavailableReason == "needs this Mac")
    }

    @Test func aTabFollowsItsServerWhenItVerifiesAndWhenItIsRenamed() async throws {
        let devboxMachine = Machine(Self.elsewhere)
        devboxMachine.answer = .failure(.noDaemon)
        var saved = [SavedServer(name: "devbox", kind: .home, address: "/tmp/devbox"),
                     SavedServer(name: "mini", kind: .home, address: "/tmp/mini")]
        let servers = ServerDirectory(home: URL(fileURLWithPath: "/tmp/kit-home"), localMachines: { [Self.thisMac] },
                                      saved: { saved }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector),
                             catalogClient: FixtureClient(fixture: Fixture(sessions: [Self.session])),
                             schedule: .init(firstRetry: .milliseconds(20), maximumRetry: .milliseconds(40), check: .milliseconds(30))) {
                if name == "devbox" { return Self.identity(machine: try devboxMachine.answer.get()) }
                return Self.identity(machine: Self.thisMac)
            }
        }
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        let (model, store) = try store(on: "devbox", servers: servers)
        defer { model.closeAll() }
        #expect(store.thisMacGate == .unverified)

        // A view reading the gate updates when the server verifies.
        let changed = Mutex(false)
        withObservationTracking { _ = store.thisMacGate } onChange: { changed.withLock { $0 = true } }
        devboxMachine.answer = .success(Self.elsewhere)
        try await eventually { changed.withLock { $0 } }
        #expect(store.thisMacGate == .needsThisMac)

        // Renamed, the tab's session is read through the renamed server.
        saved = [SavedServer(name: "mini", kind: .home, address: "/tmp/mini")]
        servers.refresh()
        store.rebind(to: TabClient("mini"))
        try await eventually { servers.connection(named: "mini")?.status.isConnected == true }
        #expect(store.thisMacGate == .available)
    }

    @Test func aTabWhoseServerHasntVerifiedWaitsForIt() throws {
        let servers = servers(devbox: Machine(Self.thisMac))
        let (model, store) = try store(on: "devbox", servers: servers)
        defer { model.closeAll() }
        #expect(store.thisMacGate == .unverified)
        #expect(store.thisMacGate.reason == "waiting for the server")
        #expect(store.terminalDirectory == nil)
    }
}

struct ThisMacGateTests {
    @Test func gatesFollowTheServersMachine() {
        #expect(ThisMacGate(thisMac: true) == .available)
        #expect(ThisMacGate(thisMac: false) == .needsThisMac)
        #expect(ThisMacGate(thisMac: nil) == .unverified)
        #expect([ThisMacGate.available, .needsThisMac, .unverified].map(\.reason) == [nil, "needs this Mac", "waiting for the server"])
    }

    @Test func aFileTabNamesTheFileOnTheServer() {
        let actions = FileTabActions(file: "Sources/App.swift", cwd: "/home/me/src/kit", gate: .needsThisMac)
        #expect(actions.path == "/home/me/src/kit/Sources/App.swift")
        #expect(actions.localReason == "needs this Mac")
        #expect(FileTabActions(file: "/etc/hosts", cwd: nil, gate: .available).path == "/etc/hosts")
        let unknown = FileTabActions(file: "a.txt", cwd: nil, gate: .available)
        #expect(unknown.path == nil)
        #expect(unknown.localReason == "Session unavailable")
        #expect(FileTabActions(file: "a.txt", cwd: "/work", gate: .available).localReason == nil)
    }

    @Test func programsAskBeforeTheyRun() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("kit-open-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        func file(_ name: String, executable: Bool = false) throws -> URL {
            let url = directory.appendingPathComponent(name)
            try "echo hi\n".write(to: url, atomically: true, encoding: .utf8)
            if executable { try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path) }
            return url
        }
        #expect(FileTabActions.runsWhenOpened(try file("notes.md")) == false)
        #expect(FileTabActions.runsWhenOpened(try file("App.swift")) == false)
        #expect(FileTabActions.runsWhenOpened(try file("build", executable: true)))
        #expect(FileTabActions.runsWhenOpened(try file("Run.command")))
        #expect(FileTabActions.runsWhenOpened(URL(fileURLWithPath: "/System/Applications/Calculator.app")))
        #expect(FileTabActions.runsWhenOpened(try file("Home.fileloc")))
        let disguised = directory.appendingPathComponent("notes.txt")
        try FileManager.default.createSymbolicLink(at: disguised, withDestinationURL: try file("tool", executable: true))
        #expect(FileTabActions.runsWhenOpened(disguised))
    }

    @MainActor @Test func copyPathCopiesThePathOnTheServer() {
        let pasteboard = NSPasteboard(name: .init("kit-tests-" + UUID().uuidString))
        defer { pasteboard.releaseGlobally() }
        FileTabActions(file: "Sources/App.swift", cwd: "/home/me/src/kit", gate: .needsThisMac).copyPath(to: pasteboard)
        #expect(pasteboard.string(forType: .string) == "/home/me/src/kit/Sources/App.swift")
    }
}

struct ThisMacServerPresentationTests {
    private let identity = ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: "i", kitVersion: "0.48.0",
                                          protocolVersion: kitWireVersion, machine: String(repeating: "b", count: 64), home: "/Users/me")

    @Test func thisMacsServerOffersUpdateUnlessItVerifiedElsewhere() throws {
        let newest = try #require(ReleaseVersion("0.49.0"))
        let home = ServerConnector.home(path: "/tmp/kit-home")
        #expect(ServerRowPresentation(name: "local", connector: home, status: .connected(identity), newest: newest,
                                      thisMac: true).action == .update)
        #expect(ServerRowPresentation(name: "local", connector: home, status: .connected(identity), newest: newest,
                                      thisMac: false).action == .menu)
        // A server too old for this app never verifies, and still offers it.
        let mismatch = ServerMismatch(serverKitVersion: "0.45.0", serverProtocol: 46, appProtocol: kitWireVersion, reason: .daemonProtocolOlder)
        #expect(ServerRowPresentation(name: "local", connector: home, status: .incompatible(mismatch), newest: newest,
                                      thisMac: nil).action == .update)
    }

    @Test func aServedServersRestartRunsHereOnlyOnThisMac() {
        let mismatch = ServerMismatch(serverKitVersion: "0.45.0", serverProtocol: 46, appProtocol: kitWireVersion, reason: .daemonProtocolOlder)
        let served = ServerConnector.url("http://127.0.0.1:7777", credential: DeviceCredential(home: "/tmp/kit-home", name: "vps"))
        let here = ServerStatePresentation(name: "vps", connector: served, status: .incompatible(mismatch), sessionTab: true,
                                           thisMac: true, appVersion: "Kit for Mac 0.5.0")
        #expect(here.action == .restart(command: "kit server restart", prompt: "After updating kit, run:"))
        // A loopback address says nothing: SSH forwarding answers there too.
        let unknown = ServerStatePresentation(name: "vps", connector: served, status: .incompatible(mismatch), sessionTab: true,
                                              thisMac: nil, appVersion: "Kit for Mac 0.5.0")
        #expect(unknown.action == .restart(command: "kit server restart", prompt: "After updating kit on vps, run there:"))
    }
}
