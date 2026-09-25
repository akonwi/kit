// Package fileindex enumerates the files and directories of a workspace for
// interactive pickers such as composer file mentions.
//
// The scan mirrors what a developer expects a project browser to show:
// hierarchical .gitignore rules apply, version-control and
// dependency directories are always skipped, symlinked files are listed but
// symlinked directories are never traversed, and the result is bounded.
package fileindex

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/akonwi/kit/internal/ignorefile"
)

// DefaultMaxEntries bounds the combined number of files and directories a scan
// returns when Options.MaxEntries is zero.
const DefaultMaxEntries = 4000

var builtInExcludes = map[string]bool{".git": true, "node_modules": true}

// Entry is one indexed path relative to the scanned root, slash-separated.
// Directories carry a trailing slash.
type Entry struct {
	Path  string
	IsDir bool
}

// Options tunes a scan.
type Options struct {
	// MaxEntries bounds the combined files and directories returned. Zero
	// uses DefaultMaxEntries.
	MaxEntries int
}

// Result is one bounded project-path scan.
type Result struct {
	Entries   []Entry
	Truncated bool
}

// Scan walks root depth-first in name order and returns directories followed by
// files, each group sorted by path. Unreadable directories are skipped.
func Scan(ctx context.Context, root string, opts Options) ([]Entry, error) {
	result, err := ScanResult(ctx, root, opts)
	return result.Entries, err
}

// ScanResult walks root like Scan and reports whether the entry bound omitted
// anything. It examines at most one indexable entry beyond the returned bound.
func ScanResult(ctx context.Context, root string, opts Options) (Result, error) {
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	scanner := &scanner{ctx: ctx, root: root, maxEntries: maxEntries + 1}
	rules, err := scanner.readIgnoreRules(root, "")
	if err != nil {
		return Result{}, err
	}
	if err := scanner.walk(root, "", rules); err != nil {
		return Result{}, err
	}
	sort.Slice(scanner.dirs, func(i, j int) bool { return scanner.dirs[i].Path < scanner.dirs[j].Path })
	sort.Slice(scanner.files, func(i, j int) bool { return scanner.files[i].Path < scanner.files[j].Path })
	entries := append(scanner.dirs, scanner.files...)
	truncated := len(entries) > maxEntries
	if truncated {
		entries = entries[:maxEntries]
	}
	return Result{Entries: entries, Truncated: truncated}, nil
}

type scanner struct {
	ctx        context.Context
	root       string
	maxEntries int
	dirs       []Entry
	files      []Entry
}

func (s *scanner) full() bool {
	return len(s.dirs)+len(s.files) >= s.maxEntries
}

func (s *scanner) walk(directory, relativeDirectory string, rules []ignorefile.Rule) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	type pending struct {
		absolute, relative string
		rules              []ignorefile.Rule
	}
	var subdirectories []pending
	for _, entry := range entries {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if s.full() {
			return nil
		}
		name := entry.Name()
		absolute := filepath.Join(directory, name)
		relative := name
		if relativeDirectory != "" {
			relative = relativeDirectory + "/" + name
		}
		if entry.IsDir() {
			if builtInExcludes[name] || ignorefile.Ignored(relative, true, false, rules) {
				continue
			}
			s.dirs = append(s.dirs, Entry{Path: relative + "/", IsDir: true})
			local, err := s.readIgnoreRules(absolute, relative)
			if err != nil {
				return err
			}
			childRules := rules
			if len(local) > 0 {
				childRules = append(append([]ignorefile.Rule(nil), rules...), local...)
			}
			subdirectories = append(subdirectories, pending{absolute: absolute, relative: relative, rules: childRules})
			continue
		}
		isFile := entry.Type().IsRegular()
		if !isFile && entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(absolute)
			if err != nil {
				continue
			}
			isFile = info.Mode().IsRegular()
		}
		if !isFile {
			continue
		}
		if name == ".git" || name == ".gitignore" {
			continue
		}
		if ignorefile.Ignored(relative, false, false, rules) {
			continue
		}
		s.files = append(s.files, Entry{Path: relative})
	}
	for _, child := range subdirectories {
		if s.full() {
			return nil
		}
		if err := s.walk(child.absolute, child.relative, child.rules); err != nil {
			return err
		}
	}
	return nil
}

// readIgnoreRules loads .gitignore from directory. Missing or non-regular
// files contribute no rules.
func (s *scanner) readIgnoreRules(directory, base string) ([]ignorefile.Rule, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, ".gitignore")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer file.Close()
	return ignorefile.Parse(s.ctx, file, base)
}
