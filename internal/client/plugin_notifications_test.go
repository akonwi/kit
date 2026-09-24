package client

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemon"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
)

func TestBoundPluginToastStreamExecutesRealFixtureAndCancels(t *testing.T) {
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
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(ctx, daemon.RunOptions{Paths: paths, Providers: reloadProviders{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	defer func() {
		stop()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	}()
	transport := daemon.NewClient(paths)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, err := transport.Probe(t.Context()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	workspace := t.TempDir()
	server := NewLocalServer(paths)
	created, err := server.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: workspace, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := server.Attach(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	watcher, ok := bound.(sessionclient.PluginToastSession)
	if !ok {
		t.Fatal("bound session lacks live notifications")
	}
	executor, ok := bound.(sessionclient.PluginCommandSession)
	if !ok {
		t.Fatal("bound session lacks plugin commands")
	}
	var commands []protocol.PluginCommand
	deadline = time.Now().Add(5 * time.Second)
	for {
		snapshot, err := bound.Snapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		commands = snapshot.PluginCommands
		if len(commands) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("plugin commands did not load")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for index, title := range []string{"Plugin context", "Plugin echo"} {
		watchContext, cancel := context.WithCancel(t.Context())
		stream, err := watcher.WatchPluginToasts(watchContext)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		command := commands[index]
		if err := executor.ExecutePluginCommand(t.Context(), protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance, Args: "example.txt"}); err != nil {
			cancel()
			t.Fatal(err)
		}
		select {
		case toast := <-stream.Updates():
			if toast.Title != title || toast.PluginID != "plugin-demo" || !strings.HasPrefix(command.Instance, toast.Instance+":") || toast.Variant != "info" {
				cancel()
				t.Fatalf("bound live toast = %#v", toast)
			}
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("missing bound live toast")
		}
		cancel()
		select {
		case _, ok := <-stream.Updates():
			if ok {
				t.Fatal("unexpected buffered toast after cancellation")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cancel did not close stream")
		}
		_ = stream.Err()
	}
}
