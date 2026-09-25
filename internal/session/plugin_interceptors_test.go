package session

import (
	"context"
	"errors"
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestOptionalPluginHostWithoutInterceptorsHasExplicitEmptyPolicy(t *testing.T) {
	bridge := &pluginInterceptorBridge{bootstrap: "runtime:1"}
	call := droids.ToolContext{BeforeHookIdentity: bridge.identity()}
	if _, err := bridge.before(t.Context(), call, droids.ToolCall{}); err == nil {
		t.Fatal("unbound bootstrap approved execution")
	}
	bridge.binding.Store(&pluginInterceptorBinding{host: emptyPluginInterception{identity: bridge.bootstrap}})
	decision, err := bridge.before(t.Context(), call, droids.ToolCall{Name: "bash"})
	if err != nil || decision.Reject {
		t.Fatalf("optional host empty policy=%#v %v", decision, err)
	}
	call.BeforeHookIdentity = "old-runtime:1"
	decision, err = bridge.before(t.Context(), call, droids.ToolCall{Name: "bash"})
	if err != nil || !decision.Reject {
		t.Fatalf("stale empty policy=%#v %v", decision, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := bridge.before(ctx, call, droids.ToolCall{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled policy=%v", err)
	}
}
