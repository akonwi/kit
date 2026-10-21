import Foundation
import Synchronization
import Testing
@testable import Kit

struct ServedServerConnectionTests {
    private static let serverID = "server_" + String(repeating: "0123456789abcdef", count: 2)
    private static let machineID = String(repeating: "ab", count: 32)
    private static let token = "kit_device_" + String(repeating: "A", count: 43)
    private static let health = """
    {"instanceId":"instance_remote","pid":7,"kitVersion":"0.42.0","protocolVersion":\(kitWireVersion),"databaseReady":true,
     "providers":[],"serverId":"\(serverID)","machineId":"\(machineID)","home":"/home/me"}
    """

    @Test func aServedServerIsReachedWithItsDeviceToken() async throws {
        let server = try LoopbackHTTPServer(body: Self.health)
        let url = try await server.start()
        defer { server.stop() }
        let credential = DeviceCredential(home: "/Users/me/.kit", name: "vps")
        let tokens = MemoryDeviceTokens([credential: (url.absoluteString, Self.token)])
        let (client, identity) = try await ServerConnector.url(url.absoluteString, credential: credential)
            .handshake(serverID: "vps", tokens: tokens)
        #expect(client.serverID == "vps")
        #expect(identity == ServerIdentity(id: Self.serverID, instance: "instance_remote", kitVersion: "0.42.0",
                                           protocolVersion: kitWireVersion, machine: Self.machineID, home: "/home/me"))
        let request = try #require(server.requests.first)
        #expect(request.hasPrefix("GET /v1/health HTTP/1.1\r\n"))
        #expect(request.contains("\r\nAuthorization: Bearer \(Self.token)\r\n"))
    }

    @Test func aServerThatRefusesTheTokenNeedsPairing() async throws {
        for (code, need) in [("credential_expired", ClientError.PairingNeed.expired), ("unauthorized", .refused)] {
            let server = try LoopbackHTTPServer(status: "401 Unauthorized", body: #"{"error":{"code":"\#(code)","message":"no"}}"#)
            let url = try await server.start()
            defer { server.stop() }
            let credential = DeviceCredential(home: "/Users/me/.kit", name: "vps")
            let tokens = MemoryDeviceTokens([credential: (url.absoluteString, Self.token)])
            await expectClientError(.needsPairing(need)) {
                _ = try await ServerConnector.url(url.absoluteString, credential: credential).handshake(serverID: "vps", tokens: tokens)
            }
        }
    }

    @Test func aServerWithoutATokenNeedsPairingWithoutBeingAsked() async throws {
        let server = try LoopbackHTTPServer(body: Self.health)
        let url = try await server.start()
        defer { server.stop() }
        let credential = DeviceCredential(home: "/Users/me/.kit", name: "vps")
        // A token issued for another URL is never sent here.
        let tokens = MemoryDeviceTokens([credential: ("https://elsewhere.example", Self.token)])
        await expectClientError(.needsPairing(.missing)) {
            _ = try await ServerConnector.url(url.absoluteString, credential: credential).handshake(serverID: "vps", tokens: tokens)
        }
        #expect(server.requests.isEmpty)
    }

    @Test func anSSHServerCantBeReachedYet() async {
        await expectClientError(.sshUnsupported) {
            _ = try await ServerConnector.ssh("me@devbox").handshake(serverID: "devbox", tokens: MemoryDeviceTokens())
        }
    }

    @Test func redeemingSendsNoCredential() async throws {
        let server = try LoopbackHTTPServer(status: "201 Created", body: """
        {"token":"\(Self.token)","device":{"id":"device_0123456789abcdef0123456789abcdef","name":"Mac","createdAt":"2026-03-01T12:00:00Z",
         "lastUsedAt":"2026-03-01T12:00:00Z","expiresAt":"2026-03-31T12:00:00Z","current":true}}
        """)
        let url = try await server.start()
        defer { server.stop() }
        let token = try await HTTPClient.redeemPairing(url: url, code: String(repeating: "a", count: 26), deviceName: "Mac")
        #expect(token == Self.token)
        let request = try #require(server.requests.first)
        #expect(request.hasPrefix("POST /v1/pairing/redeem HTTP/1.1\r\n"))
        #expect(!request.contains("Authorization:"))
        #expect(!request.contains("X-Kit-Instance-ID:"))
    }

    @Test func aUsedCodeIsReported() async throws {
        let server = try LoopbackHTTPServer(status: "401 Unauthorized", body: #"{"error":{"code":"unauthorized","message":"no"}}"#)
        let url = try await server.start()
        defer { server.stop() }
        await #expect(throws: PairingError.codeRefused) {
            _ = try await HTTPClient.redeemPairing(url: url, code: String(repeating: "a", count: 26), deviceName: "Mac")
        }
    }

    @Test func aServedServersVerifiedTransportIsReusedUntilARequestFails() async throws {
        let server = try LoopbackHTTPServer(body: Self.health)
        let url = try await server.start()
        defer { server.stop() }
        let credential = DeviceCredential(home: "/Users/me/.kit", name: "vps")
        let tokens = MemoryDeviceTokens([credential: (url.absoluteString, Self.token)])
        let client = ServerClient(serverID: "vps", connector: .url(url.absoluteString, credential: credential), tokens: tokens)
        let first = try await client.resolve()
        #expect(try await client.resolve() === first)
        #expect(server.requests.count == 1)
        // A refused credential or another instance means verifying again.
        for failure in [TransportFailures.Failure.refused, .changed, .unreachable] {
            let current = try await client.resolve()
            current.failures.report(failure)
            #expect(try await client.resolve() !== current)
        }
        #expect(server.requests.count == 4)
        // Pointing the name elsewhere forgets the transport too.
        let verified = try await client.resolve()
        client.address.repoint(to: .url(url.absoluteString + "/", credential: credential))
        #expect(client.address.verifiedClient(for: client.connector) == nil)
        client.address.repoint(to: .url(url.absoluteString, credential: credential))
        #expect(try await client.resolve() !== verified)
    }

    @Test func aVerifiedTransportDoesntKeepItsAddressAlive() async throws {
        let server = try LoopbackHTTPServer(body: Self.health)
        let url = try await server.start()
        defer { server.stop() }
        let credential = DeviceCredential(home: "/Users/me/.kit", name: "vps")
        let tokens = MemoryDeviceTokens([credential: (url.absoluteString, Self.token)])
        weak var address: ServerAddress?
        weak var transport: HTTPClient?
        do {
            let client = ServerClient(serverID: "vps", connector: .url(url.absoluteString, credential: credential), tokens: tokens)
            address = client.address
            transport = try await client.resolve()
        }
        #expect(address == nil)
        #expect(transport == nil)
    }

    @Test func aRequestTheServerRefusesIsReported() async throws {
        let server = try LoopbackHTTPServer(status: "401 Unauthorized", body: #"{"error":{"code":"unauthorized","message":"no"}}"#)
        let url = try await server.start()
        defer { server.stop() }
        let client = try HTTPClient(endpoint: url, token: Self.token, instance: "instance_remote", serverID: "vps", remote: true)
        let reported = Mutex<[TransportFailures.Failure]>([])
        client.failures.connect { failure in reported.withLock { $0.append(failure) } }
        _ = try? await client.sessions()
        #expect(reported.withLock { $0 } == [.refused])
    }

    @Test func onlyLoopbackEndpointsAreAcceptedUnlessRemote() {
        let remote = URL(string: "https://kit.example.com")!
        #expect(throws: ClientError.self) { try HTTPClient(endpoint: remote, token: "t", instance: "i", serverID: "vps") }
        #expect((try? HTTPClient(endpoint: remote, token: "t", instance: "i", serverID: "vps", remote: true)) != nil)
        #expect(throws: ClientError.self) {
            try HTTPClient(endpoint: URL(string: "https://kit.example.com/path")!, token: "t", instance: "i", serverID: "vps", remote: true)
        }
    }
}

/// Expects `body` to fail with `expected`.
func expectClientError(_ expected: ClientError, _ body: () async throws -> Void) async {
    do {
        try await body()
        Issue.record("Expected \(expected)")
    } catch {
        #expect(error.localizedDescription == expected.localizedDescription)
    }
}

@MainActor struct ServedServerConnectionStatusTests {
    @Test func aRefusedTokenAsksForPairingAndKeepsChecking() async throws {
        let client = ServerClient(serverID: "vps", connector: .ssh("unused"))
        let server = ServerConnection(client: client, schedule: .init(firstRetry: .milliseconds(10), maximumRetry: .milliseconds(10),
                                                                       check: .milliseconds(10))) {
            throw ClientError.needsPairing(.expired)
        }
        server.start()
        defer { server.stop() }
        try await eventually { server.status == .needsPairing(.expired) }
    }
}

struct PairingLinkTests {
    private let code = String(repeating: "a", count: 26)

    @Test func linksNameAServerAndACode() {
        #expect(PairingLink("https://Kit.Example.dev/pair#\(code)") == PairingLink(server: "https://kit.example.dev", code: code))
        #expect(PairingLink(" http://100.64.0.7:4517/pair#\(code) ")?.server == "http://100.64.0.7:4517")
        for invalid in ["https://kit.example.dev/pair", "https://kit.example.dev/pair#short", "ftp://kit.example.dev/pair#\(code)",
                        "https://kit.example.dev/other#\(code)", "https://me@kit.example.dev/pair#\(code)",
                        "https://kit.example.dev/pair?x=1#\(code)", "https://kit.example.dev/pair#\(code.uppercased())"] {
            #expect(PairingLink(invalid) == nil)
        }
        #expect(PairingLink.link(server: "https://kit.example.dev", code: code) == "https://kit.example.dev/pair#\(code)")
    }

    @Test func locationsAreReadLikeKitServersAdd() {
        #expect(ServerLocation("https://kit.example.dev/pair#\(code)", userHome: "/Users/me") == .link(PairingLink(server: "https://kit.example.dev", code: code)))
        #expect(ServerLocation("https://kit.example.dev/pair#nope", userHome: "/Users/me") == nil)
        #expect(ServerLocation("https://kit.example.dev", userHome: "/Users/me") == nil)
        #expect(ServerLocation("me@gpu-01.lab", userHome: "/Users/me") == .ssh("me@gpu-01.lab"))
        #expect(ServerLocation("ssh://me@gpu-01.lab:2222", userHome: "/Users/me") == .ssh("ssh://me@gpu-01.lab:2222"))
        #expect(ServerLocation("devbox", userHome: "/Users/me") == .ssh("devbox"))
        #expect(ServerLocation("~/work/kit-home/", userHome: "/Users/me") == .home("/Users/me/work/kit-home"))
        #expect(ServerLocation("/srv/kit", userHome: "/Users/me") == .home("/srv/kit"))
        #expect(ServerLocation("-oProxyCommand=x", userHome: "/Users/me") == nil)
        #expect(ServerLocation("", userHome: "/Users/me") == nil)
    }

    @Test func deviceNamesFitTheServersLimit() {
        #expect(DeviceName.clean("Akonwi’s MacBook Pro") == "Akonwi’s MacBook Pro")
        #expect(DeviceName.clean("  mini.local \n") == "mini")
        #expect(DeviceName.clean(String(repeating: "é", count: 40)).utf8.count <= 64)
        #expect(DeviceName.clean("") == "Kit for Mac")
    }

    @Test func terminalTokensAreReadOnlyWhenPrivate() throws {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        let file = home.appendingPathComponent("server-tokens.json")
        let token = "kit_device_" + String(repeating: "B", count: 43)
        try Data(#"{"tokens":{"vps":{"url":"https://kit.example.dev","token":"\#(token)"}}}"#.utf8).write(to: file)
        let credential = DeviceCredential(home: home.path, name: "vps")
        try FileManager.default.setAttributes([.posixPermissions: 0o644], ofItemAtPath: file.path)
        #expect(TerminalDeviceTokens.token(for: credential, url: "https://kit.example.dev") == nil)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
        #expect(TerminalDeviceTokens.token(for: credential, url: "https://kit.example.dev") == token)
        #expect(TerminalDeviceTokens.token(for: credential, url: "https://other.example.dev") == nil)
        #expect(TerminalDeviceTokens.token(for: DeviceCredential(home: home.path, name: "other"), url: "https://kit.example.dev") == nil)
        // The app's own token comes first.
        let own = "kit_device_" + String(repeating: "C", count: 43)
        let tokens = DeviceTokens(keychain: MemoryDeviceTokens([credential: ("https://kit.example.dev", own)]))
        #expect(tokens.token(for: credential, url: "https://kit.example.dev") == own)
        #expect(DeviceTokens(keychain: MemoryDeviceTokens()).token(for: credential, url: "https://kit.example.dev") == token)
    }
}

@MainActor struct ServerDirectoryChangeTests {
    private let code = String(repeating: "a", count: 26)
    private let token = "kit_device_" + String(repeating: "D", count: 43)

    private func directory(tokens: MemoryDeviceTokens = MemoryDeviceTokens()) throws -> (ServerDirectory, URL) {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        let list = home.appendingPathComponent("servers.json")
        let servers = ServerDirectory(home: home, tokens: tokens, saved: { (try? SavedServerList.read(list)) ?? [] }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector)) { throw ClientError.noDaemon }
        }
        return (servers, home)
    }

    @Test func addingALinkSavesTheServerAndItsToken() async throws {
        let tokens = MemoryDeviceTokens()
        let (servers, home) = try directory(tokens: tokens)
        var redeemed: (URL, String, String)?
        try await servers.add(name: "vps", link: PairingLink(server: "https://kit.example.dev", code: code), deviceName: "Mac") { url, code, name in
            redeemed = (url, code, name)
            return token
        }
        #expect(redeemed?.0 == URL(string: "https://kit.example.dev"))
        #expect(redeemed?.1 == code)
        #expect(redeemed?.2 == "Mac")
        #expect(servers.savedServers == [SavedServer(name: "vps", kind: .url, address: "https://kit.example.dev")])
        #expect(servers.connections.map(\.name) == ["local", "vps"])
        #expect(tokens.token(for: DeviceCredential(home: home.path, name: "vps"), url: "https://kit.example.dev") == token)
    }

    @Test func aFailedPairingReleasesTheName() async throws {
        let (servers, _) = try directory()
        await #expect(throws: PairingError.codeRefused) {
            try await servers.add(name: "vps", link: PairingLink(server: "https://kit.example.dev", code: code), deviceName: "Mac") { _, _, _ in
                throw PairingError.codeRefused
            }
        }
        #expect(servers.savedServers.isEmpty)
        #expect(servers.connections.map(\.name) == ["local"])
    }

    @Test func aServerIsSavedOnceAndPairedAgainOnlyWithItsOwnLink() async throws {
        let tokens = MemoryDeviceTokens()
        let (servers, home) = try directory(tokens: tokens)
        let link = PairingLink(server: "https://kit.example.dev", code: code)
        try await servers.add(name: "vps", link: link, deviceName: "Mac") { _, _, _ in token }
        await #expect(throws: PairingError.alreadySaved(name: "vps", url: "https://kit.example.dev")) {
            try await servers.add(name: "other", link: link, deviceName: "Mac") { _, _, _ in token }
        }
        await #expect(throws: PairingError.otherServer(expected: "https://kit.example.dev", link: "https://elsewhere.dev")) {
            try await servers.pairAgain("vps", link: PairingLink(server: "https://elsewhere.dev", code: code), deviceName: "Mac") { _, _, _ in token }
        }
        let renewed = "kit_device_" + String(repeating: "E", count: 43)
        try await servers.pairAgain("vps", link: link, deviceName: "Mac") { _, _, _ in renewed }
        #expect(tokens.token(for: DeviceCredential(home: home.path, name: "vps"), url: "https://kit.example.dev") == renewed)
    }

    @Test func renamingMovesTheTokenAndTellsWindows() async throws {
        let tokens = MemoryDeviceTokens()
        let (servers, home) = try directory(tokens: tokens)
        try await servers.add(name: "vps", link: PairingLink(server: "https://kit.example.dev", code: code), deviceName: "Mac") { _, _, _ in token }
        var renamed: (String, String)?
        servers.serverRenamed = { renamed = ($0, $1) }
        let old = try #require(servers.connection(named: "vps"))
        try servers.rename("vps", to: "cloud")
        // Clients still held for the old name reach the server with the moved token.
        #expect(old.client.connector == .url("https://kit.example.dev", credential: DeviceCredential(home: home.path, name: "cloud")))
        #expect(renamed?.0 == "vps")
        #expect(renamed?.1 == "cloud")
        #expect(servers.connections.map(\.name) == ["local", "cloud"])
        #expect(tokens.token(for: DeviceCredential(home: home.path, name: "cloud"), url: "https://kit.example.dev") == token)
        #expect(tokens.token(for: DeviceCredential(home: home.path, name: "vps"), url: "https://kit.example.dev") == nil)
    }

    @Test func renamingAndRemovingCarryTheKitCommandsToken() async throws {
        let (servers, home) = try directory()
        try await servers.add(name: "vps", link: PairingLink(server: "https://kit.example.dev", code: code), deviceName: "Mac") { _, _, _ in token }
        let file = home.appendingPathComponent("server-tokens.json")
        let other = "kit_device_" + String(repeating: "F", count: 43)
        try Data(#"""
        {"tokens":{"vps":{"url":"https://kit.example.dev","token":"\#(token)"},"lab":{"url":"https://lab.example.dev","token":"\#(other)"}},"note":"kept"}
        """#.utf8).write(to: file)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
        func document() throws -> NSDictionary {
            try #require(JSONSerialization.jsonObject(with: Data(contentsOf: file)) as? NSDictionary)
        }

        try servers.rename("vps", to: "cloud")
        #expect(try document() == [
            "tokens": ["cloud": ["url": "https://kit.example.dev", "token": token],
                       "lab": ["url": "https://lab.example.dev", "token": other]],
            "note": "kept",
        ] as NSDictionary)
        #expect(try FileManager.default.attributesOfItem(atPath: file.path)[.posixPermissions] as? Int == 0o600)

        try servers.remove("cloud")
        #expect(try document() == [
            "tokens": ["lab": ["url": "https://lab.example.dev", "token": other]],
            "note": "kept",
        ] as NSDictionary)
    }

    @Test func aServerRemovedWhilePairingKeepsNoToken() async throws {
        let tokens = MemoryDeviceTokens()
        let (servers, home) = try directory(tokens: tokens)
        await #expect(throws: SavedServerError.notFound("vps")) {
            try await servers.add(name: "vps", link: PairingLink(server: "https://kit.example.dev", code: code), deviceName: "Mac") { _, _, _ in
                try SavedServerList.remove("vps", from: servers.list)
                return token
            }
        }
        #expect(tokens.token(for: DeviceCredential(home: home.path, name: "vps"), url: "https://kit.example.dev") == nil)
        #expect(servers.savedServers.isEmpty)
    }

    @Test func aServerPairedAgainMeanwhileKeepsItsToken() async throws {
        let tokens = MemoryDeviceTokens()
        let (servers, home) = try directory(tokens: tokens)
        let credential = DeviceCredential(home: home.path, name: "vps")
        let other = "kit_device_" + String(repeating: "G", count: 43)
        await #expect(throws: SavedServerError.notFound("vps")) {
            try await servers.add(name: "vps", link: PairingLink(server: "https://kit.example.dev", code: code), deviceName: "Mac") { _, _, _ in
                // Meanwhile the name is removed and saved again for another server.
                try SavedServerList.remove("vps", from: servers.list)
                try SavedServerList.add(SavedServer(name: "vps", kind: .url, address: "https://other.example.dev"), to: servers.list)
                try tokens.save(other, for: credential, url: "https://other.example.dev")
                return token
            }
        }
        #expect(tokens.token(for: credential, url: "https://other.example.dev") == other)
    }

    @Test func removingForgetsTheTokenAndTellsWindows() async throws {
        let tokens = MemoryDeviceTokens()
        let (servers, home) = try directory(tokens: tokens)
        try await servers.add(name: "vps", link: PairingLink(server: "https://kit.example.dev", code: code), deviceName: "Mac") { _, _, _ in token }
        var removed: String?
        servers.serverRemoved = { removed = $0 }
        try servers.remove("vps")
        #expect(removed == "vps")
        #expect(servers.connections.map(\.name) == ["local"])
        #expect(tokens.token(for: DeviceCredential(home: home.path, name: "vps"), url: "https://kit.example.dev") == nil)
    }

    @Test func homesMustExistAndNamesAreSuggestedFromLocations() throws {
        let (servers, home) = try directory()
        #expect(throws: ServerLocationError.noHome("/nowhere/kit")) {
            try servers.add(SavedServer(name: "gone", kind: .home, address: "/nowhere/kit"))
        }
        try servers.add(SavedServer(name: "gpu-01", kind: .ssh, address: "me@gpu-01.lab"))
        #expect(servers.suggestedName(for: .ssh("me@gpu-01.lab")) == "gpu-01-2")
        #expect(servers.suggestedName(for: .ssh("ssh://me@devbox.local:2222")) == "devbox")
        #expect(servers.suggestedName(for: .link(PairingLink(server: "https://staging.example.dev", code: code))) == "staging")
        #expect(servers.suggestedName(for: .link(PairingLink(server: "http://100.64.0.7:4517", code: code))) == "100.64.0.7")
        #expect(servers.suggestedName(for: .home("/Users/me/work/Kit Home")) == "kit-home")
        #expect(servers.suggestedName(for: .home("/Users/devbox/.kit")) == "devbox")
        #expect(servers.suggestedName(for: .home("/srv/local")) == "local-2")
        _ = home
    }
}

struct ServerPairingPresentationTests {
    @Test func aTabAsksForANewLinkFromTheServer() {
        #expect(ServerPairingView.title(name: "staging", need: .expired) == "Pair with staging again")
        #expect(ServerPairingView.message(name: "staging", need: .refused)
                == "This Mac’s pairing with staging expired or was revoked. Get a new link by running kit server pair on staging, or with Pair a Device on another paired device, and paste it here.")
        #expect(ServerPairingView.title(name: "staging", need: .missing) == "Pair with staging")
        #expect(ServerPairingView.message(name: "staging", need: .missing)
                == "This Mac isn’t paired with staging. Get a link by running kit server pair on staging, or with Pair a Device on another paired device, and paste it here.")
        #expect(ServerPairingView.placeholder(.url("https://staging.example.dev", credential: DeviceCredential(home: "/h", name: "staging")))
                == "https://staging.example.dev/pair#…")
    }

    @Test func servedAndSSHServersNameTheirAddress() {
        let credential = DeviceCredential(home: "/h", name: "vps")
        #expect(ServerStatePresentation.source(name: "vps", connector: .url("https://kit.example.dev", credential: credential)) == "url · https://kit.example.dev")
        #expect(ServerStatePresentation.source(name: "devbox", connector: .ssh("me@devbox")) == "ssh · me@devbox")
        #expect(ServerStatePresentation.restartCommand(connector: .url("https://kit.example.dev", credential: credential)) == "kit server restart")
    }
}

struct ServersSettingsPresentationTests {
    private let credential = DeviceCredential(home: "/Users/me/.kit", name: "staging")
    private let identity = ServerIdentity(id: "server_" + String(repeating: "a", count: 32), instance: "i", kitVersion: "0.48.0",
                                          protocolVersion: kitWireVersion, machine: String(repeating: "b", count: 64), home: "/Users/me")

    @Test func rowsSayWhereEachServerIsAndHowItIs() {
        let user = FileManager.default.homeDirectoryForCurrentUser.path
        let local = ServerRowPresentation(name: "local", connector: .home(path: user + "/.kit"), status: .connected(identity))
        #expect(local == ServerRowPresentation(name: "local", detail: "This Mac · ~/.kit · Kit 0.48.0", status: "Running", tone: .success, action: .menu))
        let devbox = ServerRowPresentation(name: "devbox", connector: .ssh("me@devbox"), status: .unreachable(reason: "no", retry: nil))
        #expect(devbox == ServerRowPresentation(name: "devbox", detail: "SSH · me@devbox", status: "Can’t connect", tone: .hollow, action: .menu))
        let staging = ServerRowPresentation(name: "staging", connector: .url("https://staging.example.dev", credential: credential),
                                            status: .needsPairing(.expired))
        #expect(staging == ServerRowPresentation(name: "staging", detail: "https://staging.example.dev", status: "Pairing expired",
                                                 tone: .warning, action: .pairAgain))
        let vps = ServerRowPresentation(name: "vps", connector: .url("https://kit.example.dev", credential: credential), status: .connected(identity))
        #expect(vps.status == "Connected")
    }

    @Test func onlyThisMacsServerOffersANewerKit() throws {
        let user = FileManager.default.homeDirectoryForCurrentUser.path
        let newest = try #require(ReleaseVersion("0.49.0"))
        let local = ServerRowPresentation(name: "local", connector: .home(path: user + "/.kit"), status: .connected(identity), newest: newest)
        #expect(local == ServerRowPresentation(name: "local", detail: "This Mac · ~/.kit · Kit 0.48.0", status: "Running", tone: .success,
                                               action: .update, available: "0.49.0"))
        let current = ServerRowPresentation(name: "local", connector: .home(path: user + "/.kit"), status: .connected(identity),
                                            newest: ReleaseVersion("0.48.0"))
        #expect(current.action == .menu)
        #expect(current.available == nil)
        let vps = ServerRowPresentation(name: "vps", connector: .url("https://kit.example.dev", credential: credential),
                                        status: .connected(identity), newest: newest)
        #expect(vps == ServerRowPresentation(name: "vps", detail: "https://kit.example.dev · Kit 0.48.0", status: "Connected", tone: .success,
                                             action: .menu))
    }

    @Test func addServerSaysWhatItFound() {
        let code = String(repeating: "a", count: 26)
        #expect(AddServerHint("me@gpu-01.lab", location: .ssh("me@gpu-01.lab"))?.kind == .ssh)
        let link = AddServerHint("x", location: .link(PairingLink(server: "http://100.64.0.7:4517", code: code)))
        #expect(link?.text == "Pairing link for 100.64.0.7. Kit keeps this Mac’s device token in the Keychain.")
        #expect(link?.warning == "Plain HTTP: the code and token cross the network unencrypted unless it’s private, like a tailnet.")
        let loopback = AddServerHint("x", location: .link(PairingLink(server: "http://127.0.0.1:58925", code: code)))
        #expect(loopback?.text == "Pairing link for 127.0.0.1. Kit keeps this Mac’s device token in the Keychain.")
        #expect(loopback?.warning == nil)
        #expect(AddServerHint("https://kit.example.dev", location: nil)?.text
                == "A server URL needs pairing. Run kit server pair on that server and paste the link it prints.")
        #expect(AddServerHint("  ", location: nil) == nil)
    }

    @Test func pairingOffersAddressesOtherDevicesCanReach() {
        let home = ServerConnector.home(path: "/Users/me/.kit")
        #expect(PairingAddresses(connector: home, reported: ["https://mac.tail1a2b.ts.net", "http://192.168.1.20:4517", "http://127.0.0.1:4517"])
            .addresses.map(\.url) == ["https://mac.tail1a2b.ts.net", "http://192.168.1.20:4517"])
        #expect(PairingAddresses(connector: home, reported: ["http://127.0.0.1:4517"]).addresses.isEmpty)
        let served = ServerConnector.url("https://kit.example.dev", credential: credential)
        #expect(PairingAddresses(connector: served, reported: ["https://kit.example.dev", "http://[::1]:4517"]).addresses.map(\.url)
                == ["https://kit.example.dev"])
        #expect(PairingAddresses.Address(url: "http://192.168.1.20:4517").insecure)
        #expect(PairDeviceDialog.exposeCommand(connector: .home(DevHomeResolver.defaultHome)) == "kit server expose --tailscale")
        #expect(PairDeviceDialog.exposeCommand(connector: .home(path: "/work/kit home"))
                == "KIT_HOME='/work/kit home' kit server expose --tailscale")
    }

    @Test func removingSaysWhichRunningCommandsEnd() {
        #expect(RemoveServerDialog.message(name: "vps", token: false, tabs: 1, running: 2)
            == "Kit and the kit command forget vps. Its open tab closes. 2 commands still running in its terminals end. The server and its sessions keep running.")
        #expect(RemoveServerDialog.message(name: "vps", token: false, tabs: 1, running: 1)
            == "Kit and the kit command forget vps. Its open tab closes. A command still running in its terminals ends. The server and its sessions keep running.")
    }

    @Test func removingSaysWhatGoes() {
        #expect(RemoveServerDialog.message(name: "vps", token: true, tabs: 2)
                == "Kit and the kit command forget vps. Its device token is removed from the Keychain. Its 2 open tabs close. The server and its sessions keep running.")
        #expect(RemoveServerDialog.message(name: "devbox", token: false, tabs: 0)
                == "Kit and the kit command forget devbox. The server and its sessions keep running.")
    }
}

private func launcherIdentity(_ name: String) -> ServerIdentity {
    ServerIdentity(id: name, instance: "a", kitVersion: "0.42.0", protocolVersion: kitWireVersion, machine: "m", home: "/h")
}

struct LauncherServerOptionTests {
    private let identity = launcherIdentity("srv")

    @Test func availableServersShowWhereTheyAre() {
        #expect(LauncherServerOption(name: "local", connector: .home(path: "/Users/kit/.kit"), status: .connected(identity))
            == LauncherServerOption(name: "local", detail: "This Mac", address: false, available: true))
        #expect(LauncherServerOption(name: "devbox", connector: .ssh("me@devbox"), status: .connecting)
            == LauncherServerOption(name: "devbox", detail: "me@devbox", address: true, available: true))
        let credential = DeviceCredential(home: "/Users/kit/.kit", name: "vps")
        #expect(LauncherServerOption(name: "vps", connector: .url("https://kit.example.dev", credential: credential),
                                     status: .reconnecting(identity, retry: .distantFuture))
            == LauncherServerOption(name: "vps", detail: "kit.example.dev", address: true, available: true))
        #expect(LauncherServerOption(name: "lan", connector: .url("http://192.168.1.20:4517", credential: credential),
                                     status: .connected(identity)).detail == "192.168.1.20:4517")
    }

    @Test func unavailableServersSayWhatTheyNeedAndCantBeChosen() {
        let credential = DeviceCredential(home: "/Users/kit/.kit", name: "staging")
        #expect(LauncherServerOption(name: "staging", connector: .url("https://staging.example.dev", credential: credential),
                                     status: .needsPairing(.expired))
            == LauncherServerOption(name: "staging", detail: "Pair again", address: false, available: false, needsPairing: true))
        #expect(LauncherServerOption(name: "old-mini", connector: .ssh("me@old-mini"),
                                     status: .unreachable(reason: "ssh failed", retry: nil))
            == LauncherServerOption(name: "old-mini", detail: "Can’t connect", address: false, available: false))
    }
}

@MainActor struct LauncherServerTests {
    private func directory(_ servers: [SavedServer], defaults: UserDefaults? = nil,
                           check: @escaping @Sendable (String) throws -> Void = { _ in }) throws -> ServerDirectory {
        let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        for server in servers { try SavedServerList.add(server, to: home.appendingPathComponent("servers.json")) }
        let list = home.appendingPathComponent("servers.json")
        return ServerDirectory(home: home, defaults: defaults, saved: { (try? SavedServerList.read(list)) ?? [] }) { name, connector in
            ServerConnection(client: ServerClient(serverID: name, connector: connector),
                             schedule: .init(firstRetry: .seconds(60), maximumRetry: .seconds(60), check: .seconds(60))) {
                try check(name)
                return launcherIdentity(name)
            }
        }
    }

    @Test func choosingAServerPointsTheLauncherAndLaterLaunchers() throws {
        let servers = try directory([SavedServer(name: "devbox", kind: .ssh, address: "me@devbox")])
        let model = SessionWindowModel(id: UUID(), servers: servers)
        let launcher = model.tabs[0]
        model.point(launcher, at: "devbox")
        #expect(launcher.identity == SessionIdentity(server: "devbox", session: ""))
        #expect(launcher.serverLabel == nil)
        #expect(servers.launcherServer == "devbox")
        #expect(SessionWindowModel(id: UUID(), servers: servers).tabs[0].identity == SessionIdentity(server: "devbox", session: ""))
    }

    @Test func aLauncherOpensOnThisMacWhenTheLastServerCantListSessions() async throws {
        let servers = try directory([SavedServer(name: "devbox", kind: .ssh, address: "me@devbox")]) { name in
            if name == "devbox" { throw ClientError.needsPairing(.refused) }
        }
        servers.launcherServer = "devbox"
        #expect(servers.launcherDefault == "devbox")
        servers.start()
        defer { for connection in servers.connections { connection.stop() } }
        try await eventually { servers.connection(named: "devbox")?.status == .needsPairing(.refused) }
        #expect(servers.launcherDefault == "local")
        #expect(servers.launcherOptions.map(\.available) == [true, false])
    }

    @Test func theLauncherServerFollowsRenamesAndRemovals() throws {
        let defaults = try #require(UserDefaults(suiteName: "launcher-\(UUID().uuidString)"))
        let servers = try directory([SavedServer(name: "devbox", kind: .ssh, address: "me@devbox")], defaults: defaults)
        servers.launcherServer = "devbox"
        try servers.rename("devbox", to: "gpu")
        #expect(servers.launcherServer == "gpu")
        #expect(defaults.string(forKey: ServerDirectory.launcherServerKey) == "gpu")
        let model = SessionWindowModel(id: UUID(), servers: servers)
        servers.serverRemoved = { model.serverRemoved($0) }
        try servers.remove("gpu")
        #expect(servers.launcherServer == "local")
        #expect(model.tabs[0].identity == SessionIdentity(server: "local", session: ""))
    }
}

struct NewSessionPlaceTests {
    @Test func typedFoldersOnAnotherMachineStartFromItsHome() {
        #expect(NewSessionPlace.folder("~", home: "/home/me") == "/home/me")
        #expect(NewSessionPlace.folder(" ~/src/kit ", home: "/home/me") == "/home/me/src/kit")
        #expect(NewSessionPlace.folder("src/kit", home: "/home/me/") == "/home/me/src/kit")
        #expect(NewSessionPlace.folder("/srv/app", home: "/home/me") == "/srv/app")
        #expect(NewSessionPlace.folder("  ", home: "/home/me") == nil)
    }
}
