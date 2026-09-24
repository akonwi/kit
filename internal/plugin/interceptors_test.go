package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/akonwi/kit/internal/apphome"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func registerInterceptor(t *testing.T, h *Host, owner InstanceID) {
	t.Helper()
	result, err := h.handleRequest(t.Context(), owner, "kit/tool-calls/register-interceptor", nil)
	if err != nil || string(result) != "null" {
		t.Fatalf("register=%s %v", result, err)
	}
}
func TestInterceptorRegistrationAndExactExecution(t *testing.T) {
	h, root := commandHost(t)
	owner := findCommand(t, h, "demo.ok").Owner
	empty := h.InterceptorIdentity()
	registerInterceptor(t, h, owner)
	identity := h.InterceptorIdentity()
	if empty == identity {
		t.Fatal("registration did not replace policy identity")
	}
	registerInterceptor(t, h, owner)
	if h.InterceptorIdentity() != identity {
		t.Fatal("idempotent registration changed identity")
	}
	for _, raw := range []string{"null", "{}", "[]"} {
		if _, err := h.handleRequest(t.Context(), owner, "kit/tool-calls/register-interceptor", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted params %s", raw)
		}
	}
	decision, err := h.InterceptTool(t.Context(), identity, "call_1", "bash", json.RawMessage(`{"value":9007199254740993}`))
	if err != nil || decision.Reject {
		t.Fatalf("allow=%#v %v", decision, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "interceptor.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"toolCall":{"id":"call_1","name":"bash","input":{"value":9007199254740993}}}` {
		t.Fatalf("intercepted payload=%s", raw)
	}
	decision, err = h.InterceptTool(t.Context(), identity, "call_2", "reject", json.RawMessage(`{}`))
	if err != nil || !decision.Reject || decision.Message != "Policy rejected this call" {
		t.Fatalf("reject=%#v %v", decision, err)
	}
	if _, err = h.InterceptTool(t.Context(), identity, "call_3", "invalid", json.RawMessage(`{}`)); err == nil {
		t.Fatal("malformed decision allowed execution")
	}
}
func TestInterceptorReplacementRevocationAndCancellation(t *testing.T) {
	h, root := commandHost(t)
	owner := findCommand(t, h, "demo.ok").Owner
	registerInterceptor(t, h, owner)
	old := h.InterceptorIdentity()
	for range 2 {
		if _, err := h.handleRequest(t.Context(), owner, "kit/tool-calls/unregister-interceptor", nil); err != nil {
			t.Fatal(err)
		}
	}
	registerInterceptor(t, h, owner)
	if _, err := h.InterceptTool(t.Context(), old, "stale", "bash", json.RawMessage(`{}`)); !errors.Is(err, ErrInterceptorUnavailable) {
		t.Fatalf("old policy=%v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := h.InterceptTool(ctx, h.InterceptorIdentity(), "waiting", "wait", json.RawMessage(`{}`))
		done <- err
	}()
	eventuallyHost(t, func() bool { _, err := os.Stat(filepath.Join(root, "interceptor.json")); return err == nil })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("interception failed to cancel")
	}
	current := h.InterceptorIdentity()
	h.Reload()
	if _, err := h.InterceptTool(t.Context(), current, "reload", "bash", json.RawMessage(`{}`)); !errors.Is(err, ErrInterceptorUnavailable) {
		t.Fatalf("reload=%v", err)
	}
}
func TestInterceptorDecisionParsingFailsClosed(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"action":"allow","message":"extra"}`, `{"action":"allow","action":"allow"}`, `{"action":"reject-and-continue"}`, `{"action":"reject-and-continue","message":"\u001b"}`, `{"action":"allow","unknown":true}`} {
		if _, err := parseInterceptionDecision(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestInterceptorRegistrationOrderStopsAtFirstRejection(t *testing.T) {
	home := apphome.FromHome(t.TempDir())
	firstRoot := hostInstallation(t, home.Plugins, "a", "first", "normal")
	secondRoot := hostInstallation(t, home.Plugins, "b", "second", "normal")
	h := testHost(t, home, t.TempDir())
	h.Start()
	eventuallyHost(t, func() bool { return readyHost(h, 2) })
	owners := h.Instances()
	registerInterceptor(t, h, owners[1].ID)
	registerInterceptor(t, h, owners[0].ID)
	decision, err := h.InterceptTool(t.Context(), h.InterceptorIdentity(), "rejected", "reject", json.RawMessage(`{}`))
	if err != nil || !decision.Reject {
		t.Fatalf("first rejection=%#v %v", decision, err)
	}
	if _, err := os.Stat(filepath.Join(secondRoot, "interceptor.json")); err != nil {
		t.Fatalf("first registered interceptor not called: %v", err)
	}
	if _, err := os.Stat(filepath.Join(firstRoot, "interceptor.json")); !os.IsNotExist(err) {
		t.Fatalf("later interceptor called after rejection: %v", err)
	}
}

func TestUnregisterCancelsWaitingInterception(t *testing.T) {
	h, root := commandHost(t)
	owner := findCommand(t, h, "demo.ok").Owner
	registerInterceptor(t, h, owner)
	identity := h.InterceptorIdentity()
	done := make(chan error, 1)
	go func() {
		_, err := h.InterceptTool(t.Context(), identity, "waiting", "wait", json.RawMessage(`{}`))
		done <- err
	}()
	eventuallyHost(t, func() bool { _, err := os.Stat(filepath.Join(root, "interceptor.json")); return err == nil })
	if _, err := h.handleRequest(t.Context(), owner, "kit/tool-calls/unregister-interceptor", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked interceptor approved")
		}
	case <-time.After(time.Second):
		t.Fatal("unregistered interceptor remained blocked")
	}
}
