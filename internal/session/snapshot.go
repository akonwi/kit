package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

// TranscriptContentKind identifies one ordered renderer-neutral content block.
type TranscriptContentKind string

const (
	TranscriptContentText     TranscriptContentKind = "text"
	TranscriptContentThinking TranscriptContentKind = "thinking"
	TranscriptContentToolCall TranscriptContentKind = "toolCall"
	TranscriptContentImage    TranscriptContentKind = "image"
	TranscriptContentFile     TranscriptContentKind = "file"
)

// TranscriptContent is one renderer-neutral projection of persisted content.
type TranscriptContent struct {
	Kind               TranscriptContentKind
	Text               string
	ToolCallID         string
	ToolName           string
	Arguments          string
	ArgumentsTruncated bool
	Filename           string
	MediaType          string
}

// TranscriptMessage is a renderer-neutral projection of one persisted message.
type TranscriptMessage struct {
	ID           string
	TurnID       string
	Sequence     int64
	Role         string
	Content      []TranscriptContent
	StopReason   string
	ErrorMessage string
	ToolCallID   string
	ToolName     string
	Details      json.RawMessage
	IsError      bool
	CreatedAt    time.Time
}

// Snapshot is an authoritative point-in-time view of one session.
type Snapshot struct {
	Session       SessionRecord
	Messages      []TranscriptMessage
	ActiveRunID   string
	ContextTokens int
	ContextWindow int
}

// Snapshot returns persisted presentation messages, including diagnostics from
// incomplete turns, plus current in-memory run and context state for one session.
func (m *Manager) Snapshot(ctx context.Context, sessionID string) (Snapshot, error) {
	if err := m.beginOperation(); err != nil {
		return Snapshot{}, err
	}
	defer m.ops.Done()
	if strings.TrimSpace(sessionID) == "" {
		return Snapshot{}, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	record, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return Snapshot{}, err
	}
	var stored []MessageRecord
	var activeRunID, activeTurnID string
	for {
		beforeRunID, _, err := m.durableActiveRun(ctx, sessionID)
		if err != nil {
			return Snapshot{}, err
		}
		stored, err = m.store.ListMessages(ctx, sessionID)
		if err != nil {
			return Snapshot{}, err
		}
		afterRunID, afterTurnID, err := m.durableActiveRun(ctx, sessionID)
		if err != nil {
			return Snapshot{}, err
		}
		if beforeRunID == afterRunID {
			activeRunID = afterRunID
			activeTurnID = afterTurnID
			break
		}
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
	}

	snapshot := Snapshot{Session: record, ActiveRunID: activeRunID, Messages: make([]TranscriptMessage, 0, len(stored))}
	for _, messageRecord := range stored {
		if activeTurnID != "" && messageRecord.TurnID == activeTurnID {
			continue
		}
		message, err := decodeDroidMessage(messageRecord.Role, messageRecord.PayloadJSON)
		if err != nil {
			return Snapshot{}, fmt.Errorf("decode message %q: %w", messageRecord.ID, err)
		}
		projected, err := projectTranscriptMessage(messageRecord, message)
		if err != nil {
			return Snapshot{}, fmt.Errorf("project message %q: %w", messageRecord.ID, err)
		}
		if assistant, ok := message.(droids.AssistantMessage); ok && assistant.Usage.TotalTokens > 0 {
			snapshot.ContextTokens = assistant.Usage.TotalTokens
		}
		snapshot.Messages = append(snapshot.Messages, projected)
	}
	model, ok := m.providers.Model(record.ModelProvider + "/" + record.ModelID)
	if ok {
		snapshot.ContextWindow = model.ContextWindow
	}

	return snapshot, nil
}

func (m *Manager) durableActiveRun(ctx context.Context, sessionID string) (string, string, error) {
	record, err := m.store.GetActiveParentRun(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return record.ID, record.TurnID, nil
}

func projectTranscriptMessage(record MessageRecord, message droids.Message) (TranscriptMessage, error) {
	content, err := projectTranscriptContent(message)
	if err != nil {
		return TranscriptMessage{}, err
	}
	projected := TranscriptMessage{
		ID: record.ID, TurnID: record.TurnID, Sequence: record.Sequence,
		Role: record.Role, Content: content, CreatedAt: record.CreatedAt,
	}
	switch typed := message.(type) {
	case droids.AssistantMessage:
		projected.StopReason = string(typed.StopReason)
		projected.ErrorMessage = typed.ErrorMessage
		projected.IsError = typed.StopReason == droids.StopReasonError || typed.StopReason == droids.StopReasonAborted
	case droids.ToolResultMessage:
		projected.ToolCallID = typed.ToolCallID
		projected.ToolName = typed.ToolName
		projected.IsError = typed.IsError
		if len(record.PayloadJSON) > 0 {
			projected.Details, err = persistedToolDetails(record.PayloadJSON)
			if err != nil {
				return TranscriptMessage{}, err
			}
		} else if typed.Details != nil {
			projected.Details, err = json.Marshal(typed.Details)
			if err != nil {
				return TranscriptMessage{}, fmt.Errorf("project tool details: %w", err)
			}
		}
	}
	return projected, nil
}

func persistedToolDetails(payload []byte) (json.RawMessage, error) {
	var envelope struct {
		Details json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("project persisted tool details: %w", err)
	}
	details := bytes.TrimSpace(envelope.Details)
	if len(details) == 0 || bytes.Equal(details, []byte("null")) {
		return nil, nil
	}
	return append(json.RawMessage(nil), details...), nil
}

func projectTranscriptContent(message droids.Message) ([]TranscriptContent, error) {
	var content []droids.Content
	switch typed := message.(type) {
	case droids.UserMessage:
		content = typed.Content
	case droids.AssistantMessage:
		content = typed.Content
	case droids.ToolResultMessage:
		content = typed.Content
	default:
		return nil, fmt.Errorf("unsupported message %T", message)
	}

	result := make([]TranscriptContent, 0, len(content))
	for _, block := range content {
		switch typed := block.(type) {
		case droids.TextContent:
			if typed.Text != "" {
				result = append(result, TranscriptContent{Kind: TranscriptContentText, Text: typed.Text})
			}
		case droids.ThinkingContent:
			if !typed.Redacted && typed.Thinking != "" {
				result = append(result, TranscriptContent{Kind: TranscriptContentThinking, Text: typed.Thinking})
			}
		case droids.ToolCall:
			arguments, truncated := presentationToolArguments(typed.Arguments)
			result = append(result, TranscriptContent{
				Kind: TranscriptContentToolCall, ToolCallID: typed.ID,
				ToolName: typed.Name, Arguments: arguments, ArgumentsTruncated: truncated,
			})
		case droids.ImageContent:
			result = append(result, TranscriptContent{
				Kind: TranscriptContentImage, MediaType: typed.MediaType,
			})
		case droids.FileContent:
			result = append(result, TranscriptContent{
				Kind: TranscriptContentFile, Filename: typed.Filename, MediaType: typed.MediaType,
			})
		default:
			return nil, fmt.Errorf("unsupported content %T", block)
		}
	}
	return result, nil
}

const maxPresentationToolArgumentsBytes = 64 << 10

func presentationToolArguments(raw []byte) (string, bool) {
	canonical, err := normalizeJSONObject(raw)
	if err != nil {
		canonical = []byte(strings.ToValidUTF8(string(raw), "�"))
	}
	if len(canonical) > maxPresentationToolArgumentsBytes {
		return "", true
	}
	return string(canonical), false
}

func normalizeJSONObject(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		if err == nil {
			err = fmt.Errorf("value is not an object")
		}
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode JSON object: %w", err)
	}
	return canonical, nil
}

func contentText(content []droids.Content) string {
	parts := make([]string, 0, len(content))
	for _, block := range content {
		switch typed := block.(type) {
		case droids.TextContent:
			if typed.Text != "" {
				parts = append(parts, typed.Text)
			}
		case droids.ImageContent:
			parts = append(parts, "[image]")
		case droids.FileContent:
			parts = append(parts, "[file: "+typed.Filename+"]")
		case droids.ToolCall:
			parts = append(parts, "[tool: "+typed.Name+"]")
		}
	}
	return strings.Join(parts, "\n")
}
