package session_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func newDeletionTestManager(t *testing.T, providers *authorityProviders) (*session.Manager, string) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(root, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(root, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager, root
}

func TestDeletionBlockerReportsARunningTurnAndClearsWhenIdle(t *testing.T) {
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	manager, root := newDeletionTestManager(t, providers)
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.DeletionBlocker(t.Context(), created.ID); err != nil {
		t.Fatalf("idle DeletionBlocker = %v", err)
	}
	reservation, err := manager.StartPrompt(t.Context(), created.ID, "keep running")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	err = manager.DeletionBlocker(t.Context(), created.ID)
	if !errors.Is(err, session.ErrDeleteBusy) || err.Error() != "session cannot be deleted while work is active: it is running a turn" {
		t.Fatalf("running DeletionBlocker = %v", err)
	}
	if err := manager.Abort(t.Context(), created.ID, reservation.RunID); err != nil {
		t.Fatal(err)
	}
	close(providers.block)
}

func TestConfirmToolCallAsksThroughTheSessionsConfirmInteraction(t *testing.T) {
	manager, root := newDeletionTestManager(t, &authorityProviders{})
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	call := droids.ToolContext{TurnID: "turn_one", ToolCallID: "call_one"}
	type answer struct {
		result session.ToolConfirmationResult
		err    error
	}
	answers := make(chan answer, 1)
	go func() {
		result, err := manager.ConfirmToolCall(t.Context(), created.ID, call, session.ToolConfirmation{
			Title: "Delete “Docs”?", Detail: "Docs · /repo\n\nThis can’t be undone.", ConfirmLabel: "Delete", CancelLabel: "Keep",
		})
		answers <- answer{result, err}
	}()

	var pending session.InteractionRequest
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := manager.Snapshot(t.Context(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.PendingInteractions) == 1 {
			pending = snapshot.PendingInteractions[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the confirmation never became pending")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pending.Kind != session.InteractionConfirm || pending.Title != "Delete “Docs”?" || pending.Detail != "Docs · /repo\n\nThis can’t be undone." ||
		pending.ConfirmLabel != "Delete" || pending.CancelLabel != "Keep" || pending.RunID != "turn_one" || pending.ToolCallID != "call_one" {
		t.Fatalf("pending interaction = %+v", pending)
	}
	confirmed := false
	if err := manager.RespondInteraction(t.Context(), created.ID, session.InteractionResponse{RequestID: pending.ID, Confirmed: &confirmed}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-answers:
		if got.err != nil || got.result != (session.ToolConfirmationResult{Confirmed: false, Resolution: "negative_confirmation"}) {
			t.Fatalf("ConfirmToolCall = %+v, %v", got.result, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ConfirmToolCall did not return")
	}
}

func TestConfirmToolCallRefusesASessionThatIsNotLoaded(t *testing.T) {
	manager, root := newDeletionTestManager(t, &authorityProviders{})
	created, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ConfirmToolCall(t.Context(), created.ID, droids.ToolContext{TurnID: "turn_one", ToolCallID: "call_one"}, session.ToolConfirmation{Title: "Delete?"}); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("ConfirmToolCall = %v", err)
	}
}

func TestDiscoverChildSessionsListsDirectChildrenWithTheirDescendants(t *testing.T) {
	manager, root := newDeletionTestManager(t, &authorityProviders{})
	parent, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Parent", Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Child", Model: "test/echo", ParentSessionID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Grandchild", Model: "test/echo", ParentSessionID: child.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Great-grandchild", Model: "test/echo", ParentSessionID: grandchild.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Unrelated", Model: "test/echo"}); err != nil {
		t.Fatal(err)
	}
	children, err := manager.DiscoverChildSessions(t.Context(), parent.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 {
		t.Fatalf("children = %+v", children)
	}
	got := children[0]
	if got.ID != child.ID || got.Name != "Child" || got.CWD != root || got.ParentSessionID != parent.ID || got.Availability != "idle" || got.Descendants == nil || *got.Descendants != 2 {
		t.Fatalf("child = %+v (descendants %v)", got, got.Descendants)
	}
}

func TestDeleteLeafKeepsASessionWithChildren(t *testing.T) {
	manager, root := newDeletionTestManager(t, &authorityProviders{})
	parent, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Parent", Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := manager.Create(t.Context(), session.CreateInput{CWD: root, Name: "Child", Model: "test/echo", ParentSessionID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteLeaf(t.Context(), parent.ID); !errors.Is(err, session.ErrSessionHasChildren) {
		t.Fatalf("DeleteLeaf(parent) = %v", err)
	}
	if _, err := manager.Get(t.Context(), parent.ID); err != nil {
		t.Fatalf("parent after refused delete: %v", err)
	}
	if err := manager.DeleteLeaf(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteLeaf(t.Context(), parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get(t.Context(), parent.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("parent after delete: %v", err)
	}
}
