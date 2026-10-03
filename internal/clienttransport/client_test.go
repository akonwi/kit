package clienttransport

import (
	"net/http"
	"testing"
)

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
