package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func validTranscriptSnapshot() SessionSnapshot {
	return SessionSnapshot{
		Session: SessionInfo{
			ID: "session_1", CWD: "/workspace", Model: "test/model",
			CreatedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano),
			UpdatedAt: time.Unix(2, 0).UTC().Format(time.RFC3339Nano),
		},
		Messages: []TranscriptMessage{
			{
				ID: "message_1", TurnID: "turn_1", Sequence: 0, Role: "assistant",
				Content: []TranscriptContent{
					{Kind: TranscriptContentText, Text: "Inspecting."},
					{Kind: TranscriptContentToolCall, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
				},
				StopReason: "toolUse", CreatedAt: time.Unix(3, 0).UTC().Format(time.RFC3339Nano),
			},
			{
				ID: "message_2", TurnID: "turn_1", Sequence: 1, Role: "tool",
				Content:    []TranscriptContent{{Kind: TranscriptContentText, Text: "contents"}},
				ToolCallID: "call_1", ToolName: "read", Details: json.RawMessage(`{"lines":1}`),
				CreatedAt: time.Unix(4, 0).UTC().Format(time.RFC3339Nano),
			},
		},
	}
}

func TestSessionSnapshotAllowsStandaloneBashInsideParentTimeline(t *testing.T) {
	t.Parallel()
	snapshot := validTranscriptSnapshot()
	snapshot.Messages[1].Sequence = 2
	exitCode := 0
	started := time.Unix(3, 500).UTC().Format(time.RFC3339Nano)
	snapshot.Messages = append(snapshot.Messages[:1], append([]TranscriptMessage{{
		ID: "bash_1", Sequence: 1, Role: "bash", CreatedAt: started,
		Bash: &BashExecution{
			ID: "bash_1", SessionID: snapshot.Session.ID, Sequence: 1,
			Command: "git status", Status: BashExecutionCompleted,
			ExitCode: &exitCode, StartedAt: started, CompletedAt: started,
		},
	}}, snapshot.Messages[1:]...)...)
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionSnapshotValidatesStructuredTranscript(t *testing.T) {
	t.Parallel()

	snapshot := validTranscriptSnapshot()
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	snapshot.Messages[0].Content[1].Arguments = ""
	snapshot.Messages[0].Content[1].ArgumentsTruncated = true
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() rejected explicit argument truncation: %v", err)
	}
}

func TestSessionSnapshotRejectsInvalidStructuredTranscript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*SessionSnapshot)
	}{
		{
			name: "tool call arguments are missing",
			mutate: func(snapshot *SessionSnapshot) {
				snapshot.Messages[0].Content[1].Arguments = ""
			},
		},
		{
			name: "tool result has no call identity",
			mutate: func(snapshot *SessionSnapshot) {
				snapshot.Messages[1].ToolCallID = ""
			},
		},
		{
			name: "assistant error state disagrees with stop reason",
			mutate: func(snapshot *SessionSnapshot) {
				snapshot.Messages[0].IsError = true
			},
		},
		{
			name: "message id is duplicated",
			mutate: func(snapshot *SessionSnapshot) {
				snapshot.Messages[1].ID = snapshot.Messages[0].ID
			},
		},
		{
			name: "turn id is reused noncontiguously",
			mutate: func(snapshot *SessionSnapshot) {
				result := snapshot.Messages[1]
				result.Sequence = 2
				snapshot.Messages = []TranscriptMessage{
					snapshot.Messages[0],
					{
						ID: "message_other", TurnID: "turn_2", Sequence: 1, Role: "user",
						Content:   []TranscriptContent{{Kind: TranscriptContentText, Text: "other"}},
						CreatedAt: time.Unix(4, 0).UTC().Format(time.RFC3339Nano),
					},
					result,
				}
			},
		},
		{
			name: "tool result is orphaned",
			mutate: func(snapshot *SessionSnapshot) {
				snapshot.Messages = snapshot.Messages[1:]
			},
		},
		{
			name: "tool result name mismatches call",
			mutate: func(snapshot *SessionSnapshot) {
				snapshot.Messages[1].ToolName = "write"
			},
		},
		{
			name: "tool call id is duplicated",
			mutate: func(snapshot *SessionSnapshot) {
				snapshot.Messages[0].Content = append(snapshot.Messages[0].Content, snapshot.Messages[0].Content[1])
			},
		},
		{
			name: "tool details are invalid JSON",
			mutate: func(snapshot *SessionSnapshot) {
				snapshot.Messages[1].Details = json.RawMessage(`{`)
			},
		},
		{
			name: "text block carries tool metadata",
			mutate: func(snapshot *SessionSnapshot) {
				snapshot.Messages[0].Content[0].ToolName = "read"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := validTranscriptSnapshot()
			test.mutate(&snapshot)
			if err := snapshot.Validate(); err == nil {
				t.Fatal("Validate() accepted invalid transcript")
			}
		})
	}
}
