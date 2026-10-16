package contract

import (
	"strings"
	"testing"
)

func TestInteractionRequestRejectsTerminalControlText(t *testing.T) {
	t.Parallel()
	request := InteractionRequest{ID: "interaction_0123456789abcdef0123456789abcdef", SessionID: "session_0123456789abcdef0123456789abcdef", TurnID: "run_0123456789abcdef0123456789abcdef", ToolCallID: "tool_0123456789abcdef0123456789abcdef", Kind: InteractionConfirm, Title: "Continue?\x1b[2J", CreatedAt: "2025-01-01T00:00:00Z"}
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

func TestAuthorizeInteractionWireValidation(t *testing.T) {
	t.Parallel()
	valid := InteractionRequest{ID: "interaction_0123456789abcdef0123456789abcdef", SessionID: "session_0123456789abcdef0123456789abcdef", TurnID: "run_0123456789abcdef0123456789abcdef", ToolCallID: "tool_0123456789abcdef0123456789abcdef",
		Kind: InteractionAuthorize, Title: "Authorize the docs MCP server", URL: "https://auth.example.com/authorize?state=s", RedirectURI: "http://127.0.0.1:53173/mcp/callback", CreatedAt: "2025-01-01T00:00:00Z"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	for name, change := range map[string]func(*InteractionRequest){
		"page over http":     func(r *InteractionRequest) { r.URL = "http://auth.example.com/authorize" },
		"no page":            func(r *InteractionRequest) { r.URL = "" },
		"redirect elsewhere": func(r *InteractionRequest) { r.RedirectURI = "http://attacker.example:53173/cb" },
		"fields on input":    func(r *InteractionRequest) { r.Kind = InteractionInput },
	} {
		request := valid
		change(&request)
		if err := request.Validate(); err == nil {
			t.Fatalf("%s: Validate accepted the request", name)
		}
	}
}
