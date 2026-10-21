package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"

	"github.com/akonwi/kit/internal/selfupdate"
)

// Relaunch is the terminal client's request to finish an update once it
// exits: optionally restart this home's server with the newly installed Kit,
// then run that Kit on the same session (ADR 0048).
type Relaunch struct {
	mu            sync.Mutex
	requested     bool
	sessionID     string
	restartServer bool
}

// Request asks to relaunch on sessionID after the terminal client exits,
// restarting this home's server first when restartServer is set.
func (r *Relaunch) Request(sessionID string, restartServer bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requested = true
	r.sessionID = sessionID
	r.restartServer = restartServer
}

func (r *Relaunch) take() (sessionID string, restartServer, requested bool) {
	if r == nil {
		return "", false, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID, r.restartServer, r.requested
}

// relaunchArguments are the command line that reopens sessionID on the
// server target names. A temporary session was disposed with the client, so
// a new temporary session opens in its working directory instead.
func relaunchArguments(target attachTarget, sessionID string, temporary bool, cwd string) []string {
	var arguments []string
	if !target.own() {
		server := target.Home
		switch {
		case target.URL != "":
			server = target.URL
		case target.SSH != "":
			server = target.SSH
		}
		arguments = append(arguments, "attach", server)
	}
	if temporary || sessionID == "" {
		arguments = append(arguments, "--temp")
		if cwd != "" {
			arguments = append(arguments, "--cwd", cwd)
		}
		return arguments
	}
	return append(arguments, "--session", sessionID)
}

// relaunch restarts the local server when asked, then replaces this process
// with the newly installed Kit. It returns only when that fails.
func (p Prepared) relaunch(ctx context.Context, sessionID string, restartServer bool, stderr io.Writer) int {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "kit: relaunch: resolve Kit executable: %v\n", err)
		return 1
	}
	installation, err := selfupdate.Locate(executable)
	if err != nil {
		fmt.Fprintf(stderr, "kit: relaunch: %v\n", err)
		return 1
	}
	if restartServer {
		fmt.Fprintln(stderr, "Restarting the local server…")
		if err := restartLocalServer(ctx, installation, stderr, stderr); err != nil {
			fmt.Fprintf(stderr, "kit: restart server: %v\n", err)
			return interrupted(ctx, 1)
		}
	}
	arguments := relaunchArguments(p.target, sessionID, p.Options.TemporarySession, p.Options.CWD)
	err = syscall.Exec(installation.Launcher, append([]string{installation.Launcher}, arguments...), os.Environ())
	fmt.Fprintf(stderr, "kit: relaunch %s: %v\n", installation.Launcher, err)
	return 1
}
