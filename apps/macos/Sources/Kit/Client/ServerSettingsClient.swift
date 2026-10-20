import Foundation

/// How long providers are asked to keep prompt cache entries.
enum PromptCacheRetention: String, Sendable, CaseIterable {
    case short, long
}

/// The settings that govern a server's behavior. Each server has its own;
/// client preferences such as themes and diff presentation are not among them.
struct ServerSettings: Equatable, Sendable {
    /// The model for new sessions that name none; empty lets the server choose.
    var defaultModel = ""
    /// The model that summarizes context; empty uses each session's own.
    var compactionModel = ""
    /// Context-window overrides by exact provider/model.
    var contextWindows: [String: Int] = [:]
    var promptCacheRetention = PromptCacheRetention.short
    /// Whether sessions discover Claude Code configuration, such as a
    /// project's `.claude/commands`.
    var readClaudeConfigs = true
    /// The model a new session that names none would start with now, or
    /// empty when no model is available.
    var newSessionModel = ""

    /// The model a new session form starts on: the saved default, then the
    /// suggestion, then the server's choice, then the first offered.
    func preferredModel(available: [String], suggested: String?) -> String {
        [defaultModel, suggested ?? "", newSessionModel].first { !$0.isEmpty && available.contains($0) } ?? available.first ?? ""
    }
}

/// One change to a server's settings.
enum ServerSettingsChange: Equatable, Sendable {
    /// An empty model clears it.
    case defaultModel(String)
    case compactionModel(String)
    /// A context-window override; nil removes it.
    case contextWindow(String, Int?)
    case promptCacheRetention(PromptCacheRetention)
    case readClaudeConfigs(Bool)
}

/// A settings request the server refused, with its explanation.
struct ServerSettingsFailed: LocalizedError, Equatable {
    let message: String
    var errorDescription: String? { message }
}

/// A server's settings API.
protocol ServerSettingsClient: Sendable {
    func serverSettings() async throws -> ServerSettings
    /// Applies one change and returns the resulting settings.
    func updateServerSettings(_ change: ServerSettingsChange) async throws -> ServerSettings
    /// The models the server offers, for choosing settings' models.
    func models() async throws -> [WireModelCapability]
}

extension LocalClient: ServerSettingsClient {
    func serverSettings() async throws -> ServerSettings { try await HTTPClient.local().serverSettings() }
    func updateServerSettings(_ change: ServerSettingsChange) async throws -> ServerSettings {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.updateServerSettings(change)
    }
}
