import Foundation
import Observation

protocol SessionCompactionClient: SessionClient {
    func compactSession(_ id: String, input: WireCompactSessionInput) async throws -> WireCompactSessionResult
}

extension LocalClient: SessionCompactionClient {
    func compactSession(_ id: String, input: WireCompactSessionInput) async throws -> WireCompactSessionResult {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.compactSession(id, input: input)
    }
}

/// A retry must resolve the original operation, never compact a second time.
@MainActor @Observable
final class SessionCompactionOperation {
    private(set) var operationID: String?
    private(set) var pending = false
    private(set) var feedbackDismissed = false
    var showsFeedback: Bool { pending || (!feedbackDismissed && (result != nil || error != nil || refreshError != nil)) }
    func dismissFeedback() { feedbackDismissed = true }
    private(set) var result: WireCompactSessionResult?
    private(set) var error: String?
    private(set) var refreshError: String?

    var title: String {
        if pending { return result == nil ? "Compacting session…" : "Refreshing session…" }
        if let result { return result.compacted ? "Session compacted" : "Not enough turns to compact" }
        if error != nil { return "Compaction failed" }
        return "Compact session context"
    }

    func perform(session: String, client: any SessionCompactionClient,
                 refresh: @MainActor () async throws -> Void) async {
        guard !pending else { return }
        feedbackDismissed = false
        pending = true; error = nil; refreshError = nil
        defer { pending = false }
        if operationID == nil {
            operationID = "compact_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        }
        if result == nil {
            do {
                result = try await client.compactSession(session, input: .init(operationId: operationID!))
            } catch {
                self.error = error.localizedDescription
                // Preserve identity for every ambiguous outcome, including a later busy response.
            }
        }
        // Resynchronize after uncertainty as well: the request may have completed.
        do { try await refresh() }
        catch { refreshError = error.localizedDescription }
    }

    func beginNewIfResolved() {
        guard !pending, result != nil, refreshError == nil else { return }
        operationID = nil; result = nil; error = nil
    }
}
