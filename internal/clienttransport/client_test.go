package clienttransport

import (
	"net/http"
	"testing"
)

func TestCoveredReleasePair(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		protocol       int
		client, server string
		want           bool
	}{
		{42, "0.39.0", "0.39.1", true},
		{45, "0.42.0", "0.42.1", true},
		{45, "0.41.0", "0.42.0", false},
		{45, "0.42.0-rc.1", "0.42.0", false},
		{44, "0.42.0", "0.42.1", false},
	} {
		if got := coveredReleasePair(tc.protocol, tc.client, tc.server); got != tc.want {
			t.Errorf("protocol %d releases %q/%q = %t, want %t", tc.protocol, tc.client, tc.server, got, tc.want)
		}
	}
}

func TestNewEndpointDisablesEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	client := NewEndpoint("http://kit.invalid", "secret", "instance_test")

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
