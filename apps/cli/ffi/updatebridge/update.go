// Package updatebridge projects update checks and installs for the terminal
// client into Ard-friendly shapes (ADR 0048).
package updatebridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/akonwi/kit/internal/selfupdate"
	"github.com/akonwi/kit/internal/version"
)

// CheckIntervalMS is how often a running client checks for a newer release,
// in milliseconds.
func CheckIntervalMS() int { return int(selfupdate.CheckInterval.Milliseconds()) }

// Update is a newer release that this executable can install.
type Update struct {
	// Available reports a newer release; the other fields are empty without
	// one.
	Available bool
	// Current and Version are the running and newer versions, as "0.48.0".
	Current string
	Version string
	// Summary is the release notes' overview paragraph.
	Summary string
	// URL is the release's page on GitHub.
	URL string
	// Install is how it installs, such as "brew upgrade akonwi/tap/kit".
	Install string
	// Launcher runs the installed Kit once the update is installed.
	Launcher string

	release      selfupdate.Release
	installation selfupdate.Installation
}

// Checks reports whether this build checks for updates at all: only a
// release build does (ADR 0048).
func Checks() bool {
	_, err := selfupdate.Current(version.Version, version.Channel)
	return err == nil
}

// Check returns the newest release when it is newer than this executable.
// It is unavailable for an up-to-date or development build.
func Check(ctx context.Context) (Update, error) {
	return check(ctx, selfupdate.Checker{}, version.Version, os.Executable)
}

func check(ctx context.Context, checker selfupdate.Checker, current string, executable func() (string, error)) (Update, error) {
	running, err := selfupdate.Current(current, version.Channel)
	if errors.Is(err, selfupdate.ErrNotRelease) {
		return Update{}, nil
	}
	release, newer, err := checker.Newer(ctx, selfupdate.ArtifactCLI, running)
	if err != nil || !newer {
		return Update{}, err
	}
	path, err := executable()
	if err != nil {
		return Update{}, fmt.Errorf("resolve Kit executable: %w", err)
	}
	installation, err := selfupdate.Locate(path)
	if err != nil {
		return Update{}, err
	}
	return Update{
		Available:    true,
		Current:      running.String(),
		Version:      release.Version.String(),
		Summary:      release.Summary(),
		URL:          release.URL,
		Install:      installation.Describe(release),
		Launcher:     installation.Launcher,
		release:      release,
		installation: installation,
	}, nil
}

// installs tracks installs in progress, which the process waits for before
// it exits: exiting while Homebrew relinks Kit could leave none on PATH.
var installs struct {
	sync.Mutex
	running int
	stopped chan struct{}
}

func beginInstall() {
	installs.Lock()
	defer installs.Unlock()
	if installs.running == 0 {
		installs.stopped = make(chan struct{})
	}
	installs.running++
}

func endInstall() {
	installs.Lock()
	defer installs.Unlock()
	installs.running--
	if installs.running == 0 {
		close(installs.stopped)
	}
}

// Install installs update, reporting download progress as a percentage, or
// -1 while the progress is unknown, as when Homebrew installs it. Cancelling
// ctx stops the install, leaving the previous Kit installed.
func Install(ctx context.Context, update Update, progress func(int)) error {
	beginInstall()
	defer endInstall()
	return install(ctx, selfupdate.Installer{}, update, progress)
}

// Settle waits for any install still stopping after the terminal client
// exits, telling stderr why the exit waits.
func Settle(stderr io.Writer) {
	installs.Lock()
	running, stopped := installs.running, installs.stopped
	installs.Unlock()
	if running == 0 {
		return
	}
	fmt.Fprintln(stderr, "Stopping the update…")
	<-stopped
}

// Restarts reports whether finishing an update to installed restarts a
// server running serverVersion: only when that is an older release. An
// unknown or development version is left running.
func Restarts(serverVersion, installed string) bool {
	server, ok := selfupdate.ParseVersion(serverVersion)
	if !ok {
		return false
	}
	target, ok := selfupdate.ParseVersion(installed)
	return ok && server.Compare(target) < 0
}

func install(ctx context.Context, installer selfupdate.Installer, update Update, progress func(int)) error {
	if update.installation.Method == "" {
		return errors.New("no update to install")
	}
	if progress == nil {
		progress = func(int) {}
	}
	progress(-1)
	last := -1
	return installer.Install(ctx, update.installation, update.release, func(received, total int64) {
		if total <= 0 {
			return
		}
		percent := int(received * 100 / total)
		if percent != last {
			last = percent
			progress(percent)
		}
	})
}

// ReleaseNotesURL is the GitHub page of this build's release, or the
// releases list for a development build.
func ReleaseNotesURL() string {
	return selfupdate.CurrentReleaseURL(selfupdate.ArtifactCLI, version.Version, version.Channel)
}
