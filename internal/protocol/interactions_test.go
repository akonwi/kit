package protocol

import (
	"strings"
	"testing"
)

func TestInteractionRequestRejectsTerminalControlText(t *testing.T) {
	t.Parallel()
	request := InteractionRequest{ID: "interaction_0123456789abcdef0123456789abcdef", SessionID: "session_0123456789abcdef0123456789abcdef", RunID: "run_0123456789abcdef0123456789abcdef", ToolCallID: "tool_0123456789abcdef0123456789abcdef", Kind: InteractionConfirm, Title: "Continue?\x1b[2J", CreatedAt: "2025-01-01T00:00:00Z"}
	if err := request.Validate(); err == nil {
		t.Fatal("Validate accepted terminal control text")
	}
}

func TestInteractionResponseValidatesGuidedAnswerShape(t *testing.T) {
	t.Parallel()
	value := "ok"
	response := InteractionResponse{RequestID: "interaction_0123456789abcdef0123456789abcdef", Answers: map[string]InteractionAnswer{"question_0123456789abcdef0123456789abcdef": {Text: &value}}}
	if err := response.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	response.Answers["question_0123456789abcdef0123456789abcdef"] = InteractionAnswer{Text: &value, OptionIDs: []string{"option_0123456789abcdef0123456789abcdef"}}
	if err := response.Validate(); err == nil || !strings.Contains(err.Error(), "answer") {
		t.Fatalf("invalid mixed answer error = %v", err)
	}
}
