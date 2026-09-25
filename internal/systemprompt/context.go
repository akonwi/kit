package systemprompt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/apphome"
)

const (
	contextSectionID = "kit.context"
	agentsFilename   = "AGENTS.md"

	// DefaultMaxContextCandidates bounds the number of existing context-file candidates validated and read for one session.
	DefaultMaxContextCandidates = 64
	// DefaultMaxContextFileBytes bounds one context file at 128 KiB.
	DefaultMaxContextFileBytes int64 = 128 << 10
	// DefaultMaxContextBytes bounds aggregate context-file content at 256 KiB.
	DefaultMaxContextBytes int64 = 256 << 10
	// DefaultMaxContextDiagnostics bounds detailed context-loading diagnostics.
	DefaultMaxContextDiagnostics = 128

	contextPreamble = "Additional context guidance has been loaded from project context files. Follow it unless it conflicts with higher-priority instructions."
)

var (
	// ErrNoGitWorktree means cwd is not inside a Git worktree.
	ErrNoGitWorktree = errors.New("not inside a Git worktree")
)

// ContextLimits bounds filesystem guidance loaded for one session.
type ContextLimits struct {
	MaxCandidates  int
	MaxFileBytes   int64
	MaxTotalBytes  int64
	MaxDiagnostics int
}

// WorktreeRootResolver finds the Git worktree containing cwd without changing
// the process working directory.
type WorktreeRootResolver interface {
	ResolveWorktreeRoot(context.Context, string) (string, error)
}

// WorktreeRootResolverFunc adapts a function into a WorktreeRootResolver.
type WorktreeRootResolverFunc func(context.Context, string) (string, error)

// ResolveWorktreeRoot calls f.
func (f WorktreeRootResolverFunc) ResolveWorktreeRoot(ctx context.Context, cwd string) (string, error) {
	return f(ctx, cwd)
}

// GitWorktreeRootResolver resolves worktrees with a bounded git subprocess.
type GitWorktreeRootResolver struct {
	Command string
	Timeout time.Duration
}

// ResolveWorktreeRoot returns git's top-level worktree directory for cwd.
func (r GitWorktreeRootResolver) ResolveWorktreeRoot(ctx context.Context, cwd string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	commandName := r.Command
	if commandName == "" {
		commandName = "git"
	}
	command := exec.CommandContext(runCtx, commandName, "-C", cwd, "rev-parse", "--show-toplevel")
	command.Env = gitDiscoveryEnvironment(os.Environ())
	configureBoundedCommand(command)
	output, err := command.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("resolve Git worktree root: %w", context.DeadlineExceeded)
		}
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && bytes.Contains(bytes.ToLower(exitError.Stderr), []byte("not a git repository")) {
			return "", ErrNoGitWorktree
		}
		return "", fmt.Errorf("resolve Git worktree root: %w", err)
	}
	if !bytes.HasSuffix(output, []byte{'\n'}) {
		return "", errors.New("resolve Git worktree root: git returned an unterminated path")
	}
	root := string(output[:len(output)-1])
	if root == "" {
		return "", errors.New("resolve Git worktree root: git returned an empty path")
	}
	return root, nil
}

// ContextBuilderOptions configures session context discovery.
type ContextBuilderOptions struct {
	Paths    apphome.Paths
	Resolver WorktreeRootResolver
	Limits   ContextLimits
}

// ContextBuilder adds session-scoped AGENTS.md guidance to a Composer.
type ContextBuilder struct {
	composer *Composer
	home     string
	resolver WorktreeRootResolver
	limits   ContextLimits
}

var _ Builder = (*ContextBuilder)(nil)

// NewContextBuilder constructs a session-scoped AGENTS.md prompt builder.
func NewContextBuilder(composer *Composer, options ContextBuilderOptions) (*ContextBuilder, error) {
	if err := composer.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.Paths.Home) == "" {
		return nil, errors.New("system prompt context requires Kit home")
	}
	if !filepath.IsAbs(options.Paths.Home) {
		return nil, errors.New("system prompt context requires an absolute Kit home")
	}
	home, err := canonicalDirectory(options.Paths.Home)
	if err != nil {
		return nil, fmt.Errorf("resolve Kit home: %w", err)
	}
	limits, err := resolveContextLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	resolver := options.Resolver
	if resolver == nil {
		resolver = GitWorktreeRootResolver{}
	}
	return &ContextBuilder{
		composer: composer,
		home:     filepath.Clean(home),
		resolver: resolver,
		limits:   limits,
	}, nil
}

// Build discovers and appends context applicable to request.CWD.
func (b *ContextBuilder) Build(ctx context.Context, request Request) (Result, error) {
	return b.BuildWith(ctx, request)
}

// BuildWith discovers context and composes it with request-scoped sections.
func (b *ContextBuilder) BuildWith(ctx context.Context, request Request, requestSections ...Section) (Result, error) {
	if b == nil || b.composer == nil || b.resolver == nil {
		return Result{}, errors.New("system prompt context builder is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	section, err := b.contextSection(ctx, request.CWD)
	if err != nil {
		return Result{}, err
	}
	requestSections = append(requestSections, section)
	return b.composer.BuildWith(ctx, request, requestSections...)
}

type contextCandidate struct {
	path      string
	canonical string
	openPath  string
	identity  os.FileInfo
	order     int
}

type loadedContextFile struct {
	candidate contextCandidate
	content   string
}

func (b *ContextBuilder) contextSection(ctx context.Context, cwd string) (Section, error) {
	cwd, err := canonicalDirectory(cwd)
	if err != nil {
		return Section{}, fmt.Errorf("resolve session cwd: %w", err)
	}

	diagnostics := make([]Diagnostic, 0)
	projectDirectories := []string{cwd}
	root, rootErr := b.resolver.ResolveWorktreeRoot(ctx, cwd)
	switch {
	case rootErr == nil:
		root, err = canonicalDirectory(root)
		if err != nil {
			diagnostics = append(diagnostics, contextDiagnostic(
				DiagnosticWarning,
				"context.git_root_invalid",
				fmt.Sprintf("Could not use the resolved Git worktree root: %v", err),
				root,
			))
		} else if projectDirectories, err = directoriesFromRoot(root, cwd); err != nil {
			diagnostics = append(diagnostics, contextDiagnostic(
				DiagnosticWarning,
				"context.git_root_outside_cwd",
				fmt.Sprintf("Could not use the resolved Git worktree root: %v", err),
				root,
			))
			projectDirectories = []string{cwd}
		}
	case errors.Is(rootErr, ErrNoGitWorktree):
		// Cwd-only discovery is the expected non-worktree behavior.
	case rootErr != nil:
		if err := ctx.Err(); err != nil {
			return Section{}, err
		}
		diagnostics = append(diagnostics, contextDiagnostic(
			DiagnosticWarning,
			"context.git_root_unavailable",
			fmt.Sprintf("Could not resolve the Git worktree root: %v", rootErr),
			cwd,
		))
	}

	paths := make([]contextCandidate, 0, len(projectDirectories)+1)
	paths = append(paths, contextCandidate{path: filepath.Join(b.home, agentsFilename)})
	for _, directory := range projectDirectories {
		paths = append(paths, contextCandidate{path: filepath.Join(directory, agentsFilename)})
	}
	for index := range paths {
		paths[index].order = index
	}

	loaded, candidateDiagnostics, err := loadContextCandidates(ctx, paths, b.limits)
	if err != nil {
		return Section{}, err
	}
	diagnostics = append(diagnostics, candidateDiagnostics...)

	loaded, aggregateDiagnostics := retainLocalContent(loaded, b.limits.MaxTotalBytes)
	diagnostics = append(diagnostics, aggregateDiagnostics...)
	diagnostics = boundDiagnostics(diagnostics, b.limits.MaxDiagnostics)

	section := Section{
		ID:          contextSectionID,
		Kind:        SectionContext,
		Text:        renderContextFiles(loaded),
		Sources:     make([]Source, 0, len(loaded)),
		Diagnostics: diagnostics,
	}
	for _, file := range loaded {
		section.Sources = append(section.Sources, contextSource(file.candidate.path))
	}
	return section, nil
}

func resolveContextLimits(limits ContextLimits) (ContextLimits, error) {
	if limits.MaxCandidates < 0 || limits.MaxFileBytes < 0 || limits.MaxTotalBytes < 0 || limits.MaxDiagnostics < 0 {
		return ContextLimits{}, errors.New("system prompt context limits cannot be negative")
	}
	if limits.MaxFileBytes == math.MaxInt64 {
		return ContextLimits{}, errors.New("system prompt per-file context limit is too large")
	}
	if limits.MaxCandidates == 0 {
		limits.MaxCandidates = DefaultMaxContextCandidates
	}
	if limits.MaxFileBytes == 0 {
		limits.MaxFileBytes = DefaultMaxContextFileBytes
	}
	if limits.MaxTotalBytes == 0 {
		limits.MaxTotalBytes = DefaultMaxContextBytes
	}
	if limits.MaxDiagnostics == 0 {
		limits.MaxDiagnostics = DefaultMaxContextDiagnostics
	}
	return limits, nil
}

func canonicalDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%q is not absolute", path)
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", canonical)
	}
	return filepath.Clean(canonical), nil
}

func directoriesFromRoot(root, cwd string) ([]string, error) {
	relative, err := filepath.Rel(root, cwd)
	if err != nil {
		return nil, err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, fmt.Errorf("session cwd %q is outside %q", cwd, root)
	}
	directories := []string{root}
	if relative == "." {
		return directories, nil
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		directories = append(directories, current)
	}
	return directories, nil
}

func loadContextCandidates(ctx context.Context, paths []contextCandidate, limits ContextLimits) ([]loadedContextFile, []Diagnostic, error) {
	loaded := make([]loadedContextFile, 0, min(len(paths), limits.MaxCandidates))
	diagnostics := make([]Diagnostic, 0)
	seen := make(map[string]struct{}, len(paths))
	inspected := 0

	// Inspect from most local to broadest so limits and duplicates preserve the
	// closest applicable guidance. Restore composition order before returning.
	for index := len(paths) - 1; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		candidate := paths[index]
		info, err := os.Lstat(candidate.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, contextDiagnostic(
				DiagnosticWarning,
				"context.unreadable",
				fmt.Sprintf("Could not inspect context file: %v", err),
				candidate.path,
			))
			continue
		}
		if inspected >= limits.MaxCandidates {
			diagnostics = append(diagnostics, contextDiagnostic(
				DiagnosticWarning,
				"context.candidate_limit",
				"Context file omitted because the candidate limit was reached",
				candidate.path,
			))
			continue
		}
		inspected++
		if !validXMLString(candidate.path) {
			diagnostics = append(diagnostics, contextDiagnostic(
				DiagnosticWarning,
				"context.invalid_path",
				"Context file path is not valid UTF-8/XML text",
				candidate.path,
			))
			continue
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			diagnostics = append(diagnostics, contextDiagnostic(
				DiagnosticWarning,
				"context.non_regular",
				"Context candidate is not a regular file or readable file symlink",
				candidate.path,
			))
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate.path)
		if err != nil {
			diagnostics = append(diagnostics, contextDiagnostic(
				DiagnosticWarning,
				"context.unreadable",
				fmt.Sprintf("Could not resolve context file: %v", err),
				candidate.path,
			))
			continue
		}
		resolvedInfo, err := os.Stat(resolved)
		if err != nil || !resolvedInfo.Mode().IsRegular() {
			diagnostics = append(diagnostics, contextDiagnostic(
				DiagnosticWarning,
				"context.non_regular",
				"Context candidate does not resolve to a regular file",
				candidate.path,
			))
			continue
		}
		candidate.canonical = filepath.Clean(resolved)
		candidate.openPath = candidate.canonical
		candidate.identity = resolvedInfo
		if _, duplicate := seen[candidate.canonical]; duplicate {
			diagnostics = append(diagnostics, contextDiagnostic(
				DiagnosticInfo,
				"context.duplicate",
				"Context file resolves to an already selected source",
				candidate.path,
			))
			continue
		}
		seen[candidate.canonical] = struct{}{}

		content, diagnostic, err := readContextCandidate(ctx, candidate, limits.MaxFileBytes)
		if err != nil {
			return nil, nil, err
		}
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		loaded = append(loaded, loadedContextFile{candidate: candidate, content: content})
	}
	sort.Slice(loaded, func(i, j int) bool {
		return loaded[i].candidate.order < loaded[j].candidate.order
	})
	return loaded, diagnostics, nil
}

func readContextCandidate(ctx context.Context, candidate contextCandidate, maximum int64) (string, *Diagnostic, error) {
	file, err := openContextFile(candidate.openPath)
	if err != nil {
		diagnostic := contextDiagnostic(
			DiagnosticWarning,
			"context.unreadable",
			fmt.Sprintf("Could not securely open context file: %v", err),
			candidate.path,
		)
		return "", &diagnostic, nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		diagnostic := contextDiagnostic(
			DiagnosticWarning,
			"context.unreadable",
			fmt.Sprintf("Could not inspect context file: %v", err),
			candidate.path,
		)
		return "", &diagnostic, nil
	}
	if !info.Mode().IsRegular() || candidate.identity == nil || !os.SameFile(candidate.identity, info) {
		diagnostic := contextDiagnostic(
			DiagnosticWarning,
			"context.non_regular",
			"Opened context candidate is not the selected regular file",
			candidate.path,
		)
		return "", &diagnostic, nil
	}
	if info.Size() > maximum {
		diagnostic := contextDiagnostic(
			DiagnosticWarning,
			"context.file_too_large",
			fmt.Sprintf("Context file exceeds the %d-byte per-file limit", maximum),
			candidate.path,
		)
		return "", &diagnostic, nil
	}
	content, err := readContextBytes(ctx, file, maximum)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", nil, ctxErr
		}
		diagnostic := contextDiagnostic(
			DiagnosticWarning,
			"context.unreadable",
			fmt.Sprintf("Could not read context file: %v", err),
			candidate.path,
		)
		return "", &diagnostic, nil
	}
	if int64(len(content)) > maximum {
		diagnostic := contextDiagnostic(
			DiagnosticWarning,
			"context.file_too_large",
			fmt.Sprintf("Context file exceeds the %d-byte per-file limit", maximum),
			candidate.path,
		)
		return "", &diagnostic, nil
	}
	if !utf8.Valid(content) {
		diagnostic := contextDiagnostic(
			DiagnosticWarning,
			"context.invalid_utf8",
			"Context file is not valid UTF-8",
			candidate.path,
		)
		return "", &diagnostic, nil
	}
	return string(content), nil, nil
}

func retainLocalContent(files []loadedContextFile, maximum int64) ([]loadedContextFile, []Diagnostic) {
	keep := make([]bool, len(files))
	remaining := maximum
	diagnostics := make([]Diagnostic, 0)
	for index := len(files) - 1; index >= 0; index-- {
		size := int64(len(files[index].content))
		if size <= remaining {
			keep[index] = true
			remaining -= size
			continue
		}
		diagnostics = append(diagnostics, contextDiagnostic(
			DiagnosticWarning,
			"context.aggregate_limit",
			fmt.Sprintf("Context file omitted because the %d-byte aggregate limit was reached", maximum),
			files[index].candidate.path,
		))
	}
	selected := make([]loadedContextFile, 0, len(files))
	for index, file := range files {
		if keep[index] {
			selected = append(selected, file)
		}
	}
	return selected, diagnostics
}

func renderContextFiles(files []loadedContextFile) string {
	if len(files) == 0 {
		return ""
	}
	var rendered strings.Builder
	rendered.WriteString(contextPreamble)
	rendered.WriteString("\n\n<context-files>\n")
	for index, file := range files {
		if index > 0 {
			rendered.WriteString("\n\n")
		}
		rendered.WriteString(`<context-file path="`)
		rendered.WriteString(escapeXMLAttribute(file.candidate.path))
		rendered.WriteString(`">`)
		rendered.WriteByte('\n')
		rendered.WriteString(file.content)
		if !strings.HasSuffix(file.content, "\n") {
			rendered.WriteByte('\n')
		}
		rendered.WriteString("</context-file>")
	}
	rendered.WriteString("\n</context-files>")
	return rendered.String()
}

func contextSource(path string) Source {
	digest := sha256.Sum256([]byte(path))
	return Source{ID: "context-file:" + hex.EncodeToString(digest[:]), Path: path}
}

func contextDiagnostic(severity DiagnosticSeverity, code, message, path string) Diagnostic {
	return Diagnostic{
		Severity: severity,
		Code:     code,
		Message:  message,
		Source:   contextSource(path),
	}
}

func boundDiagnostics(diagnostics []Diagnostic, maximum int) []Diagnostic {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Source.Path != diagnostics[j].Source.Path {
			return diagnostics[i].Source.Path < diagnostics[j].Source.Path
		}
		return diagnostics[i].Code < diagnostics[j].Code
	})
	if len(diagnostics) <= maximum {
		return diagnostics
	}
	suppressed := len(diagnostics) - maximum + 1
	diagnostics = append([]Diagnostic(nil), diagnostics[:maximum-1]...)
	diagnostics = append(diagnostics, contextDiagnostic(
		DiagnosticWarning,
		"context.diagnostics_suppressed",
		fmt.Sprintf("Suppressed %d additional context diagnostics", suppressed),
		"",
	))
	return diagnostics
}

func readContextBytes(ctx context.Context, reader io.Reader, maximum int64) ([]byte, error) {
	var content bytes.Buffer
	buffer := make([]byte, 32<<10)
	remaining := maximum + 1
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		readSize := int64(len(buffer))
		if readSize > remaining {
			readSize = remaining
		}
		count, err := reader.Read(buffer[:readSize])
		if count > 0 {
			_, _ = content.Write(buffer[:count])
			remaining -= int64(count)
		}
		if errors.Is(err, io.EOF) {
			return content.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, io.ErrNoProgress
		}
	}
	return content.Bytes(), nil
}

func validXMLString(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character == '\t' || character == '\n' || character == '\r' ||
			character >= 0x20 && character <= 0xd7ff ||
			character >= 0xe000 && character <= 0xfffd ||
			character >= 0x10000 && character <= 0x10ffff {
			continue
		}
		return false
	}
	return true
}

func escapeXMLAttribute(value string) string {
	var escaped strings.Builder
	for _, character := range value {
		switch character {
		case '&':
			escaped.WriteString("&amp;")
		case '<':
			escaped.WriteString("&lt;")
		case '>':
			escaped.WriteString("&gt;")
		case '"':
			escaped.WriteString("&quot;")
		case '\'':
			escaped.WriteString("&apos;")
		case '\t':
			escaped.WriteString("&#x9;")
		case '\n':
			escaped.WriteString("&#xA;")
		case '\r':
			escaped.WriteString("&#xD;")
		default:
			escaped.WriteRune(character)
		}
	}
	return escaped.String()
}

func gitDiscoveryEnvironment(environment []string) []string {
	blocked := map[string]struct{}{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": {},
		"GIT_CEILING_DIRECTORIES":          {},
		"GIT_COMMON_DIR":                   {},
		"GIT_DIR":                          {},
		"GIT_DISCOVERY_ACROSS_FILESYSTEM":  {},
		"GIT_INDEX_FILE":                   {},
		"GIT_OBJECT_DIRECTORY":             {},
		"GIT_WORK_TREE":                    {},
		"LC_ALL":                           {},
	}
	clean := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if _, remove := blocked[name]; found && remove {
			continue
		}
		clean = append(clean, entry)
	}
	return append(clean, "LC_ALL=C")
}
