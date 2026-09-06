package apphome

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	t.Run("explicit home wins", func(t *testing.T) {
		environmentHome := filepath.Join(t.TempDir(), "environment")
		t.Setenv(EnvHome, environmentHome)
		explicitHome := filepath.Join(t.TempDir(), "explicit")

		paths, err := Resolve(explicitHome)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if paths.Home != explicitHome {
			t.Fatalf("Home = %q, want %q", paths.Home, explicitHome)
		}
	})

	t.Run("environment overrides default", func(t *testing.T) {
		environmentHome := filepath.Join(t.TempDir(), "environment")
		t.Setenv(EnvHome, environmentHome)

		paths, err := Resolve("")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if paths.Home != environmentHome {
			t.Fatalf("Home = %q, want %q", paths.Home, environmentHome)
		}
	})
}

func TestFromHomeBuildsLayout(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "kit")
	paths := FromHome(home)

	checks := map[string]string{
		"database":   filepath.Join(home, "kit.db"),
		"auth":       filepath.Join(home, "auth.json"),
		"settings":   filepath.Join(home, "settings.json"),
		"droids":     filepath.Join(home, "droids"),
		"executable": filepath.Join(home, "run", "kit-daemon"),
		"registry":   filepath.Join(home, "run", "server.json"),
		"token":      filepath.Join(home, "run", "server.token"),
		"log":        filepath.Join(home, "logs", "server.log"),
	}
	actual := map[string]string{
		"database":   paths.Database,
		"auth":       paths.Auth,
		"settings":   paths.Settings,
		"droids":     paths.Droids,
		"executable": paths.ServerExecutable,
		"registry":   paths.ServerRegistry,
		"token":      paths.ServerToken,
		"log":        paths.ServerLog,
	}
	for name, want := range checks {
		if got := actual[name]; got != want {
			t.Errorf("%s path = %q, want %q", name, got, want)
		}
	}
}

func TestEnsureCreatesPrivateDirectories(t *testing.T) {
	t.Parallel()

	paths := FromHome(filepath.Join(t.TempDir(), "kit"))
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	for _, directory := range []string{paths.Home, paths.Droids, paths.Run, paths.Logs, paths.Plugins} {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatalf("stat %q: %v", directory, err)
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", directory)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("mode for %q = %o, want 700", directory, info.Mode().Perm())
		}
	}
}
