package cli

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/daemon"
)

func TestRunVersion(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "kit ") {
		t.Fatalf("stdout = %q, want version", stdout.String())
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "kit new") || !strings.Contains(stdout.String(), "kit daemon start") {
		t.Fatalf("stdout = %q, want new-session and daemon help", stdout.String())
	}
}

func TestRunNewHelpAndArgumentValidation(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"new", "--help"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "kit new") || stderr.Len() != 0 {
		t.Fatalf("new help exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"new", "unexpected"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "new accepts no arguments") {
		t.Fatalf("new argument exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestSupportsInteractiveAPIKeyLoginRequiresBothProviderSources(t *testing.T) {
	t.Parallel()
	registry := daemon.Registry{CredentialSources: map[string]daemon.CredentialSource{
		auth.OpenAIProviderID: daemon.CredentialSourceStore,
	}}
	if supportsInteractiveAPIKeyLogin(registry) {
		t.Fatal("partial API-key credential metadata was accepted")
	}
	registry.CredentialSources[auth.AnthropicProviderID] = daemon.CredentialSourceEnvironment
	if !supportsInteractiveAPIKeyLogin(registry) {
		t.Fatal("complete API-key credential metadata was rejected")
	}
}

func TestInteractiveProvidersPreferCodexCredentials(t *testing.T) {
	t.Parallel()
	providers, model := interactiveProviders([]string{"anthropic", "openai", "openai-codex"})
	if !providers["openai-codex"] || !providers["openai"] || !providers["anthropic"] {
		t.Fatalf("providers = %#v", providers)
	}
	if model != "openai-codex/gpt-5.6-sol" {
		t.Fatalf("default model = %q", model)
	}
}

func TestInteractiveLocationIncludesGitBranch(t *testing.T) {
	directory := t.TempDir()
	command := exec.Command("git", "-C", directory, "init", "-b", "ui-slice")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	location := interactiveLocation(context.Background(), directory)
	if !strings.Contains(location, "(ui-slice)") {
		t.Fatalf("location = %q, want branch", location)
	}
}

func TestDaemonStatusWhenUnavailable(t *testing.T) {
	t.Setenv(apphome.EnvHome, filepath.Join(t.TempDir(), "kit"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"daemon", "status"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "daemon unavailable") {
		t.Fatalf("stderr = %q, want unavailable message", stderr.String())
	}
}
