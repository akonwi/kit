package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
)

// deletionProviders calls delete_session once with deleteArguments when it is
// set, and records the tool's result.
type deletionProviders struct {
	daemonEchoProviders
	mu              sync.Mutex
	deleteArguments string
	result          string
}

func (p *deletionProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}

func (p *deletionProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	response := droids.AssistantMessage{Provider: "test", Model: "echo", StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "done"}}}
	if len(request.Tools) == 0 {
		return &daemonEchoStream{final: response}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, message := range request.Messages {
		if result, ok := message.(droids.ToolResultMessage); ok && result.ToolName == "delete_session" {
			for _, part := range result.Content {
				if text, ok := part.(droids.TextContent); ok {
					p.result = text.Text
				}
			}
		}
	}
	if p.deleteArguments != "" {
		response.StopReason = droids.StopReasonToolUse
		response.Content = []droids.AssistantContent{droids.ToolCall{ID: "delete_call", Name: "delete_session", Arguments: json.RawMessage(p.deleteArguments)}}
		p.deleteArguments = ""
	}
	return &daemonEchoStream{final: response}
}

func TestModelDeletesItsChildSessionsAfterTheUserApproves(t *testing.T) {
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	providers := &deletionProviders{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: paths, Providers: providers, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("daemon shutdown blocked")
		}
	}()
	client := localTransport(paths)
	defer client.CloseIdleConnections()
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
	cwd := t.TempDir()
	owner, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: cwd, Name: "Coordinator", Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Prompt(t.Context(), owner.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	child, err := client.ForkSession(t.Context(), owner.ID, protocol.ForkSessionInput{Name: "Docs"})
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := client.ForkSession(t.Context(), child.Session.ID, protocol.ForkSessionInput{Name: "Docs fork"})
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := client.ForkSession(t.Context(), owner.ID, protocol.ForkSessionInput{Name: "API"})
	if err != nil {
		t.Fatal(err)
	}

	providers.mu.Lock()
	providers.deleteArguments = `{"sessionIds":["` + child.Session.ID + `"]}`
	providers.mu.Unlock()
	reservation, err := client.StartPrompt(t.Context(), owner.ID, "delete the docs session")
	if err != nil {
		t.Fatal(err)
	}
	var pending protocol.InteractionRequest
	deadline = time.Now().Add(5 * time.Second)
	for {
		snapshot, err := client.GetSessionSnapshot(t.Context(), owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.PendingInteractions) == 1 {
			pending = snapshot.PendingInteractions[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the deletion never asked for approval")
		}
		time.Sleep(10 * time.Millisecond)
	}
	wantDetail := "Docs · " + cwd + " · active just now\n↳ Docs fork · " + cwd + " · active just now\n\nThis can’t be undone."
	if pending.Kind != protocol.InteractionConfirm || pending.Title != "Delete 2 sessions?" || pending.Detail != wantDetail ||
		pending.ConfirmLabel != "Delete" || pending.CancelLabel != "Keep" || pending.TurnID != reservation.TurnID || pending.ToolCallID == "" {
		t.Fatalf("approval = %#v", pending)
	}
	confirmed := true
	if err := client.RespondInteraction(t.Context(), owner.ID, protocol.InteractionResponse{RequestID: pending.ID, Confirmed: &confirmed}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		turn, err := client.GetTurn(t.Context(), owner.ID, reservation.TurnID)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Status == protocol.TurnStatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("turn = %#v", turn)
		}
		time.Sleep(10 * time.Millisecond)
	}

	providers.mu.Lock()
	result := providers.result
	providers.mu.Unlock()
	want := fmt.Sprintf(`{"deleted":[{"id":%q,"name":"Docs fork"},{"id":%q,"name":"Docs"}],"resolution":"answered","message":"Deleted 2 sessions."}`, grandchild.Session.ID, child.Session.ID)
	if result != want {
		t.Fatalf("tool result = %s\nwant %s", result, want)
	}
	sessions, err := client.ListSessions(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	var remaining []string
	for _, session := range sessions {
		remaining = append(remaining, session.ID)
	}
	expected := []string{owner.ID, sibling.Session.ID}
	slices.Sort(remaining)
	slices.Sort(expected)
	if !slices.Equal(remaining, expected) {
		t.Fatalf("remaining sessions = %v", remaining)
	}
}
