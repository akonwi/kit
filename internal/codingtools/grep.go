package codingtools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/droids"
)

const (
	defaultGrepLimit      = 100
	maxGrepLimit          = 100_000
	maxGrepContext        = 1_000
	maxGrepLineRunes      = 2_000
	maxGrepInputLineBytes = 4 << 20
	maxSearchBytes        = 60 << 10
	maxSearchContentBytes = maxSearchBytes - 512
)

type grepArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	IgnoreCase bool   `json:"ignoreCase,omitempty"`
	Literal    bool   `json:"literal,omitempty"`
	Context    *int   `json:"context,omitempty"`
	Limit      *int   `json:"limit,omitempty"`
}

var errGrepLineTooLong = errors.New("grep input line too long")

type grepDetails struct {
	MatchCount        int  `json:"matchCount"`
	MatchLimitReached bool `json:"matchLimitReached"`
	BytesTruncated    bool `json:"bytesTruncated"`
	LinesTruncated    bool `json:"linesTruncated"`
	SkippedFiles      int  `json:"skippedFiles,omitempty"`
}

type grepLine struct {
	number    int
	text      string
	truncated bool
}

func newGrepTool(cwd string) droids.Tool[grepArgs] {
	cwd = filepath.Clean(cwd)
	return newGrepToolWithCWD(func() string { return cwd })
}

func newGrepToolWithCWD(currentCWD CWDProvider) droids.Tool[grepArgs] {
	return droids.Tool[grepArgs]{
		Name:        GrepToolName,
		Description: "Search file contents for a pattern. Returns matching lines with file paths and line numbers. Respects .gitignore. Output truncated to 100 matches. Long lines truncated to 2000 chars.",
		Parameters: objectSchema(map[string]any{
			"pattern":    stringSchema("Search pattern (regex or literal string)"),
			"path":       stringSchema("Directory or file to search (default: cwd)"),
			"glob":       stringSchema("Filter files by glob pattern, e.g. '*.ts'"),
			"ignoreCase": booleanSchema("Case-insensitive search (default: false)"),
			"literal":    booleanSchema("Treat pattern as literal string instead of regex (default: false)"),
			"context":    integerSchema("Lines of context before and after each match (default: 0)"),
			"limit":      integerSchema("Max matches to return (default: 100)"),
		}, "pattern"),
		Mode: droids.ModeParallel,
		Execute: func(ctx context.Context, _ droids.ToolContext, args grepArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			result, err := executeGrep(ctx, currentCWD(), args)
			if err != nil {
				if ctx.Err() != nil {
					return droids.ToolResult{}, ctx.Err()
				}
				return errorResult(err, grepDetails{}), nil
			}
			return result, nil
		},
	}
}

func executeGrep(ctx context.Context, cwd string, args grepArgs) (droids.ToolResult, error) {
	if args.Pattern == "" {
		return droids.ToolResult{}, fmt.Errorf("pattern is required")
	}
	limit := defaultGrepLimit
	if args.Limit != nil {
		limit = max(1, *args.Limit)
	}
	if limit > maxGrepLimit {
		return droids.ToolResult{}, fmt.Errorf("limit must not exceed %d", maxGrepLimit)
	}
	contextLines := 0
	if args.Context != nil {
		contextLines = max(0, *args.Context)
	}
	if contextLines > maxGrepContext {
		return droids.ToolResult{}, fmt.Errorf("context must not exceed %d lines", maxGrepContext)
	}

	pattern := args.Pattern
	if args.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	if args.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	matcher, err := regexp.Compile(pattern)
	if err != nil {
		return droids.ToolResult{}, fmt.Errorf("invalid search pattern: %w", err)
	}
	var fileMatcher *regexp.Regexp
	globBasename := false
	if args.Glob != "" {
		normalized := filepath.ToSlash(strings.TrimPrefix(args.Glob, "./"))
		globBasename = !strings.Contains(normalized, "/")
		fileMatcher, err = compileGlob(normalized, true)
		if err != nil {
			return droids.ToolResult{}, err
		}
	}

	root := optionalPath(cwd, args.Path)
	rootInfo, err := os.Stat(root)
	if err != nil {
		return droids.ToolResult{}, fmt.Errorf("path not found: %s", root)
	}
	rootIsDirectory := rootInfo.IsDir()
	var output []string
	details := grepDetails{}
	outputBytes := 0
	appendOutput := func(path string, line grepLine, match bool) bool {
		details.LinesTruncated = details.LinesTruncated || line.truncated
		separator := "-"
		if match {
			separator = ":"
		}
		formatted := fmt.Sprintf("%s:%d%s %s", path, line.number, separator, line.text)
		required := len(formatted)
		if len(output) > 0 {
			required++
		}
		if outputBytes+required > maxSearchContentBytes {
			details.BytesTruncated = true
			return false
		}
		output = append(output, formatted)
		outputBytes += required
		return true
	}

	err = walkWorkspace(ctx, root, func(entry walkEntry) error {
		if entry.IsDir {
			return nil
		}
		candidate := entry.Relative
		if globBasename {
			candidate = filepath.Base(candidate)
		}
		if fileMatcher != nil && !fileMatcher.MatchString(filepath.ToSlash(candidate)) {
			return nil
		}
		info, err := os.Lstat(entry.Absolute)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 && filepath.Clean(entry.Absolute) != filepath.Clean(root) {
			return nil
		}
		if info.Mode()&os.ModeSymlink == 0 && !info.Mode().IsRegular() {
			return nil
		}
		formattedPath := filepath.Base(entry.Absolute)
		if rootIsDirectory {
			formattedPath = filepath.ToSlash(entry.Relative)
		}
		matches, reached, err := grepFile(ctx, entry.Absolute, matcher, contextLines, limit-details.MatchCount, func(line grepLine, match bool) bool {
			return appendOutput(formattedPath, line, match)
		})
		details.MatchCount += matches
		if errors.Is(err, errGrepLineTooLong) {
			details.SkippedFiles++
			return nil
		}
		if err != nil {
			return err
		}
		if details.BytesTruncated {
			return errStopWalk
		}
		if reached || details.MatchCount >= limit {
			details.MatchLimitReached = true
			return errStopWalk
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return droids.ToolResult{}, err
	}
	text := strings.Join(output, "\n")
	if details.MatchCount == 0 {
		text = "No matches found"
	}
	var notices []string
	if details.MatchLimitReached {
		notices = append(notices, fmt.Sprintf("%d match limit reached — use limit=%d for more or refine pattern", limit, limit*2))
	}
	if details.BytesTruncated {
		notices = append(notices, fmt.Sprintf("%dKB output limit reached", maxSearchBytes/1024))
	}
	if details.LinesTruncated {
		notices = append(notices, fmt.Sprintf("some lines truncated to %d chars — use read tool for full lines", maxGrepLineRunes))
	}
	if details.SkippedFiles > 0 {
		notices = append(notices, fmt.Sprintf("%d file%s skipped because a line exceeded %d MiB", details.SkippedFiles, plural(details.SkippedFiles), maxGrepInputLineBytes>>20))
	}
	if len(notices) > 0 {
		text += "\n\n[" + strings.Join(notices, ". ") + "]"
	}
	return textResult(text, details), nil
}

func grepFile(
	ctx context.Context,
	path string,
	matcher *regexp.Regexp,
	contextLines, remainingMatches int,
	emit func(grepLine, bool) bool,
) (matches int, limitReached bool, err error) {
	file, err := openRegularFile(path)
	if err != nil {
		return 0, false, err
	}
	defer file.Close()
	probe := make([]byte, 8<<10)
	read, readErr := file.Read(probe)
	if readErr != nil && readErr != io.EOF {
		return 0, false, readErr
	}
	if bytes.IndexByte(probe[:read], 0) >= 0 {
		return 0, false, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, false, err
	}
	info, err := file.Stat()
	if err != nil {
		return 0, false, err
	}

	reader := bufio.NewReader(file)
	ring := make([]grepLine, 0, contextLines)
	lineNumber := 0
	lastEmitted := 0
	afterUntil := 0
	previousEndedWithNewline := false
	process := func(text string) (bool, error) {
		lineNumber++
		text = normalizeText(strings.ReplaceAll(text, "\r", ""))
		display, truncated := truncateRunes(text, maxGrepLineRunes)
		line := grepLine{number: lineNumber, text: display, truncated: truncated}
		isMatch := matches < remainingMatches && matcher.MatchString(text)
		if isMatch {
			matches++
			for _, prior := range ring {
				if prior.number > lastEmitted {
					if !emit(prior, false) {
						return true, nil
					}
					lastEmitted = prior.number
				}
			}
			if !emit(line, true) {
				return true, nil
			}
			lastEmitted = line.number
			afterUntil = line.number + contextLines
		} else if line.number <= afterUntil && line.number > lastEmitted {
			if !emit(line, false) {
				return true, nil
			}
			lastEmitted = line.number
		}
		if contextLines > 0 {
			ring = append(ring, line)
			if len(ring) > contextLines {
				ring = ring[1:]
			}
		}
		if matches >= remainingMatches && line.number >= afterUntil {
			return true, nil
		}
		return false, nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return matches, false, err
		}
		line, newline, present, overflow, err := nextBoundedLine(ctx, reader, maxGrepInputLineBytes)
		if err != nil {
			return matches, false, err
		}
		if overflow {
			return matches, false, fmt.Errorf("%w: line %d in %s exceeds %d MiB", errGrepLineTooLong, lineNumber+1, path, maxGrepInputLineBytes>>20)
		}
		if !present {
			if info.Size() == 0 || previousEndedWithNewline {
				stop, err := process("")
				if stop || err != nil {
					return matches, matches >= remainingMatches, err
				}
			}
			break
		}
		stop, err := process(strings.TrimSuffix(line, "\r"))
		if stop || err != nil {
			return matches, matches >= remainingMatches, err
		}
		previousEndedWithNewline = newline
		if !newline {
			break
		}
	}
	return matches, matches >= remainingMatches, nil
}

func truncateRunes(text string, limit int) (string, bool) {
	if utf8.RuneCountInString(text) <= limit {
		return text, false
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…", true
}
