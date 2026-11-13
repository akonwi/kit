package skills

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/systemprompt"
)

func writeExecutable(t *testing.T, path string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindRexPrefersRexBinDirThenPathThenRexApp(t *testing.T) {
	base := t.TempDir()
	binDir := filepath.Join(base, "bin")
	onPath := writeExecutable(t, filepath.Join(base, "path", "rex"), 0o755)
	home := filepath.Join(base, "home")
	app := writeExecutable(t, filepath.Join(home, rexAppCommand), 0o755)
	environment := map[string]string{"REX_BIN_DIR": binDir}
	getenv := func(name string) string { return environment[name] }
	lookPath := func(string) (string, error) { return onPath, nil }
	noPath := func(string) (string, error) { return "", errors.New("not found") }

	inBinDir := writeExecutable(t, filepath.Join(binDir, "rex"), 0o755)
	if got := FindRex(getenv, lookPath, home); got != inBinDir {
		t.Errorf("FindRex() = %q, want REX_BIN_DIR's %q", got, inBinDir)
	}
	if err := os.Chmod(inBinDir, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindRex(getenv, lookPath, home); got != onPath {
		t.Errorf("FindRex() with a non-executable REX_BIN_DIR rex = %q, want PATH's %q", got, onPath)
	}
	if got := FindRex(getenv, noPath, home); got != app {
		t.Errorf("FindRex() without rex on PATH = %q, want Rex.app's %q", got, app)
	}
	if got := FindRex(getenv, noPath, filepath.Join(base, "elsewhere")); got != "" {
		t.Errorf("FindRex() with no rex = %q, want none", got)
	}
}

func rexLoader(t *testing.T, paths apphome.Paths, rex string) *FilesystemLoader {
	t.Helper()
	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	loader.findRex = func() string { return rex }
	return loader
}

func TestTheRexSkillIsOfferedWhereRexIsInstalled(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := rexLoader(t, paths, "/Applications/Rex's Server/rex").Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := skillNames(result.Registry.Skills()), []string{KitCustomizationName, KitFiguresName, RexName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("skill names = %#v, want %#v", got, want)
	}
	rex, _ := result.Registry.Lookup(RexName)
	if rex.Source != SourceBuiltin || !strings.Contains(rex.Content, "REX='/Applications/Rex'\\''s Server/rex'\n") || strings.Contains(rex.Content, "{{rex}}") {
		t.Fatalf("Rex skill = %#v", rex)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}

	without, err := rexLoader(t, paths, "").Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := without.Registry.Lookup(RexName); ok {
		t.Fatal("the Rex skill was offered without Rex")
	}
}

func TestYourOwnRexSkillReplacesKits(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	own := writeSkill(t, paths.Skills, RexName, "My Rex notes", "my body", false)
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := rexLoader(t, paths, "/usr/local/bin/rex").Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	rex, ok := result.Registry.Lookup(RexName)
	if !ok || rex.Source != SourceUser || rex.Location != own || !strings.Contains(rex.Content, "my body") {
		t.Fatalf("Rex skill = %#v", rex)
	}
	want := []systemprompt.Diagnostic{{
		Severity: systemprompt.DiagnosticInfo, Code: "skills.builtin_replaced",
		Message: "Kit's built-in Rex skill was replaced by this skill",
	}}
	got := append([]systemprompt.Diagnostic(nil), result.Diagnostics...)
	for index := range got {
		got[index].Source = systemprompt.Source{}
	}
	if !reflect.DeepEqual(got, want) || result.Diagnostics[0].Source.Path != own {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
}
