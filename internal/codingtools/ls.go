package codingtools

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akonwi/kit/internal/droids"
)

const (
	maxListEntries = 10_000
	maxListBytes   = (60 << 10) - 256
)

type listArgs struct {
	Path string `json:"path,omitempty"`
}

type listDetails struct {
	Path      string `json:"path"`
	Count     int    `json:"count"`
	Truncated bool   `json:"truncated,omitempty"`
}

type reverseNames []string

func (names reverseNames) Len() int                  { return len(names) }
func (names reverseNames) Less(left, right int) bool { return names[left] > names[right] }
func (names reverseNames) Swap(left, right int) {
	names[left], names[right] = names[right], names[left]
}
func (names *reverseNames) Push(value any) { *names = append(*names, value.(string)) }
func (names *reverseNames) Pop() any {
	old := *names
	last := old[len(old)-1]
	*names = old[:len(old)-1]
	return last
}

func newListTool(cwd string) droids.Tool[listArgs] {
	cwd = filepath.Clean(cwd)
	return newListToolWithCWD(func() string { return cwd })
}

func newListToolWithCWD(currentCWD CWDProvider) droids.Tool[listArgs] {
	return droids.Tool[listArgs]{
		Name:        ListToolName,
		Description: "List files and directories at a path.",
		Parameters: objectSchema(map[string]any{
			"path": stringSchema("Directory to list (default: cwd)"),
		}),
		Mode: droids.ModeParallel,
		Execute: func(ctx context.Context, _ droids.ToolContext, args listArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			if err := ctx.Err(); err != nil {
				return droids.ToolResult{}, err
			}
			path := optionalPath(currentCWD(), args.Path)
			file, err := openDirectory(path)
			if err != nil {
				return errorResult(err, listDetails{Path: path}), nil
			}
			defer file.Close()

			names := &reverseNames{}
			heap.Init(names)
			truncated := false
			for {
				if err := ctx.Err(); err != nil {
					return droids.ToolResult{}, err
				}
				entries, readErr := file.ReadDir(256)
				for _, entry := range entries {
					name := entry.Name()
					if names.Len() < maxListEntries {
						heap.Push(names, name)
					} else {
						truncated = true
						if name < (*names)[0] {
							heap.Pop(names)
							heap.Push(names, name)
						}
					}
				}
				if readErr != nil {
					if !errors.Is(readErr, io.EOF) {
						return errorResult(readErr, listDetails{Path: path}), nil
					}
					break
				}
			}
			sort.Strings(*names)
			lines := make([]string, 0, names.Len())
			bytes := 0
			for _, name := range *names {
				line := name
				if info, statErr := os.Stat(filepath.Join(path, name)); statErr == nil && info.IsDir() {
					line += "/"
				}
				if bytes+len(line)+1 > maxListBytes {
					truncated = true
					break
				}
				lines = append(lines, line)
				bytes += len(line) + 1
			}
			output := strings.Join(lines, "\n")
			if output == "" {
				output = "(empty)"
			}
			if truncated {
				output += fmt.Sprintf("\n\n[list truncated after %d entries]", len(lines))
			}
			return textResult(output, listDetails{Path: path, Count: len(lines), Truncated: truncated}), nil
		},
	}
}
