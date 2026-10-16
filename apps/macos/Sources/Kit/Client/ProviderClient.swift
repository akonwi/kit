import Foundation

/// A way to sign the server in to a model provider.
enum ProviderLoginMethod: String, Sendable, CaseIterable {
    /// A code entered on the provider's verification page.
    case device
    /// The provider's authorization page in a browser.
    case browser
    /// An API key saved on the server.
    case apiKey
}

/// A model provider and whether the server has a credential for it.
struct ProviderInfo: Equatable, Sendable, Identifiable {
    let id: String
    let name: String
    let methods: [ProviderLoginMethod]
    let configured: Bool
    /// The credential comes from the server's environment, which the app
    /// can't change.
    let fromEnvironment: Bool
}

/// A device or browser sign-in the server is running.
struct ProviderLogin: Equatable, Sendable {
    enum State: String, Sendable { case pending, succeeded, failed, expired, cancelled, superseded }
    struct Device: Equatable, Sendable {
        let userCode: String
        let verificationURL: URL
        let expiresAt: Date?
    }
    let id: String
    let provider: String
    let method: ProviderLoginMethod
    let state: State
    let device: Device?
    let authorizationURL: URL?
    let failure: String?
    let expiresAt: Date?
}

/// A provider request the server refused, with its explanation.
struct ProviderRequestFailed: LocalizedError, Equatable {
    let message: String
    var errorDescription: String? { message }
}

/// The server's provider sign-in API.
protocol ProviderClient: Sendable {
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

extension LocalClient: ProviderClient {
    func providers() async throws -> [ProviderInfo] { try await HTTPClient.local().providers() }
    func startLogin(provider: String, method: ProviderLoginMethod) async throws -> ProviderLogin {
        try await HTTPClient.local().startLogin(provider: provider, method: method)
    }
    func login(_ id: String, wait: Int) async throws -> ProviderLogin { try await HTTPClient.local().login(id, wait: wait) }
    func submitAuthorization(login: String, _ authorization: String) async throws {
        try await HTTPClient.local().submitAuthorization(login: login, authorization)
    }
    func cancelLogin(_ id: String) async throws { try await HTTPClient.local().cancelLogin(id) }
    func setAPIKey(provider: String, _ key: String) async throws { try await HTTPClient.local().setAPIKey(provider: provider, key) }
    func signOut(provider: String) async throws { try await HTTPClient.local().signOut(provider: provider) }
}
