import Foundation
import Observation

/// One in-flight child configuration change and its pane-local feedback.
///
/// The roster snapshot remains authoritative: success and failure both refresh
/// it, and the operation never replays a request or projects values itself.
@MainActor @Observable
final class SubagentConfigurationOperation {
    private(set) var pending = false
    private(set) var error: String?
    private(set) var warning: String?

    func clearFeedback() {
        error = nil
        warning = nil
    }

    /// Applies a model and/or thinking patch. `outstandingWork` reads the
    /// refreshed roster so a conflict can explain why a model change was refused.
    @discardableResult
    func change(session: String, conversation: String, generation: UInt64, agent: String,
                model: String?, thinking: String?, client: any SubagentConfigurationClient,
                outstandingWork: @MainActor () -> Bool,
                refresh: @MainActor () async throws -> Void) async -> Bool {
        guard !pending, model != nil || thinking != nil else { return false }
        pending = true
        error = nil
        warning = nil
        defer { pending = false }
        var acknowledged = false
        var conflict = false
        do {
            let result = try await client.configureSubagent(session, conversation: conversation,
                generation: generation, model: model, thinking: thinking)
            acknowledged = true
            warning = result.warnings.isEmpty ? nil : result.warnings.joined(separator: "\n")
        } catch ClientError.http(409) {
            conflict = true
        } catch ClientError.http(404) {
            error = "This subagent conversation is no longer available."
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
        do { try await refresh() }
        catch is CancellationError { }
        catch { self.error = self.error ?? error.localizedDescription }
        if conflict {
            error = model != nil && outstandingWork()
                ? "\(agent) has active or queued work. Change its model when that work finishes."
                : "This conversation changed. Review its current configuration and try again."
        }
        return acknowledged
    }
}
