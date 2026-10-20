package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/savedservers"
)

// isolatedHome points KIT_HOME at a new directory and returns it.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "client")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIT_HOME", home)
	return home
}

func runServers(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeCommand(context.Background(), append([]string{"servers"}, args...), &stdout, &stderr, defaultCommandDependencies())
	return code, stdout.String(), stderr.String()
}

func TestServersAddListAndRemove(t *testing.T) {
	isolatedHome(t)
	serverHome := t.TempDir()

	if code, stdout, _ := runServers(t); code != 0 || stdout != "No saved servers. Add one with `kit servers add NAME LOCATION`.\n" {
		t.Fatalf("empty list: exit = %d, stdout = %q", code, stdout)
	}
	code, stdout, stderr := runServers(t, "add", "dev", serverHome)
	if code != 0 || stdout != "saved server \"dev\": "+serverHome+"\n" {
		t.Fatalf("add: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, stderr = runServers(t, "list")
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if code != 0 || len(lines) != 3 {
		t.Fatalf("list: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if fields := strings.Fields(lines[0]); strings.Join(fields, " ") != "NAME LOCATION STATUS" {
		t.Fatalf("header = %q", lines[0])
	}
	if fields := strings.Fields(lines[1]); fields[0] != "local" || strings.Join(fields[2:], " ") != "not running" {
		t.Fatalf("local row = %q", lines[1])
	}
	if fields := strings.Fields(lines[2]); fields[0] != "dev" || strings.Join(fields[2:], " ") != "not running" {
		t.Fatalf("dev row = %q", lines[2])
	}
	if code, stdout, _ := runServers(t, "rm", "dev"); code != 0 || stdout != "removed server \"dev\"\n" {
		t.Fatalf("remove: exit = %d, stdout = %q", code, stdout)
	}
}

func TestServersAddRejectsInvalidInput(t *testing.T) {
	isolatedHome(t)
	serverHome := t.TempDir()
	for _, test := range []struct {
		args   []string
		code   int
		stderr string
	}{
		{args: []string{"add", "local", serverHome}, code: 2, stderr: `server name "local" is reserved`},
		{args: []string{"add", "My Server", serverHome}, code: 2, stderr: "must be lowercase letters"},
		{args: []string{"add", "dev", filepath.Join(serverHome, "missing")}, code: 1, stderr: "no such file or directory"},
		{args: []string{"remove", "dev"}, code: 1, stderr: "server name is not saved"},
		{args: []string{"add", "dev"}, code: 2, stderr: "accepts 2 arg(s)"},
	} {
		code, _, stderr := runServers(t, test.args...)
		if code != test.code || !strings.Contains(stderr, test.stderr) {
			t.Errorf("servers %q: exit = %d, stderr = %q; want %d containing %q", test.args, code, stderr, test.code, test.stderr)
		}
	}
	if code, _, stderr := runServers(t, "add", "dev", serverHome); code != 0 {
		t.Fatalf("add: %s", stderr)
	}
	if code, _, stderr := runServers(t, "add", "dev", serverHome); code != 1 || !strings.Contains(stderr, "already saved") {
		t.Fatalf("duplicate add: exit = %d, stderr = %q", code, stderr)
	}
}

func TestAttachResolvesServers(t *testing.T) {
	clientHome := isolatedHome(t)
	serverHome := filepath.Join(t.TempDir(), "kit-dev")
	if err := os.MkdirAll(serverHome, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := savedservers.NewStore(filepath.Join(clientHome, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(savedservers.Server{Name: "dev", Kind: savedservers.KindHome, Home: serverHome}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	for _, test := range []struct {
		args []string
		want attachTarget
	}{
		{args: []string{"attach", "dev"}, want: attachTarget{Name: "dev", Home: serverHome}},
		{args: []string{"attach", serverHome}, want: attachTarget{Name: "kit-dev", Home: serverHome}},
		{args: []string{"attach", "local"}, want: attachTarget{}},
		{args: []string{"attach", "dev", "--cwd", "/tmp"}, want: attachTarget{Name: "dev", Home: serverHome}},
		// An unsaved user@host or ssh:// URL is an SSH destination.
		{args: []string{"attach", "me@devbox"}, want: attachTarget{Name: "me@devbox", SSH: "me@devbox"}},
		{args: []string{"attach", "ssh://me@devbox:2222"}, want: attachTarget{Name: "ssh://me@devbox:2222", SSH: "ssh://me@devbox:2222"}},
	} {
		invocation := Parse(context.Background(), test.args, &stdout, &stderr)
		if invocation.Kind != InvocationInteractive || invocation.options.Server != test.want {
			t.Fatalf("Parse(%q) = %s %+v, want interactive %+v; stderr = %q", test.args, invocation.Kind, invocation.options.Server, test.want, stderr.String())
		}
	}

	picking := Parse(context.Background(), []string{"attach", "--temp"}, &stdout, &stderr)
	if picking.Kind != InvocationServers || len(picking.Servers()) != 2 {
		t.Fatalf("attach without a server = %s %+v", picking.Kind, picking.Servers())
	}
	if local, dev := picking.Servers()[0], picking.Servers()[1]; local.Name != "local" || local.Status != "not running" || dev.Name != "dev" || dev.Status != "not running" {
		t.Fatalf("choices = %+v", picking.Servers())
	}
	picked := Attach(context.Background(), picking, "dev", &stderr)
	if picked.Kind != InvocationInteractive || !picked.options.Temporary || picked.options.Server != (attachTarget{Name: "dev", Home: serverHome}) {
		t.Fatalf("picked = %+v", picked)
	}

	stderr.Reset()
	// A bare name must be saved, so a typo never connects anywhere.
	unknown := Parse(context.Background(), []string{"attach", "nope"}, &stdout, &stderr)
	if unknown.Kind != InvocationExit || unknown.ExitCode != 2 || stderr.String() != "kit: unknown server \"nope\"; list saved servers with `kit servers`, or attach over SSH with USER@HOST or ssh://HOST\n" {
		t.Fatalf("unknown server = %+v, stderr = %q", unknown, stderr.String())
	}
}

func TestAttachWithoutSavedServersExplainsHowToAddOne(t *testing.T) {
	isolatedHome(t)
	var stdout, stderr bytes.Buffer
	invocation := Parse(context.Background(), []string{"attach"}, &stdout, &stderr)
	if invocation.Kind != InvocationExit || invocation.ExitCode != 2 || stderr.String() != "kit: no saved servers; add one with `kit servers add NAME LOCATION`\n" {
		t.Fatalf("attach = %+v, stderr = %q", invocation, stderr.String())
	}
}

func TestAttachingToAStoppedServerNeverStartsIt(t *testing.T) {
	isolatedHome(t)
	serverHome := t.TempDir()
	var stdout, stderr bytes.Buffer
	invocation := Parse(context.Background(), []string{"attach", serverHome}, &stdout, &stderr)
	environment := OpenEnvironment(context.Background(), invocation, &stderr)
	defer environment.Close()
	want := "kit: server \"" + filepath.Base(serverHome) + "\" is not running; start it with `KIT_HOME=" + serverHome + " kit server start`\n"
	if environment.ExitCode != 1 || stderr.String() != want {
		t.Fatalf("exit = %d, stderr = %q, want %q", environment.ExitCode, stderr.String(), want)
	}
	if entries, err := os.ReadDir(serverHome); err != nil || len(entries) != 0 {
		t.Fatalf("server home entries = %v, %v; want it untouched", entries, err)
	}
}

func TestServersAddSavesAnSSHDestination(t *testing.T) {
	home := isolatedHome(t)
	for _, test := range []struct{ name, location string }{{"devbox", "me@devbox"}, {"alias", "devbox"}, {"port", "ssh://me@devbox:2222"}} {
		code, stdout, stderr := runServers(t, "add", test.name, test.location)
		if code != 0 || stdout != "saved server \""+test.name+"\": "+test.location+" over SSH\n" {
			t.Fatalf("add %s: exit = %d, stdout = %q, stderr = %q", test.location, code, stdout, stderr)
		}
	}
	store, err := savedservers.NewStore(filepath.Join(home, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if server, err := store.Find("port"); err != nil || server != (savedservers.Server{Name: "port", Kind: savedservers.KindSSH, Destination: "ssh://me@devbox:2222"}) {
		t.Fatalf("saved = %#v, %v", server, err)
	}
	if code, _, stderr := runServers(t, "add", "bad", "-oProxyCommand=x"); code == 0 {
		t.Fatalf("saved an option as a destination; stderr = %q", stderr)
	}
}

func TestPairingThroughAnSSHServerNeedsAURL(t *testing.T) {
	isolatedHome(t)
	var stdout, stderr bytes.Buffer
	code := executeCommand(context.Background(), []string{"pair", "me@devbox"}, &stdout, &stderr, defaultCommandDependencies())
	if code != 1 || stderr.String() != "kit: server \"me@devbox\" is reached over SSH, so Kit does not know a URL that reaches it; pass --url, or run `kit server pair` on the host\n" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
}
