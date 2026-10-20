import Foundation
import Observation

/// Edits one server's settings for Settings › Models. It reads them from the
/// server, applies one change at a time, and shows what the server returns.
/// A failed change keeps the last confirmed settings and records the reason
/// against the field that failed.
@MainActor @Observable
final class ServerSettingsEditor {
    /// A field whose change is saving or failed.
    enum Field: Hashable {
        case defaultModel, compactionModel, promptCacheRetention, readClaudeConfigs
        /// One model's context-window override.
        case contextWindow(String)
        /// The form that adds an override.
        case newOverride
    }

    struct Failure: Equatable {
        let field: Field
        let message: String
    }

    let client: any ServerSettingsClient
    private(set) var settings = ServerSettings()
    private(set) var models: [WireModelCapability] = []
    private(set) var loaded = false
    /// Why the settings couldn't be read.
    private(set) var loadError: String?
    /// Why the model catalog couldn't be read; saved settings stay editable.
    private(set) var catalogError: String?
    private(set) var saving: Field?
    private(set) var failure: Failure?

    init(client: any ServerSettingsClient) { self.client = client }

    func load() async {
        do {
            settings = try await client.serverSettings()
            loaded = true
            loadError = nil
        } catch {
            loadError = error.localizedDescription
        }
    }

    func loadCatalog() async {
        do {
            models = try await client.models()
            catalogError = nil
        } catch {
            catalogError = error.localizedDescription
        }
    }

    /// Saves change unless another is saving, and reports whether it saved.
    @discardableResult
    func save(_ change: ServerSettingsChange, as field: Field? = nil) async -> Bool {
        guard saving == nil else { return false }
        let field = field ?? Self.field(of: change)
        saving = field
        failure = nil
        defer { saving = nil }
        do {
            settings = try await client.updateServerSettings(change)
            return true
        } catch {
            failure = Failure(field: field, message: error.localizedDescription)
            return false
        }
    }

    /// The reason field's last change failed, if it did.
    func failure(for field: Field) -> String? {
        failure?.field == field ? failure?.message : nil
    }

    /// A model's catalog name, or its id when the catalog doesn't have it.
    func name(of model: String) -> String {
        models.first { $0.id == model }?.name ?? model
    }

    private static func field(of change: ServerSettingsChange) -> Field {
        switch change {
        case .defaultModel: .defaultModel
        case .compactionModel: .compactionModel
        case .contextWindow(let model, _): .contextWindow(model)
        case .promptCacheRetention: .promptCacheRetention
        case .readClaudeConfigs: .readClaudeConfigs
        }
    }
}
