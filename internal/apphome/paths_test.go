package apphome

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akonwi/kit/internal/version"
)

func TestResolvePrecedence(t *testing.T) {
	// Resolve reads version.Channel, so this test owns it.
	t.Run("a build without a channel uses the shared home without creating files", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv(EnvHome, "")
		version.Channel = ""

		paths, err := Resolve("")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		want := filepath.Join(home, DefaultDirectory)
		if paths.Home != want {
			t.Fatalf("Home = %q, want %q", paths.Home, want)
		}
		if _, err := os.Stat(want); !os.IsNotExist(err) {
			t.Fatalf("Resolve must not create the home: stat error = %v", err)
		}
	})

	t.Run("a channel-bound build defaults to its isolated home", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv(EnvHome, "")
		version.Channel = "dev"
		defer func() { version.Channel = "" }()

		paths, err := Resolve("")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		want := filepath.Join(home, DevStateDirectory, "dev", "home")
		if paths.Home != want {
			t.Fatalf("Home = %q, want %q", paths.Home, want)
		}
		if _, err := os.Stat(want); !os.IsNotExist(err) {
			t.Fatalf("Resolve must not create the home: stat error = %v", err)
		}
	})

	t.Run("a channel never overrides an explicit home", func(t *testing.T) {
		t.Setenv(EnvHome, "")
		version.Channel = "dev"
		defer func() { version.Channel = "" }()
		explicitHome := filepath.Join(t.TempDir(), "explicit")

		paths, err := Resolve(explicitHome)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if paths.Home != explicitHome {
			t.Fatalf("Home = %q, want the explicit %q", paths.Home, explicitHome)
		}
	})

	t.Run("environment overrides a channel", func(t *testing.T) {
		environmentHome := filepath.Join(t.TempDir(), "environment")
		t.Setenv(EnvHome, environmentHome)
		version.Channel = "dev"
		defer func() { version.Channel = "" }()

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
		"cache":      filepath.Join(home, "cache"),
		"models":     filepath.Join(home, ".cache", "models.dev.json"),
		"droids":     filepath.Join(home, "droids"),
		"executable": filepath.Join(home, "run", "kit-daemon"),
		"registry":   filepath.Join(home, "run", "server.json"),
		"token":      filepath.Join(home, "run", "server.token"),
		"log":        filepath.Join(home, "logs", "server.log"),
		"server id":  filepath.Join(home, "server-id"),
		"machine id": filepath.Join(home, "machine-id"),
		"servers":    filepath.Join(home, "servers.json"),
		"tokens":     filepath.Join(home, "server-tokens.json"),
	}
	actual := map[string]string{
		"database":   paths.Database,
		"auth":       paths.Auth,
		"settings":   paths.Settings,
		"cache":      paths.Cache,
		"models":     paths.ModelCatalogCache,
		"droids":     paths.Droids,
		"executable": paths.ServerExecutable,
		"registry":   paths.ServerRegistry,
		"token":      paths.ServerToken,
		"log":        paths.ServerLog,
		"server id":  paths.ServerID,
		"machine id": paths.MachineID,
		"servers":    paths.SavedServers,
		"tokens":     paths.ServerTokens,
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

	for _, directory := range []string{paths.Home, paths.Cache, filepath.Dir(paths.ModelCatalogCache), paths.Droids, paths.Run, paths.Logs, paths.Plugins} {
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

func TestDevHomeSeparatesChannels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dev, err := DevHome("dev")
	if err != nil {
		t.Fatalf("DevHome(dev) error = %v", err)
	}
	review, err := DevHome("review")
	if err != nil {
		t.Fatalf("DevHome(review) error = %v", err)
	}
	if want := filepath.Join(home, DevStateDirectory, "dev", "home"); dev != want {
		t.Errorf("dev home = %q, want %q", dev, want)
	}
	if dev == review {
		t.Fatalf("channels must not share a home: %q", dev)
	}
	// The layout is complete before anything is written, so a home can be
	// described and reported without side effects.
	if _, err := os.Stat(dev); !os.IsNotExist(err) {
		t.Errorf("DevHome created a directory: stat error = %v", err)
	}
}

func TestDevHomeRejectsEscapingChannels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, channel := range []string{"../production", "/absolute", "a/b", ".", ".."} {
		if _, err := DevHome(channel); err == nil {
			t.Errorf("DevHome(%q) succeeded, want a channel error", channel)
		}
	}
}

func TestAnEmptyChannelIsNotAChannel(t *testing.T) {
	// An empty channel means "no channel", which selects the shared home. It
	// must never resolve to a state directory or error.
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvHome, "")
	version.Channel = ""

	paths, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := paths.Home; got != filepath.Join(os.Getenv("HOME"), DefaultDirectory) {
		t.Fatalf("Home = %q, want the shared home", got)
	}
}
