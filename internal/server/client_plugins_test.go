package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
)

const testClientPluginInstance = "clientplugin_0123456789abcdef0123456789abcdef"

// clientPluginProviders calls browser__snapshot once when asked to and
// records what the model saw.
type clientPluginProviders struct {
	daemonEchoProviders
	toolMu   sync.Mutex
	execute  bool
	offered  []string
	received string
	isError  bool
}

func (p *clientPluginProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}

func (p *clientPluginProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	if len(request.Tools) == 0 {
		return &daemonEchoStream{final: droids.AssistantMessage{Provider: "test", Model: "echo", StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "Title"}}}}
	}
	p.toolMu.Lock()
	defer p.toolMu.Unlock()
	p.offered = nil
	for _, tool := range request.Tools {
		if tool.Name == "browser__snapshot" {
			p.offered = append(p.offered, tool.Name)
		}
	}
	for _, message := range request.Messages {
		if result, ok := message.(droids.ToolResultMessage); ok && result.ToolName == "browser__snapshot" {
			p.isError = result.IsError
			for _, part := range result.Content {
				if text, ok := part.(droids.TextContent); ok {
					p.received = text.Text
				}
			}
		}
	}
	response := droids.AssistantMessage{Provider: "test", Model: "echo", StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "done"}}}
	if p.execute {
		p.execute = false
		response.StopReason = droids.StopReasonToolUse
		response.Content = []droids.AssistantContent{droids.ToolCall{ID: "call_snapshot", Name: "browser__snapshot", Arguments: json.RawMessage(`{"tab":"t1"}`)}}
	}
	return &daemonEchoStream{final: response}
}

func (p *clientPluginProviders) observed() ([]string, string, bool) {
	p.toolMu.Lock()
	defer p.toolMu.Unlock()
	return append([]string(nil), p.offered...), p.received, p.isError
}

func clientPluginDaemon(t *testing.T, providers *clientPluginProviders) (*clienttransport.Client, string) {
	t.Helper()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: paths, Providers: providers, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("daemon shutdown blocked")
		}
	})
	client := localTransport(paths)
	t.Cleanup(client.CloseIdleConnections)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, err := client.Probe(t.Context(), true); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	created, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	return client, created.ID
}

// followClientPluginEvents follows the session's event stream across the
// resyncs every new turn causes, reopening from a fresh snapshot as clients
// do, and forwards each event. instanceID binds the stream when not empty.
// The returned channel closes when the first stream is open.
func followClientPluginEvents(t *testing.T, client *clienttransport.Client, sessionID, instanceID string) (<-chan protocol.SessionEvent, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	events := make(chan protocol.SessionEvent, 256)
	opened := make(chan struct{})
	go func() {
		first := true
		for ctx.Err() == nil {
			snapshot, err := client.GetSessionSnapshot(ctx, sessionID)
			if err != nil {
				return
			}
			body, err := client.StreamClientPluginSessionEvents(ctx, sessionID, snapshot.EventStreamID, snapshot.EventCursor, instanceID)
			if err != nil {
				return
			}
			if first {
				first = false
				close(opened)
			}
			_ = clienttransport.ReadSessionEvents(body, func(_, _ string, batch protocol.SessionEventBatch) error {
				for _, event := range batch.Events {
					select {
					case events <- event:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			})
			_ = body.Close()
		}
	}()
	return events, opened
}

// nextClientPluginEvent waits for the next client_plugin.call.* event.
func nextClientPluginEvent(t *testing.T, events <-chan protocol.SessionEvent) protocol.SessionEventPayload {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-events:
			switch payload := event.Payload.(type) {
			case protocol.ClientPluginCallRequestedEvent, protocol.ClientPluginCallResolvedEvent:
				if event.TurnID != "" {
					t.Fatalf("client plugin event carries turn %q", event.TurnID)
				}
				return payload
			}
		case <-deadline:
			t.Fatal("missing client plugin event")
			return nil
		}
	}
}

var testSnapshotTool = protocol.ClientPluginToolDeclaration{
	ID: "snapshot", Description: "Outline one browser tab.", PromptSnippet: "Use refs from the outline.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"tab":{"type":"string"}},"required":["tab"],"additionalProperties":false}`),
}

func TestClientPluginToolCallRoundTripsThroughTheBoundStream(t *testing.T) {
	providers := &clientPluginProviders{}
	client, sessionID := clientPluginDaemon(t, providers)
	if err := client.AttachClientPlugin(t.Context(), sessionID, testClientPluginInstance, protocol.AttachClientPluginInput{PluginID: "browser", Tools: []protocol.ClientPluginToolDeclaration{testSnapshotTool}}); err != nil {
		t.Fatal(err)
	}
	bound, boundOpen := followClientPluginEvents(t, client, sessionID, testClientPluginInstance)
	other, otherOpen := followClientPluginEvents(t, client, sessionID, "")
	<-boundOpen
	<-otherOpen

	providers.toolMu.Lock()
	providers.execute = true
	providers.toolMu.Unlock()
	prompted := make(chan error, 1)
	go func() {
		_, err := client.Prompt(t.Context(), sessionID, "snapshot the tab")
		prompted <- err
	}()

	requested, ok := nextClientPluginEvent(t, bound).(protocol.ClientPluginCallRequestedEvent)
	if !ok || requested.ClientPluginInstanceID != testClientPluginInstance || !protocol.ValidClientPluginCallID(requested.ClientPluginCallID) || requested.ClientPluginCall == nil {
		t.Fatalf("bound stream request = %#v", requested)
	}
	if call := *requested.ClientPluginCall; call.PluginID != "browser" || call.ToolID != "snapshot" || !strings.HasPrefix(call.ToolCallID, "tool_") || !strings.HasPrefix(call.TurnID, "turn_") || call.SubagentConversationID != "" || string(call.Input) != `{"tab":"t1"}` {
		t.Fatalf("call = %#v", call)
	}
	if offered, _, _ := providers.observed(); len(offered) != 1 {
		t.Fatalf("model was offered %v", offered)
	}
	elsewhere, ok := nextClientPluginEvent(t, other).(protocol.ClientPluginCallRequestedEvent)
	if !ok || elsewhere.ClientPluginCallID != requested.ClientPluginCallID || elsewhere.ClientPluginCall != nil {
		t.Fatalf("unbound stream request = %#v", elsewhere)
	}
	snapshot, err := client.GetSessionSnapshot(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.GetSessionEvents(t.Context(), sessionID, snapshot.EventStreamID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Events {
		if request, ok := event.Payload.(protocol.ClientPluginCallRequestedEvent); ok && request.ClientPluginCall != nil {
			t.Fatalf("event page carries call %#v", request)
		}
	}

	if err := client.CompleteClientPluginCall(t.Context(), sessionID, testClientPluginInstance, requested.ClientPluginCallID, protocol.CompleteClientPluginCallInput{Content: []protocol.ClientPluginContent{{Type: "text", Text: "outline of t1"}}}); err != nil {
		t.Fatal(err)
	}
	for _, events := range []<-chan protocol.SessionEvent{bound, other} {
		resolved, ok := nextClientPluginEvent(t, events).(protocol.ClientPluginCallResolvedEvent)
		if !ok || resolved.ClientPluginCallID != requested.ClientPluginCallID || resolved.ClientPluginInstanceID != testClientPluginInstance || resolved.ClientPluginCallResolution != protocol.ClientPluginCallCompleted {
			t.Fatalf("resolution = %#v", resolved)
		}
	}
	if err := <-prompted; err != nil {
		t.Fatal(err)
	}
	if _, received, isError := providers.observed(); received != "outline of t1" || isError {
		t.Fatalf("model received %q (error %v)", received, isError)
	}

	err = client.CompleteClientPluginCall(t.Context(), sessionID, testClientPluginInstance, requested.ClientPluginCallID, protocol.CompleteClientPluginCallInput{})
	var apiErr *clienttransport.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("late result error = %v", err)
	}
}

func TestClientPluginRoutesRejectUnknownAndInvalidRequests(t *testing.T) {
	client, sessionID := clientPluginDaemon(t, &clientPluginProviders{})
	status := func(err error) int {
		var apiErr *clienttransport.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error %v is not an API error", err)
		}
		return apiErr.StatusCode
	}
	if got := status(func() error {
		_, err := client.StreamClientPluginSessionEvents(t.Context(), sessionID, "", 0, testClientPluginInstance)
		return err
	}()); got != http.StatusNotFound {
		t.Fatalf("unknown instance stream status = %d", got)
	}
	if got := status(client.DetachClientPlugin(t.Context(), sessionID, testClientPluginInstance)); got != http.StatusNotFound {
		t.Fatalf("unknown instance detach status = %d", got)
	}
	schema := testSnapshotTool
	schema.InputSchema = json.RawMessage(`{"type":"object","$ref":"#/x"}`)
	if got := status(client.AttachClientPlugin(t.Context(), sessionID, testClientPluginInstance, protocol.AttachClientPluginInput{PluginID: "browser", Tools: []protocol.ClientPluginToolDeclaration{schema}})); got != http.StatusBadRequest {
		t.Fatalf("unsupported schema status = %d", got)
	}
	if got := status(client.AttachClientPlugin(t.Context(), sessionID, testClientPluginInstance, protocol.AttachClientPluginInput{PluginID: "model", Tools: []protocol.ClientPluginToolDeclaration{}})); got != http.StatusBadRequest {
		t.Fatalf("reserved plugin id status = %d", got)
	}
	if got := status(client.AttachClientPlugin(t.Context(), "session_0123456789abcdef0123456789abcdef", testClientPluginInstance, protocol.AttachClientPluginInput{PluginID: "browser", Tools: []protocol.ClientPluginToolDeclaration{}})); got != http.StatusNotFound {
		t.Fatalf("unknown session status = %d", got)
	}
	if err := client.AttachClientPlugin(t.Context(), sessionID, testClientPluginInstance, protocol.AttachClientPluginInput{PluginID: "browser", Tools: []protocol.ClientPluginToolDeclaration{testSnapshotTool}}); err != nil {
		t.Fatal(err)
	}
	if err := client.DetachClientPlugin(t.Context(), sessionID, testClientPluginInstance); err != nil {
		t.Fatal(err)
	}
}
