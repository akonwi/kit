import Foundation
import Observation

protocol SessionReloadClient: SessionClient {
    func reloadSession(_ id: String) async throws -> WireReloadSessionResult
}

extension LocalClient: SessionReloadClient {
    func reloadSession(_ id: String) async throws -> WireReloadSessionResult {
        let transport: HTTPClient
        do { transport = try await HTTPClient.local() }
        catch { throw MutationNotSent(reason: error.localizedDescription) }
        return try await transport.reloadSession(id)
    }
}

/// Summarizes reload evidence without replacing the workspace with a results dialog.
struct SessionReloadFeedback {
    enum Recovery { case refresh, reload }
    let title: String
    let detail: String
    let report: SessionReloadReport?
    let tone: SessionFeedback.Tone
    let persistent: Bool
    let recovery: Recovery?

    @MainActor static func from(_ operation: SessionReloadOperation) -> Self? {
        guard !operation.pending, operation.error != nil || operation.result != nil else { return nil }
        let refreshError = operation.refreshError.map { "Session refresh failed: " + $0 }
        if let error = operation.error {
            let detail = [error, refreshError].compactMap { $0 }.joined(separator: " · ")
            return Self(title: "Session reload failed", detail: detail, report: nil, tone: .error,
                        persistent: true, recovery: refreshError == nil ? .reload : .refresh)
        }
        let warnings = operation.result?.warnings ?? []
        let diagnostics = operation.result?.diagnostics ?? []
        let report = SessionReloadReport(
            diagnostics: ([refreshError].compactMap { $0 } + warnings).map { .init(message: $0, sourcePath: nil) }
                + diagnostics.map { .init(message: $0.message, sourcePath: $0.source.path,
                                          sourceID: $0.source.id, severity: $0.severity, code: $0.code) },
            sources: (operation.result?.sources ?? []).map { .init(id: $0.id, path: $0.path) },
            warningCount: warnings.count + diagnostics.filter { $0.severity == "warning" }.count,
            refreshFailure: refreshError)
        let persistent = !report.diagnostics.isEmpty
        let tone: SessionFeedback.Tone = report.diagnostics.isEmpty ? .info : .warning
        return Self(title: report.diagnostics.isEmpty ? "Session context reloaded" : "Session context reloaded with issues",
                    detail: report.preview, report: report.diagnostics.isEmpty ? nil : report,
                    tone: tone, persistent: persistent,
                    recovery: refreshError == nil ? nil : .refresh)
    }
}

/// Reload is explicit; recovery refreshes never silently repeat the mutation.
@MainActor @Observable
final class SessionReloadOperation {
    private(set) var pending = false
    private(set) var result: WireReloadSessionResult?
    private(set) var error: String?
    private(set) var refreshError: String?
    var progressLabel: String {
        result != nil || error != nil ? "Refreshing session…" : "Reloading session context…"
    }

    func perform(session: String, client: any SessionReloadClient,
                 refresh: @MainActor () async throws -> Void) async {
        guard !pending else { return }
        pending = true
        result = nil; error = nil; refreshError = nil
        defer { pending = false }
        do { result = try await client.reloadSession(session) }
        catch { self.error = error.localizedDescription }
        await refreshState(refresh)
    }

    func refreshOnly(_ refresh: @MainActor () async throws -> Void) async {
        guard !pending else { return }
        pending = true
        defer { pending = false }
        await refreshState(refresh)
    }

    private func refreshState(_ refresh: @MainActor () async throws -> Void) async {
        refreshError = nil
        do { try await refresh() }
        catch is CancellationError { refreshError = "Session changed before its local refresh completed" }
        catch { refreshError = error.localizedDescription }
    }
}
