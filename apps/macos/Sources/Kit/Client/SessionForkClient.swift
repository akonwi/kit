import Foundation
import Observation

/// A published fork. `firstTurnError` reports a first message that could not
/// start; the fork itself still succeeded.
struct ForkedSession: Sendable {
    let session: SessionExcerpt
    let firstTurnError: String?
}

protocol SessionForkClient: SessionClient {
    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> ForkedSession
}

extension LocalClient: SessionForkClient {
    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> ForkedSession {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.forkSession(id, input: input)
    }
}

/// Submits one fork at a time. The server chooses the child's ID, so each
/// submission creates a new fork.
@MainActor @Observable final class SessionForkOperation {
    /// The server's bound on prompt text.
    static let maxMessageBytes = 128 << 10

    private(set) var pending = false
    private(set) var error: String?

    func submit(client: any SessionForkClient, source: String, name: String, message: String = "") async -> ForkedSession? {
        guard !pending else { return nil }
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.isEmpty || SessionName.normalized(trimmed) != nil else {
            error = "Use a name without control characters, up to 256 bytes."
            return nil
        }
        let text = message.trimmingCharacters(in: .whitespacesAndNewlines)
        guard text.utf8.count <= Self.maxMessageBytes, !text.contains("\u{0}") else {
            error = "Use a message without NUL characters, up to 128 KiB."
            return nil
        }
        let prompt = text.isEmpty ? nil : WirePromptInput(text: text, attachmentIds: nil, annotationIds: nil)
        let input = WireForkSessionInput(name: trimmed.isEmpty ? nil : trimmed, prompt: prompt)
        pending = true; error = nil
        defer { pending = false }
        do { return try await client.forkSession(source, input: input) }
        catch {
            switch error {
            case ClientError.http(409): self.error = "This session can’t be forked until its interrupted work settles. Try again after it finishes."
            case ClientError.http(400), ClientError.http(422): self.error = "This session cannot be forked with these settings. Only persistent sessions can be forked."
            default: self.error = error.localizedDescription
            }
            return nil
        }
    }
}

/// Hands a fork's failed first message to the child's window, which presents
/// it like any failed prompt submission: the message returns to the composer
/// with the failure shown. A window that is already open observes `revision`.
@MainActor @Observable final class ForkFirstTurnFailures {
    static let shared = ForkFirstTurnFailures()

    struct Failure: Equatable {
        let message: String
        let error: String
    }
    @ObservationIgnored private var failures: [SessionIdentity: Failure] = [:]
    private(set) var revision = 0

    func record(_ failure: Failure, for identity: SessionIdentity) {
        if failures.count >= 16 { failures.removeAll() }
        failures[identity] = failure
        revision += 1
    }

    func take(_ identity: SessionIdentity) -> Failure? { failures.removeValue(forKey: identity) }
}
