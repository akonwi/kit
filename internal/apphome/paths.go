// Package apphome owns Kit's on-disk path layout.
package apphome

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akonwi/kit/internal/securefs"
	"github.com/akonwi/kit/internal/version"
)

const (
	// EnvHome overrides Kit's data directory.
	EnvHome = "KIT_HOME"
	// DefaultDirectory is Kit's directory beneath the user home.
	DefaultDirectory = ".kit"
)

// Paths contains every application-owned path derived from one Kit home.
type Paths struct {
	Home string

	Database  string
	Auth      string
	MCPAuth   string
	Settings  string
	MCPConfig string
	ServerID  string
	MachineID string
	// SavedServers lists the servers this home's clients can attach to.
	SavedServers string
	// ServerTokens holds the device tokens of saved URL servers.
	ServerTokens string

	Agents            string
	Attachments       string
	Cache             string
	ModelCatalogCache string
	Droids            string
	Logs              string
	Plugins           string
	Prompts           string
	Scratchpads       string
	Skills            string
	Themes            string

	Run              string
	ServerExecutable string
	ServerRegistry   string
	ServerToken      string
	ServerLog        string
	StartupLock      string
	ServerLock       string
}

// Resolve selects an explicit home, then KIT_HOME, then the build's
// default. An explicit home and KIT_HOME win absolutely: they are how tests,
// containers, and multi-home machines choose state. A build bound to a channel
// and given neither resolves to that channel's isolated home instead of the
// shared one.
func Resolve(explicit string) (Paths, error) {
	home := explicit
	if home == "" {
		home = os.Getenv(EnvHome)
	}
	if home != "" {
		absolute, err := filepath.Abs(home)
		if err != nil {
			return Paths{}, fmt.Errorf("resolve Kit home %q: %w", home, err)
		}
		return FromHome(absolute), nil
	}
	if version.Channel != "" {
		isolated, err := DevHome(version.Channel)
		if err != nil {
			return Paths{}, fmt.Errorf("resolve development Kit home: %w", err)
		}
		return FromHome(isolated), nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user home: %w", err)
	}
	return FromHome(filepath.Join(userHome, DefaultDirectory)), nil
}

// DevStateDirectory is the machine-wide root of isolated development state. It
// is the same on every platform: Kit's supported systems are single-user
// developer machines, and one path keeps dev homes predictable when moving
// between them.
const DevStateDirectory = ".local/state/kit"

// DevHome is the isolated home a channel such as "dev" binds to. Each channel
// owns its own home beneath the user's state directory, so its daemon,
// database, credentials, and logs never share state with the shared default or
// with another channel. The home is derived, never created: Resolve only
// selects a path, and the directory appears when a command needs it.
func DevHome(channel string) (string, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	// A channel names a home directory namespace, so it must be exactly one
	// path segment. This keeps a channel such as "dev" or a worktree name
	// from escaping the state directory.
	if channel == "." || channel == ".." || channel != filepath.Base(filepath.Clean(channel)) || strings.ContainsAny(channel, "/\\") {
		return "", fmt.Errorf("invalid Kit channel %q: want a single directory name", channel)
	}
	return filepath.Join(userHome, DevStateDirectory, channel, "home"), nil
}

// FromHome derives the full path layout from home without touching disk.
func FromHome(home string) Paths {
	home = filepath.Clean(home)
	run := filepath.Join(home, "run")
	logs := filepath.Join(home, "logs")
	modelCache := filepath.Join(home, ".cache")
	return Paths{
		Home: home,

		Database:  filepath.Join(home, "kit.db"),
		Auth:      filepath.Join(home, "auth.json"),
		MCPAuth:   filepath.Join(home, "mcp-auth.json"),
		Settings:  filepath.Join(home, "settings.json"),
		MCPConfig: filepath.Join(home, "mcp.json"),
		ServerID:  filepath.Join(home, "server-id"),
		MachineID: filepath.Join(home, "machine-id"),

		SavedServers: filepath.Join(home, "servers.json"),
		ServerTokens: filepath.Join(home, "server-tokens.json"),

		Agents:            filepath.Join(home, "agents"),
		Attachments:       filepath.Join(home, "attachments"),
		Cache:             filepath.Join(home, "cache"),
		ModelCatalogCache: filepath.Join(modelCache, "models.dev.json"),
		Droids:            filepath.Join(home, "droids"),
		Logs:              logs,
		Plugins:           filepath.Join(home, "plugins"),
		Prompts:           filepath.Join(home, "prompts"),
		Scratchpads:       filepath.Join(home, "scratchpads"),
		Skills:            filepath.Join(home, "skills"),
		Themes:            filepath.Join(home, "themes"),

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
		filepath.Dir(p.ModelCatalogCache),
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
