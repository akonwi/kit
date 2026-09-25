package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/auth"
	kitserver "github.com/akonwi/kit/internal/server"
)

func TestExecuteOpenAICodexLoginPrintsInstructionsAndSuccess(t *testing.T) {
	t.Parallel()
	login := &fakeOpenAICodexLogin{instructions: auth.OpenAICodexDeviceInstructions{
		VerificationURI: "https://example.test/device", UserCode: "ABCD-EFGH",
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	var stdout, stderr bytes.Buffer
	if code := executeOpenAICodexLogin(context.Background(), login, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "https://example.test/device") ||
		!strings.Contains(stdout.String(), "ABCD-EFGH") ||
		!strings.Contains(stdout.String(), "Logged in") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if login.calls != 1 {
		t.Fatalf("login calls = %d", login.calls)
	}
}

func TestExecuteOpenAICodexLoginStopsWhenInstructionsCannotBeWritten(t *testing.T) {
	t.Parallel()
	login := &fakeOpenAICodexLogin{instructions: auth.OpenAICodexDeviceInstructions{
		VerificationURI: "https://example.test/device", UserCode: "CODE",
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	var stderr bytes.Buffer
	if code := executeOpenAICodexLogin(context.Background(), login, failingWriter{}, &stderr); code != 1 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "write failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestExecuteOpenAICodexLoginCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	login := &fakeOpenAICodexLogin{err: context.Canceled}
	var stdout, stderr bytes.Buffer
	if code := executeOpenAICodexLogin(ctx, login, &stdout, &stderr); code != 130 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestAuthStatusAndLogoutCommands(t *testing.T) {
	home := filepath.Join(t.TempDir(), "kit")
	t.Setenv(apphome.EnvHome, home)
	paths := apphome.FromHome(home)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Auth, []byte(`{
		"openai-codex": {
			"type": "oauth",
			"access": "secret-access",
			"accountId": "account",
			"revision": "v1:test"
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"auth", "status"}, &stdout, &stderr); code != 0 {
		t.Fatalf("status exit = %d, stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "openai-codex\toauth\tsaved") || strings.Contains(got, "secret-access") {
		t.Fatalf("status output = %q", got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"auth", "logout", "openai-codex"}, &stdout, &stderr); code != 0 {
		t.Fatalf("logout exit = %d, stderr = %q", code, stderr.String())
	}
	record, err := auth.NewStore(paths.Auth).LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.Revision != "" || record.Credentials.AccessToken != "" {
		t.Fatalf("record after logout = %#v", record)
	}
}

func TestLoginAndLogoutRejectEnvironmentBackedDaemon(t *testing.T) {
	home := filepath.Join(t.TempDir(), "kit")
	t.Setenv(apphome.EnvHome, home)
	t.Setenv("OPENAI_CODEX_ACCESS_TOKEN", "access")
	t.Setenv("OPENAI_CODEX_ACCOUNT_ID", "account")
	for _, name := range []string{
		"OPENAI_CODEX_REFRESH_TOKEN", "OPENAI_CODEX_ID_TOKEN", "OPENAI_CODEX_FEDRAMP", "OPENAI_CODEX_EXPIRES_AT",
	} {
		t.Setenv(name, "")
	}
	paths := apphome.FromHome(home)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- kitserver.Run(ctx, kitserver.RunOptions{
			Paths: paths, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	manager := kitserver.NewManager(paths)
	probeContext, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	for {
		if _, _, err := manager.Status(probeContext); err == nil {
			break
		}
		select {
		case <-probeContext.Done():
			t.Fatal("daemon did not become ready")
		case <-time.After(10 * time.Millisecond):
		}
	}

	for _, args := range [][]string{{"auth", "login", "openai-codex"}, {"auth", "logout", "openai-codex"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, &stdout, &stderr); code != 1 {
			t.Errorf("Run(%q) exit = %d, stderr = %q", args, code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "environment credentials") {
			t.Errorf("Run(%q) stderr = %q", args, stderr.String())
		}
	}

	stopContext, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := manager.Stop(stopContext); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-stopContext.Done():
		t.Fatal("daemon did not stop")
	}
}

func TestAuthCommandUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"auth", "login"}, {"auth", "login", "anthropic"}, {"auth", "logout"}, {"auth", "unknown"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, &stdout, &stderr); code != 2 {
			t.Errorf("Run(%q) exit = %d, stderr = %q", args, code, stderr.String())
		}
	}
}

func TestExecuteOpenAICodexLoginReportsFailure(t *testing.T) {
	t.Parallel()
	login := &fakeOpenAICodexLogin{err: errors.New("disk unavailable")}
	var stdout, stderr bytes.Buffer
	if code := executeOpenAICodexLogin(context.Background(), login, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "complete OpenAI Codex login") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type fakeOpenAICodexLogin struct {
	instructions auth.OpenAICodexDeviceInstructions
	err          error
	calls        int
}

func (f *fakeOpenAICodexLogin) Login(
	_ context.Context,
	notify func(auth.OpenAICodexDeviceInstructions) error,
) error {
	f.calls++
	if f.instructions.VerificationURI != "" {
		if err := notify(f.instructions); err != nil {
			return err
		}
	}
	return f.err
}
