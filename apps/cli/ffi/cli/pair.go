package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/pairing"
	"github.com/akonwi/kit/internal/settings"
	"rsc.io/qr"
)

// connectOwnServer attaches to this home's running server without starting it.
func connectOwnServer(ctx context.Context) (*kit.Client, apphome.Paths, error) {
	paths, err := apphome.Resolve("")
	if err != nil {
		return nil, apphome.Paths{}, err
	}
	connectContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, err := kit.Connect(connectContext, kit.Local())
	if errors.Is(err, os.ErrNotExist) {
		return nil, paths, errors.New("server is not running; start it with `kit server start`")
	}
	if err != nil {
		return nil, paths, fmt.Errorf("connect to server: %w", err)
	}
	return client, paths, nil
}

// validServedURL reports whether value is an HTTP(S) origin for --url.
func validServedURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("--url %q must be an HTTP(S) origin such as https://kit.example.com", value)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

// runPair prints pairing links for server: this home's server when it is
// empty or LocalName, otherwise a saved name, URL, or Kit home path. Links use
// every URL known to reach the server, and the extra URLs.
func runPair(ctx context.Context, server string, extraURLs []string, stdout, stderr io.Writer, terminal bool) int {
	served := make([]string, 0, len(extraURLs))
	for _, value := range extraURLs {
		origin, err := validServedURL(value)
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 2
		}
		served = append(served, origin)
	}
	if isPairingLink(server) {
		fmt.Fprintln(stderr, "kit: kit pair takes a server, not a pairing link; attach with `kit attach LINK`")
		return 2
	}
	target := attachTarget{}
	if server != "" {
		var err error
		if target, err = resolveAttachTarget(ctx, server, stderr); err != nil {
			return reportAttachError(stderr, err)
		}
	}
	var client *kit.Client
	var err error
	var paths apphome.Paths
	switch {
	case target.own():
		client, paths, err = connectOwnServer(ctx)
	case target.Home != "":
		paths = apphome.FromHome(target.Home)
		client, err = connectAttachTarget(ctx, target)
	default:
		client, err = connectAttachTarget(ctx, target)
	}
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	defer client.Close()
	code, err := client.CreatePairing(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "kit: create pairing code: %v\n", err)
		return 1
	}
	var urls []string
	if target.URL != "" {
		urls = pairing.ServedURLs(nil, nil, "", append([]string{target.URL}, served...)...)
	} else {
		urls = homeServedURLs(paths, served)
	}
	printPairing(stdout, code, urls, terminal)
	return 0
}

// homeServedURLs lists the URLs that reach the server of a Kit home on this
// machine, from its settings and discovery registry.
func homeServedURLs(paths apphome.Paths, extra []string) []string {
	var hosts, origins []string
	if store, err := settings.NewStore(paths.Settings); err == nil {
		if loaded, _, err := store.Load(); err == nil {
			hosts, origins = loaded.ServerAllowedHosts, loaded.ServerAllowedOrigins
		}
	}
	loopback := ""
	if registry, err := localdiscovery.Load(paths); err == nil {
		loopback = registry.URL
	}
	return pairing.ServedURLs(hosts, origins, loopback, extra...)
}

// printPairing prints a link per URL, with notes and a QR code of the first.
func printPairing(stdout io.Writer, code kit.Pairing, urls []string, terminal bool) {
	fmt.Fprintf(stdout, "Pair a device with one of these links. They share one code, which works once, until %s.\n\n", code.ExpiresAt.Local().Format("15:04"))
	for _, served := range urls {
		fmt.Fprintf(stdout, "  %s\n", code.Link(served))
	}
	if len(urls) == 1 && isLoopbackURL(urls[0]) {
		fmt.Fprintln(stdout, "\nOnly this machine can reach the server. Add server.allowedHosts or pass --url URL\nfor links that other devices can reach.")
	}
	for _, served := range urls {
		if !securePairingURL(served) {
			fmt.Fprintf(stdout, "\nWarning: %s is plain HTTP. Its pairing code and device token cross the network\nunencrypted unless the network itself is private, such as a tailnet.\n", served)
		}
	}
	if terminal && len(urls) > 0 {
		if rendered, err := renderQR(code.Link(urls[0])); err == nil {
			fmt.Fprintf(stdout, "\n%s", rendered)
		}
	}
}

func isLoopbackURL(served string) bool {
	parsed, err := url.Parse(served)
	if err != nil {
		return false
	}
	if parsed.Hostname() == "localhost" {
		return true
	}
	address, err := netip.ParseAddr(parsed.Hostname())
	return err == nil && address.IsLoopback()
}

// securePairingURL reports whether a link to served keeps its code and token
// off the network in plaintext: HTTPS, or HTTP to a loopback address.
func securePairingURL(served string) bool {
	parsed, err := url.Parse(served)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" || isLoopbackURL(served)
}

// renderQR draws text as a QR code for a terminal, two modules per character
// row, light modules in bright white on black so dark and light terminal
// themes both scan, with the standard four-module quiet zone.
func renderQR(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	size := code.Size + 2*quiet
	light := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x < 0 || y < 0 || x >= code.Size || y >= code.Size || !code.Black(x, y)
	}
	var builder strings.Builder
	for y := 0; y < size; y += 2 {
		builder.WriteString("\x1b[97;40m")
		for x := 0; x < size; x++ {
			top, bottom := light(x, y), y+1 < size && light(x, y+1)
			switch {
			case top && bottom:
				builder.WriteString("█")
			case top:
				builder.WriteString("▀")
			case bottom:
				builder.WriteString("▄")
			default:
				builder.WriteString(" ")
			}
		}
		builder.WriteString("\x1b[0m\n")
	}
	return builder.String(), nil
}

func runServerDevices(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	client, _, err := connectOwnServer(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	defer client.Close()
	// A device ID is revoked directly, so revoking never depends on listing.
	if len(args) == 2 && args[0] == "revoke" && identifier.Valid(args[1], "device_") {
		if err := client.RevokeDevice(ctx, args[1]); err != nil {
			fmt.Fprintf(stderr, "kit: revoke device: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "revoked device %s\n", args[1])
		return 0
	}
	devices, err := client.Devices(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "kit: list devices: %v\n", err)
		return 1
	}
	switch {
	case len(args) == 0 || (len(args) == 1 && args[0] == "list"):
		if len(devices) == 0 {
			fmt.Fprintln(stdout, "No paired devices. Pair one with `kit server pair`.")
			return 0
		}
		table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(table, "NAME\tID\tLAST USED\tLAST ADDRESS\tEXPIRES")
		now := time.Now()
		for _, device := range devices {
			expires := formatDeviceTime(device.ExpiresAt)
			if parsed, err := time.Parse(time.RFC3339Nano, device.ExpiresAt); err == nil && !now.Before(parsed) {
				expires = "expired"
			}
			address := device.LastAddress
			if address == "" {
				address = "-"
			}
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", device.Name, device.ID, formatDeviceTime(device.LastUsedAt), address, expires)
		}
		if err := table.Flush(); err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		return 0
	case len(args) == 2 && args[0] == "revoke":
		var matches []kit.DeviceInfo
		for _, device := range devices {
			if device.Name == args[1] {
				matches = append(matches, device)
			}
		}
		switch len(matches) {
		case 0:
			fmt.Fprintf(stderr, "kit: no device is named %q; list them with `kit server devices`\n", args[1])
			return 1
		case 1:
		default:
			fmt.Fprintf(stderr, "kit: %d devices are named %q; revoke one by ID\n", len(matches), args[1])
			return 1
		}
		if err := client.RevokeDevice(ctx, matches[0].ID); err != nil {
			fmt.Fprintf(stderr, "kit: revoke device: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "revoked device %q (%s)\n", matches[0].Name, matches[0].ID)
		return 0
	default:
		fmt.Fprintln(stderr, "usage: kit server devices [list|revoke ID-OR-NAME]")
		return 2
	}
}

func formatDeviceTime(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

// PairingLinks is a new pairing code as the terminal client presents it.
type PairingLinks struct {
	// Links has one pairing link per URL that reaches the server.
	Links []string
	// Expires is the code's local expiry time, such as "14:05".
	Expires string
}

// CreatePairingLinks issues a pairing code on the attached server and links
// it at each of urls.
func CreatePairingLinks(ctx context.Context, client *kit.Client, urls []string) (PairingLinks, error) {
	if len(urls) == 0 {
		return PairingLinks{}, errors.New("no URL reaches this server")
	}
	code, err := client.CreatePairing(ctx)
	if err != nil {
		return PairingLinks{}, err
	}
	links := make([]string, 0, len(urls))
	for _, served := range urls {
		links = append(links, code.Link(served))
	}
	return PairingLinks{Links: links, Expires: code.ExpiresAt.Local().Format("15:04")}, nil
}
