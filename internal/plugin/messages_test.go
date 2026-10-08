package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

func TestMessageParamsAcceptBoundedTextAndOptionalKey(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want MessageRequest
	}{
		{`{"text":"Continue the loop.\nRun experiment 3."}`, MessageRequest{Text: "Continue the loop.\nRun experiment 3."}},
		{`{"text":"Retry","idempotencyKey":"experiment-3:resume_1.a"}`, MessageRequest{Text: "Retry", IdempotencyKey: "experiment-3:resume_1.a"}},
		{`{"text":"No key","idempotencyKey":null}`, MessageRequest{Text: "No key"}},
		{`{"text":"` + strings.Repeat("x", MaxMessageTextBytes) + `"}`, MessageRequest{Text: strings.Repeat("x", MaxMessageTextBytes)}},
	} {
		got, err := parseMessage(json.RawMessage(test.raw))
		if err != nil || got != test.want {
			t.Errorf("parseMessage(%.40s) = %+v, %v; want %+v", test.raw, got, err, test.want)
		}
	}
}

func TestMessageParamsRejectInvalidRequests(t *testing.T) {
	for _, test := range []struct{ raw, message string }{
		{`{}`, "Missing required parameter: text"},
		{`{"text":null}`, "Missing required parameter: text"},
		{`{"text":"  \n "}`, "Invalid parameter: text"},
		{`{"text":"nul\u0000byte"}`, "Invalid parameter: text"},
		{`{"text":1}`, "Invalid parameter: text"},
		{`{"text":"` + strings.Repeat("x", MaxMessageTextBytes+1) + `"}`, "Invalid parameter: text"},
		{`{"text":"x","idempotencyKey":"has space"}`, "Invalid parameter: idempotencyKey"},
		{`{"text":"x","idempotencyKey":""}`, "Invalid parameter: idempotencyKey"},
		{`{"text":"x","idempotencyKey":"` + strings.Repeat("k", 129) + `"}`, "Invalid parameter: idempotencyKey"},
		{`{"text":"x","sessionId":"session_other"}`, "Unknown parameter"},
		{`{"text":"x","text":"y"}`, "Duplicate parameter"},
		{`["x"]`, "Params must be an object"},
	} {
		_, err := parseMessage(json.RawMessage(test.raw))
		var failure *RPCError
		if !errors.As(err, &failure) || failure.Code != -32602 || failure.Message != test.message {
			t.Errorf("parseMessage(%.40s) = %v; want -32602 %q", test.raw, err, test.message)
		}
	}
}

func messageHost(t *testing.T, submit func(context.Context, MessageRequest) (MessageResult, error)) (*Host, InstanceID) {
	t.Helper()
	home := apphome.FromHome(t.TempDir())
	hostInstallation(t, home.Plugins, "demo", "demo", "commands")
	host := NewHost(t.Context(), HostConfig{Home: home, CWD: t.TempDir(), Session: SessionContext{ID: "one"}, SubmitMessage: submit})
	t.Cleanup(func() {
		if err := host.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	host.Start()
	eventuallyHost(t, func() bool { return len(host.Commands()) == 4 })
	return host, host.Commands()[0].Owner
}

func TestSubmitMessageForwardsCurrentGenerationToSession(t *testing.T) {
	var mu sync.Mutex
	var requests []MessageRequest
	host, owner := messageHost(t, func(_ context.Context, request MessageRequest) (MessageResult, error) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, request)
		return MessageResult{MessageID: "pluginmsg_1", TurnID: "turn_1"}, nil
	})
	result, err := host.handleRequest(t.Context(), owner, "kit/session/submit-message", json.RawMessage(`{"text":"Start autoresearch.","idempotencyKey":"kickoff"}`))
	if err != nil || string(result) != `{"messageId":"pluginmsg_1","turnId":"turn_1"}` {
		t.Fatalf("submit = %s, %v", result, err)
	}
	host.Reload()
	_, err = host.handleRequest(t.Context(), owner, "kit/session/submit-message", json.RawMessage(`{"text":"From the old generation."}`))
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32002 || failure.Message != "Plugin generation is unavailable" {
		t.Fatalf("revoked generation = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []MessageRequest{{Owner: owner, Text: "Start autoresearch.", IdempotencyKey: "kickoff"}}
	if len(requests) != 1 || requests[0] != want[0] {
		t.Fatalf("session requests = %+v, want %+v", requests, want)
	}
}

func TestSubmitMessageReportsSessionErrorsAndMissingPort(t *testing.T) {
	busy := &RPCError{Code: -32006, Message: "Session is busy", Data: json.RawMessage(`{"reason":"session_busy"}`)}
	host, owner := messageHost(t, func(context.Context, MessageRequest) (MessageResult, error) { return MessageResult{}, busy })
	_, err := host.handleRequest(t.Context(), owner, "kit/session/submit-message", json.RawMessage(`{"text":"Continue."}`))
	if !errors.Is(err, busy) {
		t.Fatalf("session error = %v, want %v", err, busy)
	}
	unavailable, unavailableOwner := messageHost(t, nil)
	_, err = unavailable.handleRequest(t.Context(), unavailableOwner, "kit/session/submit-message", json.RawMessage(`{"text":"Continue."}`))
	var failure *RPCError
	if !errors.As(err, &failure) || failure.Code != -32601 || failure.Message != "Session message submission is unavailable" {
		t.Fatalf("missing port = %v", err)
	}
}
