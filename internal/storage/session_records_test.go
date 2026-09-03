package storage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestSessionRunAndMessageLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	session, err := store.CreateSession(ctx, NewSession{
		ID: "session-1", CWD: "/workspace", Name: "Test", Persistent: true,
		ModelProvider: "test", ModelID: "echo", ThinkingLevel: "low",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.ModelProvider != "test" || session.ModelID != "echo" || !session.Persistent {
		t.Fatalf("session = %+v", session)
	}

	turn, run, err := store.StartParentRun(ctx, session.ID, "turn-1", "run-1")
	if err != nil {
		t.Fatalf("StartParentRun() error = %v", err)
	}
	if turn.Sequence != 0 || turn.Status != RunStatusRunning || run.TurnID != turn.ID {
		t.Fatalf("turn = %+v, run = %+v", turn, run)
	}

	payloads := []NewMessageRecord{
		{Role: "user", PayloadJSON: json.RawMessage(`{"text":"hello"}`)},
		{Role: "assistant", PayloadJSON: json.RawMessage(`{"text":"hi"}`)},
	}
	records, err := store.AppendMessages(ctx, session.ID, turn.ID, payloads)
	if err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}
	if len(records) != 2 || records[0].Sequence != 0 || records[1].Sequence != 1 {
		t.Fatalf("message records = %+v", records)
	}
	if err := store.FinishParentRun(ctx, session.ID, turn.ID, run.ID, RunStatusCompleted, ""); err != nil {
		t.Fatalf("FinishParentRun() error = %v", err)
	}
	if _, err := store.AppendMessages(ctx, session.ID, turn.ID, payloads[:1]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("append to completed turn error = %v, want ErrNotFound", err)
	}

	turn2, _, err := store.StartParentRun(ctx, session.ID, "turn-2", "run-2")
	if err != nil {
		t.Fatalf("second StartParentRun() error = %v", err)
	}
	if turn2.Sequence != 1 {
		t.Fatalf("second turn sequence = %d, want 1", turn2.Sequence)
	}
	more, err := store.AppendMessages(ctx, session.ID, turn2.ID, payloads[:1])
	if err != nil {
		t.Fatalf("second AppendMessages() error = %v", err)
	}
	if more[0].Sequence != 2 {
		t.Fatalf("third message sequence = %d, want 2", more[0].Sequence)
	}
}

func TestReservedParentRunCanAbortBeforeExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if _, err := store.CreateSession(ctx, NewSession{
		ID: "session", CWD: "/workspace", Persistent: true,
		ModelProvider: "test", ModelID: "echo",
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	turn, run, err := store.ReserveParentRun(ctx, "session", "turn", "run")
	if err != nil {
		t.Fatalf("ReserveParentRun() error = %v", err)
	}
	if turn.Status != RunStatusPending || run.Status != RunStatusQueued {
		t.Fatalf("reservation = %+v / %+v", turn, run)
	}
	status, err := store.AbortReservedParentRun(ctx, "session", "run", "test abort")
	if err != nil {
		t.Fatalf("AbortReservedParentRun() error = %v", err)
	}
	if status != RunStatusAborted {
		t.Fatalf("abort status = %q", status)
	}
	turnID, status, err := store.StartReservedParentRun(ctx, "session", "run")
	if err != nil {
		t.Fatalf("StartReservedParentRun() error = %v", err)
	}
	if turnID != "turn" || status != RunStatusAborted {
		t.Fatalf("start after abort = %q/%q", turnID, status)
	}
}

func TestGetActiveParentRunPrefersRunningOverQueued(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.CreateSession(ctx, NewSession{
		ID: "session", CWD: "/workspace", Persistent: true,
		ModelProvider: "test", ModelID: "echo",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReserveParentRun(ctx, "session", "turn-running", "run-running"); err != nil {
		t.Fatal(err)
	}
	if _, status, err := store.StartReservedParentRun(ctx, "session", "run-running"); err != nil || status != RunStatusRunning {
		t.Fatalf("start running generation = %q, %v", status, err)
	}
	if _, _, err := store.ReserveParentRun(ctx, "session", "turn-queued", "run-queued"); err != nil {
		t.Fatal(err)
	}
	active, err := store.GetActiveParentRun(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != "run-running" {
		t.Fatalf("active run = %q, want running generation", active.ID)
	}
}

func TestListSessionsFiltersByCWD(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	for _, input := range []NewSession{
		{ID: "a", CWD: "/one", Persistent: true, ModelProvider: "test", ModelID: "echo"},
		{ID: "b", CWD: "/two", Persistent: true, ModelProvider: "test", ModelID: "echo"},
	} {
		if _, err := store.CreateSession(ctx, input); err != nil {
			t.Fatalf("CreateSession(%q) error = %v", input.ID, err)
		}
	}

	records, err := store.ListSessions(ctx, "/one")
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(records) != 1 || records[0].ID != "a" {
		t.Fatalf("sessions = %+v", records)
	}
}

func TestListSessionsOrdersRFC3339FractionsChronologically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	for _, id := range []string{"earlier", "later"} {
		if _, err := store.CreateSession(ctx, NewSession{
			ID: id, CWD: "/workspace", Persistent: true,
			ModelProvider: "test", ModelID: "echo",
		}); err != nil {
			t.Fatalf("CreateSession(%q) error = %v", id, err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `
		UPDATE sessions
		SET updated_at = CASE id
			WHEN 'earlier' THEN '2026-01-01T00:00:00.123Z'
			WHEN 'later' THEN '2026-01-01T00:00:00.1234Z'
		END
	`); err != nil {
		t.Fatalf("set mixed timestamp precision: %v", err)
	}
	records, err := store.ListSessions(ctx, "/workspace")
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(records) != 2 || records[0].ID != "later" {
		t.Fatalf("session order = %+v", records)
	}
}

func TestInterruptActiveRunsUnblocksSessionAndExcludesPartialReplay(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if _, err := store.CreateSession(ctx, NewSession{
		ID: "session", CWD: "/workspace", Persistent: true,
		ModelProvider: "test", ModelID: "echo",
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, _, err := store.StartParentRun(ctx, "session", "turn-stale", "run-stale"); err != nil {
		t.Fatalf("StartParentRun() error = %v", err)
	}
	if _, err := store.AppendMessages(ctx, "session", "turn-stale", []NewMessageRecord{{
		Role: "user", PayloadJSON: json.RawMessage(`{"partial":true}`),
	}}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}
	if _, _, err := store.ReserveParentRun(ctx, "session", "turn-queued", "run-queued"); err != nil {
		t.Fatalf("ReserveParentRun() error = %v", err)
	}

	recovered, err := store.InterruptActiveRuns(ctx, "test restart")
	if err != nil {
		t.Fatalf("InterruptActiveRuns() error = %v", err)
	}
	if recovered.ParentRuns != 2 || recovered.Turns != 2 {
		t.Fatalf("recovered = %+v", recovered)
	}
	recoveredAgain, err := store.InterruptActiveRuns(ctx, "test restart")
	if err != nil {
		t.Fatalf("second InterruptActiveRuns() error = %v", err)
	}
	if recoveredAgain.ParentRuns != 0 || recoveredAgain.Turns != 0 {
		t.Fatalf("second recovery = %+v", recoveredAgain)
	}
	if replay, err := store.ListReplayMessages(ctx, "session"); err != nil || len(replay) != 0 {
		t.Fatalf("ListReplayMessages() = %+v, %v; want no partial messages", replay, err)
	}
	turn, _, err := store.StartParentRun(ctx, "session", "turn-next", "run-next")
	if err != nil {
		t.Fatalf("next StartParentRun() error = %v", err)
	}
	if turn.Sequence != 2 {
		t.Fatalf("next turn sequence = %d, want 2", turn.Sequence)
	}
}

func TestOnlyOneParentRunCanBeActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if _, err := store.CreateSession(ctx, NewSession{
		ID: "session", CWD: "/workspace", Persistent: true,
		ModelProvider: "test", ModelID: "echo",
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, _, err := store.StartParentRun(ctx, "session", "turn-1", "run-1"); err != nil {
		t.Fatalf("first StartParentRun() error = %v", err)
	}
	if _, _, err := store.StartParentRun(ctx, "session", "turn-2", "run-2"); err == nil {
		t.Fatal("second active parent run was accepted")
	}
}
