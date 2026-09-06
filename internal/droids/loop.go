package droids

import "fmt"

func toolErrorText(text string) ToolResult {
	result := ToolText(boundedDiagnostic(text))
	result.IsError = true
	return result
}

func errText(message AssistantMessage) string {
	if message.ErrorMessage != "" {
		return message.ErrorMessage
	}
	return string(message.StopReason)
}

func validateAssistantToolCalls(message AssistantMessage) error {
	calls := message.ToolCalls()
	if message.StopReason == StopReasonToolUse && len(calls) == 0 {
		return fmt.Errorf("droids: provider ended with tool use but returned no tool calls")
	}
	if message.StopReason != StopReasonToolUse {
		return nil
	}
	return validateToolCallIdentities(calls, "provider")
}

func validateToolCallIdentities(calls []ToolCall, source string) error {
	seen := make(map[ToolCallID]struct{}, len(calls))
	for index, call := range calls {
		if call.ID == "" {
			return fmt.Errorf("droids: %s tool call %d has an empty id", source, index)
		}
		if call.Name == "" {
			return fmt.Errorf("droids: %s tool call %d has an empty name", source, index)
		}
		if _, duplicate := seen[call.ID]; duplicate {
			return fmt.Errorf("droids: %s tool call %d has duplicate id %q", source, index, call.ID)
		}
		seen[call.ID] = struct{}{}
	}
	return nil
}
