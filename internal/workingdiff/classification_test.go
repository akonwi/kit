package workingdiff

import (
	"errors"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/workspace"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func observeFixture(t *testing.T, d string) (*Service, protocol.WorkingTreePage) {
	t.Helper()
	ws := workspace.NewService()
	s, e := NewService(ws)
	if e != nil {
		t.Fatal(e)
	}
	ref := ws.Ref("session", d)
	p, e := s.Observe(t.Context(), "session", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID})
	if e != nil {
		t.Fatalf("%#v", e)
	}
	return s, p
}
func summaries(p protocol.WorkingTreePage) map[string]protocol.DiffFileSummary {
	m := map[string]protocol.DiffFileSummary{}
	for _, f := range p.Files {
		m[f.Path] = f
	}
	return m
}
func TestClassificationIndexAndKinds(t *testing.T) {
	d := fixture(t)
	git(t, d, "config", "core.filemode", "true")
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("staged\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git(t, d, "add", "a.txt")
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("live\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(d, "new.txt"), []byte("new\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(d, "ignored"), []byte("ignored"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(d, ".gitignore"), []byte("ignored\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(d, "binary"), []byte{'a', 0, 'b'}, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("a.txt", filepath.Join(d, "link")); e != nil {
		t.Fatal(e)
	}
	_, p := observeFixture(t, d)
	m := summaries(p)
	if m["a.txt"].Change != "modified" || m["new.txt"].Change != "added" {
		t.Fatalf("summaries=%+v", m)
	}
	if m["binary"].ContentState != "binary" || m["binary"].Reason != "nul" {
		t.Fatalf("binary=%+v", m["binary"])
	}
	if m["link"].Reason != "symlink" {
		t.Fatalf("link=%+v", m["link"])
	}
	if _, ok := m["ignored"]; ok {
		t.Fatal("ignored file included")
	}
}
func TestIntentToAddAndStagedReverted(t *testing.T) {
	d := fixture(t)
	if e := os.WriteFile(filepath.Join(d, "ita"), []byte("intent\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git(t, d, "add", "-N", "ita")
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("staged\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git(t, d, "add", "a.txt")
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("one\ntwo\n"), 0600); e != nil {
		t.Fatal(e)
	}
	_, p := observeFixture(t, d)
	m := summaries(p)
	if m["ita"].ContentState != "intent_to_add" || m["ita"].Change != "unknown" {
		t.Fatalf("ita=%+v", m["ita"])
	}
	if _, ok := m["a.txt"]; ok {
		t.Fatal("staged-reverted file reported changed")
	}
	if p.Observation.IndexSummary != "diverged" {
		t.Fatalf("index=%s", p.Observation.IndexSummary)
	}
}
func TestModeChangeAndLinkedWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	d := fixture(t)
	if e := os.Chmod(filepath.Join(d, "a.txt"), 0700); e != nil {
		t.Fatal(e)
	}
	_, p := observeFixture(t, d)
	if len(p.Files) != 1 || p.Files[0].Change != "mode_changed" {
		t.Fatalf("files=%+v", p.Files)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, d, "worktree", "add", "-q", "-b", "linked-test", linked)
	if e := os.WriteFile(filepath.Join(linked, "a.txt"), []byte("linked\n"), 0600); e != nil {
		t.Fatal(e)
	}
	_, lp := observeFixture(t, linked)
	if len(lp.Files) != 1 || lp.Files[0].Path != "a.txt" {
		t.Fatalf("linked=%+v", lp.Files)
	}
}
func TestCursorTamperAndSessionRemoval(t *testing.T) {
	d := fixture(t)
	for _, n := range []string{"b", "c"} {
		if e := os.WriteFile(filepath.Join(d, n), []byte(n), 0600); e != nil {
			t.Fatal(e)
		}
	}
	ws := workspace.NewService()
	s, e := NewService(ws)
	if e != nil {
		t.Fatal(e)
	}
	ref := ws.Ref("session", d)
	p, e := s.Observe(t.Context(), "session", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID, PageSize: 1})
	if e != nil {
		t.Fatal(e)
	}
	if p.NextCursor == "" {
		t.Fatal("missing cursor")
	}
	replacement := "A"
	if p.NextCursor[len(p.NextCursor)-1] == 'A' {
		replacement = "B"
	}
	bad := p.NextCursor[:len(p.NextCursor)-1] + replacement
	_, e = s.Observe(t.Context(), "session", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID, PageSize: 1, Cursor: bad})
	var de *Error
	if !errors.As(e, &de) || de.Code != StaleCursor {
		t.Fatalf("tamper=%v", e)
	}
	s.RemoveSession("session")
	_, e = s.Observe(t.Context(), "session", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID, PageSize: 1, Cursor: p.NextCursor})
	if !errors.As(e, &de) || de.Code != StaleCursor {
		t.Fatalf("removed=%v", e)
	}
}
