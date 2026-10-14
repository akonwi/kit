package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/serverlisten"
)

// fakeTailnet records Serve calls against an in-memory serve config.
type fakeTailnet struct {
	status tailnetStatus
	served map[int]string
	funnel map[int]bool
	calls  []string
}

func (f *fakeTailnet) Status(context.Context) (tailnetStatus, error) { return f.status, nil }

func (f *fakeTailnet) Served(_ context.Context, host string, port int) (servedHandler, error) {
	if host != f.status.DNSName {
		return servedHandler{}, nil
	}
	return servedHandler{Proxy: f.served[port], Funnel: f.funnel[port]}, nil
}

func (f *fakeTailnet) Serve(_ context.Context, port int, target string) error {
	f.calls = append(f.calls, "serve "+target)
	f.served[port] = target
	return nil
}

func (f *fakeTailnet) Unserve(_ context.Context, port int) error {
	f.calls = append(f.calls, "off")
	delete(f.served, port)
	return nil
}

const tailnetName = "studio.tail3f2a.ts.net"

type exposeHarness struct {
	t       *testing.T
	home    string
	tailnet *fakeTailnet
	// missing makes the Tailscale CLI unavailable.
	missing bool
	// foreground makes the running server one started with `kit server run`.
	foreground bool
	httpsPort  int
	ensured    []string
	restarts   int
	started    bool
}

func newExposeHarness(t *testing.T, settings string) *exposeHarness {
	home := filepath.Join(t.TempDir(), "kit")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(apphome.EnvHome, home)
	return &exposeHarness{t: t, home: home, httpsPort: 443, tailnet: &fakeTailnet{
		status: tailnetStatus{Running: true, DNSName: tailnetName, HTTPSCerts: true},
		served: map[int]string{},
		funnel: map[int]bool{},
	}}
}

func (h *exposeHarness) startServer() {
	if !h.started {
		h.started = true
		startDaemon(h.t, h.home)
	}
}

func (h *exposeHarness) run(off, force bool) (int, string, string) {
	options := exposeOptions{
		tailnet: func() (tailnet, error) {
			if h.missing {
				return nil, errors.New("the tailscale command was not found")
			}
			return h.tailnet, nil
		},
		ensure: func(_ context.Context, _ apphome.Paths, address serverlisten.Address, changed bool) error {
			h.ensured = append(h.ensured, address.String()+map[bool]string{true: " restart", false: ""}[changed])
			h.startServer()
			return nil
		},
		restart:    func(context.Context, apphome.Paths) error { h.restarts++; return nil },
		foreground: func(context.Context, apphome.Paths) bool { return h.foreground },
		httpsPort:  h.httpsPort,
		force:      force,
	}
	var stdout, stderr bytes.Buffer
	code := runExpose(context.Background(), options, off, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func (h *exposeHarness) expose() (int, string, string) { return h.run(false, false) }

func (h *exposeHarness) settings() string {
	data, err := os.ReadFile(filepath.Join(h.home, "settings.json"))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

func (h *exposeHarness) record() exposure {
	record, found, err := loadExposure(apphome.FromHome(h.home))
	if err != nil || !found {
		h.t.Fatalf("exposure = %+v, %v, %v", record, found, err)
	}
	return record
}

var pairingCode = regexp.MustCompile(`#[A-Za-z0-9_-]+`)

func normalizePairing(stdout string) string {
	stdout = pairingCode.ReplaceAllString(stdout, "#CODE")
	return regexp.MustCompile(`until \d\d:\d\d`).ReplaceAllString(stdout, "until HH:MM")
}

func TestExposeTailscaleServesAFixedPortAndUndoesItsSettings(t *testing.T) {
	h := newExposeHarness(t, `{"theme":"dusk","server":{"listen":"127.0.0.1:7777"}}`)
	code, stdout, stderr := h.expose()
	if code != 0 {
		t.Fatalf("expose code = %d, stderr = %q", code, stderr)
	}
	want := "Serving on your tailnet at https://studio.tail3f2a.ts.net\n" +
		"Only devices on your tailnet can reach it. Stop with `kit server expose --tailscale --off`.\n\n" +
		"Pair a device with one of these links. They share one code, which works once, until HH:MM.\n\n" +
		"  https://studio.tail3f2a.ts.net/pair#CODE\n"
	if got := normalizePairing(stdout); got != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", got, want)
	}
	if strings.Join(h.tailnet.calls, ",") != "serve http://127.0.0.1:7777" {
		t.Fatalf("tailnet calls = %v", h.tailnet.calls)
	}
	if strings.Join(h.ensured, ",") != "127.0.0.1:7777 restart" {
		t.Fatalf("ensured = %v", h.ensured)
	}
	wantSettings := `{
  "server": {
    "allowedHosts": [
      "studio.tail3f2a.ts.net"
    ],
    "listen": "127.0.0.1:7777"
  },
  "theme": "dusk"
}
`
	if got := h.settings(); got != wantSettings {
		t.Fatalf("settings =\n%s\nwant\n%s", got, wantSettings)
	}

	// Exposing again changes nothing and keeps the server running.
	if code, _, stderr := h.expose(); code != 0 {
		t.Fatalf("second expose code = %d, stderr = %q", code, stderr)
	}
	if strings.Join(h.ensured, ",") != "127.0.0.1:7777 restart,127.0.0.1:7777" {
		t.Fatalf("ensured = %v", h.ensured)
	}

	code, stdout, stderr = h.run(true, false)
	if code != 0 || stdout != "Stopped serving https://studio.tail3f2a.ts.net on your tailnet.\n" {
		t.Fatalf("off = %d, %q, %q", code, stdout, stderr)
	}
	if len(h.tailnet.served) != 0 || h.restarts != 1 {
		t.Fatalf("served = %v, restarts = %d", h.tailnet.served, h.restarts)
	}
	// The listener was the user's, so only the host is removed.
	wantSettings = `{
  "server": {
    "listen": "127.0.0.1:7777"
  },
  "theme": "dusk"
}
`
	if got := h.settings(); got != wantSettings {
		t.Fatalf("settings after off =\n%s\nwant\n%s", got, wantSettings)
	}
	if code, stdout, _ := h.run(true, false); code != 0 || stdout != "This server is not exposed with --tailscale.\n" {
		t.Fatalf("second off = %d, %q", code, stdout)
	}
}

func TestExposeTailscaleFixesAnEphemeralListenerAndRestoresIt(t *testing.T) {
	for _, test := range []struct{ name, settings, host, restored string }{
		{name: "unset", settings: "", host: "127.0.0.1", restored: "{}\n"},
		{name: "loopback ephemeral", settings: `{"server":{"listen":"[::1]:0"}}`, host: "[::1]", restored: "{\n  \"server\": {\n    \"listen\": \"[::1]:0\"\n  }\n}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newExposeHarness(t, test.settings)
			if code, _, stderr := h.expose(); code != 0 {
				t.Fatalf("expose code = %d, stderr = %q", code, stderr)
			}
			record := h.record()
			if !strings.HasPrefix(record.AddedListen, test.host+":") || record.Target != "http://"+record.AddedListen || !record.AddedHost {
				t.Fatalf("record = %+v", record)
			}
			if !strings.Contains(h.settings(), `"listen": "`+record.AddedListen+`"`) {
				t.Fatalf("settings =\n%s", h.settings())
			}
			if code, _, stderr := h.run(true, false); code != 0 {
				t.Fatalf("off code = %d, stderr = %q", code, stderr)
			}
			if got := h.settings(); got != test.restored {
				t.Fatalf("settings after off = %q, want %q", got, test.restored)
			}
		})
	}
}

func TestExposeTailscaleWarnsWhenTheServerListensOnEveryInterface(t *testing.T) {
	h := newExposeHarness(t, `{"server":{"listen":"0.0.0.0:7777"}}`)
	code, stdout, stderr := h.expose()
	if code != 0 {
		t.Fatalf("expose code = %d, stderr = %q", code, stderr)
	}
	want := "Serving on your tailnet at https://studio.tail3f2a.ts.net\n" +
		"The server also listens on every interface (server.listen 0.0.0.0:7777), so devices that\n" +
		"can reach this machine directly can connect too, with Kit's credentials as the\n" +
		"only barrier. Stop with `kit server expose --tailscale --off`.\n\n"
	if got := normalizePairing(stdout); !strings.HasPrefix(got, want) {
		t.Fatalf("stdout =\n%s\nwant prefix\n%s", got, want)
	}
	if strings.Join(h.tailnet.calls, ",") != "serve http://127.0.0.1:7777" {
		t.Fatalf("tailnet calls = %v", h.tailnet.calls)
	}
}

func TestExposeTailscaleLeavesAForegroundServerToItsSupervisor(t *testing.T) {
	h := newExposeHarness(t, `{"server":{"listen":"127.0.0.1:7777"}}`)
	h.foreground = true
	code, stdout, stderr := h.expose()
	if code != 0 {
		t.Fatalf("expose code = %d, stderr = %q", code, stderr)
	}
	want := "Serving on your tailnet at https://studio.tail3f2a.ts.net\n" +
		"Only devices on your tailnet can reach it. Stop with `kit server expose --tailscale --off`.\n\n" +
		"The server runs in the foreground with `kit server run`. Restart it with whatever\n" +
		"started it, such as its service manager, to apply the new settings, then pair a\n" +
		"device with `kit server pair`.\n"
	if stdout != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if len(h.ensured) != 0 || strings.Join(h.tailnet.calls, ",") != "serve http://127.0.0.1:7777" {
		t.Fatalf("ensured = %v, calls = %v", h.ensured, h.tailnet.calls)
	}

	code, stdout, _ = h.run(true, false)
	want = "Stopped serving https://studio.tail3f2a.ts.net on your tailnet.\n" +
		"The server runs in the foreground with `kit server run`. Restart it with whatever\n" +
		"started it to apply the restored settings.\n"
	if code != 0 || stdout != want || h.restarts != 0 {
		t.Fatalf("off = %d, %q, restarts = %d", code, stdout, h.restarts)
	}
}

func TestExposeTailscaleOffKeepsSettingsUntilTheHandlerIsGone(t *testing.T) {
	h := newExposeHarness(t, `{"server":{"listen":"127.0.0.1:7777"}}`)
	if code, _, stderr := h.expose(); code != 0 {
		t.Fatalf("expose code = %d, stderr = %q", code, stderr)
	}
	exposed := h.settings()
	h.missing = true
	code, stdout, stderr := h.run(true, false)
	want := "kit: the tailscale command was not found\n" +
		"Could not confirm that https://studio.tail3f2a.ts.net no longer proxies to http://127.0.0.1:7777. Fix Tailscale and try again,\n" +
		"or remove the handler with `tailscale serve --https=443 off` and run\n" +
		"`kit server expose --tailscale --off --force`.\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Fatalf("off = %d, %q, %q", code, stdout, stderr)
	}
	if h.settings() != exposed {
		t.Fatalf("settings changed: %s", h.settings())
	}
	h.record()

	code, stdout, stderr = h.run(true, true)
	if code != 0 || stdout != "Stopped serving https://studio.tail3f2a.ts.net on your tailnet.\n" ||
		stderr != "kit: warning: the tailscale command was not found; remove the handler with `tailscale serve --https=443 off`\n" {
		t.Fatalf("forced off = %d, %q, %q", code, stdout, stderr)
	}
	if got := h.settings(); got != "{\n  \"server\": {\n    \"listen\": \"127.0.0.1:7777\"\n  }\n}\n" {
		t.Fatalf("settings after forced off = %q", got)
	}
}

func TestExposeTailscaleRefusesWithoutAUsableTailnet(t *testing.T) {
	cases := []struct {
		name   string
		status tailnetStatus
		served map[int]string
		funnel map[int]bool
		want   string
	}{
		{name: "stopped", status: tailnetStatus{}, want: "kit: Tailscale is not running on this machine; start it and log in, then try again\n"},
		{
			name: "no certificates", status: tailnetStatus{Running: true, DNSName: tailnetName},
			want: "kit: your tailnet does not issue HTTPS certificates for studio.tail3f2a.ts.net; enable HTTPS in the Tailscale admin console's DNS settings, then try again\n",
		},
		{
			name: "port in use", status: tailnetStatus{Running: true, DNSName: tailnetName, HTTPSCerts: true},
			served: map[int]string{443: "http://127.0.0.1:3000"},
			want:   "kit: https://studio.tail3f2a.ts.net already serves http://127.0.0.1:3000; choose another port with --https-port\n",
		},
		{
			name: "funnel", status: tailnetStatus{Running: true, DNSName: tailnetName, HTTPSCerts: true},
			funnel: map[int]bool{443: true},
			want:   "kit: Tailscale Funnel is on for https://studio.tail3f2a.ts.net, which would put this server on the public internet; turn it off with `tailscale funnel --https=443 off`, or choose another port with --https-port\n",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			h := newExposeHarness(t, `{"server":{"listen":"127.0.0.1:7777"}}`)
			h.tailnet.status = test.status
			if test.served != nil {
				h.tailnet.served = test.served
			}
			if test.funnel != nil {
				h.tailnet.funnel = test.funnel
			}
			code, _, stderr := h.expose()
			if code != 1 || stderr != test.want {
				t.Fatalf("code = %d, stderr = %q, want %q", code, stderr, test.want)
			}
			if got := h.settings(); got != `{"server":{"listen":"127.0.0.1:7777"}}` {
				t.Fatalf("settings changed: %s", got)
			}
		})
	}
}

func TestExposeTailscaleRefusesASecondExposure(t *testing.T) {
	h := newExposeHarness(t, `{"server":{"listen":"127.0.0.1:7777"}}`)
	if code, _, stderr := h.expose(); code != 0 {
		t.Fatalf("expose code = %d, stderr = %q", code, stderr)
	}
	h.httpsPort = 8443
	code, _, stderr := h.expose()
	if code != 1 || stderr != "kit: this server is already exposed at https://studio.tail3f2a.ts.net; run `kit server expose --tailscale --off` first\n" {
		t.Fatalf("second expose = %d, %q", code, stderr)
	}
	if strings.Join(h.tailnet.calls, ",") != "serve http://127.0.0.1:7777" {
		t.Fatalf("tailnet calls = %v", h.tailnet.calls)
	}
}

func TestServedURLOmitsTheDefaultHTTPSPort(t *testing.T) {
	if got := servedURL(tailnetName, 443); got != "https://studio.tail3f2a.ts.net" {
		t.Errorf("servedURL(443) = %q", got)
	}
	if got := servedURL(tailnetName, 8443); got != "https://studio.tail3f2a.ts.net:8443" {
		t.Errorf("servedURL(8443) = %q", got)
	}
}
