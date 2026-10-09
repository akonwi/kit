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
