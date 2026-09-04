package codingtools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/akonwi/kit/internal/droids"
)

const maxReadOutputBytes = 50_000

type readArgs struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
}

type readDetails struct {
	Path      string `json:"path"`
	Lines     int    `json:"lines"`
	Truncated bool   `json:"truncated"`
}

func newReadTool(cwd string) droids.Tool[readArgs] {
	return droids.Tool[readArgs]{
		Name:        ReadToolName,
		Description: "Read the contents of a file. Supports an optional line offset and limit.",
		Parameters: objectSchema(map[string]any{
			"path":   stringSchema("Path to the file (relative to cwd or absolute)"),
			"offset": integerSchema("Line number to start reading from (1-indexed)"),
			"limit":  integerSchema("Maximum number of lines to read"),
		}, "path"),
		Mode: droids.ModeParallel,
		Execute: func(ctx context.Context, args readArgs) (droids.ToolResult, error) {
			path, err := resolvePath(cwd, args.Path)
			if err != nil {
				return errorResult(err, readDetails{Path: args.Path}), nil
			}
			if args.Limit != nil && *args.Limit < 0 {
				return errorResult(fmt.Errorf("limit must not be negative"), readDetails{Path: path}), nil
			}
			if args.Offset != nil && *args.Offset < 1 {
				return errorResult(fmt.Errorf("offset must be at least 1"), readDetails{Path: path}), nil
			}
			text, lines, truncated, err := readSelection(ctx, path, args.Offset, args.Limit)
			if err != nil {
				if ctx.Err() != nil {
					return droids.ToolResult{}, ctx.Err()
				}
				return errorResult(err, readDetails{Path: path}), nil
			}
			return textResult(text, readDetails{Path: path, Lines: lines, Truncated: truncated}), nil
		},
	}
}

func readSelection(ctx context.Context, path string, offset, limit *int) (string, int, bool, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return "", 0, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, false, err
	}

	start := 1
	if offset != nil {
		start = *offset
	}
	if limit != nil && *limit == 0 {
		return "", 0, false, nil
	}

	reader := bufio.NewReader(file)
	lineNumber := 0
	selected := 0
	previousEndedWithNewline := false
	var output strings.Builder

	appendLine := func(line string, overflow bool) bool {
		if lineNumber < start || (limit != nil && selected >= *limit) {
			return false
		}
		selected++
		line = normalizeText(line)
		separator := ""
		if selected > 1 {
			separator = "\n"
		}
		remaining := maxReadOutputBytes - output.Len()
		if len(separator)+len(line) <= remaining && !overflow {
			output.WriteString(separator)
			output.WriteString(line)
			return false
		}
		if remaining > 0 {
			combined := separator + line
			output.WriteString(truncateUTF8(combined, remaining))
		}
		return true
	}

	for {
		if err := ctx.Err(); err != nil {
			return "", selected, false, err
		}
		line, newline, present, overflow, err := nextBoundedLine(ctx, reader, maxReadOutputBytes+4)
		if err != nil {
			return "", selected, false, err
		}
		if !present {
			if info.Size() == 0 || previousEndedWithNewline {
				lineNumber++
				if appendLine("", false) {
					return output.String() + "\n[truncated]", selected, true, nil
				}
			}
			break
		}
		lineNumber++
		if appendLine(line, overflow) {
			return output.String() + "\n[truncated]", selected, true, nil
		}
		if limit != nil && selected >= *limit {
			break
		}
		previousEndedWithNewline = newline
		if !newline {
			break
		}
	}
	return output.String(), selected, false, nil
}

func nextBoundedLine(ctx context.Context, reader *bufio.Reader, captureLimit int) (line string, newline, present, overflow bool, err error) {
	var captured []byte
	for {
		if err := ctx.Err(); err != nil {
			return "", false, present, overflow, err
		}
		fragment, readErr := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			present = true
			if fragment[len(fragment)-1] == '\n' {
				fragment = fragment[:len(fragment)-1]
				newline = true
			}
			remaining := captureLimit - len(captured)
			if len(fragment) > remaining {
				overflow = true
			}
			if remaining > 0 {
				captured = append(captured, fragment[:min(remaining, len(fragment))]...)
			}
		}
		switch {
		case readErr == nil:
			return string(captured), newline, present, overflow, nil
		case errors.Is(readErr, bufio.ErrBufferFull):
			if len(captured) >= captureLimit {
				overflow = true
			}
			continue
		case errors.Is(readErr, io.EOF):
			return string(captured), newline, present, overflow, nil
		default:
			return "", false, present, overflow, readErr
		}
	}
}
