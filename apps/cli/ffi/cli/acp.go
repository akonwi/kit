package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/akonwi/kit/apps/cli/ffi/acp"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/version"
	"github.com/spf13/cobra"
)

type acpOptions struct {
	Temporary bool
}

func newACPCommand(deps commandDependencies) *cobra.Command {
	var options acpOptions
	command := &cobra.Command{
		Use:   "acp",
		Short: "Serve the Agent Client Protocol on stdin and stdout",
		Long: "Serve the Agent Client Protocol (ACP) on stdin and stdout, so an ACP client such as an\n" +
			"editor can run Kit as its agent. The client starts `kit acp` itself; stdout carries only\n" +
			"protocol messages and diagnostics go to stderr. Sessions are Kit sessions on the local\n" +
			"server, which kit acp starts when needed.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return resultForCode(deps.acp(command.Context(), options, deps.stdin, command.OutOrStdout(), command.ErrOrStderr()))
		},
	}
	command.Flags().BoolVar(&options.Temporary, "temp", false, "create temporary sessions, discarded when closed or when kit acp exits")
	return command
}

// runACP serves ACP on stdin and stdout against this home's server.
func runACP(ctx context.Context, options acpOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	out := stdout
	if file, ok := stdout.(*os.File); ok && file == os.Stdout {
		protocolOut, err := reserveStdout()
		if err != nil {
			fmt.Fprintf(stderr, "kit: %v\n", err)
			return 1
		}
		if protocolOut != nil {
			defer protocolOut.Close()
			out = protocolOut
		}
	}
	paths, err := apphome.Resolve("")
	if err != nil {
		fmt.Fprintf(stderr, "kit: %v\n", err)
		return 1
	}
	var connected struct {
		sync.Mutex
		closers []io.Closer
	}
	defer func() {
		connected.Lock()
		defer connected.Unlock()
		for _, closer := range connected.closers {
			_ = closer.Close()
		}
	}()
	return serveACP(ctx, options, stdin, out, stderr, func(ctx context.Context) (acp.Backend, error) {
		client, err := bootstrapLocalClient(ctx, paths)
		if err != nil {
			return nil, err
		}
		connected.Lock()
		connected.closers = append(connected.closers, client)
		connected.Unlock()
		return acp.ClientBackend{Client: client}, nil
	})
}

// serveACP runs one ACP connection and reports its exit code.
func serveACP(
	ctx context.Context, options acpOptions, stdin io.Reader, stdout, stderr io.Writer,
	connect func(context.Context) (acp.Backend, error),
) int {
	logger := slog.New(slog.NewTextHandler(stderr, nil)).With("component", "kit acp")
	err := acp.Serve(ctx, acp.Options{
		In: stdin, Out: stdout, Logger: logger, Connect: connect, Temporary: options.Temporary,
		NewSessionID: func() (string, error) { return identifier.New("session_") },
		Version:      version.Version,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(stderr, "kit: acp: %v\n", err)
		return 1
	}
	if ctx.Err() != nil {
		return 130
	}
	return 0
}
