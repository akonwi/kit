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
	launcher      string
	restartServer bool
}

// Request asks to relaunch on sessionID with launcher, the newly installed
// Kit, after the terminal client exits, restarting this home's server first
// when restartServer is set.
func (r *Relaunch) Request(sessionID, launcher string, restartServer bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requested = true
	r.sessionID = sessionID
	r.launcher = launcher
	r.restartServer = restartServer
}

func (r *Relaunch) take() (request relaunchRequest, requested bool) {
	if r == nil {
		return relaunchRequest{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return relaunchRequest{sessionID: r.sessionID, launcher: r.launcher, restartServer: r.restartServer}, r.requested
}

type relaunchRequest struct {
	sessionID     string
	launcher      string
	restartServer bool
}

// relaunchArguments are the command line that reopens sessionID on the
// server target names. A temporary session was disposed with the client, so
// a new temporary session opens in its working directory instead; without a
// session the client opens as it would by default.
func relaunchArguments(target attachTarget, sessionID string, temporary bool, cwd string) []string {
	var arguments []string
	if !target.own() {
		server := target.Home
		switch {
		case target.URL != "":
			server = target.URL
		case target.SSH != "" && target.Name != target.SSH:
			// A saved SSH server is found by its name; its destination may
			// be an ssh_config alias that names nothing saved.
			server = target.Name
		case target.SSH != "":
			server = target.SSH
		}
		arguments = append(arguments, "attach", server)
	}
	switch {
	case temporary:
		arguments = append(arguments, "--temp")
		if cwd != "" {
			arguments = append(arguments, "--cwd", cwd)
		}
		return arguments
	case sessionID == "":
		return arguments
	}
	return append(arguments, "--session", sessionID)
}

// relaunch restarts the local server when asked, then replaces this process
// with the newly installed Kit. It returns only when that fails.
func (p Prepared) relaunch(ctx context.Context, request relaunchRequest, stderr io.Writer) int {
	launcher := request.launcher
	if launcher == "" {
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
		launcher = installation.Launcher
	}
	if request.restartServer {
		fmt.Fprintln(stderr, "Restarting the local server…")
		if err := restartLocalServer(ctx, launcher, stderr, stderr); err != nil {
			// The old server keeps running, so the client still reopens.
			fmt.Fprintf(stderr, "kit: restart server: %v\n", err)
			if ctx.Err() != nil {
				return 130
			}
		}
	}
	// The session is temporary only when it is the one this launch created;
	// one opened since, as with /new, persists and reopens.
	temporary := p.temporaryID != "" && request.sessionID == p.temporaryID
	arguments := relaunchArguments(p.target, request.sessionID, temporary, p.Options.CWD)
	err := syscall.Exec(launcher, append([]string{launcher}, arguments...), os.Environ())
	fmt.Fprintf(stderr, "kit: relaunch %s: %v\n", launcher, err)
	return 1
}
