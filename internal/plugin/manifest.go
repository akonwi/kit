// Package plugin owns external plugin discovery and subprocess infrastructure.
// Plugins are trusted same-user code, not sandboxed processes. The session host
// owns composition, contribution registration, and user-visible diagnostics.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/apphome"
)

// MaxManifestBytes bounds an installation's launch document before decoding.
const MaxManifestBytes = 1024 * 1024

// Source identifies the discovery scope of an installation.
type Source string

const (
	// User identifies plugins under the resolved v2 home.
	User Source = "user"
	// Project identifies plugins under the session cwd's .kit/plugins directory.
	Project Source = "project"
)

// Manifest is the public manifest-v1 launch document.
type Manifest struct {
	Schema          string         `json:"$schema,omitempty"`
	ManifestVersion int            `json:"manifestVersion"`
	ID              string         `json:"id"`
	Name            string         `json:"name,omitempty"`
	Transport       StdioTransport `json:"transport"`
}

// StdioTransport launches a command directly, with literal arguments.
type StdioTransport struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Installation is a validated manifest and its discovery identity. Root is the
// absolute installation directory, even when reached through a child symlink.
type Installation struct {
	Source       Source
	Name         string
	Root         string
	ManifestPath string
	Manifest     Manifest
}

// Diagnostic describes an isolated discovery failure. Duplicate diagnostics
// include both paths, so callers can retain actionable errors across attachment.
type Diagnostic struct {
	Source            Source
	Phase             string
	PluginID          string
	ManifestPath      string
	OtherManifestPath string
	Message           string
}

// Discovery selects explicit scope directories without consulting process cwd
// or the legacy user home. Existing reserves IDs of retained instances, e.g.
// user plugins during project replacement. ReservedIDs are Kit command domains.
type Discovery struct {
	Home           apphome.Paths
	Cwd            string
	IncludeUser    bool
	IncludeProject bool
	Existing       []Installation
	ReservedIDs    []string
}

// DiscoveryResult preserves user-then-project, lexical installation order.
// Invalid and duplicate installations produce diagnostics, not runnable entries.
type DiscoveryResult struct {
	Installations []Installation
	Diagnostics   []Diagnostic
}

var pluginID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ParseManifest validates the manifest-v1 shape and host-reserved command domains.
func ParseManifest(data []byte, reservedIDs []string) (Manifest, error) {
	var manifest Manifest
	if len(data) > MaxManifestBytes {
		return manifest, errors.New("manifest exceeds 1 MiB")
	}
	if !utf8.Valid(data) {
		return manifest, errors.New("manifest is not UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	wire := struct {
		*Manifest
		Version json.Number `json:"manifestVersion"`
	}{Manifest: &manifest}
	if err := decoder.Decode(&wire); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Manifest{}, errors.New("manifest must contain one JSON object")
	}
	// Go decodes null into zero values; check optional fields explicitly so null
	// and an omitted field do not accidentally have identical schema semantics.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return Manifest{}, errors.New("manifest must be an object")
	}
	for key, raw := range fields {
		switch key {
		case "$schema", "manifestVersion", "id", "name", "transport":
		default:
			return Manifest{}, fmt.Errorf("unknown manifest field %q", key)
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Manifest{}, fmt.Errorf("manifest %s must not be null", key)
		}
	}
	if !numberIsOne(string(fields["manifestVersion"])) {
		return Manifest{}, errors.New("manifestVersion must be 1")
	}
	manifest.ManifestVersion = 1
	if !pluginID.MatchString(manifest.ID) || manifest.ID == "kit" || strings.HasPrefix(manifest.ID, "kit-") {
		return Manifest{}, errors.New("invalid or reserved plugin id")
	}
	for _, id := range reservedIDs {
		if id == manifest.ID {
			return Manifest{}, fmt.Errorf("plugin id %q is a reserved command domain", id)
		}
	}
	if _, ok := fields["name"]; ok && (utf8.RuneCountInString(manifest.Name) < 1 || utf8.RuneCountInString(manifest.Name) > 128) {
		return Manifest{}, errors.New("name must contain 1 to 128 characters")
	}
	if _, ok := fields["$schema"]; ok {
		parsed, err := url.Parse(manifest.Schema)
		if err != nil || parsed.Scheme == "" {
			return Manifest{}, errors.New("$schema must be an absolute URI")
		}
	}
	if manifest.Transport.Type != "stdio" {
		return Manifest{}, errors.New("transport.type must be stdio")
	}
	if manifest.Transport.Command == "" || strings.ContainsRune(manifest.Transport.Command, 0) {
		return Manifest{}, errors.New("transport.command must be a nonempty executable name or path without NUL")
	}
	var transport map[string]json.RawMessage
	if err := json.Unmarshal(fields["transport"], &transport); err != nil {
		return Manifest{}, fmt.Errorf("decode transport: %w", err)
	}
	for key := range transport {
		switch key {
		case "type", "command", "args":
		default:
			return Manifest{}, fmt.Errorf("unknown transport field %q", key)
		}
	}
	if raw, ok := transport["args"]; ok {
		var args []json.RawMessage
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Manifest{}, errors.New("transport.args must be an array")
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return Manifest{}, fmt.Errorf("decode args: %w", err)
		}
		for i, arg := range args {
			if bytes.Equal(bytes.TrimSpace(arg), []byte("null")) || strings.ContainsRune(manifest.Transport.Args[i], 0) {
				return Manifest{}, errors.New("transport.args must contain strings without NUL")
			}
		}
	}
	return manifest, nil
}

// Discover validates all immediate-child manifests before any process launch.
// Missing roots or manifests are normal; other filesystem failures are retained.
// Cancellation returns any results already collected along with ctx.Err().
func Discover(ctx context.Context, options Discovery) (DiscoveryResult, error) {
	var result DiscoveryResult
	roots := []struct {
		source Source
		path   string
	}{}
	if options.IncludeUser {
		if !filepath.IsAbs(options.Home.Plugins) {
			return result, errors.New("plugin discovery requires an absolute resolved user plugins path")
		}
		roots = append(roots, struct {
			source Source
			path   string
		}{User, options.Home.Plugins})
	}
	if options.IncludeProject {
		if !filepath.IsAbs(options.Cwd) {
			return result, errors.New("plugin discovery requires an absolute session cwd")
		}
		roots = append(roots, struct {
			source Source
			path   string
		}{Project, filepath.Join(options.Cwd, ".kit", "plugins")})
	}
	owners := make(map[string]string)
	for _, existing := range options.Existing {
		if _, ok := owners[existing.Manifest.ID]; !ok {
			owners[existing.Manifest.ID] = existing.ManifestPath
		}
	}
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		entries, err := os.ReadDir(root.path) // ReadDir returns lexical order.
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Source: root.source, Phase: "manifest", ManifestPath: root.path, Message: err.Error()})
			}
			continue
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			directory := filepath.Join(root.path, entry.Name())
			if !entry.IsDir() {
				if entry.Type()&os.ModeSymlink == 0 {
					continue
				}
				info, err := os.Stat(directory)
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						result.Diagnostics = append(result.Diagnostics, Diagnostic{Source: root.source, Phase: "manifest", ManifestPath: filepath.Join(directory, "plugin.json"), Message: err.Error()})
					}
					continue
				}
				if !info.IsDir() {
					continue
				}
			}
			path := filepath.Join(directory, "plugin.json")
			data, err := readManifest(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			var manifest Manifest
			if err == nil {
				manifest, err = ParseManifest(data, options.ReservedIDs)
			}
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Source: root.source, Phase: "manifest", ManifestPath: path, Message: err.Error()})
				continue
			}
			if owner, exists := owners[manifest.ID]; exists {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Source: root.source, Phase: "duplicate", PluginID: manifest.ID, ManifestPath: path, OtherManifestPath: owner, Message: fmt.Sprintf("plugin %q in %s duplicates %s", manifest.ID, path, owner)})
				continue
			}
			owners[manifest.ID] = path
			result.Installations = append(result.Installations, Installation{Source: root.source, Name: entry.Name(), Root: directory, ManifestPath: path, Manifest: manifest})
		}
	}
	return result, ctx.Err()
}

// numberIsOne compares an already JSON-validated number exactly, without float
// rounding or allocating enormous integers for untrusted exponent expressions.
func numberIsOne(raw string) bool {
	raw = strings.TrimSpace(raw)
	mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(raw), "e")
	power := 0
	if hasExponent {
		var err error
		power, err = strconv.Atoi(exponent)
		if err != nil {
			return false
		}
	}
	whole, fractional, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fractional, "0")
	trimmed := strings.TrimRight(digits, "0")
	return trimmed == "1" && power == len(fractional)-(len(digits)-len(trimmed))
}

func readManifest(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("plugin manifest must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, MaxManifestBytes+1))
}
