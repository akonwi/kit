package workingdiff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/workspace"
)

const (
	// binaryAttributes marks every file binary.
	binaryAttributes = "* -diff\n"
	// hideNewFile ignores new.txt.
	hideNewFile = "new.txt\n"
)

// changeFixture modifies a.txt and adds an untracked new.txt.
func changeFixture(t *testing.T, d string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(d, "a.txt"), []byte("one\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// plainGit runs git in d with the test process environment, as a user's git
// would run.
func plainGit(t *testing.T, d string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = d
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// requireUnaffectedObservation asserts that the service reports a.txt as a
// modified text file and new.txt as an added text file.
func requireUnaffectedObservation(t *testing.T, d string) {
	t.Helper()
	_, page := observeFixture(t, d)
	m := summaries(page)
	if len(m) != 2 || m["a.txt"].Change != "modified" || m["a.txt"].ContentState != "text" || m["new.txt"].Change != "added" || m["new.txt"].ContentState != "text" {
		t.Fatalf("summaries = %+v, want text a.txt modified and text new.txt added", m)
	}
}

// userHome points HOME and XDG_CONFIG_HOME at a fresh directory and returns
// its git configuration directory.
func userHome(t *testing.T) (home, gitConfigDir string) {
	t.Helper()
	home = t.TempDir()
	gitConfigDir = filepath.Join(home, ".config", "git")
	if err := os.MkdirAll(gitConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return home, gitConfigDir
}

// Not parallel: userHome changes HOME and XDG_CONFIG_HOME.
func TestObserveIgnoresTheUsersGlobalConfig(t *testing.T) {
	d := fixture(t)
	changeFixture(t, d)
	home, gitConfigDir := userHome(t)
	for _, path := range []string{filepath.Join(home, ".gitconfig"), filepath.Join(gitConfigDir, "config")} {
		if err := os.WriteFile(path, []byte("[malformed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := plainGit(t, d, "status", "--porcelain"); err == nil {
		t.Fatalf("plain git status = %q, want it to fail on the malformed global config", out)
	}
	requireUnaffectedObservation(t, d)
}

// Not parallel: userHome changes HOME and XDG_CONFIG_HOME.
func TestObserveIgnoresTheUsersDefaultAttributesAndIgnoreFiles(t *testing.T) {
	d := fixture(t)
	changeFixture(t, d)
	_, gitConfigDir := userHome(t)
	if err := os.WriteFile(filepath.Join(gitConfigDir, "attributes"), []byte(binaryAttributes), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitConfigDir, "ignore"), []byte(hideNewFile), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := plainGit(t, d, "check-attr", "diff", "--", "a.txt"); err != nil || out != "a.txt: diff: unset" {
		t.Fatalf("plain git check-attr = %q, %v; want the default attributes file applied", out, err)
	}
	if out, err := plainGit(t, d, "ls-files", "--others", "--exclude-standard"); err != nil || out != "" {
		t.Fatalf("plain git untracked files = %q, %v; want the default ignore file applied", out, err)
	}
	requireUnaffectedObservation(t, d)
}

// Not parallel: userHome changes HOME and XDG_CONFIG_HOME.
func TestObserveIgnoresAttributesAndIgnoreFilesNamedByRepositoryConfig(t *testing.T) {
	d := fixture(t)
	changeFixture(t, d)
	userHome(t)
	outside := t.TempDir()
	attributes := filepath.Join(outside, "attributes")
	ignore := filepath.Join(outside, "ignore")
	if err := os.WriteFile(attributes, []byte(binaryAttributes), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignore, []byte(hideNewFile), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, d, "config", "core.attributesFile", attributes)
	git(t, d, "config", "core.excludesFile", ignore)
	if out, err := plainGit(t, d, "check-attr", "diff", "--", "a.txt"); err != nil || out != "a.txt: diff: unset" {
		t.Fatalf("plain git check-attr = %q, %v; want the configured attributes file applied", out, err)
	}
	if out, err := plainGit(t, d, "ls-files", "--others", "--exclude-standard"); err != nil || out != "" {
		t.Fatalf("plain git untracked files = %q, %v; want the configured ignore file applied", out, err)
	}
	requireUnaffectedObservation(t, d)
}

// Captures a regression: each service created a kit-diff-git-* directory in
// TMPDIR that nothing removed. Not parallel: it changes TMPDIR.
func TestServiceLeavesNoTemporaryFiles(t *testing.T) {
	d := fixture(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	ws := workspace.NewService()
	s, err := NewService(ws)
	if err != nil {
		t.Fatal(err)
	}
	ref := ws.Ref("session", d)
	if _, err := s.Observe(t.Context(), "session", d, protocol.ObserveWorkingTreeInput{WorkspaceID: ref.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("TMPDIR entries after creating and using a service = %v, want none", entries)
	}
}
