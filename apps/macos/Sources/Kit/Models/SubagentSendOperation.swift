import Foundation
import Observation

/// One draft per session and agent, independent of the pane's lifetime.
@MainActor @Observable
final class SubagentSendOperation {
    var draft = ""
    private(set) var pending = false
    private(set) var error: String?
    private(set) var warning: String?
    private(set) var conversationID: String?

    func send(session: String, agent: String, conversation: String?, client: any SubagentMessagingClient,
              refresh: @MainActor () async throws -> Void) async {
        guard !pending, !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        let message = draft
        guard message.utf8.count <= 128 * 1024, !message.contains("\0") else {
            error = "The message must be at most 128 KiB and contain no null characters."
            return
        }
        pending = true
        error = nil
        warning = nil
        defer { pending = false }
        do {
            let receipt = try await client.sendSubagent(session, agent: agent,
                conversation: conversation ?? conversationID, message: message)
            conversationID = receipt.conversationID
            warning = receipt.warning
            if draft == message { draft = "" }
        } catch {
            self.error = error.localizedDescription
        }
        // The endpoint has no idempotency key: resync on uncertainty, never replay.
        do { try await refresh() }
        catch is CancellationError { }
        catch { self.error = self.error ?? error.localizedDescription }
    }
}
