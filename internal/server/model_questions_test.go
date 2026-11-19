package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
)

// questionProviders has the model call one question tool once, then records
// the tool's result.
type questionProviders struct {
	daemonEchoProviders
	mu     sync.Mutex
	call   *droids.ToolCall
	result string
}

func (p *questionProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}

func (p *questionProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	response := droids.AssistantMessage{Provider: "test", Model: "echo", StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "done"}}}
	if len(request.Tools) == 0 {
		// Titles and other requests without tools.
		return &daemonEchoStream{final: response}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, message := range request.Messages {
		if result, ok := message.(droids.ToolResultMessage); ok && result.ProviderCallID == "question_call" {
			p.result = result.Content[0].(droids.TextContent).Text
		}
	}
	if p.call != nil {
		response.StopReason = droids.StopReasonToolUse
		response.Content = []droids.AssistantContent{*p.call}
		p.call = nil
	}
	return &daemonEchoStream{final: response}
}

func TestClientsAnswerTheModelsQuestionsOverHTTP(t *testing.T) {
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	providers := &questionProviders{}
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
	record, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Name: "Questions", Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}

	yes, text := true, "blue"
	cases := []struct {
		tool, arguments string
		answer          func(protocol.InteractionRequest) protocol.InteractionResponse
		want            string
	}{
		{"confirm_from_user", `{"title":"Proceed?"}`, func(request protocol.InteractionRequest) protocol.InteractionResponse {
			return protocol.InteractionResponse{RequestID: request.ID, Confirmed: &yes}
		}, `{"confirmed":true}`},
		{"input_from_user", `{"title":"Colour?"}`, func(request protocol.InteractionRequest) protocol.InteractionResponse {
			return protocol.InteractionResponse{RequestID: request.ID, Value: &text}
		}, `{"cancelled":false,"value":"blue"}`},
		{"select_from_user", `{"title":"Pick","options":[{"label":"One","value":"one"},{"label":"Two","value":"two"}]}`, func(request protocol.InteractionRequest) protocol.InteractionResponse {
			return protocol.InteractionResponse{RequestID: request.ID, SelectedOptionID: request.Options[1].ID}
		}, `{"cancelled":false,"label":"Two","value":"two"}`},
		{"guided_questions", `{"title":"Setup","questions":[{"id":"name","prompt":"Name?","kind":"text","required":true},{"id":"ok","prompt":"OK?","kind":"boolean"}]}`, func(request protocol.InteractionRequest) protocol.InteractionResponse {
			return protocol.InteractionResponse{RequestID: request.ID, Answers: map[string]protocol.InteractionAnswer{"name": {Text: &text}, "ok": {Boolean: &yes}}}
		}, `{"answeredCount":2,"answers":{"name":"blue","ok":true},"cancelled":false,"completed":true,"totalQuestions":2}`},
		{"confirm_from_user", `{"title":"Proceed?"}`, func(request protocol.InteractionRequest) protocol.InteractionResponse {
			return protocol.InteractionResponse{RequestID: request.ID, Cancelled: true}
		}, `{"confirmed":false,"reason":"the user declined"}`},
	}
	for _, tc := range cases {
		providers.mu.Lock()
		providers.call = &droids.ToolCall{ID: "question_call", Name: tc.tool, Arguments: json.RawMessage(tc.arguments)}
		providers.result = ""
		providers.mu.Unlock()
		reservation, err := client.StartPrompt(t.Context(), record.ID, "ask me")
		if err != nil {
			t.Fatal(err)
		}
		var pending protocol.InteractionRequest
		deadline := time.Now().Add(5 * time.Second)
		for {
			snapshot, err := client.GetSessionSnapshot(t.Context(), record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.PendingInteractions) == 1 {
				pending = snapshot.PendingInteractions[0]
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s never asked", tc.tool)
			}
			time.Sleep(10 * time.Millisecond)
		}
		// Another kind's answer is refused and leaves the question pending.
		wrong := protocol.InteractionResponse{RequestID: pending.ID, Value: &text}
		if tc.tool == "input_from_user" {
			wrong = protocol.InteractionResponse{RequestID: pending.ID, Confirmed: &yes}
		}
		if err := client.RespondInteraction(t.Context(), record.ID, wrong); err == nil {
			t.Fatalf("%s accepted another kind's answer", tc.tool)
		}
		if err := client.RespondInteraction(t.Context(), record.ID, tc.answer(pending)); err != nil {
			t.Fatalf("answering %s: %v", tc.tool, err)
		}
		deadline = time.Now().Add(5 * time.Second)
		for {
			turn, err := client.GetTurn(t.Context(), record.ID, reservation.TurnID)
			if err != nil {
				t.Fatal(err)
			}
			if turn.Status == protocol.TurnStatusCompleted {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s turn = %#v", tc.tool, turn)
			}
			time.Sleep(10 * time.Millisecond)
		}
		providers.mu.Lock()
		result := providers.result
		providers.mu.Unlock()
		if result != tc.want {
			t.Fatalf("%s result = %s, want %s", tc.tool, result, tc.want)
		}
	}
}
