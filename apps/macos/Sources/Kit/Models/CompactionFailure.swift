import Foundation

/// How the app explains a failed context compaction. The server classifies
/// the failure and the app words it, so no provider text reaches the user.
extension WireCompactionFailureReason {
    /// The sentence that explains why compaction failed.
    var explanation: String {
        switch self {
        case .summaryTruncated: "The summary was longer than the compaction model's output limit."
        case .summaryInvalid: "The compaction model didn't return a usable summary."
        case .policyStop: "The provider stopped the summary under its usage policy."
        case .providerError: "The request to the model provider failed."
        case .notAdaptable: "The conversation can't be reduced to fit the model's context."
        case .canceled: "Compaction was canceled before it finished."
        case .conflict: "The session changed while compacting."
        case .internalError: "Kit couldn't finish compacting because of an internal error."
        }
    }

    /// Whether compacting again might succeed. A conversation that can't be
    /// reduced, a policy stop, or a summary over its output limit fails the
    /// same way again.
    var retryable: Bool {
        switch self {
        case .notAdaptable, .policyStop, .summaryTruncated: false
        case .summaryInvalid, .providerError, .canceled, .conflict, .internalError: true
        }
    }
}

/// A session operation that failed because its compaction did: an explicit
/// compaction, or a model change that needed one.
struct CompactionFailed: LocalizedError, Equatable {
    /// The start of a failed turn's row when compaction failed it.
    static let title = "Context compaction failed."

    let reason: WireCompactionFailureReason

    init(reason: WireCompactionFailureReason) { self.reason = reason }

    /// The reason a compaction_failed error carries. The generated and app
    /// vocabularies are the same closed set.
    init(_ details: Components.Schemas.CompactionFailedErrorDetails) {
        reason = WireCompactionFailureReason(rawValue: details.reason.rawValue) ?? .internalError
    }

    var errorDescription: String? { reason.explanation }

    /// The row of a turn that failed because its context couldn't be
    /// compacted.
    static func turnText(_ reason: WireCompactionFailureReason) -> String {
        title + " " + reason.explanation
    }
}
