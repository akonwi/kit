package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/savedservers"
	"github.com/akonwi/kit/internal/settings"
	kittheme "github.com/akonwi/kit/internal/theme"
)

// serverProbeTimeout bounds each server's status check in listings.
const serverProbeTimeout = 2 * time.Second

// attachTarget is the server an interactive invocation attaches to. The zero
// value is this home's own server, which the client may start.
type attachTarget struct {
	// Name labels the server in the terminal client.
	Name string
	// Home is the server's Kit home.
	Home string
}

// ServerChoice is one server offered by the attach picker.
type ServerChoice struct {
	// Name is the saved name, or savedservers.LocalName for this home's server.
	Name string
	// Location is the server's Kit home for display.
	Location string
	// Status is "running", "not running", "incompatible", or "unavailable".
	Status string
	// Detail explains a status other than running, or is empty.
	Detail string
}

func savedServerStore() (*savedservers.Store, apphome.Paths, error) {
	paths, err := apphome.Resolve("")
	if err != nil {
		return nil, apphome.Paths{}, err
	}
	store, err := savedservers.NewStore(paths.SavedServers)
	return store, paths, err
}

// resolveAttachTarget maps a server argument to a target: LocalName is this
// home's server, a saved name is that server, and an argument containing a
// path separator, or starting with '.' or '~', is a Kit home path.
func resolveAttachTarget(argument string) (attachTarget, error) {
	if argument == savedservers.LocalName {
		return attachTarget{}, nil
	}
	if strings.ContainsRune(argument, filepath.Separator) || strings.HasPrefix(argument, ".") || strings.HasPrefix(argument, "~") {
		home, err := resolveServerHome(argument)
		if err != nil {
			return attachTarget{}, err
		}
		return attachTarget{Name: filepath.Base(home), Home: home}, nil
	}
	store, _, err := savedServerStore()
	if err != nil {
		return attachTarget{}, err
	}
	server, err := store.Find(argument)
	if errors.Is(err, savedservers.ErrNotFound) {
		return attachTarget{}, usagef("unknown server %q; list saved servers with `kit servers`", argument)
	}
	if err != nil {
		return attachTarget{}, err
	}
	return attachTarget{Name: server.Name, Home: server.Home}, nil
}

// resolveServerHome makes a Kit home argument absolute, expanding a leading
// '~', and requires it to be an existing directory.
func resolveServerHome(argument string) (string, error) {
	path := argument
	if path == "~" || strings.HasPrefix(path, "~/") {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		path = filepath.Join(userHome, strings.TrimPrefix(path, "~"))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve Kit home %q: %w", argument, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("Kit home %q: %w", argument, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Kit home %q is not a directory", argument)
	}
	return absolute, nil
}

// serverChoices lists this home's server followed by the saved servers, each
// with its current status.
func serverChoices(ctx context.Context) ([]ServerChoice, error) {
	store, paths, err := savedServerStore()
	if err != nil {
		return nil, err
	}
	saved, err := store.List()
	if err != nil {
		return nil, err
	}
	if len(saved) == 0 {
		return nil, nil
	}
	homes := append([]savedservers.Server{{Name: savedservers.LocalName, Home: paths.Home}}, saved...)
	choices := make([]ServerChoice, len(homes))
	var wait sync.WaitGroup
	for index, server := range homes {
		choices[index] = ServerChoice{Name: server.Name, Location: interactiveLocation(ctx, server.Home)}
		wait.Add(1)
		go func() {
			defer wait.Done()
			choices[index].Status, choices[index].Detail = serverStatus(ctx, server.Home)
		}()
	}
	wait.Wait()
	return choices, nil
}

// serverStatus reports whether a compatible server is running for home, and
// why not.
func serverStatus(ctx context.Context, home string) (status, detail string) {
	probeContext, cancel := context.WithTimeout(ctx, serverProbeTimeout)
	defer cancel()
	client, err := kit.Connect(probeContext, kit.Home(home))
	switch {
	case err == nil:
		client.Close()
		return "running", ""
	case errors.Is(err, os.ErrNotExist):
		return "not running", ""
	case errors.Is(err, kit.ErrIncompatibleServer):
		return "incompatible", err.Error()
	default:
		return "unavailable", err.Error()
	}
}

// connectAttachTarget connects to target's server without starting it.
func connectAttachTarget(ctx context.Context, target attachTarget) (*kit.Client, error) {
	connectContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, err := kit.Connect(connectContext, kit.Home(target.Home))
	if err == nil {
		return client, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("server %q is not running; start it with `KIT_HOME=%s kit server start`", target.Name, target.Home)
	}
	return nil, fmt.Errorf("attach to server %q: %w", target.Name, err)
}

// PickerTheme loads the user's theme for a picker shown before the terminal
// client's environment opens. Problems fall back to the system theme quietly;
// opening the environment reports them.
func PickerTheme() kittheme.Definition {
	paths, err := apphome.Resolve("")
	if err != nil {
		return kittheme.Definition{}
	}
	store, err := settings.NewStore(paths.Settings)
	if err != nil {
		return kittheme.Definition{}
	}
	loaded, _, err := store.Load()
	if err != nil || loaded.Theme == kittheme.SystemName {
		return kittheme.Definition{}
	}
	definition, _, err := kittheme.Load(paths.Themes, loaded.Theme)
	if err != nil {
		return kittheme.Definition{}
	}
	return definition
}

func runServersCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	store, _, err := savedServerStore()
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	switch {
	case len(args) == 1 && args[0] == "list":
		choices, err := serverChoices(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		if len(choices) == 0 {
			fmt.Fprintln(stdout, "No saved servers. Add one with `kit servers add NAME HOME`.")
			return 0
		}
		table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(table, "NAME\tHOME\tSTATUS")
		for _, choice := range choices {
			fmt.Fprintf(table, "%s\t%s\t%s\n", choice.Name, choice.Location, choice.Status)
		}
		if err := table.Flush(); err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		for _, choice := range choices {
			if choice.Detail != "" {
				fmt.Fprintf(stdout, "\n%s: %s\n", choice.Name, choice.Detail)
			}
		}
		return 0
	case len(args) == 3 && args[0] == "add":
		if err := savedservers.ValidateName(args[1]); err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 2
		}
		home, err := resolveServerHome(args[2])
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		if err := store.Add(savedservers.Server{Name: args[1], Home: home}); err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "saved server %q: %s\n", args[1], home)
		return 0
	case len(args) == 2 && args[0] == "remove":
		if err := store.Remove(args[1]); err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "removed server %q\n", args[1])
		return 0
	default:
		fmt.Fprintln(stderr, "usage: kit servers <list|add NAME HOME|remove NAME>")
		return 2
	}
}
