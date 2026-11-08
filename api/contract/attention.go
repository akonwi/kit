package contract

import (
	"strings"
	"time"
)

// CompareAttention orders two listed sessions in SessionListOrderAttention.
// It returns a negative number when a comes before b, a positive number when
// it comes after, and zero only for the same session ID.
//
// Sessions awaiting a response come first, the one whose oldest pending
// interaction is oldest first. Running sessions come next and idle sessions
// last, each most recently updated first. Ties fall back to ascending
// session ID, so the order is total. Clients that merge lists from several
// servers apply the same rule.
func CompareAttention(a, b SessionInfo) int {
	rankA, rankB := attentionRank(a), attentionRank(b)
	if rankA != rankB {
		return rankA - rankB
	}
	if rankA == 0 {
		if order := attentionTime(a.Activity.Since).Compare(attentionTime(b.Activity.Since)); order != 0 {
			return order
		}
	} else if order := attentionTime(b.UpdatedAt).Compare(attentionTime(a.UpdatedAt)); order != 0 {
		return order
	}
	return strings.Compare(a.ID, b.ID)
}

// attentionRank is a session's group in the attention order: 0 awaiting a
// response, 1 running, 2 idle.
func attentionRank(session SessionInfo) int {
	if session.Activity == nil {
		return 2
	}
	switch session.Activity.Status {
	case SessionActivityAwaitingResponse:
		return 0
	case SessionActivityRunning:
		return 1
	default:
		return 2
	}
}

// attentionTime reads a validated timestamp. An unreadable one sorts as the
// zero time; validation rejects it before a client relies on the order.
func attentionTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
