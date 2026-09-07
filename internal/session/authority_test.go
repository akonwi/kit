package session_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func TestManagerCreateIsIdempotentForClientSelectedSessionID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(
		store, &authorityProviders{}, "system", session.WithDroidStoreDirectory(filepath.Join(root, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	input := session.CreateInput{
		ID: "session_0123456789abcdef0123456789abcdef", CWD: root, Name: "Retry safe", Model: "test/echo",
	}
	first, err := manager.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := manager.List(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != input.ID || second.ID != first.ID || len(listed) != 1 {
		t.Fatalf("idempotent creates first=%q second=%q listed=%+v", first.ID, second.ID, listed)
	}
	conflict := input
	conflict.CWD = t.TempDir()
	if _, err := manager.Create(t.Context(), conflict); err == nil {
		t.Fatal("Create accepted a reused session id with different immutable metadata")
	}
	renamed, err := manager.Rename(t.Context(), first.ID, " Renamed session ")
	if err != nil || renamed.Name != "Renamed session" {
		t.Fatalf("Rename() = %+v, %v", renamed, err)
	}
	replayed, err := manager.Create(t.Context(), input)
	if err != nil || replayed.ID != first.ID || replayed.Name != "Renamed session" {
		t.Fatalf("Create() replay after rename = %+v, %v", replayed, err)
	}
	if _, err := manager.Rename(t.Context(), first.ID, "   "); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("empty Rename() error = %v", err)
	}
}

func TestManagerProjectsCanonicalDroidHistoryAcrossRestart(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, "system", session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.RunPrompt(t.Context(), created.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.RunID != result.TurnID || result.Status != session.RunStatusCompleted {
		t.Fatalf("result = %+v", result)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Messages) != 2 || snapshot.Messages[0].ID == "" || snapshot.Messages[0].TurnID != result.TurnID {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	reopened, err := session.NewManager(store, providers, "system", session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	afterRestart, err := reopened.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterRestart.Messages) != 2 || afterRestart.Messages[0].ID != snapshot.Messages[0].ID {
		t.Fatalf("reopened snapshot = %+v", afterRestart)
	}
}

func TestInitializedSessionFailsClosedWhenDroidStoreIsMissing(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	droidDirectory := filepath.Join(root, "droids")
	manager, err := session.NewManager(store, &authorityProviders{}, "system", session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(droidDirectory, created.ID+".db")); err != nil {
		t.Fatal(err)
	}
	reopened, err := session.NewManager(store, &authorityProviders{}, "system", session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if _, err := reopened.Snapshot(t.Context(), created.ID); !errors.Is(err, session.ErrDroidStoreMissing) {
		t.Fatalf("Snapshot error = %v, want ErrDroidStoreMissing", err)
	}
}

func TestCompletedBashBecomesPendingThenConsumedDroidBoundary(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, "system", session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	bash, err := manager.StartBash(t.Context(), created.ID, "bash_0123456789abcdef0123456789abcdef", "printf boundary", false)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for bash.Status == session.BashExecutionRunning {
		bash, err = manager.GetBash(t.Context(), created.ID, bash.ID)
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("bash did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	pending, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.Boundaries) != 1 || pending.Boundaries[0].ID != bash.ID || len(pending.Boundaries[0].Details) == 0 {
		t.Fatalf("pending boundaries = %+v", pending.Boundaries)
	}
	if _, err := manager.RunPrompt(t.Context(), created.ID, "consume boundary"); err != nil {
		t.Fatal(err)
	}
	consumed, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(consumed.Boundaries) != 0 {
		t.Fatalf("pending after prompt = %+v", consumed.Boundaries)
	}
	found := false
	for _, message := range consumed.Messages {
		if message.Role == "context" && message.BoundaryID == bash.ID && message.BoundaryKind == "bash" {
			found = len(message.Details) > 0
		}
	}
	if !found {
		t.Fatalf("consumed boundary not projected: %+v", consumed.Messages)
	}
}

func TestExcludedBashRemainsTransient(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, "system", session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	bash, err := manager.StartBash(t.Context(), created.ID, "bash_abcdef0123456789abcdef0123456789", "printf private", true)
	if err != nil {
		t.Fatal(err)
	}
	for bash.Status == session.BashExecutionRunning {
		bash, err = manager.GetBash(t.Context(), created.ID, bash.ID)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Boundaries) != 0 {
		t.Fatalf("excluded bash reached droid: %+v", snapshot.Boundaries)
	}
	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened, err := session.NewManager(store, &authorityProviders{}, "system", session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if _, err := reopened.GetBash(t.Context(), created.ID, bash.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("reopened excluded bash = %v, want transient not found", err)
	}
}

func TestWaitCancellationDetachesWithoutAbortingDroid(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, "system", session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	waitContext, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := manager.RunPrompt(waitContext, created.ID, "keep running")
		result <- err
	}()
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("RunPrompt error = %v, want detached cancellation", err)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveRunID == "" {
		t.Fatal("wait cancellation aborted the droid")
	}
	if err := manager.Abort(t.Context(), created.ID, snapshot.ActiveRunID); err != nil {
		t.Fatal(err)
	}
	close(providers.block)
}

func TestDelayedAbortCannotCancelSuccessorTurn(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, "system", session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.RunPrompt(t.Context(), created.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	providers.block = make(chan struct{})
	block := providers.block
	providers.mu.Unlock()
	second, err := manager.StartPrompt(t.Context(), created.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Abort(t.Context(), created.ID, first.TurnID); !errors.Is(err, session.ErrRunNotAbortable) {
		t.Fatalf("delayed Abort = %v", err)
	}
	run, err := manager.GetRun(t.Context(), created.ID, second.TurnID)
	if err != nil || run.Status != session.RunStatusRunning {
		t.Fatalf("successor = %+v, %v", run, err)
	}
	if err := manager.Abort(t.Context(), created.ID, second.TurnID); err != nil {
		t.Fatal(err)
	}
	close(block)
}

func TestAbortChecksDroidTurnGeneration(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{})}
	manager, err := session.NewManager(store, providers, "system", session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := manager.StartPrompt(t.Context(), created.ID, "block")
	if err != nil {
		t.Fatal(err)
	}
	activeSnapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !activeSnapshot.EventReplayAvailable || activeSnapshot.EventStreamID == "" || activeSnapshot.ActiveRunID != reservation.TurnID {
		t.Fatalf("active synchronization metadata = %+v", activeSnapshot)
	}
	if err := manager.Abort(t.Context(), created.ID, "turn_wrong"); !errors.Is(err, session.ErrRunNotAbortable) {
		t.Fatalf("wrong-generation Abort = %v", err)
	}
	if err := manager.Abort(t.Context(), created.ID, reservation.TurnID); err != nil {
		t.Fatal(err)
	}
	close(providers.block)
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := manager.GetRun(t.Context(), created.ID, reservation.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == session.RunStatusAborted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not abort: %+v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type authorityProviders struct {
	mu      sync.Mutex
	calls   int
	block   chan struct{}
	started chan struct{}
}

func (p *authorityProviders) ID() string             { return "test" }
func (p *authorityProviders) Models() []droids.Model { return []droids.Model{p.model()} }
func (p *authorityProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *authorityProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *authorityProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "echo" || id == "test/echo"
}
func (p *authorityProviders) RefreshModels(context.Context) error { return nil }
func (p *authorityProviders) Stream(ctx context.Context, _ droids.Model, _ droids.Request) droids.Stream {
	p.mu.Lock()
	p.calls++
	call := p.calls
	block := p.block
	p.mu.Unlock()
	if block != nil {
		if p.started != nil {
			select {
			case <-p.started:
			default:
				close(p.started)
			}
		}
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
	return &authorityStream{message: droids.AssistantMessage{
		Provider: "test", Model: "echo", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: fmt.Sprintf("reply %d", call)}},
	}}
}
func (*authorityProviders) model() droids.Model {
	return droids.Model{ID: "echo", Provider: "test", API: droids.ModelAPIOpenAIResponses, ContextWindow: 128_000, MaxOutputTokens: 8_192}
}

type authorityStream struct{ message droids.AssistantMessage }

func (s *authorityStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 2)
	events <- droids.StreamStart{Partial: droids.AssistantMessage{Provider: "test", Model: "echo"}}
	events <- droids.StreamDone{Message: s.message}
	close(events)
	return events
}
func (s *authorityStream) Result() droids.AssistantMessage { return s.message }
