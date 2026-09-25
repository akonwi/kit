import Foundation
import Observation

protocol SessionForkClient: SessionClient {
    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> SessionExcerpt
}

extension LocalClient: SessionForkClient {
    func forkSession(_ id: String, input: WireForkSessionInput) async throws -> SessionExcerpt {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.forkSession(id, input: input)
    }
}

/// One child identity is retained across retries, including lost acknowledgements.
@MainActor @Observable final class SessionForkOperation {
    private(set) var input: WireForkSessionInput?
    private(set) var pending = false
    private(set) var error: String?

    func submit(client: any SessionForkClient, source: String, name: String) async -> SessionExcerpt? {
        guard !pending else { return nil }
        if input == nil {
            let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard trimmed.isEmpty || SessionName.normalized(trimmed) != nil else {
                error = "Use a name without control characters, up to 256 bytes."
                return nil
            }
            input = WireForkSessionInput(id: "session_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased(), name: trimmed)
        }
        guard let input else { return nil }
        pending = true; error = nil
        defer { pending = false }
        do { return try await client.forkSession(source, input: input) }
        catch {
            switch error {
            case ClientError.http(409): self.error = "The session must be settled before it can be forked. Try again after its current work finishes."
            case ClientError.http(400), ClientError.http(422): self.error = "This session cannot be forked with these settings. Only settled persistent sessions can be forked."
            default: self.error = error.localizedDescription
            }
            return nil
        }
    }
}
