package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/providerlogin"
)

func TestExecuteOpenAICodexLoginPrintsInstructionsAndSuccess(t *testing.T) {
	t.Parallel()
	login := &fakeDeviceLogin{instructions: DeviceLoginInstructions{
		VerificationURI: "https://example.test/device", UserCode: "ABCD-EFGH",
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	var stdout, stderr bytes.Buffer
	if code := executeOpenAICodexLogin(context.Background(), login, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	want := "Open https://example.test/device\nEnter code: ABCD-EFGH\nWaiting for authorization…\nLogged in to OpenAI Codex.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestExecuteOpenAICodexLoginStopsWhenInstructionsCannotBeWritten(t *testing.T) {
	t.Parallel()
	login := &fakeDeviceLogin{instructions: DeviceLoginInstructions{VerificationURI: "https://example.test/device", UserCode: "CODE"}}
	var stderr bytes.Buffer
	if code := executeOpenAICodexLogin(context.Background(), login, failingWriter{}, &stderr); code != 1 || stderr.String() != "kit: complete OpenAI Codex login: write failed\n" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestExecuteOpenAICodexLoginCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if code := executeOpenAICodexLogin(ctx, &fakeDeviceLogin{err: context.Canceled}, &stdout, &stderr); code != 130 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

// codexCredential is a saved OpenAI Codex credential, as a login stores it.
const codexCredential = `{"openai-codex": {"type": "oauth", "access": "secret-access", "accountId": "account", "revision": "v1:test"}}`

// authHome runs a server for an isolated home whose device logins save a
// Codex credential once approved.
func authHome(t *testing.T) (string, chan<- error) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "kit")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(apphome.EnvHome, home)
	approved := make(chan error, 1)
	startDaemonWith(t, home, &providerlogin.Flows{
		Device: func(ctx context.Context, _ string, notify func(providerlogin.DeviceInstructions)) error {
			notify(providerlogin.DeviceInstructions{VerificationURI: "https://auth.openai.com/codex/device", UserCode: "ABCD-1234", ExpiresAt: time.Now().Add(15 * time.Minute)})
			select {
			case err := <-approved:
				if err != nil {
					return err
				}
				return os.WriteFile(apphome.FromHome(home).Auth, []byte(codexCredential), 0o600)
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	return home, approved
}

func TestAuthCommandsLogTheServerInAndOut(t *testing.T) {
	_, approved := authHome(t)
	status := func() string {
		t.Helper()
		code, stdout, stderr := runCLI("auth", "status")
		if code != 0 {
			t.Fatalf("status = %d, %q", code, stderr)
		}
		return stdout
	}
	if got := status(); got != "openai-codex  OpenAI Codex  not logged in\n"+
		"anthropic     Anthropic     not logged in\n"+
		"openai        OpenAI        not logged in\n"+
		"opencode      OpenCode Zen  not logged in\n"+
		"opencode-go   OpenCode Go   not logged in\n" {
		t.Fatalf("status before login =\n%s", got)
	}

	approved <- nil
	code, stdout, stderr := runCLI("auth", "login", "openai-codex")
	if code != 0 || stdout != "Open https://auth.openai.com/codex/device\nEnter code: ABCD-1234\nWaiting for authorization…\nLogged in to OpenAI Codex.\n" {
		t.Fatalf("login = %d, %q, %q", code, stdout, stderr)
	}
	if got := status(); !strings.HasPrefix(got, "openai-codex  OpenAI Codex  logged in\n") || strings.Contains(got, "secret-access") {
		t.Fatalf("status after login =\n%s", got)
	}

	if code, stdout, stderr := runCLI("auth", "logout", "openai-codex"); code != 0 || stdout != "Logged out of openai-codex.\n" {
		t.Fatalf("logout = %d, %q, %q", code, stdout, stderr)
	}
	if got := status(); !strings.HasPrefix(got, "openai-codex  OpenAI Codex  not logged in\n") {
		t.Fatalf("status after logout =\n%s", got)
	}
	if code, _, stderr := runCLI("auth", "logout", "nope"); code != 2 || stderr != "kit: unknown provider \"nope\"; list providers with `kit auth status`\n" {
		t.Fatalf("logout unknown = %d, %q", code, stderr)
	}
}

func TestAFailedLoginReportsTheServersReason(t *testing.T) {
	_, approved := authHome(t)
	approved <- errors.New("device authorization was denied")
	code, _, stderr := runCLI("auth", "login", "openai-codex")
	if code != 1 || stderr != "kit: complete OpenAI Codex login: device authorization was denied\n" {
		t.Fatalf("login = %d, %q", code, stderr)
	}
}

func TestEnvironmentCredentialsCannotBeChangedFromTheCLI(t *testing.T) {
	t.Setenv("OPENAI_CODEX_ACCESS_TOKEN", "access")
	t.Setenv("OPENAI_CODEX_ACCOUNT_ID", "account")
	for _, name := range []string{"OPENAI_CODEX_REFRESH_TOKEN", "OPENAI_CODEX_ID_TOKEN", "OPENAI_CODEX_FEDRAMP", "OPENAI_CODEX_EXPIRES_AT"} {
		t.Setenv(name, "")
	}
	authHome(t)
	if code, stdout, _ := runCLI("auth", "status"); code != 0 || !strings.HasPrefix(stdout, "openai-codex  OpenAI Codex  logged in from the server's environment\n") {
		t.Fatalf("status = %d\n%s", code, stdout)
	}
	refusal := "the server's credentials for OpenAI Codex come from its environment; change them there\n"
	if code, _, stderr := runCLI("auth", "login", "openai-codex"); code != 1 || stderr != "kit: complete OpenAI Codex login: "+refusal {
		t.Errorf("login = %d, %q", code, stderr)
	}
	if code, _, stderr := runCLI("auth", "logout", "openai-codex"); code != 1 || stderr != "kit: log out of openai-codex: "+refusal {
		t.Errorf("logout = %d, %q", code, stderr)
	}
}

func TestAuthCommandUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"auth", "login"}, {"auth", "login", "anthropic"}, {"auth", "logout"}, {"auth", "unknown"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Parse(context.Background(), args, &stdout, &stderr).ExitCode; code != 2 {
			t.Errorf("Parse(%q) exit = %d, stderr = %q", args, code, stderr.String())
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type fakeDeviceLogin struct {
	instructions DeviceLoginInstructions
	err          error
}

func (f *fakeDeviceLogin) Login(_ context.Context, notify func(DeviceLoginInstructions) error) error {
	if f.instructions.VerificationURI != "" {
		if err := notify(f.instructions); err != nil {
			return err
		}
	}
	return f.err
}

// Reading status never starts a server, which would replace one that is
// serving sessions with an incompatible version.
func TestAuthStatusNeedsARunningServer(t *testing.T) {
	t.Setenv(apphome.EnvHome, filepath.Join(t.TempDir(), "kit"))
	if code, stdout, stderr := runCLI("auth", "status"); code != 1 || stdout != "" ||
		stderr != "kit: server is not running; start it with `kit server start`\n" {
		t.Fatalf("status = %d, %q, %q", code, stdout, stderr)
	}
}
