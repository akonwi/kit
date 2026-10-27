package contract

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
)

// Client-hosted plugin bounds (ADR 0051).
const (
	// MaxClientPluginTools bounds the tools one client instance declares.
	MaxClientPluginTools = 32
	// MaxClientPluginDeclarationBytes bounds an encoded attachment.
	MaxClientPluginDeclarationBytes = 256 << 10
	// MaxClientPluginCallInputBytes bounds the input of one call.
	MaxClientPluginCallInputBytes = 64 << 10
	// MaxClientPluginResultBytes bounds a result's decoded content.
	MaxClientPluginResultBytes = 4 << 20
	// MaxClientPluginResultParts bounds a result's content parts.
	MaxClientPluginResultParts = 64
	// MaxClientPluginErrorBytes bounds a client's failure message.
	MaxClientPluginErrorBytes = 8 << 10
)

var (
	clientPluginID     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	clientPluginToolID = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// ValidClientPluginInstanceID reports whether id is a client-generated
// instance identifier: "clientplugin_" and 32 lowercase hex digits.
func ValidClientPluginInstanceID(id string) bool {
	return identifier.Valid(id, "clientplugin_") && strings.ToLower(id) == id
}

// ValidClientPluginCallID reports whether id is a server-generated call
// identifier.
func ValidClientPluginCallID(id string) bool {
	return identifier.Valid(id, "clientcall_")
}

// ClientPluginToolDeclaration declares one tool a client instance hosts. Its
// fields and limits are those of a plugin tool registration; the server also
// checks inputSchema against the plugin tool schema subset.
type ClientPluginToolDeclaration struct {
	ID               string          `json:"id"`
	Label            string          `json:"label,omitempty"`
	Description      string          `json:"description"`
	InputSchema      json.RawMessage `json:"inputSchema"`
	ExecutionMode    string          `json:"executionMode,omitempty"`
	PromptSnippet    string          `json:"promptSnippet,omitempty"`
	PromptGuidelines []string        `json:"promptGuidelines,omitempty"`
}

// Validate checks the declaration's shape and bounds.
func (tool ClientPluginToolDeclaration) Validate() error {
	if !clientPluginToolID.MatchString(tool.ID) {
		return errors.New("tool id must match ^[a-z][a-z0-9_]*$")
	}
	if tool.Label != "" && !validRendererText(tool.Label, 512) {
		return errors.New("tool label is invalid")
	}
	if !validProtocolText(tool.Description, 8<<10) {
		return errors.New("tool description is required and bounded to 8 KiB")
	}
	if tool.ExecutionMode != "" && tool.ExecutionMode != "sequential" && tool.ExecutionMode != "parallel" {
		return errors.New("tool execution mode must be sequential or parallel")
	}
	if tool.PromptSnippet != "" && !validProtocolText(tool.PromptSnippet, 8<<10) {
		return errors.New("tool prompt snippet is bounded to 8 KiB")
	}
	if len(tool.PromptGuidelines) > 32 {
		return errors.New("a tool has at most 32 prompt guidelines")
	}
	for _, guideline := range tool.PromptGuidelines {
		if !validProtocolText(guideline, 1024) {
			return errors.New("tool prompt guidelines are bounded to 1 KiB")
		}
	}
	if !jsonObject(tool.InputSchema) {
		return errors.New("tool input schema must be a JSON object")
	}
	return nil
}

// AttachClientPluginInput declares or replaces a client instance's plugin ID
// and complete tool list.
type AttachClientPluginInput struct {
	PluginID string                        `json:"pluginId"`
	Tools    []ClientPluginToolDeclaration `json:"tools"`
}

// Validate checks the attachment's shape and bounds.
func (input AttachClientPluginInput) Validate() error {
	if !clientPluginID.MatchString(input.PluginID) {
		return errors.New("plugin id must match ^[a-z][a-z0-9-]{0,31}$")
	}
	if input.Tools == nil {
		return errors.New("tools are required")
	}
	if len(input.Tools) > MaxClientPluginTools {
		return fmt.Errorf("a client plugin declares at most %d tools", MaxClientPluginTools)
	}
	seen := make(map[string]struct{}, len(input.Tools))
	for index, tool := range input.Tools {
		if err := tool.Validate(); err != nil {
			return fmt.Errorf("tool %d: %w", index, err)
		}
		if _, duplicate := seen[tool.ID]; duplicate {
			return fmt.Errorf("tool %q is declared twice", tool.ID)
		}
		seen[tool.ID] = struct{}{}
		if len(input.PluginID)+2+len(tool.ID) > 64 {
			return fmt.Errorf("model-facing name of tool %q exceeds 64 characters", tool.ID)
		}
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > MaxClientPluginDeclarationBytes {
		return errors.New("client plugin declaration exceeds 256 KiB")
	}
	return nil
}

// ClientPluginContent is one part of a client tool result: text, or a base64
// PNG, JPEG, GIF, or WebP image.
type ClientPluginContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

// CompleteClientPluginCallInput answers one call: content (possibly empty)
// with optional details, or an error message.
type CompleteClientPluginCallInput struct {
	Content []ClientPluginContent `json:"content,omitempty"`
	Details json.RawMessage       `json:"details,omitempty"`
	Error   string                `json:"error,omitempty"`
}

// Validate checks the answer's shape and bounds.
func (input CompleteClientPluginCallInput) Validate() error {
	if input.Error != "" {
		if len(input.Content) != 0 || len(input.Details) != 0 || !validProtocolText(input.Error, MaxClientPluginErrorBytes) {
			return errors.New("an error answer carries only a message of at most 8 KiB")
		}
		return nil
	}
	if len(input.Content) > MaxClientPluginResultParts {
		return fmt.Errorf("a result has at most %d content parts", MaxClientPluginResultParts)
	}
	total := 0
	for index, part := range input.Content {
		switch part.Type {
		case "text":
			if part.Data != "" || part.MIMEType != "" || !utf8.ValidString(part.Text) || strings.IndexByte(part.Text, 0) >= 0 {
				return fmt.Errorf("content %d: text content carries only text", index)
			}
			total += len(part.Text)
		case "image":
			switch part.MIMEType {
			case "image/png", "image/jpeg", "image/gif", "image/webp":
			default:
				return fmt.Errorf("content %d: unsupported image type", index)
			}
			decoded, err := base64.StdEncoding.Strict().DecodeString(part.Data)
			if err != nil || len(decoded) == 0 || part.Text != "" {
				return fmt.Errorf("content %d: image content requires base64 data", index)
			}
			total += len(decoded)
		default:
			return fmt.Errorf("content %d: content type must be text or image", index)
		}
		if total > MaxClientPluginResultBytes {
			return errors.New("result content exceeds 4 MiB")
		}
	}
	if len(input.Details) > 0 && (len(input.Details) > 64<<10 || !json.Valid(input.Details)) {
		return errors.New("result details must be JSON of at most 64 KiB")
	}
	return nil
}

// ClientPluginCallResolution is how a client tool call ended.
type ClientPluginCallResolution string

// Client tool call outcomes.
const (
	ClientPluginCallCompleted   ClientPluginCallResolution = "completed"
	ClientPluginCallFailed      ClientPluginCallResolution = "failed"
	ClientPluginCallTimedOut    ClientPluginCallResolution = "timed_out"
	ClientPluginCallWithdrawn   ClientPluginCallResolution = "withdrawn"
	ClientPluginCallUnavailable ClientPluginCallResolution = "unavailable"
)

// EnumValues lists the call outcomes.
func (ClientPluginCallResolution) EnumValues() []string {
	return []string{"completed", "failed", "timed_out", "withdrawn", "unavailable"}
}

func (resolution ClientPluginCallResolution) valid() bool {
	for _, value := range resolution.EnumValues() {
		if string(resolution) == value {
			return true
		}
	}
	return false
}

// ClientPluginCall is the work a client_plugin.call.requested event asks a
// client instance to do. TurnID is set when the session's primary agent made
// the call and SubagentConversationID when a subagent did.
type ClientPluginCall struct {
	PluginID               string          `json:"pluginId"`
	ToolID                 string          `json:"toolId"`
	ToolCallID             string          `json:"toolCallId"`
	TurnID                 string          `json:"turnId,omitempty"`
	SubagentConversationID string          `json:"subagentConversationId,omitempty"`
	Input                  json.RawMessage `json:"input"`
}

// Validate checks the call's identity and input bounds.
func (call ClientPluginCall) Validate() error {
	if !clientPluginID.MatchString(call.PluginID) || !clientPluginToolID.MatchString(call.ToolID) || !validProtocolText(call.ToolCallID, 512) {
		return errors.New("client plugin call identity is invalid")
	}
	if (call.TurnID == "") == (call.SubagentConversationID == "") || len(call.TurnID) > 128 || len(call.SubagentConversationID) > 128 {
		return errors.New("client plugin call names exactly one calling agent")
	}
	if len(call.Input) > MaxClientPluginCallInputBytes || !jsonObject(call.Input) {
		return errors.New("client plugin call input must be a JSON object of at most 64 KiB")
	}
	return nil
}

func (call ClientPluginCall) payloadBytes() int {
	return len(call.PluginID) + len(call.ToolID) + len(call.ToolCallID) + len(call.TurnID) + len(call.SubagentConversationID) + len(call.Input)
}

func jsonObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(trimmed, &object) == nil
}
