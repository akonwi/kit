package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/localdiscovery"
	"github.com/akonwi/kit/internal/savedservers"
)

// pairingLink asks the server of home for a pairing link at its loopback URL.
func pairingLink(t *testing.T, home string) string {
	t.Helper()
	client, err := kit.Connect(t.Context(), kit.Home(home))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	code, err := client.CreatePairing(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := localdiscovery.Load(apphome.FromHome(home))
	if err != nil {
		t.Fatal(err)
	}
	return code.Link(registry.URL)
}

func TestURLServersPairAttachAndRecoverFromRevocation(t *testing.T) {
	serverHome := filepath.Join(t.TempDir(), "server")
	if err := os.MkdirAll(serverHome, 0o700); err != nil {
		t.Fatal(err)
	}
	startDaemon(t, serverHome)
	clientHome := isolatedHome(t)
	link := pairingLink(t, serverHome)
	serverURL := link[:strings.Index(link, "/pair#")]

	// Saving needs a pairing link; a plain URL explains how to get one.
	if code, _, stderr := runServers(t, "add", "vps", serverURL); code != 2 || !strings.Contains(stderr, "needs pairing") {
		t.Fatalf("add URL: exit %d, stderr %q", code, stderr)
	}
	code, stdout, stderr := runServers(t, "add", "vps", link)
	if code != 0 || stdout != "paired and saved server \"vps\": "+serverURL+"\n" {
		t.Fatalf("add link: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	info, err := os.Stat(filepath.Join(clientHome, "server-tokens.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file = %v, %v", info, err)
	}
	if saved, _ := os.ReadFile(filepath.Join(clientHome, "servers.json")); strings.Contains(string(saved), "kit_device_") {
		t.Fatal("servers.json holds a device token")
	}
	if code, _, stderr := runServers(t, "add", "again", pairingLink(t, serverHome)); code != 1 || !strings.Contains(stderr, `already saved as "vps"`) {
		t.Fatalf("add a saved URL again: exit %d, stderr %q", code, stderr)
	}

	_, stdout, _ = runServers(t)
	if !strings.Contains(stdout, "vps    "+serverURL) || !strings.Contains(strings.Split(stdout, "\n")[2], "running") {
		t.Fatalf("servers list:\n%s", stdout)
	}

	// Attach by name and by URL; the server shares this machine, so the
	// shell directory applies, and it has no providers, so attaching stops
	// before the TUI with how to log in.
	var out, errs bytes.Buffer
	for _, argument := range []string{"vps", serverURL + "/"} {
		invocation := Parse(context.Background(), []string{"attach", argument, "--cwd", t.TempDir()}, &out, &errs)
		if invocation.Kind != InvocationInteractive || invocation.options.Server != (attachTarget{Name: "vps", URL: serverURL}) {
			t.Fatalf("attach %s = %+v, stderr %q", argument, invocation, errs.String())
		}
	}
	invocation := Parse(context.Background(), []string{"attach", "vps", "--cwd", t.TempDir()}, &out, &errs)
	environment := OpenEnvironment(context.Background(), invocation, &errs)
	if environment.ExitCode != 0 {
		t.Fatalf("open environment: %s", errs.String())
	}
	errs.Reset()
	// The server has no providers, so the terminal client starts by logging
	// it in; the logins run on the server.
	prepared := environment.Prepare(context.Background(), invocation, &errs)
	environment.Close()
	if prepared.ExitCode != 0 || prepared.Options.Authenticated {
		t.Fatalf("prepare: exit %d, authenticated %v, stderr %q", prepared.ExitCode, prepared.Options.Authenticated, errs.String())
	}
	if _, ok := prepared.Options.Login.(serverDeviceLogin); !ok {
		t.Fatalf("device login = %T", prepared.Options.Login)
	}
	if _, ok := prepared.Options.Logout.(serverLogout); !ok {
		t.Fatalf("logout = %T", prepared.Options.Logout)
	}

	// kit pair from this device prints a link at the saved URL.
	code, stdout, stderr = runCLI("pair", "vps")
	if code != 0 || !strings.Contains(stdout, "  "+serverURL+"/pair#") {
		t.Fatalf("pair vps: exit %d\n%s%s", code, stdout, stderr)
	}

	// Once revoked, the server needs pairing; attaching through a new link
	// pairs again and works.
	if code, _, stderr := runCLIInHome(t, serverHome, "server", "devices", "revoke", deviceName()); code != 0 {
		t.Fatalf("revoke: %s", stderr)
	}
	_, stdout, _ = runServers(t)
	if !strings.Contains(stdout, "needs pairing") {
		t.Fatalf("servers list after revoke:\n%s", stdout)
	}
	errs.Reset()
	environment = OpenEnvironment(context.Background(), invocation, &errs)
	environment.Close()
	if environment.ExitCode != 1 || !strings.Contains(errs.String(), "no longer accepts this device's token; get a new link with `kit server pair`") {
		t.Fatalf("attach revoked: exit %d, stderr %q", environment.ExitCode, errs.String())
	}
	errs.Reset()
	repaired := Parse(context.Background(), []string{"attach", pairingLink(t, serverHome), "--cwd", t.TempDir()}, &out, &errs)
	if repaired.Kind != InvocationInteractive || repaired.options.Server.Name != "vps" {
		t.Fatalf("attach link = %+v, stderr %q", repaired, errs.String())
	}
	environment = OpenEnvironment(context.Background(), repaired, &errs)
	environment.Close()
	if environment.ExitCode != 0 {
		t.Fatalf("attach after re-pairing: %s", errs.String())
	}

	if code, stdout, _ := runServers(t, "remove", "vps"); code != 0 || stdout != "removed server \"vps\"\n" {
		t.Fatalf("remove: exit %d, %q", code, stdout)
	}
	tokens, _ := savedservers.NewTokens(filepath.Join(clientHome, "server-tokens.json"))
	if _, err := tokens.Get("vps", serverURL); err == nil {
		t.Fatal("the removed server's token was kept")
	}
}

func TestAFailedPairingWarnsAboutPlainHTTPAndSavesNothing(t *testing.T) {
	serverHome := isolatedHome(t)
	startDaemon(t, serverHome)
	clientHome := isolatedHome(t)
	link := pairingLink(t, serverHome)
	// The same code at a plain-HTTP address on another machine, which does
	// not resolve.
	remote := "http://kit.example.invalid" + link[strings.Index(link, "/pair#"):]
	code, _, stderr := runServers(t, "add", "vps", remote)
	warning := "kit: warning: http://kit.example.invalid is plain HTTP; the pairing code and device token cross the network\n" +
		"unencrypted unless the network itself is private, such as a tailnet\n"
	if code != 1 || !strings.HasPrefix(stderr, warning+"kit: pair with vps: ") {
		t.Fatalf("add: exit %d, stderr %q", code, stderr)
	}
	if saved, err := os.ReadFile(filepath.Join(clientHome, "servers.json")); err == nil && strings.Contains(string(saved), "vps") {
		t.Fatalf("servers.json after a failed pairing:\n%s", saved)
	}
}

func TestAttachingAnUnsavedURLOrLinkExplainsHowToSaveIt(t *testing.T) {
	isolatedHome(t)
	var out, errs bytes.Buffer
	for argument, want := range map[string]string{
		"https://kit.example.ts.net":                                 "kit: no saved server is at https://kit.example.ts.net; pair with a link from `kit server pair` on that server and save it with `kit servers add NAME LINK`\n",
		"https://kit.example.ts.net/pair#" + strings.Repeat("a", 26): "kit: no saved server is at https://kit.example.ts.net; save it under a name with `kit servers add NAME LINK`\n",
	} {
		errs.Reset()
		invocation := Parse(context.Background(), []string{"attach", argument}, &out, &errs)
		if invocation.Kind != InvocationExit || invocation.ExitCode != 2 || errs.String() != want {
			t.Errorf("attach %s: %+v, stderr %q", argument, invocation, errs.String())
		}
	}
}

func TestSessionsOnAnotherMachineStartInServerPaths(t *testing.T) {
	remote := kit.ServerInfo{Home: "/home/me"}
	for requested, want := range map[string]string{
		"":            "/home/me",
		"/srv/app":    "/srv/app",
		"~":           "/home/me",
		"~/src/app/.": "/home/me/src/app",
	} {
		if got, err := serverCWD(remote, "vps", requested); err != nil || got != want {
			t.Errorf("serverCWD(%q) = %q, %v; want %q", requested, got, err, want)
		}
	}
	for requested, want := range map[string]string{
		"src/app":  `--cwd "src/app" must be an absolute path on server "vps", or start with ~/`,
		"~other/x": `--cwd "~other/x" must be an absolute path on server "vps", or start with ~/`,
	} {
		if _, err := serverCWD(remote, "vps", requested); err == nil || err.Error() != want {
			t.Errorf("serverCWD(%q) error = %v; want %q", requested, err, want)
		}
	}
	if _, err := serverCWD(kit.ServerInfo{}, "vps", ""); err == nil || err.Error() != `server "vps" did not report its home directory; pass --cwd with an absolute path on that machine` {
		t.Errorf("unknown home: %v", err)
	}
	directory := t.TempDir()
	if got, err := serverCWD(kit.ServerInfo{SameMachine: true}, "vps", directory); err != nil || got != directory {
		t.Errorf("same machine: %q, %v", got, err)
	}
}

// runCLIInHome runs a command with KIT_HOME set to home, restoring it after.
func runCLIInHome(t *testing.T, home string, args ...string) (int, string, string) {
	t.Helper()
	previous := os.Getenv(apphome.EnvHome)
	os.Setenv(apphome.EnvHome, home)
	defer os.Setenv(apphome.EnvHome, previous)
	return runCLI(args...)
}

func TestTheTUIPairsDevicesWithLinksAtEachURL(t *testing.T) {
	serverHome := filepath.Join(t.TempDir(), "server")
	if err := os.MkdirAll(serverHome, 0o700); err != nil {
		t.Fatal(err)
	}
	startDaemon(t, serverHome)
	client, err := kit.Connect(t.Context(), kit.Home(serverHome))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	urls := []string{"https://kit.example.ts.net", "http://127.0.0.1:7781"}
	pairing, err := CreatePairingLinks(t.Context(), client, urls)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairing.Links) != 2 || len(pairing.Expires) != len("15:04") {
		t.Fatalf("pairing = %+v", pairing)
	}
	code := pairing.Links[0][strings.Index(pairing.Links[0], "#"):]
	for index, link := range pairing.Links {
		if link != urls[index]+"/pair"+code {
			t.Errorf("link %d = %q", index, link)
		}
	}
	if _, err := CreatePairingLinks(t.Context(), client, nil); err == nil {
		t.Fatal("pairing without a URL succeeded")
	}
}

func TestServerPathsAreAbbreviatedRelativeToTheServerHome(t *testing.T) {
	for _, test := range []struct{ home, path, want string }{
		{home: "/home/me", path: "/home/me", want: "~"},
		{home: "/home/me", path: "/home/me/src/app", want: "~/src/app"},
		{home: "/home/me/", path: "/home/me/src/", want: "~/src"},
		{home: "/home/me", path: "/home/meow/src", want: "/home/meow/src"},
		{home: "/home/me", path: "/srv/app", want: "/srv/app"},
		{home: "/", path: "/srv/app", want: "/srv/app"},
		{home: "", path: "/Users/me/src", want: "/Users/me/src"},
	} {
		if got := homeRelative(test.home, test.path); got != test.want {
			t.Errorf("homeRelative(%q, %q) = %q, want %q", test.home, test.path, got, test.want)
		}
	}
}

func TestRepairingRevokesTheReplacedDevice(t *testing.T) {
	serverHome := isolatedHome(t)
	startDaemon(t, serverHome)
	isolatedHome(t)
	if code, _, stderr := runServers(t, "add", "vps", pairingLink(t, serverHome)); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	var out, errs bytes.Buffer
	repaired := Parse(context.Background(), []string{"attach", pairingLink(t, serverHome), "--cwd", t.TempDir()}, &out, &errs)
	if repaired.Kind != InvocationInteractive {
		t.Fatalf("attach link = %+v, stderr %q", repaired, errs.String())
	}

	server, err := kit.Connect(t.Context(), kit.Home(serverHome))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	listed, err := server.Devices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("devices after re-pairing = %+v, want only the new one", listed)
	}
	// The remaining device is the one whose token is saved.
	environment := OpenEnvironment(context.Background(), repaired, &errs)
	environment.Close()
	if environment.ExitCode != 0 {
		t.Fatalf("attach after re-pairing: %s", errs.String())
	}
}
