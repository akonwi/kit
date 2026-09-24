package daemon

import (
	"context"
	"errors"
	"github.com/akonwi/kit/internal/session"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/protocol"
)

func commandOwnerInstance(selection string) string {
	index := strings.LastIndexByte(selection, ':')
	if index < 0 {
		return selection
	}
	return selection[:index]
}

func TestPluginCommandHTTPFixtureCatalogExecutionAndStaleOwner(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python fixture requires python3")
	}
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	root := filepath.Join(paths.Plugins, "plugin-demo")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plugin.json", "plugin.py"} {
		data, err := os.ReadFile(filepath.Join("..", "..", ".kit", "plugins", "plugin-demo", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: paths, Providers: &daemonEchoProviders{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("daemon did not stop")
		}
	}()
	client := NewClient(paths)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, err := client.Probe(t.Context()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cwd := t.TempDir()
	created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: cwd, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	catalog := func(old string) protocol.SessionSnapshot {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			snapshot, err := client.GetSessionSnapshot(t.Context(), created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.PluginCommands) == 2 && snapshot.PluginCommands[0].Instance != old {
				return snapshot
			}
			if time.Now().After(deadline) {
				t.Fatalf("catalog not ready: %#v", snapshot)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	before := catalog("")
	command := before.PluginCommands[1]
	if command.ID != "plugin-demo.echo" || command.LocalID != "echo" || command.Description == "" {
		t.Fatalf("command = %#v", command)
	}
	input := protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance, Args: "example.txt"}
	toastContext, cancelToasts := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelToasts()
	toastBody, err := client.StreamPluginToasts(toastContext, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer toastBody.Close()
	notices := make(chan protocol.PluginToast, 1)
	toastResult := make(chan error, 1)
	go func() {
		toastResult <- ReadPluginToasts(toastBody, func(toast protocol.PluginToast) error { notices <- toast; return io.EOF })
	}()
	if err := client.ExecutePluginCommand(t.Context(), created.ID, input); err != nil {
		t.Fatal(err)
	}
	select {
	case toast := <-notices:
		if toast.Title != "Plugin echo" || toast.Subtitle != input.Args || toast.PluginID != "plugin-demo" || toast.Instance != commandOwnerInstance(command.Instance) || toast.Variant != "info" {
			t.Fatalf("live toast = %#v", toast)
		}
	case <-toastContext.Done():
		t.Fatal("missing live Python plugin toast")
	}
	if err := <-toastResult; !errors.Is(err, io.EOF) {
		t.Fatalf("toast decoding = %v", err)
	}
	toastBody.Close()
	cancelToasts()
	after, err := client.GetSessionSnapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Messages) != 0 || after.ActiveRunID != "" || after.Usage != before.Usage {
		t.Fatalf("command started model run: %#v", after)
	}
	if _, err := client.ReloadSession(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	_ = catalog(command.Instance)
	if err := client.ExecutePluginCommand(t.Context(), created.ID, input); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("stale selection = %v", err)
	}
	other, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: cwd, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ExecutePluginCommand(t.Context(), other.ID, input); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("cross-session selection = %v", err)
	}
}

func TestPluginCommandErrorsHaveSafeStableCodes(t *testing.T) {
	for _, test := range []struct {
		err           error
		status        int
		code, message string
	}{
		{session.ErrPluginCommandUnavailable, http.StatusConflict, protocol.PluginCommandUnavailable, session.ErrPluginCommandUnavailable.Error()},
		{errors.Join(session.ErrPluginCommandFailed, errors.New("private plugin failure detail")), http.StatusUnprocessableEntity, protocol.PluginCommandFailed, session.ErrPluginCommandFailed.Error()},
	} {
		recorder := httptest.NewRecorder()
		writeSessionError(recorder, test.err)
		var failure *APIError
		if err := decodeAPIError(recorder.Code, recorder.Body.Bytes()); !errors.As(err, &failure) || failure.StatusCode != test.status || failure.Code != test.code || failure.Message != test.message {
			t.Fatalf("error projection = %#v", err)
		}
	}
}

func TestPluginCommandErrorDecoderValidatesBoundary(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
	}{
		{http.StatusOK, `{"error":{"code":"plugin_command_failed","message":"Failed"}}`},
		{http.StatusConflict, `{"error":{"code":"plugin_command_failed","message":"Failed"}}`},
		{http.StatusConflict, `{"error":{"code":"plugin_command_unavailable","message":"\u001b"}}`},
		{http.StatusConflict, `{"error":{"code":"plugin_command_unavailable","message":"Unavailable","details":{"unexpected":true}}}`},
	} {
		var failure *protocol.PluginCommandError
		if err := decodeAPIError(test.status, []byte(test.body)); errors.As(err, &failure) {
			t.Fatalf("accepted malformed typed failure: %#v", failure)
		}
	}
	err := decodeAPIError(http.StatusConflict, []byte(`{"error":{"code":"plugin_command_unavailable","message":"Unavailable"}}`))
	var failure *protocol.PluginCommandError
	if !errors.As(err, &failure) || failure.Code != protocol.PluginCommandUnavailable {
		t.Fatalf("typed unwrap = %#v", err)
	}
}
