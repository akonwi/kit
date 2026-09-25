package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/systemprompt"
	"go.yaml.in/yaml/v4"
	"golang.org/x/sys/unix"
)

const (
	projectSkillsPath       = ".agents/skills"
	maxDiscoveryDirectories = 512
	maxDiscoveryDepth       = 32
	maxDirectoryEntries     = 1024
	maxDiscoveryDiagnostics = 128
	maxDiagnosticMessage    = 4096
)

// LoadResult is one immutable session skill snapshot and its non-fatal discovery diagnostics.
type LoadResult struct {
	Registry    *Registry
	Diagnostics []systemprompt.Diagnostic
}

// Loader discovers the complete skill registry applicable to one session cwd.
// Load may be called concurrently for different sessions.
type Loader interface {
	Load(context.Context, string) (LoadResult, error)
}

// FilesystemLoader discovers user-global and project-local skills.
type FilesystemLoader struct {
	userHome     string
	userHomeInfo os.FileInfo
}

// NewFilesystemLoader constructs the standard skill loader from resolved Kit paths.
func NewFilesystemLoader(paths apphome.Paths) (*FilesystemLoader, error) {
	if strings.TrimSpace(paths.Home) == "" || !filepath.IsAbs(paths.Home) ||
		filepath.Clean(paths.Skills) != filepath.Join(filepath.Clean(paths.Home), "skills") {
		return nil, errors.New("skill discovery requires consistent absolute Kit home and skills paths")
	}
	userHome := filepath.Clean(paths.Home)
	if canonical, err := filepath.EvalSymlinks(userHome); err == nil {
		userHome = canonical
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("resolve Kit home for skill discovery: %w", err)
	}
	userHomeInfo, err := os.Stat(userHome)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect Kit home for skill discovery: %w", err)
	}
	return &FilesystemLoader{userHome: userHome, userHomeInfo: userHomeInfo}, nil
}

// Load scans resolved global paths and the explicit cwd without reading or changing process-global cwd.
func (l *FilesystemLoader) Load(ctx context.Context, cwd string) (LoadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(cwd) == "" || !filepath.IsAbs(cwd) {
		return LoadResult{}, errors.New("skill discovery requires an absolute cwd")
	}
	canonicalCWD, err := filepath.EvalSymlinks(filepath.Clean(cwd))
	if err != nil {
		return LoadResult{}, fmt.Errorf("resolve skill discovery cwd: %w", err)
	}
	info, err := os.Stat(canonicalCWD)
	if err != nil || !info.IsDir() {
		return LoadResult{}, errors.New("skill discovery requires an existing directory cwd")
	}
	if l == nil || l.userHome == "" {
		return LoadResult{}, errors.New("skill loader is not initialized")
	}

	state := discoveryState{
		seenNames:       map[string]string{KitCustomizationName: "built-in"},
		seenDirectories: make(map[string]struct{}),
	}
	state.scanRoot(ctx, l.userHome, l.userHomeInfo, "skills", SourceUser)
	state.scanRoot(ctx, canonicalCWD, info, projectSkillsPath, SourceProject)
	if err := ctx.Err(); err != nil {
		return LoadResult{}, err
	}
	registry, err := NewRegistry(state.skills...)
	if err != nil {
		return LoadResult{}, err
	}
	return LoadResult{Registry: registry, Diagnostics: state.diagnostics}, nil
}

type discoveryState struct {
	directories     int
	skills          []Skill
	diagnostics     []systemprompt.Diagnostic
	seenNames       map[string]string
	seenDirectories map[string]struct{}
	skillLimitHit   bool
}

func (s *discoveryState) scanRoot(ctx context.Context, rootPath string, expected os.FileInfo, relative string, source Source) {
	searchPath := filepath.Join(rootPath, relative)
	var anchor *os.Root
	if expected != nil {
		var err error
		anchor, err = os.OpenRoot(rootPath)
		if err != nil {
			s.warn("skills.root_replaced", "Skill search root changed during discovery and was omitted", searchPath)
			return
		}
		defer anchor.Close()
		openedInfo, statErr := anchor.Stat(".")
		if statErr != nil || !os.SameFile(expected, openedInfo) {
			s.warn("skills.root_replaced", "Skill search root changed during discovery and was omitted", searchPath)
			return
		}
	}
	root, err := openResolvedSkillDirectory(searchPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		s.warn("skills.unreadable_directory", "Could not open skill directory: "+err.Error(), searchPath)
		return
	}
	defer root.Close()
	if expected != nil {
		current, statErr := os.Stat(rootPath)
		if statErr != nil || !os.SameFile(expected, current) {
			s.warn("skills.root_replaced", "Skill search root changed during discovery and was omitted", searchPath)
			return
		}
	}
	s.scan(ctx, root, ".", searchPath, source, 0)
}

func openResolvedSkillDirectory(path string) (*os.Root, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	selected, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !selected.IsDir() {
		return nil, fmt.Errorf("%q does not resolve to a directory", path)
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !opened.IsDir() || !os.SameFile(selected, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("%q changed while opening", path)
	}
	return root, nil
}

func (s *discoveryState) scanLinkedDirectory(ctx context.Context, physicalPath, displayPath string, source Source, depth int) {
	root, err := openResolvedSkillDirectory(physicalPath)
	if errors.Is(err, os.ErrNotExist) {
		s.warn("skills.unreadable_directory", "Could not resolve skill directory: "+err.Error(), displayPath)
		return
	}
	if err != nil {
		s.warn("skills.non_directory", "Skill search path does not resolve to a directory: "+err.Error(), displayPath)
		return
	}
	defer root.Close()
	s.scan(ctx, root, ".", displayPath, source, depth)
}

func (s *discoveryState) scan(ctx context.Context, root *os.Root, directory, displayDirectory string, source Source, depth int) {
	if ctx.Err() != nil {
		return
	}
	if len(s.skills)+1 >= maxSkillsPerRegistry {
		if !s.skillLimitHit {
			s.skillLimitHit = true
			s.warn("skills.skill_limit", "Additional skills were omitted because the registry limit was reached", displayDirectory)
		}
		return
	}
	if depth > maxDiscoveryDepth {
		s.warn("skills.depth_limit", "Skill directory omitted because the discovery depth limit was reached", displayDirectory)
		return
	}
	if s.directories >= maxDiscoveryDirectories {
		s.warn("skills.directory_limit", "Skill directory omitted because the discovery directory limit was reached", displayDirectory)
		return
	}
	info, err := root.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		s.warn("skills.unreadable_directory", "Could not inspect skill directory: "+err.Error(), displayDirectory)
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		s.scanLinkedDirectory(ctx, rootedPath(root, directory), displayDirectory, source, depth)
		return
	}
	if !info.IsDir() {
		s.warn("skills.non_directory", "Skill search path does not resolve to a directory", displayDirectory)
		return
	}
	canonicalDirectory, err := filepath.EvalSymlinks(rootedPath(root, directory))
	if err != nil {
		s.warn("skills.unreadable_directory", "Could not resolve skill directory: "+err.Error(), displayDirectory)
		return
	}
	if _, seen := s.seenDirectories[canonicalDirectory]; seen {
		return
	}
	s.seenDirectories[canonicalDirectory] = struct{}{}
	opened, err := root.OpenFile(directory, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		s.warn("skills.unreadable_directory", "Could not open skill directory: "+err.Error(), displayDirectory)
		return
	}
	openedInfo, statErr := opened.Stat()
	if statErr != nil || !openedInfo.IsDir() || !os.SameFile(info, openedInfo) {
		_ = opened.Close()
		s.warn("skills.non_directory", "Opened skill search path is not the selected directory", displayDirectory)
		return
	}
	entries, readErr := opened.Readdir(maxDirectoryEntries + 1)
	closeErr := opened.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		s.warn("skills.unreadable_directory", "Could not read skill directory: "+readErr.Error(), displayDirectory)
		return
	}
	if closeErr != nil {
		s.warn("skills.unreadable_directory", "Could not close skill directory: "+closeErr.Error(), displayDirectory)
		return
	}
	if len(entries) > maxDirectoryEntries {
		s.warn("skills.entry_limit", "Skill directory omitted because its entry limit was exceeded", displayDirectory)
		return
	}
	s.directories++
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.Name() == "SKILL.md" {
			s.loadFile(root, filepath.Join(directory, entry.Name()), filepath.Join(displayDirectory, entry.Name()), source)
			return
		}
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			s.scanLinkedDirectory(ctx, rootedPath(root, filepath.Join(directory, name)), filepath.Join(displayDirectory, name), source, depth+1)
			continue
		}
		if entry.IsDir() {
			s.scan(ctx, root, filepath.Join(directory, name), filepath.Join(displayDirectory, name), source, depth+1)
		}
	}
}

func (s *discoveryState) loadFile(root *os.Root, relative, path string, source Source) {
	physicalPath := rootedPath(root, relative)
	info, err := root.Lstat(relative)
	if err != nil {
		s.warn("skills.unreadable", "Could not inspect skill file: "+err.Error(), path)
		return
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		s.warn("skills.non_regular", "Skill definition is not a regular file or readable file symlink", path)
		return
	}
	resolvedInfo, err := os.Stat(physicalPath)
	if err != nil {
		s.warn("skills.unreadable", "Could not resolve skill file: "+err.Error(), path)
		return
	}
	if !resolvedInfo.Mode().IsRegular() {
		s.warn("skills.non_regular", "Skill definition does not resolve to a regular file", path)
		return
	}
	var opened *os.File
	if info.Mode()&os.ModeSymlink != 0 {
		opened, err = os.OpenFile(physicalPath, os.O_RDONLY|unix.O_NONBLOCK, 0)
	} else {
		opened, err = root.OpenFile(relative, os.O_RDONLY|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		s.warn("skills.unreadable", "Could not open skill file: "+err.Error(), path)
		return
	}
	openedInfo, statErr := opened.Stat()
	if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(resolvedInfo, openedInfo) {
		_ = opened.Close()
		s.warn("skills.non_regular", "Opened skill definition is not the selected regular file", path)
		return
	}
	content, readErr := io.ReadAll(io.LimitReader(opened, int64(maxSkillContentBytes)+1))
	closeErr := opened.Close()
	if readErr != nil {
		s.warn("skills.unreadable", "Could not read skill file: "+readErr.Error(), path)
		return
	}
	if closeErr != nil {
		s.warn("skills.unreadable", "Could not close skill file: "+closeErr.Error(), path)
		return
	}
	if len(content) > maxSkillContentBytes {
		s.warn("skills.oversized", fmt.Sprintf("Skill definition exceeds the %d-byte limit", maxSkillContentBytes), path)
		return
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		s.warn("skills.invalid_text", "Skill definition is not valid UTF-8 text without NUL", path)
		return
	}
	metadata, err := parseSkillFrontmatter(string(content))
	if err != nil {
		s.warn("skills.invalid_frontmatter", "Could not parse skill frontmatter: "+err.Error(), path)
		return
	}
	parent := filepath.Base(filepath.Dir(path))
	name := metadata.Name
	if name == "" {
		name = parent
	}
	if name != parent {
		s.warn("skills.name_mismatch", fmt.Sprintf("Skill name %q does not match directory %q", name, parent), path)
		return
	}
	skill := Skill{
		Name: name, Description: metadata.Description, Content: string(content),
		Source: source, Location: path, DisableModelInvocation: metadata.DisableModelInvocation,
	}
	normalized, err := normalizeSkill(skill)
	if err != nil {
		s.warn("skills.invalid_definition", err.Error(), path)
		return
	}
	if previous, exists := s.seenNames[normalized.Name]; exists {
		code := "skills.duplicate_name"
		if normalized.Name == KitCustomizationName {
			code = "skills.reserved_name"
		}
		s.warn(code, fmt.Sprintf("Skill %q was already loaded from %s and was omitted", normalized.Name, previous), path)
		return
	}
	candidateSkills := append(append([]Skill(nil), s.skills...), normalized)
	candidateRegistry, err := NewRegistry(candidateSkills...)
	if err != nil {
		s.warn("skills.invalid_registry", "Skill could not be added to the registry: "+err.Error(), path)
		return
	}
	if _, err := candidateRegistry.CatalogSection(); err != nil {
		s.warn("skills.catalog_limit", "Skill omitted because it would exceed the model-visible catalog limit", path)
		return
	}
	s.seenNames[normalized.Name] = path
	s.skills = candidateSkills
}

type skillFrontmatter struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
}

func parseSkillFrontmatter(content string) (skillFrontmatter, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return skillFrontmatter{}, nil
	}
	remainder := normalized[4:]
	end := exactDelimiterOffset(remainder)
	if end < 0 {
		return skillFrontmatter{}, errors.New("closing delimiter is missing")
	}
	var metadata skillFrontmatter
	if err := yaml.Unmarshal([]byte(remainder[:end]), &metadata); err != nil {
		return skillFrontmatter{}, err
	}
	metadata.Name = strings.TrimSpace(metadata.Name)
	metadata.Description = strings.TrimSpace(metadata.Description)
	return metadata, nil
}

func exactDelimiterOffset(content string) int {
	offset := 0
	for {
		newline := strings.IndexByte(content[offset:], '\n')
		if newline < 0 {
			if content[offset:] == "---" {
				return offset
			}
			return -1
		}
		if content[offset:offset+newline] == "---" {
			return offset
		}
		offset += newline + 1
	}
}

func rootedPath(root *os.Root, relative string) string {
	return filepath.Join(root.Name(), relative)
}

func (s *discoveryState) warn(code, message, path string) {
	if len(s.diagnostics) >= maxDiscoveryDiagnostics {
		return
	}
	message = boundedDiagnosticText(message)
	digest := sha256.Sum256([]byte(path))
	path = boundedDiagnosticText(path)
	s.diagnostics = append(s.diagnostics, systemprompt.Diagnostic{
		Severity: systemprompt.DiagnosticWarning, Code: code, Message: message,
		Source: systemprompt.Source{ID: "skill-file:" + hex.EncodeToString(digest[:]), Path: path},
	})
}

func boundedDiagnosticText(message string) string {
	message = strings.ReplaceAll(strings.ToValidUTF8(message, "�"), "\x00", "�")
	if len(message) <= maxDiagnosticMessage {
		return message
	}
	message = message[:maxDiagnosticMessage]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}
