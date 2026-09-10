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

type TranscriptContentKind string

const (
	TranscriptContentText     TranscriptContentKind = "text"
	TranscriptContentThinking TranscriptContentKind = "thinking"
	TranscriptContentToolCall TranscriptContentKind = "toolCall"
	TranscriptContentImage    TranscriptContentKind = "image"
	TranscriptContentFile     TranscriptContentKind = "file"
)

type TranscriptContent struct {
	Kind               TranscriptContentKind `json:"kind"`
	Text               string                `json:"text,omitempty"`
	ToolCallID         string                `json:"toolCallId,omitempty"`
	ToolName           string                `json:"toolName,omitempty"`
	Arguments          string                `json:"arguments,omitempty"`
	ArgumentsTruncated bool                  `json:"argumentsTruncated,omitempty"`
	Filename           string                `json:"filename,omitempty"`
	MediaType          string                `json:"mediaType,omitempty"`
}

type TranscriptMessage struct {
	ID             string
	TurnID         string
	Sequence       int64
	Role           string
	Content        []TranscriptContent
	StopReason     string
	ErrorMessage   string
	ToolCallID     string
	ToolName       string
	BoundaryID     string
	BoundaryKind   string
	BoundarySource string
	Details        json.RawMessage
	IsError        bool
	CreatedAt      time.Time
}

type PendingBoundary struct {
	ID         string
	Kind       string
	Source     string
	Content    []TranscriptContent
	Details    json.RawMessage
	AcceptedAt time.Time
}

type PromptCommand struct {
	Name        string
	Description string
	Source      string
	Location    string
}

type SessionUsage struct {
	Input       int
	Output      int
	CacheRead   int
	CacheWrite  int
	Reasoning   int
	TotalTokens int
	Cost        SessionUsageCost
}

type SessionUsageCost struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
	Total      float64
}

type Snapshot struct {
	Session               SessionRecord
	Messages              []TranscriptMessage
	Boundaries            []PendingBoundary
	ActiveRunID           string
	ActiveBashExecutionID string
	EventStreamID         string
	EventCursor           int64
	EventReplayFrom       int64
	EventReplayAvailable  bool
	ContextTokens         int
	ContextWindow         int
	Usage                 SessionUsage
	PromptCommands        []PromptCommand
	FollowUps             FollowUpQueue
	Warnings              []string
}

// Snapshot projects canonical droid history directly. While a turn is active,
// its history is omitted because the runtime event stream owns its live view.
func (m *Manager) Snapshot(ctx context.Context, sessionID string) (Snapshot, error) {
	if err := m.beginOperation(); err != nil {
		return Snapshot{}, err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(sessionID) == "" {
		return Snapshot{}, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return Snapshot{}, err
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	record, err := m.sessionRecordAtWorkspace(ctx, sessionID, loaded.workspace)
	if err != nil {
		return Snapshot{}, err
	}
	loaded.events.mu.Lock()
	defer loaded.events.mu.Unlock()
	activeRunID := loaded.activeRun
	completeActiveStream := activeRunID != "" && loaded.runs[activeRunID] != nil &&
		loaded.runs[activeRunID].completeStream && loaded.events.replayAvailable &&
		len(loaded.events.events) > 0 && loaded.events.events[0].Kind == EventRunStarted &&
		loaded.events.events[0].RunID == activeRunID
	droidSnapshot, err := loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		return Snapshot{}, err
	}
	result := Snapshot{
		Session: record, ActiveRunID: activeRunID,
		EventStreamID: loaded.events.streamID, EventCursor: loaded.events.next - 1,
		EventReplayAvailable: completeActiveStream,
		ContextTokens:        droidSnapshot.Context.Usage.EstimatedInput,
		ContextWindow:        droidSnapshot.Context.Usage.ContextWindow,
		Usage:                projectSessionUsage(droidSnapshot.Usage),
		FollowUps:            projectFollowUpQueue(loaded.followUps),
		Warnings:             append([]string(nil), loaded.configurationWarnings...),
	}
	if loaded.bundle.PromptCommands != nil {
		for _, command := range loaded.bundle.PromptCommands.Commands() {
			result.PromptCommands = append(result.PromptCommands, PromptCommand{
				Name: command.Name, Description: command.Description,
				Source: string(command.Source), Location: command.Location,
			})
		}
	}
	m.bashMu.Lock()
	if active := m.bashActive[sessionID]; active != nil {
		result.ActiveBashExecutionID = active.id
	}
	m.bashMu.Unlock()
	for _, pending := range droidSnapshot.Pending.Boundaries {
		content, err := projectDroidContent(pending.Message.Content)
		if err != nil {
			return Snapshot{}, err
		}
		result.Boundaries = append(result.Boundaries, PendingBoundary{
			ID: pending.Message.ID, Kind: pending.Message.Kind, Source: pending.Message.Source,
			Content: content, Details: append(json.RawMessage(nil), pending.Message.Details...),
			AcceptedAt: pending.AcceptedAt,
		})
	}
	var cursor uint64
	var sequence int64
	for {
		page, err := loaded.droid.History(ctx, droids.HistoryQuery{After: cursor, Limit: 1000})
		if err != nil {
			return Snapshot{}, err
		}
		for _, envelope := range page.Messages {
			if completeActiveStream && string(envelope.TurnID) == activeRunID {
				continue
			}
			message, err := projectTranscriptMessage(envelope, sequence)
			if err != nil {
				return Snapshot{}, fmt.Errorf("project message %q: %w", envelope.ID, err)
			}
			result.Messages = append(result.Messages, message)
			sequence++
		}
		cursor = page.Next
		if !page.HasMore {
			break
		}
	}
	return result, nil
}

// sessionRecordAtWorkspace reads a record at a stable workspace publication.
// Persistence is prepared before the lock-free workspace state is published,
// so a concurrent model tool can briefly expose a newer record. Retry that
// narrow window rather than blocking readers on mutation I/O.
func (m *Manager) sessionRecordAtWorkspace(ctx context.Context, sessionID string, workspace *workspaceScope) (SessionRecord, error) {
	cwd, generation := workspace.snapshot()
	record, err := m.sessionRecord(ctx, sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	afterCWD, afterGeneration := workspace.snapshot()
	if cwd == afterCWD && generation == afterGeneration && record.CWD == afterCWD {
		return record, nil
	}

	workspace.mutationMu.Lock()
	defer workspace.mutationMu.Unlock()
	record, err = m.sessionRecord(ctx, sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	cwd, _ = workspace.snapshot()
	if record.CWD != cwd {
		return SessionRecord{}, fmt.Errorf("%w: persisted cwd %q does not match runtime cwd %q", ErrBusy, record.CWD, cwd)
	}
	return record, nil
}

func projectSessionUsage(usage droids.SessionUsage) SessionUsage {
	return SessionUsage{
		Input: usage.Input, Output: usage.Output,
		CacheRead: usage.CacheRead, CacheWrite: usage.CacheWrite,
		Reasoning: usage.Reasoning, TotalTokens: usage.TotalTokens,
		Cost: SessionUsageCost{
			Input: usage.Cost.Input, Output: usage.Cost.Output,
			CacheRead: usage.Cost.CacheRead, CacheWrite: usage.Cost.CacheWrite,
			Total: usage.Cost.Total,
		},
	}
}

func projectTranscriptMessage(envelope droids.MessageEnvelope, sequence int64) (TranscriptMessage, error) {
	content, err := projectTranscriptContent(envelope.Message)
	if err != nil {
		return TranscriptMessage{}, err
	}
	projected := TranscriptMessage{
		ID: string(envelope.ID), TurnID: string(envelope.TurnID), Sequence: sequence,
		Content: content, CreatedAt: envelope.CreatedAt,
	}
	switch typed := envelope.Message.(type) {
	case droids.UserMessage:
		projected.Role = "user"
	case droids.AssistantMessage:
		projected.Role = "assistant"
		projected.StopReason = string(typed.StopReason)
		projected.ErrorMessage = typed.ErrorMessage
		projected.IsError = typed.StopReason == droids.StopReasonError || typed.StopReason == droids.StopReasonAborted
	case droids.ToolResultMessage:
		projected.Role = "tool"
		projected.ToolCallID = string(typed.ToolCallID)
		projected.ToolName = typed.ToolName
		projected.Details = append(json.RawMessage(nil), typed.Details...)
		projected.IsError = typed.IsError
	case droids.ContextMessage:
		projected.Role = "context"
		projected.BoundaryID = typed.BoundaryID
		projected.BoundaryKind = typed.Kind
		projected.BoundarySource = typed.Source
		projected.Details = append(json.RawMessage(nil), typed.Details...)
	default:
		return TranscriptMessage{}, fmt.Errorf("unsupported message %T", envelope.Message)
	}
	return projected, nil
}

func projectTranscriptContent(message droids.Message) ([]TranscriptContent, error) {
	switch typed := message.(type) {
	case droids.UserMessage:
		return projectDroidContent(typed.Content)
	case droids.ContextMessage:
		return projectDroidContent(typed.Content)
	case droids.AssistantMessage:
		return projectDroidContent(typed.Content)
	case droids.ToolResultMessage:
		return projectDroidContent(typed.Content)
	default:
		return nil, fmt.Errorf("unsupported message %T", message)
	}
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
	return json.Marshal(value)
}

func contentText[T any](content []T) string {
	parts := make([]string, 0, len(content))
	for _, raw := range content {
		switch block := any(raw).(type) {
		case droids.TextInput:
			parts = append(parts, block.Text)
		case droids.TextContent:
			parts = append(parts, block.Text)
		case droids.FileInput:
			parts = append(parts, "[file: "+block.Filename+"]")
		case droids.FileContent:
			parts = append(parts, "[file: "+block.Filename+"]")
		case droids.ToolCall:
			parts = append(parts, "[tool: "+block.Name+"]")
		}
	}
	return strings.Join(parts, "\n")
}

func projectDroidContent[T any](content []T) ([]TranscriptContent, error) {
	result := make([]TranscriptContent, 0, len(content))
	for _, raw := range content {
		switch block := any(raw).(type) {
		case droids.TextInput:
			if block.Text != "" {
				result = append(result, TranscriptContent{Kind: TranscriptContentText, Text: block.Text})
			}
		case droids.TextContent:
			if block.Text != "" {
				result = append(result, TranscriptContent{Kind: TranscriptContentText, Text: block.Text})
			}
		case droids.ThinkingContent:
			if block.Thinking != "" && !block.Redacted {
				result = append(result, TranscriptContent{Kind: TranscriptContentThinking, Text: block.Thinking})
			}
		case droids.ToolCall:
			arguments, truncated := presentationToolArguments(block.Arguments)
			result = append(result, TranscriptContent{
				Kind: TranscriptContentToolCall, ToolCallID: string(block.ID), ToolName: block.Name,
				Arguments: arguments, ArgumentsTruncated: truncated,
			})
		case droids.FileInput:
			kind := TranscriptContentFile
			if strings.HasPrefix(strings.ToLower(block.MediaType), "image/") {
				kind = TranscriptContentImage
			}
			result = append(result, TranscriptContent{Kind: kind, Filename: block.Filename, MediaType: block.MediaType})
		case droids.FileContent:
			kind := TranscriptContentFile
			if strings.HasPrefix(strings.ToLower(block.MediaType), "image/") {
				kind = TranscriptContentImage
			}
			result = append(result, TranscriptContent{Kind: kind, Filename: block.Filename, MediaType: block.MediaType})
		default:
			return nil, fmt.Errorf("unsupported content %T", raw)
		}
	}
	return result, nil
}
