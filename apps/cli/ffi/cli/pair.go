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

func runServerPair(ctx context.Context, extraURLs []string, stdout, stderr io.Writer, terminal bool) int {
	served := make([]string, 0, len(extraURLs))
	for _, value := range extraURLs {
		origin, err := validServedURL(value)
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 2
		}
		served = append(served, origin)
	}
	client, paths, err := connectOwnServer(ctx)
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
	urls := pairing.ServedURLs(hosts, origins, loopback, served...)
	fmt.Fprintf(stdout, "Pair a device with one of these links. They share one code, which works once, until %s.\n\n", code.ExpiresAt.Local().Format("15:04"))
	for _, served := range urls {
		fmt.Fprintf(stdout, "  %s\n", code.Link(served))
	}
	if len(urls) == 1 && urls[0] == loopback {
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
	return 0
}

// securePairingURL reports whether a link to served keeps its code and token
// off the network in plaintext: HTTPS, or HTTP to a loopback address.
func securePairingURL(served string) bool {
	parsed, err := url.Parse(served)
	if err != nil {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
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
