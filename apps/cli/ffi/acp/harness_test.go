package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	kit "github.com/akonwi/kit/api"
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/apphome"
	kitserver "github.com/akonwi/kit/internal/server"
)

// The package's tests share one Kit server, run in this process on an
// isolated home with a scripted model provider.
var testProvider = &scriptProvider{}

func TestMain(m *testing.M) {
	os.Exit(runWithServer(m))
}

func runWithServer(m *testing.M) int {
	home, err := os.MkdirTemp("", "kit-acp-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(home)
	if err := os.Setenv("KIT_HOME", home); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	paths := apphome.FromHome(home)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- kitserver.Run(ctx, kitserver.RunOptions{
			Paths: paths, Providers: testProvider, Logger: slog.New(slog.DiscardHandler),
		})
	}()
	defer func() {
		cancel()
		<-done
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		connectContext, connectCancel := context.WithTimeout(ctx, time.Second)
		client, err := kit.Connect(connectContext, kit.Local())
		connectCancel()
		if err == nil {
			_ = client.Close()
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "test server did not start:", err)
			return 1
		}
		time.Sleep(20 * time.Millisecond)
	}
	if os.Getenv("KIT_ACP_TEST_AGENT") == "1" {
		return serveTestAgent(ctx)
	}
	return m.Run()
}

// serveTestAgent serves ACP on stdin and stdout against the scripted test
// server instead of running the tests, so a compiled test binary can stand in
// for kit acp under the ACP Test Compatibility Kit (see testdata/README.md).
func serveTestAgent(ctx context.Context) int {
	err := Serve(ctx, Options{
		In: os.Stdin, Out: os.Stdout, Connect: connectTestServer,
		Logger:       slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		NewSessionID: func() (string, error) { return fmt.Sprintf("session_tck%d", time.Now().UnixNano()), nil },
		Version:      "test",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// connectTestServer connects to the shared test server.
func connectTestServer(ctx context.Context) (Backend, error) {
	client, err := kit.Connect(ctx, kit.Local())
	if err != nil {
		return nil, err
	}
	return ClientBackend{Client: client}, nil
}

// scriptProvider is a model provider whose replies follow the last user
// message:
//
//   - "read PATH" calls read on PATH; "edit PATH" replaces "old" with "new"
//     in PATH; "write PATH" writes "hello" to PATH; "confirm" and "choose"
//     ask the user; each then reports the tool result.
//   - "think" thinks before replying; "slow" waits until it is cancelled;
//     "fail auth" fails with an authentication error.
//   - anything else is answered with "Hello, world." in two parts.
type scriptProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *scriptProvider) ID() string { return "test" }

func (p *scriptProvider) Models() []droids.Model {
	return []droids.Model{{
		ID: "script", Name: "Script", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		Reasoning: true, ReasoningLevels: []string{"low", "high"},
		Input: []string{"text"}, ContextWindow: 100_000, MaxOutputTokens: 8_192,
		Cost: droids.Cost{Input: 1, Output: 2},
	}, {
		ID: "other", Name: "Other", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		Input: []string{"text"}, ContextWindow: 50_000, MaxOutputTokens: 4_096,
	}}
}

func (p *scriptProvider) Model(id string) (droids.Model, bool) {
	for _, model := range p.Models() {
		if id == model.ID || id == model.Provider+"/"+model.ID {
			return model, true
		}
	}
	return droids.Model{}, false
}

func (p *scriptProvider) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}

func (p *scriptProvider) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}

func (p *scriptProvider) RefreshModels(context.Context) error { return nil }

func (p *scriptProvider) Stream(ctx context.Context, model droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	message := droids.AssistantMessage{
		Provider: "test", Model: model.ID, StopReason: droids.StopReasonStop,
		Usage: droids.Usage{Input: 1_000, Output: 100, TotalTokens: 1_100}, Timestamp: time.Now().UnixMilli(),
	}
	if strings.Contains(request.SystemPrompt, "conversation titles") {
		message.Content = []droids.AssistantContent{droids.TextContent{Text: "Scripted session"}}
		return &scriptStream{final: message}
	}
	last := request.Messages[len(request.Messages)-1]
	if result, ok := last.(droids.ToolResultMessage); ok {
		message.Content = []droids.AssistantContent{droids.TextContent{Text: "Finished " + result.ToolName + "."}}
		return &scriptStream{events: textEvents("Finished " + result.ToolName + "."), final: message}
	}
	prompt := lastUserText(request.Messages)
	command, argument, _ := strings.Cut(prompt, " ")
	toolCall := func(name string, arguments any) *scriptStream {
		raw, _ := json.Marshal(arguments)
		call := droids.ToolCall{ID: droids.ToolCallID(fmt.Sprintf("call_%d", call)), Name: name, Arguments: raw}
		message.StopReason = droids.StopReasonToolUse
		message.Content = []droids.AssistantContent{call}
		return &scriptStream{final: message}
	}
	switch command {
	case "read":
		return toolCall("read", map[string]any{"path": argument})
	case "edit":
		return toolCall("edit", map[string]any{"path": argument, "edits": []map[string]string{{"oldText": "old", "newText": "new"}}})
	case "write":
		return toolCall("write", map[string]any{"path": argument, "content": "hello\n"})
	case "confirm":
		return toolCall("confirm_from_user", map[string]any{"title": "Proceed with the change?", "detail": "It edits one file."})
	case "choose":
		return toolCall("select_from_user", map[string]any{"title": "Pick a color", "options": []map[string]string{
			{"label": "Red", "value": "red"}, {"label": "Blue", "value": "blue"},
		}})
	case "think":
		message.Content = []droids.AssistantContent{droids.ThinkingContent{Thinking: "Pondering."}, droids.TextContent{Text: "Thought it over."}}
		return &scriptStream{events: []droids.StreamEvent{droids.StreamThinkingDelta{Delta: "Pondering."}, droids.StreamTextDelta{ContentIndex: 1, Delta: "Thought it over."}}, final: message}
	case "slow":
		<-ctx.Done()
		message.StopReason, message.ErrorMessage = droids.StopReasonAborted, "aborted"
		return &scriptStream{final: message, failed: true}
	case "fail":
		message.StopReason, message.ErrorKind, message.ErrorMessage = droids.StopReasonError, droids.ProviderAuthentication, "The provider rejected the credential."
		return &scriptStream{final: message, failed: true}
	}
	message.Content = []droids.AssistantContent{droids.TextContent{Text: "Hello, world."}}
	return &scriptStream{events: textEvents("Hello, ", "world."), final: message}
}

func textEvents(parts ...string) []droids.StreamEvent {
	events := make([]droids.StreamEvent, 0, len(parts))
	for _, part := range parts {
		events = append(events, droids.StreamTextDelta{Delta: part})
	}
	return events
}

func lastUserText(messages []droids.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		user, ok := messages[index].(droids.UserMessage)
		if !ok {
			continue
		}
		var text strings.Builder
		for _, content := range user.Content {
			switch block := content.(type) {
			case droids.TextInput:
				text.WriteString(block.Text)
			case droids.PromptCommandInput:
				text.WriteString(block.Text)
			}
		}
		return strings.TrimSpace(text.String())
	}
	return ""
}

type scriptStream struct {
	events []droids.StreamEvent
	final  droids.AssistantMessage
	failed bool
}

func (s *scriptStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, len(s.events)+2)
	events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: "test", Model: s.final.Model}}
	for _, event := range s.events {
		events <- event
	}
	if s.failed {
		events <- droids.StreamError{Message: s.final}
	} else {
		events <- droids.StreamDone{Message: s.final}
	}
	close(events)
	return events
}

func (s *scriptStream) Result() droids.AssistantMessage { return s.final }

var _ io.Closer = (*kit.Client)(nil)
