import Foundation
import Testing
@testable import Kit

/// Answers provider requests by port, so each test reads one scenario.
private final class ProviderResponse: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        #expect(request.value(forHTTPHeaderField: "X-Kit-Instance-ID") == "server")
        let login = #"{"id":"login_1","provider":"openai-codex","method":"device","state":"pending","expiresAt":"2026-10-16T05:00:00Z","device":{"userCode":"WDJB-MJHT","verificationUri":"https://auth.openai.com/codex/device","expiresAt":"2026-10-16T05:00:00Z"}}"#
        let (status, body): (Int, String) = switch url.port {
        case 19301: (200, #"{"providers":[{"id":"openai-codex","name":"OpenAI Codex","methods":["device"],"configured":false,"source":"store"},{"id":"anthropic","name":"Anthropic","methods":["browser","apiKey"],"configured":true,"source":"environment"}]}"#)
        case 19302: (201, login)
        case 19303: (201, login.replacingOccurrences(of: "https://auth.openai.com", with: "file://"))
        case 19304: (400, #"{"error":{"code":"invalid_request","message":"The API key was rejected."}}"#)
        case 19305: (200, #"{"providers":[{"id":"Bad ID","name":"x","methods":[],"configured":false,"source":"store"}]}"#)
        default: (500, "")
        }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!, cacheStoragePolicy: .notAllowed)
        if !body.isEmpty { client?.urlProtocol(self, didLoad: Data(body.utf8)) }
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

struct ProviderTransportTests {
    private func client(_ port: Int) throws -> HTTPClient {
        let config = URLSessionConfiguration.ephemeral; config.protocolClasses = [ProviderResponse.self]
        return try HTTPClient(endpoint: URL(string: "http://127.0.0.1:\(port)")!, token: "test", instance: "server", serverID: "test", configuration: config)
    }

    @Test func projectsProvidersAndTheirCredentialSource() async throws {
        let providers = try await client(19301).providers()
        #expect(providers == [
            ProviderInfo(id: "openai-codex", name: "OpenAI Codex", methods: [.device], configured: false, fromEnvironment: false),
            ProviderInfo(id: "anthropic", name: "Anthropic", methods: [.browser, .apiKey], configured: true, fromEnvironment: true),
        ])
    }

    @Test func projectsDeviceInstructions() async throws {
        let login = try await client(19302).startLogin(provider: "openai-codex", method: .device)
        #expect(login.id == "login_1")
        #expect(login.state == .pending)
        #expect(login.device?.userCode == "WDJB-MJHT")
        #expect(login.device?.verificationURL == URL(string: "https://auth.openai.com/codex/device"))
    }

    @Test func rejectsPayloadsOutsideTheContract() async throws {
        for port in [19303, 19305] {
            do {
                if port == 19303 { _ = try await client(port).startLogin(provider: "openai-codex", method: .device) }
                else { _ = try await client(port).providers() }
                Issue.record("\(port): expected invalid payload")
            } catch ClientError.invalidPayload {}
        }
    }

    @Test func surfacesTheServersExplanation() async throws {
        await #expect(throws: ProviderRequestFailed(message: "The API key was rejected.")) {
            try await client(19304).setAPIKey(provider: "openai", "sk-wrong")
        }
    }

    @Test func refusesToSendAnEmptyKey() async throws {
        await #expect(throws: MutationNotSent.self) { try await client(19304).setAPIKey(provider: "openai", "  ") }
    }
}

/// A server whose sign-ins finish when the test says so.
private actor FakeProviders: ProviderClient {
    var list: [ProviderInfo]
    var logins: [ProviderLogin] = []
    var cancelled: [String] = []
    var keys: [String: String] = [:]
    var rejectKey = false
    var catalog: [WireModelCapability]

    init(list: [ProviderInfo], models: [WireModelCapability] = []) { self.list = list; catalog = models }

    func providers() async throws -> [ProviderInfo] { list }
    func startLogin(provider: String, method: ProviderLoginMethod) async throws -> ProviderLogin {
        login(provider, method, .pending)
    }
    func login(_ id: String, wait: Int) async throws -> ProviderLogin {
        // Each poll reports the next queued state; pending when none is queued.
        if logins.isEmpty { try await Task.sleep(for: .milliseconds(20)); return login("openai-codex", .device, .pending) }
        let next = logins.removeFirst()
        if next.state == .succeeded { configure(next.provider) }
        return next
    }
    func submitAuthorization(login: String, _ authorization: String) async throws {}
    func cancelLogin(_ id: String) async throws { cancelled.append(id) }
    func setAPIKey(provider: String, _ key: String) async throws {
        if rejectKey { throw ProviderRequestFailed(message: "The API key was rejected.") }
        keys[provider] = key
        configure(provider)
    }
    func signOut(provider: String) async throws {
        list = list.map { $0.id == provider ? ProviderInfo(id: $0.id, name: $0.name, methods: $0.methods, configured: false, fromEnvironment: false) : $0 }
    }
    func models() async throws -> [WireModelCapability] { catalog }

    func queue(_ state: ProviderLogin.State, failure: String? = nil) { logins.append(login("openai-codex", .device, state, failure: failure)) }
    func setRejectKey(_ value: Bool) { rejectKey = value }

    private func configure(_ provider: String) {
        list = list.map { $0.id == provider ? ProviderInfo(id: $0.id, name: $0.name, methods: $0.methods, configured: true, fromEnvironment: false) : $0 }
    }

    private func login(_ provider: String, _ method: ProviderLoginMethod, _ state: ProviderLogin.State, failure: String? = nil) -> ProviderLogin {
        ProviderLogin(id: "login_1", provider: provider, method: method, state: state,
                      device: .init(userCode: "WDJB-MJHT", verificationURL: URL(string: "https://auth.openai.com/codex/device")!, expiresAt: nil),
                      authorizationURL: nil, failure: failure, expiresAt: nil)
    }
}

@MainActor
struct ProviderSignInTests {
    static let providers = [
        ProviderInfo(id: "openai-codex", name: "OpenAI Codex", methods: [.device], configured: false, fromEnvironment: false),
        ProviderInfo(id: "anthropic", name: "Anthropic", methods: [.browser, .apiKey], configured: false, fromEnvironment: false),
        ProviderInfo(id: "openai", name: "OpenAI", methods: [.apiKey], configured: false, fromEnvironment: false),
    ]

    static func model(_ id: String, _ provider: String) -> WireModelCapability {
        WireModelCapability(id: id, name: id, provider: provider, api: provider, contextWindow: 200_000,
                            maxInputTokens: nil, maxOutputTokens: nil, thinkingLevels: nil, inputs: nil, available: true)
    }

    private func signIn(_ server: FakeProviders, saved: String = "") -> (ProviderSignIn, Box) {
        let box = Box()
        let signIn = ProviderSignIn(client: server)
        signIn.open = { box.opened.append($0) }
        signIn.loadDefaults = { .init(defaultModel: saved) }
        signIn.saveDefault = { model in await MainActor.run { box.saved.append(model) } }
        return (signIn, box)
    }

    @MainActor final class Box { var opened: [URL] = []; var saved: [String] = [] }

    private func until(_ condition: @MainActor () -> Bool) async throws {
        for _ in 0..<200 where !condition() { try await Task.sleep(for: .milliseconds(10)) }
        #expect(condition())
    }

    @Test func listsOneRowPerMethodNamingClaudeForAnthropicsBrowserSignIn() async {
        let (signIn, _) = signIn(FakeProviders(list: Self.providers))
        await signIn.load()
        #expect(signIn.options.map { "\($0.title) · \($0.detail)" } == [
            "OpenAI Codex · ChatGPT plan · device code",
            "Anthropic · API key",
            "OpenAI · API key",
            "Claude · Pro or Max plan · browser",
        ])
        #expect(!signIn.anyConnected)
    }

    @Test func deviceSignInWaitsThenConnectsAndSuggestsTheProvidersModel() async throws {
        let server = FakeProviders(list: Self.providers, models: [Self.model("anthropic/opus", "anthropic"), Self.model("openai-codex/gpt-5", "openai-codex")])
        let (signIn, _) = signIn(server)
        await signIn.load()
        let device = try #require(signIn.options.first)
        signIn.choose(device)
        try await until { signIn.login?.device?.userCode == "WDJB-MJHT" }
        #expect(signIn.step == .signingIn(device))
        await server.queue(.succeeded)
        try await until { signIn.step == .connected(device) }
        #expect(signIn.anyConnected)
        #expect(signIn.defaultModel == "openai-codex/gpt-5")
    }

    @Test func keepsASavedDefaultThatStillWorksAndSavesOnlyAChange() async throws {
        let server = FakeProviders(list: Self.providers, models: [Self.model("anthropic/opus", "anthropic"), Self.model("openai/gpt-5", "openai")])
        let (signIn, box) = signIn(server, saved: "anthropic/opus")
        await signIn.load()
        let key = try #require(signIn.options.first { $0.provider == "openai" })
        signIn.choose(key)
        await signIn.save(apiKey: "sk-test")
        #expect(signIn.step == .connected(key))
        #expect(signIn.defaultModel == "anthropic/opus")
        await signIn.finish()
        #expect(box.saved.isEmpty)
        #expect(signIn.step == .list)

        signIn.choose(key)
        await signIn.save(apiKey: "sk-test")
        signIn.defaultModel = "openai/gpt-5"
        await signIn.finish()
        #expect(box.saved == ["openai/gpt-5"])
    }

    @Test func aRejectedKeyStaysOnTheStepWithTheServersMessage() async throws {
        let server = FakeProviders(list: Self.providers)
        await server.setRejectKey(true)
        let (signIn, _) = signIn(server)
        await signIn.load()
        let key = try #require(signIn.options.first { $0.provider == "openai" })
        signIn.choose(key)
        await signIn.save(apiKey: "sk-wrong")
        #expect(signIn.step == .signingIn(key))
        #expect(signIn.error == "The API key was rejected.")
    }

    @Test func anExpiredSignInOffersARetryAndBackCancelsAPendingOne() async throws {
        let server = FakeProviders(list: Self.providers)
        let (signIn, _) = signIn(server)
        await signIn.load()
        let device = try #require(signIn.options.first)
        signIn.choose(device)
        try await until { signIn.login != nil }
        await server.queue(.expired, failure: "The code expired.")
        try await until { signIn.error != nil }
        #expect(signIn.error == "The code expired.")

        signIn.retry()
        #expect(signIn.error == nil)
        try await until { signIn.login?.state == .pending }
        signIn.back()
        #expect(signIn.step == .list)
        for _ in 0..<200 where await server.cancelled.isEmpty { try await Task.sleep(for: .milliseconds(10)) }
        #expect(await server.cancelled == ["login_1"])
    }

    @Test func settingsSignInChoosesAmongAProvidersMethods() async throws {
        let (signIn, _) = signIn(FakeProviders(list: Self.providers))
        await signIn.load()
        signIn.signIn(Self.providers[1])
        #expect(signIn.step == .methods("anthropic"))
        signIn.back()
        signIn.signIn(Self.providers[2])
        #expect(signIn.step == .signingIn(try #require(signIn.options.first { $0.provider == "openai" })))
    }

    @Test func describesEachProviderInSettings() {
        #expect(ProviderSettingsView.detail(Self.providers[1]) == "Claude Pro or Max plan · browser, or API key")
        #expect(ProviderSettingsView.detail(Self.providers[0]) == "ChatGPT plan · device code")
        let env = ProviderInfo(id: "openai", name: "OpenAI", methods: [.apiKey], configured: true, fromEnvironment: true)
        #expect(ProviderSettingsView.detail(env) == "API key · from the server’s environment")
    }
}
