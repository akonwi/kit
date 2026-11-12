package session_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akonwi/kit/internal/session"
)

func TestViewRevealsReachWatchersUntilTheSessionIsDeleted(t *testing.T) {
	manager := pluginTestManager(t, func(context.Context, session.PluginSession, func()) session.PluginHost { return &mockPluginHost{} })
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	reveals, stop, err := manager.WatchViewReveals(t.Context(), record.ID, "view_a")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	delivered, err := manager.RevealView(t.Context(), record.ID, "view_a", session.ViewReveal{Line: 12})
	if err != nil || !delivered {
		t.Fatalf("RevealView = %v, %v; want delivered", delivered, err)
	}
	if got := <-reveals; got != (session.ViewReveal{Line: 12}) {
		t.Fatalf("watcher received %+v", got)
	}
	if err := manager.Delete(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if _, open := <-reveals; open {
		t.Fatal("deleting the session left the watch open")
	}
	if _, err := manager.RevealView(t.Context(), record.ID, "view_a", session.ViewReveal{Line: 1}); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("RevealView after delete = %v, want ErrNotFound", err)
	}
	if _, _, err := manager.WatchViewReveals(t.Context(), record.ID, "view_a"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("WatchViewReveals after delete = %v, want ErrNotFound", err)
	}
}
