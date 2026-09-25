package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// PluginCommand is a session-owned executable contribution, not a prompt template.
// Instance is an opaque generation-and-registration selection token that must
// accompany every invocation.
type PluginCommand struct {
	ID, LocalID, PluginID, Instance string
	Description, ArgName, Category  string
}

// PluginCommandHost is the optional command capability of a loaded plugin host.
// Catalog reads must not block on process IO; execution must honor cancellation.
type PluginCommandHost interface {
	Commands() []PluginCommand
	ExecuteCommand(context.Context, string, string, string) error
}

// ErrPluginCommandUnavailable rejects a stale or missing command owner.
var ErrPluginCommandUnavailable = errors.New("plugin command is unavailable; refresh the catalog without replaying the command")

// ErrPluginCommandFailed reports a dispatched failure without promising rollback.
var ErrPluginCommandFailed = errors.New("plugin command failed; effects may have partially completed")

// ExecutePluginCommand runs one selected plugin callback without admitting a model turn.
func (m *Manager) ExecutePluginCommand(ctx context.Context, sessionID, instance, id, args string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(instance) == 0 || len(instance) > 128 || len(id) == 0 || len(id) > 257 || len(args) > 64*1024 || !utf8.ValidString(args) || strings.ContainsRune(args, 0) {
		return fmt.Errorf("%w: invalid plugin command selection or arguments", ErrInvalidInput)
	}
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return err
	}
	loaded.mu.Lock()
	host, ok := loaded.plugins.(PluginCommandHost)
	loaded.mu.Unlock()
	if !ok {
		return ErrPluginCommandUnavailable
	}
	return host.ExecuteCommand(ctx, instance, id, args)
}
