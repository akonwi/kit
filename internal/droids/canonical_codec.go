package droids

import (
	"encoding/json"
	"fmt"
	"time"
)

const maxToolDetailsBytes = 64 << 10

type wireMessageEnvelope struct {
	ID             MessageID      `json:"id"`
	ConversationID ConversationID `json:"conversation_id"`
	TurnID         TurnID         `json:"turn_id"`
	CreatedAt      time.Time      `json:"created_at"`
	Message        wireMessage    `json:"message"`
}

type wireMessage struct {
	Role           Role            `json:"role"`
	Content        []wireContent   `json:"content,omitempty"`
	Provider       string          `json:"provider,omitempty"`
	Model          string          `json:"model,omitempty"`
	ResponseModel  string          `json:"response_model,omitempty"`
	ResponseID     string          `json:"response_id,omitempty"`
	ProviderScope  string          `json:"provider_scope,omitempty"`
	Usage          Usage           `json:"usage,omitempty"`
	StopReason     StopReason      `json:"stop_reason,omitempty"`
	ErrorKind      ErrorKind       `json:"error_kind,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	Error          *ProviderError  `json:"error,omitempty"`
	ToolCallID     ToolCallID      `json:"tool_call_id,omitempty"`
	ProviderCallID string          `json:"provider_call_id,omitempty"`
	ToolName       string          `json:"tool_name,omitempty"`
	BoundaryID     string          `json:"boundary_id,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
	IsError        bool            `json:"is_error,omitempty"`
	Terminate      bool            `json:"terminate,omitempty"`
}

type wireContent struct {
	Type           string     `json:"type"`
	Text           string     `json:"text,omitempty"`
	Thinking       string     `json:"thinking,omitempty"`
	Signature      string     `json:"signature,omitempty"`
	Redacted       bool       `json:"redacted,omitempty"`
	Filename       string     `json:"filename,omitempty"`
	MediaType      string     `json:"media_type,omitempty"`
	URL            string     `json:"url,omitempty"`
	AttachmentID   string     `json:"attachment_id,omitempty"`
	ID             ToolCallID `json:"id,omitempty"`
	ProviderCallID string     `json:"provider_call_id,omitempty"`
	Name           string     `json:"name,omitempty"`
	Arguments      []byte     `json:"arguments,omitempty"`
}

func encodeMessageEnvelope(envelope MessageEnvelope) ([]byte, error) {
	wire, err := messageEnvelopeToWire(envelope)
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire)
}

func decodeMessageEnvelope(data []byte) (MessageEnvelope, error) {
	var wire wireMessageEnvelope
	if err := json.Unmarshal(data, &wire); err != nil {
		return MessageEnvelope{}, fmt.Errorf("droids: decode message envelope: %w", err)
	}
	return messageEnvelopeFromWire(wire)
}

func messageEnvelopeToWire(envelope MessageEnvelope) (wireMessageEnvelope, error) {
	message, err := messageToWire(envelope.Message)
	if err != nil {
		return wireMessageEnvelope{}, err
	}
	return wireMessageEnvelope{
		ID: envelope.ID, ConversationID: envelope.ConversationID, TurnID: envelope.TurnID,
		CreatedAt: envelope.CreatedAt, Message: message,
	}, nil
}

func messageEnvelopeFromWire(wire wireMessageEnvelope) (MessageEnvelope, error) {
	if wire.ID == "" || wire.ConversationID == "" || wire.TurnID == "" || wire.CreatedAt.IsZero() {
		return MessageEnvelope{}, fmt.Errorf("droids: message envelope identity and timestamp are required")
	}
	message, err := messageFromWire(wire.Message)
	if err != nil {
		return MessageEnvelope{}, err
	}
	return MessageEnvelope{
		ID: wire.ID, ConversationID: wire.ConversationID, TurnID: wire.TurnID,
		CreatedAt: wire.CreatedAt, Message: message,
	}, nil
}

func messageToWire(message Message) (wireMessage, error) {
	switch value := message.(type) {
	case UserMessage:
		content, err := contentToWire(value.Content)
		return wireMessage{Role: RoleUser, Content: content}, err
	case AssistantMessage:
		content, err := contentToWire(value.Content)
		providerError := cloneProviderError(value.Error)
		if providerError != nil {
			providerError.Message = boundedDiagnostic(providerError.Message)
		}
		return wireMessage{
			Role: RoleAssistant, Content: content, Provider: value.Provider, Model: value.Model,
			ResponseModel: value.ResponseModel, ResponseID: value.ResponseID,
			ProviderScope: value.ProviderScope, Usage: value.Usage, StopReason: value.StopReason,
			ErrorKind: value.ErrorKind, ErrorMessage: boundedDiagnostic(value.ErrorMessage), Error: providerError,
		}, err
	case ToolResultMessage:
		content, err := contentToWire(value.Content)
		if err == nil && len(value.Details) > 0 && !json.Valid(value.Details) {
			err = fmt.Errorf("droids: tool details are not valid JSON")
		}
		if err == nil && len(value.Details) > maxToolDetailsBytes {
			err = fmt.Errorf("droids: tool details exceed %d bytes", maxToolDetailsBytes)
		}
		return wireMessage{
			Role: RoleToolResult, Content: content, ToolCallID: value.ToolCallID,
			ProviderCallID: value.ProviderCallID, ToolName: value.ToolName,
			Details: append(json.RawMessage(nil), value.Details...),
			IsError: value.IsError, Terminate: value.Terminate,
		}, err
	case ContextMessage:
		content, err := contentToWire(value.Content)
		if err == nil && len(value.Details) > 0 && !json.Valid(value.Details) {
			err = fmt.Errorf("droids: context details are not valid JSON")
		}
		if err == nil && len(value.Details) > maxToolDetailsBytes {
			err = fmt.Errorf("droids: context details exceed %d bytes", maxToolDetailsBytes)
		}
		return wireMessage{
			Role: RoleContext, Content: content, ToolName: value.Kind, Provider: value.Source,
			BoundaryID: value.BoundaryID, Details: append(json.RawMessage(nil), value.Details...),
		}, err
	default:
		return wireMessage{}, fmt.Errorf("droids: unsupported message type %T", message)
	}
}

func messageFromWire(wire wireMessage) (Message, error) {
	switch wire.Role {
	case RoleUser:
		content, err := inputContentFromWire(wire.Content)
		return UserMessage{Content: content}, err
	case RoleAssistant:
		content, err := assistantContentFromWire(wire.Content)
		if err != nil {
			return nil, err
		}
		return AssistantMessage{
			Content: content, Provider: wire.Provider, Model: wire.Model,
			ResponseModel: wire.ResponseModel, ResponseID: wire.ResponseID,
			ProviderScope: wire.ProviderScope, Usage: wire.Usage, StopReason: wire.StopReason,
			ErrorKind: wire.ErrorKind, ErrorMessage: wire.ErrorMessage, Error: cloneProviderError(wire.Error),
		}, nil
	case RoleToolResult:
		content, err := resultContentFromWire(wire.Content)
		if err != nil {
			return nil, err
		}
		return ToolResultMessage{
			ToolCallID: wire.ToolCallID, ProviderCallID: wire.ProviderCallID,
			ToolName: wire.ToolName, Content: content,
			Details: append(json.RawMessage(nil), wire.Details...),
			IsError: wire.IsError, Terminate: wire.Terminate,
		}, nil
	case RoleContext:
		content, err := inputContentFromWire(wire.Content)
		return ContextMessage{
			BoundaryID: wire.BoundaryID, Kind: wire.ToolName, Source: wire.Provider,
			Content: content, Details: append(json.RawMessage(nil), wire.Details...),
		}, err
	default:
		return nil, fmt.Errorf("droids: unsupported message role %q", wire.Role)
	}
}

func contentToWire[T any](content []T) ([]wireContent, error) {
	out := make([]wireContent, 0, len(content))
	for _, block := range content {
		switch value := any(block).(type) {
		case TextInput:
			out = append(out, wireContent{Type: "text", Text: value.Text, AttachmentID: value.AttachmentID, Filename: value.Filename, MediaType: value.MediaType})
		case FileInput:
			if _, err := NewFileInputURL(value.Filename, value.MediaType, value.URL); err != nil {
				return nil, fmt.Errorf("droids: invalid file input: %w", err)
			}
			out = append(out, wireContent{Type: "file", Filename: value.Filename, MediaType: value.MediaType, URL: value.URL, AttachmentID: value.AttachmentID})
		case TextContent:
			out = append(out, wireContent{Type: "text", Text: value.Text, Signature: value.Signature})
		case ThinkingContent:
			out = append(out, wireContent{Type: "thinking", Thinking: value.Thinking, Signature: value.Signature, Redacted: value.Redacted})
		case FileContent:
			var err error
			if isImageMediaType(value.MediaType) {
				err = validateImageContent(value)
			} else {
				err = validateFileContent(value)
			}
			if err != nil {
				return nil, fmt.Errorf("droids: invalid file result: %w", err)
			}
			out = append(out, wireContent{Type: "file", Filename: value.Filename, MediaType: value.MediaType, URL: value.URL, AttachmentID: value.AttachmentID})
		case ToolCall:
			out = append(out, wireContent{
				Type: "tool_call", ID: value.ID, ProviderCallID: value.ProviderCallID,
				Name: value.Name, Arguments: append([]byte(nil), value.Arguments...),
				Signature: value.Signature,
			})
		default:
			return nil, fmt.Errorf("droids: unsupported content type %T", block)
		}
	}
	return out, nil
}

func inputContentFromWire(content []wireContent) ([]InputContent, error) {
	out := make([]InputContent, 0, len(content))
	for _, block := range content {
		switch block.Type {
		case "text":
			out = append(out, TextInput{Text: block.Text, AttachmentID: block.AttachmentID, Filename: block.Filename, MediaType: block.MediaType})
		case "file":
			out = append(out, FileInput{Filename: block.Filename, MediaType: block.MediaType, URL: block.URL, AttachmentID: block.AttachmentID})
		default:
			return nil, fmt.Errorf("droids: unsupported input content kind %q", block.Type)
		}
	}
	return out, nil
}

func assistantContentFromWire(content []wireContent) ([]AssistantContent, error) {
	out := make([]AssistantContent, 0, len(content))
	for _, block := range content {
		switch block.Type {
		case "text":
			out = append(out, TextContent{Text: block.Text, Signature: block.Signature})
		case "thinking":
			out = append(out, ThinkingContent{Thinking: block.Thinking, Signature: block.Signature, Redacted: block.Redacted})
		case "tool_call":
			out = append(out, ToolCall{
				ID: block.ID, ProviderCallID: block.ProviderCallID, Name: block.Name,
				Arguments: append([]byte(nil), block.Arguments...), Signature: block.Signature,
			})
		default:
			return nil, fmt.Errorf("droids: unsupported assistant content kind %q", block.Type)
		}
	}
	return out, nil
}

func resultContentFromWire(content []wireContent) ([]ResultContent, error) {
	out := make([]ResultContent, 0, len(content))
	for _, block := range content {
		switch block.Type {
		case "text":
			out = append(out, TextContent{Text: block.Text, Signature: block.Signature})
		case "file":
			out = append(out, FileContent{Filename: block.Filename, MediaType: block.MediaType, URL: block.URL, AttachmentID: block.AttachmentID})
		default:
			return nil, fmt.Errorf("droids: unsupported result content kind %q", block.Type)
		}
	}
	return out, nil
}

func cloneProviderError(value *ProviderError) *ProviderError {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func encodeDetails(details any) (json.RawMessage, error) {
	if details == nil {
		return nil, nil
	}
	if raw, ok := details.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return nil, fmt.Errorf("droids: tool details are not valid JSON")
		}
		if len(raw) > maxToolDetailsBytes {
			return nil, fmt.Errorf("droids: tool details exceed %d bytes", maxToolDetailsBytes)
		}
		return append(json.RawMessage(nil), raw...), nil
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("droids: encode tool details: %w", err)
	}
	if len(raw) > maxToolDetailsBytes {
		return nil, fmt.Errorf("droids: tool details exceed %d bytes", maxToolDetailsBytes)
	}
	return raw, nil
}
