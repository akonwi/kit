package session_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

// sessionWatcher follows one session's live events the way a second attached
// client does: from a snapshot's cursor, through the event stream.
type sessionWatcher struct {
	t         *testing.T
	manager   *session.Manager
	sessionID string
	stream    string
	cursor    int64
}

func watchSession(t *testing.T, manager *session.Manager, sessionID string) *sessionWatcher {
	t.Helper()
	snapshot, err := manager.Snapshot(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return &sessionWatcher{t: t, manager: manager, sessionID: sessionID, stream: snapshot.EventStreamID, cursor: snapshot.EventCursor}
}

// next returns the next event of kind on the watched stream, skipping other
// kinds. It fails if the stream is replaced first.
func (w *sessionWatcher) next(kind session.EventKind) session.Event {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(w.t.Context(), 5*time.Second)
	defer cancel()
	for {
		page, err := w.manager.WaitEvents(ctx, w.sessionID, w.stream, w.cursor)
		if err != nil {
			w.t.Fatalf("watcher did not receive %s: %v", kind, err)
		}
		if page.ResyncRequired {
			w.t.Fatalf("watched stream was replaced before %s arrived", kind)
		}
		for _, event := range page.Events {
			// Later events of this page are read again by the next call.
			w.cursor = event.Sequence
			if event.Kind == kind {
				return event
			}
		}
	}
}

// queue returns the queue the next follow_ups.changed event carries.
func (w *sessionWatcher) queue() session.FollowUpQueue {
	w.t.Helper()
	event := w.next(session.EventFollowUpsChanged)
	if event.TurnID != "" || event.FollowUps == nil {
		w.t.Fatalf("follow-up queue event = %+v", event)
	}
	return *event.FollowUps
}

func TestFollowUpQueueChangesReachEveryAttachedClient(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPrompt(t.Context(), created.ID, "first"); err != nil {
		t.Fatal(err)
	}
	<-providers.started
	watcher := watchSession(t, manager, created.ID)

	if _, err := manager.SubmitPrompt(t.Context(), created.ID, "second"); err != nil {
		t.Fatal(err)
	}
	if got, want := watcher.queue(), (session.FollowUpQueue{Count: 1, Previews: []string{"second"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("queue after enqueue = %+v; want %+v", got, want)
	}
	if _, err := manager.SubmitPrompt(t.Context(), created.ID, "third"); err != nil {
		t.Fatal(err)
	}
	if got, want := watcher.queue(), (session.FollowUpQueue{Count: 2, Previews: []string{"second", "third"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("queue after second enqueue = %+v; want %+v", got, want)
	}
	// Previews show as one renderer-safe line: format and control characters,
	// as inside a joined emoji, become spaces, and text with nothing visible
	// is shown as a message.
	if _, err := manager.SubmitPrompt(t.Context(), created.ID, "family 👨\u200d👧\x1b[0m"); err != nil {
		t.Fatal(err)
	}
	watcher.queue()
	if _, err := manager.SubmitPrompt(t.Context(), created.ID, "\u200b\u200d"); err != nil {
		t.Fatal(err)
	}
	if got, want := watcher.queue(), (session.FollowUpQueue{Count: 4, Previews: []string{"second", "third", "family 👨 👧 [0m", "Message"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("queue after format characters = %+v; want %+v", got, want)
	}
	restored, err := manager.RestoreFollowUps(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := watcher.queue(), (session.FollowUpQueue{Previews: []string{}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("queue after restore = %+v; want %+v", got, want)
	}
	for _, message := range restored.Messages[:2] {
		if _, err := manager.SubmitPromptInput(t.Context(), created.ID, message); err != nil {
			t.Fatal(err)
		}
		watcher.queue()
	}
	if _, err := manager.PromoteFollowUps(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if got, want := watcher.queue(), (session.FollowUpQueue{Count: 1, Previews: []string{"third"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("queue after first promotion = %+v; want %+v", got, want)
	}
	if got, want := watcher.queue(), (session.FollowUpQueue{Previews: []string{}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("queue after promotion = %+v; want %+v", got, want)
	}

	// The queued prompt starts as the running turn ends. Its turn's stream
	// reports the queue without it, after the turn's start and its message,
	// and no snapshot shows the prompt both queued and running.
	if _, err := manager.SubmitPrompt(t.Context(), created.ID, "automatic"); err != nil {
		t.Fatal(err)
	}
	watcher.queue()
	close(providers.block)
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := manager.Snapshot(t.Context(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.EventStreamID != watcher.stream {
			if snapshot.FollowUps.Count != 0 {
				t.Fatalf("snapshot after admission still lists the started prompt: %+v", snapshot.FollowUps)
			}
			page, err := manager.Events(t.Context(), created.ID, snapshot.EventStreamID, 0)
			if err != nil {
				t.Fatal(err)
			}
			var kinds []session.EventKind
			for _, event := range page.Events[:3] {
				kinds = append(kinds, event.Kind)
			}
			if want := []session.EventKind{session.EventRunStarted, session.EventUserMessage, session.EventFollowUpsChanged}; !reflect.DeepEqual(kinds, want) {
				t.Fatalf("admitted turn's first events = %v; want %v", kinds, want)
			}
			if page.Events[1].Text != "automatic" || page.Events[2].FollowUps.Count != 0 || page.Events[2].TurnID != "" {
				t.Fatalf("admitted turn's events = %+v", page.Events[:3])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queued prompt did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestFailedFollowUpAdmissionHoldsTheQueueForEveryClient(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	attachments, err := attachment.NewFilesystem(filepath.Join(root, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")), session.WithAttachmentStore(attachments))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPrompt(t.Context(), created.ID, "first"); err != nil {
		t.Fatal(err)
	}
	<-providers.started
	file, err := attachments.Put(t.Context(), attachment.PutInput{SessionID: created.ID, Filename: "notes.txt", MediaType: "text/plain", Content: strings.NewReader("notes"), MaxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	queued := session.PromptInput{Text: "read the notes", AttachmentIDs: []string{file.ID}}
	if _, err := manager.SubmitPromptInput(t.Context(), created.ID, queued); err != nil {
		t.Fatal(err)
	}
	watcher := watchSession(t, manager, created.ID)
	// The attachment disappears while the prompt waits, so it cannot start.
	if err := attachments.Remove(t.Context(), created.ID, file.ID); err != nil {
		t.Fatal(err)
	}
	close(providers.block)

	held := watcher.queue()
	if held.Count != 1 || !reflect.DeepEqual(held.Previews, []string{"read the notes"}) || held.AdmissionFailure != `attachment "`+file.ID+`" is unavailable` {
		t.Fatalf("held queue = %+v", held)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveRunID != "" || !reflect.DeepEqual(snapshot.FollowUps, held) {
		t.Fatalf("snapshot of held queue: active=%q queue=%+v; want %+v", snapshot.ActiveRunID, snapshot.FollowUps, held)
	}
	// New prompts wait behind the held queue, and say why.
	for name, start := range map[string]func() error{
		"start":  func() error { _, err := manager.StartPrompt(t.Context(), created.ID, "new"); return err },
		"submit": func() error { _, err := manager.SubmitPrompt(t.Context(), created.ID, "new"); return err },
	} {
		err := start()
		if !errors.Is(err, session.ErrBusy) || !strings.Contains(err.Error(), "the first queued follow-up could not start ("+held.AdmissionFailure+")") {
			t.Fatalf("%s behind a held queue: %v", name, err)
		}
	}

	restored, err := manager.RestoreFollowUps(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Messages, []session.PromptInput{queued}) || restored.Queue.Count != 0 || restored.Queue.AdmissionFailure != "" {
		t.Fatalf("restored held queue = %+v", restored)
	}
	if got, want := watcher.queue(), (session.FollowUpQueue{Previews: []string{}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("queue after restore = %+v; want %+v", got, want)
	}
	if _, err := manager.StartPrompt(t.Context(), created.ID, "new"); err != nil {
		t.Fatalf("StartPrompt() after restore = %v", err)
	}
}

func TestSessionConfigurationReachesEveryAttachedClient(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &configurationProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
	if err != nil {
		t.Fatal(err)
	}
	watcher := watchSession(t, manager, record.ID)
	high := "high"
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: 1, Model: "test/large", ThinkingLevel: &high}); err != nil {
		t.Fatal(err)
	}
	event := watcher.next(session.EventSessionConfigurationChanged)
	if want := (session.SessionConfiguration{ModelProvider: "test", ModelID: "large", ThinkingLevel: "high", ConfigurationRevision: 2}); event.TurnID != "" || event.Configuration == nil || *event.Configuration != want {
		t.Fatalf("thinking change event = %+v; want %+v", event, want)
	}

	// A model change replaces the stream; the replacement starts with the
	// applied configuration.
	configured, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: 2, Model: "test/small"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := manager.Events(t.Context(), record.ID, configured.EventStreamID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := (session.SessionConfiguration{ModelProvider: "test", ModelID: "small", ThinkingLevel: "high", ConfigurationRevision: 3}); len(page.Events) != 1 ||
		page.Events[0].Kind != session.EventSessionConfigurationChanged || *page.Events[0].Configuration != want {
		t.Fatalf("replacement stream = %+v; want %+v", page.Events, want)
	}
}

func TestSessionCompactionReachesEveryAttachedClient(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &configurationProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two", "three"} {
		if _, err := manager.RunPrompt(t.Context(), record.ID, text); err != nil {
			t.Fatal(err)
		}
	}
	gate, entered := make(chan struct{}), make(chan struct{}, 1)
	providers.mu.Lock()
	providers.compactionGate, providers.compactionEntered = gate, entered
	providers.mu.Unlock()

	// A compaction that fails leaves the stream in place and reports why.
	watcher := watchSession(t, manager, record.ID)
	failing, cancel := context.WithCancel(t.Context())
	failed := make(chan error, 1)
	go func() { _, err := manager.CompactSession(failing, record.ID, "compact_watch_failed"); failed <- err }()
	started := watcher.next(session.EventCompactionStarted)
	if started.TurnID != "" || started.CompactionID != "compact_watch_failed" {
		t.Fatalf("started compaction = %+v", started)
	}
	<-entered
	cancel()
	if err := <-failed; err == nil {
		t.Fatal("cancelled compaction succeeded")
	}
	ended := watcher.next(session.EventCompactionFailed)
	if ended.TurnID != "" || ended.CompactionID != "compact_watch_failed" || ended.CompactionFailure != session.CompactionFailureCanceled || ended.ErrorMessage != "" {
		t.Fatalf("failed compaction = %+v, want canceled", ended)
	}

	// Every client sees a successful compaction start before its result
	// replaces the stream.
	watcher = watchSession(t, manager, record.ID)
	succeeded := make(chan error, 1)
	go func() { _, err := manager.CompactSession(t.Context(), record.ID, "compact_watch_ok"); succeeded <- err }()
	started = watcher.next(session.EventCompactionStarted)
	if started.TurnID != "" || started.CompactionID != "compact_watch_ok" {
		t.Fatalf("started compaction = %+v", started)
	}
	<-entered
	close(gate)
	if err := <-succeeded; err != nil {
		t.Fatal(err)
	}

	// Repeating a request for a compaction that succeeded returns its result
	// and is not announced as a second compaction.
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CompactSession(t.Context(), record.ID, "compact_watch_ok"); err != nil {
		t.Fatal(err)
	}
	page, err := manager.Events(t.Context(), record.ID, snapshot.EventStreamID, snapshot.EventCursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Events {
		if event.Kind == session.EventCompactionStarted {
			t.Fatalf("repeated compaction request was announced: %+v", event)
		}
	}
}

// waitForEmptyQueue waits until the session's queued prompts have started.
func waitForEmptyQueue(t *testing.T, manager *session.Manager, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		snapshot, err := manager.Snapshot(t.Context(), sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.FollowUps.Count == 0 {
			return
		}
		if snapshot.FollowUps.AdmissionFailure != "" || time.Now().After(deadline) {
			t.Fatalf("queued prompt did not start: %+v", snapshot.FollowUps)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestQueuedFollowUpStartsWhenAdmissionHeldWithoutATurnIsReleased(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &authorityProviders{}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.QueueFollowUpForTest(t.Context(), created.ID, session.PromptInput{Text: "queued"}); err != nil {
		t.Fatal(err)
	}
	release, err := manager.HoldAdmissionForTest(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartQueuedFollowUpsForTest(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	// However long admission stays held, the prompt waits queued.
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveRunID != "" || snapshot.FollowUps.Count != 1 || snapshot.FollowUps.AdmissionFailure != "" {
		t.Fatalf("queue while admission is held = %+v, active run %q", snapshot.FollowUps, snapshot.ActiveRunID)
	}
	release()
	waitForEmptyQueue(t, manager, created.ID)
}

func TestQueuedFollowUpStartsAfterAResumedTurn(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	droidDirectory := filepath.Join(root, "droids")
	manager, err := session.NewManager(store, interruptibleProviders{providers}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPrompt(t.Context(), created.ID, "interrupted by a restart"); err != nil {
		t.Fatal(err)
	}
	<-providers.started
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}

	// The reopened session resumes the interrupted turn, which asks the
	// provider again, and a prompt submitted meanwhile is queued behind it.
	block := make(chan struct{})
	providers.mu.Lock()
	providers.block, providers.started = block, make(chan struct{})
	resumed := providers.started
	providers.mu.Unlock()
	reopened, err := session.NewManager(store, interruptibleProviders{providers}, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(droidDirectory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if _, err := reopened.Snapshot(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	<-resumed
	submission, err := reopened.SubmitPrompt(t.Context(), created.ID, "queued behind the resumed turn")
	if err != nil {
		t.Fatal(err)
	}
	if !submission.Queued {
		t.Fatalf("submission during the resumed turn = %+v", submission)
	}
	providers.mu.Lock()
	providers.block = nil
	providers.mu.Unlock()
	close(block)
	waitForEmptyQueue(t, reopened, created.ID)
}

// interruptibleProviders fails a model request that shutdown cancels, so the
// interrupted turn keeps no reply and asks the provider again when resumed.
type interruptibleProviders struct{ *authorityProviders }

func (p interruptibleProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}

func (p interruptibleProviders) Stream(ctx context.Context, model droids.Model, request droids.Request) droids.Stream {
	stream := p.authorityProviders.Stream(ctx, model, request)
	if ctx.Err() != nil {
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "echo", StopReason: droids.StopReasonError,
			ErrorKind: droids.ProviderTransport, ErrorMessage: ctx.Err().Error(),
		}}
	}
	return stream
}
