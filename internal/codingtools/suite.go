// Package codingtools provides Kit's server-owned base coding tool set.
package codingtools

import (
	"context"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"
	"sync"

	"github.com/akonwi/kit/internal/droids"
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
	if cwd == nil {
		panic("codingtools: cwd provider is required")
	}
	return []droids.AnyTool{
		droids.MustTool(newBashToolWithCWD(cwd)),
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
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Clean(filepath.Join(cwd, path)), nil
}

func optionalPath(cwd, path string) string {
	if path == "" {
		return cwd
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
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
