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
	"unicode/utf8"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
	"github.com/akonwi/kit/internal/devices"
	"github.com/akonwi/kit/internal/pairing"
	"github.com/akonwi/kit/internal/savedservers"
	"github.com/akonwi/kit/internal/settings"
	"github.com/akonwi/kit/internal/sshtunnel"
	kittheme "github.com/akonwi/kit/internal/theme"
	"github.com/akonwi/kit/internal/version"
)

// serverProbeTimeout bounds each server's status check in listings.
const serverProbeTimeout = 2 * time.Second

// sshProbeTimeout bounds an SSH server's status check, which connects over
// SSH first.
const sshProbeTimeout = 10 * time.Second

// attachTarget is the server an interactive invocation attaches to. The zero
// value is this home's own server, which the client may start.
type attachTarget struct {
	// Name labels the server in the terminal client.
	Name string
	// Home is the Kit home of a server on this machine.
	Home string
	// URL is the origin of a served server, reached with the device token
	// saved under Name.
	URL string
	// SSH is the destination of a host whose Kit server is reached over SSH.
	SSH string
}

// own reports whether the target is this home's own server.
func (t attachTarget) own() bool { return t.Home == "" && t.URL == "" && t.SSH == "" }

// remote reports whether the target may run on another machine.
func (t attachTarget) remote() bool { return t.URL != "" || t.SSH != "" }

// ServerChoice is one server offered by the attach picker.
type ServerChoice struct {
	// Name is the saved name, or savedservers.LocalName for this home's server.
	Name string
	// Location is the server's Kit home, URL, or SSH destination for display.
	Location string
	// Status is "running", "not running", "needs pairing", "incompatible",
	// or "unavailable".
	Status string
	// Detail explains a status other than running, or is empty.
	Detail string
	// Starts reports that attaching starts the server when it is not
	// running, as for this home's server and an SSH server.
	Starts bool
}

// serverStores opens this home's saved servers and device tokens.
func serverStores() (*savedservers.Store, *savedservers.Tokens, apphome.Paths, error) {
	paths, err := apphome.Resolve("")
	if err != nil {
		return nil, nil, apphome.Paths{}, err
	}
	store, err := savedservers.NewStore(paths.SavedServers)
	if err != nil {
		return nil, nil, paths, err
	}
	tokens, err := savedservers.NewTokens(paths.ServerTokens)
	return store, tokens, paths, err
}

func isPairingLink(argument string) bool {
	return strings.Contains(argument, pairing.LinkPath+"#")
}

func isURL(argument string) bool {
	return strings.HasPrefix(argument, "https://") || strings.HasPrefix(argument, "http://")
}

func isHomePath(argument string) bool {
	return strings.ContainsRune(argument, filepath.Separator) || strings.HasPrefix(argument, ".") || strings.HasPrefix(argument, "~")
}

func isSSHURL(argument string) bool {
	return strings.HasPrefix(argument, "ssh://")
}

func targetFor(server savedservers.Server) attachTarget {
	switch server.Kind {
	case savedservers.KindURL:
		return attachTarget{Name: server.Name, URL: server.URL}
	case savedservers.KindSSH:
		return attachTarget{Name: server.Name, SSH: server.Destination}
	}
	return attachTarget{Name: server.Name, Home: server.Home}
}

// resolveAttachTarget maps a server argument to a target, in this order:
// LocalName is this home's server; a pairing link re-pairs the saved server
// with its URL; an HTTP(S) URL is the saved server with that URL; an ssh://
// URL is an SSH destination; an argument containing a path separator, or
// starting with '.' or '~', is a Kit home path; then a saved name; and an
// unsaved user@host is an SSH destination. A bare host must be saved or
// written as an ssh:// URL, so a mistyped name never connects anywhere.
func resolveAttachTarget(ctx context.Context, argument string, stderr io.Writer) (attachTarget, error) {
	if argument == savedservers.LocalName {
		return attachTarget{}, nil
	}
	store, tokens, _, err := serverStores()
	if err != nil {
		return attachTarget{}, err
	}
	switch {
	case isPairingLink(argument):
		serverURL, _, err := pairing.ParseLink(argument)
		if err != nil {
			return attachTarget{}, usagef("%v", err)
		}
		server, err := store.FindURL(strings.ToLower(serverURL))
		if errors.Is(err, savedservers.ErrNotFound) {
			return attachTarget{}, usagef("no saved server is at %s; save it under a name with `kit servers add NAME LINK`", serverURL)
		}
		if err != nil {
			return attachTarget{}, err
		}
		if err := pairServer(ctx, tokens, server.Name, argument, stderr); err != nil {
			return attachTarget{}, err
		}
		return targetFor(server), nil
	case isURL(argument):
		serverURL, err := savedservers.CanonicalURL(argument)
		if err != nil {
			return attachTarget{}, usagef("%v", err)
		}
		server, err := store.FindURL(serverURL)
		if errors.Is(err, savedservers.ErrNotFound) {
			return attachTarget{}, usagef("no saved server is at %s; pair with a link from `kit server pair` on that server and save it with `kit servers add NAME LINK`", serverURL)
		}
		if err != nil {
			return attachTarget{}, err
		}
		return targetFor(server), nil
	case isSSHURL(argument):
		if err := sshtunnel.ValidateDestination(argument); err != nil {
			return attachTarget{}, usagef("%v", err)
		}
		return attachTarget{Name: argument, SSH: argument}, nil
	case isHomePath(argument):
		home, err := resolveServerHome(argument)
		if err != nil {
			return attachTarget{}, err
		}
		return attachTarget{Name: filepath.Base(home), Home: home}, nil
	}
	server, err := store.Find(argument)
	if errors.Is(err, savedservers.ErrNotFound) {
		if strings.Contains(argument, "@") && sshtunnel.ValidateDestination(argument) == nil {
			return attachTarget{Name: argument, SSH: argument}, nil
		}
		return attachTarget{}, usagef("unknown server %q; list saved servers with `kit servers`, or attach over SSH with USER@HOST or ssh://HOST", argument)
	}
	if err != nil {
		return attachTarget{}, err
	}
	return targetFor(server), nil
}

// pairServer redeems a pairing link as this machine and saves the device token
// under name, bound to the link's server URL. It warns on stderr when the link
// sends the code and token across the network unencrypted.
func pairServer(ctx context.Context, tokens *savedservers.Tokens, name, link string, stderr io.Writer) error {
	if serverURL, _, err := pairing.ParseLink(link); err == nil && !securePairingURL(serverURL) {
		fmt.Fprintf(stderr, "kit: warning: %s is plain HTTP; the pairing code and device token cross the network\nunencrypted unless the network itself is private, such as a tailnet\n", serverURL)
	}
	pairContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	paired, err := kit.Pair(pairContext, link, deviceName())
	if err != nil {
		return fmt.Errorf("pair with %s: %w", name, describePairingFailure(err))
	}
	previous, _ := tokens.Get(name, paired.ServerURL)
	if err := tokens.Set(name, paired.ServerURL, paired.Token); err != nil {
		return fmt.Errorf("save device token for %q: %w", name, err)
	}
	if previous != "" && previous != paired.Token {
		retireDevice(ctx, paired.ServerURL, previous)
	}
	return nil
}

// retireDevice revokes the device a replaced token belongs to, which would
// otherwise stay valid with no copy left here to revoke it. A token the server
// no longer accepts needs nothing.
func retireDevice(ctx context.Context, serverURL, token string) {
	retireContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, err := kit.Connect(retireContext, kit.Endpoint(serverURL, kit.AccessToken(token)))
	if err != nil {
		return
	}
	defer client.Close()
	listed, err := client.Devices(retireContext)
	if err != nil {
		return
	}
	for _, device := range listed {
		if device.Current {
			_ = client.RevokeDevice(retireContext, device.ID)
			return
		}
	}
}

func describePairingFailure(err error) error {
	var failure *kit.ServerError
	if errors.As(err, &failure) {
		switch failure.Code {
		case kit.ErrorUnauthorized:
			return errors.New("the pairing code was already used or has expired; get a new link with `kit server pair` on the server")
		case kit.ErrorRateLimited:
			return errors.New("too many failed attempts; try again shortly")
		}
	}
	return err
}

// deviceName names this machine to the servers it pairs with: its hostname
// without a .local suffix, within the device name limit.
func deviceName() string {
	name, err := os.Hostname()
	name = strings.TrimSpace(strings.TrimSuffix(name, ".local"))
	if err != nil || name == "" {
		return "kit"
	}
	for len(name) > devices.MaxNameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if devices.ValidateName(name) != nil {
		return "kit"
	}
	return name
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
	store, _, paths, err := serverStores()
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
	servers := append([]savedservers.Server{{Name: savedservers.LocalName, Kind: savedservers.KindHome, Home: paths.Home}}, saved...)
	choices := make([]ServerChoice, len(servers))
	var wait sync.WaitGroup
	for index, server := range servers {
		location := server.Location()
		if server.Kind == savedservers.KindHome {
			location = interactiveLocation(ctx, server.Home)
		}
		choices[index] = ServerChoice{Name: server.Name, Location: location, Starts: server.Name == savedservers.LocalName || server.Kind == savedservers.KindSSH}
		target := targetFor(server)
		if server.Name == savedservers.LocalName {
			target = attachTarget{Name: server.Name, Home: server.Home}
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			choices[index].Status, choices[index].Detail = serverStatus(ctx, target)
		}()
	}
	wait.Wait()
	return choices, nil
}

// serverStatus reports whether a compatible server is running for target,
// and why not.
func serverStatus(ctx context.Context, target attachTarget) (status, detail string) {
	if target.SSH != "" {
		return sshServerStatus(ctx, target)
	}
	probeContext, cancel := context.WithTimeout(ctx, serverProbeTimeout)
	defer cancel()
	client, err := connectTarget(probeContext, target)
	switch {
	case err == nil:
		client.Close()
		return "running", ""
	case errors.Is(err, os.ErrNotExist) && target.URL == "":
		return "not running", ""
	case errors.Is(err, kit.ErrIncompatibleServer):
		return "incompatible", err.Error()
	case needsPairing(err):
		return "needs pairing", "pair again with a link from `kit server pair` on the server, then `kit attach LINK`"
	default:
		return "unavailable", err.Error()
	}
}

// sshServerStatus asks an SSH server's host for its status without starting
// the server or prompting.
func sshServerStatus(ctx context.Context, target attachTarget) (status, detail string) {
	probeContext, cancel := context.WithTimeout(ctx, sshProbeTimeout)
	defer cancel()
	report, err := sshtunnel.Query(probeContext, sshtunnel.Options{Destination: target.SSH})
	switch {
	case errors.Is(err, sshtunnel.ErrNotRunning):
		return "not running", ""
	case err != nil:
		return "unavailable", err.Error()
	}
	if clienttransport.CheckCompatibility(report.ProtocolVersion, report.KitVersion) != nil {
		return "incompatible", fmt.Sprintf("it runs Kit %s (protocol %d) and this is Kit %s (protocol %d)", report.KitVersion, report.ProtocolVersion, version.Version, version.SessionProtocolVersion)
	}
	return "running", ""
}

// errNoToken reports a URL server whose device token is missing.
var errNoToken = errors.New("no device token is saved for this server")

// needsPairing reports a failure that pairing again resolves: a missing,
// revoked, or expired device token.
func needsPairing(err error) bool {
	var failure *kit.ServerError
	return errors.Is(err, errNoToken) ||
		(errors.As(err, &failure) && (failure.Code == kit.ErrorUnauthorized || failure.Code == kit.ErrorCredentialExpired))
}

// connectTarget connects to an explicit target's server without starting it.
func connectTarget(ctx context.Context, target attachTarget) (*kit.Client, error) {
	if target.URL == "" {
		return kit.Connect(ctx, kit.Home(target.Home))
	}
	_, tokens, _, err := serverStores()
	if err != nil {
		return nil, err
	}
	token, err := tokens.Get(target.Name, target.URL)
	if errors.Is(err, savedservers.ErrNotFound) {
		return nil, errNoToken
	}
	if err != nil {
		return nil, err
	}
	return kit.Connect(ctx, kit.Endpoint(target.URL, kit.AccessToken(token)))
}

// connectAttachTarget connects to target's server, explaining how to recover
// from the usual failures. It starts only an SSH server, and lets ssh prompt
// on the terminal while it connects.
func connectAttachTarget(ctx context.Context, target attachTarget) (*kit.Client, error) {
	if target.SSH != "" {
		client, err := kit.Connect(ctx, kit.SSH(target.SSH).WithSSHPrompts(os.Stderr))
		if err != nil {
			return nil, fmt.Errorf("attach to %s over SSH: %w", target.Name, err)
		}
		return client, nil
	}
	connectContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, err := connectTarget(connectContext, target)
	switch {
	case err == nil:
		return client, nil
	case errors.Is(err, os.ErrNotExist) && target.URL == "":
		return nil, fmt.Errorf("server %q is not running; start it with `KIT_HOME=%s kit server start`", target.Name, target.Home)
	case needsPairing(err):
		return nil, fmt.Errorf("server %q no longer accepts this device's token; get a new link with `kit server pair` on the server, then run `kit attach LINK`", target.Name)
	default:
		return nil, fmt.Errorf("attach to server %q: %w", target.Name, err)
	}
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
	store, tokens, _, err := serverStores()
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
			fmt.Fprintln(stdout, "No saved servers. Add one with `kit servers add NAME LOCATION`.")
			return 0
		}
		table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(table, "NAME\tLOCATION\tSTATUS")
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
		return addServer(ctx, store, tokens, args[1], args[2], stdout, stderr)
	case len(args) == 2 && args[0] == "remove":
		server, err := store.Find(args[1])
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		if err := store.Remove(server.Name); err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		if err := tokens.Delete(server.Name); err != nil {
			fmt.Fprintf(stderr, "kit: removed server %q but not its device token: %v\n", server.Name, err)
			return 1
		}
		fmt.Fprintf(stdout, "removed server %q\n", server.Name)
		return 0
	default:
		fmt.Fprintln(stderr, "usage: kit servers <list|add NAME LOCATION|remove NAME>\n\nLOCATION is a Kit home path, a pairing link, or an SSH destination such as user@host.")
		return 2
	}
}

// addServer saves a Kit home or an SSH destination, or pairs with a pairing
// link and saves its server with the device token. A location that is
// neither a link, a URL, nor an existing directory or path is an SSH
// destination.
func addServer(ctx context.Context, store *savedservers.Store, tokens *savedservers.Tokens, name, location string, stdout, stderr io.Writer) int {
	if err := savedservers.ValidateName(name); err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 2
	}
	// Check the name before redeeming, which spends the pairing code.
	if _, err := store.Find(name); err == nil {
		fmt.Fprintf(stderr, "kit: %v: %q\n", savedservers.ErrExists, name)
		return 1
	}
	switch {
	case isPairingLink(location):
		serverURL, _, err := pairing.ParseLink(location)
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 2
		}
		serverURL = strings.ToLower(serverURL)
		if existing, err := store.FindURL(serverURL); err == nil {
			fmt.Fprintf(stderr, "kit: %s is already saved as %q; re-pair it with `kit attach LINK`\n", serverURL, existing.Name)
			return 1
		}
		// Saving the server first claims the name, so a concurrent add of
		// the same name fails before pairing rather than replacing a token.
		if err := store.Add(savedservers.Server{Name: name, Kind: savedservers.KindURL, URL: serverURL}); err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		if err := pairServer(ctx, tokens, name, location, stderr); err != nil {
			_ = store.Remove(name)
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "paired and saved server %q: %s\n", name, serverURL)
		return 0
	case isURL(location):
		fmt.Fprintf(stderr, "kit: a server URL needs pairing; run `kit server pair` on that server and add the link it prints\n")
		return 2
	}
	if isSSHURL(location) || (!isHomePath(location) && !isDirectory(location)) {
		if err := store.Add(savedservers.Server{Name: name, Kind: savedservers.KindSSH, Destination: location}); err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "saved server %q: %s over SSH\n", name, location)
		return 0
	}
	home, err := resolveServerHome(location)
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	if err := store.Add(savedservers.Server{Name: name, Kind: savedservers.KindHome, Home: home}); err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "saved server %q: %s\n", name, home)
	return 0
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
