package githubpr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParsePullRequest(t *testing.T) {
	good := `{"number":42,"url":"https://github.com/a/b/pull/42","headRefName":"main"}`
	if got := parse([]byte(good), "main"); got == nil || got.Number != 42 || got.URL != "https://github.com/a/b/pull/42" {
		t.Fatalf("parsed=%v", got)
	}
	for _, raw := range []string{`null`, `{}`, good + `{}`, strings.Replace(good, `42,`, `0,`, 1), strings.Replace(good, `42,`, `1.5,`, 1), strings.Replace(good, `42,`, `9007199254740992,`, 1), strings.Replace(good, "main", "other", 1), strings.Replace(good, "https://github.com/a/b/pull/42", "file:///tmp/a", 1)} {
		if got := parse([]byte(raw), "main"); got != nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, target := range []string{"javascript:alert(1)", "https://", "https://user:pass@example.com/pr/1", "https://example.com/\n", "https://example.com/a b", "https://example.com\\evil", strings.Repeat("a", 8193)} {
		if ValidURL(target) {
			t.Fatalf("accepted URL %q", target)
		}
	}
}
func fakeGH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
func TestLookupUsesExplicitBranchAndIgnoresRepositoryOverrides(t *testing.T) {
	cwd := t.TempDir()
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("KIT_GH_ARGS", args)
	t.Setenv("GH_REPO", "wrong/repo")
	t.Setenv("GIT_DIR", "/wrong")
	t.Setenv("GH_TOKEN", "test-token")
	fakeGH(t, `[ -z "$GH_REPO" ] && [ -z "$GIT_DIR" ] && [ "$GH_TOKEN" = test-token ] && [ "$GH_PROMPT_DISABLED" = 1 ] || exit 1
printf '%s\n' "$PWD" "$@" > "$KIT_GH_ARGS"
printf '%s' '{"number":12,"url":"https://github.com/a/b/pull/12","headRefName":"--branch"}'
`)
	got := Lookup(t.Context(), cwd, "--branch")
	if got == nil || got.Number != 12 {
		t.Fatalf("lookup=%v", got)
	}
	raw, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	expected := canonical + "\npr\nview\n--json\nnumber,url,headRefName\n--\n--branch\n"
	// Shell PWD may retain the original cwd spelling on platforms with /tmp symlinks.
	if string(raw) != expected && string(raw) != strings.Replace(expected, canonical, cwd, 1) {
		t.Fatalf("args=%q", raw)
	}
}
func TestLookupBoundsOutputAndCancellation(t *testing.T) {
	t.Run("output", func(t *testing.T) {
		fakeGH(t, `printf '%65536s' ' '
printf '%s' '{"number":42,"url":"https://github.com/a/b/pull/42","headRefName":"main"}'`)
		if got := Lookup(t.Context(), t.TempDir(), "main"); got != nil {
			t.Fatal(got)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		fakeGH(t, "sleep 30\n")
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		if got := Lookup(ctx, t.TempDir(), "main"); got != nil {
			t.Fatal(got)
		}
		if time.Since(start) > time.Second {
			t.Fatal("lookup ignored cancellation")
		}
	})
	t.Run("failure", func(t *testing.T) {
		fakeGH(t, "exit 1\n")
		if got := Lookup(t.Context(), t.TempDir(), "main"); got != nil {
			t.Fatal(got)
		}
	})
}
