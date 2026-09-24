package mcpruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/akonwi/kit/internal/droids/mcp"
)

// CloseManager closes an MCP manager without exceeding the caller's deadline.
//
// The agent core's Close is synchronous. Kit-created HTTP transports bound the
// SDK's graceful session deletion and stdio processes are supervised, but this
// outer deadline also protects teardown from a custom transport that violates
// cancellation or any future unbounded SDK close path.
//
// If that final bound expires, cleanup continues in the background. Every stdio
// server process is started under the launcher lifetime, so runtime shutdown
// still terminates its process group.
func CloseManager(ctx context.Context, manager *mcp.Manager) error {
	if manager == nil {
		return nil
	}
	return closeWithinDeadline(ctx, manager)
}

// managerCloser is the smallest manager surface needed by bounded teardown.
// Keeping the deadline mechanism independent of the concrete manager lets its
// unresponsive-peer behavior be tested without adding hooks to agent core.
type managerCloser interface {
	Close() error
}

func closeWithinDeadline(ctx context.Context, manager managerCloser) error {
	if ctx == nil {
		ctx = context.Background()
	}
	closed := make(chan error, 1)
	go func() { closed <- manager.Close() }()
	select {
	case err := <-closed:
		return err
	case <-ctx.Done():
		return fmt.Errorf("close MCP manager: %w", errors.Join(ctx.Err(), errClosePending))
	}
}

// errClosePending marks a close that is still settling in the background.
var errClosePending = errors.New("close is still settling; server processes end with the runtime lifetime")
