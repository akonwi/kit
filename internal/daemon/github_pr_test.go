package daemon

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
	"github.com/akonwi/kit/internal/protocol"
)

func TestGitHubFooterLookupIsNonblockingCachedAndBranchScoped(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("requires git")
	}
	fixture := t.TempDir()
	t.Setenv("KIT_GH_FIXTURE", fixture)
	t.Setenv("PATH", fixture+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
printf '%s\n' "$6" >> "$KIT_GH_FIXTURE/calls"
if [ "$6" = main ]; then
    touch "$KIT_GH_FIXTURE/started"
    while [ ! -f "$KIT_GH_FIXTURE/release" ]; do sleep 0.01; done
    printf '%s' '{"number":42,"url":"https://github.com/a/b/pull/42","headRefName":"main"}'
else
    printf '%s' '{"number":7,"url":"https://github.com/a/b/pull/7","headRefName":"other"}'
fi
`
	if err := os.WriteFile(filepath.Join(fixture, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitPath, append([]string{"-C", cwd, "-c", "user.name=Kit test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	git("init", "-b", "main")
	git("commit", "--allow-empty", "-m", "initial")
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
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
	created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: cwd, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	// The held gh command cannot block the local Git/status response.
	requestCtx, requestCancel := context.WithTimeout(t.Context(), time.Second)
	defer requestCancel()
	first, err := client.GetSessionVCSStatus(requestCtx, created.ID)
	if err != nil || first.Status == nil || first.Status.Head.Name != "main" || first.Status.PullRequest != nil {
		t.Fatalf("initial VCS=%+v err=%v", first, err)
	}
	eventually(t, func() bool { _, err := os.Stat(filepath.Join(fixture, "started")); return err == nil })
	git("checkout", "-b", "other")
	if err := os.WriteFile(filepath.Join(fixture, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	eventuallyObservedVCS(t, func() bool {
		status, err := client.GetSessionVCSStatus(t.Context(), created.ID)
		return err == nil && status.Status != nil && status.Status.PullRequest != nil && status.Status.PullRequest.Number == 7
	})
	if err := os.WriteFile(filepath.Join(fixture, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		status, err := client.GetSessionVCSStatus(t.Context(), created.ID)
		if err != nil || status.Status == nil || status.Status.Head.Name != "other" || status.Status.PullRequest == nil || status.Status.PullRequest.Number != 7 || status.Status.PullRequest.URL != "https://github.com/a/b/pull/7" {
			t.Fatalf("current PR changed: %+v %v", status, err)
		}
	}
	calls, err := os.ReadFile(filepath.Join(fixture, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "main\nother\n" {
		t.Fatalf("lookup coalescing=%q", calls)
	}
	git("checkout", "--detach")
	eventuallyObservedVCS(t, func() bool {
		detached, err := client.GetSessionVCSStatus(t.Context(), created.ID)
		return err == nil && detached.Status != nil && detached.Status.Head.Kind == protocol.VCSHeadDetached && detached.Status.PullRequest == nil
	})
	// A return to the original branch may use its own completed cache, never the other branch's.
	git("checkout", "main")
	eventuallyObservedVCS(t, func() bool {
		status, err := client.GetSessionVCSStatus(t.Context(), created.ID)
		return err == nil && status.Status != nil && status.Status.PullRequest != nil && status.Status.PullRequest.Number == 42
	})
	calls, err = os.ReadFile(filepath.Join(fixture, "calls"))
	if err != nil || strings.Count(string(calls), "\n") != 2 {
		t.Fatalf("cache return=%q %v", calls, err)
	}
}

func eventuallyObservedVCS(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("VCS observation did not settle")
}
