package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

const persistedMessageVersion = 1

type persistedMessage struct {
	Version   int                `json:"version"`
	Type      string             `json:"type"`
	Timestamp int64              `json:"timestamp"`
	Content   []persistedContent `json:"content"`

	Provider      string          `json:"provider,omitempty"`
	Model         string          `json:"model,omitempty"`
	ResponseModel string          `json:"responseModel,omitempty"`
	ResponseID    string          `json:"responseId,omitempty"`
	ProviderScope string          `json:"providerScope,omitempty"`
	Usage         *persistedUsage `json:"usage,omitempty"`
	StopReason    string          `json:"stopReason,omitempty"`
	ErrorKind     string          `json:"errorKind,omitempty"`
	ErrorMessage  string          `json:"errorMessage,omitempty"`

	ToolCallID string `json:"toolCallId,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	Details    any    `json:"details,omitempty"`
	IsError    bool   `json:"isError,omitempty"`
}

type persistedContent struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Redacted  bool   `json:"redacted,omitempty"`
	Filename  string `json:"filename,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	URL       string `json:"url,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments []byte `json:"arguments,omitempty"`
}

type persistedUsage struct {
	Input       int                `json:"input"`
	Output      int                `json:"output"`
	CacheRead   int                `json:"cacheRead"`
	CacheWrite  int                `json:"cacheWrite"`
	Reasoning   int                `json:"reasoning"`
	TotalTokens int                `json:"totalTokens"`
	Cost        persistedUsageCost `json:"cost"`
}

type persistedUsageCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

func encodeDroidMessage(message droids.Message) (string, []byte, time.Time, error) {
	payload := persistedMessage{Version: persistedMessageVersion}
	var role string
	switch typed := message.(type) {
	case droids.UserMessage:
		role = "user"
		payload.Type = role
		payload.Timestamp = typed.Timestamp
		content, err := encodeContent(typed.Content)
		if err != nil {
			return "", nil, time.Time{}, err
		}
		payload.Content = content
	case droids.AssistantMessage:
		role = "assistant"
		payload.Type = role
		payload.Timestamp = typed.Timestamp
		content, err := encodeContent(typed.Content)
		if err != nil {
			return "", nil, time.Time{}, err
		}
		payload.Content = content
		payload.Provider = typed.Provider
		payload.Model = typed.Model
		payload.ResponseModel = typed.ResponseModel
		payload.ResponseID = typed.ResponseID
		payload.ProviderScope = typed.ProviderScope
		payload.StopReason = string(typed.StopReason)
		payload.ErrorKind = string(typed.ErrorKind)
		payload.ErrorMessage = typed.ErrorMessage
		payload.Usage = encodeUsage(typed.Usage)
	case droids.ToolResultMessage:
		role = "tool"
		payload.Type = role
		payload.Timestamp = typed.Timestamp
		content, err := encodeContent(typed.Content)
		if err != nil {
			return "", nil, time.Time{}, err
		}
		payload.Content = content
		payload.ToolCallID = typed.ToolCallID
		payload.ToolName = typed.ToolName
		payload.Details = typed.Details
		payload.IsError = typed.IsError
	default:
		return "", nil, time.Time{}, fmt.Errorf("unsupported droids message %T", message)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, time.Time{}, fmt.Errorf("encode %s message: %w", role, err)
	}
	createdAt := time.UnixMilli(payload.Timestamp).UTC()
	if payload.Timestamp <= 0 {
		createdAt = time.Now().UTC()
	}
	return role, body, createdAt, nil
}

func decodeDroidMessage(role string, body []byte) (droids.Message, error) {
	var payload persistedMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode persisted message: %w", err)
	}
	if payload.Version != persistedMessageVersion {
		return nil, fmt.Errorf("unsupported persisted message version %d", payload.Version)
	}
	if payload.Type != role {
		return nil, fmt.Errorf("persisted message type %q does not match role %q", payload.Type, role)
	}
	content, err := decodeContent(payload.Content)
	if err != nil {
		return nil, err
	}
	switch role {
	case "user":
		return droids.UserMessage{Content: content, Timestamp: payload.Timestamp}, nil
	case "assistant":
		usage := droids.Usage{}
		if payload.Usage != nil {
			usage = decodeUsage(*payload.Usage)
		}
		return droids.AssistantMessage{
			Content:       content,
			Provider:      payload.Provider,
			Model:         payload.Model,
			ResponseModel: payload.ResponseModel,
			ResponseID:    payload.ResponseID,
			ProviderScope: payload.ProviderScope,
			Usage:         usage,
			StopReason:    droids.StopReason(payload.StopReason),
			ErrorKind:     droids.ErrorKind(payload.ErrorKind),
			ErrorMessage:  payload.ErrorMessage,
			Timestamp:     payload.Timestamp,
		}, nil
	case "tool":
		return droids.ToolResultMessage{
			ToolCallID: payload.ToolCallID,
			ToolName:   payload.ToolName,
			Content:    content,
			Details:    payload.Details,
			IsError:    payload.IsError,
			Timestamp:  payload.Timestamp,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported persisted message role %q", role)
	}
}

func encodeContent(content []droids.Content) ([]persistedContent, error) {
	encoded := make([]persistedContent, 0, len(content))
	for _, block := range content {
		switch typed := block.(type) {
		case droids.TextContent:
			encoded = append(encoded, persistedContent{Type: "text", Text: typed.Text, Signature: typed.Signature})
		case droids.ThinkingContent:
			encoded = append(encoded, persistedContent{
				Type: "thinking", Thinking: typed.Thinking,
				Signature: typed.Signature, Redacted: typed.Redacted,
			})
		case droids.ImageContent:
			encoded = append(encoded, persistedContent{Type: "image", MediaType: typed.MediaType, URL: typed.URL})
		case droids.FileContent:
			encoded = append(encoded, persistedContent{
				Type: "file", Filename: typed.Filename, MediaType: typed.MediaType, URL: typed.URL,
			})
		case droids.ToolCall:
			encoded = append(encoded, persistedContent{
				Type: "toolCall", ID: typed.ID, Name: typed.Name,
				Arguments: append([]byte(nil), typed.Arguments...), Signature: typed.Signature,
			})
		default:
			return nil, fmt.Errorf("unsupported droids content %T", block)
		}
	}
	return encoded, nil
}

func decodeContent(content []persistedContent) ([]droids.Content, error) {
	decoded := make([]droids.Content, 0, len(content))
	for _, block := range content {
		switch block.Type {
		case "text":
			decoded = append(decoded, droids.TextContent{Text: block.Text, Signature: block.Signature})
		case "thinking":
			decoded = append(decoded, droids.ThinkingContent{
				Thinking: block.Thinking, Signature: block.Signature, Redacted: block.Redacted,
			})
		case "image":
			decoded = append(decoded, droids.ImageContent{MediaType: block.MediaType, URL: block.URL})
		case "file":
			decoded = append(decoded, droids.FileContent{
				Filename: block.Filename, MediaType: block.MediaType, URL: block.URL,
			})
		case "toolCall":
			decoded = append(decoded, droids.ToolCall{
				ID: block.ID, Name: block.Name, Arguments: append([]byte(nil), block.Arguments...),
				Signature: block.Signature,
			})
		default:
			return nil, fmt.Errorf("unsupported persisted content type %q", block.Type)
		}
	}
	return decoded, nil
}

func encodeUsage(usage droids.Usage) *persistedUsage {
	return &persistedUsage{
		Input:       usage.Input,
		Output:      usage.Output,
		CacheRead:   usage.CacheRead,
		CacheWrite:  usage.CacheWrite,
		Reasoning:   usage.Reasoning,
		TotalTokens: usage.TotalTokens,
		Cost: persistedUsageCost{
			Input: usage.Cost.Input, Output: usage.Cost.Output,
			CacheRead: usage.Cost.CacheRead, CacheWrite: usage.Cost.CacheWrite,
			Total: usage.Cost.Total,
		},
	}
}

func decodeUsage(usage persistedUsage) droids.Usage {
	return droids.Usage{
		Input:       usage.Input,
		Output:      usage.Output,
		CacheRead:   usage.CacheRead,
		CacheWrite:  usage.CacheWrite,
		Reasoning:   usage.Reasoning,
		TotalTokens: usage.TotalTokens,
		Cost: droids.UsageCost{
			Input: usage.Cost.Input, Output: usage.Cost.Output,
			CacheRead: usage.Cost.CacheRead, CacheWrite: usage.Cost.CacheWrite,
			Total: usage.Cost.Total,
		},
	}
}
