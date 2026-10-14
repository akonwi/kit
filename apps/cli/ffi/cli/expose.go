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
	// Served returns the proxy target of the HTTPS handler at host:port, or
	// "" when none is configured.
	Served(ctx context.Context, host string, port int) (string, error)
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

func (t tailscaleCLI) Served(ctx context.Context, host string, port int) (string, error) {
	output, err := t.run(ctx, "serve", "status", "--json")
	if err != nil {
		return "", err
	}
	var config struct {
		Web map[string]struct {
			Handlers map[string]struct{ Proxy string }
		}
	}
	if err := json.Unmarshal(output, &config); err != nil {
		return "", fmt.Errorf("decode tailscale serve status: %w", err)
	}
	return config.Web[net.JoinHostPort(host, strconv.Itoa(port))].Handlers["/"].Proxy, nil
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
	restart   func(context.Context, apphome.Paths) error
	httpsPort int
	terminal  bool
}

func defaultExposeOptions(httpsPort int, terminal bool) exposeOptions {
	return exposeOptions{tailnet: findTailscale, ensure: ensureServer, restart: restartIfRunning, httpsPort: httpsPort, terminal: terminal}
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
		if err != nil {
			fmt.Fprintf(stderr, "kit: warning: %v; remove the Serve handler yourself with `tailscale serve reset` if it remains\n", err)
			tailscale = nil
		}
		return unexpose(ctx, options, paths, store, tailscale, stdout, stderr)
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
	current, _, err := store.Load()
	if err != nil {
		return fail("load settings: %v", err)
	}
	previous, _, err := loadExposure(paths)
	if err != nil {
		return fail("%v", err)
	}

	// Tailscale Serve proxies to a fixed loopback port.
	listen := current.ServerListen
	addedListen := previous.AddedListen
	var address serverlisten.Address
	if listen != "" {
		if address, err = serverlisten.Parse(listen); err != nil {
			return fail("%v", err)
		}
	}
	if listen == "" || address.Port() == 0 {
		port := 0
		if registry, err := localdiscovery.Load(paths); err == nil {
			port = registryPort(registry.URL)
		}
		if port == 0 {
			if port, err = freeLoopbackPort(); err != nil {
				return fail("choose a port: %v", err)
			}
		}
		listen = "127.0.0.1:" + strconv.Itoa(port)
		addedListen = listen
		address = serverlisten.MustParse(listen)
	}
	target := "http://" + netip.AddrPortFrom(address.Loopback(), uint16(address.Port())).String()

	served, err := tailscale.Served(ctx, status.DNSName, options.httpsPort)
	if err != nil {
		return fail("%v", err)
	}
	if served != "" && served != target && served != previous.Target {
		return fail("%s already serves %s; choose another port with --https-port", servedURL(status.DNSName, options.httpsPort), served)
	}

	hosts := current.ServerAllowedHosts
	addedHost := previous.AddedHost && previous.Host == status.DNSName
	if !slices.Contains(hosts, status.DNSName) {
		hosts = append(slices.Clone(hosts), status.DNSName)
		addedHost = true
	}
	changed := listen != current.ServerListen || len(hosts) != len(current.ServerAllowedHosts)
	record := exposure{Host: status.DNSName, HTTPSPort: options.httpsPort, Target: target, AddedListen: addedListen, AddedHost: addedHost}
	if err := saveExposure(paths, record); err != nil {
		return fail("record exposure: %v", err)
	}
	if changed {
		if _, err := store.UpdateServerAccess(listen, hosts); err != nil {
			return fail("%v", err)
		}
	}
	if err := options.ensure(ctx, paths, address, changed); err != nil {
		return fail("%v", err)
	}
	if err := tailscale.Serve(ctx, options.httpsPort, target); err != nil {
		return fail("%v", err)
	}

	url := servedURL(status.DNSName, options.httpsPort)
	fmt.Fprintf(stdout, "Serving on your tailnet at %s\n", url)
	fmt.Fprintln(stdout, "Only devices on your tailnet can reach it. Stop with `kit server expose --tailscale --off`.")
	fmt.Fprintln(stdout)
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

func unexpose(ctx context.Context, options exposeOptions, paths apphome.Paths, store *settings.Store, tailscale tailnet, stdout, stderr io.Writer) int {
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
	// Remove the Serve handler only while it still proxies to this server.
	if tailscale != nil {
		served, err := tailscale.Served(ctx, record.Host, record.HTTPSPort)
		if err == nil && served == record.Target {
			err = tailscale.Unserve(ctx, record.HTTPSPort)
		}
		if err != nil {
			fmt.Fprintf(stderr, "kit: warning: %v; remove the Serve handler yourself if it remains\n", err)
		}
	}
	current, _, err := store.Load()
	if err != nil {
		return fail("load settings: %v", err)
	}
	listen, hosts := current.ServerListen, current.ServerAllowedHosts
	if record.AddedListen != "" && listen == record.AddedListen {
		listen = ""
	}
	if record.AddedHost {
		hosts = slices.DeleteFunc(slices.Clone(hosts), func(host string) bool { return host == record.Host })
	}
	changed := listen != current.ServerListen || len(hosts) != len(current.ServerAllowedHosts)
	if changed {
		if _, err := store.UpdateServerAccess(listen, hosts); err != nil {
			return fail("%v", err)
		}
	}
	if err := os.Remove(exposurePath(paths)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail("%v", err)
	}
	if changed {
		if err := options.restart(ctx, paths); err != nil {
			return fail("%v", err)
		}
	}
	fmt.Fprintf(stdout, "Stopped serving %s on your tailnet.\n", servedURL(record.Host, record.HTTPSPort))
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
