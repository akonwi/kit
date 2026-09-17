import Foundation
import Observation

/// Synthetic behavior for the offline demo; never writes the server replica.
@MainActor @Observable
final class DemoSessionState {
    private(set) var messages: [TranscriptMessage] = []
    var model = "Recorded model"
    var thinking = "high"
    private(set) var replaying = false
    var notice = ""
    var interactions: [InteractionFlow] = []
    private var original: SessionExcerpt?
    @ObservationIgnored private var replayTask: Task<Void, Never>?

    deinit { replayTask?.cancel() }

    func select(_ session: SessionExcerpt?) {
        stop()
        original = session
        messages = session?.messages ?? []
        model = session?.model ?? "Recorded model"
        thinking = session?.thinking ?? "high"
        notice = ""
        interactions = []
    }

    func append(_ message: TranscriptMessage) { messages.append(message) }

    func stop() {
        replayTask?.cancel()
        replayTask = nil
        replaying = false
    }

    func replay() {
        stop()
        guard let original = original?.messages, let last = original.last, last.role == "assistant" else { return }
        messages = Array(original.dropLast())
        messages.append(TranscriptMessage(id: last.id, role: last.role, text: "", tools: []))
        replaying = true
        notice = "Replaying a recorded response"
        replayTask = Task { [weak self] in
            let characters = Array(last.text)
            for count in stride(from: 12, through: characters.count + 11, by: 12) {
                do { try await Task.sleep(for: .milliseconds(35)) } catch { return }
                guard let self, !Task.isCancelled, !self.messages.isEmpty else { return }
                self.messages[self.messages.count - 1].text = String(characters.prefix(count))
            }
            self?.replaying = false
            self?.notice = "Recorded response complete"
        }
    }

}
