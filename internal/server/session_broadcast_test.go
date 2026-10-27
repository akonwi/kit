package server

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/clienttransport"
	kitsession "github.com/akonwi/kit/internal/session"
)

// broadcastDaemon runs a daemon whose turns wait for block and returns two
// independent clients of it.
func broadcastDaemon(t *testing.T, providers *daemonEchoProviders) (*clienttransport.Client, *clienttransport.Client) {
	t.Helper()
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: paths, Providers: providers, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("daemon shutdown blocked")
		}
	})
	first, second := localTransport(paths), localTransport(paths)
	t.Cleanup(first.CloseIdleConnections)
	t.Cleanup(second.CloseIdleConnections)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, err := first.Probe(t.Context(), true); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return first, second
}

// nextFollowUps returns the queue carried by the next follow_ups.changed
// event a stream delivers.
func nextFollowUps(t *testing.T, batches <-chan protocol.SessionEventBatch) protocol.FollowUpQueue {
	t.Helper()
	for {
		batch := nextSessionEvents(t, batches)
		if batch.ResyncRequired {
			t.Fatal("stream required resynchronization before the queue changed")
		}
		for _, event := range batch.Events {
			if changed, ok := event.Payload.(protocol.FollowUpsChangedEvent); ok {
				if event.TurnID != "" {
					t.Fatalf("follow-up queue event carries turn %q", event.TurnID)
				}
				return *changed.FollowUps
			}
		}
	}
}

func TestFollowUpQueueChangesReachAnotherClientsEventStream(t *testing.T) {
	block, started := make(chan struct{}), make(chan struct{})
	writer, watcher := broadcastDaemon(t, &daemonEchoProviders{block: block, requestStarted: started})
	defer close(block)
	created, err := writer.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.StartPrompt(t.Context(), created.ID, "first"); err != nil {
		t.Fatal(err)
	}
	<-started
	snapshot, err := watcher.GetSessionSnapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	batches := openSessionEvents(t, watcher, created.ID, snapshot.EventStreamID, snapshot.EventCursor)
	nextSessionEvents(t, batches) // The catch-up batch.

	if _, err := writer.SubmitPrompt(t.Context(), created.ID, "second"); err != nil {
		t.Fatal(err)
	}
	if got, want := nextFollowUps(t, batches), (protocol.FollowUpQueue{Count: 1, Previews: []string{"second"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("watched queue after enqueue = %+v; want %+v", got, want)
	}
	if _, err := writer.RestoreFollowUps(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if got, want := nextFollowUps(t, batches), (protocol.FollowUpQueue{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("watched queue after restore = %+v; want %+v", got, want)
	}
}

func TestSessionEventsProjectQueuesAndConfigurations(t *testing.T) {
	service := runtimeSessionService{}
	queue := service.projectSessionEventPayload(kitsession.Event{NewEvent: kitsession.NewEvent{
		SessionID: "session_test", Kind: kitsession.EventFollowUpsChanged,
		FollowUps: &kitsession.FollowUpQueue{Count: 1, Previews: []string{"next"}, AdmissionFailure: "attachment is unavailable"},
	}})
	if want := (protocol.FollowUpsChangedEvent{FollowUps: &protocol.FollowUpQueue{
		Count: 1, Previews: []string{"next"}, AdmissionFailure: &protocol.FollowUpAdmissionFailure{Message: "attachment is unavailable"},
	}}); !reflect.DeepEqual(queue, want) {
		t.Fatalf("queue payload = %#v; want %#v", queue, want)
	}
	configuration := service.projectSessionEventPayload(kitsession.Event{NewEvent: kitsession.NewEvent{
		SessionID: "session_test", Kind: kitsession.EventSessionConfigurationChanged,
		Configuration: &kitsession.SessionConfiguration{ModelProvider: "openai", ModelID: "gpt-5", ThinkingLevel: "high", ConfigurationRevision: 4},
	}})
	if want := (protocol.SessionConfigurationChangedEvent{Configuration: &protocol.SessionConfiguration{
		Model: "openai/gpt-5", ThinkingLevel: "high", ConfigurationRevision: 4,
	}}); !reflect.DeepEqual(configuration, want) {
		t.Fatalf("configuration payload = %#v; want %#v", configuration, want)
	}
}
