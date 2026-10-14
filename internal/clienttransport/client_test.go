package clienttransport

import (
	"errors"
	"net/http"
	"testing"

	"github.com/akonwi/kit/internal/version"
)

func TestCheckCompatibilityComparesOnlyProtocols(t *testing.T) {
	t.Parallel()
	for _, release := range []string{version.Version, "0.42.0", "0.43.0-rc.1", "dev", "release-b"} {
		if err := CheckCompatibility(version.SessionProtocolVersion, release); err != nil {
			t.Errorf("server %q on the client protocol rejected: %v", release, err)
		}
	}
	for _, tc := range []struct {
		protocol int
		want     CompatibilityReason
	}{
		{version.SessionProtocolVersion - 1, ServerProtocolOlder},
		{version.SessionProtocolVersion + 1, ClientProtocolOlder},
	} {
		err := CheckCompatibility(tc.protocol, "other")
		var mismatch *CompatibilityError
		if !errors.Is(err, ErrIncompatibleServer) || !errors.As(err, &mismatch) || mismatch.Reason != tc.want || mismatch.ServerProtocol != tc.protocol || mismatch.ServerVersion != "other" {
			t.Errorf("protocol %d = %v (%+v), want %s", tc.protocol, err, mismatch, tc.want)
		}
	}
}

func TestNewEndpointDisablesEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	client := NewEndpoint("http://kit.invalid", "secret")

	for name, httpClient := range map[string]*http.Client{
		"bounded": client.http,
		"session": client.sessionHTTP,
	} {
		transport, ok := httpClient.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("%s transport = %T", name, httpClient.Transport)
		}
		if transport.Proxy != nil {
			t.Fatalf("%s transport inherited an environment proxy", name)
		}
	}
}
