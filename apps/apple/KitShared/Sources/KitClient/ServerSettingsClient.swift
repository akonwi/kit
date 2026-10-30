import Foundation

/// How long providers are asked to keep prompt cache entries.
public enum PromptCacheRetention: String, Sendable, CaseIterable {
    case short, long
}

/// The settings that govern a server's behavior. Each server has its own;
/// client preferences such as themes and diff presentation are not among them.
public struct ServerSettings: Equatable, Sendable {
    /// The model for new sessions that name none; empty lets the server choose.
    public var defaultModel = ""
    /// The model that summarizes context; empty uses each session's own.
    public var compactionModel = ""
    /// Context-window overrides by exact provider/model.
    public var contextWindows: [String: Int] = [:]
    public var promptCacheRetention: PromptCacheRetention = .short
    /// Whether sessions discover Claude Code configuration, such as a
    /// project's `.claude/commands`.
    public var readClaudeConfigs = true
    /// The model a new session that names none would start with now, or
    /// empty when no model is available.
    public var newSessionModel = ""

    /// The model a new session form starts on: the saved default, then the
    /// suggestion, then the server's choice, then the first offered.
    public func preferredModel(available: [String], suggested: String?) -> String {
        [defaultModel, suggested ?? "", newSessionModel].first { !$0.isEmpty && available.contains($0) } ?? available.first ?? ""
    }

    public init(defaultModel: String = "", compactionModel: String = "", contextWindows: [String: Int] = [:], promptCacheRetention: PromptCacheRetention = .short, readClaudeConfigs: Bool = true, newSessionModel: String = "") {
        self.defaultModel = defaultModel
        self.compactionModel = compactionModel
        self.contextWindows = contextWindows
        self.promptCacheRetention = promptCacheRetention
        self.readClaudeConfigs = readClaudeConfigs
        self.newSessionModel = newSessionModel
    }
}

/// One change to a server's settings.
public enum ServerSettingsChange: Equatable, Sendable {
    /// An empty model clears it.
    case defaultModel(String)
    case compactionModel(String)
    /// A context-window override; nil removes it.
    case contextWindow(String, Int?)
    case promptCacheRetention(PromptCacheRetention)
    case readClaudeConfigs(Bool)
}

/// A settings request the server refused, with its explanation.
public struct ServerSettingsFailed: LocalizedError, Equatable {
    public let message: String
    public var errorDescription: String? { message }

    public init(message: String) {
        self.message = message
    }
}

/// A server's settings API.
public protocol ServerSettingsClient: Sendable {
    func serverSettings() async throws -> ServerSettings
    /// Applies one change and returns the resulting settings.
    func updateServerSettings(_ change: ServerSettingsChange) async throws -> ServerSettings
    /// The models the server offers, for choosing settings' models.
    func models() async throws -> [WireModelCapability]
}

extension ServerClient: ServerSettingsClient {
    public func serverSettings() async throws -> ServerSettings { try await resolve().serverSettings() }
    public func updateServerSettings(_ change: ServerSettingsChange) async throws -> ServerSettings {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.updateServerSettings(change)
    }
}
