package session

import (
	"context"

	"github.com/akonwi/kit/internal/droids"
)

// MCPTools resolves the MCP namespace tools owned by one session runtime so its
// children can borrow them.
//
// A subagent shares its owner's cwd, so it resolves the same MCP configuration.
// Borrowing the owner's namespaces keeps one set of server processes per session
// instead of one per concurrent child. Ownership does not transfer: the owning
// runtime closes the manager during its own teardown.
// LogoutMCP closes one session-owned namespace and clears its saved OAuth state.
func (m *Manager) LogoutMCP(ctx context.Context, sessionID, serverName string) error {
	runtime, err := m.runtime(ctx, sessionID)
	if err != nil {
		return err
	}
	if runtime.bundle.MCP == nil {
		return ErrNotFound
	}
	return runtime.bundle.MCP.Logout(ctx, serverName)
}

func (m *Manager) MCPTools(ctx context.Context, sessionID string) ([]droids.AnyTool, error) {
	runtime, err := m.runtime(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if runtime.bundle.MCP == nil {
		return nil, nil
	}
	return runtime.bundle.MCP.Tools(), nil
}
