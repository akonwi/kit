package session_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func newBashEventManager(t *testing.T) (*session.Manager, session.SessionRecord) {
	t.Helper()
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
	return manager, created
}

// bashEvents waits until the stream after cursor carries count bash events and
// returns them, in stream order.
func bashEvents(t *testing.T, manager *session.Manager, sessionID, streamID string, after int64, count int) []session.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var found []session.Event
	for len(found) < count {
		page, err := manager.WaitEvents(ctx, sessionID, streamID, after)
		if err != nil {
			t.Fatalf("WaitEvents() after %d found %d of %d bash events: %v", after, len(found), count, err)
		}
		if page.ResyncRequired {
			t.Fatal("stream requires resynchronization")
		}
		for _, event := range page.Events {
			if event.Kind == session.EventBashStarted || event.Kind == session.EventBashCompleted {
				found = append(found, event)
			}
			after = event.Sequence
		}
	}
	return found
}

func settleBash(t *testing.T, manager *session.Manager, sessionID string, execution session.BashExecution) session.BashExecution {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for execution.Status == session.BashExecutionRunning {
		if time.Now().After(deadline) {
			t.Fatalf("bash %s did not settle", execution.ID)
		}
		time.Sleep(5 * time.Millisecond)
		var err error
		if execution, err = manager.GetBash(t.Context(), sessionID, execution.ID); err != nil {
			t.Fatal(err)
		}
	}
	return execution
}

func TestBashCommandsAnnounceStartAndCompletionInOrder(t *testing.T) {
	manager, created := newBashEventManager(t)
	before, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	commands := []struct {
		id, command, output string
		exclude             bool
	}{
		{"bash_11111111111111111111111111111111", "printf private", "private", true},
		{"bash_22222222222222222222222222222222", "printf shared", "shared", false},
	}
	for _, command := range commands {
		execution, err := manager.StartBash(t.Context(), created.ID, command.id, command.command, command.exclude)
		if err != nil {
			t.Fatal(err)
		}
		settleBash(t, manager, created.ID, execution)
	}
	// A repeated start of a known execution announces nothing.
	if _, err := manager.StartBash(t.Context(), created.ID, commands[0].id, commands[0].command, true); err != nil {
		t.Fatal(err)
	}
	events := bashEvents(t, manager, created.ID, before.EventStreamID, before.EventCursor, 4)
	type observed struct {
		kind    session.EventKind
		id      string
		status  session.BashExecutionStatus
		output  string
		exclude bool
		turn    string
	}
	var got []observed
	for _, event := range events {
		got = append(got, observed{event.Kind, event.Bash.ID, event.Bash.Status, event.Bash.Output, event.Bash.ExcludeFromContext, event.TurnID})
	}
	want := []observed{
		{session.EventBashStarted, commands[0].id, session.BashExecutionRunning, "", true, ""},
		{session.EventBashCompleted, commands[0].id, session.BashExecutionCompleted, "private", true, ""},
		{session.EventBashStarted, commands[1].id, session.BashExecutionRunning, "", false, ""},
		{session.EventBashCompleted, commands[1].id, session.BashExecutionCompleted, "shared", false, ""},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("bash events =\n%v\nwant\n%v", got, want)
	}
	page, err := manager.Events(t.Context(), created.ID, before.EventStreamID, events[3].Sequence)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Events {
		if event.Kind == session.EventBashStarted || event.Kind == session.EventBashCompleted {
			t.Fatalf("repeated start announced %s for %s", event.Kind, event.Bash.ID)
		}
	}
}

// Regression: a completion announced while holding the bash lock deadlocked
// against a snapshot, which takes the event log before the bash lock.
func TestSnapshotsDuringCompletionsDoNotDeadlock(t *testing.T) {
	manager, created := newBashEventManager(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	snapshots := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				snapshots <- nil
				return
			default:
			}
			if _, err := manager.Snapshot(ctx, created.ID); err != nil {
				snapshots <- err
				return
			}
		}
	}()
	for index := range 40 {
		id := fmt.Sprintf("bash_%032x", index+1)
		execution, err := manager.StartBash(ctx, created.ID, id, "true", true)
		if err != nil {
			t.Fatal(err)
		}
		settleBash(t, manager, created.ID, execution)
	}
	close(stop)
	if err := <-snapshots; err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
}

func TestIncludedBashBoundaryPrecedesItsCompletion(t *testing.T) {
	manager, created := newBashEventManager(t)
	before, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := manager.StartBash(t.Context(), created.ID, "bash_33333333333333333333333333333333", "printf included", false)
	if err != nil {
		t.Fatal(err)
	}
	settleBash(t, manager, created.ID, execution)
	completed := bashEvents(t, manager, created.ID, before.EventStreamID, before.EventCursor, 2)[1]
	page, err := manager.Events(t.Context(), created.ID, before.EventStreamID, before.EventCursor)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []session.EventKind
	for _, event := range page.Events {
		if event.Sequence > completed.Sequence {
			break
		}
		if event.Kind == session.EventBoundaryCreated && event.BoundaryID != execution.ID {
			continue
		}
		kinds = append(kinds, event.Kind)
	}
	want := []session.EventKind{session.EventBashStarted, session.EventBoundaryCreated, session.EventBashCompleted}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("event kinds = %v; want %v", kinds, want)
	}
}

func TestSnapshotListsTheCommandsTheTranscriptDoesNotRecord(t *testing.T) {
	manager, created := newBashEventManager(t)
	excluded, err := manager.StartBash(t.Context(), created.ID, "bash_44444444444444444444444444444444", "printf private", true)
	if err != nil {
		t.Fatal(err)
	}
	excluded = settleBash(t, manager, created.ID, excluded)
	included, err := manager.StartBash(t.Context(), created.ID, "bash_55555555555555555555555555555555", "printf shared", false)
	if err != nil {
		t.Fatal(err)
	}
	settleBash(t, manager, created.ID, included)
	running, err := manager.StartBash(t.Context(), created.ID, "bash_66666666666666666666666666666666", "sleep 30", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.AbortBash(context.Background(), created.ID, running.ID) })

	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	type listed struct {
		id     string
		status session.BashExecutionStatus
		output string
	}
	var got []listed
	for _, execution := range snapshot.BashExecutions {
		got = append(got, listed{execution.ID, execution.Status, execution.Output})
	}
	want := []listed{
		{excluded.ID, session.BashExecutionCompleted, "private"},
		{running.ID, session.BashExecutionRunning, ""},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) || snapshot.ActiveBashExecutionID != running.ID {
		t.Fatalf("snapshot bash executions = %v, active %q; want %v, active %q", got, snapshot.ActiveBashExecutionID, want, running.ID)
	}
}

func TestSnapshotKeepsTheMostRecentUnrecordedCommands(t *testing.T) {
	manager, created := newBashEventManager(t)
	var ids []string
	for index := range 34 {
		id := fmt.Sprintf("bash_%032x", index+1)
		execution, err := manager.StartBash(t.Context(), created.ID, id, "true", true)
		if err != nil {
			t.Fatal(err)
		}
		settleBash(t, manager, created.ID, execution)
		ids = append(ids, id)
	}
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, execution := range snapshot.BashExecutions {
		got = append(got, execution.ID)
	}
	if fmt.Sprint(got) != fmt.Sprint(ids[2:]) {
		t.Fatalf("snapshot bash executions = %v; want the 32 most recent %v", got, ids[2:])
	}
}

func TestSnapshotListsCommandsFromItsOldestMessage(t *testing.T) {
	manager, created := newBashEventManager(t)
	older, err := manager.StartBash(t.Context(), created.ID, "bash_77777777777777777777777777777777", "printf older", true)
	if err != nil {
		t.Fatal(err)
	}
	settleBash(t, manager, created.ID, older)
	for turn := range 30 {
		if _, err := manager.RunPrompt(t.Context(), created.ID, fmt.Sprintf("turn %d", turn)); err != nil {
			t.Fatal(err)
		}
	}
	newer, err := manager.StartBash(t.Context(), created.ID, "bash_88888888888888888888888888888888", "printf newer", true)
	if err != nil {
		t.Fatal(err)
	}
	settleBash(t, manager, created.ID, newer)
	snapshot, err := manager.Snapshot(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.HasMoreMessages {
		t.Fatal("snapshot lists the whole transcript")
	}
	if len(snapshot.BashExecutions) != 1 || snapshot.BashExecutions[0].ID != newer.ID {
		t.Fatalf("snapshot bash executions = %+v; want only %s", snapshot.BashExecutions, newer.ID)
	}
}
