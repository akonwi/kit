package session_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/akonwi/kit/internal/session"
)

// urlPluginHost is a session plugin host whose URL requests the test issues
// and whose current generation the test can revoke.
type urlPluginHost struct {
	mockPluginHost
	urlMu    sync.Mutex
	observer func(context.Context, session.PluginURLRequest, func() bool) error
	revoked  atomic.Bool
}

func (h *urlPluginHost) SetURLObserver(observer func(context.Context, session.PluginURLRequest, func() bool) error) {
	h.urlMu.Lock()
	defer h.urlMu.Unlock()
	h.observer = observer
}

func (h *urlPluginHost) open(t *testing.T, url string) error {
	t.Helper()
	h.urlMu.Lock()
	observer := h.observer
	h.urlMu.Unlock()
	if observer == nil {
		t.Fatal("session did not install a plugin URL observer")
	}
	return observer(t.Context(), session.PluginURLRequest{URL: url}, func() bool { return !h.revoked.Load() })
}

type urlEvent struct {
	Kind           session.EventKind
	TurnID, RunID  string
	URL            string
	SequenceOffset int64
}

func urlEventsAfter(t *testing.T, manager *session.Manager, sessionID string, snapshot session.Snapshot) []urlEvent {
	t.Helper()
	page, err := manager.Events(t.Context(), sessionID, snapshot.EventStreamID, snapshot.EventCursor)
	if err != nil {
		t.Fatal(err)
	}
	events := []urlEvent{}
	for _, event := range page.Events {
		events = append(events, urlEvent{Kind: event.Kind, TurnID: event.TurnID, RunID: event.RunID, URL: event.URL, SequenceOffset: event.Sequence - snapshot.EventCursor})
	}
	return events
}

func TestPluginURLRequestIsALiveSessionEventOutsideTheTranscript(t *testing.T) {
	host := &urlPluginHost{}
	manager := pluginTestManager(t, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record := createLoadedSession(t, manager, t.TempDir())
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := host.open(t, "https://example.com/docs?q=1#top"); err != nil {
		t.Fatal(err)
	}
	if err := host.open(t, "http://localhost:3000/"); err != nil {
		t.Fatal(err)
	}
	want := []urlEvent{
		{Kind: session.EventURLOpenRequested, URL: "https://example.com/docs?q=1#top", SequenceOffset: 1},
		{Kind: session.EventURLOpenRequested, URL: "http://localhost:3000/", SequenceOffset: 2},
	}
	if got := urlEventsAfter(t, manager, record.ID, before); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %+v, want %+v", got, want)
	}

	// The request is ephemeral: the transcript a later attachment loads is unchanged.
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Messages, before.Messages) || after.EventCursor != before.EventCursor+2 {
		t.Fatalf("snapshot messages = %+v at cursor %d, want %+v at cursor %d", after.Messages, after.EventCursor, before.Messages, before.EventCursor+2)
	}
}

func TestPluginURLRequestRejectsInvalidAddressesAndRevokedGenerations(t *testing.T) {
	host := &urlPluginHost{}
	manager := pluginTestManager(t, func(context.Context, session.PluginSession, func()) session.PluginHost { return host })
	record := createLoadedSession(t, manager, t.TempDir())
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"", "file:///etc/passwd", "HTTPS://example.com/", "https://user:secret@example.com/"} {
		if err := host.open(t, url); !errors.Is(err, session.ErrInvalidInput) {
			t.Errorf("open(%q) = %v, want invalid input", url, err)
		}
	}
	host.revoked.Store(true)
	if err := host.open(t, "https://example.com/"); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("revoked open = %v, want closed", err)
	}
	if got := urlEventsAfter(t, manager, record.ID, before); len(got) != 0 {
		t.Fatalf("rejected requests published %+v", got)
	}
}
