package droids

import (
	"strings"
	"testing"
)

func TestValidateAssistantToolCallsRejectsMalformedProviderOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message AssistantMessage
		want    string
	}{
		{
			name:    "tool use without calls",
			message: AssistantMessage{StopReason: StopReasonToolUse},
			want:    "returned no tool calls",
		},
		{
			name: "empty id",
			message: AssistantMessage{StopReason: StopReasonToolUse, Content: []Content{
				ToolCall{Name: "read", Arguments: []byte(`{}`)},
			}},
			want: "empty id",
		},
		{
			name: "empty name",
			message: AssistantMessage{StopReason: StopReasonToolUse, Content: []Content{
				ToolCall{ID: "call_1", Arguments: []byte(`{}`)},
			}},
			want: "empty name",
		},
		{
			name: "duplicate id",
			message: AssistantMessage{StopReason: StopReasonToolUse, Content: []Content{
				ToolCall{ID: "call_1", Name: "read", Arguments: []byte(`{}`)},
				ToolCall{ID: "call_1", Name: "write", Arguments: []byte(`{}`)},
			}},
			want: "duplicate id",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAssistantToolCalls(test.message)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateAssistantToolCalls() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateAssistantToolCallsAcceptsValidProviderOutput(t *testing.T) {
	t.Parallel()

	err := validateAssistantToolCalls(AssistantMessage{StopReason: StopReasonToolUse, Content: []Content{
		ToolCall{ID: "call_1", Name: "read", Arguments: []byte(`{"path":"README.md"}`)},
		ToolCall{ID: "call_2", Name: "grep", Arguments: []byte(`{"pattern":"TODO"}`)},
	}})
	if err != nil {
		t.Fatalf("validateAssistantToolCalls() error = %v", err)
	}
}
