package vcs

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParseStatus(t *testing.T) {
	t.Parallel()
	const oid = "0123456789abcdef0123456789abcdef01234567"
	for _, test := range []struct {
		name  string
		input string
		head  Head
		dirty bool
		valid bool
	}{
		{name: "clean branch", input: "# branch.oid " + oid + "\n# branch.head main\n", head: Head{Kind: HeadBranch, Name: "main"}, valid: true},
		{name: "dirty branch", input: "# branch.oid " + oid + "\n# branch.head feature/status\n1 .M N... 100644 100644 100644 " + oid + " " + oid + " file.go\n", head: Head{Kind: HeadBranch, Name: "feature/status"}, dirty: true, valid: true},
		{name: "untracked", input: "# branch.oid " + oid + "\n# branch.head main\n? notes.txt\n", head: Head{Kind: HeadBranch, Name: "main"}, dirty: true, valid: true},
		{name: "unborn", input: "# branch.oid (initial)\n# branch.head main\n", head: Head{Kind: HeadUnborn, Name: "main"}, valid: true},
		{name: "unborn literal detached branch", input: "# branch.oid (initial)\n# branch.head (detached)\n", head: Head{Kind: HeadUnborn, Name: "(detached)"}, valid: true},
		{name: "detached", input: "# branch.oid " + oid + "\n# branch.head (detached)\n", head: Head{Kind: HeadDetached, OID: oid}, valid: true},
		{name: "ignored record", input: "# branch.oid " + oid + "\n# branch.head main\n! ignored.txt\n", head: Head{Kind: HeadBranch, Name: "main"}, valid: true},
		{name: "missing head", input: "# branch.oid " + oid + "\n", valid: false},
		{name: "missing oid", input: "# branch.head main\n", valid: false},
		{name: "duplicate head", input: "# branch.oid " + oid + "\n# branch.head main\n# branch.head other\n", valid: false},
		{name: "duplicate oid", input: "# branch.oid " + oid + "\n# branch.oid " + oid + "\n# branch.head main\n", valid: false},
		{name: "malformed record", input: "# branch.oid " + oid + "\n# branch.head main\narbitrary output\n", valid: false},
		{name: "bad detached oid", input: "# branch.oid nope\n# branch.head (detached)\n", valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, valid := parseStatus("/repo", test.input)
			if valid != test.valid {
				t.Fatalf("valid = %t, want %t: %+v", valid, test.valid, status)
			}
			if !valid {
				return
			}
			if status.Root != "/repo" || status.Head != test.head || status.Dirty != test.dirty {
				t.Fatalf("status = %+v, want head %+v dirty %t", status, test.head, test.dirty)
			}
		})
	}
}

func TestProbeReportsBranchAndDirtyState(t *testing.T) {
	repository := t.TempDir()
	runGitTest(t, repository, "init", "-b", "main")

	status, err := Probe(t.Context(), repository)
	if err != nil {
		t.Fatalf("Probe(clean unborn) error = %v", err)
	}
	if status == nil || status.Head != (Head{Kind: HeadUnborn, Name: "main"}) || status.Dirty {
		t.Fatalf("clean unborn status = %+v", status)
	}

	if err := os.WriteFile(filepath.Join(repository, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = Probe(t.Context(), repository)
	if err != nil {
		t.Fatalf("Probe(dirty) error = %v", err)
	}
	if status == nil || !status.Dirty {
		t.Fatalf("dirty status = %+v", status)
	}
}

func TestProbeTracksCleanIgnoredModifiedAndDetachedStates(t *testing.T) {
	repository := t.TempDir()
	runGitTest(t, repository, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repository, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "tracked.txt"), []byte("clean\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repository, "add", ".gitignore", "tracked.txt")
	runGitTest(t, repository, "-c", "user.name=Kit Test", "-c", "user.email=kit@example.com", "commit", "-m", "initial")

	if err := os.WriteFile(filepath.Join(repository, "ignored.txt"), []byte("ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Probe(t.Context(), repository)
	if err != nil || status == nil || status.Head != (Head{Kind: HeadBranch, Name: "main"}) || status.Dirty {
		t.Fatalf("clean status with ignored file = %+v, %v", status, err)
	}

	if err := os.WriteFile(filepath.Join(repository, "tracked.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = Probe(t.Context(), repository)
	if err != nil || status == nil || !status.Dirty {
		t.Fatalf("modified status = %+v, %v", status, err)
	}
	runGitTest(t, repository, "restore", "tracked.txt")
	runGitTest(t, repository, "checkout", "--detach", "HEAD")
	status, err = Probe(t.Context(), repository)
	if err != nil || status == nil || status.Head.Kind != HeadDetached || len(status.Head.OID) != 40 || status.Dirty {
		t.Fatalf("detached status = %+v, %v", status, err)
	}
	runGitTest(t, repository, "switch", "main")
	runGitTest(t, repository, "branch", "(detached)")
	runGitTest(t, repository, "switch", "(detached)")
	status, err = Probe(t.Context(), repository)
	if err != nil || status == nil || status.Head != (Head{Kind: HeadBranch, Name: "(detached)"}) || status.Dirty {
		t.Fatalf("literal detached branch status = %+v, %v", status, err)
	}
}

func TestProbeSupportsLinkedWorktrees(t *testing.T) {
	repository := t.TempDir()
	runGitTest(t, repository, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repository, "tracked.txt"), []byte("clean\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repository, "add", "tracked.txt")
	runGitTest(t, repository, "-c", "user.name=Kit Test", "-c", "user.email=kit@example.com", "commit", "-m", "initial")
	worktree := filepath.Join(t.TempDir(), "linked")
	runGitTest(t, repository, "worktree", "add", "-b", "linked", worktree, "HEAD")
	status, err := Probe(t.Context(), worktree)
	resolvedWorktree, resolveErr := filepath.EvalSymlinks(worktree)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || status == nil || (status.Root != worktree && status.Root != resolvedWorktree) || status.Head != (Head{Kind: HeadBranch, Name: "linked"}) || status.Dirty {
		t.Fatalf("linked worktree status = %+v, %v", status, err)
	}
}

func TestProbePreservesRepositoryRootTrailingSpace(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repository ")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repository, "init", "-b", "main")
	status, err := Probe(t.Context(), repository)
	resolved, resolveErr := filepath.EvalSymlinks(repository)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || status == nil || (status.Root != repository && status.Root != resolved) {
		t.Fatalf("trailing-space root status = %+v, %v", status, err)
	}
}

func TestProbeIgnoresInheritedRepositoryOverrides(t *testing.T) {
	repository := t.TempDir()
	other := t.TempDir()
	runGitTest(t, repository, "init", "-b", "expected")
	runGitTest(t, other, "init", "-b", "wrong")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	status, err := Probe(t.Context(), repository)
	if err != nil || status == nil || status.Head.Name != "expected" {
		t.Fatalf("status with inherited Git overrides = %+v, %v", status, err)
	}
}

func TestProbeDisablesRepositoryFSMonitor(t *testing.T) {
	repository := t.TempDir()
	runGitTest(t, repository, "init", "-b", "main")
	marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
	hook := filepath.Join(t.TempDir(), "fsmonitor.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch \""+marker+"\"\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repository, "config", "core.fsmonitor", hook)
	status, err := Probe(t.Context(), repository)
	if err != nil || status == nil {
		t.Fatalf("status with configured fsmonitor = %+v, %v", status, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository fsmonitor executed during probe: %v", err)
	}
}

func TestRunGitKillsDescendantsAtDeadline(t *testing.T) {
	bin := t.TempDir()
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	script := "#!/bin/sh\nsleep 30 &\necho $! > \"$KIT_VCS_CHILD_PID\"\nwait\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KIT_VCS_CHILD_PID", pidPath)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, _, err := runGit(ctx, t.TempDir(), 1024, "status")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runGit() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("runGit() returned after %v", elapsed)
	}
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatalf("git descendant %d survived cancellation", pid)
	}
}

func TestProbeReturnsNilOutsideGitRepository(t *testing.T) {
	t.Parallel()
	status, err := Probe(context.Background(), t.TempDir())
	if err != nil || status != nil {
		t.Fatalf("Probe(non-repository) = %+v, %v", status, err)
	}
}

func TestProbeHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Probe(ctx, t.TempDir()); err == nil {
		t.Fatal("Probe() ignored caller cancellation")
	}
}

func TestCappedBufferBoundsOutput(t *testing.T) {
	t.Parallel()
	buffer := cappedBuffer{limit: 3}
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 {
		t.Fatalf("Write() = %d, %v", written, err)
	}
	if buffer.String() != "abc" || !buffer.overflow {
		t.Fatalf("buffer = %q overflow %t", buffer.String(), buffer.overflow)
	}
}

func runGitTest(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}

func TestCappedBufferCopyCannotBypassLimit(t *testing.T) {
	buffer := cappedBuffer{limit: 3}
	// Hide strings.Reader.WriteTo so io.Copy exercises destination fast paths.
	source := struct{ io.Reader }{strings.NewReader("abcdef")}
	n, err := io.Copy(&buffer, source)
	if err != nil || n != 6 || buffer.String() != "abc" || !buffer.overflow {
		t.Fatalf("copy=%d,%v buffer=%q overflow=%t", n, err, buffer.String(), buffer.overflow)
	}
}
