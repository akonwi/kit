package sessionclient

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
)

var (
	// ErrSessionNotFound indicates that no saved session matches a selector.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionAmbiguous indicates that a short selector matches several sessions.
	ErrSessionAmbiguous = errors.New("session selector is ambiguous")
)

// ResolveSession finds an exact or uniquely prefixed saved session. Exact IDs
// win over short-ID matching. Short selectors may omit the session_ prefix.
func ResolveSession(ctx context.Context, server Server, selector string) (protocol.SessionInfo, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return protocol.SessionInfo{}, fmt.Errorf("%w: selector is empty", ErrSessionNotFound)
	}
	sessions, err := server.ListSessions(ctx, "")
	if err != nil {
		return protocol.SessionInfo{}, err
	}
	for _, candidate := range sessions {
		if candidate.ID == selector {
			return candidate, nil
		}
	}
	canonicalPrefix := selector
	if !strings.HasPrefix(canonicalPrefix, "session_") {
		canonicalPrefix = "session_" + canonicalPrefix
	}
	var match protocol.SessionInfo
	matches := 0
	for _, candidate := range sessions {
		if strings.HasPrefix(candidate.ID, canonicalPrefix) {
			match = candidate
			matches++
		}
	}
	switch matches {
	case 0:
		return protocol.SessionInfo{}, fmt.Errorf("%w: %s", ErrSessionNotFound, selector)
	case 1:
		return match, nil
	default:
		return protocol.SessionInfo{}, fmt.Errorf("%w: %s matches %d sessions", ErrSessionAmbiguous, selector, matches)
	}
}
