package workingdiff

import (
	"context"
	"errors"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNestedRepositoryAndLineLimitAreIncomplete(t *testing.T) {
	d := fixture(t)
	nested := filepath.Join(d, "inner")
	git(t, d, "init", "-q", nested)
	_, p := observeFixture(t, d)
	f := summaries(p)["inner"]
	if f.Reason != "nested_repository" || f.Change != "unknown" || p.Observation.Complete {
		t.Fatalf("nested=%+v complete=%v", f, p.Observation.Complete)
	}
	_ = os.RemoveAll(nested)
	var b strings.Builder
	for i := 0; i < protocol.MaxDiffFileLines+1; i++ {
		b.WriteString("x\n")
	}
	if e := os.WriteFile(filepath.Join(d, "many"), []byte(b.String()), 0600); e != nil {
		t.Fatal(e)
	}
	_, p = observeFixture(t, d)
	f = summaries(p)["many"]
	if f.Reason != "line_count" || p.Observation.Complete {
		t.Fatalf("many=%+v complete=%v", f, p.Observation.Complete)
	}
}
func TestGitBooleanSparseAndAuthorityReplacement(t *testing.T) {
	d := fixture(t)
	git(t, d, "config", "core.sparseCheckout", "yes")
	if e := observeError(t, d); e == nil {
		t.Fatal("accepted Git true spelling")
	}
	git(t, d, "config", "core.sparseCheckout", "false")
	ws, s, p := serviceObservation(t, d)
	old := filepath.Join(d, ".git-old")
	if e := os.Rename(filepath.Join(d, ".git"), old); e != nil {
		t.Fatal(e)
	}
	copyTree(t, old, filepath.Join(d, ".git"))
	_, e := s.ReadFile(t.Context(), "session", d, protocol.ReadFileDiffInput{TargetID: p.Observation.Target.ID, TargetRevision: p.Observation.Revision, Path: "a.txt"})
	if e == nil {
		t.Fatal("repository authority replacement accepted")
	}
	_ = ws
}
func serviceObservation(t *testing.T, d string) (*workspace.Service, *Service, protocol.WorkingTreePage) {
	t.Helper()
	w := workspace.NewService()
	s, e := NewService(w)
	if e != nil {
		t.Fatal(e)
	}
	r := w.Ref("session", d)
	if e = os.WriteFile(filepath.Join(d, "a.txt"), []byte("changed\n"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := s.Observe(t.Context(), "session", d, protocol.ObserveWorkingTreeInput{WorkspaceID: r.WorkspaceID})
	if e != nil {
		t.Fatal(e)
	}
	return w, s, p
}
func TestUnsafeControlAndCorruptHeadFailClosed(t *testing.T) {
	d := fixture(t)
	head := filepath.Join(d, ".git", "HEAD")
	if e := os.Chmod(head, 0666); e != nil {
		t.Fatal(e)
	}
	if e := observeError(t, d); e == nil {
		t.Fatal("accepted writable HEAD")
	}
	if e := os.Chmod(head, 0644); e != nil {
		t.Fatal(e)
	}
	ref := stringsTrim(runGitOutput(t, d, "symbolic-ref", "HEAD"))
	if e := os.WriteFile(filepath.Join(d, ".git", filepath.FromSlash(ref)), []byte(strings.Repeat("a", 40)+"\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := observeError(t, d); e == nil {
		t.Fatal("corrupt HEAD treated as unborn")
	}
}

func TestUnrelatedConfigDoesNotStale(t *testing.T) {
	d := fixture(t)
	_, s, p := serviceObservation(t, d)
	git(t, d, "config", "user.name", "Changed")
	_, e := s.ReadFile(t.Context(), "session", d, protocol.ReadFileDiffInput{TargetID: p.Observation.Target.ID, TargetRevision: p.Observation.Revision, Path: "a.txt"})
	if e != nil {
		t.Fatalf("unrelated config staled target: %v", e)
	}
}

func TestObjectSymlinkEscapingAuthorityIsRejected(t *testing.T) {
	d := fixture(t)
	oid := stringsTrim(runGitOutput(t, d, "rev-parse", "HEAD:a.txt"))
	loose := filepath.Join(d, ".git", "objects", oid[:2], oid[2:])
	outside := filepath.Join(t.TempDir(), "object")
	data, e := os.ReadFile(loose)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(outside, data, 0444); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(loose); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, loose); e != nil {
		t.Fatal(e)
	}
	if e = observeError(t, d); e == nil {
		t.Fatal("accepted object-store symlink")
	}
}

func TestObservationCancellationRemainsCancellation(t *testing.T) {
	d := fixture(t)
	w := workspace.NewService()
	s, e := NewService(w)
	if e != nil {
		t.Fatal(e)
	}
	r := w.Ref("s", d)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = s.Observe(ctx, "s", d, protocol.ObserveWorkingTreeInput{WorkspaceID: r.WorkspaceID})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("error=%v", e)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	if e := os.CopyFS(dst, os.DirFS(src)); e != nil {
		t.Fatal(e)
	}
}
