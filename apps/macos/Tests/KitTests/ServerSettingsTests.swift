import Foundation
import Testing
@testable import Kit

/// Answers settings requests by port and records each update's body.
private final class SettingsResponse: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var bodies: [Int: Data] = [:]
    static let lock = NSLock()

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(request.value(forHTTPHeaderField: "X-Kit-Instance-ID") == "server")
        if let stream = request.httpBodyStream {
            stream.open(); defer { stream.close() }
            var data = Data(), buffer = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable { let read = stream.read(&buffer, maxLength: buffer.count); if read <= 0 { break }; data.append(buffer, count: read) }
            Self.lock.withLock { Self.bodies[url.port ?? 0] = data }
        }
        let settings = #"{"defaultModel":"anthropic/claude-opus-4-6","modelOverrides":[{"model":"anthropic/claude-sonnet-4-6","contextWindow":1000000}],"promptCacheRetention":"long","readClaudeConfigs":false,"newSessionModel":"anthropic/claude-opus-4-6","newSessionThinkingLevel":"medium"}"#
        let (status, body): (Int, String) = switch url.port {
        case 19571, 19572: (200, settings)
        case 19573: (409, #"{"error":{"code":"conflict","message":"the server's settings.json needs fixing; its log says why"}}"#)
        case 19574: (200, #"{"defaultModel":"no-slash","modelOverrides":[],"promptCacheRetention":"short","readClaudeConfigs":true}"#)
        default: (500, "")
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        if !body.isEmpty { client?.urlProtocol(self, didLoad: Data(body.utf8)) }
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct ServerSettingsTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let config = URLSessionConfiguration.ephemeral; config.protocolClasses = [SettingsResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "server", serverID: "test", configuration: config)
    }

    @Test func projectsTheServersSettings() async throws {
        let settings = try await client(19571).serverSettings()
        #expect(settings == ServerSettings(defaultModel: "anthropic/claude-opus-4-6", compactionModel: "",
                                           contextWindows: ["anthropic/claude-sonnet-4-6": 1_000_000],
                                           promptCacheRetention: .long, readClaudeConfigs: false,
                                           newSessionModel: "anthropic/claude-opus-4-6"))
    }

    @Test func anUpdateCarriesOnlyItsChange() async throws {
        _ = try await client(19572).updateServerSettings(.readClaudeConfigs(false))
        let body = try #require(SettingsResponse.lock.withLock { SettingsResponse.bodies[19572] })
        let object = try #require(JSONSerialization.jsonObject(with: body) as? [String: Any])
        #expect(object.keys.sorted() == ["readClaudeConfigs"])
        #expect(object["readClaudeConfigs"] as? Bool == false)
    }

    @Test func surfacesTheServersExplanation() async throws {
        await #expect(throws: ServerSettingsFailed(message: "the server's settings.json needs fixing; its log says why")) {
            try await client(19573).serverSettings()
        }
        await #expect(throws: ServerSettingsFailed(message: "the server's settings.json needs fixing; its log says why")) {
            try await client(19573).updateServerSettings(.promptCacheRetention(.long))
        }
    }

    @Test func rejectsSettingsOutsideTheContract() async throws {
        await #expect(throws: ClientError.self) { try await client(19574).serverSettings() }
    }

    @Test func refusesToSendAnInvalidChange() async throws {
        await #expect(throws: MutationNotSent.self) { try await client(19572).updateServerSettings(.defaultModel("no-slash")) }
        await #expect(throws: MutationNotSent.self) { try await client(19572).updateServerSettings(.contextWindow("p/m", 0)) }
    }
}

/// A server whose settings change as the editor asks, failing when told to.
private actor FakeSettings: ServerSettingsClient {
    var settings = ServerSettings(newSessionModel: "anthropic/sonnet")
    var failure: String?
    private(set) var changes: [ServerSettingsChange] = []

    func serverSettings() async throws -> ServerSettings { settings }
    func updateServerSettings(_ change: ServerSettingsChange) async throws -> ServerSettings {
        if let failure { throw ServerSettingsFailed(message: failure) }
        changes.append(change)
        switch change {
        case .defaultModel(let model): settings.defaultModel = model
        case .compactionModel(let model): settings.compactionModel = model
        case .contextWindow(let model, let tokens): settings.contextWindows[model] = tokens
        case .promptCacheRetention(let retention): settings.promptCacheRetention = retention
        case .readClaudeConfigs(let enabled): settings.readClaudeConfigs = enabled
        }
        return settings
    }
    func models() async throws -> [WireModelCapability] { [] }
    func fail(_ message: String?) { failure = message }
}

@MainActor
struct ServerSettingsEditorTests {
    @Test func aSavedChangeShowsTheServersSettings() async {
        let server = FakeSettings()
        let editor = ServerSettingsEditor(client: server)
        await editor.load()
        #expect(editor.loaded)
        #expect(editor.settings.readClaudeConfigs)
        #expect(await editor.save(.readClaudeConfigs(false)))
        #expect(!editor.settings.readClaudeConfigs)
        #expect(editor.saving == nil)
        #expect(await server.changes == [.readClaudeConfigs(false)])
    }

    @Test func aFailedChangeKeepsTheConfirmedSettingsAndExplainsItsField() async {
        let server = FakeSettings()
        let editor = ServerSettingsEditor(client: server)
        await editor.load()
        await server.fail("the server's settings.json needs fixing; its log says why")
        #expect(!(await editor.save(.promptCacheRetention(.long))))
        #expect(editor.settings.promptCacheRetention == .short)
        #expect(editor.failure(for: .promptCacheRetention) == "the server's settings.json needs fixing; its log says why")
        #expect(editor.failure(for: .defaultModel) == nil)
        await server.fail(nil)
        #expect(await editor.save(.contextWindow("p/m", 2000), as: .newOverride))
        #expect(editor.failure(for: .promptCacheRetention) == nil)
        #expect(editor.settings.contextWindows == ["p/m": 2000])
    }

    @Test func anUnreadableServerExplainsWhy() async {
        struct Unreadable: ServerSettingsClient {
            func serverSettings() async throws -> ServerSettings { throw ServerSettingsFailed(message: "needs fixing") }
            func updateServerSettings(_ change: ServerSettingsChange) async throws -> ServerSettings { throw ServerSettingsFailed(message: "needs fixing") }
            func models() async throws -> [WireModelCapability] { [] }
        }
        let editor = ServerSettingsEditor(client: Unreadable())
        await editor.load()
        #expect(!editor.loaded)
        #expect(editor.loadError == "needs fixing")
    }
}

struct ServerSettingsDefaultsTests {
    @Test func newSessionsPreferTheDefaultThenTheSuggestionThenTheServersChoice() {
        let saved = ServerSettings(defaultModel: "p/preferred", newSessionModel: "p/server")
        #expect(saved.preferredModel(available: ["p/other", "p/preferred"], suggested: "p/other") == "p/preferred")
        #expect(saved.preferredModel(available: ["p/other", "p/suggested"], suggested: "p/suggested") == "p/suggested")
        let automatic = ServerSettings(newSessionModel: "p/server")
        #expect(automatic.preferredModel(available: ["p/first", "p/server"], suggested: nil) == "p/server")
        #expect(ServerSettings().preferredModel(available: ["p/first"], suggested: nil) == "p/first")
    }
}

@MainActor
struct DiffPreferencesTests {
    @Test func preferencesPersistAndDiffsFollowThem() throws {
        let suite = "DiffPreferencesTests.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let preferences = DiffPreferences(defaults: defaults)
        #expect(preferences.layout == .unified)
        #expect(!preferences.wrapLines)
        let diff = DiffState(preferences: preferences)
        diff.layout = .split
        preferences.wrapLines = true
        #expect(diff.wrapLines)
        #expect(defaults.string(forKey: DiffPreferences.layoutKey) == "split")
        let restored = DiffPreferences(defaults: defaults)
        #expect(restored.layout == .split)
        #expect(restored.wrapLines)
    }
}
