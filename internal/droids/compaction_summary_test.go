package droids

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestCompactionSummaryTextProjection(t *testing.T) {
	memory := "Preserve previous decisions. " + strings.Repeat("memory ", 1_000)
	messages := []Message{
		ContextMessage{Kind: "summary", Source: "compaction", Content: []InputContent{TextInput{Text: "[context summary]\n" + memory}}},
		UserMessage{Content: []InputContent{
			TextInput{Text: "Fix the parser"},
			AnnotationInput{Text: "main.go:12: keep this check"},
			FileInput{Filename: "screen.png", MediaType: "image/png", URL: "data:image/png;base64,aW1hZ2U="},
		}},
		AssistantMessage{Provider: "old-provider", Model: "old-model", ResponseID: "opaque-response", ProviderScope: "opaque-scope", StopReason: StopReasonToolUse, Content: []AssistantContent{
			ThinkingContent{Thinking: "private reasoning", Signature: "opaque-thinking"},
			TextContent{Text: "Inspecting the parser", Signature: "opaque-text"},
			ToolCall{ID: "call_read", ProviderCallID: "provider-call", Name: "read", Arguments: []byte(`{"path":"main.go"}`), Signature: "opaque-call"},
		}},
		ToolResultMessage{ToolCallID: "call_read", ToolName: "read", IsError: true, Content: []ResultContent{TextContent{Text: strings.Repeat("omitted tool output", 1_000)}}},
		ContextMessage{Kind: "notice", Source: "child", Content: []InputContent{TextInput{Text: "A child completed its review"}}},
	}
	before := cloneMessages(messages)
	previous, parts := serializeCompaction(messages)
	if previous != memory {
		t.Fatalf("previous summary was changed: length %d, want %d", len(previous), len(memory))
	}
	want := []string{
		"[User]\nFix the parser\nmain.go:12: keep this check\n[Attachment: screen.png (image/png)]",
		"[Assistant]\nInspecting the parser\n\n[Assistant tool call]\nread({\"path\":\"main.go\"})\n\n[Tool result: read; error; output omitted]",
		"[Context: notice / child]\nA child completed its review",
	}
	if !reflect.DeepEqual(parts, want) {
		t.Fatalf("serialized transcript = %#v, want %#v", parts, want)
	}
	if !reflect.DeepEqual(messages, before) {
		t.Fatal("summary serialization mutated the source messages")
	}

	request := compactionSummaryRequest("session", "custom summary instructions", previous, parts, 123)
	wantText := "<conversation>\n" + strings.Join(want, "\n\n") + "\n</conversation>\n\n<previous-summary>\n" + memory + "\n</previous-summary>\n\n" + compactionUpdateInstructions + compactionSummaryInstructions
	wantRequest := Request{
		SessionID: "session", SystemPrompt: "custom summary instructions", MaxTokens: 123,
		Messages: []Message{UserMessage{Content: []InputContent{TextInput{Text: wantText}}}},
	}
	if !reflect.DeepEqual(request, wantRequest) {
		t.Fatalf("request = %+v, want a standalone text-only user request", request)
	}
}

func TestCompactionRecognizesOnlyItsOwnCheckpointSummaries(t *testing.T) {
	messages := []Message{
		ContextMessage{Kind: "summary", Source: "external", Content: []InputContent{TextInput{Text: "external summary"}}},
		ContextMessage{BoundaryID: "external_boundary", Kind: "summary", Source: "compaction", Content: []InputContent{TextInput{Text: "boundary summary"}}},
	}
	previous, parts := serializeCompaction(messages)
	if previous != "" || !reflect.DeepEqual(parts, []string{"[Context: summary / external]\nexternal summary", "[Context: summary / compaction]\nboundary summary"}) {
		t.Fatalf("external context = %q, %#v", previous, parts)
	}
}

func TestCompactionSummaryFoldsBoundedChunksAndAccountsEveryResponse(t *testing.T) {
	provider := newCompactionTestProvider(1_000)
	droid := spawnCompactionTestDroid(t, provider, nil)
	messages := []Message{
		ContextMessage{Kind: "summary", Source: "compaction", Content: []InputContent{TextInput{Text: "prior memory"}}},
	}
	for i := range 6 {
		messages = append(messages, UserMessage{Content: []InputContent{TextInput{Text: fmt.Sprintf("chunk-%d ", i) + strings.Repeat("x", 500)}}})
	}
	result, err := droid.sdk.summarizeCompaction(t.Context(), provider, droid.model, messages, "")
	if err != nil {
		t.Fatal(err)
	}
	requests := provider.summaryRequests()
	if len(requests) < 2 {
		t.Fatalf("summary requests = %d, want bounded incremental requests", len(requests))
	}
	var transcript strings.Builder
	for index, request := range requests {
		if !compactionRequestFits(droid.model, request, 64) {
			t.Fatalf("request %d exceeds summary model capacity", index)
		}
		text := compactionRequestText(t, request)
		memory := "prior memory"
		if index > 0 {
			memory = "checkpoint memory"
		}
		if !strings.Contains(text, "<previous-summary>\n"+memory+"\n</previous-summary>") {
			t.Fatalf("request %d lost prior memory", index)
		}
		transcript.WriteString(text)
	}
	for i := range 6 {
		if count := strings.Count(transcript.String(), fmt.Sprintf("chunk-%d ", i)); count != 1 {
			t.Fatalf("chunk %d included %d times, want once", i, count)
		}
	}
	if result != "checkpoint memory" {
		t.Fatalf("summary = %q", result)
	}
	if got := droid.sdk.state.SessionUsage.TotalTokens; got != len(requests)*5 {
		t.Fatalf("usage = %d, want %d", got, len(requests)*5)
	}
	if droid.sdk.state.CheckpointID != "" || len(droid.sdk.state.Context) != 0 {
		t.Fatal("intermediate summarization installed active context")
	}
}

func TestCompactionSummaryChunkFailurePreservesContextAndObservedUsage(t *testing.T) {
	provider := newCompactionTestProvider(1_000)
	provider.summaryResponse = func(call int) AssistantMessage {
		message := provider.summaryMessage()
		if call == 2 {
			message.StopReason = StopReasonLength
		}
		return message
	}
	droid := spawnCompactionTestDroid(t, provider, nil)
	messages := make([]Message, 6)
	for i := range messages {
		messages[i] = UserMessage{Content: []InputContent{TextInput{Text: strings.Repeat("x", 500)}}}
	}
	if _, err := droid.sdk.summarizeCompaction(t.Context(), provider, droid.model, messages, ""); err == nil {
		t.Fatal("accepted an incomplete intermediate summary")
	}
	if got := len(provider.summaryRequests()); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	if droid.sdk.state.SessionUsage.TotalTokens != 10 || droid.sdk.state.CheckpointID != "" || len(droid.sdk.state.Context) != 0 {
		t.Fatalf("state after chunk failure = %+v", droid.sdk.state)
	}
}

func TestCompactionSummaryRejectsOversizedSourceWithoutTruncating(t *testing.T) {
	provider := newCompactionTestProvider(1_000)
	droid := spawnCompactionTestDroid(t, provider, nil)
	messages := []Message{UserMessage{Content: []InputContent{TextInput{Text: strings.Repeat("x", 4_000)}}}}
	if _, err := droid.sdk.summarizeCompaction(t.Context(), provider, droid.model, messages, ""); err == nil {
		t.Fatal("accepted an oversized source message")
	}
	if got := len(provider.summaryRequests()); got != 0 {
		t.Fatalf("oversized request reached provider %d times", got)
	}
}

func TestCompactionSummaryUsesModelInputLimitNotAnArbitraryCap(t *testing.T) {
	for _, inputLimit := range []int{0, 100_000} {
		t.Run(fmt.Sprintf("input_limit=%d", inputLimit), func(t *testing.T) {
			provider := newCompactionTestProvider(1_000_000)
			provider.model.MaxInputTokens = inputLimit
			droid := spawnCompactionTestDroid(t, provider, nil)
			text := strings.Repeat("source text ", 20_000)
			_, err := droid.sdk.summarizeCompaction(t.Context(), provider, droid.model, []Message{UserMessage{Content: []InputContent{TextInput{Text: text}}}}, "")
			requests := provider.summaryRequests()
			if inputLimit > 0 {
				if !errors.Is(err, ErrContextNotAdaptable) || len(requests) != 0 {
					t.Fatalf("oversized single message: requests=%d, error=%v", len(requests), err)
				}
				return
			}
			if err != nil || len(requests) != 1 {
				t.Fatalf("large model: requests=%d, error=%v", len(requests), err)
			}
			if got := estimateRequestTokens(requests[0]); got <= 100_000 {
				t.Fatalf("summary input = %d, want over 100K", got)
			}
			want := "<conversation>\n[User]\n" + text + "\n</conversation>\n\n" + compactionSummaryInstructions
			if got := compactionRequestText(t, requests[0]); got != want {
				t.Fatal("large model did not receive the complete source message")
			}
		})
	}
}

func TestCompactionSummaryProviderControlledOutputStillReservesCapacity(t *testing.T) {
	provider := newCompactionTestProvider(1_000)
	provider.model.OutputLimitMode = OutputLimitProviderControlled
	droid := spawnCompactionTestDroid(t, provider, nil)
	// Fill all but the reserved output with a single serialized message.
	empty := compactionSummaryRequest("session", defaultCompactionPrompt, "", []string{"[User]\n"}, 0)
	textBytes := 2 * (provider.model.ContextWindow - estimateRequestTokens(empty))
	messages := []Message{UserMessage{Content: []InputContent{TextInput{Text: strings.Repeat("x", textBytes)}}}}
	if _, err := droid.sdk.summarizeCompaction(t.Context(), provider, droid.model, messages, ""); err == nil {
		t.Fatal("provider-controlled summary used its output reservation for input")
	}
	messages = []Message{UserMessage{Content: []InputContent{TextInput{Text: "short input"}}}}
	if _, err := droid.sdk.summarizeCompaction(t.Context(), provider, droid.model, messages, ""); err != nil {
		t.Fatal(err)
	}
	requests := provider.summaryRequests()
	if len(requests) != 1 || requests[0].MaxTokens != 0 {
		t.Fatalf("provider-controlled requests = %+v", requests)
	}
}

func compactionRequestText(t *testing.T, request Request) string {
	t.Helper()
	if len(request.Messages) != 1 || len(request.Tools) != 0 || request.Reasoning != "" {
		t.Fatalf("not a standalone summary request: %+v", request)
	}
	user, ok := request.Messages[0].(UserMessage)
	if !ok || len(user.Content) != 1 {
		t.Fatalf("summary message = %#v", request.Messages[0])
	}
	text, ok := user.Content[0].(TextInput)
	if !ok {
		t.Fatalf("summary content = %#v", user.Content)
	}
	return text.Text
}

type compactionTestProvider struct {
	model           Model
	mu              sync.Mutex
	requests        []Request
	summaryCount    int
	normalCount     int
	summaryResponse func(int) AssistantMessage
	normalResponse  func(int) AssistantMessage
}

func newCompactionTestProvider(window int) *compactionTestProvider {
	return &compactionTestProvider{model: Model{
		Provider: "test", ID: "checkpoint", API: ModelAPIOpenAIResponses,
		ContextWindow: window, MaxOutputTokens: 64,
	}}
}

func (*compactionTestProvider) ID() string        { return "test" }
func (p *compactionTestProvider) Models() []Model { return []Model{p.model} }
func (*compactionTestProvider) ValidateReplay(_ context.Context, _ Model, messages []Message) error {
	return validateMessageSequence(messages)
}
func (p *compactionTestProvider) Stream(_ context.Context, _ Model, request Request) (AssistantStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	request.Messages = cloneMessages(request.Messages)
	p.requests = append(p.requests, request)
	message := AssistantMessage{
		Provider: p.ID(), Model: p.model.ID, StopReason: StopReasonStop,
		Content: []AssistantContent{TextContent{Text: "continued"}},
	}
	if request.SystemPrompt != "normal system" {
		p.summaryCount++
		message = p.summaryMessage()
		if p.summaryResponse != nil {
			message = p.summaryResponse(p.summaryCount)
		}
	} else {
		p.normalCount++
		if p.normalResponse != nil {
			message = p.normalResponse(p.normalCount)
		}
	}
	return newForkTestStream(message), nil
}
func (p *compactionTestProvider) summaryMessage() AssistantMessage {
	return AssistantMessage{
		Provider: p.ID(), Model: p.model.ID, StopReason: StopReasonStop,
		Content: []AssistantContent{TextContent{Text: "checkpoint memory"}},
		Usage:   Usage{Input: 3, Output: 2, TotalTokens: 5},
	}
}
func (p *compactionTestProvider) summaryRequests() []Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	var requests []Request
	for _, request := range p.requests {
		if request.SystemPrompt != "normal system" {
			requests = append(requests, request)
		}
	}
	return requests
}
func spawnCompactionTestDroid(t *testing.T, provider *compactionTestProvider, store Store) *Droid {
	t.Helper()
	model, err := BindModel(provider, provider.model)
	if err != nil {
		t.Fatal(err)
	}
	droid, err := Spawn(t.Context(), "conversation_compaction_pipeline", Config{Model: model, Store: store, SystemPrompt: "normal system"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	return droid
}
