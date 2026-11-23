// Package codingtools provides Kit's server-owned base coding tool set.
package codingtools

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/akonwi/kit/droids"
)

const (
	BashToolName  = "bash"
	ReadToolName  = "read"
	WriteToolName = "write"
	EditToolName  = "edit"
	ListToolName  = "ls"
	GrepToolName  = "grep"
	FindToolName  = "find"
)

var (
	mutationLocks     [64]chan struct{}
	mutationLocksOnce sync.Once
)

// CWDProvider returns the current absolute filesystem scope for one session.
type CWDProvider func() string

// New returns the ordered base coding tool set rooted at a fixed cwd.
func New(cwd string) []droids.AnyTool {
	cwd = filepath.Clean(cwd)
	return NewDynamic(func() string { return cwd })
}

// NewDynamic returns the ordered base coding tool set rooted at the cwd read at
// the start of each tool execution.
func NewDynamic(cwd CWDProvider) []droids.AnyTool {
	return NewSession("", cwd)
}

// NewSession returns the tool set of NewDynamic for the session sessionID:
// its bash commands see the ID in KIT_SESSION_ID.
func NewSession(sessionID string, cwd CWDProvider) []droids.AnyTool {
	if cwd == nil {
		panic("codingtools: cwd provider is required")
	}
	return []droids.AnyTool{
		droids.MustTool(newBashToolWithCWD(cwd, sessionID)),
		droids.MustTool(newReadToolWithCWD(cwd)),
		droids.MustTool(newWriteToolWithCWD(cwd)),
		droids.MustTool(newEditToolWithCWD(cwd)),
		droids.MustTool(newListToolWithCWD(cwd)),
		droids.MustTool(newGrepToolWithCWD(cwd)),
		droids.MustTool(newFindToolWithCWD(cwd)),
	}
}

func resolvePath(cwd, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	return optionalPath(cwd, path)
}

// optionalPath resolves path against cwd, which is the result for an empty
// path. A relative path is refused while cwd is missing, so a session whose
// directory was removed neither misreports it as a missing file nor
// recreates it by writing beneath it.
func optionalPath(cwd, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if err := CheckCWD(cwd, "use an absolute path"); err != nil {
		return "", err
	}
	if path == "" {
		return cwd, nil
	}
	return filepath.Clean(filepath.Join(cwd, path)), nil
}

// CheckCWD reports a session cwd that is not an existing directory as an
// error that names it and suggests changing it or, after "or", alternative.
// Other inspection failures are left to the operation that uses cwd.
func CheckCWD(cwd, alternative string) error {
	info, err := os.Stat(cwd)
	switch {
	case err == nil && info.IsDir():
		return nil
	case err == nil:
		return fmt.Errorf("the working directory %s is not a directory; %s", cwd, cwdHint(alternative))
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return fmt.Errorf("the working directory %s does not exist; %s", cwd, cwdHint(alternative))
	default:
		return nil
	}
}

// CreateParents creates the missing parent directories of path. When cwd is
// set, path was given relative to it, and directories beneath cwd are created
// through a handle on cwd: a cwd removed meanwhile fails the call instead of
// being recreated. An empty cwd creates parents by path.
func CreateParents(cwd, path string) error {
	parent := filepath.Dir(path)
	if cwd == "" {
		return os.MkdirAll(parent, 0o755)
	}
	relative, err := filepath.Rel(cwd, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		// A relative path that leaves cwd names no directory beneath it.
		return os.MkdirAll(parent, 0o755)
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		if cwdErr := CheckCWD(cwd, "use an absolute path"); cwdErr != nil {
			return cwdErr
		}
		return err
	}
	defer root.Close()
	if relative == "." {
		_, err = root.Stat(".")
	} else {
		err = root.MkdirAll(relative, 0o755)
	}
	if err != nil {
		if cwdErr := CheckCWD(cwd, "use an absolute path"); cwdErr != nil {
			return cwdErr
		}
	}
	return err
}

func cwdHint(alternative string) string {
	if alternative == "" {
		return "change the working directory"
	}
	return "change the working directory or " + alternative
}

func withMutationLock(ctx context.Context, path string, operation func() error) error {
	mutationLocksOnce.Do(func() {
		for index := range mutationLocks {
			mutationLocks[index] = make(chan struct{}, 1)
		}
	})
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(path))
	lock := mutationLocks[hash.Sum32()%uint32(len(mutationLocks))]
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
		return operation()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func textResult(text string, details any) droids.ToolResult {
	encoded, _ := droids.EncodeDetails(details)
	return droids.ToolResult{
		Content: []droids.ResultContent{droids.TextContent{Text: text}},
		Details: encoded,
	}
}

func errorResult(err error, details any) droids.ToolResult {
	result := textResult("Error: "+err.Error(), details)
	result.IsError = true
	return result
}

func truncateUTF8(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	end := maxBytes
	for end > 0 && !isUTF8Start(text[end]) {
		end--
	}
	return text[:end]
}

func isUTF8Start(value byte) bool {
	return value&0xc0 != 0x80
}

func normalizeText(text string) string {
	return strings.ToValidUTF8(text, "�")
}
