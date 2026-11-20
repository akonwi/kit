package session

import "time"

func testInteractionRequest(id string) InteractionRequest {
	return InteractionRequest{ID: id, SessionID: "session_0123456789abcdef0123456789abcdef", RunID: "run_0123456789abcdef0123456789abcdef", ToolCallID: "tool_0123456789abcdef0123456789abcdef", Kind: InteractionConfirm, Title: "Continue?", CreatedAt: time.Now().UTC()}
}
