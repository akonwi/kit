// Package promptcommands discovers and expands user-defined prompt commands.
package promptcommands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/apphome"
	"go.yaml.in/yaml/v4"
	"golang.org/x/sys/unix"
)

const (
	projectPromptsPath  = ".agents/prompts"
	maxCommands         = 128
	maxDirectoryEntries = 1024
	maxTemplateBytes    = 128 << 10
	maxDescriptionBytes = 1024
	maxLocationBytes    = 4 << 10
	fallbackDescription = 60
)

// Source identifies the owner of a prompt command.
type Source string

const (
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

// Command is one immutable prompt template exposed as a palette command.
type Command struct {
	Name        string
	Description string
	Content     string
	Location    string
	Source      Source
}

// Registry is one session's immutable prompt-command snapshot.
type Registry struct {
	commands []Command
	byName   map[string]Command
}

// NewRegistry validates and constructs a deterministic command registry.
func NewRegistry(commands ...Command) (*Registry, error) {
	if len(commands) > maxCommands {
		return nil, fmt.Errorf("prompt command registry exceeds %d commands", maxCommands)
	}
	all := append([]Command(nil), commands...)
	byName := make(map[string]Command, len(all))
	for index, command := range all {
		normalized, err := normalize(command)
		if err != nil {
			return nil, err
		}
		if _, exists := byName[normalized.Name]; exists {
			return nil, fmt.Errorf("prompt command %q is duplicated", normalized.Name)
		}
		all[index] = normalized
		byName[normalized.Name] = normalized
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return &Registry{commands: all, byName: byName}, nil
}

// Commands returns all commands ordered by stable name.
func (r *Registry) Commands() []Command {
	if r == nil {
		return nil
	}
	return append([]Command(nil), r.commands...)
}

// Lookup returns a copy of one named command.
func (r *Registry) Lookup(name string) (Command, bool) {
	if r == nil {
		return Command{}, false
	}
	command, ok := r.byName[name]
	return command, ok
}

// Loader discovers all prompt commands applicable to an explicit session cwd.
type Loader interface {
	Load(context.Context, string) (*Registry, error)
}

// FilesystemLoader discovers user-global and project-local prompt commands.
type FilesystemLoader struct {
	userHome     string
	userHomeInfo os.FileInfo
}

// NewFilesystemLoader constructs a loader rooted in resolved Kit paths.
func NewFilesystemLoader(paths apphome.Paths) (*FilesystemLoader, error) {
	if strings.TrimSpace(paths.Home) == "" || !filepath.IsAbs(paths.Home) ||
		filepath.Clean(paths.Prompts) != filepath.Join(filepath.Clean(paths.Home), "prompts") {
		return nil, errors.New("prompt discovery requires consistent absolute Kit home and prompts paths")
	}
	userHome := filepath.Clean(paths.Home)
	if canonical, err := filepath.EvalSymlinks(userHome); err == nil {
		userHome = canonical
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("resolve Kit home for prompt discovery: %w", err)
	}
	info, err := os.Stat(userHome)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect Kit home for prompt discovery: %w", err)
	}
	return &FilesystemLoader{userHome: userHome, userHomeInfo: info}, nil
}

// Load scans global prompts first, followed by project prompts. First name wins.
func (l *FilesystemLoader) Load(ctx context.Context, cwd string) (*Registry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil || l.userHome == "" {
		return nil, errors.New("prompt loader is not initialized")
	}
	if strings.TrimSpace(cwd) == "" || !filepath.IsAbs(cwd) {
		return nil, errors.New("prompt discovery requires an absolute cwd")
	}
	canonicalCWD, err := filepath.EvalSymlinks(filepath.Clean(cwd))
	if err != nil {
		return nil, fmt.Errorf("resolve prompt discovery cwd: %w", err)
	}
	cwdInfo, err := os.Stat(canonicalCWD)
	if err != nil || !cwdInfo.IsDir() {
		return nil, errors.New("prompt discovery requires an existing directory cwd")
	}
	seen := make(map[string]struct{})
	commands := make([]Command, 0)
	commands = l.scanRoot(ctx, l.userHome, l.userHomeInfo, "prompts", SourceUser, commands, seen)
	commands = l.scanRoot(ctx, canonicalCWD, cwdInfo, projectPromptsPath, SourceProject, commands, seen)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return NewRegistry(commands...)
}

func (l *FilesystemLoader) scanRoot(ctx context.Context, rootPath string, expected os.FileInfo, relative string, source Source, commands []Command, seen map[string]struct{}) []Command {
	if ctx.Err() != nil || len(commands) >= maxCommands {
		return commands
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return commands
	}
	defer root.Close()
	if expected != nil {
		openedInfo, statErr := root.Stat(".")
		if statErr != nil || !os.SameFile(expected, openedInfo) {
			return commands
		}
	}
	info, err := root.Lstat(relative)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return commands
	}
	directory, err := root.OpenFile(relative, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return commands
	}
	openedInfo, statErr := directory.Stat()
	if statErr != nil || !openedInfo.IsDir() {
		_ = directory.Close()
		return commands
	}
	entries, readErr := directory.Readdir(maxDirectoryEntries + 1)
	_ = directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(entries) > maxDirectoryEntries {
		return commands
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if ctx.Err() != nil || len(commands) >= maxCommands {
			break
		}
		if entry.Mode()&os.ModeSymlink != 0 || !entry.Mode().IsRegular() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		if _, exists := seen[name]; exists {
			continue
		}
		command, ok := loadCommand(root, filepath.Join(relative, entry.Name()), source)
		if !ok {
			continue
		}
		seen[command.Name] = struct{}{}
		commands = append(commands, command)
	}
	return commands
}

func loadCommand(root *os.Root, relative string, source Source) (Command, bool) {
	path := filepath.Join(root.Name(), relative)
	if len(path) > maxLocationBytes || !utf8.ValidString(path) {
		return Command{}, false
	}
	info, err := root.Lstat(relative)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Command{}, false
	}
	file, err := root.OpenFile(relative, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return Command{}, false
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil || !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		return Command{}, false
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maxTemplateBytes+1))
	_ = file.Close()
	if readErr != nil || len(raw) > maxTemplateBytes || !utf8.Valid(raw) || strings.IndexByte(string(raw), 0) >= 0 {
		return Command{}, false
	}
	metadata, body, err := parseTemplate(string(raw))
	if err != nil || strings.TrimSpace(body) == "" {
		return Command{}, false
	}
	description := strings.TrimSpace(metadata.Description)
	if description == "" {
		description = deriveDescription(body)
	}
	command, err := normalize(Command{
		Name: strings.TrimSuffix(filepath.Base(path), ".md"), Description: description,
		Content: strings.TrimSpace(body), Location: path, Source: source,
	})
	return command, err == nil
}

type templateFrontmatter struct {
	Description string `yaml:"description"`
}

func parseTemplate(content string) (templateFrontmatter, string, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return templateFrontmatter{}, normalized, nil
	}
	remainder := normalized[4:]
	end := delimiterOffset(remainder)
	if end < 0 {
		return templateFrontmatter{}, "", errors.New("closing frontmatter delimiter is missing")
	}
	var metadata templateFrontmatter
	if err := yaml.Unmarshal([]byte(remainder[:end]), &metadata); err != nil {
		return templateFrontmatter{}, "", err
	}
	bodyStart := end + 3
	if bodyStart < len(remainder) && remainder[bodyStart] == '\n' {
		bodyStart++
	}
	return metadata, strings.TrimSpace(remainder[bodyStart:]), nil
}

func delimiterOffset(content string) int {
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

func deriveDescription(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > fallbackDescription {
			return string(runes[:fallbackDescription]) + "..."
		}
		return line
	}
	return ""
}

func normalize(command Command) (Command, error) {
	command.Name = strings.TrimSpace(command.Name)
	command.Description = strings.TrimSpace(command.Description)
	command.Content = strings.TrimSpace(command.Content)
	if command.Name == "" || len(command.Name) > 128 || !validName(command.Name) {
		return Command{}, errors.New("prompt command name must be 1-128 visible non-whitespace characters")
	}
	if command.Description == "" || len(command.Description) > maxDescriptionBytes || !validRendererText(command.Description) {
		return Command{}, fmt.Errorf("prompt command %q has an invalid description", command.Name)
	}
	if command.Content == "" || len(command.Content) > maxTemplateBytes || !validText(command.Content) {
		return Command{}, fmt.Errorf("prompt command %q has invalid content", command.Name)
	}
	if command.Source != SourceUser && command.Source != SourceProject {
		return Command{}, fmt.Errorf("prompt command %q has invalid source", command.Name)
	}
	if !filepath.IsAbs(command.Location) || len(command.Location) > maxLocationBytes || !validRendererText(command.Location) {
		return Command{}, fmt.Errorf("prompt command %q has invalid location", command.Name)
	}
	return command, nil
}

func validName(name string) bool {
	if !validText(name) {
		return false
	}
	for _, character := range name {
		if unicode.IsSpace(character) || unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || character == '/' || character == '\\' {
			return false
		}
	}
	return true
}

func validText(value string) bool {
	return utf8.ValidString(value) && strings.IndexByte(value, 0) < 0
}

func validRendererText(value string) bool {
	if !validText(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

var (
	positionalPattern = regexp.MustCompile(`\$(\d+)`)
	slicePattern      = regexp.MustCompile(`\$\{@:(\d+)(?::(\d+))?\}`)
)

// Expand parses quoted arguments and substitutes this command's placeholders.
func (command Command) Expand(rawArgs string) (string, error) {
	if len(rawArgs) > maxTemplateBytes || !validText(rawArgs) {
		return "", errors.New("prompt command arguments are invalid or oversized")
	}
	args := ParseArgs(rawArgs)
	result, err := replacePatternBounded(command.Content, positionalPattern, func(placeholder string) string {
		position, parseErr := strconv.ParseUint(placeholder[1:], 10, 64)
		if parseErr != nil || position == 0 || position > uint64(len(args)) {
			return ""
		}
		return args[position-1]
	})
	if err != nil {
		return "", err
	}
	result, err = replacePatternBounded(result, slicePattern, func(placeholder string) string {
		parts := slicePattern.FindStringSubmatch(placeholder)
		startValue, parseErr := strconv.ParseUint(parts[1], 10, 64)
		if parseErr != nil || startValue == 0 || startValue > uint64(len(args)) {
			return ""
		}
		start := int(startValue - 1)
		end := len(args)
		if parts[2] != "" {
			length, lengthErr := strconv.ParseUint(parts[2], 10, 64)
			if lengthErr == nil && length < uint64(len(args)-start) {
				end = start + int(length)
			}
		}
		return strings.Join(args[start:end], " ")
	})
	if err != nil {
		return "", err
	}
	all := strings.Join(args, " ")
	result, err = replaceLiteralBounded(result, "$ARGUMENTS", all)
	if err != nil {
		return "", err
	}
	result, err = replaceLiteralBounded(result, "$@", all)
	if err != nil {
		return "", err
	}
	result = strings.TrimSpace(result)
	if result == "" || len(result) > maxTemplateBytes || !validText(result) {
		return "", errors.New("expanded prompt is empty, invalid, or oversized")
	}
	return result, nil
}

func appendBounded(builder *strings.Builder, value string) bool {
	if len(value) > maxTemplateBytes-builder.Len() {
		return false
	}
	builder.WriteString(value)
	return true
}

func replacePatternBounded(input string, pattern *regexp.Regexp, replacement func(string) string) (string, error) {
	var output strings.Builder
	output.Grow(min(len(input), maxTemplateBytes))
	cursor := 0
	for _, match := range pattern.FindAllStringIndex(input, -1) {
		if !appendBounded(&output, input[cursor:match[0]]) || !appendBounded(&output, replacement(input[match[0]:match[1]])) {
			return "", errors.New("expanded prompt is oversized")
		}
		cursor = match[1]
	}
	if !appendBounded(&output, input[cursor:]) {
		return "", errors.New("expanded prompt is oversized")
	}
	return output.String(), nil
}

func replaceLiteralBounded(input, target, replacement string) (string, error) {
	var output strings.Builder
	output.Grow(min(len(input), maxTemplateBytes))
	cursor := 0
	for {
		index := strings.Index(input[cursor:], target)
		if index < 0 {
			if !appendBounded(&output, input[cursor:]) {
				return "", errors.New("expanded prompt is oversized")
			}
			return output.String(), nil
		}
		index += cursor
		if !appendBounded(&output, input[cursor:index]) || !appendBounded(&output, replacement) {
			return "", errors.New("expanded prompt is oversized")
		}
		cursor = index + len(target)
	}
}

// ParseArgs splits command arguments while preserving whitespace inside matching quotes.
func ParseArgs(value string) []string {
	var args []string
	var current strings.Builder
	var quote rune
	for _, character := range value {
		switch {
		case quote != 0 && character == quote:
			quote = 0
		case quote != 0:
			current.WriteRune(character)
		case character == '\'' || character == '"':
			quote = character
		case character == ' ' || character == '\t':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(character)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
