package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/selfupdate"
)

// fakeUpdate records what `kit update` did with replaced effects.
type fakeUpdate struct {
	latest     selfupdate.Release
	checkErr   error
	installed  bool
	installErr error
	server     localServer
	running    bool
	restarted  bool
}

func (f *fakeUpdate) updater(current, stdin string, interactive bool) updater {
	return updater{
		current:     current,
		stdin:       strings.NewReader(stdin),
		interactive: interactive,
		locate: func() (selfupdate.Installation, error) {
			return selfupdate.Installation{Method: selfupdate.MethodHomebrew, Executable: "/brew/Cellar/kit/0.48.0/bin/kit", Launcher: "/brew/bin/kit", Brew: "/brew/bin/brew"}, nil
		},
		newer: func(_ context.Context, current selfupdate.Version) (selfupdate.Release, bool, error) {
			if f.checkErr != nil {
				return selfupdate.Release{}, false, f.checkErr
			}
			return f.latest, f.latest.Version.Compare(current) > 0, nil
		},
		install: func(_ context.Context, installation selfupdate.Installation, release selfupdate.Release, output io.Writer, _ selfupdate.Progress) error {
			if installation.Launcher != "/brew/bin/kit" || release.Tag != f.latest.Tag {
				return errors.New("unexpected installation")
			}
			f.installed = f.installErr == nil
			return f.installErr
		},
		server: func(context.Context) (localServer, bool) { return f.server, f.running },
		restart: func(_ context.Context, launcher string, stdout, _ io.Writer) error {
			f.restarted = launcher == "/brew/bin/kit"
			io.WriteString(stdout, "server restarted\n")
			return nil
		},
	}
}

var release049 = selfupdate.Release{
	Tag: "v0.49.0", Version: selfupdate.Version{Minor: 49},
	Notes: "# Kit 0.49.0\n\nKit tells you when an update is out.\n\n## What's changed\n",
	URL:   "https://github.com/akonwi/kit/releases/tag/v0.49.0",
}

func TestUpdateInstallsAndOffersToRestartAnOlderLocalServer(t *testing.T) {
	fake := &fakeUpdate{latest: release049, running: true, server: localServer{Version: "0.48.0", Active: 2}}
	var stdout, stderr bytes.Buffer
	code := fake.updater("0.48.0", "\ny\n", true).run(context.Background(), updateOptions{}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	const want = "Kit 0.48.0 → 0.49.0\n\n" +
		"Kit tells you when an update is out.\n\n" +
		"Release notes: https://github.com/akonwi/kit/releases/tag/v0.49.0\n" +
		"Update with `brew upgrade akonwi/tap/kit`? [Y/n] " +
		"Kit 0.49.0 is installed.\n\n" +
		"The local server still runs 0.48.0. Restarting it interrupts 2 sessions that are working or waiting on you.\n" +
		"Restart it now? [y/N] server restarted\n"
	if stdout.String() != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", stdout.String(), want)
	}
	if !fake.installed || !fake.restarted {
		t.Fatalf("installed %v, restarted %v", fake.installed, fake.restarted)
	}
}

func TestUpdateKeepsABusyServerRunningUnlessTheUserAgrees(t *testing.T) {
	fake := &fakeUpdate{latest: release049, running: true, server: localServer{Version: "0.48.0", Active: 1}}
	var stdout, stderr bytes.Buffer
	code := fake.updater("0.48.0", "y\n\n", true).run(context.Background(), updateOptions{}, &stdout, &stderr)
	if code != 0 || fake.restarted {
		t.Fatalf("exit %d, restarted %v", code, fake.restarted)
	}
	const tail = "The local server still runs 0.48.0. Restarting it interrupts 1 session that is working or waiting on you.\n" +
		"Restart it now? [y/N] Restart it later with `kit server restart`.\n"
	if !strings.HasSuffix(stdout.String(), tail) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestUpdateYesRestartsOnlyAnIdleServer(t *testing.T) {
	for _, c := range []struct {
		active  int
		restart bool
		tail    string
	}{
		{0, true, "The local server still runs 0.48.0. No sessions are working, so restarting it interrupts nothing.\nserver restarted\n"},
		{3, false, "The local server still runs 0.48.0. Restarting it interrupts 3 sessions that are working or waiting on you.\nRestart it later with `kit server restart`.\n"},
		{-1, false, "The local server still runs 0.48.0. Restarting it interrupts any session that is working.\nRestart it later with `kit server restart`.\n"},
	} {
		fake := &fakeUpdate{latest: release049, running: true, server: localServer{Version: "0.48.0", Active: c.active}}
		var stdout, stderr bytes.Buffer
		code := fake.updater("0.48.0", "", false).run(context.Background(), updateOptions{Yes: true}, &stdout, &stderr)
		if code != 0 || !fake.installed || fake.restarted != c.restart {
			t.Fatalf("active %d: exit %d, installed %v, restarted %v, stderr %q", c.active, code, fake.installed, fake.restarted, stderr.String())
		}
		if !strings.HasSuffix(stdout.String(), c.tail) {
			t.Fatalf("active %d: stdout = %q", c.active, stdout.String())
		}
	}
}

func TestUpdateWhenUpToDateStillOffersToRestartAnOlderServer(t *testing.T) {
	fake := &fakeUpdate{latest: release049, running: true, server: localServer{Version: "0.48.0", Active: 0}}
	var stdout, stderr bytes.Buffer
	code := fake.updater("0.49.0", "\n", true).run(context.Background(), updateOptions{}, &stdout, &stderr)
	const want = "Kit 0.49.0 is up to date.\n\n" +
		"The local server still runs 0.48.0. No sessions are working, so restarting it interrupts nothing.\n" +
		"Restart it now? [Y/n] server restarted\n"
	if code != 0 || stdout.String() != want || fake.installed {
		t.Fatalf("exit %d, installed %v, stdout %q", code, fake.installed, stdout.String())
	}

	current := &fakeUpdate{latest: release049, running: true, server: localServer{Version: "0.49.0"}}
	stdout.Reset()
	code = current.updater("0.49.0", "", true).run(context.Background(), updateOptions{}, &stdout, &stderr)
	if code != 0 || stdout.String() != "Kit 0.49.0 is up to date.\n" || current.restarted {
		t.Fatalf("exit %d, restarted %v, stdout %q", code, current.restarted, stdout.String())
	}
}

func TestUpdateDeclinedOrUnattendedDoesNotInstall(t *testing.T) {
	fake := &fakeUpdate{latest: release049}
	var stdout, stderr bytes.Buffer
	if code := fake.updater("0.48.0", "n\n", true).run(context.Background(), updateOptions{}, &stdout, &stderr); code != 0 || fake.installed {
		t.Fatalf("declined: exit %d, installed %v", code, fake.installed)
	}
	stdout.Reset()
	code := fake.updater("0.48.0", "", false).run(context.Background(), updateOptions{}, &stdout, &stderr)
	if code != 2 || fake.installed || stderr.String() != "kit: kit update asks before installing; run it in a terminal or pass --yes\n" {
		t.Fatalf("unattended: exit %d, installed %v, stderr %q", code, fake.installed, stderr.String())
	}
}

func TestUpdateRefusesDevelopmentBuildsAndReportsFailures(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeUpdate{latest: release049}
	if code := fake.updater("dev", "", true).run(context.Background(), updateOptions{}, &stdout, &stderr); code != 1 ||
		stderr.String() != "kit: this is a development build (dev); kit update updates release builds\n" {
		t.Fatalf("dev build: exit %d, stderr %q", code, stderr.String())
	}
	channel := fake.updater("0.48.0", "", true)
	channel.channel = "dev"
	stderr.Reset()
	if code := channel.run(context.Background(), updateOptions{}, &stdout, &stderr); code != 1 ||
		stderr.String() != "kit: this is a development build (0.48.0, channel dev); kit update updates release builds\n" {
		t.Fatalf("channel build: exit %d, stderr %q", code, stderr.String())
	}

	stderr.Reset()
	failing := &fakeUpdate{checkErr: errors.New("list releases: GitHub responded 403 Forbidden")}
	if code := failing.updater("0.48.0", "", true).run(context.Background(), updateOptions{}, &stdout, &stderr); code != 1 ||
		stderr.String() != "kit: check for updates: list releases: GitHub responded 403 Forbidden\n" {
		t.Fatalf("check failure: exit %d, stderr %q", code, stderr.String())
	}

	stderr.Reset()
	broken := &fakeUpdate{latest: release049, installErr: errors.New("brew upgrade failed: Error: no space left on device")}
	if code := broken.updater("0.48.0", "", true).run(context.Background(), updateOptions{Yes: true}, &stdout, &stderr); code != 1 ||
		stderr.String() != "kit: update failed: brew upgrade failed: Error: no space left on device\n" {
		t.Fatalf("install failure: exit %d, stderr %q", code, stderr.String())
	}
}

func TestUpdateCommandParsesYes(t *testing.T) {
	var got updateOptions
	deps := commandDependencies{update: func(_ context.Context, options updateOptions, _, _ io.Writer) int {
		got = options
		return 0
	}}
	var stdout, stderr bytes.Buffer
	if code := executeCommand(context.Background(), []string{"update", "--yes"}, &stdout, &stderr, deps); code != 0 || !got.Yes {
		t.Fatalf("exit %d, options %+v, stderr %q", code, got, stderr.String())
	}
}

func TestWrapWords(t *testing.T) {
	if got := wrapWords("one two three four", 9); got != "one two\nthree\nfour" {
		t.Fatalf("wrapWords() = %q", got)
	}
}
