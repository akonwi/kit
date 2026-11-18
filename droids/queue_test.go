package droids_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/droids/sqlitestore"
)

func TestQueueStartsAtOnceWhenIdleThenInOrderAfterEachTurn(t *testing.T) {
	providers := newQueueProviders()
	var started []droids.TurnID
	var startedMu sync.Mutex
	droid := spawnQueueDroid(t, "queue_order", droids.NewMemoryStore(), providers, func(id droids.TurnID) {
		startedMu.Lock()
		started = append(started, id)
		startedMu.Unlock()
	})

	first, err := droid.Enqueue(t.Context(), textInput("block first"), droids.QueueOptions{})
	if err != nil || first.Execution == nil || first.Replayed {
		t.Fatalf("first admission = %+v, %v; want a started turn", first, err)
	}
	providers.waitStarted(t, "block first")
	second, err := droid.Enqueue(t.Context(), textInput("second"), droids.QueueOptions{Metadata: json.RawMessage(`{"draft":"second"}`)})
	if err != nil || second.Execution != nil {
		t.Fatalf("second admission = %+v, %v; want it queued", second, err)
	}
	third, err := droid.Enqueue(t.Context(), textInput("third"), droids.QueueOptions{})
	if err != nil || third.Execution != nil {
		t.Fatalf("third admission = %+v, %v; want it queued", third, err)
	}

	queue, err := droid.Queue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := queuedTexts(queue); !slices.Equal(got, []string{"second", "third"}) || queue.Held || queue.Blocked != "" {
		t.Fatalf("queue = %+v (%v), want second, third, neither held nor blocked", queue, got)
	}
	if string(queue.Prompts[0].Metadata) != `{"draft":"second"}` || queue.Prompts[0].ID != second.Prompt.ID {
		t.Fatalf("first queued prompt = %+v, want second with its metadata", queue.Prompts[0])
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{})
	if err != nil || snapshot.Pending.Queued != 2 {
		t.Fatalf("snapshot pending = %+v, %v; want 2 queued", snapshot.Pending, err)
	}

	providers.release()
	waitSettled(t, droid)
	if got := providers.userTexts(); !slices.Equal(got, []string{"block first", "second", "third"}) {
		t.Fatalf("turn prompts = %v, want block first, second, third", got)
	}
	startedMu.Lock()
	defer startedMu.Unlock()
	if len(started) != 3 || started[0] != first.Execution.TurnID() {
		t.Fatalf("started turns = %v, want three beginning with %s", started, first.Execution.TurnID())
	}
	// Each turn reports the queued prompt that started it, through a handle
	// the application can look up by turn.
	for index, want := range []droids.QueuedPromptID{first.Prompt.ID, second.Prompt.ID, third.Prompt.ID} {
		handle, err := droid.Execution(t.Context(), started[index])
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := handle.Wait(t.Context())
		if err != nil || outcome.Status != droids.ExecutionCompleted || outcome.QueuedPromptID != want || outcome.FinalMessage == nil {
			t.Fatalf("turn %d outcome = %+v, %v; want completed from %s", index, outcome, err, want)
		}
		turn, err := droid.Turn(t.Context(), started[index])
		if err != nil || turn.QueuedPromptID != want {
			t.Fatalf("turn %d = %+v, %v; want queued prompt %s", index, turn, err, want)
		}
	}
	if _, err := droid.Execution(t.Context(), "turn_unknown"); !errors.Is(err, droids.ErrTurnNotFound) {
		t.Fatalf("unknown turn error = %v", err)
	}
	queue, err = droid.Queue(t.Context())
	if err != nil || len(queue.Prompts) != 0 {
		t.Fatalf("drained queue = %+v, %v", queue, err)
	}
}

func TestQueueAdmissionKeyIsIdempotentAfterThePromptRuns(t *testing.T) {
	providers := newQueueProviders()
	droid := spawnQueueDroid(t, "queue_admission", droids.NewMemoryStore(), providers, nil)
	if _, err := droid.Enqueue(t.Context(), textInput("block first"), droids.QueueOptions{}); err != nil {
		t.Fatal(err)
	}
	providers.waitStarted(t, "block first")
	options := droids.QueueOptions{AdmissionKey: "client_send_1", Metadata: json.RawMessage(`{"n":1}`)}
	admitted, err := droid.Enqueue(t.Context(), textInput("follow up"), options)
	if err != nil || admitted.Replayed {
		t.Fatalf("admission = %+v, %v", admitted, err)
	}
	replayed, err := droid.Enqueue(t.Context(), textInput("follow up"), options)
	if err != nil || !replayed.Replayed || replayed.Prompt.ID != admitted.Prompt.ID || replayed.Execution != nil {
		t.Fatalf("queued replay = %+v, %v; want prompt %s still queued", replayed, err, admitted.Prompt.ID)
	}
	if _, err := droid.Enqueue(t.Context(), textInput("something else"), options); !errors.Is(err, droids.ErrConflict) {
		t.Fatalf("reused key with other input error = %v, want conflict", err)
	}
	if _, err := droid.Enqueue(t.Context(), textInput("follow up"), droids.QueueOptions{AdmissionKey: "client_send_1"}); !errors.Is(err, droids.ErrConflict) {
		t.Fatalf("reused key with other metadata error = %v, want conflict", err)
	}

	providers.release()
	waitSettled(t, droid)
	replayed, err = droid.Enqueue(t.Context(), textInput("follow up"), options)
	if err != nil || !replayed.Replayed || replayed.Prompt.ID != admitted.Prompt.ID || replayed.Execution == nil {
		t.Fatalf("replay after start = %+v, %v; want the current turn", replayed, err)
	}
	if outcome, err := replayed.Execution.Wait(t.Context()); err != nil || outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("replayed execution outcome = %+v, %v", outcome, err)
	}
	if got := providers.userTexts(); !slices.Equal(got, []string{"block first", "follow up"}) {
		t.Fatalf("turn prompts = %v, want the follow-up to run once", got)
	}
}

func TestQueueWithdrawAndPromoteToSteering(t *testing.T) {
	providers := newQueueProviders()
	droid := spawnQueueDroid(t, "queue_promote", droids.NewMemoryStore(), providers, nil)
	if _, err := droid.Promote(t.Context()); !errors.Is(err, droids.ErrConflict) {
		t.Fatalf("idle promote error = %v, want conflict", err)
	}
	running, err := droid.Enqueue(t.Context(), textInput("block first"), droids.QueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	providers.waitStarted(t, "block first")
	var ids []droids.QueuedPromptID
	for _, text := range []string{"a", "b", "c"} {
		admission, err := droid.Enqueue(t.Context(), textInput(text), droids.QueueOptions{Metadata: json.RawMessage(`"` + text + `"`)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, admission.Prompt.ID)
	}

	withdrawn, err := droid.Withdraw(t.Context(), ids[1], "queued_00000000000000000000000000000000")
	if err != nil || len(withdrawn) != 1 || withdrawn[0].ID != ids[1] || string(withdrawn[0].Metadata) != `"b"` || promptText(withdrawn[0]) != "b" {
		t.Fatalf("withdrawn = %+v, %v; want b with its metadata", withdrawn, err)
	}
	promoted, err := droid.Promote(t.Context())
	if err != nil || len(promoted) != 2 || promoted[0].ID != ids[0] || promoted[1].ID != ids[2] {
		t.Fatalf("promoted = %+v, %v; want a, c", promoted, err)
	}
	queue, err := droid.Queue(t.Context())
	if err != nil || len(queue.Prompts) != 0 {
		t.Fatalf("queue after promote = %+v, %v", queue, err)
	}

	providers.release()
	outcome, err := running.Execution.Wait(t.Context())
	if err != nil || outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	waitSettled(t, droid)
	// The promoted prompts steer the running turn: its second model request
	// carries them, and no other turn starts.
	requests := providers.requests()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want 2", len(requests))
	}
	var steered []string
	for _, message := range requests[1].Messages {
		if user, ok := message.(droids.UserMessage); ok {
			steered = append(steered, user.Content[0].(droids.TextInput).Text)
		}
	}
	if !slices.Equal(steered, []string{"block first", "a", "c"}) {
		t.Fatalf("second request user messages = %v, want block first, a, c", steered)
	}
}

func TestQueueWaitsAfterRestartUntilStarted(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		store := droids.NewMemoryStore()
		exerciseQueueRestart(t, func() droids.Store { return store })
	})
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "queue.sqlite")
		exerciseQueueRestart(t, func() droids.Store {
			store, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store
		})
	})
}

func exerciseQueueRestart(t *testing.T, openStore func() droids.Store) {
	providers := newQueueProviders()
	firstStore := openStore()
	tracking := func(store droids.Store) droids.Config {
		return droids.Config{Store: store, Model: resolvedTestModel(providers, "test/queue"), TrackStartedPrompts: true}
	}
	first := spawnQueueDroidWith(t, "queue_restart", tracking(firstStore))
	running, err := first.Enqueue(t.Context(), textInput("block first"), droids.QueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	providers.waitStarted(t, "block first")
	options := droids.QueueOptions{AdmissionKey: "client_send_restart", Metadata: json.RawMessage(`{"kept":true}`)}
	queued, err := first.Enqueue(t.Context(), textInput("queued before restart"), options)
	if err != nil {
		t.Fatal(err)
	}
	// Prompt bodies are their own runtime records, so commits that rewrite
	// the runtime state do not rewrite them. The running turn's prompt stays
	// until the turn settles.
	persisted, err := firstStore.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, record := range persisted.RuntimeState {
		if record.Kind == "queued_prompt" {
			bodies = append(bodies, record.ID)
		}
	}
	slices.Sort(bodies)
	want := []string{string(running.Prompt.ID), string(queued.Prompt.ID)}
	slices.Sort(want)
	if !slices.Equal(bodies, want) {
		t.Fatalf("queued prompt records = %v, want %v", bodies, want)
	}
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if closer, ok := firstStore.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	second := spawnQueueDroidWith(t, "queue_restart", tracking(openStore()))
	queue, err := second.Queue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !queue.Held || len(queue.Prompts) != 1 || queue.Prompts[0].ID != queued.Prompt.ID || string(queue.Prompts[0].Metadata) != `{"kept":true}` || promptText(queue.Prompts[0]) != "queued before restart" {
		t.Fatalf("restored queue = %+v, want the held prompt with its metadata", queue)
	}
	replayed, err := second.Enqueue(t.Context(), textInput("queued before restart"), options)
	if err != nil || !replayed.Replayed || replayed.Prompt.ID != queued.Prompt.ID {
		t.Fatalf("replay after restart = %+v, %v", replayed, err)
	}
	// The interrupted turn still reports the queued prompt that started it.
	interrupted := running.Execution.TurnID()
	started, err := second.StartedPrompts(t.Context())
	if err != nil || len(started) != 1 || started[0].TurnID != interrupted || started[0].Prompt.ID != running.Prompt.ID || promptText(started[0].Prompt) != "block first" {
		t.Fatalf("started prompts after the restart = %+v, %v", started, err)
	}
	// Settling the interrupted turn leaves the held queue waiting.
	if err := second.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, second)
	if got := providers.userTexts(); !slices.Equal(got, []string{"block first"}) {
		t.Fatalf("turn prompts after abort = %v, want no queued turn", got)
	}
	// A settled turn's prompt stays until the application acknowledges it.
	if started, err := second.StartedPrompts(t.Context()); err != nil || len(started) != 1 || started[0].TurnID != interrupted {
		t.Fatalf("started prompts after the turn settled = %+v, %v", started, err)
	}
	if err := second.AcknowledgeStarted(t.Context(), running.Prompt.ID); err != nil {
		t.Fatal(err)
	}
	if started, err := second.StartedPrompts(t.Context()); err != nil || len(started) != 0 {
		t.Fatalf("started prompts after acknowledgement = %+v, %v", started, err)
	}

	handle, err := second.StartQueue(t.Context())
	if err != nil || handle == nil {
		t.Fatalf("StartQueue = %v, %v; want a started turn", handle, err)
	}
	if outcome, err := handle.Wait(t.Context()); err != nil || outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("queued outcome = %+v, %v", outcome, err)
	}
	if got := providers.userTexts(); !slices.Equal(got, []string{"block first", "queued before restart"}) {
		t.Fatalf("turn prompts = %v", got)
	}
	if handle, err := second.StartQueue(t.Context()); err != nil || handle != nil {
		t.Fatalf("StartQueue on an empty queue = %v, %v", handle, err)
	}
}

func TestQueueEnqueueReleasesAHeldQueue(t *testing.T) {
	store := droids.NewMemoryStore()
	providers := newQueueProviders()
	first := spawnQueueDroid(t, "queue_release", store, providers, nil)
	if _, err := first.Enqueue(t.Context(), textInput("block first"), droids.QueueOptions{}); err != nil {
		t.Fatal(err)
	}
	providers.waitStarted(t, "block first")
	if _, err := first.Enqueue(t.Context(), textInput("older"), droids.QueueOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	second := spawnQueueDroid(t, "queue_release", store, providers, nil)
	if err := second.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	admission, err := second.Enqueue(t.Context(), textInput("newer"), droids.QueueOptions{})
	if err != nil || admission.Execution != nil {
		t.Fatalf("admission = %+v, %v; want it queued behind the older prompt", admission, err)
	}
	waitSettled(t, second)
	if got := providers.userTexts(); !slices.Equal(got, []string{"block first", "older", "newer"}) {
		t.Fatalf("turn prompts = %v, want the older prompt first", got)
	}
}

func TestQueueHoldsAfterPolicyStop(t *testing.T) {
	providers := newQueueProviders()
	droid := spawnQueueDroid(t, "queue_policy", droids.NewMemoryStore(), providers, nil)
	running, err := droid.Enqueue(t.Context(), textInput("block policy"), droids.QueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	providers.waitStarted(t, "block policy")
	if _, err := droid.Enqueue(t.Context(), textInput("after policy"), droids.QueueOptions{}); err != nil {
		t.Fatal(err)
	}
	providers.release()
	if outcome, err := running.Execution.Wait(t.Context()); err != nil || outcome.Status != droids.ExecutionFailed {
		t.Fatalf("policy outcome = %+v, %v", outcome, err)
	}
	waitSettled(t, droid)
	queue, err := droid.Queue(t.Context())
	if err != nil || !queue.Held || len(queue.Prompts) != 1 {
		t.Fatalf("queue after policy stop = %+v, %v; want one held prompt", queue, err)
	}
	if got := providers.userTexts(); !slices.Equal(got, []string{"block policy"}) {
		t.Fatalf("turn prompts = %v", got)
	}
}

func TestQueueIsBounded(t *testing.T) {
	providers := newQueueProviders()
	droid := spawnQueueDroid(t, "queue_bounded", droids.NewMemoryStore(), providers, nil)
	if _, err := droid.Enqueue(t.Context(), textInput("block first"), droids.QueueOptions{}); err != nil {
		t.Fatal(err)
	}
	providers.waitStarted(t, "block first")
	for index := range 64 {
		if _, err := droid.Enqueue(t.Context(), textInput("queued"), droids.QueueOptions{}); err != nil {
			t.Fatalf("enqueue %d: %v", index, err)
		}
	}
	if _, err := droid.Enqueue(t.Context(), textInput("one too many"), droids.QueueOptions{}); !errors.Is(err, droids.ErrBusy) {
		t.Fatalf("over-capacity error = %v, want busy", err)
	}
	if _, err := droid.Enqueue(t.Context(), textInput("bad metadata"), droids.QueueOptions{Metadata: json.RawMessage(`{`)}); err == nil {
		t.Fatal("invalid metadata was accepted")
	}
	if _, err := droid.Withdraw(t.Context()); err != nil {
		t.Fatal(err)
	}
	providers.release()
	waitSettled(t, droid)
}

func spawnQueueDroid(t *testing.T, id droids.ConversationID, store droids.Store, providers *queueProviders, started func(droids.TurnID)) *droids.Droid {
	t.Helper()
	return spawnQueueDroidWith(t, id, droids.Config{Store: store, Model: resolvedTestModel(providers, "test/queue"), TurnStarted: started})
}

func spawnQueueDroidWith(t *testing.T, id droids.ConversationID, config droids.Config) *droids.Droid {
	t.Helper()
	droid, err := droids.Spawn(t.Context(), id, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	return droid
}

func waitSettled(t *testing.T, droid *droids.Droid) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := droid.WaitQuiescent(ctx); err != nil {
		t.Fatal(err)
	}
}

func textInput(text string) droids.Input {
	return droids.Input{Content: []droids.InputContent{droids.TextInput{Text: text}}}
}

func promptText(prompt droids.QueuedPrompt) string {
	return prompt.Input.Content[0].(droids.TextInput).Text
}

func queuedTexts(queue droids.QueueSnapshot) []string {
	texts := make([]string, 0, len(queue.Prompts))
	for _, prompt := range queue.Prompts {
		texts = append(texts, promptText(prompt))
	}
	return texts
}

// queueProviders answers each request from the last user message: text
// starting with "block" waits until release or cancellation, and text ending
// in "policy" then ends in a provider policy stop.
type queueProviders struct {
	mu        sync.Mutex
	recorded  []droids.Request
	started   chan string
	releaseCh chan struct{}
	once      sync.Once
}

func newQueueProviders() *queueProviders {
	return &queueProviders{started: make(chan string, 128), releaseCh: make(chan struct{})}
}

func (p *queueProviders) release() { p.once.Do(func() { close(p.releaseCh) }) }

func (p *queueProviders) waitStarted(t *testing.T, text string) {
	t.Helper()
	select {
	case got := <-p.started:
		if got != text {
			t.Fatalf("started %q, want %q", got, text)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%q did not start", text)
	}
}

func (p *queueProviders) requests() []droids.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.recorded)
}

// userTexts returns the newest user prompt of each turn's first request.
func (p *queueProviders) userTexts() []string {
	var texts []string
	for _, request := range p.requests() {
		text := lastUserText(request)
		if len(texts) == 0 || texts[len(texts)-1] != text {
			texts = append(texts, text)
		}
	}
	return texts
}

func lastUserText(request droids.Request) string {
	for index := len(request.Messages) - 1; index >= 0; index-- {
		if user, ok := request.Messages[index].(droids.UserMessage); ok {
			return user.Content[0].(droids.TextInput).Text
		}
	}
	return ""
}

func (p *queueProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *queueProviders) Resolve(id string) (droids.Model, error) {
	if id != "test/queue" {
		return droids.Model{}, errors.New("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), p.model())
}
func (p *queueProviders) Model(id string) (droids.Model, bool) { return p.model(), id == "test/queue" }
func (*queueProviders) RefreshModels(context.Context) error    { return nil }
func (*queueProviders) model() droids.Model {
	return droids.Model{ID: "queue", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
}

func (p *queueProviders) Stream(ctx context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.recorded = append(p.recorded, request)
	p.mu.Unlock()
	text := lastUserText(request)
	message := droids.AssistantMessage{
		Provider: "test", Model: "queue", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "done: " + text}},
	}
	if strings.HasSuffix(text, "policy") {
		message.StopReason = droids.StopReasonError
		message.Content = nil
		message.Error = &droids.ProviderError{Kind: droids.ProviderTransport, Code: droids.MisalignmentPolicyViolation}
	}
	if !strings.HasPrefix(text, "block") {
		return sdkStaticStream(message)
	}
	return &queueBlockingStream{ctx: ctx, message: message, text: text, providers: p}
}

type queueBlockingStream struct {
	ctx       context.Context
	message   droids.AssistantMessage
	text      string
	providers *queueProviders
	result    droids.AssistantMessage
	mu        sync.Mutex
}

func (s *queueBlockingStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	go func() {
		defer close(events)
		events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: "test", Model: "queue"}}
		s.providers.started <- s.text
		select {
		case <-s.providers.releaseCh:
			s.setResult(s.message)
			if s.message.StopReason == droids.StopReasonError {
				events <- droids.StreamError{Message: s.message}
			} else {
				events <- droids.StreamDone{Message: s.message}
			}
		case <-s.ctx.Done():
			aborted := droids.AssistantMessage{Provider: "test", Model: "queue", StopReason: droids.StopReasonAborted}
			s.setResult(aborted)
			events <- droids.StreamError{Message: aborted}
		}
	}()
	return events
}

func (s *queueBlockingStream) setResult(message droids.AssistantMessage) {
	s.mu.Lock()
	s.result = message
	s.mu.Unlock()
}

func (s *queueBlockingStream) Result() droids.AssistantMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result
}

func TestQueueBlocksOnAPromptThatCannotStartUntilItIsWithdrawn(t *testing.T) {
	providers := newQueueProviders()
	store := &failQueuedAdmissionStore{Store: droids.NewMemoryStore()}
	droid := spawnQueueDroid(t, "queue_blocked", store, providers, nil)
	running, err := droid.Enqueue(t.Context(), textInput("block first"), droids.QueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	providers.waitStarted(t, "block first")
	stuck, err := droid.Enqueue(t.Context(), textInput("stuck"), droids.QueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := droid.Enqueue(t.Context(), textInput("next"), droids.QueueOptions{}); err != nil {
		t.Fatal(err)
	}
	store.armed.Store(true)
	providers.release()
	if _, err := running.Execution.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, droid)
	queue, err := droid.Queue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queue.Blocked, "injected queued admission failure") || !slices.Equal(queuedTexts(queue), []string{"stuck", "next"}) {
		t.Fatalf("queue = %+v, want stuck blocking next", queue)
	}
	if handle, err := droid.StartQueue(t.Context()); err == nil || handle != nil {
		t.Fatalf("StartQueue on a blocked queue = %v, %v; want the block reported", handle, err)
	}

	withdrawn, err := droid.Withdraw(t.Context(), stuck.Prompt.ID)
	if err != nil || len(withdrawn) != 1 {
		t.Fatalf("withdrawn = %+v, %v", withdrawn, err)
	}
	waitSettled(t, droid)
	if got := providers.userTexts(); !slices.Equal(got, []string{"block first", "next"}) {
		t.Fatalf("turn prompts = %v, want next to start once stuck was withdrawn", got)
	}
	queue, err = droid.Queue(t.Context())
	if err != nil || queue.Blocked != "" || len(queue.Prompts) != 0 {
		t.Fatalf("queue = %+v, %v", queue, err)
	}
}

// failQueuedAdmissionStore fails, once armed, the commit that starts a turn
// from the queue.
type failQueuedAdmissionStore struct {
	droids.Store
	armed atomic.Bool
}

func (s *failQueuedAdmissionStore) Commit(ctx context.Context, request droids.CommitRequest) (droids.CommitResult, error) {
	for _, event := range request.Events {
		if event.Kind == "turn.admitted" && strings.Contains(string(event.Payload), "queued_id") && s.armed.CompareAndSwap(true, false) {
			return droids.CommitResult{}, errors.New("injected queued admission failure")
		}
	}
	return s.Store.Commit(ctx, request)
}

func TestQueueAdmissionsStayWithTheirConversationWhenForked(t *testing.T) {
	providers := newQueueProviders()
	source := spawnQueueDroid(t, "queue_fork_source", droids.NewMemoryStore(), providers, nil)
	options := droids.QueueOptions{AdmissionKey: "client_send_fork"}
	admitted, err := source.Enqueue(t.Context(), textInput("before the fork"), options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admitted.Execution.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	forked, err := source.Fork(t.Context(), "queue_fork_child", droids.ForkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	child := forked.Droid
	t.Cleanup(func() { _ = child.Close() })
	// The child never admitted the source's prompt, so the key admits anew.
	again, err := child.Enqueue(t.Context(), textInput("in the child"), options)
	if err != nil || again.Replayed || again.Prompt.ID == admitted.Prompt.ID || again.Execution == nil {
		t.Fatalf("child admission = %+v, %v; want a new admission", again, err)
	}
	if _, err := again.Execution.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestQueueWaitsWhileTooManyStartedPromptsAreUnacknowledged(t *testing.T) {
	providers := newQueueProviders()
	droid := spawnQueueDroidWith(t, "queue_started_bound", droids.Config{
		Store: droids.NewMemoryStore(), Model: resolvedTestModel(providers, "test/queue"), TrackStartedPrompts: true,
	})
	var admitted []droids.QueuedPromptID
	for index := range 128 {
		admission, err := droid.Enqueue(t.Context(), textInput(fmt.Sprintf("prompt %d", index)), droids.QueueOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admission.Execution.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		admitted = append(admitted, admission.Prompt.ID)
	}
	started, err := droid.StartedPrompts(t.Context())
	if err != nil || len(started) != 128 || started[0].Prompt.ID != admitted[0] {
		t.Fatalf("started prompts = %d, %v; want all 128", len(started), err)
	}
	// The next prompt waits until the application acknowledges one.
	waiting, err := droid.Enqueue(t.Context(), textInput("waiting"), droids.QueueOptions{})
	if err != nil || waiting.Execution != nil {
		t.Fatalf("admission past the bound = %+v, %v; want it queued", waiting, err)
	}
	if err := droid.AcknowledgeStarted(t.Context(), admitted[0]); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, droid)
	if got := providers.userTexts(); got[len(got)-1] != "waiting" {
		t.Fatalf("last turn prompt = %q, want waiting", got[len(got)-1])
	}
}

func TestQueueForgetsStartedPromptsWhenTheApplicationDoesNotTrackThem(t *testing.T) {
	providers := newQueueProviders()
	store := droids.NewMemoryStore()
	droid := spawnQueueDroid(t, "queue_started_untracked", store, providers, nil)
	admission, err := droid.Enqueue(t.Context(), textInput("block first"), droids.QueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	providers.waitStarted(t, "block first")
	if started, err := droid.StartedPrompts(t.Context()); err != nil || len(started) != 1 || started[0].TurnID != admission.Execution.TurnID() {
		t.Fatalf("started prompts of the running turn = %+v, %v", started, err)
	}
	providers.release()
	waitSettled(t, droid)
	if started, err := droid.StartedPrompts(t.Context()); err != nil || len(started) != 0 {
		t.Fatalf("started prompts after the turn settled = %+v, %v", started, err)
	}
	persisted, err := store.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range persisted.RuntimeState {
		if record.Kind == "queued_prompt" {
			t.Fatalf("a settled turn kept its prompt record %s", record.ID)
		}
	}
}

func TestQueueForgetsTrackedStartedPromptsWhenReopenedWithoutTracking(t *testing.T) {
	providers := newQueueProviders()
	store := droids.NewMemoryStore()
	config := droids.Config{Store: store, Model: resolvedTestModel(providers, "test/queue"), TrackStartedPrompts: true}
	first := spawnQueueDroidWith(t, "queue_started_retracked", config)
	admission, err := first.Enqueue(t.Context(), textInput("tracked"), droids.QueueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admission.Execution.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := first.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	config.TrackStartedPrompts = false
	second := spawnQueueDroidWith(t, "queue_started_retracked", config)
	if started, err := second.StartedPrompts(t.Context()); err != nil || len(started) != 0 {
		t.Fatalf("started prompts after reopening untracked = %+v, %v", started, err)
	}
	persisted, err := store.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range persisted.RuntimeState {
		if record.Kind == "queued_prompt" {
			t.Fatalf("a settled turn kept its prompt record %s", record.ID)
		}
	}
}
