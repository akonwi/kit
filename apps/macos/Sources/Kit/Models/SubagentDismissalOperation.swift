import Foundation
import Observation

@MainActor @Observable
final class SubagentDismissalOperation {
    private(set) var pending = false
    private(set) var error: String?

    func dismiss(session: String, conversation: String, generation: UInt64,
                 client: any SubagentDismissalClient,
                 refresh: @MainActor () async throws -> Void) async -> Bool {
        guard !pending else { return false }
        pending = true
        error = nil
        defer { pending = false }
        var acknowledged = false
        do {
            try await client.dismissSubagent(session, conversation: conversation, generation: generation)
            acknowledged = true
        } catch ClientError.http(409) {
            error = "This conversation changed. Review its current state and try again."
        } catch {
            self.error = error.localizedDescription
        }
        // Refresh after failures too, but never replay a mutation against a newer generation.
        do { try await refresh() }
        catch is CancellationError { }
        catch { self.error = self.error ?? error.localizedDescription }
        return acknowledged
    }
}
