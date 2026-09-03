package session

import (
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestProjectDroidErrorKind(t *testing.T) {
	t.Parallel()
	tests := map[droids.ErrorKind]ProviderErrorKind{
		droids.ErrorAuthentication: ProviderErrorAuthentication,
		droids.ErrorEntitlement:    ProviderErrorEntitlement,
		droids.ErrorUsageLimit:     ProviderErrorUsageLimit,
		droids.ErrorRateLimit:      ProviderErrorRateLimit,
		droids.ErrorTransport:      ProviderErrorTransport,
		droids.ErrorProtocol:       ProviderErrorProtocol,
		"unknown":                  "",
	}
	for input, want := range tests {
		if got := projectDroidErrorKind(input); got != want {
			t.Errorf("projectDroidErrorKind(%q) = %q, want %q", input, got, want)
		}
	}
}
