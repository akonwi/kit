// Package vcs reads bounded, renderer-neutral version-control status.
package vcs

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	probeTimeout       = 2 * time.Second
	gitWaitDelay       = 250 * time.Millisecond
	maxRootOutputBytes = 16 << 10
	maxStatusBytes     = 1 << 20
)

// HeadKind identifies the checked-out Git head shape.
type HeadKind string

const (
	// HeadBranch identifies a normal branch checkout.
	HeadBranch HeadKind = "branch"
	// HeadDetached identifies a detached commit checkout.
	HeadDetached HeadKind = "detached"
	// HeadUnborn identifies a branch without a first commit.
	HeadUnborn HeadKind = "unborn"
)

// Head describes the checked-out Git head.
type Head struct {
	Kind HeadKind
	Name string
	OID  string
}

// Status is a point-in-time Git status for one workspace.
type Status struct {
	Root  string
	Head  Head
	Dirty bool
}

// Probe returns Git status for cwd. A nil status with a nil error means cwd is
// not a usable Git worktree or Git could not be inspected safely. Caller
// cancellation remains an error so transport shutdown can stop promptly.
func Probe(ctx context.Context, cwd string) (*Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	probeContext, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	rootOutput, _, rootOverflow, err := runGit(probeContext, cwd, maxRootOutputBytes, "rev-parse", "--show-toplevel")
	if err != nil || rootOverflow {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	root, ok := singleOutputLine(rootOutput)
	if !ok || !filepath.IsAbs(root) || !validText(root, maxRootOutputBytes) {
		return nil, nil
	}

	statusOutput, _, statusOverflow, err := runGit(probeContext, cwd, maxStatusBytes, "status", "--porcelain=2", "--branch", "--untracked-files=normal")
	if err != nil || statusOverflow {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	status, ok := parseStatus(root, statusOutput)
	if !ok {
		return nil, nil
	}
	if status.Head.Kind == HeadDetached {
		symbolicOutput, _, overflow, symbolicErr := runGit(probeContext, cwd, maxRootOutputBytes, "symbolic-ref", "--quiet", "--short", "HEAD")
		switch {
		case symbolicErr == nil && !overflow:
			branch, valid := singleOutputLine(symbolicOutput)
			if !valid || !validText(branch, 4096) {
				return nil, nil
			}
			status.Head = Head{Kind: HeadBranch, Name: branch}
		case isExitCode(symbolicErr, 1) && !overflow:
		default:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, nil
		}
	}
	return &status, nil
}

func runGit(ctx context.Context, cwd string, limit int, arguments ...string) (string, string, bool, error) {
	gitArguments := []string{"-c", "core.fsmonitor=false", "-C", cwd}
	gitArguments = append(gitArguments, arguments...)
	command := exec.CommandContext(ctx, "git", gitArguments...)
	command.Env = gitEnvironment()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = gitWaitDelay
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	var stdout, stderr cappedBuffer
	stdout.limit = limit
	stderr.limit = 16 << 10
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		return stdout.String(), stderr.String(), stdout.overflow || stderr.overflow, ctx.Err()
	}
	return stdout.String(), stderr.String(), stdout.overflow || stderr.overflow, err
}

func gitEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if blockedGitEnvironmentKey(key) || key == "LC_ALL" {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
}

func blockedGitEnvironmentKey(key string) bool {
	return strings.HasPrefix(key, "GIT_")
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

// Keep bytes.Buffer unexported: promoting ReadFrom would bypass Write
// limits when os/exec copies subprocess output with io.Copy.
func (b *cappedBuffer) String() string { return b.buffer.String() }

func (b *cappedBuffer) Write(value []byte) (int, error) {
	length := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		_, _ = b.buffer.Write(value[:min(remaining, length)])
	}
	if length > remaining {
		b.overflow = true
	}
	return length, nil
}

func parseStatus(root, output string) (Status, bool) {
	var branch, oid string
	var branchSeen, oidSeen bool
	dirty := false
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			if branchSeen {
				return Status{}, false
			}
			branchSeen = true
			branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.oid "):
			if oidSeen {
				return Status{}, false
			}
			oidSeen = true
			oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "#"):
		case validStatusRecord(line):
			dirty = true
		case strings.HasPrefix(line, "! "):
		default:
			return Status{}, false
		}
	}
	if !branchSeen || !oidSeen || !validText(branch, 4096) {
		return Status{}, false
	}
	head := Head{Name: branch, Kind: HeadBranch}
	switch {
	case oid == "(initial)":
		head.Kind = HeadUnborn
	case branch == "(detached)":
		if !validOID(oid) {
			return Status{}, false
		}
		head = Head{Kind: HeadDetached, OID: oid}
	case !validOID(oid):
		return Status{}, false
	}
	return Status{Root: root, Head: head, Dirty: dirty}, true
}

func validStatusRecord(line string) bool {
	return strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 ") ||
		strings.HasPrefix(line, "u ") || strings.HasPrefix(line, "? ")
}

func singleOutputLine(output string) (string, bool) {
	output = strings.TrimSuffix(output, "\n")
	output = strings.TrimSuffix(output, "\r")
	return output, output != "" && !strings.ContainsAny(output, "\r\n")
}

func isExitCode(err error, code int) bool {
	var exitError *exec.ExitError
	return errors.As(err, &exitError) && exitError.ExitCode() == code
}

func validText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validOID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
