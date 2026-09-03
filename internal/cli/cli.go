// Package cli is Kit's executable role dispatcher.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	kitclient "github.com/akonwi/kit/internal/client"
	"github.com/akonwi/kit/internal/daemon"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"github.com/akonwi/kit/internal/version"
)

// Run dispatches a Kit process role and returns its exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Fprintf(stdout, "kit %s (%s)\n", version.Version, version.Commit)
			return 0
		case "__daemon":
			return runInternalDaemon(ctx, args[1:], stderr)
		case "daemon":
			return runDaemonCommand(ctx, args[1:], stdout, stderr)
		case "-p", "--print":
			return runPrint(ctx, args[1:], stdout, stderr)
		case "help", "--help", "-h":
			writeHelp(stdout)
			return 0
		default:
			fmt.Fprintf(stderr, "kit: unsupported v2 bootstrap command %q\n", args[0])
			writeHelp(stderr)
			return 2
		}
	}

	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	manager := daemon.NewManager(paths)
	startContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	registry, err := manager.Ensure(startContext)
	if err != nil {
		fmt.Fprintf(stderr, "kit: start local daemon: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Kit v2 daemon ready (pid %d, %s). Native TUI bootstrap is next.\n", registry.PID, registry.URL)
	return 0
}

func runPrint(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("--print", flag.ContinueOnError)
	flags.SetOutput(stderr)
	model := flags.String("model", "", "provider/model to use for a new session")
	sessionID := flags.String("session", "", "existing session id")
	cwd := flags.String("cwd", "", "session working directory")
	name := flags.String("name", "", "name for a new session")
	thinking := flags.String("thinking", "", "reasoning level for a new session")
	newSession := flags.Bool("new-session", false, "always create a new session")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() == 0 {
		fmt.Fprintln(stderr, "kit: --print requires a prompt")
		return 2
	}
	if *sessionID != "" && (*newSession || *model != "" || *thinking != "") {
		fmt.Fprintln(stderr, "kit: --session cannot be combined with --new-session, --model, or --thinking")
		return 2
	}
	prompt := strings.Join(flags.Args(), " ")
	if *cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "kit: determine current directory: %v\n", err)
			return 1
		}
		*cwd = current
	}
	absoluteCWD, err := filepath.Abs(*cwd)
	if err != nil {
		fmt.Fprintf(stderr, "kit: resolve working directory: %v\n", err)
		return 1
	}
	*cwd = absoluteCWD

	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	startContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	_, err = daemon.NewManager(paths).Ensure(startContext)
	cancel()
	if err != nil {
		fmt.Fprintf(stderr, "kit: start local daemon: %v\n", err)
		return 1
	}
	return executePrint(ctx, kitclient.NewLocalServer(paths), printOptions{
		CWD: *cwd, Name: *name, Model: *model, ThinkingLevel: *thinking,
		SessionID: *sessionID, NewSession: *newSession, Prompt: prompt,
	}, stdout, stderr)
}

type printOptions struct {
	CWD           string
	Name          string
	Model         string
	ThinkingLevel string
	SessionID     string
	NewSession    bool
	Prompt        string
}

func executePrint(
	ctx context.Context,
	client sessionclient.Server,
	options printOptions,
	stdout, stderr io.Writer,
) int {
	sessionID := options.SessionID
	if sessionID == "" {
		if !options.NewSession {
			sessions, err := client.ListSessions(ctx, options.CWD)
			if err != nil {
				fmt.Fprintf(stderr, "kit: list sessions: %v\n", err)
				return 1
			}
			if len(sessions) > 0 && (options.Model == "" || sessions[0].Model == options.Model) {
				sessionID = sessions[0].ID
			}
		}
		if sessionID == "" {
			if options.Model == "" {
				fmt.Fprintln(stderr, "kit: --model is required when no matching session exists")
				return 2
			}
			created, err := client.CreateSession(ctx, protocol.CreateSessionInput{
				CWD: options.CWD, Name: options.Name, Model: options.Model,
				ThinkingLevel: options.ThinkingLevel,
			})
			if err != nil {
				fmt.Fprintf(stderr, "kit: create session: %v\n", err)
				return 1
			}
			sessionID = created.ID
		}
	}

	bound, err := client.Attach(ctx, sessionID)
	if err != nil {
		fmt.Fprintf(stderr, "kit: attach session: %v\n", err)
		return 1
	}
	run, err := bound.StartPrompt(ctx, options.Prompt)
	if err != nil {
		fmt.Fprintf(stderr, "kit: start prompt: %v\n", err)
		return 1
	}
	outcome, err := run.Wait(ctx)
	if err != nil {
		if ctx.Err() != nil {
			abortContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			abortErr := run.Abort(abortContext)
			cancel()
			var apiError *daemon.APIError
			if abortErr != nil && (!errors.As(abortErr, &apiError) || apiError.StatusCode != 409) {
				fmt.Fprintf(stderr, "kit: abort session after interruption: %v\n", abortErr)
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				fmt.Fprintln(stderr, "kit: interrupted")
				return 130
			}
			fmt.Fprintln(stderr, "kit: prompt timed out")
			return 1
		}
		fmt.Fprintf(stderr, "kit: run prompt: %v\n", err)
		return 1
	}
	if outcome.Status != protocol.RunStatusCompleted {
		message := outcome.ErrorMessage
		if message == "" {
			message = "agent run " + string(outcome.Status)
		}
		fmt.Fprintf(stderr, "kit: %s\n", message)
		return 1
	}
	fmt.Fprint(stdout, outcome.Text)
	return 0
}

func runInternalDaemon(ctx context.Context, args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("__daemon", flag.ContinueOnError)
	flags.SetOutput(stderr)
	home := flags.String("home", "", "Kit home directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "kit: __daemon accepts no positional arguments")
		return 2
	}
	paths, err := apphome.Resolve(*home)
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := daemon.Run(ctx, daemon.RunOptions{Paths: paths, Logger: logger}); err != nil {
		if errors.Is(err, daemon.ErrAlreadyRunning) {
			fmt.Fprintln(stderr, "kit: local daemon is already running")
			return 2
		}
		fmt.Fprintf(stderr, "kit: local daemon failed: %v\n", err)
		return 1
	}
	return 0
}

func runDaemonCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: kit daemon <start|status|stop|restart>")
		return 2
	}
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	manager := daemon.NewManager(paths)

	switch args[0] {
	case "start":
		if len(args) != 1 {
			return daemonUsage(stderr)
		}
		operationContext, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		registry, err := manager.Ensure(operationContext)
		if err != nil {
			fmt.Fprintf(stderr, "kit: start daemon: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "daemon running: pid %d, %s\n", registry.PID, registry.URL)
		return 0
	case "status":
		if len(args) != 1 {
			return daemonUsage(stderr)
		}
		operationContext, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		registry, _, err := manager.Status(operationContext)
		if err != nil {
			fmt.Fprintf(stderr, "daemon unavailable: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "daemon running: pid %d, %s, version %s\n", registry.PID, registry.URL, registry.KitVersion)
		return 0
	case "stop":
		if len(args) != 1 {
			return daemonUsage(stderr)
		}
		operationContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := manager.Stop(operationContext); err != nil {
			fmt.Fprintf(stderr, "kit: stop daemon: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "daemon stopped")
		return 0
	case "restart":
		if len(args) != 1 {
			return daemonUsage(stderr)
		}
		operationContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := manager.Stop(operationContext); err != nil {
			fmt.Fprintf(stderr, "kit: stop daemon: %v\n", err)
			return 1
		}
		registry, err := manager.Ensure(operationContext)
		if err != nil {
			fmt.Fprintf(stderr, "kit: restart daemon: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "daemon restarted: pid %d, %s\n", registry.PID, registry.URL)
		return 0
	default:
		return daemonUsage(stderr)
	}
}

func daemonUsage(output io.Writer) int {
	fmt.Fprintln(output, "usage: kit daemon <start|status|stop|restart>")
	return 2
}

func writeHelp(output io.Writer) {
	fmt.Fprintln(output, `Kit v2 bootstrap

Usage:
  kit                         start/attach to the local daemon
  kit daemon start            start or discover the local daemon
  kit daemon status           inspect the local daemon
  kit daemon stop             stop the local daemon
  kit -p [options] PROMPT     run a persisted prompt without the TUI
  kit daemon restart          restart the local daemon
  kit version                 print version information

Print options:
  --model PROVIDER/MODEL      model for a new session
  --session ID                continue an exact session
  --new-session               create instead of resuming by cwd

Development data defaults to ~/.kit-v2. Set KIT_HOME to override it.`)
}
