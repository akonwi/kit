package sessionbridge

import (
	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

// CompactionFailed is the title of a failed compaction, and the start of a
// failed turn's row when compaction failed it.
const CompactionFailed = "Context compaction failed."

// CompactionFailureText is the sentence that explains why a compaction
// failed. It is empty for a reason this client does not know.
func CompactionFailureText(reason string) string {
	switch protocol.CompactionFailureReason(reason) {
	case protocol.CompactionFailureSummaryTruncated:
		return "The summary was longer than the compaction model's output limit."
	case protocol.CompactionFailureSummaryInvalid:
		return "The compaction model didn't return a usable summary."
	case protocol.CompactionFailurePolicyStop:
		return "The provider stopped the summary under its usage policy."
	case protocol.CompactionFailureProviderError:
		return "The request to the model provider failed."
	case protocol.CompactionFailureNotAdaptable:
		return "The conversation can't be reduced to fit the model's context."
	case protocol.CompactionFailureCanceled:
		return "Compaction was canceled before it finished."
	case protocol.CompactionFailureConflict:
		return "The session changed while compacting."
	case protocol.CompactionFailureInternal:
		return "Kit couldn't finish compacting because of an internal error."
	default:
		return ""
	}
}

// CompactionFailedTurnText is the row of a turn that failed because its
// context could not be compacted: the failure and, when known, its reason.
func CompactionFailedTurnText(reason string) string {
	if text := CompactionFailureText(reason); text != "" {
		return CompactionFailed + " " + text
	}
	return CompactionFailed
}

// CompactionFailureReason is the reason err reports that a compaction, or a
// model change that needed one, failed. It is empty for any other error.
func CompactionFailureReason(err error) string {
	reason, ok := kit.CompactionFailure(err)
	if !ok {
		return ""
	}
	return string(reason)
}

// OperationErrorText is how a client shows a failed session operation: the
// reason a compaction failed, or err's own text.
func OperationErrorText(err error) string {
	if text := CompactionFailureText(CompactionFailureReason(err)); text != "" {
		return text
	}
	return err.Error()
}
