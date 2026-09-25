package mcpconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/apphome"
)

const (
	// MaxFileBytes bounds one configuration document.
	MaxFileBytes = 1 << 20
	// MaxServers bounds the merged server set across all files.
	MaxServers = 256
	// MaxDiagnostics bounds retained diagnostics.
	MaxDiagnostics = 128

	maxNameRunes        = 128
	maxDescriptionRunes = 1024
)

// Loader discovers MCP configuration for a session cwd. A Loader is immutable
// after construction and safe for concurrent use by multiple sessions.
type Loader struct {
	kitConfigPath string
	userHome      string
	lookupEnv     func(string) (string, bool)
}

// Option customizes a Loader. Options exist so tests and headless callers can
// supply an environment without mutating process-global state.
type Option func(*Loader)

// WithLookupEnv overrides environment variable resolution.
func WithLookupEnv(lookup func(string) (string, bool)) Option {
	return func(l *Loader) {
		if lookup != nil {
			l.lookupEnv = lookup
		}
	}
}

// WithUserHome overrides the home directory used for `~` expansion. The path
// must be absolute.
func WithUserHome(home string) Option {
	return func(l *Loader) {
		if home != "" {
			l.userHome = filepath.Clean(home)
		}
	}
}

// NewLoader constructs the standard loader from resolved Kit paths. The user
// home directory is resolved once, for `~` expansion, so configuration never
// depends on process-global cwd.
func NewLoader(paths apphome.Paths, opts ...Option) (*Loader, error) {
	kitConfig := filepath.Clean(paths.MCPConfig)
	if paths.MCPConfig == "" || !filepath.IsAbs(kitConfig) {
		return nil, fmt.Errorf("MCP config path must be absolute: %q", paths.MCPConfig)
	}
	loader := &Loader{kitConfigPath: kitConfig, lookupEnv: os.LookupEnv}
	if home, err := os.UserHomeDir(); err == nil {
		loader.userHome = filepath.Clean(home)
	}
	for _, opt := range opts {
		opt(loader)
	}
	if loader.userHome != "" && !filepath.IsAbs(loader.userHome) {
		return nil, fmt.Errorf("user home must be absolute: %q", loader.userHome)
	}
	return loader, nil
}

// Load reads and merges every supported location for an absolute cwd. It
// returns an error only for caller misuse or cancellation; file-level problems
// are reported as diagnostics so one bad file cannot disable every server.
func (l *Loader) Load(ctx context.Context, cwd string) (Result, error) {
	if l == nil {
		return Result{}, errors.New("load MCP config: loader is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(cwd) == "" || !filepath.IsAbs(cwd) {
		return Result{}, fmt.Errorf("load MCP config requires an absolute cwd: %q", cwd)
	}
	cwd = filepath.Clean(cwd)

	candidates := []File{
		{Source: SourceKitUser, Path: l.kitConfigPath},
		{Source: SourceSharedProject, Path: filepath.Join(cwd, ".mcp.json")},
		{Source: SourceKitProject, Path: filepath.Join(cwd, ".agents", "mcp.json")},
	}

	result := Result{Files: make([]File, 0, len(candidates))}
	merged := map[string]*mergedServer{}

	for rank, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if candidate.Path == "" {
			continue
		}
		file := candidate
		document, present, err := readDocument(file.Path)
		file.Present = present
		if err != nil {
			result.addDiagnostic(Diagnostic{Source: file.Source, Path: file.Path, Message: err.Error()})
			result.Files = append(result.Files, file)
			continue
		}
		if !present {
			result.Files = append(result.Files, file)
			continue
		}
		file.Loaded = true
		result.Files = append(result.Files, file)

		for _, name := range sortedKeys(document) {
			if message := validateName(name); message != "" {
				result.addDiagnostic(Diagnostic{Source: file.Source, Path: file.Path, Server: name, Message: message})
				continue
			}
			raw, ok := decodeServer(name, document[name], file, &result)
			if !ok {
				continue
			}
			entry, known := merged[name]
			if !known {
				if len(merged) >= MaxServers {
					evicted, ok := evictLowestPrecedence(merged, rank)
					if !ok {
						result.addDiagnostic(Diagnostic{
							Source:  file.Source,
							Path:    file.Path,
							Server:  name,
							Message: fmt.Sprintf("ignored: more than %d servers are configured", MaxServers),
						})
						continue
					}
					// Higher-precedence files must not be starved by a large
					// lower-precedence file, so drop the lowest-precedence entry.
					delete(merged, evicted)
					result.addDiagnostic(Diagnostic{
						Source:  file.Source,
						Path:    file.Path,
						Server:  evicted,
						Message: fmt.Sprintf("ignored: replaced by higher-precedence server %q at the %d server limit", name, MaxServers),
					})
				}
				entry = &mergedServer{}
				merged[name] = entry
			}
			entry.rank = rank
			entry.merge(raw, file, rank)
		}
	}

	// Build output from the surviving keys so an evicted-then-redefined server
	// is emitted exactly once.
	order := make([]string, 0, len(merged))
	for name := range merged {
		order = append(order, name)
	}
	sort.Strings(order)
	for _, name := range order {
		entry, ok := merged[name]
		if !ok || entry.source.Path == "" {
			continue
		}
		server, ok := l.normalize(name, entry, &result)
		if ok {
			result.Servers = append(result.Servers, server)
		}
	}
	return result, nil
}

// addDiagnostic retains bounded diagnostics, reserving the final slot to record
// that later problems were dropped rather than silently losing them.
func (r *Result) addDiagnostic(d Diagnostic) {
	switch {
	case len(r.Diagnostics) < MaxDiagnostics-1:
		r.Diagnostics = append(r.Diagnostics, d)
	case len(r.Diagnostics) == MaxDiagnostics-1:
		r.Diagnostics = append(r.Diagnostics, Diagnostic{
			Source:  d.Source,
			Path:    d.Path,
			Message: fmt.Sprintf("additional MCP configuration problems were omitted after %d diagnostics", MaxDiagnostics-1),
		})
	}
}

// readDocument reads one configuration file into its raw server entries.
// A missing file is reported as absent rather than as an error.
func readDocument(path string) (map[string]json.RawMessage, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cannot read MCP config: %v", err)
	}
	if !info.Mode().IsRegular() {
		return nil, true, errors.New("MCP config must be a regular file")
	}
	if info.Size() > MaxFileBytes {
		return nil, true, fmt.Errorf("MCP config exceeds %d bytes", MaxFileBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, true, fmt.Errorf("cannot open MCP config: %v", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, true, fmt.Errorf("cannot read MCP config: %v", err)
	}
	if len(data) > MaxFileBytes {
		return nil, true, fmt.Errorf("MCP config exceeds %d bytes", MaxFileBytes)
	}
	if !utf8.Valid(data) {
		return nil, true, errors.New("MCP config is not valid UTF-8")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, true, fmt.Errorf("MCP config must be a JSON object: %v", err)
	}
	raw, ok := root["mcpServers"]
	if !ok || isNull(raw) {
		return map[string]json.RawMessage{}, true, nil
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, true, fmt.Errorf(`"mcpServers" must be an object of server name to definition: %v`, err)
	}
	return servers, true, nil
}

// evictLowestPrecedence selects the entry from the lowest-precedence source to
// drop so a higher-ranked file can still contribute a new server. It returns
// false when no retained entry ranks below rank. Ties break on the last name in
// lexical order so eviction is deterministic.
func evictLowestPrecedence(merged map[string]*mergedServer, rank int) (string, bool) {
	victim := ""
	lowest := 0
	for name, entry := range merged {
		if entry.rank >= rank {
			continue
		}
		if victim == "" || entry.rank < lowest || (entry.rank == lowest && name > victim) {
			victim = name
			lowest = entry.rank
		}
	}
	return victim, victim != ""
}

func validateName(name string) string {
	switch {
	case strings.TrimSpace(name) == "":
		return "ignored: server name must not be blank"
	case utf8.RuneCountInString(name) > maxNameRunes:
		return fmt.Sprintf("ignored: server name must be at most %d characters", maxNameRunes)
	case strings.ContainsRune(name, 0):
		return "ignored: server name must not contain NUL"
	default:
		return ""
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(bytesTrimSpace(raw)) == "null"
}

func bytesTrimSpace(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}
