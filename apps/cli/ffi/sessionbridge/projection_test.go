package sessionbridge

import (
	"reflect"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

func TestEventsProjectsScratchpadChanges(t *testing.T) {
	record := &protocol.Scratchpad{OwnerSessionID: "session_test", Content: "remote notes", Revision: 2}
	projected := Events([]protocol.SessionEvent{{
		StreamID:  "stream_test",
		Sequence:  1,
		SessionID: "session_test",
		Payload:   protocol.ScratchpadChangedEvent{Scratchpad: record},
	}})
	if len(projected) != 1 || projected[0].Scratchpad != record {
		t.Fatalf("Events() = %+v, want projected scratchpad", projected)
	}
}

func TestEventsProjectsWorkingDirectoryChanges(t *testing.T) {
	projected := Events([]protocol.SessionEvent{{
		StreamID:  "stream_test",
		Sequence:  1,
		SessionID: "session_test",
		Payload:   protocol.SessionCWDChangedEvent{Workspace: &protocol.WorkspaceRef{SessionID: "session_test", CWD: "/repo/app", WorkspaceID: "workspace_app"}},
	}})
	if len(projected) != 1 || projected[0].Kind != "session.cwd.changed" || projected[0].CWD != "/repo/app" || projected[0].WorkspaceID != "workspace_app" {
		t.Fatalf("Events() = %+v, want the new working directory", projected)
	}
}

func TestEventsProjectsSessionChangesAnotherClientMade(t *testing.T) {
	queue := &protocol.FollowUpQueue{Count: 1, Previews: []string{"next"}, AdmissionFailure: &protocol.FollowUpAdmissionFailure{Message: "attachment is unavailable"}}
	configuration := &protocol.SessionConfiguration{Model: "openai/gpt-5", ThinkingLevel: "high", ConfigurationRevision: 3}
	session := func(sequence int64, payload protocol.SessionEventPayload) protocol.SessionEvent {
		return protocol.SessionEvent{StreamID: "stream_test", Sequence: sequence, SessionID: "session_test", Payload: payload}
	}
	projected := Events([]protocol.SessionEvent{
		session(1, protocol.SessionNameChangedEvent{SessionName: "Renamed"}),
		session(2, protocol.SessionConfigurationChangedEvent{Configuration: configuration}),
		session(3, protocol.FollowUpsChangedEvent{FollowUps: queue}),
		session(4, protocol.CompactionStartedEvent{CompactionID: "compact_1"}),
		session(5, protocol.CompactionCompletedEvent{CompactionID: "compact_1", ErrorMessage: "Context compaction failed"}),
	})
	want := []Event{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Kind: "session.name.changed", SessionName: "Renamed"},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", Kind: "session.configuration.changed", Configuration: configuration},
		{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", Kind: "follow_ups.changed", FollowUps: queue},
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", Kind: "compaction.started", CompactionID: "compact_1"},
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", Kind: "compaction.completed", CompactionID: "compact_1", ErrorMessage: "Context compaction failed"},
	}
	if !reflect.DeepEqual(projected, want) {
		t.Fatalf("Events() = %+v\nwant %+v", projected, want)
	}
}

func TestEventsProjectsInteractionLifecycle(t *testing.T) {
	request := &protocol.InteractionRequest{ID: "interaction_1", Kind: protocol.InteractionConfirm, Title: "Proceed?"}
	projected := Events([]protocol.SessionEvent{
		{Sequence: 1, TurnID: "turn_1", Payload: protocol.InteractionRequestedEvent{Interaction: request}},
		{Sequence: 2, TurnID: "turn_1", Payload: protocol.InteractionResolvedEvent{InteractionID: "interaction_1", InteractionResolution: "answered"}},
	})
	if len(projected) != 2 {
		t.Fatalf("Events() returned %d events, want 2", len(projected))
	}
	if projected[0].Kind != "interaction.requested" || projected[0].Interaction != request || projected[0].InteractionID != "interaction_1" {
		t.Fatalf("requested = %+v", projected[0])
	}
	if projected[1].Kind != "interaction.resolved" || projected[1].Interaction != nil || projected[1].InteractionID != "interaction_1" {
		t.Fatalf("resolved = %+v", projected[1])
	}
}

func TestMessagesCarryTheirSessionSequence(t *testing.T) {
	t.Parallel()

	got := Messages([]protocol.TranscriptMessage{{
		ID: "message_1", TurnID: "turn_1", Sequence: 42, Role: "user",
		Content: []protocol.TranscriptContent{protocol.TextBlock("hello")},
	}})
	want := Message{
		ID: "message_1", TurnID: "turn_1", Sequence: 42, Role: "user",
		Content: []Content{{Kind: "text", Text: "hello"}},
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("Messages() = %#v, want %#v", got, []Message{want})
	}
}

func TestEventsProjectsPluginMessages(t *testing.T) {
	projected := Events([]protocol.SessionEvent{{
		Sequence: 2, TurnID: "turn_1",
		Payload: protocol.PluginMessageAddedEvent{PluginID: "autoresearch", Text: "Continue the experiment loop."},
	}})
	if len(projected) != 1 || projected[0].Kind != "plugin.message.added" || projected[0].PluginID != "autoresearch" || projected[0].Text != "Continue the experiment loop." {
		t.Fatalf("Events() = %+v, want the plugin message", projected)
	}
}

func TestEventsProjectsComposerCommandBoundaries(t *testing.T) {
	projected := Events([]protocol.SessionEvent{{
		Sequence: 3, SessionID: "session_1",
		Payload: protocol.BoundaryCreatedEvent{
			BoundaryID: "bash_1", BoundaryKind: "bash", BoundarySource: "composer", CreatedAt: "2026-10-09T10:00:01Z",
			Content: protocol.ContextContent{protocol.TextBlock("[bash from composer]"), protocol.TextBlock("[bash command: ls]\nREADME.md")},
			Details: boundaryDetails(t, map[string]any{"command": "ls", "status": "completed", "exitCode": 0}),
		},
	}, {
		Sequence: 4, SessionID: "session_1",
		Payload: protocol.BoundaryCreatedEvent{
			BoundaryID: "cwd:1", BoundaryKind: "cwd", BoundarySource: "user", CreatedAt: "2026-10-09T10:00:02Z",
			Content: protocol.ContextContent{protocol.TextBlock("[cwd from user]")},
		},
	}})
	want := Bash{ID: "bash_1", Command: "ls", Status: "completed", HasExitCode: true, Output: "README.md"}
	if len(projected) != 2 || projected[0].Kind != "boundary.created" || projected[0].Bash == nil || !reflect.DeepEqual(*projected[0].Bash, want) {
		t.Fatalf("Events() = %+v, want bash %+v", projected, want)
	}
	if projected[1].Kind != "boundary.created" || projected[1].Bash != nil {
		t.Fatalf("other boundary = %+v, want no bash", projected[1])
	}
}

func TestMessagesProjectPersistedPluginMessages(t *testing.T) {
	got := Messages([]protocol.TranscriptMessage{{
		ID: "pluginmsg_1", TurnID: "turn_1", Sequence: 1, Role: "context",
		BoundaryID: "pluginmsg_1", BoundaryKind: protocol.PluginMessageBoundaryKind, BoundarySource: "autoresearch",
		Content: []protocol.TranscriptContent{protocol.TextBlock("Continue the experiment loop.")},
	}})
	want := Message{
		ID: "pluginmsg_1", TurnID: "turn_1", Sequence: 1, Role: "plugin", PluginID: "autoresearch",
		Content: []Content{{Kind: "text", Text: "Continue the experiment loop."}},
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("Messages() = %#v, want %#v", got, []Message{want})
	}
}

func TestMessagesProjectAPromptCommandAsItsInvocation(t *testing.T) {
	got := Messages([]protocol.TranscriptMessage{{
		ID: "message_command", TurnID: "turn_command", Role: "user",
		Content: []protocol.TranscriptContent{protocol.NewTranscriptContent(protocol.PromptCommandContent{
			Name: "claude-fix", Arguments: "123 high", Source: protocol.PromptCommandSourceClaudeProject,
			Text: "Pretend to fix issue #123 at high priority.",
		})},
	}})
	want := []Content{{Kind: "promptCommand", Text: "/claude-fix 123 high"}}
	if len(got) != 1 || !reflect.DeepEqual(got[0].Content, want) {
		t.Fatalf("Messages() = %#v, want content %#v", got, want)
	}
}

func TestToolCallsCarryTheirArgumentsAndResultsTheirDetails(t *testing.T) {
	t.Parallel()

	messages := Messages([]protocol.TranscriptMessage{
		{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Content: []protocol.TranscriptContent{
			protocol.NewTranscriptContent(protocol.ToolCallContent{ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"a.go"}`}),
			protocol.NewTranscriptContent(protocol.ToolCallContent{ToolCallID: "call_2", ToolName: "write", ArgumentsTruncated: true}),
		}},
		{ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read", Details: []byte(`{"lines":2}`)},
	})
	calls := messages[0].Content
	if calls[0].Arguments != `{"path":"a.go"}` || calls[0].ArgumentsTruncated || !calls[1].ArgumentsTruncated {
		t.Fatalf("calls = %+v, want their arguments", calls)
	}
	if messages[1].Details != `{"lines":2}` {
		t.Fatalf("result details = %q", messages[1].Details)
	}

	events := Events([]protocol.SessionEvent{
		{Sequence: 1, TurnID: "turn_1", Payload: protocol.ToolStartedEvent{ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"a.go"}`}},
		{Sequence: 2, TurnID: "turn_1", Payload: protocol.ToolCompletedEvent{ToolCallID: "call_1", ToolName: "read", Details: []byte(`{"lines":2}`)}},
	})
	if events[0].Arguments != `{"path":"a.go"}` || events[1].Details != `{"lines":2}` {
		t.Fatalf("events = %+v, want the arguments and details", events)
	}
}
