package session_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func TestManagerPersistsAndResumesDroidsSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	providers := &echoProviders{}
	manager, err := kitsession.NewManager(store, providers, "You are a test agent.")
	if err != nil {
		t.Fatalf("kitsession.NewManager() error = %v", err)
	}

	workspace := t.TempDir()
	session, err := manager.Create(ctx, kitsession.CreateInput{CWD: workspace, Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	first, err := runPrompt(manager, ctx, session.ID, "hello")
	if err != nil {
		t.Fatalf("first RunPrompt() error = %v", err)
	}
	if first.Text != "reply 1" || first.Status != storage.RunStatusCompleted {
		t.Fatalf("first result = %+v", first)
	}
	manager.Close()

	resumed, err := kitsession.NewManager(store, providers, "You are a test agent.")
	if err != nil {
		t.Fatalf("resumed kitsession.NewManager() error = %v", err)
	}
	defer resumed.Close()
	second, err := runPrompt(resumed, ctx, session.ID, "again")
	if err != nil {
		t.Fatalf("second RunPrompt() error = %v", err)
	}
	if second.Text != "reply 2" || second.Status != storage.RunStatusCompleted {
		t.Fatalf("second result = %+v", second)
	}

	requests := providers.Requests()
	if len(requests) != 2 {
		t.Fatalf("provider request count = %d, want 2", len(requests))
	}
	if len(requests[1].Messages) != 3 {
		t.Fatalf("resumed request message count = %d, want 3", len(requests[1].Messages))
	}
	if text := requests[1].Messages[0].(droids.UserMessage).Content[0].(droids.TextContent).Text; text != "hello" {
		t.Fatalf("rehydrated first prompt = %q", text)
	}

	records, err := store.ListMessages(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("persisted message count = %d, want 4", len(records))
	}
	for index, want := range []string{"user", "assistant", "user", "assistant"} {
		if records[index].Role != want || records[index].Sequence != int64(index) {
			t.Errorf("message %d = role %q sequence %d, want %q/%d", index, records[index].Role, records[index].Sequence, want, index)
		}
	}
}

func TestManagerLoadsDifferentSessionsConcurrently(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	repository := &blockingReplayRepository{
		Repository: store, started: make(chan struct{}), gate: make(chan struct{}),
	}
	defer func() {
		select {
		case <-repository.gate:
		default:
			close(repository.gate)
		}
	}()
	manager, err := kitsession.NewManager(repository, &echoProviders{}, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	first, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	second, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	repository.blockedSession = first.ID

	firstDone := make(chan error, 1)
	go func() {
		_, err := runPrompt(manager, ctx, first.ID, "first")
		firstDone <- err
	}()
	select {
	case <-repository.started:
	case <-time.After(time.Second):
		t.Fatal("first session did not begin replay")
	}
	secondContext, cancelSecond := context.WithTimeout(ctx, time.Second)
	_, secondErr := runPrompt(manager, secondContext, second.ID, "second")
	cancelSecond()
	if secondErr != nil {
		close(repository.gate)
		t.Fatalf("second RunPrompt() was blocked by unrelated replay: %v", secondErr)
	}
	close(repository.gate)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RunPrompt() error = %v", err)
	}
}

func TestManagerPublishesCompletionAfterRunCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	manager, err := kitsession.NewManager(store, &echoProviders{}, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for index := range 25 {
		if _, err := runPrompt(manager, ctx, created.ID, fmt.Sprintf("prompt %d", index)); err != nil {
			t.Fatalf("RunPrompt(%d) error = %v", index, err)
		}
	}
}

func TestManagerRecoversFailedTerminalWriteWithoutRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	repository := &flakyFinishRepository{Repository: store, failNext: true}
	manager, err := kitsession.NewManager(repository, &echoProviders{}, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	first, err := runPrompt(manager, ctx, created.ID, "first")
	if err != nil {
		t.Fatalf("first RunPrompt() error = %v", err)
	}
	if first.Status != kitsession.RunStatusInterrupted {
		t.Fatalf("first outcome = %+v", first)
	}
	second, err := runPrompt(manager, ctx, created.ID, "second")
	if err != nil {
		t.Fatalf("second RunPrompt() error = %v", err)
	}
	if second.Status != kitsession.RunStatusCompleted {
		t.Fatalf("second outcome = %+v", second)
	}
}

func TestManagerRunSurvivesWaitingClientCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	gate := make(chan struct{})
	providers := &echoProviders{gate: gate}
	manager, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	waitingContext, cancelWait := context.WithCancel(context.Background())
	waitResult := make(chan error, 1)
	go func() {
		_, err := runPrompt(manager, waitingContext, created.ID, "keep going")
		waitResult <- err
	}()
	providers.WaitForRequest(t)
	cancelWait()
	if err := <-waitResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting RunPrompt() error = %v, want context.Canceled", err)
	}
	close(gate)

	deadline := time.Now().Add(2 * time.Second)
	for {
		replayed, err := store.ListReplayMessages(ctx, created.ID)
		if err != nil {
			t.Fatalf("ListReplayMessages() error = %v", err)
		}
		if len(replayed) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("replayed message count = %d, want detached run completion", len(replayed))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManagerAbortBeforeAdmissionTargetsFutureMatchingRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	providers := &echoProviders{}
	manager, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	runID := newRunID(t)
	if _, err := manager.ReservePrompt(ctx, created.ID, runID); err != nil {
		t.Fatalf("ReservePrompt() error = %v", err)
	}
	if err := manager.Abort(ctx, created.ID, runID); err != nil {
		t.Fatalf("Abort() before admission error = %v", err)
	}
	outcome, err := manager.RunPrompt(ctx, created.ID, runID, "do not run")
	if err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if outcome.Status != kitsession.RunStatusAborted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(providers.Requests()) != 0 {
		t.Fatal("pre-aborted prompt reached provider")
	}
}

func TestManagerDelayedAbortCannotCancelSuccessorRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	providers := &echoProviders{}
	manager, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	firstRunID := newRunID(t)
	if _, err := manager.ReservePrompt(ctx, created.ID, firstRunID); err != nil {
		t.Fatalf("first ReservePrompt() error = %v", err)
	}
	if _, err := manager.RunPrompt(ctx, created.ID, firstRunID, "first"); err != nil {
		t.Fatalf("first RunPrompt() error = %v", err)
	}
	gate := make(chan struct{})
	providers.SetGate(gate)
	secondRunID := newRunID(t)
	if _, err := manager.ReservePrompt(ctx, created.ID, secondRunID); err != nil {
		t.Fatalf("second ReservePrompt() error = %v", err)
	}
	type runResult struct {
		outcome kitsession.PromptResult
		err     error
	}
	secondDone := make(chan runResult, 1)
	go func() {
		outcome, err := manager.RunPrompt(ctx, created.ID, secondRunID, "second")
		secondDone <- runResult{outcome: outcome, err: err}
	}()
	providers.WaitForRequestCount(t, 2)
	if err := manager.Abort(ctx, created.ID, firstRunID); !errors.Is(err, kitsession.ErrRunNotAbortable) {
		t.Fatalf("delayed first Abort() error = %v, want ErrRunNotAbortable", err)
	}
	select {
	case result := <-secondDone:
		t.Fatalf("successor ended after stale abort: %+v, %v", result.outcome, result.err)
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	result := <-secondDone
	if result.err != nil || result.outcome.Status != kitsession.RunStatusCompleted {
		t.Fatalf("successor result = %+v, %v", result.outcome, result.err)
	}
}

func TestManagerAbortIsExplicitAndDurable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	providers := &echoProviders{gate: make(chan struct{})}
	manager, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	type runResult struct {
		outcome kitsession.PromptResult
		err     error
	}
	completed := make(chan runResult, 1)
	runID := newRunID(t)
	if _, err := manager.ReservePrompt(ctx, created.ID, runID); err != nil {
		t.Fatalf("ReservePrompt() error = %v", err)
	}
	go func() {
		outcome, err := manager.RunPrompt(ctx, created.ID, runID, "abort me")
		completed <- runResult{outcome: outcome, err: err}
	}()
	providers.WaitForRequest(t)
	if err := manager.Abort(ctx, created.ID, runID); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	select {
	case result := <-completed:
		if result.err != nil {
			t.Fatalf("RunPrompt() error = %v", result.err)
		}
		if result.outcome.Status != kitsession.RunStatusAborted {
			t.Fatalf("outcome = %+v", result.outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("aborted run did not finish")
	}
	if replay, err := store.ListReplayMessages(ctx, created.ID); err != nil || len(replay) != 0 {
		t.Fatalf("ListReplayMessages() = %+v, %v; want no aborted context", replay, err)
	}
}

func TestManagerValidatesSessionBeforePersistence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	manager, err := kitsession.NewManager(store, &echoProviders{}, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	if _, err := manager.Create(ctx, kitsession.CreateInput{CWD: ".", Model: "test/echo"}); !errors.Is(err, kitsession.ErrInvalidInput) {
		t.Fatalf("relative cwd Create() error = %v", err)
	}
	if _, err := manager.Create(ctx, kitsession.CreateInput{
		CWD: t.TempDir(), Model: "test/echo", ThinkingLevel: "high",
	}); !errors.Is(err, kitsession.ErrInvalidInput) {
		t.Fatalf("unsupported thinking Create() error = %v", err)
	}
	records, err := manager.List(ctx, "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("invalid sessions were persisted: %+v", records)
	}
}

func TestManagerRejectsConcurrentParentRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	gate := make(chan struct{})
	providers := &echoProviders{gate: gate}
	manager, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("kitsession.NewManager() error = %v", err)
	}
	defer manager.Close()
	session, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := runPrompt(manager, ctx, session.ID, "first")
		firstDone <- err
	}()
	providers.WaitForRequest(t)
	if _, err := runPrompt(manager, ctx, session.ID, "second"); err != kitsession.ErrBusy {
		t.Fatalf("concurrent RunPrompt() error = %v, want ErrBusy", err)
	}
	close(gate)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RunPrompt() error = %v", err)
	}
}

func runPrompt(
	manager *kitsession.Manager,
	ctx context.Context,
	sessionID, prompt string,
) (kitsession.PromptResult, error) {
	runID := newRunID(nil)
	if _, err := manager.ReservePrompt(ctx, sessionID, runID); err != nil {
		return kitsession.PromptResult{}, err
	}
	return manager.RunPrompt(ctx, sessionID, runID, prompt)
}

func newRunID(t testing.TB) string {
	if t != nil {
		t.Helper()
	}
	id, err := identifier.New("run_")
	if err != nil {
		if t != nil {
			t.Fatalf("identifier.New() error = %v", err)
		}
		panic(err)
	}
	return id
}

type blockingReplayRepository struct {
	kitsession.Repository
	blockedSession string
	started        chan struct{}
	gate           chan struct{}
	startedOnce    sync.Once
}

func (r *blockingReplayRepository) ListReplayMessages(
	ctx context.Context,
	sessionID string,
) ([]kitsession.MessageRecord, error) {
	if sessionID == r.blockedSession {
		r.startedOnce.Do(func() { close(r.started) })
		select {
		case <-r.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.Repository.ListReplayMessages(ctx, sessionID)
}

type flakyFinishRepository struct {
	kitsession.Repository
	mu       sync.Mutex
	failNext bool
}

func (r *flakyFinishRepository) FinishParentRun(
	ctx context.Context,
	sessionID, turnID, runID string,
	status kitsession.RunStatus,
	errorMessage string,
) error {
	r.mu.Lock()
	fail := r.failNext
	r.failNext = false
	r.mu.Unlock()
	if fail {
		return errors.New("simulated terminal write failure")
	}
	return r.Repository.FinishParentRun(ctx, sessionID, turnID, runID, status, errorMessage)
}

type echoProviders struct {
	mu       sync.Mutex
	requests []droids.Request
	gate     <-chan struct{}
	received chan struct{}
}

func (p *echoProviders) Models() []droids.Model {
	return []droids.Model{p.model()}
}

func (p *echoProviders) Model(id string) (droids.Model, bool) {
	if id == "echo" || id == "test/echo" {
		return p.model(), true
	}
	return droids.Model{}, false
}

func (p *echoProviders) RefreshModels(context.Context) error { return nil }

func (p *echoProviders) Stream(ctx context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	count := len(p.requests)
	gate := p.gate
	if p.received != nil {
		close(p.received)
		p.received = nil
	}
	p.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return newEchoErrorStream(ctx.Err())
		}
	}
	text := fmt.Sprintf("reply %d", count)
	final := droids.AssistantMessage{
		Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
		Content:   []droids.Content{droids.TextContent{Text: text}},
		Timestamp: time.Now().UnixMilli(),
	}
	events := []droids.StreamEvent{
		droids.StreamStart{Partial: droids.AssistantMessage{Provider: "test", Model: "echo"}},
		droids.StreamTextStart{ContentIndex: 0},
		droids.StreamTextDelta{ContentIndex: 0, Delta: text},
		droids.StreamTextEnd{ContentIndex: 0, Text: text},
		droids.StreamDone{Message: final},
	}
	return &echoStream{events: events, final: final}
}

func (p *echoProviders) model() droids.Model {
	return droids.Model{
		ID: "echo", Name: "Echo", Provider: "test",
		API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192,
	}
}

func (p *echoProviders) SetGate(gate <-chan struct{}) {
	p.mu.Lock()
	p.gate = gate
	p.mu.Unlock()
}

func (p *echoProviders) Requests() []droids.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]droids.Request(nil), p.requests...)
}

func (p *echoProviders) WaitForRequestCount(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		p.mu.Lock()
		current := len(p.requests)
		p.mu.Unlock()
		if current >= count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider request count = %d, want at least %d", current, count)
		}
		time.Sleep(time.Millisecond)
	}
}

func (p *echoProviders) WaitForRequest(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	if len(p.requests) > 0 {
		p.mu.Unlock()
		return
	}
	if p.received == nil {
		p.received = make(chan struct{})
	}
	received := p.received
	p.mu.Unlock()
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("provider did not receive request")
	}
}

type echoStream struct {
	events []droids.StreamEvent
	final  droids.AssistantMessage
}

func (s *echoStream) Events() <-chan droids.StreamEvent {
	channel := make(chan droids.StreamEvent, len(s.events))
	for _, event := range s.events {
		channel <- event
	}
	close(channel)
	return channel
}

func (s *echoStream) Result() droids.AssistantMessage { return s.final }

func newEchoErrorStream(err error) droids.Stream {
	final := droids.AssistantMessage{
		Provider: "test", Model: "echo", StopReason: droids.StopReasonAborted,
		ErrorMessage: err.Error(), Timestamp: time.Now().UnixMilli(),
	}
	return &echoStream{
		events: []droids.StreamEvent{droids.StreamError{Message: final}},
		final:  final,
	}
}
