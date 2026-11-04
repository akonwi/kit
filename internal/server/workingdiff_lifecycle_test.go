package server

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemonctl"
	kitworkingdiff "github.com/akonwi/kit/internal/workingdiff"
	kitworkspace "github.com/akonwi/kit/internal/workspace"
)

// newTestDiffService returns a working-tree diff service that is closed when
// the test ends.
func newTestDiffService(t *testing.T, workspaces *kitworkspace.Service) *kitworkingdiff.Service {
	t.Helper()
	diffs, err := kitworkingdiff.NewService(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := diffs.Close(context.Background()); err != nil {
			t.Errorf("close diff service: %v", err)
		}
	})
	return diffs
}

// isolateTempDir points the process temporary directory at a fresh directory
// and returns it. It uses t.Setenv, so callers must not run in parallel.
func isolateTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func diffGitHomes(t *testing.T, tmp string) []string {
	t.Helper()
	homes, err := filepath.Glob(filepath.Join(tmp, "kit-diff-git-*"))
	if err != nil {
		t.Fatal(err)
	}
	return homes
}

// Not parallel: isolateTempDir changes TMPDIR for the process.
func TestRunRemovesTheDiffGitHomeOnShutdown(t *testing.T) {
	tmp := isolateTempDir(t)
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, RunOptions{Paths: paths, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()

	client := daemonctl.NewClient(paths)
	probeContext, probeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer probeCancel()
	for {
		_, _, err := client.Probe(probeContext)
		if err == nil {
			break
		}
		select {
		case <-probeContext.Done():
			t.Fatalf("daemon did not become ready: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if homes := diffGitHomes(t, tmp); len(homes) != 1 {
		t.Fatalf("diff git homes while serving = %q, want one", homes)
	}

	stopContext, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := daemonctl.NewManager(paths).Stop(stopContext); err != nil {
		t.Fatalf("Manager.Stop() error = %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-stopContext.Done():
		t.Fatal("daemon did not stop")
	}
	if homes := diffGitHomes(t, tmp); len(homes) != 0 {
		t.Fatalf("diff git homes after shutdown = %q, want none", homes)
	}
}

// Not parallel: isolateTempDir changes TMPDIR for the process.
func TestRunRemovesTheDiffGitHomeWhenStartupFails(t *testing.T) {
	tmp := isolateTempDir(t)
	// The listen address is checked after the diff service is created.
	err := Run(context.Background(), RunOptions{
		Paths: apphome.FromHome(filepath.Join(t.TempDir(), "kit")), Listen: "192.168.1.10:7777",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err == nil || !strings.Contains(err.Error(), "loopback address or 0.0.0.0/::") {
		t.Fatalf("Run() error = %v, want the listen address rule", err)
	}
	if homes := diffGitHomes(t, tmp); len(homes) != 0 {
		t.Fatalf("diff git homes after failed startup = %q, want none", homes)
	}
}
