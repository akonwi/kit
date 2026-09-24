package mcpconfig

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

// harness builds a loader over a temporary user home and project directory.
type harness struct {
	t       *testing.T
	home    string
	kitHome string
	cwd     string
	env     map[string]string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{
		t:       t,
		home:    filepath.Join(root, "home"),
		kitHome: filepath.Join(root, "home", ".kit-v2"),
		cwd:     filepath.Join(root, "project"),
		env:     map[string]string{},
	}
	for _, dir := range []string{h.home, h.kitHome, h.cwd} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	return h
}

func (h *harness) pathFor(source Source) string {
	h.t.Helper()
	switch source {
	case SourceKitUser:
		return filepath.Join(h.kitHome, "mcp.json")
	case SourceSharedProject:
		return filepath.Join(h.cwd, ".mcp.json")
	case SourceKitProject:
		return filepath.Join(h.cwd, ".agents", "mcp.json")
	default:
		h.t.Fatalf("unknown source %q", source)
		return ""
	}
}

func (h *harness) write(source Source, contents string) {
	h.t.Helper()
	path := h.pathFor(source)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		h.t.Fatalf("write %s: %v", path, err)
	}
}

func (h *harness) load() Result {
	h.t.Helper()
	loader, err := NewLoader(
		apphome.FromHome(h.kitHome),
		WithUserHome(h.home),
		WithLookupEnv(func(key string) (string, bool) {
			value, ok := h.env[key]
			return value, ok
		}),
	)
	if err != nil {
		h.t.Fatalf("new loader: %v", err)
	}
	result, err := loader.Load(context.Background(), h.cwd)
	if err != nil {
		h.t.Fatalf("load: %v", err)
	}
	return result
}

func (h *harness) mustServer(result Result, name string) Server {
	h.t.Helper()
	server, ok := result.Lookup(name)
	if !ok {
		h.t.Fatalf("server %q not found; got %v, diagnostics %v", name, serverNames(result), messages(result))
	}
	return server
}

func serverNames(result Result) []string {
	names := make([]string, 0, len(result.Servers))
	for _, server := range result.Servers {
		names = append(names, server.Name)
	}
	return names
}

func messages(result Result) []string {
	out := make([]string, 0, len(result.Diagnostics))
	for _, diagnostic := range result.Diagnostics {
		out = append(out, diagnostic.Error())
	}
	return out
}

func fileFor(t *testing.T, result Result, source Source) File {
	t.Helper()
	for _, file := range result.Files {
		if file.Source == source {
			return file
		}
	}
	t.Fatalf("no candidate file for source %q", source)
	return File{}
}

func requireNoDiagnostics(t *testing.T, result Result) {
	t.Helper()
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %v", messages(result))
	}
}
