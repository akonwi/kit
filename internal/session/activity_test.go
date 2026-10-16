package session_test

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
)

func TestManagerActivityReportsSavedSessionsDoingWork(t *testing.T) {
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
	const (
		turn      = "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		bash      = "session_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		delegated = "session_cccccccccccccccccccccccccccccccc"
		idle      = "session_dddddddddddddddddddddddddddddddd"
		temporary = "session_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	)
	for _, id := range []string{turn, bash, delegated, idle, temporary} {
		if _, err := manager.Create(t.Context(), session.CreateInput{ID: id, CWD: root, Model: "test/echo", Temporary: id == temporary}); err != nil {
			t.Fatal(err)
		}
	}
	reservation, err := manager.StartPrompt(t.Context(), turn, "keep running")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	if _, err := manager.StartBash(t.Context(), bash, "bash_0123456789abcdef0123456789abcdef", "sleep 30", false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartBash(t.Context(), temporary, "bash_abcdef0123456789abcdef0123456789", "sleep 30", false); err != nil {
		t.Fatal(err)
	}
	// No supervisor runs in this manager, so the task stays queued.
	admission := subagent.Admission{
		OwnerSessionID: delegated,
		Definition: subagent.Definition{
			Name: "scout", Description: "finds things", Instructions: "Inspect carefully.",
			Source: subagent.Source{Kind: subagent.SourceUser, Path: "/tmp/scout.md"},
		},
		CWD: root, Model: "test/echo", ThinkingLevel: "medium", Message: "look around", Now: time.Now(),
	}
	if _, _, err := store.Admit(t.Context(), admission, subagent.DefaultLimits()); err != nil {
		t.Fatal(err)
	}

	waitForActivity(t, manager, []session.SessionActivity{
		{SessionID: turn, Status: session.ActivityRunning},
		{SessionID: bash, Status: session.ActivityRunning},
		{SessionID: delegated, Status: session.ActivityRunning},
	})

	if err := manager.Abort(t.Context(), turn, reservation.RunID); err != nil {
		t.Fatal(err)
	}
	close(providers.block)
	if err := manager.AbortBash(t.Context(), bash, "bash_0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	waitForActivity(t, manager, []session.SessionActivity{
		{SessionID: delegated, Status: session.ActivityRunning},
	})
}

// waitForActivity polls until the manager reports exactly want, because
// aborted turns and bash commands settle asynchronously.
func waitForActivity(t *testing.T, manager *session.Manager, want []session.SessionActivity) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := manager.Activity(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if reflect.DeepEqual(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Activity() = %#v, want %#v", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
