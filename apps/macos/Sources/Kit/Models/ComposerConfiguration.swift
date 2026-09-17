import Foundation
import Observation

@MainActor @Observable
final class ComposerConfiguration {
    var models: [WireModelCapability] = []
    var loading = false
    var changing = false
    var error: String?

    func load(client: any ComposerClient) async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        do { models = try await client.models(); error = nil }
        catch is CancellationError {} catch { self.error = error.localizedDescription }
    }

    func change(client: any ComposerClient, session: SessionExcerpt, model: String, thinking: String,
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
