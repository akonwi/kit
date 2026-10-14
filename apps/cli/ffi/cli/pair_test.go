package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemonctl"
	"github.com/akonwi/kit/internal/pairing"
	kitserver "github.com/akonwi/kit/internal/server"
	"rsc.io/qr"
)

// startDaemon runs this home's server in process and stops it when the test
// ends.
func startDaemon(t *testing.T, home string) {
	t.Helper()
	paths := apphome.FromHome(home)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- kitserver.Run(ctx, kitserver.RunOptions{Paths: paths, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-result:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	manager := daemonctl.NewManager(paths)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, err := manager.Status(context.Background()); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runCLI(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := executeCommand(context.Background(), args, &stdout, &stderr, defaultCommandDependencies())
	return code, stdout.String(), stderr.String()
}

func TestPairingAndDevicesEndToEnd(t *testing.T) {
	home := filepath.Join(t.TempDir(), "kit")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"server":{"allowedHosts":["kit.example.ts.net"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(apphome.EnvHome, home)
	startDaemon(t, home)

	code, stdout, stderr := runCLI("server", "pair", "--url", "https://proxy.example/")
	if code != 0 {
		t.Fatalf("server pair: exit %d, stderr %q", code, stderr)
	}
	var links []string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "  ") {
			links = append(links, strings.TrimSpace(line))
		}
	}
	if len(links) != 3 || !strings.HasPrefix(links[0], "https://kit.example.ts.net/pair#") ||
		!strings.HasPrefix(links[1], "https://proxy.example/pair#") || !strings.HasPrefix(links[2], "http://127.0.0.1:") {
		t.Fatalf("links = %q\n%s", links, stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatal("a QR code was drawn for output that is not a terminal")
	}

	// Another device redeems the loopback link, as an SSH-forwarded or
	// same-machine client would, and connects with its device token.
	paired, err := kit.Pair(t.Context(), links[2], "laptop")
	if err != nil {
		t.Fatalf("Pair() error = %v", err)
	}
	remote, err := kit.Connect(t.Context(), kit.Endpoint(paired.ServerURL, kit.AccessToken(paired.Token)))
	if err != nil {
		t.Fatalf("Connect() with the device token: %v", err)
	}
	defer remote.Close()
	if _, err := kit.Pair(t.Context(), links[0], "again"); err == nil {
		t.Fatal("a pairing code was redeemed twice")
	}
	if listed, err := remote.Devices(t.Context()); err != nil || len(listed) != 1 || !listed[0].Current {
		t.Fatalf("Devices() = %+v, %v", listed, err)
	}

	code, stdout, stderr = runCLI("server", "devices")
	if code != 0 || !strings.HasPrefix(stdout, "NAME") || !strings.Contains(stdout, "laptop  "+paired.Device.ID) || !strings.Contains(stdout, "127.0.0.1") {
		t.Fatalf("devices: exit %d\n%s%s", code, stdout, stderr)
	}
	code, stdout, stderr = runCLI("server", "devices", "revoke", "laptop")
	if code != 0 || stdout != "revoked device \"laptop\" ("+paired.Device.ID+")\n" {
		t.Fatalf("revoke: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if code, _, stderr := runCLI("server", "devices", "revoke", paired.Device.ID); code != 1 || !strings.Contains(stderr, "device not found") {
		t.Fatalf("revoke by ID again: exit %d, stderr %q", code, stderr)
	}
	_, err = remote.Devices(t.Context())
	var failure *kit.ServerError
	if !errors.As(err, &failure) || failure.Code != kit.ErrorUnauthorized {
		t.Fatalf("revoked device error = %v, want unauthorized", err)
	}
	if code, stdout, _ := runCLI("server", "devices"); code != 0 || stdout != "No paired devices. Pair one with `kit server pair`.\n" {
		t.Fatalf("devices after revoke: exit %d, %q", code, stdout)
	}
}

func TestPairingNeedsARunningServer(t *testing.T) {
	t.Setenv(apphome.EnvHome, filepath.Join(t.TempDir(), "kit"))
	code, _, stderr := runCLI("server", "pair")
	if code != 1 || stderr != "kit: server is not running; start it with `kit server start`\n" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if code, _, stderr := runCLI("server", "pair", "--url", "kit.example.com"); code != 2 || !strings.Contains(stderr, "must be an HTTP(S) origin") {
		t.Fatalf("invalid --url: exit %d, stderr %q", code, stderr)
	}
}

func TestQRCodesEncodeTheLink(t *testing.T) {
	link := pairing.Link("https://kit.example.ts.net", strings.Repeat("a", 26))
	rendered, err := renderQR(link)
	if err != nil {
		t.Fatal(err)
	}
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		t.Fatal(err)
	}
	// Each line draws two module rows light on dark, inside a four-module
	// quiet zone; reading the rows back must give the encoder's modules.
	size := code.Size + 8
	lines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
	if len(lines) != (size+1)/2 {
		t.Fatalf("QR lines = %d, want %d", len(lines), (size+1)/2)
	}
	for row, line := range lines {
		body, ok := strings.CutPrefix(line, "\x1b[97;40m")
		body, ok2 := strings.CutSuffix(body, "\x1b[0m")
		cells := []rune(body)
		if !ok || !ok2 || len(cells) != size {
			t.Fatalf("line %d = %q", row, line)
		}
		for x, cell := range cells {
			for half, y := range []int{2 * row, 2*row + 1} {
				if y >= size {
					continue
				}
				light := strings.ContainsRune([]string{"█▀", "█▄"}[half], cell)
				mx, my := x-4, y-4
				want := mx < 0 || my < 0 || mx >= code.Size || my >= code.Size || !code.Black(mx, my)
				if light != want {
					t.Fatalf("module (%d, %d) light = %v, want %v", x, y, light, want)
				}
			}
		}
	}
}

func TestPlainHTTPLinksToOtherMachinesAreFlagged(t *testing.T) {
	for served, secure := range map[string]bool{
		"https://kit.example.ts.net": true,
		"http://127.0.0.1:7777":      true,
		"http://[::1]:7777":          true,
		"http://localhost:7777":      true,
		"http://box.local:8080":      false,
		"http://192.168.1.10:7777":   false,
	} {
		if securePairingURL(served) != secure {
			t.Errorf("securePairingURL(%q) = %v, want %v", served, !secure, secure)
		}
	}
}
