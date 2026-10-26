package server

import (
	"context"
	"reflect"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/clienttransport"
)

const openURLFixture = "open-url-demo"

// urlRequestEvent is the observable form of one url.open.requested event.
type urlRequestEvent struct {
	Sequence int64
	TurnID   string
	URL      string
}

func urlRequestEvents(events []protocol.SessionEvent) []urlRequestEvent {
	result := []urlRequestEvent{}
	for _, event := range events {
		if request, ok := event.Payload.(protocol.URLOpenRequestedEvent); ok {
			result = append(result, urlRequestEvent{Sequence: event.Sequence, TurnID: event.TurnID, URL: request.URL})
		}
	}
	return result
}

// openSessionEvents opens an event stream after cursor and returns its
// batches. The first batch is the catch-up the server sends on opening.
func openSessionEvents(t *testing.T, client *clienttransport.Client, sessionID, streamID string, after int64) <-chan protocol.SessionEventBatch {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	body, err := client.StreamSessionEvents(ctx, sessionID, streamID, after)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = body.Close() })
	batches := make(chan protocol.SessionEventBatch, 32)
	go func() {
		_ = clienttransport.ReadSessionEvents(body, func(_, _ string, batch protocol.SessionEventBatch) error {
			select {
			case batches <- batch:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	return batches
}

func nextSessionEvents(t *testing.T, batches <-chan protocol.SessionEventBatch) protocol.SessionEventBatch {
	t.Helper()
	select {
	case batch := <-batches:
		return batch
	case <-time.After(5 * time.Second):
		t.Fatal("missing session event batch")
		return protocol.SessionEventBatch{}
	}
}

func TestPluginURLRequestReachesOnlyLiveStreams(t *testing.T) {
	client, sessionID, command := pluginFixtureClient(t, openURLFixture, 1)
	before, err := client.GetSessionSnapshot(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	toastContext, cancelToasts := context.WithCancel(t.Context())
	defer cancelToasts()
	toastBody, err := client.StreamPluginToasts(toastContext, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer toastBody.Close()
	toasts := make(chan protocol.PluginToast, 32)
	go func() {
		_ = ReadPluginToasts(toastBody, func(toast protocol.PluginToast) error { toasts <- toast; return nil })
	}()
	nextToast := func() [2]string {
		t.Helper()
		select {
		case toast := <-toasts:
			return [2]string{toast.Title, toast.Subtitle}
		case <-time.After(5 * time.Second):
			t.Fatal("missing plugin toast")
			return [2]string{}
		}
	}
	open := func(url string) {
		t.Helper()
		if err := client.ExecutePluginCommand(t.Context(), sessionID, protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance, Args: url}); err != nil {
			t.Fatal(err)
		}
	}

	live := openSessionEvents(t, client, sessionID, before.EventStreamID, before.EventCursor)
	if opened := nextSessionEvents(t, live); len(opened.Events) != 0 || opened.LastSequence != before.EventCursor {
		t.Fatalf("opening batch = %+v, want no events through cursor %d", opened, before.EventCursor)
	}

	// The plugin learns its request was accepted, and the stream already open
	// receives its canonical address.
	open("HTTPS://example.com/docs?q=1")
	if got, want := nextToast(), [2]string{"URL requested", "HTTPS://example.com/docs?q=1"}; got != want {
		t.Fatalf("toast = %q, want %q", got, want)
	}
	first := before.EventCursor + 1
	if got, want := urlRequestEvents(nextSessionEvents(t, live).Events), []urlRequestEvent{{Sequence: first, URL: "https://example.com/docs?q=1"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("live events = %+v, want %+v", got, want)
	}

	// An invalid address is rejected to the plugin and publishes nothing, so
	// the next accepted request takes the next sequence.
	open("file:///etc/passwd")
	if got, want := nextToast(), [2]string{"URL rejected", "-32602 Invalid parameter: url must use http or https"}; got != want {
		t.Fatalf("toast = %q, want %q", got, want)
	}
	open("http://localhost:3000/")
	if got, want := nextToast(), [2]string{"URL requested", "http://localhost:3000/"}; got != want {
		t.Fatalf("toast = %q, want %q", got, want)
	}
	if got, want := urlRequestEvents(nextSessionEvents(t, live).Events), []urlRequestEvent{{Sequence: first + 1, URL: "http://localhost:3000/"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("live events = %+v, want %+v", got, want)
	}

	// A stream reconnecting from before the requests catches up on them
	// without their addresses, as does an event page, so neither opens them.
	withheld := []urlRequestEvent{{Sequence: first}, {Sequence: first + 1}}
	caughtUp := openSessionEvents(t, client, sessionID, before.EventStreamID, before.EventCursor)
	if got := urlRequestEvents(nextSessionEvents(t, caughtUp).Events); !reflect.DeepEqual(got, withheld) {
		t.Fatalf("catch-up events = %+v, want %+v", got, withheld)
	}
	page, err := client.GetSessionEvents(t.Context(), sessionID, before.EventStreamID, before.EventCursor)
	if err != nil {
		t.Fatal(err)
	}
	if got := urlRequestEvents(page.Events); !reflect.DeepEqual(got, withheld) {
		t.Fatalf("page events = %+v, want %+v", got, withheld)
	}

	// The reconnected stream receives a later request live, with its address.
	open("https://example.com/later")
	if got, want := nextToast(), [2]string{"URL requested", "https://example.com/later"}; got != want {
		t.Fatalf("toast = %q, want %q", got, want)
	}
	later := []urlRequestEvent{{Sequence: first + 2, URL: "https://example.com/later"}}
	if got := urlRequestEvents(nextSessionEvents(t, caughtUp).Events); !reflect.DeepEqual(got, later) {
		t.Fatalf("reconnected live events = %+v, want %+v", got, later)
	}
	if got := urlRequestEvents(nextSessionEvents(t, live).Events); !reflect.DeepEqual(got, later) {
		t.Fatalf("first stream live events = %+v, want %+v", got, later)
	}

	// The transcript a later attachment loads holds no request.
	after, err := client.GetSessionSnapshot(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Messages, before.Messages) || after.EventCursor != first+2 {
		t.Fatalf("snapshot messages = %+v at cursor %d, want %+v at cursor %d", after.Messages, after.EventCursor, before.Messages, first+2)
	}
}
