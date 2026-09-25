import Foundation
import Observation

@MainActor @Observable
final class SubagentConversationState {
    private(set) var messages: [TranscriptMessage] = []
    private(set) var activity: String?
    private(set) var loaded = false
    private(set) var loading = false
    private(set) var error: String?
    private(set) var unavailable = false
    private var generation = UUID()

    /// The view owns this lifetime; hidden panes cancel polling and reconnect delays.
    func watch(client: any SubagentClient, session: String, conversation: String) async {
        guard let stream = client as? any SubagentStreamingClient else {
            await load(client: client, session: session, conversation: conversation)
            return
        }
        let request = UUID()
        generation = request
        loading = !loaded
        error = nil
        unavailable = false
        defer { if generation == request { loading = false; activity = nil } }
        var delay = 1
        while !Task.isCancelled, generation == request {
            do {
                try await stream.watchSubagent(session: session, conversation: conversation) { [weak self] update in
                    await self?.receive(update, request: request)
                }
                try Task.checkCancellation()
                throw ClientError.disconnected
            } catch {
                guard !Task.isCancelled, generation == request else { return }
                loading = false
                activity = nil
                let hadConnection = self.error == nil && loaded
                if hadConnection { delay = 1 }
                switch error {
                case ClientError.http(404):
                    unavailable = true
                    self.error = "This subagent conversation is no longer available."
                    return
                case ClientError.http(401), ClientError.http(403), ClientError.invalidPayload,
                     ClientError.oversized, ClientError.incompatible, is DecodingError:
                    self.error = error.localizedDescription
                    return
                default:
                    self.error = "\(error.localizedDescription) Retrying in \(delay)s…"
                }
                do { try await Task.sleep(for: .seconds(delay)) } catch { return }
                delay = min(delay * 2, 15)
            }
        }
    }

    private func receive(_ update: SubagentTranscriptUpdate, request: UUID) {
        guard generation == request, !Task.isCancelled else { return }
        messages = update.messages
        activity = update.activity
        loading = false
        loaded = true
        error = nil
        unavailable = false
    }

    /// The presenting pane owns and cancels the task. Late responses cannot replace newer data.
    func load(client: any SubagentClient, session: String, conversation: String) async {
        let request = UUID()
        generation = request
        loading = true
        error = nil
        unavailable = false
        defer { if generation == request { loading = false } }
        do {
            let result = try await client.subagentTranscript(session: session, conversation: conversation)
            try Task.checkCancellation()
            guard generation == request else { return }
            messages = result
            loaded = true
        } catch {
            guard !Task.isCancelled, generation == request else { return }
            if case ClientError.http(404) = error {
                unavailable = true
                self.error = "This subagent conversation is no longer available."
            } else {
                self.error = error.localizedDescription
            }
        }
    }
}
