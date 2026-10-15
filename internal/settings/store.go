// Package settings owns Kit's shared JSON settings file.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/serveraccess"
	"github.com/akonwi/kit/internal/serverlisten"
	"github.com/akonwi/kit/internal/theme"
	"golang.org/x/sys/unix"
)

// Settings contains the settings currently consumed by the native application.
// Unrecognized JSON fields are retained internally when settings are updated.
type Settings struct {
	Theme           string
	DefaultModel    string
	CompactionModel string
	DiffWrapLines   bool
	ModelOverrides  map[string]ModelOverride
	// PromptCacheRetention is "short" (the default) or "long".
	PromptCacheRetention string
	// ReadClaudeConfigs enables discovery of Claude Code configuration, which is
	// a project's .claude/commands directory. It defaults to true.
	ReadClaudeConfigs bool
	// ServerListen is the daemon listen address from server.listen, or empty
	// for the default loopback address on an ephemeral port.
	ServerListen string
	// ServerAllowedHosts are canonical hosts from server.allowedHosts that the
	// daemon accepts in Host headers in addition to loopback addresses.
	ServerAllowedHosts []string
	// ServerAllowedOrigins are canonical origins from server.allowedOrigins
	// that the daemon accepts in Origin headers in addition to its own host.
	ServerAllowedOrigins []string

	fields map[string]json.RawMessage
}

// ModelOverride contains user request defaults for one exact model selector.
type ModelOverride struct {
	ContextWindow int `json:"contextWindow,omitempty"`
}

// Warning describes a setting that could not be used and fell back safely.
type Warning struct {
	Field string
	Err   error
}

func (w Warning) Error() string {
	return fmt.Sprintf("settings field %q: %v", w.Field, w.Err)
}

const maxFileSize = 1 << 20

// ErrMalformed marks a settings file that cannot be read or updated as it
// stands until the user fixes it.
var ErrMalformed = errors.New("settings file is malformed")

type malformedError struct{ error }

func (malformedError) Is(target error) bool { return target == ErrMalformed }

func (e malformedError) Unwrap() error { return e.error }

func malformed(err error) error { return malformedError{err} }

// Prompt cache retention values. Providers map them to their own lifetimes.
// Anthropic retains short entries for five minutes and long entries for one
// hour. Public OpenAI Responses maps long to 24h, and short to in_memory, only
// for reviewed earlier models. GPT-5.6 and later ignore it and use a 30-minute
// prompt_cache_options.ttl.
const (
	PromptCacheShort = "short"
	PromptCacheLong  = "long"
)

// Store serializes settings operations performed through one application-owned
// instance. Independent processes use last-writer-wins persistence.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore returns a settings store for an absolute path.
func NewStore(path string) (*Store, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("settings path must be absolute: %q", path)
	}
	return &Store{path: filepath.Clean(path)}, nil
}

// Load reads settings. A missing file resolves to defaults. Invalid individual
// fields produce warnings; malformed documents return an error.
func (s *Store) Load() (Settings, []Warning, error) {
	if s == nil {
		return Settings{}, nil, errors.New("load settings: store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Patch changes the settings that govern the server's behavior. Nil fields
// are left unchanged, and an empty string clears a model.
type Patch struct {
	DefaultModel    *string
	CompactionModel *string
	// PromptCacheRetention is PromptCacheShort or PromptCacheLong.
	PromptCacheRetention *string
	ReadClaudeConfigs    *bool
	// ContextWindows sets each model selector's context-window override; zero
	// removes it.
	ContextWindows map[string]int
}

func (patch Patch) empty() bool {
	return patch.DefaultModel == nil && patch.CompactionModel == nil &&
		patch.PromptCacheRetention == nil && patch.ReadClaudeConfigs == nil && len(patch.ContextWindows) == 0
}

// Update applies patch after re-reading the latest settings and writes them
// once. Override entries it does not touch, including ones Load ignores as
// invalid, are kept. A value it cannot apply fails the whole patch.
func (s *Store) Update(patch Patch) (Settings, error) {
	if s == nil {
		return Settings{}, errors.New("update settings: store is nil")
	}
	for name, selector := range map[string]*string{"defaultModel": patch.DefaultModel, "compactionModel": patch.CompactionModel} {
		if selector != nil && *selector != "" && !validModelSelector(*selector) {
			return Settings{}, fmt.Errorf("update settings: %s %q is not an exact provider/model selector", name, *selector)
		}
	}
	if retention := patch.PromptCacheRetention; retention != nil && *retention != PromptCacheShort && *retention != PromptCacheLong {
		return Settings{}, fmt.Errorf("update settings: prompt cache retention must be %q or %q", PromptCacheShort, PromptCacheLong)
	}
	for selector, contextWindow := range patch.ContextWindows {
		if !validModelSelector(selector) || contextWindow < 0 {
			return Settings{}, fmt.Errorf("update settings: invalid context-window override %q=%d", selector, contextWindow)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, _, err := s.load()
	if err != nil {
		return Settings{}, err
	}
	if patch.empty() {
		return cloneSettings(current), nil
	}
	setString := func(name string, value *string) {
		switch {
		case value == nil:
		case *value == "":
			delete(current.fields, name)
		default:
			current.fields[name], _ = json.Marshal(*value)
		}
	}
	setString("defaultModel", patch.DefaultModel)
	setString("compactionModel", patch.CompactionModel)
	setString("promptCacheRetention", patch.PromptCacheRetention)
	if patch.ReadClaudeConfigs != nil {
		current.fields["readClaudeConfigs"], _ = json.Marshal(*patch.ReadClaudeConfigs)
	}
	if len(patch.ContextWindows) > 0 {
		overrides := map[string]json.RawMessage{}
		if raw, ok := current.fields["modelOverrides"]; ok {
			if err := json.Unmarshal(raw, &overrides); err != nil {
				return Settings{}, malformed(fmt.Errorf("update settings: settings field %q is not an object", "modelOverrides"))
			}
			if overrides == nil {
				overrides = map[string]json.RawMessage{}
			}
		}
		for selector, contextWindow := range patch.ContextWindows {
			// Keep an override's other keys; replace one that is not an object.
			entry := map[string]json.RawMessage{}
			if raw, ok := overrides[selector]; ok && (json.Unmarshal(raw, &entry) != nil || entry == nil) {
				entry = map[string]json.RawMessage{}
			}
			if contextWindow == 0 {
				delete(entry, "contextWindow")
			} else {
				entry["contextWindow"], _ = json.Marshal(contextWindow)
			}
			if len(entry) == 0 {
				delete(overrides, selector)
			} else {
				overrides[selector], _ = json.Marshal(entry)
			}
		}
		if len(overrides) == 0 {
			delete(current.fields, "modelOverrides")
		} else {
			current.fields["modelOverrides"], _ = json.Marshal(overrides)
		}
	}
	if err := s.write(current.fields); err != nil {
		return Settings{}, err
	}
	updated, _, err := s.load()
	if err != nil {
		return Settings{}, err
	}
	return cloneSettings(updated), nil
}

// UpdateDiffWrapLines persists the working-tree diff wrapping preference.
func (s *Store) UpdateDiffWrapLines(enabled bool) (Settings, error) {
	if s == nil {
		return Settings{}, errors.New("update diff wrapping: store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, _, err := s.load()
	if err != nil {
		return Settings{}, err
	}
	current.DiffWrapLines = enabled
	encoded, _ := json.Marshal(enabled)
	current.fields["diffWrapLines"] = encoded
	if err := s.write(current.fields); err != nil {
		return Settings{}, err
	}
	return cloneSettings(current), nil
}

// ServerAccessChange is a change to server.listen and server.allowedHosts.
type ServerAccessChange struct {
	// SetListen replaces server.listen with Listen, or removes it when Listen
	// is empty.
	SetListen bool
	Listen    string
	// AddHosts are appended to server.allowedHosts unless already listed.
	AddHosts []string
	// RemoveHosts are removed from server.allowedHosts.
	RemoveHosts []string
}

// UpdateServerAccess applies change after re-reading the latest settings. It
// edits the stored allowedHosts list in place, so entries it does not touch,
// including ones Load ignores as invalid, are kept, as are other server
// fields. An emptied list or server object is removed.
func (s *Store) UpdateServerAccess(change ServerAccessChange) (Settings, error) {
	if s == nil {
		return Settings{}, errors.New("update server access: store is nil")
	}
	if change.SetListen && change.Listen != "" {
		address, err := serverlisten.Parse(change.Listen)
		if err != nil {
			return Settings{}, fmt.Errorf("update server access: %w", err)
		}
		change.Listen = address.String()
	}
	canonical := func(hosts []string) ([]string, error) {
		parsed := make([]string, 0, len(hosts))
		for _, host := range hosts {
			value, err := serveraccess.ParseHost(host)
			if err != nil {
				return nil, fmt.Errorf("update server access: %w", err)
			}
			parsed = append(parsed, value)
		}
		return parsed, nil
	}
	add, err := canonical(change.AddHosts)
	if err != nil {
		return Settings{}, err
	}
	remove, err := canonical(change.RemoveHosts)
	if err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, _, err := s.load()
	if err != nil {
		return Settings{}, err
	}
	server := map[string]json.RawMessage{}
	if raw, ok := current.fields["server"]; ok {
		if err := json.Unmarshal(raw, &server); err != nil || server == nil {
			return Settings{}, fmt.Errorf("update server access: settings field %q is not an object", "server")
		}
	}
	if change.SetListen {
		if change.Listen == "" {
			delete(server, "listen")
		} else {
			server["listen"], _ = json.Marshal(change.Listen)
		}
	}
	if len(add) > 0 || len(remove) > 0 {
		var entries []json.RawMessage
		if raw, ok := server["allowedHosts"]; ok {
			if err := json.Unmarshal(raw, &entries); err != nil {
				return Settings{}, fmt.Errorf("update server access: settings field %q is not an array", "server.allowedHosts")
			}
		}
		// listed reports an entry's canonical host, if it is a valid one.
		listed := func(entry json.RawMessage) (string, bool) {
			var value string
			if json.Unmarshal(entry, &value) != nil {
				return "", false
			}
			host, err := serveraccess.ParseHost(value)
			return host, err == nil
		}
		entries = slices.DeleteFunc(entries, func(entry json.RawMessage) bool {
			host, ok := listed(entry)
			return ok && slices.Contains(remove, host)
		})
		for _, host := range add {
			if !slices.ContainsFunc(entries, func(entry json.RawMessage) bool {
				existing, ok := listed(entry)
				return ok && existing == host
			}) {
				encoded, _ := json.Marshal(host)
				entries = append(entries, encoded)
			}
		}
		if len(entries) == 0 {
			delete(server, "allowedHosts")
		} else {
			server["allowedHosts"], _ = json.Marshal(entries)
		}
	}
	if len(server) == 0 {
		delete(current.fields, "server")
	} else {
		current.fields["server"], _ = json.Marshal(server)
	}
	if err := s.write(current.fields); err != nil {
		return Settings{}, err
	}
	updated, _, err := s.load()
	if err != nil {
		return Settings{}, err
	}
	return cloneSettings(updated), nil
}

// UpdateTheme validates and persists the selected theme after re-reading the
// latest settings. The returned settings can be applied immediately by callers.
func (s *Store) UpdateTheme(name string) (Settings, error) {
	if s == nil {
		return Settings{}, errors.New("update settings theme: store is nil")
	}
	if !validThemeName(name) {
		return Settings{}, fmt.Errorf("update settings theme: invalid theme name %q", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, _, err := s.load()
	if err != nil {
		return Settings{}, err
	}
	current.Theme = name
	encodedName, _ := json.Marshal(name)
	current.fields["theme"] = encodedName
	if err := s.write(current.fields); err != nil {
		return Settings{}, err
	}
	return cloneSettings(current), nil
}

func (s *Store) load() (Settings, []Warning, error) {
	if s.path == "" {
		return Settings{}, nil, errors.New("load settings: path is required")
	}
	file, err := os.OpenFile(s.path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return defaultSettings(), nil, nil
	}
	if err != nil {
		return Settings{}, nil, fmt.Errorf("read settings %q: %w", s.path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Settings{}, nil, fmt.Errorf("inspect settings %q: %w", s.path, err)
	}
	if !info.Mode().IsRegular() {
		return Settings{}, nil, malformed(fmt.Errorf("read settings %q: not a regular file", s.path))
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return Settings{}, nil, fmt.Errorf("read settings %q: %w", s.path, err)
	}
	if len(data) > maxFileSize {
		return Settings{}, nil, malformed(fmt.Errorf("read settings %q: file exceeds %d bytes", s.path, maxFileSize))
	}
	if !utf8.Valid(data) {
		return Settings{}, nil, malformed(fmt.Errorf("decode settings %q: invalid UTF-8", s.path))
	}
	fields, err := decodeObject(data)
	if err != nil {
		return Settings{}, nil, malformed(fmt.Errorf("decode settings %q: %w", s.path, err))
	}
	result := Settings{Theme: theme.SystemName, PromptCacheRetention: PromptCacheShort, ReadClaudeConfigs: true, fields: fields}
	var warnings []Warning
	if raw, ok := fields["theme"]; ok {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			warnings = append(warnings, Warning{Field: "theme", Err: errors.New("must be a string; using system")})
		} else if !validThemeName(name) {
			warnings = append(warnings, Warning{Field: "theme", Err: errors.New("is not a valid theme name; using system")})
		} else {
			result.Theme = name
		}
	}
	if raw, ok := fields["diffWrapLines"]; ok {
		if err := json.Unmarshal(raw, &result.DiffWrapLines); err != nil {
			warnings = append(warnings, Warning{Field: "diffWrapLines", Err: errors.New("must be a boolean; using clip mode")})
			result.DiffWrapLines = false
		}
	}
	if raw, ok := fields["defaultModel"]; ok {
		var selector string
		if err := json.Unmarshal(raw, &selector); err != nil || !validModelSelector(selector) {
			warnings = append(warnings, Warning{Field: "defaultModel", Err: errors.New("must be an exact provider/model selector; ignoring")})
		} else {
			result.DefaultModel = selector
		}
	}
	if raw, ok := fields["compactionModel"]; ok {
		var selector string
		if err := json.Unmarshal(raw, &selector); err != nil || !validModelSelector(selector) {
			warnings = append(warnings, Warning{Field: "compactionModel", Err: errors.New("must be an exact provider/model selector; ignoring")})
		} else {
			result.CompactionModel = selector
		}
	}
	if raw, ok := fields["promptCacheRetention"]; ok {
		var retention string
		if err := json.Unmarshal(raw, &retention); err != nil || (retention != PromptCacheShort && retention != PromptCacheLong) {
			warnings = append(warnings, Warning{Field: "promptCacheRetention", Err: errors.New(`must be "short" or "long"; using short`)})
		} else {
			result.PromptCacheRetention = retention
		}
	}
	if raw, ok := fields["readClaudeConfigs"]; ok {
		if err := json.Unmarshal(raw, &result.ReadClaudeConfigs); err != nil {
			warnings = append(warnings, Warning{Field: "readClaudeConfigs", Err: errors.New("must be a boolean; using true")})
			result.ReadClaudeConfigs = true
		}
	}
	if raw, ok := fields["server"]; ok {
		var server map[string]json.RawMessage
		if err := json.Unmarshal(raw, &server); err != nil || server == nil {
			warnings = append(warnings, Warning{Field: "server", Err: errors.New("must be an object; ignoring")})
		} else {
			if encoded, ok := server["listen"]; ok {
				var listen string
				if err := json.Unmarshal(encoded, &listen); err != nil {
					warnings = append(warnings, Warning{Field: "server.listen", Err: errors.New("must be a string; using " + serverlisten.Default)})
				} else if address, err := serverlisten.Parse(listen); err != nil {
					warnings = append(warnings, Warning{Field: "server.listen", Err: fmt.Errorf("%w; using %s", err, serverlisten.Default)})
				} else {
					result.ServerListen = address.String()
				}
			}
			var listWarnings []Warning
			result.ServerAllowedHosts, listWarnings = loadStringList(server, "allowedHosts", serveraccess.ParseHost)
			warnings = append(warnings, listWarnings...)
			result.ServerAllowedOrigins, listWarnings = loadStringList(server, "allowedOrigins", serveraccess.ParseOrigin)
			warnings = append(warnings, listWarnings...)
		}
	}
	if raw, ok := fields["modelOverrides"]; ok {
		var overrides map[string]json.RawMessage
		if err := json.Unmarshal(raw, &overrides); err != nil {
			warnings = append(warnings, Warning{Field: "modelOverrides", Err: errors.New("must be an object; ignoring")})
		} else {
			result.ModelOverrides = make(map[string]ModelOverride, len(overrides))
			for selector, encoded := range overrides {
				// An entry with only keys this version does not know overrides
				// nothing it uses.
				var keys map[string]json.RawMessage
				if json.Unmarshal(encoded, &keys) == nil && keys != nil {
					if _, ok := keys["contextWindow"]; !ok && validModelSelector(selector) {
						continue
					}
				}
				var override ModelOverride
				err := json.Unmarshal(encoded, &override)
				if err != nil || !validModelSelector(selector) || override.ContextWindow <= 0 {
					warnings = append(warnings, Warning{Field: "modelOverrides." + selector, Err: errors.New("must contain a positive integer contextWindow for an exact provider/model selector; ignoring")})
					continue
				}
				result.ModelOverrides[selector] = override
			}
		}
	}
	return cloneSettings(result), warnings, nil
}

// write replaces the settings file atomically, so readers in this or another
// process never see it half-written. A symlinked settings file stays a
// symlink: the file it names is replaced. An existing file keeps its mode.
func (s *Store) write(fields map[string]json.RawMessage) error {
	data, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings %q: %w", s.path, err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o777); err != nil {
		return fmt.Errorf("create settings directory %q: %w", directory, err)
	}
	target, mode := s.path, os.FileMode(0o644)
	if resolved, err := filepath.EvalSymlinks(s.path); err == nil {
		target = resolved
		info, err := os.Stat(target)
		if err != nil {
			return fmt.Errorf("inspect settings %q: %w", s.path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("write settings %q: not a regular file", s.path)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("resolve settings %q: %w", s.path, err)
	} else if info, lstatErr := os.Lstat(s.path); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("write settings %q: the symlink's target does not exist", s.path)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".settings-*.json")
	if err != nil {
		return fmt.Errorf("write settings %q: %w", s.path, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporary.Name())
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write settings %q: %w", s.path, err)
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write settings %q: %w", s.path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close settings %q: %w", s.path, err)
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		return fmt.Errorf("replace settings %q: %w", s.path, err)
	}
	committed = true
	return nil
}

// loadStringList decodes server.<name> as a list of strings, canonicalizing
// each with parse. Invalid entries are dropped with a warning; duplicates are
// removed.
func loadStringList(server map[string]json.RawMessage, name string, parse func(string) (string, error)) ([]string, []Warning) {
	encoded, ok := server[name]
	if !ok {
		return nil, nil
	}
	field := "server." + name
	var values []string
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil, []Warning{{Field: field, Err: errors.New("must be a list of strings; ignoring")}}
	}
	var result []string
	var warnings []Warning
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		canonical, err := parse(value)
		if err != nil {
			warnings = append(warnings, Warning{Field: field, Err: fmt.Errorf("%w; ignoring", err)})
			continue
		}
		if !seen[canonical] {
			seen[canonical] = true
			result = append(result, canonical)
		}
	}
	return result, warnings
}

func defaultSettings() Settings {
	return Settings{Theme: theme.SystemName, PromptCacheRetention: PromptCacheShort, ReadClaudeConfigs: true, fields: make(map[string]json.RawMessage)}
}

func cloneSettings(source Settings) Settings {
	result := Settings{Theme: source.Theme, DefaultModel: source.DefaultModel, CompactionModel: source.CompactionModel, DiffWrapLines: source.DiffWrapLines,
		PromptCacheRetention: source.PromptCacheRetention, ReadClaudeConfigs: source.ReadClaudeConfigs, ServerListen: source.ServerListen,
		ServerAllowedHosts: slices.Clone(source.ServerAllowedHosts), ServerAllowedOrigins: slices.Clone(source.ServerAllowedOrigins), fields: make(map[string]json.RawMessage, len(source.fields))}
	if source.ModelOverrides != nil {
		result.ModelOverrides = make(map[string]ModelOverride, len(source.ModelOverrides))
		for selector, override := range source.ModelOverrides {
			result.ModelOverrides[selector] = override
		}
	}
	for name, value := range source.fields {
		result.fields[name] = append(json.RawMessage(nil), value...)
	}
	return result
}

func validModelSelector(selector string) bool {
	provider, model, found := strings.Cut(selector, "/")
	return found && strings.TrimSpace(provider) == provider && strings.TrimSpace(model) == model && provider != "" && model != ""
}

func validThemeName(name string) bool {
	return name == theme.SystemName || theme.ValidName(name)
}

func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return nil, errors.New("expected an object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key := keyToken.(string)
		if _, duplicate := fields[key]; duplicate {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = append(json.RawMessage(nil), value...)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return nil, errors.New("multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return fields, nil
}
