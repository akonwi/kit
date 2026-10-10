package sessionbridge

import (
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

func TestSubagentPagesProjectAsSessionEvents(t *testing.T) {
	got := subagentPage(protocol.SubagentLiveEventPage{
		StreamID: "substream_a", FirstSequence: 4, LastSequence: 7,
		Events: []protocol.SubagentLiveEvent{
			{Sequence: 4, Kind: "message.thinking.delta", TurnID: "turn_a", MessageID: "m1", Delta: "Plan"},
			{Sequence: 5, Kind: "tool.planned", TurnID: "turn_a", ToolCallID: "c1", ToolName: "read"},
			{Sequence: 6, Kind: "tool.completed", TurnID: "turn_a", ToolCallID: "c1", ToolName: "read", Text: "body", IsError: true},
			{Sequence: 7, Kind: "turn.settled", TurnID: "turn_a"},
		},
	})
	if got.StreamID != "substream_a" || got.LastSequence != 7 || len(got.Events) != 4 {
		t.Fatalf("page = %+v", got)
	}
	kinds := []string{}
	for _, event := range got.Events {
		if event.StreamID != "substream_a" || event.TurnID != "turn_a" {
			t.Fatalf("event identity = %+v", event)
		}
		kinds = append(kinds, event.Kind)
	}
	if want := "assistant.thinking.delta tool.planned tool.completed turn.completed"; strings.Join(kinds, " ") != want {
		t.Fatalf("kinds = %q, want %q", strings.Join(kinds, " "), want)
	}
	thinking, completed := got.Events[0], got.Events[2]
	if thinking.MessageID != "m1" || thinking.Delta != "Plan" || thinking.Sequence != 4 {
		t.Fatalf("thinking = %+v", thinking)
	}
	if len(completed.Content) != 1 || completed.Content[0].Kind != "text" || completed.Content[0].Text != "body" || !completed.IsError {
		t.Fatalf("completed = %+v", completed)
	}
}

func TestSubagentChangesNameTheirConversation(t *testing.T) {
	got := Events([]protocol.SessionEvent{{Payload: protocol.SubagentChangedEvent{SubagentConversationID: "subagent_a"}}})
	if len(got) != 1 || got[0].Kind != "subagent.changed" || got[0].SubagentConversationID != "subagent_a" {
		t.Fatalf("events = %+v", got)
	}
}
