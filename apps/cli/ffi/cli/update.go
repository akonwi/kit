package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemonctl"
	"github.com/akonwi/kit/internal/selfupdate"
	"github.com/akonwi/kit/internal/version"
)

// updateOptions are `kit update`'s flags.
type updateOptions struct {
	// Yes answers the prompts for scripts, restarting only an idle server.
	Yes bool
}

// localServer is what `kit update` needs to know about this home's server.
type localServer struct {
	// Version is the server's Kit version.
	Version string
	// Active counts sessions that are working or waiting on their user, or
	// is negative when the server could not report them.
	Active int
}

// updater carries `kit update`'s effects so tests can replace them.
type updater struct {
	current string
	channel string
	stdin   io.Reader
	// interactive reports whether stdin is a terminal a person answers.
	interactive bool
	// progressLine reports whether stdout can redraw a progress line.
	progressLine bool
	locate       func() (selfupdate.Installation, error)
	newer        func(context.Context, selfupdate.Version) (selfupdate.Release, bool, error)
	install      func(context.Context, selfupdate.Installation, selfupdate.Release, io.Writer, selfupdate.Progress) error
	// server reports this home's running server, or false when none runs.
	server func(context.Context) (localServer, bool)
	// restart restarts this home's server with the Kit at a launcher path.
	restart func(context.Context, string, io.Writer, io.Writer) error
}

func defaultUpdater() updater {
	return updater{
		current:      version.Version,
		channel:      version.Channel,
		stdin:        os.Stdin,
		interactive:  stdinIsTerminal(),
		progressLine: stdoutIsTerminal(),
		locate: func() (selfupdate.Installation, error) {
			executable, err := os.Executable()
			if err != nil {
				return selfupdate.Installation{}, fmt.Errorf("resolve Kit executable: %w", err)
			}
			return selfupdate.Locate(executable)
		},
		newer: func(ctx context.Context, current selfupdate.Version) (selfupdate.Release, bool, error) {
			return selfupdate.Checker{}.Newer(ctx, selfupdate.ArtifactCLI, current)
		},
		install: func(ctx context.Context, installation selfupdate.Installation, release selfupdate.Release, output io.Writer, progress selfupdate.Progress) error {
			return selfupdate.Installer{Output: output}.Install(ctx, installation, release, progress)
		},
		server:  inspectLocalServer,
		restart: restartLocalServer,
	}
}

func runUpdateCommand(ctx context.Context, options updateOptions, stdout, stderr io.Writer) int {
	return defaultUpdater().run(ctx, options, stdout, stderr)
}

func (u updater) run(ctx context.Context, options updateOptions, stdout, stderr io.Writer) int {
	current, err := selfupdate.Current(u.current, u.channel)
	if err != nil {
		fmt.Fprintf(stderr, "kit: this is a development build (%s); kit update updates release builds\n", u.describeBuild())
		return 1
	}
	installation, err := u.locate()
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	checkContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	release, newer, err := u.newer(checkContext, current)
	cancel()
	if err != nil {
		fmt.Fprintf(stderr, "kit: check for updates: %v\n", err)
		return interrupted(ctx, 1)
	}
	answers := bufio.NewReader(u.stdin)
	installed := current
	if !newer {
		fmt.Fprintf(stdout, "Kit %s is up to date.\n", current)
	} else {
		fmt.Fprintf(stdout, "Kit %s → %s\n", current, release.Version)
		if summary := release.Summary(); summary != "" {
			fmt.Fprintf(stdout, "\n%s\n", wrapWords(summary, 80))
		}
		fmt.Fprintf(stdout, "\nRelease notes: %s\n", release.URL)
		if !options.Yes {
			if !u.interactive {
				fmt.Fprintln(stderr, "kit: kit update asks before installing; run it in a terminal or pass --yes")
				return 2
			}
			if !u.confirm(answers, stdout, fmt.Sprintf("Update with `%s`?", installation.Describe(release)), true) {
				return 0
			}
		}
		if code := u.installRelease(ctx, installation, release, stdout, stderr); code != 0 {
			return code
		}
		installed = release.Version
		fmt.Fprintf(stdout, "Kit %s is installed.\n", installed)
	}
	return u.offerRestart(ctx, options, installation, installed, answers, stdout, stderr)
}

func (u updater) describeBuild() string {
	if u.channel != "" {
		return u.current + ", channel " + u.channel
	}
	return u.current
}

func (u updater) installRelease(ctx context.Context, installation selfupdate.Installation, release selfupdate.Release, stdout, stderr io.Writer) int {
	var progress selfupdate.Progress
	if installation.Method == selfupdate.MethodArchive {
		name := selfupdate.ArchiveName(release.Tag, runtime.GOOS, runtime.GOARCH)
		lastPercent := -1
		if u.progressLine {
			progress = func(received, total int64) {
				if total <= 0 {
					return
				}
				percent := int(received * 100 / total)
				if percent != lastPercent {
					lastPercent = percent
					fmt.Fprintf(stdout, "\rDownloading %s… %d%%", name, percent)
				}
			}
		} else {
			fmt.Fprintf(stdout, "Downloading %s…\n", name)
		}
	}
	err := u.install(ctx, installation, release, stdout, progress)
	if progress != nil {
		fmt.Fprintln(stdout)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return interrupted(ctx, 1)
		}
		fmt.Fprintf(stderr, "kit: update failed: %v\n", err)
		return interrupted(ctx, 1)
	}
	return 0
}

// offerRestart asks to restart this home's server when it runs an older Kit
// than the one installed, saying what a restart interrupts.
func (u updater) offerRestart(
	ctx context.Context, options updateOptions, installation selfupdate.Installation,
	installed selfupdate.Version, answers *bufio.Reader, stdout, stderr io.Writer,
) int {
	server, running := u.server(ctx)
	if !running {
		return 0
	}
	serverVersion, ok := selfupdate.ParseVersion(server.Version)
	if !ok || serverVersion.Compare(installed) >= 0 {
		return 0
	}
	fmt.Fprintf(stdout, "\nThe local server still runs %s. %s\n", serverVersion, describeInterruption(server.Active))
	restart := false
	switch {
	case options.Yes:
		restart = server.Active == 0
	case u.interactive:
		restart = u.confirm(answers, stdout, "Restart it now?", server.Active == 0)
	}
	if !restart {
		fmt.Fprintln(stdout, "Restart it later with `kit server restart`.")
		return 0
	}
	if err := u.restart(ctx, installation.Launcher, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "kit: restart server: %v\n", err)
		return interrupted(ctx, 1)
	}
	return 0
}

// describeInterruption says what restarting a server with active sessions
// interrupts.
func describeInterruption(active int) string {
	switch {
	case active < 0:
		return "Restarting it interrupts any session that is working."
	case active == 0:
		return "No sessions are working, so restarting it interrupts nothing."
	case active == 1:
		return "Restarting it interrupts 1 session that is working or waiting on you."
	default:
		return fmt.Sprintf("Restarting it interrupts %d sessions that are working or waiting on you.", active)
	}
}

// confirm asks a yes-or-no question; an empty answer takes the default.
func (u updater) confirm(answers *bufio.Reader, stdout io.Writer, question string, preferYes bool) bool {
	choices := "[y/N]"
	if preferYes {
		choices = "[Y/n]"
	}
	fmt.Fprintf(stdout, "%s %s ", question, choices)
	line, err := answers.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(stdout)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return preferYes
	case "y", "yes":
		return true
	default:
		return false
	}
}

// inspectLocalServer reports this home's running server without starting
// one.
func inspectLocalServer(ctx context.Context) (localServer, bool) {
	paths, err := apphome.Resolve("")
	if err != nil {
		return localServer{}, false
	}
	statusContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	registry, _, err := daemonctl.NewManager(paths).Status(statusContext)
	if err != nil {
		return localServer{}, false
	}
	server := localServer{Version: registry.KitVersion, Active: -1}
	client, err := kit.Connect(statusContext, kit.Home(paths.Home))
	if err != nil {
		return server, true
	}
	defer client.Close()
	if activity, err := client.SessionActivity(statusContext); err == nil {
		server.Active = len(activity)
	}
	return server, true
}

// restartLocalServer restarts this home's server with the newly installed
// executable, which the server then runs.
func restartLocalServer(ctx context.Context, launcher string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, launcher, "server", "restart")
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = 5 * time.Second
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s server restart: %w", launcher, err)
	}
	return nil
}

// wrapWords wraps text at spaces to lines of at most width runes where it can.
func wrapWords(text string, width int) string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) > width:
			lines = append(lines, line)
			line = word
		default:
			line += " " + word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
