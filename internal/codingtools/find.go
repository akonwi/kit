package codingtools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akonwi/kit/internal/droids"
)

const (
	defaultFindLimit = 1_000
	maxFindLimit     = 100_000
)

type findArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Limit   *int   `json:"limit,omitempty"`
}

type findDetails struct {
	Count              int  `json:"count"`
	ResultLimitReached bool `json:"resultLimitReached"`
	BytesTruncated     bool `json:"bytesTruncated"`
}

func newFindTool(cwd string) droids.Tool[findArgs] {
	cwd = filepath.Clean(cwd)
	return newFindToolWithCWD(func() string { return cwd })
}

func newFindToolWithCWD(currentCWD CWDProvider) droids.Tool[findArgs] {
	return droids.Tool[findArgs]{
		Name:        FindToolName,
		Description: "Find files by glob pattern. Returns paths relative to the search directory. Respects .gitignore. Truncated to 1000 results.",
		Parameters: objectSchema(map[string]any{
			"pattern": stringSchema("Glob pattern to match files, e.g. '*.ts', '**/*.json', 'src/**/*.spec.ts'"),
			"path":    stringSchema("Directory to search (default: cwd)"),
			"limit":   integerSchema("Max results (default: 1000)"),
		}, "pattern"),
		Mode: droids.ModeParallel,
		Execute: func(ctx context.Context, _ droids.ToolContext, args findArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			result, err := executeFind(ctx, currentCWD(), args)
			if err != nil {
				if ctx.Err() != nil {
					return droids.ToolResult{}, ctx.Err()
				}
				return errorResult(err, findDetails{}), nil
			}
			return result, nil
		},
	}
}

func executeFind(ctx context.Context, cwd string, args findArgs) (droids.ToolResult, error) {
	if args.Pattern == "" {
		return droids.ToolResult{}, fmt.Errorf("pattern is required")
	}
	limit := defaultFindLimit
	if args.Limit != nil {
		limit = *args.Limit
	}
	if limit < 1 {
		return droids.ToolResult{}, fmt.Errorf("limit must be positive")
	}
	if limit > maxFindLimit {
		return droids.ToolResult{}, fmt.Errorf("limit must not exceed %d", maxFindLimit)
	}
	normalized := filepath.ToSlash(strings.TrimPrefix(args.Pattern, "./"))
	basenameOnly := !strings.Contains(normalized, "/")
	matcher, err := compileGlob(normalized, true)
	if err != nil {
		return droids.ToolResult{}, err
	}
	root := optionalPath(cwd, args.Path)
	if _, err := os.Stat(root); err != nil {
		return droids.ToolResult{}, fmt.Errorf("path not found: %s", root)
	}
	return collectFindResults(ctx, root, matcher, basenameOnly, limit)
}

func collectFindResults(ctx context.Context, root string, matcher *regexp.Regexp, basenameOnly bool, limit int) (droids.ToolResult, error) {
	results := make([]string, 0, min(limit, 1024))
	details := findDetails{}
	outputBytes := 0
	err := walkWorkspace(ctx, root, func(entry walkEntry) error {
		candidate := filepath.ToSlash(entry.Relative)
		if basenameOnly {
			candidate = filepath.Base(candidate)
		}
		if !matcher.MatchString(candidate) {
			return nil
		}
		result := filepath.ToSlash(entry.Relative)
		if entry.IsDir {
			result += "/"
		}
		details.Count++
		required := len(result)
		if len(results) > 0 {
			required++
		}
		if outputBytes+required > maxSearchContentBytes {
			details.BytesTruncated = true
			return errStopWalk
		}
		results = append(results, result)
		outputBytes += required
		if details.Count >= limit {
			details.ResultLimitReached = true
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return droids.ToolResult{}, err
	}
	if len(results) == 0 {
		return textResult("No files found matching pattern", map[string]any{}), nil
	}

	output := strings.Join(results, "\n")
	var notices []string
	if details.ResultLimitReached {
		notices = append(notices, fmt.Sprintf("%d results limit reached — use limit=%d or refine pattern", limit, limit*2))
	}
	if details.BytesTruncated {
		notices = append(notices, fmt.Sprintf("%dKB output limit reached", maxSearchBytes/1024))
	}
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}
	return textResult(output, details), nil
}
