import Foundation

/// A way to sign the server in to a model provider.
public enum ProviderLoginMethod: String, Sendable, CaseIterable {
    /// A code entered on the provider's verification page.
    case device
    /// The provider's authorization page in a browser.
    case browser
    /// An API key saved on the server.
    case apiKey
}

/// A model provider and whether the server has a credential for it.
public struct ProviderInfo: Equatable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let methods: [ProviderLoginMethod]
    public let configured: Bool
    /// The credential comes from the server's environment, which the app
    /// can't change.
    public let fromEnvironment: Bool

    public init(id: String, name: String, methods: [ProviderLoginMethod], configured: Bool, fromEnvironment: Bool) {
        self.id = id
        self.name = name
        self.methods = methods
        self.configured = configured
        self.fromEnvironment = fromEnvironment
    }
}

/// A device or browser sign-in the server is running.
public struct ProviderLogin: Equatable, Sendable {
    public enum State: String, Sendable { case pending, succeeded, failed, expired, cancelled, superseded }
    public struct Device: Equatable, Sendable {
        public let userCode: String
        public let verificationURL: URL
        public let expiresAt: Date?

    public init(userCode: String, verificationURL: URL, expiresAt: Date?) {
        self.userCode = userCode
        self.verificationURL = verificationURL
        self.expiresAt = expiresAt
    }
    }
    public let id: String
    public let provider: String
    public let method: ProviderLoginMethod
    public let state: State
    public let device: Device?
    public let authorizationURL: URL?
    /// Where the browser returns after approval, when it is a loopback http
    /// address this Mac can listen on.
    public let redirectURI: URL?
    public let failure: String?
    public let expiresAt: Date?

    public init(id: String, provider: String, method: ProviderLoginMethod, state: State, device: Device?, authorizationURL: URL?, redirectURI: URL?, failure: String?, expiresAt: Date?) {
        self.id = id
        self.provider = provider
        self.method = method
        self.state = state
        self.device = device
        self.authorizationURL = authorizationURL
        self.redirectURI = redirectURI
        self.failure = failure
        self.expiresAt = expiresAt
    }
}

/// A provider request the server refused, with its explanation.
public struct ProviderRequestFailed: LocalizedError, Equatable {
    public let message: String
    public var errorDescription: String? { message }

    public init(message: String) {
        self.message = message
    }
}

/// The server's provider sign-in API.
public protocol ProviderClient: Sendable {
    func providers() async throws -> [ProviderInfo]
    func startLogin(provider: String, method: ProviderLoginMethod) async throws -> ProviderLogin
    /// The login, waiting up to `wait` seconds for a pending one to finish.
    func login(_ id: String, wait: Int) async throws -> ProviderLogin
    func submitAuthorization(login: String, _ authorization: String) async throws
    func cancelLogin(_ id: String) async throws
    func setAPIKey(provider: String, _ key: String) async throws
    func signOut(provider: String) async throws
    func models() async throws -> [WireModelCapability]
}

extension ServerClient: ProviderClient {
    public func providers() async throws -> [ProviderInfo] { try await resolve().providers() }
    public func startLogin(provider: String, method: ProviderLoginMethod) async throws -> ProviderLogin {
        try await resolve().startLogin(provider: provider, method: method)
    }
    public func login(_ id: String, wait: Int) async throws -> ProviderLogin { try await resolve().login(id, wait: wait) }
    public func submitAuthorization(login: String, _ authorization: String) async throws {
        try await resolve().submitAuthorization(login: login, authorization)
    }
    public func cancelLogin(_ id: String) async throws { try await resolve().cancelLogin(id) }
    public func setAPIKey(provider: String, _ key: String) async throws { try await resolve().setAPIKey(provider: provider, key) }
    public func signOut(provider: String) async throws { try await resolve().signOut(provider: provider) }
}
