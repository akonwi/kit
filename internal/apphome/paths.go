// Package apphome owns Kit's on-disk path layout.
package apphome

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/akonwi/kit/internal/securefs"
)

const (
	// EnvHome overrides Kit's data directory.
	EnvHome = "KIT_HOME"
	// DevelopmentDirectory isolates the rewrite from the current Kit release.
	DevelopmentDirectory = ".kit-v2"
)

// Paths contains every application-owned path derived from one Kit home.
type Paths struct {
	Home string

	Database  string
	Auth      string
	Settings  string
	MCPConfig string

	Agents      string
	Attachments string
	Cache       string
	Droids      string
	Logs        string
	Plugins     string
	Prompts     string
	Scratchpads string
	Skills      string
	Themes      string

	Run              string
	ServerExecutable string
	ServerRegistry   string
	ServerToken      string
	ServerLog        string
	StartupLock      string
	ServerLock       string
}

// Resolve selects an explicit home, then KIT_HOME, then ~/.kit-v2.
func Resolve(explicit string) (Paths, error) {
	home := explicit
	if home == "" {
		home = os.Getenv(EnvHome)
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user home: %w", err)
		}
		home = filepath.Join(userHome, DevelopmentDirectory)
	}

	absolute, err := filepath.Abs(home)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve Kit home %q: %w", home, err)
	}
	return FromHome(absolute), nil
}

// FromHome derives the full path layout from home without touching disk.
func FromHome(home string) Paths {
	home = filepath.Clean(home)
	run := filepath.Join(home, "run")
	logs := filepath.Join(home, "logs")
	return Paths{
		Home: home,

		Database:  filepath.Join(home, "kit.db"),
		Auth:      filepath.Join(home, "auth.json"),
		Settings:  filepath.Join(home, "settings.json"),
		MCPConfig: filepath.Join(home, "mcp.json"),

		Agents:      filepath.Join(home, "agents"),
		Attachments: filepath.Join(home, "attachments"),
		Cache:       filepath.Join(home, "cache"),
		Droids:      filepath.Join(home, "droids"),
		Logs:        logs,
		Plugins:     filepath.Join(home, "plugins"),
		Prompts:     filepath.Join(home, "prompts"),
		Scratchpads: filepath.Join(home, "scratchpads"),
		Skills:      filepath.Join(home, "skills"),
		Themes:      filepath.Join(home, "themes"),

		Run:              run,
		ServerExecutable: filepath.Join(run, "kit-daemon"),
		ServerRegistry:   filepath.Join(run, "server.json"),
		ServerToken:      filepath.Join(run, "server.token"),
		ServerLog:        filepath.Join(logs, "server.log"),
		StartupLock:      filepath.Join(run, "startup.lock"),
		ServerLock:       filepath.Join(run, "server.lock"),
	}
}

// Ensure creates application directories with user-only permissions.
func (p Paths) Ensure() error {
	if p.Home == "" {
		return errors.New("Kit home is empty")
	}

	directories := []string{
		p.Home,
		p.Agents,
		p.Attachments,
		p.Cache,
		p.Droids,
		p.Logs,
		p.Plugins,
		p.Prompts,
		p.Run,
		p.Scratchpads,
		p.Skills,
		p.Themes,
	}
	for _, directory := range directories {
		if err := securefs.MakePrivateDir(directory); err != nil {
			return fmt.Errorf("create private Kit directory %q: %w", directory, err)
		}
	}
	return nil
}
