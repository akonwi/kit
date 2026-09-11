package subagent

import (
	"bytes"
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
	"go.yaml.in/yaml/v4"
	"golang.org/x/sys/unix"
)

const (
	projectAgentsPath       = ".kit/agents"
	maxDirectoryEntries     = 1024
	maxDefinitionBytes      = 128 << 10
	maxDiscoveryDiagnostics = 128
	maxDiagnosticBytes      = 4096
)

// LoadResult is one immutable catalog and its non-fatal discovery diagnostics.
type LoadResult struct {
	Catalog     Catalog
	Diagnostics []Diagnostic
}

// Loader discovers definitions applicable to an explicit session cwd.
type Loader interface {
	Load(context.Context, string) (LoadResult, error)
}

// FilesystemLoader discovers user-global then project-local agent definitions.
type FilesystemLoader struct {
	userHome     string
	userHomeInfo os.FileInfo
}

// NewFilesystemLoader constructs the standard loader from resolved Kit paths.
func NewFilesystemLoader(paths apphome.Paths) (*FilesystemLoader, error) {
	if strings.TrimSpace(paths.Home) == "" || !filepath.IsAbs(paths.Home) ||
		filepath.Clean(paths.Agents) != filepath.Join(filepath.Clean(paths.Home), "agents") {
		return nil, errors.New("subagent discovery requires consistent absolute Kit home and agents paths")
	}
	userHome := filepath.Clean(paths.Home)
	if canonical, err := filepath.EvalSymlinks(userHome); err == nil {
		userHome = canonical
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("resolve Kit home for subagent discovery: %w", err)
	}
	info, err := os.Stat(userHome)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect Kit home for subagent discovery: %w", err)
	}
	return &FilesystemLoader{userHome: userHome, userHomeInfo: info}, nil
}

// Load scans non-recursive global and project directories in first-loaded-wins order.
func (l *FilesystemLoader) Load(ctx context.Context, cwd string) (LoadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil || l.userHome == "" {
		return LoadResult{}, errors.New("subagent loader is not initialized")
	}
	if strings.TrimSpace(cwd) == "" || !filepath.IsAbs(cwd) {
		return LoadResult{}, errors.New("subagent discovery requires an absolute cwd")
	}
	canonicalCWD, err := filepath.EvalSymlinks(filepath.Clean(cwd))
	if err != nil {
		return LoadResult{}, fmt.Errorf("resolve subagent discovery cwd: %w", err)
	}
	cwdInfo, err := os.Stat(canonicalCWD)
	if err != nil || !cwdInfo.IsDir() {
		return LoadResult{}, errors.New("subagent discovery requires an existing directory cwd")
	}

	state := discoveryState{seen: make(map[string]string)}
	state.scanRoot(ctx, l.userHome, l.userHomeInfo, "agents", SourceUser)
	state.scanRoot(ctx, canonicalCWD, cwdInfo, projectAgentsPath, SourceProject)
	if err := ctx.Err(); err != nil {
		return LoadResult{}, err
	}
	catalog, err := NewCatalog(state.definitions...)
	if err != nil {
		return LoadResult{}, err
	}
	return LoadResult{Catalog: catalog, Diagnostics: append([]Diagnostic(nil), state.diagnostics...)}, nil
}

type discoveryState struct {
	definitions []Definition
	diagnostics []Diagnostic
	seen        map[string]string
	limitHit    bool
}

func (s *discoveryState) scanRoot(ctx context.Context, rootPath string, expected os.FileInfo, relative string, source SourceKind) {
	if ctx.Err() != nil || s.limitHit {
		return
	}
	if expected != nil {
		current, err := os.Stat(rootPath)
		if err != nil || !os.SameFile(expected, current) {
			s.warn("subagents.root_replaced", "Agent search root changed during discovery and was omitted", Source{Kind: source, Path: filepath.Join(rootPath, relative)})
			return
		}
	}
	directoryPath := filepath.Join(rootPath, relative)
	directoryInfo, err := os.Lstat(directoryPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		s.warn("subagents.unreadable_directory", "Could not inspect agent directory: "+err.Error(), Source{Kind: source, Path: directoryPath})
		return
	}
	if directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() {
		s.warn("subagents.non_directory", "Agent search path is not a regular non-symlink directory", Source{Kind: source, Path: directoryPath})
		return
	}
	directory, err := os.OpenFile(directoryPath, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		s.warn("subagents.unreadable_directory", "Could not open agent directory: "+err.Error(), Source{Kind: source, Path: directoryPath})
		return
	}
	info, statErr := directory.Stat()
	if statErr != nil || !info.IsDir() || !os.SameFile(directoryInfo, info) {
		_ = directory.Close()
		s.warn("subagents.non_directory", "Agent search path is not a directory", Source{Kind: source, Path: directoryPath})
		return
	}
	entries, readErr := directory.ReadDir(maxDirectoryEntries + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		s.warn("subagents.unreadable_directory", "Could not read agent directory: "+readErr.Error(), Source{Kind: source, Path: directoryPath})
		return
	}
	if closeErr != nil {
		s.warn("subagents.unreadable_directory", "Could not close agent directory: "+closeErr.Error(), Source{Kind: source, Path: directoryPath})
		return
	}
	if len(entries) > maxDirectoryEntries {
		s.warn("subagents.entry_limit", "Agent directory omitted because its entry limit was exceeded", Source{Kind: source, Path: directoryPath})
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if len(s.definitions) >= maxDefinitions {
			if !s.limitHit {
				s.limitHit = true
				s.warn("subagents.definition_limit", "Additional agents were omitted because the catalog limit was reached", Source{Kind: source, Path: directoryPath})
			}
			return
		}
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		s.loadFile(filepath.Join(directoryPath, entry.Name()), source)
	}
}

func (s *discoveryState) loadFile(path string, sourceKind SourceKind) {
	source := Source{Kind: sourceKind, Path: path}
	if len(path) > maxLocationBytes || !validRendererText(path) {
		s.warn("subagents.invalid_location", "Agent definition path is invalid or oversized", source)
		return
	}
	linkInfo, err := os.Lstat(path)
	if err != nil {
		s.warn("subagents.unreadable", "Could not inspect agent definition: "+err.Error(), source)
		return
	}
	if !linkInfo.Mode().IsRegular() && linkInfo.Mode()&os.ModeSymlink == 0 {
		s.warn("subagents.non_regular", "Agent definition is not a regular file or readable file symlink", source)
		return
	}
	info, err := os.Stat(path) // Follow readable file symlinks deliberately.
	if err != nil {
		s.warn("subagents.unreadable", "Could not inspect agent definition: "+err.Error(), source)
		return
	}
	if !info.Mode().IsRegular() {
		s.warn("subagents.non_regular", "Agent definition is not a regular file", source)
		return
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		s.warn("subagents.unreadable", "Could not open agent definition: "+err.Error(), source)
		return
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		s.warn("subagents.non_regular", "Opened agent definition is not a regular file", source)
		return
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, int64(maxDefinitionBytes)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		s.warn("subagents.unreadable", "Could not read agent definition", source)
		return
	}
	if len(raw) > maxDefinitionBytes {
		s.warn("subagents.oversized", fmt.Sprintf("Agent definition exceeds the %d-byte limit", maxDefinitionBytes), source)
		return
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		s.warn("subagents.invalid_text", "Agent definition is not valid UTF-8 text without NUL", source)
		return
	}
	metadata, instructions, err := parseDefinition(string(raw))
	if err != nil {
		s.warn("subagents.invalid_frontmatter", "Could not parse agent frontmatter: "+err.Error(), source)
		return
	}
	definition, err := normalizeDefinition(Definition{
		Name: metadata.Name, Description: metadata.Description, Model: metadata.Model,
		Instructions: instructions, Source: source,
	})
	if err != nil {
		s.warn("subagents.invalid_definition", err.Error(), source)
		return
	}
	if previous, exists := s.seen[definition.Name]; exists {
		s.warn("subagents.duplicate_name", fmt.Sprintf("Agent %q was already loaded from %s and was omitted", definition.Name, previous), source)
		return
	}
	s.seen[definition.Name] = path
	s.definitions = append(s.definitions, definition)
}

type definitionFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Model       string `yaml:"model"`
}

func parseDefinition(content string) (definitionFrontmatter, string, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return definitionFrontmatter{}, "", errors.New("opening delimiter is missing")
	}
	remainder := normalized[4:]
	end := frontmatterDelimiterOffset(remainder)
	if end < 0 {
		return definitionFrontmatter{}, "", errors.New("closing delimiter is missing")
	}
	decoder := yaml.NewDecoder(strings.NewReader(remainder[:end]))
	decoder.KnownFields(true)
	var metadata definitionFrontmatter
	if err := decoder.Decode(&metadata); err != nil {
		return definitionFrontmatter{}, "", err
	}
	metadata.Name = strings.TrimSpace(metadata.Name)
	metadata.Description = strings.TrimSpace(metadata.Description)
	metadata.Model = strings.TrimSpace(metadata.Model)
	bodyStart := end + 3
	if bodyStart < len(remainder) && remainder[bodyStart] == '\n' {
		bodyStart++
	}
	return metadata, strings.TrimSpace(remainder[bodyStart:]), nil
}

func frontmatterDelimiterOffset(content string) int {
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

func (s *discoveryState) warn(code, message string, source Source) {
	if len(s.diagnostics) >= maxDiscoveryDiagnostics {
		return
	}
	message = boundedDiagnostic(message)
	source.Path = boundedDiagnostic(source.Path)
	if source.Path == "" {
		digest := sha256.Sum256([]byte(message))
		source.Path = "diagnostic:" + hex.EncodeToString(digest[:])
	}
	s.diagnostics = append(s.diagnostics, Diagnostic{Severity: DiagnosticWarning, Code: code, Message: message, Source: source})
}

func boundedDiagnostic(value string) string {
	value = strings.ReplaceAll(strings.ToValidUTF8(value, "�"), "\x00", "�")
	if len(value) <= maxDiagnosticBytes {
		return value
	}
	value = value[:maxDiagnosticBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
