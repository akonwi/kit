package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

// messagePluginHost is a session plugin host whose current generation can be
// revoked by the test.
type messagePluginHost struct {
	mockPluginHost
	messageMu sync.Mutex
	observer  func(context.Context, session.PluginMessageInput, func() bool) (session.PluginMessageResult, error)
	revoked   atomic.Bool
	completed func(session.PluginTurn)
}

func (h *messagePluginHost) TurnCompleted(turn session.PluginTurn) {
	h.mockPluginHost.TurnCompleted(turn)
	h.messageMu.Lock()
	completed := h.completed
	h.messageMu.Unlock()
	if completed != nil {
		completed(turn)
	}
}

func (h *messagePluginHost) submitAsync(ctx context.Context, text string) error {
	h.messageMu.Lock()
	observer := h.observer
	h.messageMu.Unlock()
	_, err := observer(ctx, session.PluginMessageInput{PluginID: "autoresearch", Text: text}, func() bool { return !h.revoked.Load() })
	return err
}

func (h *messagePluginHost) SetMessageObserver(observer func(context.Context, session.PluginMessageInput, func() bool) (session.PluginMessageResult, error)) {
	h.messageMu.Lock()
	defer h.messageMu.Unlock()
	h.observer = observer
}

func (h *messagePluginHost) submit(t *testing.T, text, key string) (session.PluginMessageResult, error) {
	t.Helper()
	h.messageMu.Lock()
	observer := h.observer
	h.messageMu.Unlock()
	if observer == nil {
		t.Fatal("session did not install a plugin message observer")
	}
	return observer(t.Context(), session.PluginMessageInput{PluginID: "autoresearch", Text: text, IdempotencyKey: key}, func() bool { return !h.revoked.Load() })
}

// createLoadedSession creates a session and loads its runtime, which installs
// the plugin host and its message observer.
func createLoadedSession(t *testing.T, manager *session.Manager, cwd string) session.SessionRecord {
	t.Helper()
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	return record
}

func waitPluginMessageRun(t *testing.T, manager *session.Manager, sessionID, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := manager.GetRun(t.Context(), sessionID, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == session.RunStatusCompleted {
			return
		}
		if run.Status != session.RunStatusRunning || time.Now().After(deadline) {
			t.Fatalf("plugin message run = %+v", run)
		}
		time.Sleep(time.Millisecond)
	}
}

// pluginMessageRows returns the session's plugin message transcript rows.
func pluginMessageRows(t *testing.T, manager *session.Manager, sessionID string) []session.TranscriptMessage {
	t.Helper()
	snapshot, err := manager.Snapshot(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	var rows []session.TranscriptMessage
	for _, message := range snapshot.Messages {
		if message.Role == "context" && message.BoundaryKind == session.PluginMessageBoundaryKind {
			rows = append(rows, message)
		}
	}
	return rows
}

func providerContextTexts(providers *authorityProviders) [][]string {
	providers.mu.Lock()
	defer providers.mu.Unlock()
	var requests [][]string
	for _, request := range providers.requests {
		var texts []string
		for _, message := range request.Messages {
			if boundary, ok := message.(droids.ContextMessage); ok {
				for _, content := range boundary.Content {
					texts = append(texts, content.(droids.TextInput).Text)
				}
			}
		}
		requests = append(requests, texts)
	}
	return requests
}

func TestPluginMessageStartsAttributedTurnInIdleSession(t *testing.T) {
	providers := &authorityProviders{}
	host := &messagePluginHost{}
	manager := pluginTestManagerWithProviders(t, providers, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record := createLoadedSession(t, manager, t.TempDir())
	result, err := host.submit(t, "Start autoresearch: reduce build time.", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.MessageID, "pluginmsg_") || result.TurnID == "" {
		t.Fatalf("result = %+v", result)
	}
	waitPluginMessageRun(t, manager, record.ID, result.TurnID)

	rows := pluginMessageRows(t, manager, record.ID)
	if len(rows) != 1 {
		t.Fatalf("plugin message rows = %#v", rows)
	}
	row := rows[0]
	if row.TurnID != result.TurnID || row.BoundaryID != result.MessageID || row.BoundarySource != "autoresearch" ||
		!reflect.DeepEqual(row.Content, []session.TranscriptContent{{Kind: session.TranscriptContentText, Text: "Start autoresearch: reduce build time."}}) ||
		string(row.Details) != `{"version":1,"pluginId":"autoresearch"}` {
		t.Fatalf("plugin message row = %+v", row)
	}
	want := [][]string{{"[plugin_message from autoresearch]", "Start autoresearch: reduce build time."}}
	if got := providerContextTexts(providers); !reflect.DeepEqual(got, want) {
		t.Fatalf("model context = %q, want %q", got, want)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		host.mu.Lock()
		events := append([]string(nil), host.turnEvents...)
		host.mu.Unlock()
		if reflect.DeepEqual(events, []string{"started:" + result.TurnID, "completed:" + result.TurnID}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("plugin turn events = %q", events)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPluginMessageRejectsBusySessionWithoutRecording(t *testing.T) {
	block := make(chan struct{})
	providers := &authorityProviders{block: block, started: make(chan struct{})}
	host := &messagePluginHost{}
	manager := pluginTestManagerWithProviders(t, providers, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record := createLoadedSession(t, manager, t.TempDir())
	first, err := manager.StartPrompt(t.Context(), record.ID, "user turn")
	if err != nil {
		t.Fatal(err)
	}
	<-providers.started
	if _, err := host.submit(t, "Continue while busy.", "busy-key"); !errors.Is(err, session.ErrBusy) {
		t.Fatalf("active-turn submission = %v, want ErrBusy", err)
	}
	queued, err := manager.SubmitPrompt(t.Context(), record.ID, "queued follow-up")
	if err != nil || !queued.Queued {
		t.Fatalf("queue follow-up = %+v, %v", queued, err)
	}
	if _, err := host.submit(t, "Continue behind follow-up.", ""); !errors.Is(err, session.ErrBusy) {
		t.Fatalf("queued follow-up submission = %v, want ErrBusy", err)
	}
	close(block)
	waitPluginMessageRun(t, manager, record.ID, first.RunID)
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := manager.Snapshot(t.Context(), record.ID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.ActiveRunID == "" && len(snapshot.Messages) == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("follow-up did not settle: %+v", snapshot)
		}
		time.Sleep(time.Millisecond)
	}
	if rows := pluginMessageRows(t, manager, record.ID); len(rows) != 0 {
		t.Fatalf("rejected submissions were recorded: %#v", rows)
	}
	// A busy rejection records no receipt, so the same key is admitted later.
	result, err := host.submit(t, "Continue while busy.", "busy-key")
	if err != nil {
		t.Fatalf("retry after busy = %v", err)
	}
	waitPluginMessageRun(t, manager, record.ID, result.TurnID)
	if rows := pluginMessageRows(t, manager, record.ID); len(rows) != 1 || rows[0].Content[0].Text != "Continue while busy." {
		t.Fatalf("plugin rows after retry = %#v", rows)
	}
}

func TestPluginMessageRejectsRevokedGenerationAndInvalidInput(t *testing.T) {
	providers := &authorityProviders{}
	host := &messagePluginHost{}
	manager := pluginTestManagerWithProviders(t, providers, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record := createLoadedSession(t, manager, t.TempDir())
	for _, test := range []struct{ text, key string }{
		{"   ", ""},
		{"nul\x00byte", ""},
		{strings.Repeat("x", 128<<10+1), ""},
		{"bad key", "spaces are invalid"},
		{"long key", strings.Repeat("k", 129)},
	} {
		if _, err := host.submit(t, test.text, test.key); !errors.Is(err, session.ErrInvalidInput) {
			t.Errorf("submit(%.20q, %.20q) = %v, want ErrInvalidInput", test.text, test.key, err)
		}
	}
	host.revoked.Store(true)
	if _, err := host.submit(t, "From a revoked generation.", "stale"); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("revoked submission = %v, want ErrClosed", err)
	}
	host.revoked.Store(false)
	// The revoked attempt recorded nothing, so its key is free for the current generation.
	result, err := host.submit(t, "From the current generation.", "stale")
	if err != nil {
		t.Fatalf("current generation with the same key = %v", err)
	}
	waitPluginMessageRun(t, manager, record.ID, result.TurnID)
	if rows := pluginMessageRows(t, manager, record.ID); len(rows) != 1 || rows[0].Content[0].Text != "From the current generation." {
		t.Fatalf("plugin rows = %#v", rows)
	}
	providers.mu.Lock()
	calls := providers.calls
	providers.mu.Unlock()
	if calls != 1 {
		t.Fatalf("provider calls = %d, want 1", calls)
	}
}

func TestPluginMessageIdempotencyAcrossRestart(t *testing.T) {
	root := t.TempDir()
	repository, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	providers := &authorityProviders{}
	droidDirectory := filepath.Join(root, "droids")
	firstHost := &messagePluginHost{}
	manager, err := session.NewManager(repository, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory), session.WithPluginHostFactory(func(context.Context, session.PluginSession, func()) session.PluginHost { return firstHost }))
	if err != nil {
		t.Fatal(err)
	}
	record := createLoadedSession(t, manager, root)
	original, err := firstHost.submit(t, "Run experiment 1.", "experiment-1")
	if err != nil {
		t.Fatal(err)
	}
	waitPluginMessageRun(t, manager, record.ID, original.TurnID)
	if retried, err := firstHost.submit(t, "Run experiment 1.", "experiment-1"); err != nil || retried != original {
		t.Fatalf("retry = %+v, %v; want %+v", retried, err, original)
	}
	if _, err := firstHost.submit(t, "Run experiment 2.", "experiment-1"); !errors.Is(err, session.ErrPluginMessageConflict) {
		t.Fatalf("conflicting reuse = %v, want ErrPluginMessageConflict", err)
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	if err := manager.Shutdown(shutdownCtx); err != nil {
		cancelShutdown()
		t.Fatal(err)
	}
	cancelShutdown()

	secondHost := &messagePluginHost{}
	reopened, err := session.NewManager(repository, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory), session.WithPluginHostFactory(func(context.Context, session.PluginSession, func()) session.PluginHost { return secondHost }))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if retried, err := secondHost.submit(t, "Run experiment 1.", "experiment-1"); err != nil || retried != original {
		t.Fatalf("retry after restart = %+v, %v; want %+v", retried, err, original)
	}
	if _, err := secondHost.submit(t, "Run experiment 2.", "experiment-1"); !errors.Is(err, session.ErrPluginMessageConflict) {
		t.Fatalf("conflicting reuse after restart = %v, want ErrPluginMessageConflict", err)
	}
	if rows := pluginMessageRows(t, reopened, record.ID); len(rows) != 1 || rows[0].BoundaryID != original.MessageID {
		t.Fatalf("plugin rows after retries = %#v", rows)
	}
	providers.mu.Lock()
	calls := providers.calls
	providers.mu.Unlock()
	if calls != 1 {
		t.Fatalf("provider calls = %d, want 1", calls)
	}
}

func TestPluginMessagesAreIsolatedToTheOwningSession(t *testing.T) {
	block := make(chan struct{})
	providers := &authorityProviders{block: block, started: make(chan struct{})}
	hosts := map[string]*messagePluginHost{}
	var hostsMu sync.Mutex
	manager := pluginTestManagerWithProviders(t, providers, func(_ context.Context, input session.PluginSession, _ func()) session.PluginHost {
		hostsMu.Lock()
		defer hostsMu.Unlock()
		host := &messagePluginHost{}
		hosts[input.ID] = host
		return host
	})
	busy := createLoadedSession(t, manager, t.TempDir())
	idle := createLoadedSession(t, manager, t.TempDir())
	userRun, err := manager.StartPrompt(t.Context(), busy.ID, "user turn")
	if err != nil {
		t.Fatal(err)
	}
	<-providers.started
	hostsMu.Lock()
	busyHost, idleHost := hosts[busy.ID], hosts[idle.ID]
	hostsMu.Unlock()
	if _, err := busyHost.submit(t, "Busy session message.", ""); !errors.Is(err, session.ErrBusy) {
		t.Fatalf("busy session submission = %v, want ErrBusy", err)
	}
	result, err := idleHost.submit(t, "Idle session message.", "")
	if err != nil {
		t.Fatalf("idle session submission = %v", err)
	}
	close(block)
	waitPluginMessageRun(t, manager, busy.ID, userRun.RunID)
	waitPluginMessageRun(t, manager, idle.ID, result.TurnID)
	if rows := pluginMessageRows(t, manager, busy.ID); len(rows) != 0 {
		t.Fatalf("busy session plugin rows = %#v", rows)
	}
	rows := pluginMessageRows(t, manager, idle.ID)
	if len(rows) != 1 || rows[0].Content[0].Text != "Idle session message." {
		t.Fatalf("idle session plugin rows = %#v", rows)
	}
	var details map[string]any
	if err := json.Unmarshal(rows[0].Details, &details); err != nil || details["pluginId"] != "autoresearch" {
		t.Fatalf("details = %s, %v", rows[0].Details, err)
	}
}

func TestPluginMessageContinuesImmediatelyAfterTurnCompletion(t *testing.T) {
	const continuations = 20
	providers := &authorityProviders{}
	host := &messagePluginHost{}
	manager := pluginTestManagerWithProviders(t, providers, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record := createLoadedSession(t, manager, t.TempDir())
	submitted := make(chan error, continuations)
	var next atomic.Int32
	host.messageMu.Lock()
	// Submit from the completion event itself, before the finished turn settles.
	host.completed = func(session.PluginTurn) {
		index := next.Add(1)
		if index > continuations {
			return
		}
		go func() { submitted <- host.submitAsync(t.Context(), fmt.Sprintf("Continue %d.", index)) }()
		// The finished turn cannot release admission until this delivery returns,
		// so the submission above observes a settling turn.
		time.Sleep(20 * time.Millisecond)
	}
	host.messageMu.Unlock()
	if _, err := host.submit(t, "Start.", ""); err != nil {
		t.Fatal(err)
	}
	for range continuations {
		select {
		case err := <-submitted:
			if err != nil {
				t.Fatalf("continuation submission = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("continuation was not submitted")
		}
	}
	want := []string{"Start."}
	for index := 1; index <= continuations; index++ {
		want = append(want, fmt.Sprintf("Continue %d.", index))
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := manager.Snapshot(t.Context(), record.ID)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, row := range pluginMessageRows(t, manager, record.ID) {
			got = append(got, row.Content[0].Text)
		}
		if snapshot.ActiveRunID == "" && reflect.DeepEqual(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("plugin message chain = %q, want %q", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPluginMessageKeyedRetryReplaysWhileItsTurnRuns(t *testing.T) {
	block := make(chan struct{})
	providers := &authorityProviders{block: block, started: make(chan struct{})}
	host := &messagePluginHost{}
	manager := pluginTestManagerWithProviders(t, providers, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record := createLoadedSession(t, manager, t.TempDir())
	original, err := host.submit(t, "Run experiment 7.", "experiment-7")
	if err != nil {
		t.Fatal(err)
	}
	<-providers.started
	if retried, err := host.submit(t, "Run experiment 7.", "experiment-7"); err != nil || retried != original {
		t.Fatalf("retry while running = %+v, %v; want %+v", retried, err, original)
	}
	if _, err := host.submit(t, "Run experiment 8.", "experiment-7"); !errors.Is(err, session.ErrPluginMessageConflict) {
		t.Fatalf("conflict while running = %v, want ErrPluginMessageConflict", err)
	}
	close(block)
	waitPluginMessageRun(t, manager, record.ID, original.TurnID)
	if rows := pluginMessageRows(t, manager, record.ID); len(rows) != 1 || rows[0].BoundaryID != original.MessageID {
		t.Fatalf("plugin rows = %#v", rows)
	}
}

func TestPluginMessageConcurrentKeyedSubmissionsShareOneAdmission(t *testing.T) {
	providers := &authorityProviders{}
	host := &messagePluginHost{}
	manager := pluginTestManagerWithProviders(t, providers, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record := createLoadedSession(t, manager, t.TempDir())
	const rounds = 10
	for round := range rounds {
		key := fmt.Sprintf("round-%d", round)
		text := fmt.Sprintf("Concurrent round %d.", round)
		results := make(chan session.PluginMessageResult, 2)
		errs := make(chan error, 2)
		start := make(chan struct{})
		for range 2 {
			go func() {
				<-start
				h := host
				h.messageMu.Lock()
				observer := h.observer
				h.messageMu.Unlock()
				result, err := observer(t.Context(), session.PluginMessageInput{PluginID: "autoresearch", Text: text, IdempotencyKey: key}, func() bool { return true })
				results <- result
				errs <- err
			}()
		}
		close(start)
		first, second := <-results, <-results
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatalf("round %d submission = %v, want admission or replay", round, err)
			}
		}
		if first != second || first.TurnID == "" {
			t.Fatalf("round %d results = %+v and %+v, want one shared admission", round, first, second)
		}
		waitPluginMessageRun(t, manager, record.ID, first.TurnID)
	}
	if rows := pluginMessageRows(t, manager, record.ID); len(rows) != rounds {
		t.Fatalf("plugin rows = %d, want %d", len(rows), rounds)
	}
}
