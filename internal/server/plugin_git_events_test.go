package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/protocol"
)

func TestPluginGitEventsObserveExternalChangesWithoutAttachedClient(t *testing.T) {
	gitExecutable, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git fixture requires git")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python fixture requires python3")
	}
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	root := filepath.Join(paths.Plugins, "git-events")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"manifestVersion":1,"id":"git-events","transport":{"type":"stdio","command":` + mustJSON(t, python) + `,"args":["plugin.py"]}}`
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := `import json, sys
for line in sys.stdin:
    message = json.loads(line)
    method = message.get("method")
    if method == "initialize":
        print(json.dumps({"jsonrpc":"2.0","id":message["id"],"result":{"protocolVersion":1}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","id":"ready-register","method":"kit/commands/register","params":{"id":"ready","description":"Ready marker"}}), flush=True)
    elif method == "shutdown":
        print(json.dumps({"jsonrpc":"2.0","id":message["id"],"result":None}), flush=True)
        break
    elif method in ("kit/events/git.changed",):
        with open("events.jsonl", "a") as output:
            output.write(json.dumps(message, separators=(",", ":")) + "\n")
`
	if err := os.WriteFile(filepath.Join(root, "plugin.py"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
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
			t.Error("daemon shutdown blocked")
		}
	}()
	client := NewClient(paths)
	defer client.http.CloseIdleConnections()
	eventually(t, func() bool { _, _, err := client.Probe(t.Context()); return err == nil })
	cwd := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitExecutable, append([]string{"-C", cwd, "-c", "user.name=Kit test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	git("init", "-b", "main")
	git("commit", "--allow-empty", "-m", "initial")
	canonicalCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: cwd, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		snapshot, err := client.GetSessionSnapshot(t.Context(), created.ID)
		return err == nil && len(snapshot.PluginCommands) == 1 && snapshot.PluginCommands[0].ID == "git-events.ready"
	})

	// After readiness, observation uses neither an attached client nor VCS HTTP polling.
	client.http.CloseIdleConnections()
	// Allow one ten-second poll plus scheduling/probe headroom.
	waitForGit := func(condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("Git event did not arrive")
	}
	assertGit := func(count int, branch any, dirty bool) {
		t.Helper()
		waitForGit(func() bool {
			events := readJSONLines(t, filepath.Join(root, "events.jsonl"))
			if len(events) != count {
				return false
			}
			event := events[count-1]
			params := event["params"].(map[string]any)
			value := params["git"].(map[string]any)
			if event["method"] != "kit/events/git.changed" || len(params) != 1 || len(value) != 4 || value["pullRequest"] != nil || value["root"] != canonicalCWD || value["branch"] != branch || value["dirty"] != dirty {
				t.Fatalf("Git event=%#v", event)
			}
			return true
		})
	}
	if err := os.WriteFile(filepath.Join(cwd, "external.txt"), []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	assertGit(1, "main", true)
	git("checkout", "-b", "other")
	assertGit(2, "other", true)
	if err := os.Remove(filepath.Join(cwd, "external.txt")); err != nil {
		t.Fatal(err)
	}
	assertGit(3, "other", false)
	git("checkout", "--detach")
	assertGit(4, nil, false)
	time.Sleep(10500 * time.Millisecond)
	if events := readJSONLines(t, filepath.Join(root, "events.jsonl")); len(events) != 4 {
		t.Fatalf("duplicate Git events=%#v", events)
	}
}
