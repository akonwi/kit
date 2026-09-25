// Package theme owns Kit's renderer-neutral custom theme format.
package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	// SystemName identifies the terminal-derived built-in theme.
	SystemName = "system"
	// MaxFileBytes bounds custom theme files read across the filesystem boundary.
	MaxFileBytes = 256 << 10
)

// Color is a parsed sRGB theme color. Alpha is retained for renderer-specific
// handling; terminal renderers may need to composite it over a background.
type Color struct {
	R uint8
	G uint8
	B uint8
	A uint8
}

// String returns the normalized compatible color spelling.
func (c Color) String() string {
	if c.A == 0 && c.R == 0 && c.G == 0 && c.B == 0 {
		return "transparent"
	}
	if c.A == 0xff {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

// Definition is one compatible partial custom-theme definition. Unknown color
// role names remain in their section maps for future consumers. Extra retains
// unknown top-level fields without interpreting them.
type Definition struct {
	Tokens        map[string]Color
	SyntaxPalette map[string]Color
	Extra         map[string]json.RawMessage
}

// Resolved contains renderer-neutral semantic colors after applying overrides.
type Resolved struct {
	Tokens        map[string]Color
	SyntaxPalette map[string]Color
}

// Diagnostic identifies one invalid color override omitted during parsing.
type Diagnostic struct {
	Section string
	Name    string
	Err     error
}

func (d Diagnostic) Error() string {
	if d.Name == "" {
		return fmt.Sprintf("%s: %v", d.Section, d.Err)
	}
	return fmt.Sprintf("%s.%q: %v", d.Section, d.Name, d.Err)
}

// Parse decodes a compatible custom theme. Invalid individual color values are
// returned as diagnostics and omitted so callers can retain semantic fallbacks.
func Parse(data []byte) (Definition, []Diagnostic, error) {
	if !utf8.Valid(data) {
		return Definition{}, nil, errors.New("decode theme: invalid UTF-8")
	}
	raw, err := decodeRawObject(data)
	if err != nil {
		return Definition{}, nil, fmt.Errorf("decode theme: %w", err)
	}

	definition := Definition{
		Tokens:        make(map[string]Color),
		SyntaxPalette: make(map[string]Color),
		Extra:         make(map[string]json.RawMessage),
	}
	var diagnostics []Diagnostic
	for name, value := range raw {
		switch name {
		case "tokens":
			section, sectionDiagnostics, err := parseColorSection(name, value)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Section: name, Err: err})
				continue
			}
			definition.Tokens = section
			diagnostics = append(diagnostics, sectionDiagnostics...)
		case "syntaxPalette":
			section, sectionDiagnostics, err := parseColorSection(name, value)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Section: name, Err: err})
				continue
			}
			definition.SyntaxPalette = section
			diagnostics = append(diagnostics, sectionDiagnostics...)
		default:
			definition.Extra[name] = append(json.RawMessage(nil), value...)
		}
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Section != diagnostics[j].Section {
			return diagnostics[i].Section < diagnostics[j].Section
		}
		return diagnostics[i].Name < diagnostics[j].Name
	})
	return definition, diagnostics, nil
}

func decodeRawObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return nil, errors.New("expected an object")
	}
	result := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("expected an object key")
		}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		result[key] = append(json.RawMessage(nil), value...)
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
	return result, nil
}

func parseColorSection(section string, data json.RawMessage) (map[string]Color, []Diagnostic, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return make(map[string]Color), nil, nil
	}
	raw, err := decodeRawObject(data)
	if err != nil {
		return nil, nil, fmt.Errorf("must be an object: %w", err)
	}
	colors := make(map[string]Color, len(raw))
	diagnostics := make([]Diagnostic, 0)
	for name, value := range raw {
		var spelling string
		if err := json.Unmarshal(value, &spelling); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Section: section, Name: name, Err: errors.New("color must be a string")})
			continue
		}
		color, err := ParseColor(spelling)
		knownOpaqueRole := (section == "tokens" && IsKnownToken(name) && name != TokenBackgroundTransparent) ||
			(section == "syntaxPalette" && IsKnownSyntaxRole(name))
		if err == nil && color.A == 0 && knownOpaqueRole {
			err = errors.New("fully transparent color is not permitted for this role")
		}
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Section: section, Name: name, Err: err})
			continue
		}
		colors[name] = color
	}
	return colors, diagnostics, nil
}

// ParseColor accepts the color spellings supported by existing Kit themes.
func ParseColor(value string) (Color, error) {
	if value == "transparent" {
		return Color{}, nil
	}
	if !strings.HasPrefix(value, "#") {
		return Color{}, fmt.Errorf("unsupported color %q", value)
	}
	hex := value[1:]
	switch len(hex) {
	case 3, 4:
		expanded := make([]byte, 0, len(hex)*2)
		for index := range len(hex) {
			expanded = append(expanded, hex[index], hex[index])
		}
		hex = string(expanded)
	case 6, 8:
	default:
		return Color{}, fmt.Errorf("unsupported color %q", value)
	}
	components := [4]uint8{0, 0, 0, 0xff}
	for index := 0; index < len(hex)/2; index++ {
		component, ok := parseHexByte(hex[index*2 : index*2+2])
		if !ok {
			return Color{}, fmt.Errorf("unsupported color %q", value)
		}
		components[index] = component
	}
	return Color{R: components[0], G: components[1], B: components[2], A: components[3]}, nil
}

func parseHexByte(value string) (uint8, bool) {
	var result uint8
	for index := range len(value) {
		result <<= 4
		digit := value[index]
		switch {
		case digit >= '0' && digit <= '9':
			result |= digit - '0'
		case digit >= 'a' && digit <= 'f':
			result |= digit - 'a' + 10
		case digit >= 'A' && digit <= 'F':
			result |= digit - 'A' + 10
		default:
			return 0, false
		}
	}
	return result, true
}

// Resolve overlays a partial definition on a semantic fallback without
// mutating either input.
func Resolve(base Resolved, definition Definition) Resolved {
	resolved := Resolved{
		Tokens:        cloneColors(base.Tokens),
		SyntaxPalette: cloneColors(base.SyntaxPalette),
	}
	for name, color := range definition.Tokens {
		resolved.Tokens[name] = color
	}
	for name, color := range definition.SyntaxPalette {
		resolved.SyntaxPalette[name] = color
	}
	return resolved
}

func cloneColors(source map[string]Color) map[string]Color {
	result := make(map[string]Color, len(source))
	for name, color := range source {
		result[name] = color
	}
	return result
}

// Discover returns custom theme names from directory in deterministic order.
// A missing directory is equivalent to no installed custom themes.
func Discover(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("discover themes: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if !ValidName(name) || name == SystemName {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// Load reads and parses a named theme from directory.
func Load(directory, name string) (Definition, []Diagnostic, error) {
	if !ValidName(name) || name == SystemName {
		return Definition{}, nil, fmt.Errorf("invalid custom theme name %q", name)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Definition{}, nil, fmt.Errorf("open theme directory: %w", err)
	}
	defer root.Close()
	file, err := root.OpenFile(name+".json", os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return Definition{}, nil, fmt.Errorf("open theme %q: %w", name, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Definition{}, nil, fmt.Errorf("inspect theme %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return Definition{}, nil, fmt.Errorf("open theme %q: not a regular file", name)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return Definition{}, nil, fmt.Errorf("read theme %q: %w", name, err)
	}
	if len(data) > MaxFileBytes {
		return Definition{}, nil, fmt.Errorf("read theme %q: file exceeds %d bytes", name, MaxFileBytes)
	}
	definition, diagnostics, err := Parse(data)
	if err != nil {
		return Definition{}, nil, fmt.Errorf("parse theme %q: %w", name, err)
	}
	return definition, diagnostics, nil
}

// ValidName reports whether name can safely identify one theme file.
func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) ||
		strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\x00') || filepath.Base(name) != name {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) || isBidiControl(character) {
			return false
		}
	}
	return true
}

func isBidiControl(character rune) bool {
	return character == '\u061c' || character == '\u200e' || character == '\u200f' ||
		(character >= '\u202a' && character <= '\u202e') ||
		(character >= '\u2066' && character <= '\u2069')
}
