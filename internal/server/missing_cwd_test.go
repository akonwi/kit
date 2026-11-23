package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemonctl"
	"github.com/akonwi/kit/internal/identifier"
)

// startTestDaemon runs a daemon over paths until stopTestDaemon stops it.
func startTestDaemon(t *testing.T, paths apphome.Paths, providers *daemonEchoProviders) <-chan error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, RunOptions{Paths: paths, Providers: providers, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	probeContext, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	for {
		if _, _, err := localTransport(paths).Probe(probeContext, true); err == nil {
			return result
		}
		select {
		case <-probeContext.Done():
			t.Fatal("daemon did not become ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func stopTestDaemon(t *testing.T, paths apphome.Paths, result <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := daemonctl.NewManager(paths).Stop(ctx); err != nil {
		t.Fatalf("stop daemon: %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("daemon did not stop")
	}
}

// A session outlives its working directory: after the directory is removed
// and the daemon restarts, every session route still serves it.
func TestSessionServesAfterItsWorkingDirectoryIsRemoved(t *testing.T) {
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	providers := &daemonEchoProviders{}
	running := startTestDaemon(t, paths, providers)
	client := localTransport(paths)
	ctx := t.Context()

	removed := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(filepath.Join(removed, ".agents", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Home, "AGENTS.md"), []byte("global guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(removed, "AGENTS.md"), []byte("removed guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(destination, "AGENTS.md"), []byte("destination guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionID, err := identifier.New("session_")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateSession(ctx, protocol.CreateSessionInput{
		ID: sessionID, CWD: removed, Model: "test/echo", ThinkingLevel: "off", Name: "Removed worktree",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Prompt(ctx, sessionID, "before removal"); err != nil {
		t.Fatal(err)
	}
	stopTestDaemon(t, paths, running)
	if err := os.RemoveAll(removed); err != nil {
		t.Fatal(err)
	}
	running = startTestDaemon(t, paths, providers)
	defer stopTestDaemon(t, paths, running)

	snapshot, err := client.GetSessionSnapshot(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionSnapshot() of a session whose cwd was removed: %v", err)
	}
	if snapshot.Session.CWD != removed || snapshot.Workspace == nil || snapshot.Workspace.State != protocol.WorkspaceUnavailable {
		t.Fatalf("snapshot cwd = %q, workspace = %+v", snapshot.Session.CWD, snapshot.Workspace)
	}
	toasts, err := client.StreamPluginToasts(ctx, sessionID)
	if err != nil {
		t.Fatalf("StreamPluginToasts(): %v", err)
	}
	_ = toasts.Close()
	if err := client.AttachClientPlugin(ctx, sessionID, testClientPluginInstance, protocol.AttachClientPluginInput{
		PluginID: "browser", Tools: []protocol.ClientPluginToolDeclaration{testSnapshotTool},
	}); err != nil {
		t.Fatalf("AttachClientPlugin(): %v", err)
	}
	if _, err := client.GetSessionVCSStatus(ctx, sessionID); err != nil {
		t.Fatalf("GetSessionVCSStatus(): %v", err)
	}
	if _, err := client.Prompt(ctx, sessionID, "after removal"); err != nil {
		t.Fatalf("Prompt(): %v", err)
	}
	if _, err := client.ForkSession(ctx, sessionID, protocol.ForkSessionInput{}); err != nil {
		t.Fatalf("ForkSession(): %v", err)
	}
	if _, err := client.ReloadSession(ctx, sessionID); err != nil {
		t.Fatalf("ReloadSession(): %v", err)
	}
	if _, err := client.ChangeSessionCWD(ctx, sessionID, destination); err != nil {
		t.Fatalf("ChangeSessionCWD(): %v", err)
	}
	if _, err := client.Prompt(ctx, sessionID, "after the move"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	systemPrompt := providers.requests[len(providers.requests)-1].SystemPrompt
	providers.mu.Unlock()
	if !strings.Contains(systemPrompt, "global guidance") || !strings.Contains(systemPrompt, "destination guidance") {
		t.Fatalf("system prompt after moving from the removed cwd lacks rediscovered context:\n%s", systemPrompt)
	}
}
