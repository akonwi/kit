import Foundation

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
