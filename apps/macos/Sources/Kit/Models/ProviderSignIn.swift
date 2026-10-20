import AppKit
import Foundation

/// One way to sign in to a provider, as the provider list shows it. A
/// provider with several methods gets a row for each, so Anthropic's browser
/// sign-in reads as Claude.
struct ProviderOption: Hashable, Identifiable, Sendable {
    let provider: String
    let providerName: String
    let method: ProviderLoginMethod

    var id: String { provider + "/" + method.rawValue }

    var title: String { provider == "anthropic" && method == .browser ? "Claude" : providerName }

    var detail: String {
        switch method {
        case .device: provider == "openai-codex" ? "ChatGPT plan · device code" : "Device code"
        case .browser: provider == "anthropic" ? "Pro or Max plan · browser" : "Browser"
        case .apiKey: "API key"
        }
    }

    var icon: Icon {
        switch method {
        case .device: .hash
        case .browser: .arrowSquareOut
        case .apiKey: .key
        }
    }

    /// Where to create an API key, for the providers whose page is known.
    var keyPage: (name: String, url: URL)? {
        switch provider {
        case "anthropic": ("Anthropic Console", URL(string: "https://console.anthropic.com/settings/keys")!)
        case "openai": ("OpenAI dashboard", URL(string: "https://platform.openai.com/api-keys")!)
        default: nil
        }
    }

    /// Every provider's methods in the server's order, with browser sign-ins
    /// last.
    static func options(_ providers: [ProviderInfo]) -> [ProviderOption] {
        let all = providers.flatMap { provider in
            provider.methods.map { ProviderOption(provider: provider.id, providerName: provider.name, method: $0) }
        }
        return all.filter { $0.method != .browser } + all.filter { $0.method == .browser }
    }
}

/// Signs the server in to model providers: the provider list, the chosen
/// method's step, and the connected step that picks a default model.
@MainActor @Observable
final class ProviderSignIn {
    enum Step: Equatable {
        case list
        /// The methods of one provider, when Settings signs it in.
        case methods(String)
        case signingIn(ProviderOption)
        case connected(ProviderOption)
    }

    let client: any ProviderClient
    private(set) var providers: [ProviderInfo] = []
    private(set) var loaded = false
    /// Why the provider list couldn't load.
    private(set) var loadError: String?
    private(set) var step = Step.list
    private(set) var login: ProviderLogin?
    private(set) var pending = false
    var error: String?
    /// The models the server offers once connected, and the default chosen among them.
    private(set) var models: [WireModelCapability] = []
    var defaultModel = ""
    /// The server's default model, or nil when it couldn't be read.
    private var savedDefault: String?
    /// The default suggested on connecting.
    private var suggestedDefault = ""

    @ObservationIgnored var open: @MainActor (URL) -> Void = { NSWorkspace.shared.open($0) }
    /// Read and save the server's default model.
    @ObservationIgnored var loadDefault: @Sendable () async throws -> String
    @ObservationIgnored var saveDefault: @Sendable (String) async throws -> Void
    @ObservationIgnored private var polling: Task<Void, Never>?
    /// Listens for a browser login's redirect while one is pending.
    @ObservationIgnored private var receiver: LoopbackRedirectReceiver?

    /// How long one request waits for a pending sign-in to finish.
    static let wait = 20

    init(client: any ProviderClient) {
        self.client = client
        let settings = client as? any ServerSettingsClient
        loadDefault = {
            guard let settings else { throw MutationNotSent(reason: "This server’s settings can’t be read.") }
            return try await settings.serverSettings().defaultModel
        }
        saveDefault = {
            guard let settings else { throw MutationNotSent(reason: "This server’s settings can’t be changed.") }
            _ = try await settings.updateServerSettings(.defaultModel($0))
        }
    }

    var options: [ProviderOption] { ProviderOption.options(providers) }
    var anyConnected: Bool { providers.contains(where: \.configured) }
    /// The providers the server has a credential for.
    var connected: Set<String> { Set(providers.filter(\.configured).map(\.id)) }

    func load() async {
        do {
            providers = try await client.providers()
            loadError = nil
        } catch {
            loadError = error.localizedDescription
        }
        loaded = true
    }

    /// Settings' Sign in: the provider's only method, or a choice of them.
    func signIn(_ provider: ProviderInfo) {
        let options = options.filter { $0.provider == provider.id }
        if options.count == 1 { choose(options[0]) } else { step = .methods(provider.id) }
    }

    func choose(_ option: ProviderOption) {
        cancelPolling()
        error = nil
        login = nil
        step = .signingIn(option)
        if option.method != .apiKey { start(option) }
    }

    /// Starts the sign-in again after it failed or expired.
    func retry() {
        guard case let .signingIn(option) = step else { return }
        choose(option)
    }

    /// Returns to the list, cancelling a sign-in that is still waiting.
    func back() {
        let waiting = login.flatMap { $0.state == .pending ? $0.id : nil }
        cancelPolling()
        login = nil
        error = nil
        pending = false
        step = .list
        if let waiting { Task { [client] in try? await client.cancelLogin(waiting) } }
    }

    func reopenBrowser() {
        if let url = login?.authorizationURL { open(url) }
    }

    /// Receives the pending browser login's redirect on this Mac and submits
    /// it, as the terminal client does, until a redirect arrives, the login
    /// ends, or the caller is cancelled. The browser step runs it while it
    /// shows. When the redirect can't be received here, such as when another
    /// client already listens on its port, it returns and the user pastes
    /// the code instead. A later call replaces an earlier one.
    func receiveRedirect() async {
        stopReceiving()
        guard let login, login.state == .pending, login.method == .browser,
              let authorization = login.authorizationURL, let address = login.redirectURI,
              let redirect = LoopbackRedirect(redirect: address, authorization: authorization),
              let receiver = try? await LoopbackRedirectReceiver.start(redirect) else { return }
        defer {
            receiver.stop()
            if self.receiver === receiver { self.receiver = nil }
        }
        guard !Task.isCancelled, isPending(login) else { return }
        stopReceiving()
        self.receiver = receiver
        guard let url = await receiver.received(),
              isPending(login) else { return }
        await submit(authorization: url)
    }

    func submit(authorization: String) async {
        guard let login, login.state == .pending, !pending else { return }
        pending = true; error = nil
        defer { pending = false }
        do { try await client.submitAuthorization(login: login.id, authorization) }
        catch { self.error = error.localizedDescription }
    }

    func save(apiKey: String) async {
        guard case let .signingIn(option) = step, option.method == .apiKey, !pending else { return }
        pending = true; error = nil
        defer { pending = false }
        do {
            try await client.setAPIKey(provider: option.provider, apiKey)
            await connected(option)
        } catch { self.error = error.localizedDescription }
    }

    func signOut(_ provider: ProviderInfo) async {
        guard !pending else { return }
        pending = true; error = nil
        defer { pending = false }
        do { try await client.signOut(provider: provider.id) }
        catch { self.error = error.localizedDescription }
        await load()
    }

    /// Saves the default model if it changed, then returns to the list. When
    /// the server's default couldn't be read, only a model the user chose
    /// replaces it.
    func finish() async {
        let changed = savedDefault.map { defaultModel != $0 } ?? (defaultModel != suggestedDefault)
        if !defaultModel.isEmpty, changed {
            do { try await saveDefault(defaultModel) }
            catch { self.error = "Couldn’t save the default model. " + error.localizedDescription; return }
        }
        step = .list
    }

    // MARK: Device and browser

    private func start(_ option: ProviderOption) {
        pending = true
        polling = Task { [weak self, client] in
            do {
                var login = try await client.startLogin(provider: option.provider, method: option.method)
                guard let self, !Task.isCancelled else { return }
                self.login = login
                self.pending = false
                if let url = login.authorizationURL { self.open(url) }
                while login.state == .pending {
                    login = try await client.login(login.id, wait: Self.wait)
                    guard !Task.isCancelled else { return }
                    self.login = login
                }
                self.stopReceiving()
                switch login.state {
                case .succeeded: await self.connected(option)
                case .expired: self.error = login.failure ?? "The sign-in expired."
                default: self.error = login.failure ?? "The sign-in didn’t finish."
                }
            } catch {
                guard let self, !Task.isCancelled else { return }
                self.stopReceiving()
                self.pending = false
                self.error = error.localizedDescription
            }
        }
    }

    private func cancelPolling() {
        polling?.cancel()
        polling = nil
        stopReceiving()
    }

    private func stopReceiving() {
        receiver?.stop()
        receiver = nil
    }

    /// Whether `login` is still the pending sign-in.
    private func isPending(_ login: ProviderLogin) -> Bool {
        self.login?.id == login.id && self.login?.state == .pending
    }

    private func connected(_ option: ProviderOption) async {
        await load()
        models = (try? await client.models()) ?? []
        savedDefault = try? await loadDefault()
        // Keep a default that still works; otherwise suggest this provider's first model.
        let own = models.first { $0.provider == option.provider || $0.id.hasPrefix(option.provider + "/") }
        defaultModel = savedDefault.flatMap { saved in models.contains(where: { $0.id == saved }) ? saved : nil } ?? (own ?? models.first)?.id ?? ""
        suggestedDefault = defaultModel
        step = .connected(option)
    }
}
