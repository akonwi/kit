package workingdiff

import (
	"errors"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/workspace"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func observeError(t *testing.T, d string) error {
	t.Helper()
	ws := workspace.NewService()
	s, e := NewService(ws)
	if e != nil {
		t.Fatal(e)
	}
	ref := ws.Ref("s", d)
	_, e = s.Observe(t.Context(), "s", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID})
	return e
}
func TestRepositoryAuthorityRejectsIncludesAlternatesAndArbitraryGitfile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{{"include", func(t *testing.T, d string) {
		f, e := os.OpenFile(filepath.Join(d, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = f.WriteString("\n[include]\npath = /tmp/hostile\n")
		_ = f.Close()
	}}, {"alternates", func(t *testing.T, d string) {
		p := filepath.Join(d, ".git", "objects", "info", "alternates")
		if e := os.WriteFile(p, []byte("/tmp/objects\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}}, {"arbitrary_gitfile", func(t *testing.T, d string) {
		other := fixture(t)
		if e := os.RemoveAll(filepath.Join(d, ".git")); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(d, ".git"), []byte("gitdir: "+filepath.Join(other, ".git")+"\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}}} {
		t.Run(tc.name, func(t *testing.T) {
			d := fixture(t)
			tc.mutate(t, d)
			e := observeError(t, d)
			var de *Error
			if !errors.As(e, &de) || de.Code != UnsupportedRepository {
				t.Fatalf("error=%#v", e)
			}
		})
	}
}
func TestConflictAssumeUnchangedAndGitlinkClassification(t *testing.T) {
	d := fixture(t)
	git(t, d, "checkout", "-qb", "side")
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("side\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git(t, d, "commit", "-qam", "side")
	git(t, d, "checkout", "-q", "master")
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("main\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git(t, d, "commit", "-qam", "main")
	cmd := exec.Command("git", "merge", "side")
	cmd.Dir = d
	_ = cmd.Run()
	_, p := observeFixture(t, d)
	m := summaries(p)
	if m["a.txt"].ContentState != "conflict" || p.Observation.IndexSummary != "conflicted" {
		t.Fatalf("conflict=%+v observation=%+v", m, p.Observation)
	}
	git(t, d, "merge", "--abort")
	git(t, d, "update-index", "--assume-unchanged", "a.txt")
	if e := os.WriteFile(filepath.Join(d, "a.txt"), []byte("assumed\n"), 0600); e != nil {
		t.Fatal(e)
	}
	_, p = observeFixture(t, d)
	if summaries(p)["a.txt"].Change != "modified" {
		t.Fatalf("assume-unchanged hidden: %+v", p.Files)
	}
	git(t, d, "update-index", "--no-assume-unchanged", "a.txt")
	git(t, d, "checkout", "--", "a.txt")
	head := stringsTrim(runGitOutput(t, d, "rev-parse", "HEAD"))
	git(t, d, "update-index", "--add", "--cacheinfo", "160000,"+head+",sub")
	_, p = observeFixture(t, d)
	if summaries(p)["sub"].Reason != "submodule" || p.Observation.Complete {
		t.Fatalf("submodule=%+v complete=%v", summaries(p)["sub"], p.Observation.Complete)
	}
}
func runGitOutput(t *testing.T, d string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = d
	b, e := c.Output()
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func stringsTrim(v string) string {
	for len(v) > 0 && (v[len(v)-1] == '\n' || v[len(v)-1] == '\r') {
		v = v[:len(v)-1]
	}
	return v
}
