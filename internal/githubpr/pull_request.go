// Package githubpr provides optional, bounded GitHub metadata through the user's gh CLI.
package githubpr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const lookupTimeout = 2500 * time.Millisecond
const maxOutputBytes = 64 << 10

// PullRequest contains only the metadata needed by the native footer.
type PullRequest struct {
	Number int
	URL    string
}

// ValidURL accepts bounded absolute HTTP(S) destinations without credentials or controls.
func ValidURL(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\\r\n\t ") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) {
			return false
		}
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Hostname() != "" && parsed.User == nil && parsed.Opaque == ""
}

func parse(raw []byte, branch string) *PullRequest {
	if !utf8.Valid(raw) {
		return nil
	}
	var value struct {
		Number      int    `json:"number"`
		URL         string `json:"url"`
		HeadRefName string `json:"headRefName"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Number <= 0 || int64(value.Number) > 9007199254740991 || !ValidURL(value.URL) || value.HeadRefName != branch {
		return nil
	}
	return &PullRequest{Number: value.Number, URL: value.URL}
}

// Lookup returns nil silently when gh, authentication, a matching PR, or valid metadata
// is unavailable. The explicit branch prevents a concurrent checkout from retargeting it.
func Lookup(ctx context.Context, cwd, branch string) *PullRequest {
	if branch == "" || len(branch) > 4096 || !utf8.ValidString(branch) || strings.ContainsRune(branch, 0) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "gh", "pr", "view", "--json", "number,url,headRefName", "--", branch)
	command.Dir = cwd
	// Keep credentials, but prevent inherited repository overrides or interactive tools.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") || key == "GH_REPO" || key == "GH_PROMPT_DISABLED" || key == "GH_PAGER" || key == "PAGER" {
			continue
		}
		command.Env = append(command.Env, entry)
	}
	command.Env = append(command.Env, "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "PAGER=cat", "GIT_TERMINAL_PROMPT=0")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 250 * time.Millisecond
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
	var output cappedBuffer
	command.Stdout = &output
	command.Stderr = io.Discard
	err := command.Run()
	// A helper may retain the pipe after gh exits; WaitDelay bounds pipe IO,
	// while this also revokes any remaining members of the owned process group.
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if err != nil || ctx.Err() != nil || output.overflow {
		return nil
	}
	return parse(output.buffer.Bytes(), branch)
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	n := len(data)
	remaining := maxOutputBytes - b.buffer.Len()
	if remaining > 0 {
		_, _ = b.buffer.Write(data[:min(n, remaining)])
	}
	if n > remaining {
		b.overflow = true
	}
	return n, nil
}
