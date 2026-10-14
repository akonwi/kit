package cli

import (
	"bytes"
	"context"
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
	calls  []string
}

func (f *fakeTailnet) Status(context.Context) (tailnetStatus, error) { return f.status, nil }

func (f *fakeTailnet) Served(_ context.Context, host string, port int) (string, error) {
	if host != f.status.DNSName {
		return "", nil
	}
	return f.served[port], nil
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
	t        *testing.T
	home     string
	tailnet  *fakeTailnet
	ensured  []string
	restarts int
	started  bool
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
	return &exposeHarness{t: t, home: home, tailnet: &fakeTailnet{
		status: tailnetStatus{Running: true, DNSName: tailnetName, HTTPSCerts: true},
		served: map[int]string{},
	}}
}

func (h *exposeHarness) run(off bool) (int, string, string) {
	options := exposeOptions{
		tailnet: func() (tailnet, error) { return h.tailnet, nil },
		ensure: func(_ context.Context, _ apphome.Paths, address serverlisten.Address, changed bool) error {
			h.ensured = append(h.ensured, address.String()+map[bool]string{true: " restart", false: ""}[changed])
			if !h.started {
				h.started = true
				startDaemon(h.t, h.home)
			}
			return nil
		},
		restart:   func(context.Context, apphome.Paths) error { h.restarts++; return nil },
		httpsPort: 443,
	}
	var stdout, stderr bytes.Buffer
	code := runExpose(context.Background(), options, off, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func (h *exposeHarness) settings() string {
	data, err := os.ReadFile(filepath.Join(h.home, "settings.json"))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

var pairingCode = regexp.MustCompile(`#[A-Za-z0-9_-]+`)

func TestExposeTailscaleServesAFixedPortAndUndoesItsSettings(t *testing.T) {
	h := newExposeHarness(t, `{"theme":"dusk","server":{"listen":"127.0.0.1:7777"}}`)
	code, stdout, stderr := h.run(false)
	if code != 0 {
		t.Fatalf("expose code = %d, stderr = %q", code, stderr)
	}
	stdout = pairingCode.ReplaceAllString(stdout, "#CODE")
	stdout = regexp.MustCompile(`until \d\d:\d\d`).ReplaceAllString(stdout, "until HH:MM")
	want := "Serving on your tailnet at https://studio.tail3f2a.ts.net\n" +
		"Only devices on your tailnet can reach it. Stop with `kit server expose --tailscale --off`.\n\n" +
		"Pair a device with one of these links. They share one code, which works once, until HH:MM.\n\n" +
		"  https://studio.tail3f2a.ts.net/pair#CODE\n"
	if stdout != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", stdout, want)
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
	if code, _, stderr := h.run(false); code != 0 {
		t.Fatalf("second expose code = %d, stderr = %q", code, stderr)
	}
	if strings.Join(h.ensured, ",") != "127.0.0.1:7777 restart,127.0.0.1:7777" {
		t.Fatalf("ensured = %v", h.ensured)
	}

	code, stdout, stderr = h.run(true)
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
	if code, stdout, _ := h.run(true); code != 0 || stdout != "This server is not exposed with --tailscale.\n" {
		t.Fatalf("second off = %d, %q", code, stdout)
	}
}

func TestExposeTailscaleFixesAnEphemeralListener(t *testing.T) {
	h := newExposeHarness(t, "")
	if code, _, stderr := h.run(false); code != 0 {
		t.Fatalf("expose code = %d, stderr = %q", code, stderr)
	}
	record, found, err := loadExposure(apphome.FromHome(h.home))
	if err != nil || !found {
		t.Fatalf("exposure = %+v, %v, %v", record, found, err)
	}
	if !strings.HasPrefix(record.AddedListen, "127.0.0.1:") || record.Target != "http://"+record.AddedListen || !record.AddedHost {
		t.Fatalf("record = %+v", record)
	}
	if !strings.Contains(h.settings(), `"listen": "`+record.AddedListen+`"`) {
		t.Fatalf("settings =\n%s", h.settings())
	}
	if code, _, stderr := h.run(true); code != 0 {
		t.Fatalf("off code = %d, stderr = %q", code, stderr)
	}
	if got := h.settings(); got != "{}\n" {
		t.Fatalf("settings after off = %q", got)
	}
}

func TestExposeTailscaleRefusesWithoutAUsableTailnet(t *testing.T) {
	cases := []struct {
		name   string
		status tailnetStatus
		served map[int]string
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
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			h := newExposeHarness(t, `{"server":{"listen":"127.0.0.1:7777"}}`)
			h.tailnet.status = test.status
			if test.served != nil {
				h.tailnet.served = test.served
			}
			code, _, stderr := h.run(false)
			if code != 1 || stderr != test.want {
				t.Fatalf("code = %d, stderr = %q, want %q", code, stderr, test.want)
			}
			if got := h.settings(); got != `{"server":{"listen":"127.0.0.1:7777"}}` {
				t.Fatalf("settings changed: %s", got)
			}
		})
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
