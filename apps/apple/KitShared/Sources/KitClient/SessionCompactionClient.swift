import Foundation
import Observation

public protocol SessionCompactionClient: SessionClient {
    func compactSession(_ id: String, input: WireCompactSessionInput) async throws -> WireCompactSessionResult
}

extension ServerClient: SessionCompactionClient {
    public func compactSession(_ id: String, input: WireCompactSessionInput) async throws -> WireCompactSessionResult {
        let transport: HTTPClient
        do { transport = try await resolve() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.compactSession(id, input: input)
    }
}

/// A retry must resolve the original operation, never compact a second time.
@MainActor @Observable
public final class SessionCompactionOperation {
    public init() {}

    public private(set) var operationID: String?
    public private(set) var pending = false
    public private(set) var feedbackDismissed = false
    public var showsFeedback: Bool { pending || (!feedbackDismissed && (result != nil || error != nil || refreshError != nil)) }
    public func dismissFeedback() { feedbackDismissed = true }
    public private(set) var result: WireCompactSessionResult?
    public private(set) var error: String?
    /// Why the server reported the compaction failed, when it did.
    public private(set) var failure: CompactionFailed?
    public private(set) var refreshError: String?
    /// Whether asking again might succeed: not when the server reported a
    /// failure that would repeat.
    public var retryable: Bool { failure?.reason.retryable ?? true }

    public var title: String {
        if pending { return result == nil ? "Compacting session…" : "Refreshing session…" }
        if let result { return result.compacted ? "Session compacted" : "Not enough turns to compact" }
        if error != nil { return "Compaction failed" }
        return "Compact session context"
    }

    public func perform(session: String, client: any SessionCompactionClient,
                 refresh: @MainActor () async throws -> Void) async {
        guard !pending else { return }
        feedbackDismissed = false
        pending = true; error = nil; failure = nil; refreshError = nil
        defer { pending = false }
        if operationID == nil {
            operationID = "compact_" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        }
        if result == nil {
            do {
                result = try await client.compactSession(session, input: .init(operationId: operationID!))
            } catch {
                self.error = error.localizedDescription
                failure = error as? CompactionFailed
                // Preserve identity for every ambiguous outcome, including a later busy response.
            }
        }
        // Resynchronize after uncertainty as well: the request may have completed.
        do { try await refresh() }
        catch { refreshError = error.localizedDescription }
    }

    public func beginNewIfResolved() {
        guard !pending, result != nil, refreshError == nil else { return }
        operationID = nil; result = nil; error = nil; failure = nil
    }
}
