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
	"sync"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/theme"
	"golang.org/x/sys/unix"
)

// Settings contains the settings currently consumed by the native application.
// Unrecognized JSON fields are retained internally when settings are updated.
type Settings struct {
	Theme string

	fields map[string]json.RawMessage
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
		return Settings{}, nil, fmt.Errorf("read settings %q: not a regular file", s.path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return Settings{}, nil, fmt.Errorf("read settings %q: %w", s.path, err)
	}
	if len(data) > maxFileSize {
		return Settings{}, nil, fmt.Errorf("read settings %q: file exceeds %d bytes", s.path, maxFileSize)
	}
	if !utf8.Valid(data) {
		return Settings{}, nil, fmt.Errorf("decode settings %q: invalid UTF-8", s.path)
	}
	fields, err := decodeObject(data)
	if err != nil {
		return Settings{}, nil, fmt.Errorf("decode settings %q: %w", s.path, err)
	}
	result := Settings{Theme: theme.SystemName, fields: fields}
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
	return cloneSettings(result), warnings, nil
}

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
	file, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|unix.O_NONBLOCK, 0o666)
	if err != nil {
		return fmt.Errorf("write settings %q: %w", s.path, err)
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return fmt.Errorf("inspect settings %q: %w", s.path, statErr)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return fmt.Errorf("write settings %q: not a regular file", s.path)
	}
	if err := file.Truncate(0); err != nil {
		_ = file.Close()
		return fmt.Errorf("truncate settings %q: %w", s.path, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write settings %q: %w", s.path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close settings %q: %w", s.path, err)
	}
	return nil
}

func defaultSettings() Settings {
	return Settings{Theme: theme.SystemName, fields: make(map[string]json.RawMessage)}
}

func cloneSettings(source Settings) Settings {
	result := Settings{Theme: source.Theme, fields: make(map[string]json.RawMessage, len(source.fields))}
	for name, value := range source.fields {
		result.fields[name] = append(json.RawMessage(nil), value...)
	}
	return result
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
