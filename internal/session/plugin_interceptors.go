package session

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"

	"github.com/akonwi/kit/internal/droids"
)

// PluginInterceptorHost exposes generation-fenced tool policy without IPC types.
type PluginInterceptorHost interface {
	InterceptorIdentity() string
	InterceptTool(context.Context, string, string, string, json.RawMessage) (PluginInterceptionDecision, error)
}

// PluginInterceptionDecision rejects one tool while allowing the turn to continue.
type PluginInterceptionDecision struct {
	Reject  bool
	Message string
}

// Hosts implementing only other optional contribution ports have an explicit,
// lifetime-owned empty policy rather than an unavailable dispatcher.
type emptyPluginInterception struct{ identity string }

func (h emptyPluginInterception) InterceptorIdentity() string { return h.identity }
func (h emptyPluginInterception) InterceptTool(ctx context.Context, identity, _, _ string, _ json.RawMessage) (PluginInterceptionDecision, error) {
	if err := ctx.Err(); err != nil {
		return PluginInterceptionDecision{}, err
	}
	if identity != h.identity {
		return PluginInterceptionDecision{}, errors.New("Plugin interception policy changed")
	}
	return PluginInterceptionDecision{}, nil
}

type pluginInterceptorBinding struct{ host PluginInterceptorHost }
type pluginInterceptorBridge struct {
	bootstrap string
	binding   atomic.Pointer[pluginInterceptorBinding]
}

func (b *pluginInterceptorBridge) identity() string {
	if binding := b.binding.Load(); binding != nil {
		return binding.host.InterceptorIdentity()
	}
	return b.bootstrap
}
func (b *pluginInterceptorBridge) before(ctx context.Context, call droids.ToolContext, tool droids.ToolCall) (droids.BeforeToolResult, error) {
	binding := b.binding.Load()
	if binding == nil {
		return droids.BeforeToolResult{}, errors.New("Plugin interception is unavailable; tool execution blocked")
	}
	decision, err := binding.host.InterceptTool(ctx, call.BeforeHookIdentity, string(tool.ID), tool.Name, tool.Arguments)
	if err != nil {
		if ctx.Err() != nil {
			return droids.BeforeToolResult{}, ctx.Err()
		}
		return droids.BeforeToolResult{Reject: true, Reason: "Plugin interception failed; tool execution blocked. No automatic replay was performed."}, nil
	}
	return droids.BeforeToolResult{Reject: decision.Reject, Reason: decision.Message}, nil
}

// PluginInterceptors resolves the owning runtime policy for session-owned children.
func (m *Manager) PluginInterceptors(ctx context.Context, sessionID string) (PluginInterceptorHost, error) {
	runtime, err := m.runtime(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	host, _ := runtime.plugins.(PluginInterceptorHost)
	return host, nil
}
