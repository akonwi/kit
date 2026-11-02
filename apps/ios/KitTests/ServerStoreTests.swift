import Foundation
import KitClient
import Testing
@testable import Kit

@MainActor
struct ServerStoreTests {
    let link = PairingLink("https://studio.tail3f2a.ts.net/pair#abcdefghijklmnopqrstuvwxyz")!

    @Test func pairingSavesTheServerAndItsTokenThenChecksIt() async throws {
        let (store, tokens, list) = try makeStore(connector: FakeConnector(status: .success(ServerStatus(kitVersion: "0.53.0", sessions: 6))))

        try await store.pair(link, as: "studio", deviceName: "Akonwi’s iPhone")

        #expect(try SavedServerList.read(list) == [SavedServer(name: "studio", kind: .url, address: "https://studio.tail3f2a.ts.net")])
        #expect(tokens.token(for: DeviceCredential(home: "ios", name: "studio"), url: "https://studio.tail3f2a.ts.net") == FakeConnector.token)
        #expect(store.entries.map(\.id) == ["studio"])
        #expect(store.entry("studio")?.state == .online(kitVersion: "0.53.0"))
        #expect(store.entry("studio")?.sessions == 6)
    }

    @Test func aRefusedCodeReleasesTheName() async throws {
        let (store, tokens, list) = try makeStore(connector: FakeConnector(redeem: .failure(PairingError.codeRefused)))

        await #expect(throws: PairingError.codeRefused) {
            try await store.pair(link, as: "studio", deviceName: "iPhone")
        }

        #expect(try SavedServerList.read(list) == [])
        #expect(store.entries == [])
        #expect(tokens.saved == [:])
    }

    @Test func aServerOfAnOlderProtocolIsKeptAsNeedingAnUpdate() async throws {
        let mismatch = ClientError.incompatibleDaemon(daemonVersion: "0.49.2", appProtocol: 50, daemonProtocol: 48, reason: .daemonProtocolOlder)
        let (store, _, _) = try makeStore(connector: FakeConnector(status: .failure(mismatch)))

        try await store.pair(link, as: "studio", deviceName: "iPhone")

        #expect(store.entry("studio")?.state == .serverNeedsUpdate(kitVersion: "0.49.2", serverProtocol: 48))
    }

    @Test func aURLAlreadySavedIsRefusedBeforeTheCodeIsSpent() async throws {
        let connector = FakeConnector()
        let (store, _, _) = try makeStore(connector: connector)
        try await store.pair(link, as: "studio", deviceName: "iPhone")

        await #expect(throws: PairingError.alreadySaved(name: "studio", url: "https://studio.tail3f2a.ts.net")) {
            try await store.pair(link, as: "other", deviceName: "iPhone")
        }
        #expect(connector.redemptions == 1)
    }

    @Test func pairingAgainNeedsALinkForTheSameServer() async throws {
        let (store, _, _) = try makeStore(connector: FakeConnector())
        try await store.pair(link, as: "studio", deviceName: "iPhone")
        let other = PairingLink("https://vps.example.com/pair#abcdefghijklmnopqrstuvwxyz")!

        await #expect(throws: PairingError.otherServer(expected: "https://studio.tail3f2a.ts.net", link: "https://vps.example.com")) {
            try await store.pairAgain("studio", link: other, deviceName: "iPhone")
        }
    }

    @Test func aServerWithoutATokenNeedsPairing() async throws {
        let (store, _, list) = try makeStore(connector: FakeConnector())
        try SavedServerList.add(SavedServer(name: "laptop", kind: .url, address: "https://laptop.example.com"), to: list)

        await store.checkAll()

        #expect(store.entry("laptop")?.state == .needsPairing(.missing))
    }

    @Test func removingForgetsTheServerAndItsToken() async throws {
        let (store, tokens, list) = try makeStore(connector: FakeConnector())
        try await store.pair(link, as: "studio", deviceName: "iPhone")

        try store.remove("studio")

        #expect(try SavedServerList.read(list) == [])
        #expect(store.entries == [])
        #expect(tokens.saved == [:])
    }

    @Test func anUnreachableServerKeepsItsLastSessionCount() async throws {
        let connector = FakeConnector()
        let (store, _, _) = try makeStore(connector: connector, now: Date(timeIntervalSince1970: 100))
        try await store.pair(link, as: "studio", deviceName: "iPhone")
        connector.status = .failure(URLError(.cannotConnectToHost))

        await store.check("studio")

        #expect(store.entry("studio")?.state == .unreachable(since: Date(timeIntervalSince1970: 100)))
        #expect(store.entry("studio")?.sessions == 3)
    }

    private func makeStore(connector: FakeConnector, now: Date = Date()) throws -> (ServerStore, MemoryTokens, URL) {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        let list = directory.appendingPathComponent("servers.json")
        let tokens = MemoryTokens()
        return (ServerStore(list: list, tokens: tokens, connector: connector, now: { now }), tokens, list)
    }
}

struct ServerNamingTests {
    @Test func aHostIsNamedByItsFirstLabel() {
        #expect(ServerNaming.suggested(host: "studio.tail3f2a.ts.net", taken: []) == "studio")
    }

    @Test func anAddressIsNamedWhole() {
        #expect(ServerNaming.suggested(host: "192.168.1.20", taken: []) == "192.168.1.20")
    }

    @Test func aTakenNameGetsTheNextNumber() {
        #expect(ServerNaming.suggested(host: "studio.example.com", taken: ["studio", "studio-2"]) == "studio-3")
    }

    @Test func theReservedLocalNameIsNeverOffered() {
        #expect(ServerNaming.suggested(host: "local", taken: []) == "local-2")
    }
}

struct ServerStateTests {
    @Test func expiredAndRevokedPairingsBothNeedPairingAgain() {
        #expect(ServerState.from(ClientError.needsPairing(.expired), at: .now) == .needsPairing(.expired))
        #expect(ServerState.from(ClientError.http(401), at: .now) == .needsPairing(.refused))
        #expect(ServerStatePresentation.summary(.needsPairing(.expired)) == "No longer paired. Pair again.")
        #expect(ServerStatePresentation.summary(.needsPairing(.refused)) == "No longer paired. Pair again.")
    }

    @Test func aNewerServerAsksForANewerApp() {
        let error = ClientError.incompatibleDaemon(daemonVersion: "0.60.0", appProtocol: 50, daemonProtocol: 51, reason: .clientProtocolOlder)
        #expect(ServerState.from(error, at: .now) == .appNeedsUpdate(kitVersion: "0.60.0", serverProtocol: 51))
    }

    @Test func networkFailuresMeanUnreachable() {
        let date = Date(timeIntervalSince1970: 5)
        #expect(ServerState.from(URLError(.timedOut), at: date) == .unreachable(since: date))
    }

    @Test func elapsedTimeIsShort() {
        let start = Date(timeIntervalSince1970: 0)
        #expect(ServerRow.elapsed(start, now: start.addingTimeInterval(20)) == "just now")
        #expect(ServerRow.elapsed(start, now: start.addingTimeInterval(75)) == "1m ago")
        #expect(ServerRow.elapsed(start, now: start.addingTimeInterval(7200)) == "2h ago")
    }
}

struct ServerAddressTests {
    @Test func httpsShowsTheHostAndHTTPShowsTheWholeURL() {
        #expect(ServerAddress.display("https://studio.tail3f2a.ts.net") == "studio.tail3f2a.ts.net")
        #expect(ServerAddress.display("https://kit.example.com:8443") == "kit.example.com:8443")
        #expect(ServerAddress.display("http://192.168.1.20:7777") == "http://192.168.1.20:7777")
    }

    @Test func tailnetHostsAreRecognized() {
        #expect(ServerAddress.isTailnet("https://studio.tail3f2a.ts.net"))
        #expect(ServerAddress.isTailnet("http://100.101.2.3:7777"))
        #expect(!ServerAddress.isTailnet("http://192.168.1.20:7777"))
    }
}

final class FakeConnector: ServerNetwork, @unchecked Sendable {
    static let token = "kit_device_" + String(repeating: "a", count: 43)
    private let lock = NSLock()
    private var _redeem: Result<String, any Error>
    private var _status: Result<ServerStatus, any Error>
    private var _redemptions = 0

    init(redeem: Result<String, any Error> = .success(FakeConnector.token),
         status: Result<ServerStatus, any Error> = .success(ServerStatus(kitVersion: "0.53.0", sessions: 3))) {
        _redeem = redeem
        _status = status
    }

    var status: Result<ServerStatus, any Error> {
        get { lock.withLock { _status } }
        set { lock.withLock { _status = newValue } }
    }

    var redemptions: Int { lock.withLock { _redemptions } }

    func redeem(server: URL, code: String, deviceName: String) async throws -> String {
        try lock.withLock {
            _redemptions += 1
            return try _redeem.get()
        }
    }

    func status(server: URL, token: String, name: String) async throws -> ServerStatus {
        try status.get()
    }
}

final class MemoryTokens: DeviceTokenStore, @unchecked Sendable {
    private let lock = NSLock()
    private var _saved: [DeviceCredential: (token: String, url: String)] = [:]

    var saved: [DeviceCredential: String] { lock.withLock { _saved.mapValues(\.token) } }

    func token(for credential: DeviceCredential, url: String) -> String? {
        lock.withLock { _saved[credential].flatMap { $0.url == url ? $0.token : nil } }
    }

    func save(_ token: String, for credential: DeviceCredential, url: String) throws {
        lock.withLock { _saved[credential] = (token, url) }
    }

    func delete(_ credential: DeviceCredential) throws {
        _ = lock.withLock { _saved.removeValue(forKey: credential) }
    }
}
