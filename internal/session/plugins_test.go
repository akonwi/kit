package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
)

type mockPluginHost struct {
	mu                       sync.Mutex
	started, closed, reloads int
	cwds, names              []string
	warnings                 []string
	turnStarted              []string
	turnCompleted            []session.PluginTurn
	turnEvents               []string
	toastObserver            func(context.Context, session.PluginToast) error
	closeToast               *session.PluginToast
	subagents                []session.PluginSubagent
	tools                    []session.PluginTool
}

func (h *mockPluginHost) Start() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed == 0 {
		h.started++
	}
}
func (h *mockPluginHost) ChangeCWD(cwd string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cwds = append(h.cwds, cwd)
}
func (h *mockPluginHost) Rename(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.names = append(h.names, name)
}
func (h *mockPluginHost) Reload() { h.mu.Lock(); defer h.mu.Unlock(); h.reloads++ }
func (h *mockPluginHost) TurnStarted(turnID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.turnStarted = append(h.turnStarted, turnID)
	h.turnEvents = append(h.turnEvents, "started:"+turnID)
	return true
}
func (h *mockPluginHost) TurnCompleted(turn session.PluginTurn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.turnCompleted = append(h.turnCompleted, turn)
	h.turnEvents = append(h.turnEvents, "completed:"+turn.ID)
}
func (h *mockPluginHost) Close(ctx context.Context) error {
	h.mu.Lock()
	h.closed++
	observer := h.toastObserver
	toast := h.closeToast
	h.mu.Unlock()
	if observer != nil && toast != nil {
		return observer(ctx, *toast)
	}
	return nil
}
func (h *mockPluginHost) SetToastObserver(observer func(context.Context, session.PluginToast) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toastObserver = observer
}
func (h *mockPluginHost) Tools() []session.PluginTool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]session.PluginTool(nil), h.tools...)
}
func (h *mockPluginHost) ExecuteTool(context.Context, session.PluginTool, string, json.RawMessage) (session.PluginToolResult, error) {
	return session.PluginToolResult{}, errors.New("not implemented")
}
func (h *mockPluginHost) Subagents() []session.PluginSubagent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]session.PluginSubagent(nil), h.subagents...)
}
func (h *mockPluginHost) Warnings() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.warnings...)
}

func pluginTestManager(t *testing.T, factory session.PluginHostFactory) *session.Manager {
	return pluginTestManagerWithProviders(t, &authorityProviders{}, factory)
}

func pluginTestManagerWithProviders(t *testing.T, providers droids.Providers, factory session.PluginHostFactory, extra ...session.ManagerOption) *session.Manager {
	t.Helper()
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	options := []session.ManagerOption{session.WithDroidStoreDirectory(filepath.Join(root, "droids")), session.WithPluginHostFactory(factory)}
	options = append(options, extra...)
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

type testPluginSubagentRegistry struct {
	mu       sync.Mutex
	provider func() (subagent.Catalog, error)
}

func (r *testPluginSubagentRegistry) RegisterPluginCatalogProvider(_ string, provider func() (subagent.Catalog, error)) (func(), error) {
	r.mu.Lock()
	r.provider = provider
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		if reflect.ValueOf(r.provider).Pointer() == reflect.ValueOf(provider).Pointer() {
			r.provider = nil
		}
		r.mu.Unlock()
	}, nil
}
func (r *testPluginSubagentRegistry) catalog() (subagent.Catalog, error) {
	r.mu.Lock()
	provider := r.provider
	r.mu.Unlock()
	if provider == nil {
		return subagent.NewCatalog()
	}
	return provider()
}

func TestPluginSubagentsPublishIntoEffectiveSessionCatalog(t *testing.T) {
	host := &mockPluginHost{}
	providers := &authorityProviders{}
	registry := &testPluginSubagentRegistry{}
	var changed func()
	manager := pluginTestManagerWithProviders(t, providers, func(_ context.Context, _ session.PluginSession, notify func()) session.PluginHost {
		changed = notify
		return host
	}, session.WithPluginSubagentCatalogRegistry(registry))
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	host.subagents = []session.PluginSubagent{{ID: "demo.reviewer", PluginID: "demo", Instance: "owner:1", Registration: 1, Description: "Reviews", Instructions: "Review changes", Model: "test/echo", SourcePath: "/plugin.json"}}
	host.mu.Unlock()
	changed()
	var snapshot session.Snapshot
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = manager.Snapshot(t.Context(), record.ID)
		if err == nil && len(snapshot.SubagentDefinitions) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(snapshot.SubagentDefinitions) != 1 || snapshot.SubagentDefinitions[0].Name != "demo.reviewer" || snapshot.SubagentDefinitions[0].Source.Kind != "plugin" || snapshot.SubagentDefinitions[0].Source.PluginID != "demo" {
		t.Fatalf("plugin subagent snapshot = %#v", snapshot.SubagentDefinitions)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "use the reviewer"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	promptWithPlugin := providers.requests[len(providers.requests)-1].SystemPrompt
	providers.mu.Unlock()
	if !strings.Contains(promptWithPlugin, "<name>demo.reviewer</name>") || !strings.Contains(promptWithPlugin, "<description>Reviews</description>") {
		t.Fatalf("model prompt omitted plugin subagent:\n%s", promptWithPlugin)
	}
	applied, err := registry.catalog()
	if err != nil || len(applied.Definitions()) != 1 || applied.Definitions()[0].Name != "demo.reviewer" {
		t.Fatalf("applied tool catalog = %#v, %v", applied.Definitions(), err)
	}
	// The same process generation may unregister and re-register one canonical
	// name. Its new registration cannot resurrect the previously applied body.
	host.mu.Lock()
	host.subagents = []session.PluginSubagent{{ID: "demo.reviewer", PluginID: "demo", Instance: "owner:1", Registration: 2, Description: "Replacement", Instructions: "New instructions", SourcePath: "/plugin.json"}}
	host.mu.Unlock()
	applied, err = registry.catalog()
	if err != nil || len(applied.Definitions()) != 0 {
		t.Fatalf("unapplied replacement exposed stale definition: %#v, %v", applied.Definitions(), err)
	}
	changed()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = manager.Snapshot(t.Context(), record.ID)
		if err == nil && len(snapshot.SubagentDefinitions) == 1 && snapshot.SubagentDefinitions[0].Description == "Replacement" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(snapshot.SubagentDefinitions) != 1 || snapshot.SubagentDefinitions[0].Description != "Replacement" {
		t.Fatalf("replacement definition was not applied: %#v", snapshot.SubagentDefinitions)
	}
	// Force publication failure through a model-name collision, then restore
	// the previous desired fingerprint. Recovery must reapply it rather than
	// incorrectly treating the old successful fingerprint as current.
	host.mu.Lock()
	host.tools = []session.PluginTool{{ID: "demo.bad", Instance: "owner:1:1", ModelName: "bash", Description: "conflict", ExecutionMode: "sequential", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	host.mu.Unlock()
	changed()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = manager.Snapshot(t.Context(), record.ID)
		if err == nil && len(snapshot.SubagentDefinitions) == 0 && len(snapshot.Warnings) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(snapshot.SubagentDefinitions) != 0 || len(snapshot.Warnings) == 0 {
		t.Fatalf("failed publication remained advertised: definitions=%#v warnings=%#v", snapshot.SubagentDefinitions, snapshot.Warnings)
	}
	host.mu.Lock()
	host.tools = nil
	host.mu.Unlock()
	changed()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = manager.Snapshot(t.Context(), record.ID)
		if err == nil && len(snapshot.SubagentDefinitions) == 1 && len(snapshot.Warnings) == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(snapshot.SubagentDefinitions) != 1 || len(snapshot.Warnings) != 0 {
		t.Fatalf("restored fingerprint was not republished: definitions=%#v warnings=%#v", snapshot.SubagentDefinitions, snapshot.Warnings)
	}
	host.mu.Lock()
	host.subagents = nil
	host.mu.Unlock()
	// Dispatch consults current generation activity even before asynchronous
	// prompt/snapshot publication catches up.
	applied, err = registry.catalog()
	if err != nil || len(applied.Definitions()) != 0 {
		t.Fatalf("revoked tool catalog = %#v, %v", applied.Definitions(), err)
	}
	changed()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = manager.Snapshot(t.Context(), record.ID)
		if err == nil && len(snapshot.SubagentDefinitions) == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(snapshot.SubagentDefinitions) != 0 {
		t.Fatalf("unregistered plugin subagent survived = %#v", snapshot.SubagentDefinitions)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "continue"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	promptWithoutPlugin := providers.requests[len(providers.requests)-1].SystemPrompt
	providers.mu.Unlock()
	if promptWithoutPlugin == promptWithPlugin || strings.Contains(promptWithoutPlugin, "demo.reviewer") {
		t.Fatalf("unregistered subagent remained in model prompt:\n%s", promptWithoutPlugin)
	}
}

func TestPluginNotificationSubscribersRemainOpenThroughHostCleanup(t *testing.T) {
	expected := session.PluginToast{PluginID: "demo", Instance: "owner:1", Title: "Plugin failed", Variant: "error", Persistent: true}
	host := &mockPluginHost{closeToast: &expected}
	manager := pluginTestManager(t, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	toasts, unsubscribe, err := manager.SubscribePluginToasts(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if err := manager.Delete(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if toast, ok := <-toasts; !ok || toast != expected {
		t.Fatalf("cleanup toast = %#v, open = %v", toast, ok)
	}
	if _, ok := <-toasts; ok {
		t.Fatal("notification subscription remained open after cleanup")
	}
}

func TestSessionPluginHostLoadsOnceAndFollowsRuntimeTransitions(t *testing.T) {
	host := &mockPluginHost{warnings: []string{"Plugins: persistent test diagnostic"}}
	var mu sync.Mutex
	var calls []session.PluginSession
	var changed func()
	manager := pluginTestManager(t, func(ctx context.Context, input session.PluginSession, notify func()) session.PluginHost {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, input)
		changed = notify
		return host
	})
	cwd := t.TempDir()
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/echo", Name: "original"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	count := len(calls)
	mu.Unlock()
	if count != 0 {
		t.Fatal("unloaded session constructed host")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	mu.Lock()
	count = len(calls)
	input := calls[0]
	notify := changed
	mu.Unlock()
	if count != 1 || input.ID != record.ID || input.CWD != cwd || input.Name != "original" {
		t.Fatalf("host constructions = %d, %#v", count, input)
	}
	host.mu.Lock()
	started := host.started
	host.mu.Unlock()
	if started != 1 {
		t.Fatalf("host starts = %d", started)
	}
	// Turns are accepted with no plugin-readiness handshake on the session port.
	result, err := manager.RunPrompt(t.Context(), record.ID, "first turn")
	if err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	startedTurns := append([]string(nil), host.turnStarted...)
	completedTurns := append([]session.PluginTurn(nil), host.turnCompleted...)
	host.mu.Unlock()
	wantTurn := session.PluginTurn{ID: result.TurnID, Messages: []session.PluginTurnMessage{{Role: "user", Content: []string{"first turn"}}, {Role: "assistant", Content: []string{"reply 1"}}}}
	if !reflect.DeepEqual(startedTurns, []string{result.TurnID}) || !reflect.DeepEqual(completedTurns, []session.PluginTurn{wantTurn}) {
		t.Fatalf("turn events = started %#v, completed %#v", startedTurns, completedTurns)
	}
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.Warnings, host.warnings) {
		t.Fatalf("warnings = %v", snapshot.Warnings)
	}
	notify()
	refreshed, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.EventStreamID == snapshot.EventStreamID {
		t.Fatal("plugin diagnostics did not invalidate snapshot stream")
	}
	second := t.TempDir()
	if _, err := manager.ChangeCWD(t.Context(), record.ID, second); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Rename(t.Context(), record.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReloadSession(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	cwds := append([]string(nil), host.cwds...)
	names := append([]string(nil), host.names...)
	reloads := host.reloads
	host.mu.Unlock()
	if !reflect.DeepEqual(cwds, []string{second}) || !reflect.DeepEqual(names, []string{"renamed"}) || reloads != 1 {
		t.Fatalf("transitions: %v, %v, %d", cwds, names, reloads)
	}
	if err := manager.Delete(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	closed := host.closed
	host.mu.Unlock()
	if closed != 1 {
		t.Fatalf("delete closed host %d times", closed)
	}
}

func TestSessionPluginTurnEventsOrderQueuedTurns(t *testing.T) {
	block := make(chan struct{})
	providerStarted := make(chan struct{})
	providers := &authorityProviders{block: block, started: providerStarted}
	host := &mockPluginHost{}
	manager := pluginTestManagerWithProviders(t, providers, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.StartPrompt(t.Context(), record.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	submission, err := manager.SubmitPrompt(t.Context(), record.ID, "second")
	if err != nil || !submission.Queued {
		t.Fatalf("queue second = %#v, %v", submission, err)
	}
	close(block)
	deadline := time.Now().Add(5 * time.Second)
	for {
		host.mu.Lock()
		events := append([]string(nil), host.turnEvents...)
		started := append([]string(nil), host.turnStarted...)
		host.mu.Unlock()
		if len(events) == 4 && len(started) == 2 {
			want := []string{"started:" + first.TurnID, "completed:" + first.TurnID, "started:" + started[1], "completed:" + started[1]}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("queued turn events = %#v, want %#v", events, want)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued turn events did not settle: %#v", events)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSessionPluginTurnCompletionIsExactlyOnceAfterAbort(t *testing.T) {
	providerStarted := make(chan struct{})
	providers := &authorityProviders{block: make(chan struct{}), started: providerStarted}
	host := &mockPluginHost{}
	manager := pluginTestManagerWithProviders(t, providers, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	reservation, err := manager.StartPrompt(t.Context(), record.ID, "cancel this turn")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	host.mu.Lock()
	started := append([]string(nil), host.turnStarted...)
	host.mu.Unlock()
	if !reflect.DeepEqual(started, []string{reservation.TurnID}) {
		t.Fatalf("started before abort = %#v", started)
	}
	if err := manager.Abort(t.Context(), record.ID, reservation.RunID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := manager.GetRun(t.Context(), record.ID, reservation.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != session.RunStatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("aborted turn did not settle")
		}
		time.Sleep(time.Millisecond)
	}
	for {
		host.mu.Lock()
		completed := append([]session.PluginTurn(nil), host.turnCompleted...)
		started = append([]string(nil), host.turnStarted...)
		host.mu.Unlock()
		if len(completed) == 1 {
			if len(started) != 1 || completed[0].ID != reservation.TurnID {
				t.Fatalf("aborted turn events = started %#v, completed %#v", started, completed)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("aborted turn completion did not settle: %#v", completed)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSessionPluginTurnCompletionIsExactlyOnceAfterFailure(t *testing.T) {
	host := &mockPluginHost{}
	manager := pluginTestManagerWithProviders(t, &authorityProviders{failure: true}, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.RunPrompt(t.Context(), record.ID, "fail this turn")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != session.RunStatusFailed {
		t.Fatalf("result = %#v", result)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.turnStarted) != 1 || len(host.turnCompleted) != 1 || host.turnStarted[0] != result.TurnID || host.turnCompleted[0].ID != result.TurnID {
		t.Fatalf("failed turn events = started %#v, completed %#v", host.turnStarted, host.turnCompleted)
	}
}

func TestRecoveredInterruptedTurnDoesNotReplayPluginEvents(t *testing.T) {
	root := t.TempDir()
	repository, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	droidDirectory := filepath.Join(root, "droids")
	firstHost := &mockPluginHost{}
	manager, err := session.NewManager(repository, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory), session.WithPluginHostFactory(func(context.Context, session.PluginSession, func()) session.PluginHost { return firstHost }))
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := manager.StartPrompt(t.Context(), record.ID, "interrupted before restart")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	if err := manager.Shutdown(shutdownCtx); err != nil {
		cancelShutdown()
		t.Fatal(err)
	}
	cancelShutdown()

	providers.mu.Lock()
	providers.block = nil
	providers.started = nil
	providers.mu.Unlock()
	secondHost := &mockPluginHost{}
	reopened, err := session.NewManager(repository, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory), session.WithPluginHostFactory(func(context.Context, session.PluginSession, func()) session.PluginHost { return secondHost }))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := reopened.GetRun(t.Context(), record.ID, reservation.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != session.RunStatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recovered turn did not settle")
		}
		time.Sleep(time.Millisecond)
	}
	secondHost.mu.Lock()
	replayed := append([]string(nil), secondHost.turnEvents...)
	secondHost.mu.Unlock()
	if len(replayed) != 0 {
		t.Fatalf("recovered turn replayed plugin events: %#v", replayed)
	}
	result, err := reopened.RunPrompt(t.Context(), record.ID, "new turn after recovery")
	if err != nil {
		t.Fatal(err)
	}
	secondHost.mu.Lock()
	events := append([]string(nil), secondHost.turnEvents...)
	secondHost.mu.Unlock()
	want := []string{"started:" + result.TurnID, "completed:" + result.TurnID}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("new turn events = %#v, want %#v", events, want)
	}
}

func TestSessionPluginFactoryMayReturnNil(t *testing.T) {
	manager := pluginTestManager(t, func(context.Context, session.PluginSession, func()) session.PluginHost { return nil })
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "no plugin host"); err != nil {
		t.Fatal(err)
	}
}

func TestSessionPluginHostRejectedLoadNeverStarts(t *testing.T) {
	constructed := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	host := &mockPluginHost{}
	var owner context.Context
	manager := pluginTestManager(t, func(ctx context.Context, input session.PluginSession, changed func()) session.PluginHost {
		owner = ctx
		close(constructed)
		<-release
		return host
	})
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	loaded := make(chan error, 1)
	go func() { _, err := manager.Snapshot(t.Context(), record.ID); loaded <- err }()
	<-constructed
	stopped := make(chan error, 1)
	go func() { stopped <- manager.Shutdown(context.Background()) }()
	select {
	case <-owner.Done():
	case <-time.After(time.Second):
		t.Fatal("manager did not cancel host lifetime")
	}
	once.Do(func() { close(release) })
	if err := <-loaded; !errors.Is(err, session.ErrClosed) {
		t.Fatalf("load race = %v", err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.started != 0 || host.closed != 1 {
		t.Fatalf("rejected host started=%d, closed=%d", host.started, host.closed)
	}
}

func TestTemporarySessionDisposalClosesPluginHost(t *testing.T) {
	host := &mockPluginHost{}
	manager := pluginTestManager(t, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.DisposeTemporary(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.closed != 1 {
		t.Fatalf("temporary disposal close count = %d", host.closed)
	}
}

func TestSessionPluginHostReconcilesRenameDuringLoad(t *testing.T) {
	constructed := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	host := &mockPluginHost{}
	manager := pluginTestManager(t, func(context.Context, session.PluginSession, func()) session.PluginHost {
		close(constructed)
		<-release
		return host
	})
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo", Name: "before"})
	if err != nil {
		t.Fatal(err)
	}
	loaded := make(chan error, 1)
	go func() { _, err := manager.Snapshot(t.Context(), record.ID); loaded <- err }()
	<-constructed
	if _, err := manager.Rename(t.Context(), record.ID, "during-load"); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err := <-loaded; err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if !reflect.DeepEqual(host.names, []string{"during-load"}) {
		t.Fatalf("missed in-flight rename: %v", host.names)
	}
}

type commandPluginHost struct {
	mockPluginHost
	command session.PluginCommand
	invoked chan []string
}

func (h *commandPluginHost) Commands() []session.PluginCommand {
	return []session.PluginCommand{h.command}
}
func (h *commandPluginHost) ExecuteCommand(ctx context.Context, instance, id, args string) error {
	select {
	case h.invoked <- []string{instance, id, args}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func TestPluginCommandSessionPortDoesNotStartModelRun(t *testing.T) {
	host := &commandPluginHost{command: session.PluginCommand{ID: "demo.run", LocalID: "run", PluginID: "demo", Instance: "owner:1", Description: "Run"}, invoked: make(chan []string, 1)}
	manager := pluginTestManager(t, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.PluginCommands, []session.PluginCommand{host.command}) {
		t.Fatalf("catalog = %#v", before.PluginCommands)
	}
	if err := manager.ExecutePluginCommand(t.Context(), record.ID, "owner:1", "demo.run", "literal args"); err != nil {
		t.Fatal(err)
	}
	if got := <-host.invoked; !reflect.DeepEqual(got, []string{"owner:1", "demo.run", "literal args"}) {
		t.Fatalf("invocation = %v", got)
	}
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ActiveRunID != "" || !reflect.DeepEqual(after.Messages, before.Messages) || after.Usage != before.Usage {
		t.Fatalf("command changed model state: %#v", after)
	}
}
