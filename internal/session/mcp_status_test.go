package session

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/mcpconfig"
)

func TestSafeMCPErrorNeverProjectsCredentialBearingDetails(t *testing.T) {
	secret := "Authorization: Bearer token-value?code=callback&client_secret=hidden"
	got := safeMCPError(secret)
	if got != "Connection failed." || strings.Contains(got, "token-value") || strings.Contains(got, "callback") {
		t.Fatalf("safe MCP error = %q", got)
	}
	if got := safeMCPError("  "); got != "" {
		t.Fatalf("empty error = %q", got)
	}
}

func TestMCPConfigurationWarningOmitsDiagnosticSecrets(t *testing.T) {
	warning := safeMCPDiagnostic(mcpconfig.Diagnostic{Path: "/tmp/.mcp.json", Server: "docs", Field: "url", Message: "invalid https://example.test?token=secret"})
	if strings.Contains(warning, "secret") || strings.Contains(warning, "example.test") {
		t.Fatalf("warning leaked diagnostic: %q", warning)
	}
}

func TestBoundedMCPText(t *testing.T) {
	got := boundedMCPText(strings.Repeat("x", 600), 512)
	if len(got) != 512 || !strings.HasSuffix(got, "...") {
		t.Fatalf("bounded text length = %d", len(got))
	}
}
