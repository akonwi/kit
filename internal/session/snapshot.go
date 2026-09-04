package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

// TranscriptMessage is a renderer-neutral projection of one persisted message.
type TranscriptMessage struct {
	ID        string
	TurnID    string
	Sequence  int64
	Role      string
	Text      string
	Thinking  string
	ToolName  string
	IsError   bool
	CreatedAt time.Time
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
		projected := TranscriptMessage{
			ID: messageRecord.ID, TurnID: messageRecord.TurnID,
			Sequence: messageRecord.Sequence, Role: messageRecord.Role,
			Text: transcriptText(message), CreatedAt: messageRecord.CreatedAt,
		}
		if tool, ok := message.(droids.ToolResultMessage); ok {
			projected.ToolName = tool.ToolName
			projected.IsError = tool.IsError
		}
		if assistant, ok := message.(droids.AssistantMessage); ok {
			_, projected.Thinking = assistantPresentation(assistant)
			if assistant.StopReason == droids.StopReasonError || assistant.StopReason == droids.StopReasonAborted {
				projected.IsError = true
				if projected.Text == "" {
					projected.Text = assistant.ErrorMessage
				}
			}
			if assistant.Usage.TotalTokens > 0 {
				snapshot.ContextTokens = assistant.Usage.TotalTokens
			}
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

func transcriptText(message droids.Message) string {
	switch typed := message.(type) {
	case droids.UserMessage:
		return contentText(typed.Content)
	case droids.AssistantMessage:
		return typed.Text()
	case droids.ToolResultMessage:
		return contentText(typed.Content)
	default:
		return ""
	}
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
