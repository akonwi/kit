package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemonctl"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/serverlisten"
	"github.com/akonwi/kit/internal/settings"
)

// tailnet is the part of the Tailscale CLI that exposing a server uses.
type tailnet interface {
	// Status reports this machine's tailnet DNS name, without a trailing
	// dot, and whether Tailscale is running with HTTPS certificates for it.
	Status(ctx context.Context) (tailnetStatus, error)
	// Served describes the HTTPS handler at host:port.
	Served(ctx context.Context, host string, port int) (servedHandler, error)
	// Serve proxies HTTPS on port to target in the background.
	Serve(ctx context.Context, port int, target string) error
	// Unserve removes the HTTPS handler on port.
	Unserve(ctx context.Context, port int) error
}

type tailnetStatus struct {
	Running    bool
	DNSName    string
	HTTPSCerts bool
}

// servedHandler is Tailscale Serve's configuration for one host:port.
type servedHandler struct {
	// Proxy is the proxy target of the root handler, or "" when none is set.
	Proxy string
	// Funnel reports that Tailscale Funnel exposes host:port publicly.
	Funnel bool
}

// tailscaleCLI runs the tailscale executable.
type tailscaleCLI struct{ path string }

// findTailscale locates the tailscale CLI on PATH, or inside the macOS app.
func findTailscale() (tailnet, error) {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return tailscaleCLI{path: path}, nil
	}
	if runtime.GOOS == "darwin" {
		const app = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
		if _, err := os.Stat(app); err == nil {
			return tailscaleCLI{path: app}, nil
		}
	}
	return nil, errors.New("the tailscale command was not found; install Tailscale and its CLI")
}

func (t tailscaleCLI) run(ctx context.Context, args ...string) ([]byte, error) {
	runContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(runContext, t.path, args...)
	command.Stdin = nil
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("tailscale %s: %w: %s", strings.Join(args, " "), err, message)
	}
	return stdout.Bytes(), nil
}

func (t tailscaleCLI) Status(ctx context.Context) (tailnetStatus, error) {
	output, err := t.run(ctx, "status", "--json")
	if err != nil {
		return tailnetStatus{}, err
	}
	var status struct {
		BackendState string
		Self         struct{ DNSName string }
		CertDomains  []string
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return tailnetStatus{}, fmt.Errorf("decode tailscale status: %w", err)
	}
	name := strings.TrimSuffix(status.Self.DNSName, ".")
	return tailnetStatus{
		Running: status.BackendState == "Running", DNSName: name,
		HTTPSCerts: name != "" && slices.Contains(status.CertDomains, name),
	}, nil
}

func (t tailscaleCLI) Served(ctx context.Context, host string, port int) (servedHandler, error) {
	output, err := t.run(ctx, "serve", "status", "--json")
	if err != nil {
		return servedHandler{}, err
	}
	var config struct {
		Web map[string]struct {
			Handlers map[string]struct{ Proxy string }
		}
		AllowFunnel map[string]bool
	}
	if err := json.Unmarshal(output, &config); err != nil {
		return servedHandler{}, fmt.Errorf("decode tailscale serve status: %w", err)
	}
	hostPort := net.JoinHostPort(host, strconv.Itoa(port))
	return servedHandler{Proxy: config.Web[hostPort].Handlers["/"].Proxy, Funnel: config.AllowFunnel[hostPort]}, nil
}

func (t tailscaleCLI) Serve(ctx context.Context, port int, target string) error {
	_, err := t.run(ctx, "serve", "--bg", "--https="+strconv.Itoa(port), target)
	return err
}

func (t tailscaleCLI) Unserve(ctx context.Context, port int) error {
	_, err := t.run(ctx, "serve", "--https="+strconv.Itoa(port), "off")
	return err
}

// exposure records what `kit server expose --tailscale` added, so `--off`
// removes exactly that.
type exposure struct {
	// Host is the tailnet DNS name served and added to server.allowedHosts.
	Host string `json:"host"`
	// HTTPSPort is the tailnet HTTPS port Tailscale Serve listens on.
	HTTPSPort int `json:"httpsPort"`
	// Target is the daemon URL Tailscale Serve proxies to.
	Target string `json:"target"`
	// AddedListen is the server.listen value expose set, or "" when the
	// listener was already fixed.
	AddedListen string `json:"addedListen,omitempty"`
	// PreviousListen is the server.listen value AddedListen replaced, or ""
	// when it was unset.
	PreviousListen string `json:"previousListen,omitempty"`
	// AddedHost reports whether expose added Host to server.allowedHosts.
	AddedHost bool `json:"addedHost,omitempty"`
}

func exposurePath(paths apphome.Paths) string {
	return filepath.Join(paths.Home, "tailscale-serve.json")
}

func loadExposure(paths apphome.Paths) (exposure, bool, error) {
	data, err := os.ReadFile(exposurePath(paths))
	if errors.Is(err, os.ErrNotExist) {
		return exposure{}, false, nil
	}
	if err != nil {
		return exposure{}, false, err
	}
	var record exposure
	if err := json.Unmarshal(data, &record); err != nil {
		return exposure{}, false, fmt.Errorf("decode %s: %w", exposurePath(paths), err)
	}
	return record, true, nil
}

func saveExposure(paths apphome.Paths, record exposure) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(exposurePath(paths), append(data, '\n'), 0o600)
}

// servedURL is the tailnet URL of host on an HTTPS port.
func servedURL(host string, port int) string {
	if port == 443 {
		return "https://" + host
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// freeLoopbackPort finds a currently unused loopback port.
func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// exposeOptions are the dependencies and flags of one expose command.
type exposeOptions struct {
	tailnet func() (tailnet, error)
	// ensure starts the server on an address, restarting it when changed.
	ensure func(context.Context, apphome.Paths, serverlisten.Address, bool) error
	// restart restarts the server when one is running.
	restart func(context.Context, apphome.Paths) error
	// foreground reports whether the running server was started with
	// `kit server run`, so its supervisor, not Kit, must restart it.
	foreground func(context.Context, apphome.Paths) bool
	httpsPort  int
	// force lets --off undo settings when the Serve handler cannot be
	// confirmed removed.
	force    bool
	terminal bool
}

func defaultExposeOptions(httpsPort int, force, terminal bool) exposeOptions {
	return exposeOptions{
		tailnet: findTailscale, ensure: ensureServer, restart: restartIfRunning, foreground: runningInForeground,
		httpsPort: httpsPort, force: force, terminal: terminal,
	}
}

func runExpose(ctx context.Context, options exposeOptions, off bool, stdout, stderr io.Writer) int {
	fail := func(format string, arguments ...any) int {
		fmt.Fprintf(stderr, "kit: "+format+"\n", arguments...)
		return 1
	}
	paths, err := apphome.Resolve("")
	if err != nil {
		return fail("%v", err)
	}
	store, err := settings.NewStore(paths.Settings)
	if err != nil {
		return fail("%v", err)
	}
	tailscale, err := options.tailnet()
	if off {
		return unexpose(ctx, options, paths, store, tailscale, err, stdout, stderr)
	}
	if err != nil {
		return fail("%v", err)
	}
	status, err := tailscale.Status(ctx)
	switch {
	case err != nil:
		return fail("%v", err)
	case !status.Running || status.DNSName == "":
		return fail("Tailscale is not running on this machine; start it and log in, then try again")
	case !status.HTTPSCerts:
		return fail("your tailnet does not issue HTTPS certificates for %s; enable HTTPS in the Tailscale admin console's DNS settings, then try again", status.DNSName)
	}
	url := servedURL(status.DNSName, options.httpsPort)
	previous, exposed, err := loadExposure(paths)
	if err != nil {
		return fail("%v", err)
	}
	if exposed && (previous.Host != status.DNSName || previous.HTTPSPort != options.httpsPort) {
		return fail("this server is already exposed at %s; run `kit server expose --tailscale --off` first", servedURL(previous.Host, previous.HTTPSPort))
	}
	current, _, err := store.Load()
	if err != nil {
		return fail("load settings: %v", err)
	}

	// Tailscale Serve proxies to a fixed port. An unset or ephemeral listener
	// is fixed on its host, keeping the server's current port when it runs.
	record := exposure{
		Host: status.DNSName, HTTPSPort: options.httpsPort,
		AddedListen: previous.AddedListen, PreviousListen: previous.PreviousListen, AddedHost: previous.AddedHost,
	}
	listen := current.ServerListen
	if listen == "" {
		listen = serverlisten.Default
	}
	address, err := serverlisten.Parse(listen)
	if err != nil {
		return fail("%v", err)
	}
	change := settings.ServerAccessChange{}
	if address.Port() == 0 {
		port := 0
		if registry, err := localdiscovery.Load(paths); err == nil {
			port = registryPort(registry.URL)
		}
		if port == 0 {
			if port, err = freeLoopbackPort(); err != nil {
				return fail("choose a port: %v", err)
			}
		}
		fixed := strings.TrimSuffix(address.String(), ":0") + ":" + strconv.Itoa(port)
		if address, err = serverlisten.Parse(fixed); err != nil {
			return fail("%v", err)
		}
		change.SetListen, change.Listen = true, address.String()
		record.AddedListen, record.PreviousListen = address.String(), current.ServerListen
	}
	record.Target = "http://" + netip.AddrPortFrom(address.Loopback(), uint16(address.Port())).String()
	if !slices.Contains(current.ServerAllowedHosts, status.DNSName) {
		change.AddHosts = []string{status.DNSName}
		record.AddedHost = true
	}
	changed := change.SetListen || len(change.AddHosts) > 0

	served, err := tailscale.Served(ctx, status.DNSName, options.httpsPort)
	switch {
	case err != nil:
		return fail("%v", err)
	case served.Funnel:
		return fail("Tailscale Funnel is on for %s, which would put this server on the public internet; turn it off with `tailscale funnel --https=%d off`, or choose another port with --https-port", url, options.httpsPort)
	case served.Proxy != "" && served.Proxy != record.Target && served.Proxy != previous.Target:
		return fail("%s already serves %s; choose another port with --https-port", url, served.Proxy)
	}

	if err := saveExposure(paths, record); err != nil {
		return fail("record exposure: %v", err)
	}
	if changed {
		if _, err := store.UpdateServerAccess(change); err != nil {
			return fail("%v", err)
		}
	}
	// A foreground server belongs to whatever started it, which must apply
	// the new settings by restarting it.
	supervised := options.foreground(ctx, paths)
	if !supervised {
		if err := options.ensure(ctx, paths, address, changed); err != nil {
			return fail("%v", err)
		}
	}
	if err := tailscale.Serve(ctx, options.httpsPort, record.Target); err != nil {
		return fail("%v", err)
	}
	if served, err := tailscale.Served(ctx, status.DNSName, options.httpsPort); err != nil || served.Funnel {
		return fail("could not confirm that Tailscale Funnel is off for %s; check `tailscale serve status`", url)
	}

	fmt.Fprintf(stdout, "Serving on your tailnet at %s\n", url)
	if address.Exposed() {
		fmt.Fprintf(stdout, "The server also listens on every interface (server.listen %s), so devices that\ncan reach this machine directly can connect too, with Kit's credentials as the\nonly barrier.", address)
	} else {
		fmt.Fprint(stdout, "Only devices on your tailnet can reach it.")
	}
	fmt.Fprintln(stdout, " Stop with `kit server expose --tailscale --off`.")
	fmt.Fprintln(stdout)
	if supervised && changed {
		fmt.Fprintln(stdout, "The server runs in the foreground with `kit server run`. Restart it with whatever\nstarted it, such as its service manager, to apply the new settings, then pair a\ndevice with `kit server pair`.")
		return 0
	}
	client, err := kit.Connect(ctx, kit.Local())
	if err != nil {
		return fail("connect to server: %v", err)
	}
	defer client.Close()
	code, err := client.CreatePairing(ctx)
	if err != nil {
		return fail("create pairing code: %v", err)
	}
	printPairing(stdout, code, []string{url}, options.terminal)
	return 0
}

// unexpose removes the Serve handler and settings that expose added. Unless
// forced, it keeps them and fails while the handler may still proxy to the
// server's port: a process that later binds that port would be served on the
// tailnet.
func unexpose(ctx context.Context, options exposeOptions, paths apphome.Paths, store *settings.Store, tailscale tailnet, tailnetErr error, stdout, stderr io.Writer) int {
	fail := func(format string, arguments ...any) int {
		fmt.Fprintf(stderr, "kit: "+format+"\n", arguments...)
		return 1
	}
	record, found, err := loadExposure(paths)
	if err != nil {
		return fail("%v", err)
	}
	if !found {
		fmt.Fprintln(stdout, "This server is not exposed with --tailscale.")
		return 0
	}
	url := servedURL(record.Host, record.HTTPSPort)
	err = tailnetErr
	if err == nil {
		var served servedHandler
		if served, err = tailscale.Served(ctx, record.Host, record.HTTPSPort); err == nil && served.Proxy == record.Target {
			err = tailscale.Unserve(ctx, record.HTTPSPort)
		}
	}
	if err != nil {
		if !options.force {
			return fail("%v\nCould not confirm that %s no longer proxies to %s. Fix Tailscale and try again,\nor remove the handler with `tailscale serve --https=%d off` and run\n`kit server expose --tailscale --off --force`.", err, url, record.Target, record.HTTPSPort)
		}
		fmt.Fprintf(stderr, "kit: warning: %v; remove the handler with `tailscale serve --https=%d off`\n", err, record.HTTPSPort)
	}
	current, _, err := store.Load()
	if err != nil {
		return fail("load settings: %v", err)
	}
	change := settings.ServerAccessChange{}
	if record.AddedListen != "" && current.ServerListen == record.AddedListen {
		change.SetListen, change.Listen = true, record.PreviousListen
	}
	if record.AddedHost && slices.Contains(current.ServerAllowedHosts, record.Host) {
		change.RemoveHosts = []string{record.Host}
	}
	changed := change.SetListen || len(change.RemoveHosts) > 0
	if changed {
		if _, err := store.UpdateServerAccess(change); err != nil {
			return fail("%v", err)
		}
	}
	if err := os.Remove(exposurePath(paths)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail("%v", err)
	}
	fmt.Fprintf(stdout, "Stopped serving %s on your tailnet.\n", url)
	if changed {
		if options.foreground(ctx, paths) {
			fmt.Fprintln(stdout, "The server runs in the foreground with `kit server run`. Restart it with whatever\nstarted it to apply the restored settings.")
		} else if err := options.restart(ctx, paths); err != nil {
			return fail("%v", err)
		}
	}
	return 0
}

func registryPort(published string) int {
	address, err := netip.ParseAddrPort(strings.TrimPrefix(published, "http://"))
	if err != nil {
		return 0
	}
	return int(address.Port())
}

// ensureServer starts this home's server on address, restarting it when its
// settings changed or it listens elsewhere.
func ensureServer(ctx context.Context, paths apphome.Paths, address serverlisten.Address, changed bool) error {
	manager := daemonctl.NewManager(paths, daemonctl.WithListen(address))
	operation, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if changed {
		if err := manager.Stop(operation); err != nil {
			return fmt.Errorf("stop server: %w", err)
		}
	}
	_, err := manager.Ensure(operation)
	if errors.Is(err, daemonctl.ErrListenMismatch) {
		if err := manager.Stop(operation); err != nil {
			return fmt.Errorf("stop server: %w", err)
		}
		_, err = manager.Ensure(operation)
	}
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	return nil
}

// restartIfRunning restarts this home's server when one is running.
func restartIfRunning(ctx context.Context, paths apphome.Paths) error {
	manager := daemonctl.NewManager(paths)
	operation, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, _, err := manager.Status(operation); err != nil {
		return nil
	}
	if err := manager.Restart(operation); err != nil {
		return fmt.Errorf("restart server: %w", err)
	}
	return nil
}
