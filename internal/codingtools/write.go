package codingtools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akonwi/kit/internal/droids"
)

const maxWriteBytes = 16 << 20

type writeArgs struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}

type writeDetails struct {
	Path  string `json:"path"`
	Lines int    `json:"lines"`
}

func newWriteTool(cwd string) droids.Tool[writeArgs] {
	cwd = filepath.Clean(cwd)
	return newWriteToolWithCWD(func() string { return cwd })
}

func newWriteToolWithCWD(currentCWD CWDProvider) droids.Tool[writeArgs] {
	return droids.Tool[writeArgs]{
		Name:        WriteToolName,
		Description: "Write content to a file. Creates the file and any missing parent directories.",
		Parameters: objectSchema(map[string]any{
			"path":    stringSchema("Path to the file (relative to cwd or absolute)"),
			"content": stringSchema("Content to write"),
		}, "path", "content"),
		Mode: droids.ModeSequential,
		Execute: func(ctx context.Context, _ droids.ToolContext, args writeArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			if err := ctx.Err(); err != nil {
				return droids.ToolResult{}, err
			}
			if args.Content == nil {
				return errorResult(fmt.Errorf("content is required"), writeDetails{Path: args.Path}), nil
			}
			content := *args.Content
			if len(content) > maxWriteBytes {
				return errorResult(fmt.Errorf("content exceeds %d MiB limit", maxWriteBytes>>20), writeDetails{Path: args.Path}), nil
			}
			path, err := resolvePath(currentCWD(), args.Path)
			if err != nil {
				return errorResult(err, writeDetails{Path: args.Path}), nil
			}
			err = withMutationLock(ctx, path, func() error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return fmt.Errorf("create parent directories: %w", err)
				}
				return writeRegularFile(ctx, path, []byte(content), 0o666)
			})
			if err != nil {
				if ctx.Err() != nil {
					return droids.ToolResult{}, ctx.Err()
				}
				return errorResult(err, writeDetails{Path: path}), nil
			}
			lines := strings.Count(content, "\n") + 1
			return textResult(fmt.Sprintf("Wrote %d lines to %s", lines, path), writeDetails{Path: path, Lines: lines}), nil
		},
	}
}
