package workingdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/workspace"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, e := c.CombinedOutput(); e != nil {
		t.Fatalf("git %v: %v: %s", args, e, out)
	}
}
func fixture(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	git(t, d, "init", "-q")
	git(t, d, "config", "user.name", "Test")
	git(t, d, "config", "user.email", "test@example.com")
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("one\ntwo\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git(t, d, "add", "a.txt")
	git(t, d, "commit", "-qm", "base")
	return d
}
func TestObserveBulkTrackedRepositoryWithoutRepeatingWorkspaceNameBudget(t *testing.T) {
	d := t.TempDir()
	git(t, d, "init", "-q")
	git(t, d, "config", "user.name", "Test")
	git(t, d, "config", "user.email", "test@example.com")
	for index := range 300 {
		name := filepath.Join(d, fmt.Sprintf("file-%03d.txt", index))
		if err := os.WriteFile(name, []byte("base\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(d, "large.bin"), bytes.Repeat([]byte("a"), protocol.MaxDiffFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "base")
	if err := os.WriteFile(filepath.Join(d, "file-299.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := workspace.NewService()
	service, err := NewService(ws)
	if err != nil {
		t.Fatal(err)
	}
	ref := ws.Ref("session_test", d)
	page, err := service.Observe(t.Context(), "session_test", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Files) != 1 || page.Files[0].Path != "file-299.txt" {
		t.Fatalf("files = %+v", page.Files)
	}
}

func TestReadFileDiffPaginatesCompleteSemanticHunks(t *testing.T) {
	d := t.TempDir()
	git(t, d, "init", "-q")
	git(t, d, "config", "user.name", "Test")
	git(t, d, "config", "user.email", "test@example.com")
	oldContent := strings.Repeat("old\n", 30)
	if err := os.WriteFile(filepath.Join(d, "a.txt"), []byte(oldContent), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, d, "add", "a.txt")
	git(t, d, "commit", "-qm", "base")
	if err := os.WriteFile(filepath.Join(d, "a.txt"), []byte(strings.Repeat("new\n", 30)), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := workspace.NewService()
	service, err := NewService(ws)
	if err != nil {
		t.Fatal(err)
	}
	ref := ws.Ref("session_test", d)
	observation, err := service.Observe(t.Context(), "session_test", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID})
	if err != nil || len(observation.Files) != 1 {
		t.Fatalf("observation = %+v, %v", observation, err)
	}
	cursor, lines, pages := "", 0, 0
	for {
		page, readErr := service.ReadFile(t.Context(), "session_test", d, protocol.ReadFileDiffInput{
			TargetID: observation.Observation.Target.ID, TargetRevision: observation.Observation.Revision,
			Path: "a.txt", ExpectedFileRevision: observation.Files[0].FileRevision,
			PageSize: 5, MaxHunks: 2, Cursor: cursor,
		})
		if readErr != nil {
			t.Fatalf("page %d cursor bytes %d: %v", pages+1, len(cursor), readErr)
		}
		pages++
		for _, hunk := range page.Hunks {
			lines += len(hunk.Lines)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if pages < 2 || lines != 60 {
		t.Fatalf("pages = %d, lines = %d", pages, lines)
	}
}

func TestObserveAndReadSemanticDiff(t *testing.T) {
	d := fixture(t)
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("one\nchanged\n"), 0600); e != nil {
		t.Fatal(e)
	}
	ws := workspace.NewService()
	s, e := NewService(ws)
	if e != nil {
		t.Fatalf("%#v", e)
	}
	ref := ws.Ref("session_test", d)
	page, e := s.Observe(t.Context(), "session_test", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID})
	if e != nil {
		t.Fatalf("%#v", e)
	}
	if len(page.Files) != 1 || page.Files[0].Path != "a.txt" || page.Files[0].Change != "modified" {
		t.Fatalf("files=%+v", page.Files)
	}
	diff, e := s.ReadFile(t.Context(), "session_test", d, protocol.ReadFileDiffInput{TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "a.txt", ExpectedFileRevision: page.Files[0].FileRevision})
	if e != nil {
		t.Fatalf("%#v", e)
	}
	if len(diff.Hunks) != 1 {
		t.Fatalf("hunks=%+v", diff.Hunks)
	}
	var kinds []string
	for _, l := range diff.Hunks[0].Lines {
		kinds = append(kinds, l.Kind)
	}
	if strings.Join(kinds, ",") != "context,deletion,addition" {
		t.Fatalf("kinds=%v", kinds)
	}
	if diff.Hunks[0].Lines[1].OldLine == nil || diff.Hunks[0].Lines[1].NewLine != nil {
		t.Fatal("deletion coordinates")
	}
	oldEvidence, err := s.ReadLineRange(t.Context(), "session_test", d, LineRangeInput{
		TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "a.txt",
		FileRevision: page.Files[0].FileRevision, Side: "old", StartLine: 2, EndLine: 2,
	})
	if err != nil || oldEvidence.Content != "two" {
		t.Fatalf("old evidence = %+v, %v", oldEvidence, err)
	}
	newEvidence, err := s.ReadLineRange(t.Context(), "session_test", d, LineRangeInput{
		TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "a.txt",
		FileRevision: page.Files[0].FileRevision, Side: "new", StartLine: 2, EndLine: 2,
	})
	if err != nil || newEvidence.Content != "changed" {
		t.Fatalf("new evidence = %+v, %v", newEvidence, err)
	}
}
func TestStaleFileAndHostileHelpersNotExecuted(t *testing.T) {
	d := fixture(t)
	marker := filepath.Join(t.TempDir(), "marker")
	script := filepath.Join(t.TempDir(), "hostile.sh")
	if e := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\ncat\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("*.txt filter=evil diff=evil\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git(t, d, "add", ".gitattributes")
	git(t, d, "commit", "-qm", "attrs")
	git(t, d, "config", "core.fsmonitor", script)
	git(t, d, "config", "filter.evil.clean", script)
	git(t, d, "config", "diff.evil.textconv", script)
	git(t, d, "config", "core.pager", script)
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("changed\n"), 0600); e != nil {
		t.Fatal(e)
	}
	ws := workspace.NewService()
	s, e := NewService(ws)
	if e != nil {
		t.Fatalf("%#v", e)
	}
	ref := ws.Ref("session_test", d)
	page, e := s.Observe(t.Context(), "session_test", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID})
	if e != nil {
		t.Fatalf("%#v", e)
	}
	if _, e = os.Stat(marker); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("hostile helper executed")
	}
	if len(page.Files) != 1 || page.Files[0].ContentState != "unsupported_transform" || page.Files[0].Change != "unknown" || page.Files[0].FileRevision != "" {
		t.Fatalf("files=%+v", page.Files)
	}
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("again\n"), 0600); e != nil {
		t.Fatal(e)
	}
	_, e = s.ReadFile(context.Background(), "session_test", d, protocol.ReadFileDiffInput{TargetID: page.Observation.Target.ID, TargetRevision: page.Observation.Revision, Path: "a.txt"})
	var de *Error
	if !errors.As(e, &de) || de.Code != StaleFile {
		t.Fatalf("error=%v", e)
	}
}
func TestUnbornAndSparseRejection(t *testing.T) {
	d := t.TempDir()
	git(t, d, "init", "-q")
	if e := os.WriteFile(filepath.Join(d, "new.txt"), []byte("new\n"), 0600); e != nil {
		t.Fatal(e)
	}
	ws := workspace.NewService()
	s, e := NewService(ws)
	if e != nil {
		t.Fatalf("%#v", e)
	}
	ref := ws.Ref("s", d)
	page, e := s.Observe(t.Context(), "s", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID})
	if e != nil {
		t.Fatalf("%#v", e)
	}
	if page.Observation.Head.State != "unborn" || len(page.Files) != 1 || page.Files[0].Change != "added" {
		t.Fatalf("page=%+v", page)
	}
	git(t, d, "config", "core.sparseCheckout", "true")
	_, e = s.Observe(t.Context(), "s", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID})
	var de *Error
	if !errors.As(e, &de) || de.Code != UnsupportedRepository {
		t.Fatalf("error=%v", e)
	}
}
