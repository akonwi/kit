package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func newTestStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store
}

func TestLoadMissingSettingsUsesSystemTheme(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, filepath.Join(t.TempDir(), "missing", "settings.json"))
	settings, warnings, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if settings.Theme != "system" || len(warnings) != 0 {
		t.Fatalf("Load() = %#v, %v", settings, warnings)
	}
}

func TestLoadSelectedTheme(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"theme":"solarized"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, warnings, err := newTestStore(t, path).Load()
	if err != nil || len(warnings) != 0 || settings.Theme != "solarized" {
		t.Fatalf("Load() = %#v, %v, %v", settings, warnings, err)
	}
}

func TestLoadInvalidThemeFallsBackWithWarning(t *testing.T) {
	t.Parallel()

	for name, document := range map[string]string{
		"wrong type": `{"theme":42}`,
		"unsafe":     `{"theme":"../escape"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			settings, warnings, err := newTestStore(t, path).Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if settings.Theme != "system" || len(warnings) != 1 || warnings[0].Field != "theme" {
				t.Fatalf("Load() = %#v, %#v", settings, warnings)
			}
		})
	}
}

func TestNewStoreRequiresAbsolutePath(t *testing.T) {
	t.Parallel()

	if _, err := NewStore("settings.json"); err == nil {
		t.Fatal("NewStore() unexpectedly accepted a relative path")
	}
}

func TestLoadRejectsOversizedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, make([]byte, maxFileSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newTestStore(t, path).Load(); err == nil || !strings.Contains(err.Error(), "file exceeds") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsFIFO(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newTestStore(t, path).Load(); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsMalformedDocument(t *testing.T) {
	t.Parallel()

	for _, document := range [][]byte{
		[]byte(`[]`),
		[]byte(`{"theme":"a","theme":"b"}`),
		[]byte(`{} {}`),
		{0xff},
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, document, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := newTestStore(t, path).Load(); err == nil {
			t.Errorf("Load(%q) unexpectedly succeeded", document)
		}
	}
}

func TestUpdateThemePreservesUnknownSettings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	initial := `{
		"theme": "old",
		"pager": true,
		"workspace": {"paneRatio": 0.4},
		"future": [1, 2, 3]
	}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	updated, err := newTestStore(t, path).UpdateTheme("new-theme")
	if err != nil {
		t.Fatalf("UpdateTheme() error = %v", err)
	}
	if updated.Theme != "new-theme" {
		t.Fatalf("Theme = %q", updated.Theme)
	}
	var document map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("saved settings are malformed: %v", err)
	}
	for _, field := range []string{"pager", "workspace", "future"} {
		if _, ok := document[field]; !ok {
			t.Errorf("field %q was not preserved", field)
		}
	}
	var theme string
	if err := json.Unmarshal(document["theme"], &theme); err != nil || theme != "new-theme" {
		t.Fatalf("saved theme = %q, %v", theme, err)
	}
}

func TestUpdateThemeCreatesParentDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	updated, err := newTestStore(t, path).UpdateTheme("cozy")
	if err != nil || updated.Theme != "cozy" {
		t.Fatalf("UpdateTheme() = %#v, %v", updated, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file was not created: %v", err)
	}
}

func TestUpdateThemeRejectsInvalidNameWithoutWriting(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := newTestStore(t, path).UpdateTheme("../escape"); err == nil {
		t.Fatal("UpdateTheme() unexpectedly succeeded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("settings file exists after rejected update: %v", err)
	}
}

func TestUpdateThemeDoesNotOverwriteMalformedSettings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"theme":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestStore(t, path).UpdateTheme("cozy"); err == nil {
		t.Fatal("UpdateTheme() unexpectedly succeeded")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != `{"theme":` {
		t.Fatalf("malformed settings changed to %q, %v", data, err)
	}
}

func TestUpdateThemeReportsWriteFailure(t *testing.T) {
	t.Parallel()

	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := newTestStore(t, filepath.Join(parentFile, "settings.json")).UpdateTheme("cozy")
	if err == nil || (!strings.Contains(err.Error(), "read settings") && !strings.Contains(err.Error(), "create settings directory")) {
		t.Fatalf("UpdateTheme() error = %v", err)
	}
}

func TestStoreSerializesConcurrentUpdates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	store := newTestStore(t, path)
	names := []string{"one", "two", "three", "four", "five", "six"}
	var wait sync.WaitGroup
	errors := make(chan error, len(names))
	for _, name := range names {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := store.UpdateTheme(name)
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent UpdateTheme() error = %v", err)
		}
	}
	loaded, warnings, err := store.Load()
	if err != nil || len(warnings) != 0 {
		t.Fatalf("Load() = %#v, %v, %v", loaded, warnings, err)
	}
	found := false
	for _, name := range names {
		found = found || loaded.Theme == name
	}
	if !found {
		t.Fatalf("final theme = %q", loaded.Theme)
	}
}

func TestLoadModelOverrides(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	document := `{"modelOverrides":{"anthropic/claude-sonnet-4-6":{"contextWindow":16000},"invalid":{"contextWindow":42},"openai/gpt":{"contextWindow":0}}}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, warnings, err := newTestStore(t, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.ModelOverrides["anthropic/claude-sonnet-4-6"].ContextWindow; got != 16000 {
		t.Fatalf("contextWindow = %d, want 16000", got)
	}
	if len(loaded.ModelOverrides) != 1 || len(warnings) != 2 {
		t.Fatalf("overrides = %#v, warnings = %#v", loaded.ModelOverrides, warnings)
	}
}

func TestStoreUpdatesAndClearsModelContextWindow(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"theme":"system","future":{"kept":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateModelContextWindow("openai-codex/gpt-5.6-sol", 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ModelOverrides["openai-codex/gpt-5.6-sol"].ContextWindow != 1_000_000 {
		t.Fatalf("updated settings = %+v", updated)
	}
	if _, err := store.UpdateModelContextWindow("openai-codex/gpt-5.6-sol", 0); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{\n  \"future\": {\n    \"kept\": true\n  },\n  \"theme\": \"system\"\n}\n" {
		t.Fatalf("settings document = %s", raw)
	}
}
