// Package cli is Kit's executable role dispatcher.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemon"
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
  kit daemon restart          restart the local daemon
  kit version                 print version information

Development data defaults to ~/.kit-v2. Set KIT_HOME to override it.`)
}
