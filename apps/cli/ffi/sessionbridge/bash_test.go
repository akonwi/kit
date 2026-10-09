package sessionbridge

import (
	"encoding/json"
	"reflect"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

func exitCode(value int) *int { return &value }

func TestBashOfShowsNoticesAndTheFailureWithoutOutput(t *testing.T) {
	timedOut := BashOf(protocol.BashExecution{
		ID: "bash_1", Command: "sleep 9", Status: protocol.BashExecutionCompleted, Output: "tick\n",
		TimedOut: true, Truncated: true, ExitCode: exitCode(124),
	})
	want := Bash{ID: "bash_1", Command: "sleep 9", Status: "completed", ExitCode: 124, HasExitCode: true, TimedOut: true, Output: "tick\n[timed out]\n[output truncated]"}
	if !reflect.DeepEqual(timedOut, want) || timedOut.Succeeded() {
		t.Fatalf("timed out = %+v", timedOut)
	}
	failed := BashOf(protocol.BashExecution{ID: "bash_2", Command: "x", Status: protocol.BashExecutionFailed, ErrorMessage: "no shell"})
	if failed.Output != "no shell" || failed.HasExitCode {
		t.Fatalf("failed = %+v", failed)
	}
	ok := BashOf(protocol.BashExecution{ID: "bash_3", Command: "true", Status: protocol.BashExecutionCompleted, ExitCode: exitCode(0)})
	if !ok.Succeeded() || ok.Running() {
		t.Fatalf("ok = %+v", ok)
	}
}

func boundaryDetails(t *testing.T, details map[string]any) json.RawMessage {
	t.Helper()
	details["version"] = 1
	details["startedAt"] = "2026-10-09T10:00:00Z"
	details["completedAt"] = "2026-10-09T10:00:01Z"
	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPersistedBashRunsBecomeBashMessages(t *testing.T) {
	messages := Messages([]protocol.TranscriptMessage{{
		ID: "msg_1", Sequence: 4, Role: "context", BoundaryID: "bash_1", BoundaryKind: "bash", BoundarySource: "composer",
		Content: []protocol.TranscriptContent{protocol.TextBlock("[bash command: ls\n-la] (exit code: 2)\nmissing\n[output truncated]")},
		Details: boundaryDetails(t, map[string]any{"command": "ls\n-la", "status": "completed", "exitCode": 2}),
	}, {
		ID: "msg_2", Sequence: 5, Role: "context", BoundaryKind: "note",
		Content: []protocol.TranscriptContent{protocol.TextBlock("other context")},
	}})
	if messages[0].Role != "bash" || messages[0].Bash == nil || messages[1].Role != "context" || messages[1].Bash != nil {
		t.Fatalf("messages = %+v", messages)
	}
	want := Bash{ID: "bash_1", Command: "ls\n-la", Status: "completed", ExitCode: 2, HasExitCode: true, Output: "missing\n[output truncated]"}
	if !reflect.DeepEqual(*messages[0].Bash, want) {
		t.Fatalf("bash = %+v, want %+v", *messages[0].Bash, want)
	}
}

func TestPendingBashShowsTheFailureInPlaceOfItsNotice(t *testing.T) {
	pending := PendingBash([]protocol.PendingBoundary{{
		ID: "bash_2", Kind: "bash", Source: "composer",
		Content: []protocol.TranscriptContent{protocol.TextBlock("[bash command: make]\n[aborted: aborted by user]")},
		Details: boundaryDetails(t, map[string]any{"command": "make", "status": "aborted", "errorMessage": "aborted by user"}),
	}, {ID: "note_1", Kind: "note"}})
	want := []Bash{{ID: "bash_2", Command: "make", Status: "aborted", Output: "aborted by user"}}
	if !reflect.DeepEqual(pending, want) {
		t.Fatalf("pending = %+v, want %+v", pending, want)
	}
}
