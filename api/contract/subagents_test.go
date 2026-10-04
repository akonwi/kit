package contract_test

import (
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
)

func TestConfigureSubagentValidation(t *testing.T) {
	model := "test/model"
	thinking := protocol.ThinkingHigh
	valid := []protocol.ConfigureSubagentInput{
		{Generation: 1, Model: &model},
		{Generation: 1, ThinkingLevel: &thinking},
		{Generation: 1, Model: &model, ThinkingLevel: &thinking},
	}
	for _, input := range valid {
		if err := input.Validate(); err != nil {
			t.Fatalf("valid input %#v: %v", input, err)
		}
	}
	invalidModel := "model"
	invalidThinking := protocol.ThinkingLevel("extreme")
	for _, input := range []protocol.ConfigureSubagentInput{
		{}, {Generation: 1}, {Model: &model},
		{Generation: 1, Model: &invalidModel},
		{Generation: 1, ThinkingLevel: &invalidThinking},
	} {
		if err := input.Validate(); err == nil {
			t.Fatalf("invalid input accepted: %#v", input)
		}
	}
	result := protocol.ConfigureSubagentResult{Conversation: protocol.SubagentConversation{
		ID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AgentName: "scout",
		Model: model, ThinkingLevel: "high", State: "idle", Generation: 1,
		UpdatedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano),
	}}
	if err := result.ValidateApplied(protocol.ConfigureSubagentInput{Generation: 1, Model: &model, ThinkingLevel: &thinking}); err != nil {
		t.Fatal(err)
	}
}

func TestSubagentOperationInputValidation(t *testing.T) {
	valid := []protocol.SubagentOperationInput{
		{Action: protocol.SubagentListAgents},
		{Action: protocol.SubagentStart, Agent: "scout", Message: "inspect"},
		{Action: protocol.SubagentMessage, ConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Message: "continue"},
		{Action: protocol.SubagentInspect, TaskID: "task_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{Action: protocol.SubagentWait, TaskID: "task_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", TimeoutMS: 1000},
		{Action: protocol.SubagentCancel, TaskID: "task_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Generation: 1},
		{Action: protocol.SubagentDismiss, Agent: "scout", Generation: 1},
	}
	for _, input := range valid {
		if err := input.Validate(); err != nil {
			t.Errorf("Validate(%#v) = %v", input, err)
		}
	}
	invalid := []protocol.SubagentOperationInput{
		{},
		{Action: protocol.SubagentStart, Agent: "scout"},
		{Action: protocol.SubagentMessage, Agent: "scout", ConversationID: "also", Message: "bad"},
		{Action: protocol.SubagentWait, TaskID: "task_cccccccccccccccccccccccccccccccc", TimeoutMS: 30_001},
		{Action: protocol.SubagentCancel, TaskID: "task_cccccccccccccccccccccccccccccccc"},
		{Action: protocol.SubagentDismiss, Agent: "scout"},
		{Action: protocol.SubagentStart, Agent: "scout", Message: strings.Repeat("x", (128<<10)+1)},
	}
	for _, input := range invalid {
		if err := input.Validate(); err == nil {
			t.Errorf("Validate(%#v) succeeded", input)
		}
	}
}

func TestSubagentLiveEventPageValidation(t *testing.T) {
	page := protocol.SubagentLiveEventPage{
		StreamID: "substream_dddddddddddddddddddddddddddddddd", FirstSequence: 1, LastSequence: 2,
		Events: []protocol.SubagentLiveEvent{
			{Sequence: 1, Kind: "message.text.delta", MessageID: "message_1", Delta: "hello"},
			{Sequence: 2, Kind: "tool.started", ToolCallID: "call_1", ToolName: "read"},
		},
	}
	if err := page.Validate(); err != nil {
		t.Fatal(err)
	}
	page.Events[1].Sequence = 3
	if err := page.Validate(); err == nil {
		t.Fatal("event page accepted a sequence gap")
	}
}

func TestSubagentOperationResultValidation(t *testing.T) {
	now := time.Now().Format(time.RFC3339Nano)
	result := protocol.SubagentOperationResult{
		Definitions: []protocol.SubagentDefinition{{
			Name: "scout", Description: "inspects repositories", Model: "production-scout",
			Source: protocol.SubagentSource{Kind: "user", Path: "/tmp/scout.md"},
		}},
		Warning: "Requested model unavailable; using active model.",
		Conversations: []protocol.SubagentConversation{{
			ID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AgentName: "scout", Model: "test/echo", ThinkingLevel: "medium", State: "running",
			Generation: 1, UpdatedAt: now,
			Tasks: []protocol.SubagentTask{{ID: "task_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Sequence: 1, State: "queued", CancellationGeneration: 1, QueuedAt: now}},
		}},
	}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
	result.Conversations[0].ThinkingLevel = ""
	if err := result.Validate(); err == nil {
		t.Fatal("result accepted a missing subagent thinking level")
	}
	result.Conversations[0].ThinkingLevel = "extreme"
	if err := result.Validate(); err == nil {
		t.Fatal("result accepted an invalid subagent thinking level")
	}
	result.Conversations[0].ThinkingLevel = "medium"
	result.Conversations[0].Tasks[0].State = "mystery"
	if err := result.Validate(); err == nil {
		t.Fatal("result accepted an invalid task state")
	}
}
