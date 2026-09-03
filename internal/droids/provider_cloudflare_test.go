package droids

import "testing"

func TestCloudflareGatewayOpenAI(t *testing.T) {
	gw := CloudflareGateway{AccountID: "acct", GatewayID: "gw", Token: "tok"}
	cfg := gw.OpenAI(OpenAI{APIKey: "sk", Headers: map[string]string{"cf-aig-skip-cache": "true"}})

	want := "https://gateway.ai.cloudflare.com/v1/acct/gw/openai"
	if cfg.BaseURL != want {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, want)
	}
	if cfg.APIKey != "sk" {
		t.Fatalf("APIKey not preserved: %q", cfg.APIKey)
	}
	if got := cfg.Headers["cf-aig-authorization"]; got != "Bearer tok" {
		t.Fatalf("auth header = %q", got)
	}
	if got := cfg.Headers["cf-aig-skip-cache"]; got != "true" {
		t.Fatalf("config header not merged: %q", got)
	}
}

func TestCloudflareGatewayAnthropic(t *testing.T) {
	gw := CloudflareGateway{AccountID: "acct", GatewayID: "gw"}
	cfg := gw.Anthropic(Anthropic{APIKey: "sk"})

	want := "https://gateway.ai.cloudflare.com/v1/acct/gw/anthropic"
	if cfg.BaseURL != want {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, want)
	}
	// No token => no auth header.
	if _, ok := cfg.Headers["cf-aig-authorization"]; ok {
		t.Fatal("unexpected auth header without token")
	}
}

func TestCloudflareGatewayConfigHeaderWins(t *testing.T) {
	gw := CloudflareGateway{
		AccountID: "a", GatewayID: "g", Token: "gwtok",
		Headers: map[string]string{"x-shared": "gateway"},
	}
	cfg := gw.OpenAI(OpenAI{Headers: map[string]string{"x-shared": "config"}})
	if got := cfg.Headers["x-shared"]; got != "config" {
		t.Fatalf("config header should win, got %q", got)
	}
}
