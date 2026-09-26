package server

import (
	"errors"
	"net/http"
	"testing"

	"github.com/akonwi/kit/internal/sessionclient"
)

func TestSessionAPICompatibilityClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"protocol rejection", &APIError{StatusCode: http.StatusUpgradeRequired, Message: "upgrade required"}, true},
		{"typed registry mismatch", &DaemonCompatibilityError{Reason: ClientProtocolOlder}, true},
		{"authentication", &APIError{StatusCode: http.StatusUnauthorized}, false},
		{"missing session", &APIError{StatusCode: http.StatusNotFound}, false},
		{"transient transport", errors.New("connection refused"), false},
		{"no error", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionclient.IsIncompatibleDaemon(tc.err); got != tc.want {
				t.Fatalf("IsIncompatibleDaemon(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}
