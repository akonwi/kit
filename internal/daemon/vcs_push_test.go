package daemon

import (
	"context"
	"encoding/json"
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

func TestVCSPushSharesPRCompletionWithPluginsAndReconnect(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("requires git")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("requires python3")
	}
	fixture := t.TempDir()
	release := filepath.Join(fixture, "release")
	t.Setenv("KIT_PR_RELEASE", release)
	t.Setenv("PATH", fixture+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(fixture, "gh"), []byte(`#!/bin/sh
while [ ! -f "$KIT_PR_RELEASE" ]; do sleep 0.01; done
printf '%s' '{"number":42,"url":"https://github.com/a/b/pull/42","headRefName":"main"}'
`), 0700); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=Kit test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "--allow-empty", "-m", "initial"}} {
		cmd := exec.Command(git, append([]string{"-C", repo}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Git: %v %s", err, output)
		}
	}
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	root := filepath.Join(paths.Plugins, "vcs-events")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"manifestVersion":1,"id":"vcs-events","transport":{"type":"stdio","command":` + mustJSON(t, python) + `,"args":["plugin.py"]}}`
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	plugin := `import json, sys
for line in sys.stdin:
    message = json.loads(line)
    method = message.get("method")
    if method == "initialize":
        with open("initial.json", "w") as output: json.dump(message["params"], output)
        print(json.dumps({"jsonrpc":"2.0","id":message["id"],"result":{"protocolVersion":1}}), flush=True)
        print(json.dumps({"jsonrpc":"2.0","id":"ready","method":"kit/commands/register","params":{"id":"ready","description":"Ready"}}), flush=True)
    elif method == "shutdown":
        print(json.dumps({"jsonrpc":"2.0","id":message["id"],"result":None}), flush=True)
        break
    elif method == "kit/events/git.changed":
        with open("events.jsonl", "a") as output: output.write(json.dumps(message) + "\n")
`
	if err := os.WriteFile(filepath.Join(root, "plugin.py"), []byte(plugin), 0600); err != nil {
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
	created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: repo, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, stop := context.WithCancel(t.Context())
	defer stop()
	body, err := client.StreamSessionVCS(streamCtx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	received := make(chan protocol.SessionVCSStatus, 16)
	readerDone := make(chan error, 1)
	go func() {
		readerDone <- ReadSessionVCS(body, func(value protocol.SessionVCSStatus) error {
			select {
			case received <- value:
				return nil
			case <-streamCtx.Done():
				return streamCtx.Err()
			}
		})
	}()
	defer func() {
		stop()
		body.Close()
		select {
		case <-readerDone:
		case <-time.After(time.Second):
			t.Error("VCS reader did not stop")
		}
	}()
	next := func(match func(protocol.SessionVCSStatus) bool) protocol.SessionVCSStatus {
		t.Helper()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case value := <-received:
				if match(value) {
					return value
				}
			case <-timer.C:
				t.Fatal("no pushed VCS update")
				return protocol.SessionVCSStatus{}
			}
		}
	}
	local := next(func(value protocol.SessionVCSStatus) bool { return value.Status != nil })
	if local.Status.Head.Name != "main" || local.Status.PullRequest != nil {
		t.Fatalf("initial local state=%+v", local)
	}
	eventually(t, func() bool {
		snapshot, err := client.GetSessionSnapshot(t.Context(), created.ID)
		return err == nil && len(snapshot.PluginCommands) == 1
	})
	raw, err := os.ReadFile(filepath.Join(root, "initial.json"))
	if err != nil {
		t.Fatal(err)
	}
	var initial map[string]any
	if err := json.Unmarshal(raw, &initial); err != nil {
		t.Fatal(err)
	}
	gitContext := initial["context"].(map[string]any)["project"].(map[string]any)["git"].(map[string]any)
	if _, exists := gitContext["pullRequest"]; !exists || gitContext["pullRequest"] != nil {
		t.Fatalf("initial Git context=%v", gitContext)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	remote := next(func(value protocol.SessionVCSStatus) bool {
		return value.Status != nil && value.Status.PullRequest != nil
	})
	if remote.Status.PullRequest.Number != 42 || remote.Status.Head != local.Status.Head || remote.Status.Dirty != local.Status.Dirty {
		t.Fatalf("PR-only push=%+v", remote)
	}
	eventually(t, func() bool { return len(readJSONLines(t, filepath.Join(root, "events.jsonl"))) == 1 })
	event := readJSONLines(t, filepath.Join(root, "events.jsonl"))[0]
	pluginGit := event["params"].(map[string]any)["git"].(map[string]any)
	pr := pluginGit["pullRequest"].(map[string]any)
	if pr["number"] != float64(42) || pr["url"] != remote.Status.PullRequest.URL {
		t.Fatalf("plugin/client disagree: %v %+v", pluginGit, remote.Status.PullRequest)
	}
	reconnectCtx, reconnectCancel := context.WithCancel(t.Context())
	reconnected, err := client.StreamSessionVCS(reconnectCtx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var fresh protocol.SessionVCSStatus
	_ = ReadSessionVCS(reconnected, func(value protocol.SessionVCSStatus) error { fresh = value; reconnectCancel(); return context.Canceled })
	reconnected.Close()
	reconnectCancel()
	if fresh.Status == nil || fresh.Status.PullRequest == nil || fresh.Status.PullRequest.Number != 42 {
		t.Fatalf("reconnect snapshot=%+v", fresh)
	}
	outside := t.TempDir()
	changed, err := client.ChangeSessionCWD(t.Context(), created.ID, outside)
	if err != nil {
		t.Fatal(err)
	}
	cleared := next(func(value protocol.SessionVCSStatus) bool { return value.CWD == changed.CWD })
	if cleared.Status != nil {
		t.Fatalf("new cwd retained old VCS: %+v", cleared)
	}
	eventually(t, func() bool {
		events := readJSONLines(t, filepath.Join(root, "events.jsonl"))
		return len(events) == 2 && events[1]["params"].(map[string]any)["git"] == nil
	})
}
