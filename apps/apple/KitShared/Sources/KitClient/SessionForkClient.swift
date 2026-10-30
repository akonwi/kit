import Foundation
import Observation

/// A published fork. `firstTurnError` reports a first message that could not
/// start; the fork itself still succeeded.
public struct ForkedSession: Sendable {
    public let session: SessionExcerpt
    public let firstTurnError: String?

    public init(session: SessionExcerpt, firstTurnError: String?) {
        self.session = session
        self.firstTurnError = firstTurnError
    }
}

public protocol SessionForkClient: SessionClient {
    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> ForkedSession
}

extension ServerClient: SessionForkClient {
    public func forkSession(_ id: String, input: WireForkSessionInput) async throws -> ForkedSession {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.forkSession(id, input: input)
    }
}

/// Submits one fork at a time. The server chooses the child's ID, so each
/// submission creates a new fork.
@MainActor @Observable public final class SessionForkOperation {
    public init() {}

    /// The server's bound on prompt text.
    public static let maxMessageBytes = 128 << 10

    public private(set) var pending = false
    public private(set) var error: String?

    public func submit(client: any SessionForkClient, source: String, name: String, message: String = "") async -> ForkedSession? {
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
@MainActor @Observable public final class ForkFirstTurnFailures {
    public static let shared = ForkFirstTurnFailures()

    public struct Failure: Equatable {
        public let message: String
        public let error: String

    public init(message: String, error: String) {
        self.message = message
        self.error = error
    }
    }
    @ObservationIgnored private var failures: [SessionIdentity: Failure] = [:]
    public private(set) var revision = 0

    public func record(_ failure: Failure, for identity: SessionIdentity) {
        if failures.count >= 16 { failures.removeAll() }
        failures[identity] = failure
        revision += 1
    }

    public func take(_ identity: SessionIdentity) -> Failure? { failures.removeValue(forKey: identity) }
}
