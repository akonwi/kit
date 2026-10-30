import Foundation
import Observation

/// A session's model catalog and changes to its model and thinking level.
@MainActor @Observable
public final class ComposerConfiguration {
    public var models: [WireModelCapability] = []
    public var loading = false
    public var changing = false
    public var error: String?

    public init() {}

    public func replaceCatalog(_ models: [WireModelCapability]) {
        self.models = models
    }

    public func load(client: any ComposerClient) async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        do { models = try await client.models(); error = nil }
        catch let failure where isCancellation(failure) {}
        catch { self.error = error.localizedDescription }
    }

    /// The thinking levels a model offers, as the catalog lists them.
    public static func thinkingLevels(model: String, models: [WireModelCapability]) -> [String] {
        models.first(where: { $0.id == model })?.thinkingLevels?.map(\.rawValue) ?? []
    }

    /// The thinking level to keep when switching to `model`: the current one
    /// when the model offers it, otherwise its first level.
    public static func thinkingLevel(switchingTo model: String, from current: String, models: [WireModelCapability]) -> String {
        let levels = thinkingLevels(model: model, models: models)
        return levels.contains(current) ? current : levels.first ?? "off"
    }

    public func change(client: any ComposerClient, session: SessionExcerpt, model: String, thinking: String,
                       refresh: @escaping @MainActor () async throws -> Void) async {
        guard !changing, let revision = session.configurationRevision,
              let level = WireThinkingLevel(rawValue: thinking) else { return }
        changing = true; error = nil
        defer { changing = false }
        do {
            _ = try await client.configure(session.id, input: .init(expectedRevision: revision, model: model, thinkingLevel: level))
            try await refresh()
        } catch {
            self.error = error.localizedDescription
            // A concurrent change or lost acknowledgement requires authoritative metadata.
            try? await refresh()
        }
    }
}
