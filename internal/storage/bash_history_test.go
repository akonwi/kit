package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
)

func TestAnInterruptedCommandStaysUnrecordedWithItsOutput(t *testing.T) {
	t.Parallel()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_bash", ScratchpadOwnerID: "session_bash", CWD: t.TempDir(), Persistent: true, ModelProvider: "test", ModelID: "model",
	}); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	completed := started.Add(time.Second)
	exitCode := 0
	if err := store.AppendBashHistory(t.Context(), session.BashHistoryEntry{Execution: session.BashExecution{
		ID: "bash_1", SessionID: "session_bash", Sequence: 1, Command: "ls", CWD: "/tmp",
		Status: session.BashExecutionCompleted, Output: "a.go", ExitCode: &exitCode,
		StartedAt: started, CompletedAt: &completed,
	}}); err != nil {
		t.Fatal(err)
	}
	unrecorded, err := store.RecentUnrecordedBashHistory(t.Context(), "session_bash", time.Time{}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrecorded) != 0 {
		t.Fatalf("an included command is unrecorded before it is marked: %+v", unrecorded)
	}

	if err := store.MarkBashHistoryInterrupted(t.Context(), "session_bash", "bash_1", "persist bash boundary: closed"); err != nil {
		t.Fatal(err)
	}
	unrecorded, err = store.RecentUnrecordedBashHistory(t.Context(), "session_bash", time.Time{}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrecorded) != 1 {
		t.Fatalf("unrecorded = %+v, want bash_1", unrecorded)
	}
	got := unrecorded[0]
	if got.ID != "bash_1" || got.Status != session.BashExecutionInterrupted || got.ErrorMessage != "persist bash boundary: closed" ||
		got.Output != "a.go" || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("marked execution = %+v", got)
	}

	if err := store.MarkBashHistoryInterrupted(t.Context(), "session_bash", "bash_2", "x"); err == nil {
		t.Fatal("marking an unrecorded execution succeeded")
	}
}
