package pluginprotocol

import "encoding/json"

// CommandExecuteParams runs one of the plugin's commands.
type CommandExecuteParams struct {
	ID   string `json:"id" schema:"def=LocalID"`
	Args string `json:"args" doc:"The literal text after the command, or empty."`
}

// ToolExecuteParams runs one of the plugin's tools with input Kit validated
// against the tool's schema.
type ToolExecuteParams struct {
	ID         string          `json:"id" schema:"def=ToolID"`
	ToolCallID string          `json:"toolCallId" schema:"minLength=1"`
	Input      json.RawMessage `json:"input" schema:"object"`
}

// ToolExecuteResult is a tool's output.
type ToolExecuteResult struct {
	Content   []ToolContent   `json:"content" schema:"maxItems=64"`
	Details   json.RawMessage `json:"details,omitempty" doc:"Any JSON, at most 64 KiB, kept with the call but not shown to the model."`
	Terminate *bool           `json:"terminate,omitempty" doc:"End the turn after this tool's batch completes."`
}

// ToolContent is one part of a tool result: text, or a base64 image.
type ToolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

// JSONSchema describes a text or image tool result part.
func (ToolContent) JSONSchema() map[string]any {
	return map[string]any{"oneOf": []any{
		map[string]any{"type": "object", "required": []string{"type", "text"}, "additionalProperties": false,
			"properties": map[string]any{"type": map[string]any{"const": "text"}, "text": map[string]any{"type": "string"}}},
		map[string]any{"type": "object", "required": []string{"type", "data", "mimeType"}, "additionalProperties": false,
			"properties": map[string]any{"type": map[string]any{"const": "image"},
				"data":     map[string]any{"type": "string", "minLength": 1, "contentEncoding": "base64"},
				"mimeType": map[string]any{"enum": []string{"image/png", "image/jpeg", "image/gif", "image/webp"}}}},
	}}
}

// BeforeExecuteParams asks an interceptor about one tool call.
type BeforeExecuteParams struct {
	ToolCall ToolCall `json:"toolCall"`
}

// ToolCall is a tool call the model made.
type ToolCall struct {
	ID    string          `json:"id" schema:"minLength=1"`
	Name  string          `json:"name" schema:"minLength=1" doc:"The model-facing tool name, such as bash or plugin__tool."`
	Input json.RawMessage `json:"input" schema:"object"`
}

// ToolCallDecision is an interceptor's answer.
type ToolCallDecision struct {
	Action  DecisionAction `json:"action"`
	Message string         `json:"message,omitempty"`
}

// DecisionAction is what an interceptor decided.
type DecisionAction string

// Interceptor decisions.
const (
	DecisionAllow             DecisionAction = "allow"
	DecisionRejectAndContinue DecisionAction = "reject-and-continue"
)

// JSONSchema describes an allow or a rejection with a message for the model.
func (ToolCallDecision) JSONSchema() map[string]any {
	return map[string]any{"oneOf": []any{
		map[string]any{"type": "object", "required": []string{"action"}, "additionalProperties": false,
			"properties": map[string]any{"action": map[string]any{"const": string(DecisionAllow)}}},
		map[string]any{"type": "object", "required": []string{"action", "message"}, "additionalProperties": false,
			"properties": map[string]any{"action": map[string]any{"const": string(DecisionRejectAndContinue)},
				"message": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192, "pattern": mustTextPattern(TextMultiline),
					"description": "Shown to the model as the call's result. At most 8 KiB."}}},
	}}
}

// CancelParams cancels a request the sender sent earlier.
type CancelParams struct {
	ID RequestID `json:"id"`
}
