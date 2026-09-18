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
	"github.com/akonwi/kit/internal/showimage"
	"github.com/akonwi/kit/internal/subagent"
)

type TranscriptContentKind string

const (
	TranscriptContentText        TranscriptContentKind = "text"
	TranscriptContentThinking    TranscriptContentKind = "thinking"
	TranscriptContentToolCall    TranscriptContentKind = "toolCall"
	TranscriptContentImage       TranscriptContentKind = "image"
	TranscriptContentFile        TranscriptContentKind = "file"
	TranscriptContentAnnotations TranscriptContentKind = "annotations"
)

type TranscriptContent struct {
	Kind               TranscriptContentKind        `json:"kind"`
	Text               string                       `json:"text,omitempty"`
	ToolCallID         string                       `json:"toolCallId,omitempty"`
	ToolName           string                       `json:"toolName,omitempty"`
	Arguments          string                       `json:"arguments,omitempty"`
	ArgumentsTruncated bool                         `json:"argumentsTruncated,omitempty"`
	Filename           string                       `json:"filename,omitempty"`
	MediaType          string                       `json:"mediaType,omitempty"`
	AttachmentID       string                       `json:"attachmentId,omitempty"`
	Annotations        []droids.SubmittedAnnotation `json:"annotations,omitempty"`
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
	ArgumentHint string
	Name         string
	Description  string
	Source       string
	Location     string
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

type SubagentDefinition struct {
	Name        string
	Description string
	Model       string
	Source      subagent.Source
}

type SubagentDiagnostic struct {
	Severity string
	Code     string
	Message  string
	Source   subagent.Source
}

type SubagentConversation struct {
	ID                  string
	AgentName           string
	Model               string
	ThinkingLevel       string
	State               string
	Generation          uint64
	ActiveTaskID        string
	QueuedTasks         int
	LastCompletedTaskID string
	LastResultSummary   string
	UpdatedAt           time.Time
	Tasks               []SubagentTask
}

type SubagentMailboxItem struct {
	ID             string
	ConversationID string
	TaskID         string
	AgentName      string
	State          string
	Summary        string
	Error          string
	CreatedAt      time.Time
}

type SubagentTask struct {
	ID                     string
	Sequence               uint64
	State                  string
	CancellationGeneration uint64
	QueuedAt               time.Time
	StartedAt              *time.Time
	FinishedAt             *time.Time
	ResultSummary          string
	Error                  string
}

const (
	transcriptPageMessageTarget = 50
	transcriptPageMinimumTurns  = 4
	transcriptHistoryReadLimit  = 64
)

type SessionUsageCost struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
	Total      float64
}

// ProviderRetry is the authoritative provider retry currently delaying a run.
type ProviderRetry struct {
	Count   int
	RetryAt time.Time
}

// ActiveCompaction is an authoritative automatic compaction in progress.
type ActiveCompaction struct {
	ID    string
	RunID string
}

type Snapshot struct {
	Session               SessionRecord
	Messages              []TranscriptMessage
	PreviousMessageCursor uint64
	HasMoreMessages       bool
	Boundaries            []PendingBoundary
	ActiveRunID           string
	ProviderRetry         *ProviderRetry
	ActiveCompaction      *ActiveCompaction
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
	SubagentDefinitions   []SubagentDefinition
	SubagentDiagnostics   []SubagentDiagnostic
	SubagentConversations []SubagentConversation
	SubagentMailbox       []SubagentMailboxItem
	PendingInteractions   []InteractionRequest
}

// TranscriptPage is one page of older complete-turn history.
type TranscriptPage struct {
	Messages              []TranscriptMessage
	PreviousMessageCursor uint64
	HasMoreMessages       bool
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
	for {
		droidSnapshot, err := loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
		if err != nil {
			return Snapshot{}, err
		}
		if loaded.activeRun != "" && loaded.eventCursor != droidSnapshot.LastEvent {
			if loaded.eventCursor > droidSnapshot.LastEvent {
				continue
			}
			published := loaded.eventChanged
			loaded.mu.Unlock()
			select {
			case <-ctx.Done():
				loaded.mu.Lock()
				return Snapshot{}, ctx.Err()
			case <-published:
			}
			loaded.mu.Lock()
			continue
		}
		unlockMetadata := m.lockSessionMetadata(sessionID)
		record, err := m.sessionRecordAtWorkspace(ctx, sessionID, loaded.workspace)
		if err != nil {
			unlockMetadata()
			return Snapshot{}, err
		}
		pendingInteractions := loaded.interactions.snapshot()
		loaded.events.mu.Lock()
		result, err := m.projectSnapshotLocked(ctx, sessionID, loaded, record, droidSnapshot, pendingInteractions)
		if err != nil {
			loaded.events.mu.Unlock()
			unlockMetadata()
			return Snapshot{}, err
		}
		latest, err := loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
		if err != nil {
			loaded.events.mu.Unlock()
			unlockMetadata()
			return Snapshot{}, err
		}
		if loaded.activeRun == "" || latest.LastEvent == droidSnapshot.LastEvent {
			loaded.events.mu.Unlock()
			unlockMetadata()
			return result, nil
		}
		loaded.events.mu.Unlock()
		unlockMetadata()
		published := loaded.eventChanged
		loaded.mu.Unlock()
		select {
		case <-ctx.Done():
			loaded.mu.Lock()
			return Snapshot{}, ctx.Err()
		case <-published:
		}
		loaded.mu.Lock()
	}
}

// TranscriptPage returns complete turns preceding the exclusive durable cursor.
func (m *Manager) TranscriptPage(ctx context.Context, sessionID string, before uint64) (TranscriptPage, error) {
	if err := m.beginOperation(); err != nil {
		return TranscriptPage{}, err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(sessionID) == "" || before == 0 {
		return TranscriptPage{}, fmt.Errorf("%w: session id and transcript cursor are required", ErrInvalidInput)
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return TranscriptPage{}, err
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	anchor, err := loaded.droid.History(ctx, droids.HistoryQuery{After: before - 1, Limit: 1})
	if err != nil {
		return TranscriptPage{}, err
	}
	if len(anchor.Messages) != 1 || anchor.Messages[0].Sequence != before {
		return TranscriptPage{}, ErrTranscriptCursorUnavailable
	}
	preceding, err := loaded.droid.History(ctx, droids.HistoryQuery{Before: before, Limit: 1, Descending: true})
	if err != nil {
		return TranscriptPage{}, err
	}
	if len(preceding.Messages) == 1 && anchor.Messages[0].TurnID != "" && preceding.Messages[0].TurnID == anchor.Messages[0].TurnID {
		return TranscriptPage{}, ErrTranscriptCursorUnavailable
	}
	return projectTranscriptPage(ctx, loaded.droid, before, "", false)
}

func projectProviderRetry(activeRunID string, active *droids.ExecutionSnapshot) *ProviderRetry {
	if activeRunID == "" || active == nil || string(active.TurnID) != activeRunID || active.Retry == nil ||
		(active.Status != droids.ExecutionRetrying && active.Status != droids.ExecutionInterrupted) {
		return nil
	}
	return &ProviderRetry{Count: active.Retry.Count, RetryAt: active.Retry.RetryAt}
}

func projectActiveCompaction(activeRunID string, active *droids.ExecutionSnapshot) *ActiveCompaction {
	if activeRunID == "" || active == nil || string(active.TurnID) != activeRunID || active.Compaction == nil ||
		active.Compaction.ID == "" || string(active.Compaction.TurnID) != activeRunID {
		return nil
	}
	return &ActiveCompaction{ID: active.Compaction.ID, RunID: activeRunID}
}

func (m *Manager) projectSnapshotLocked(ctx context.Context, sessionID string, loaded *runtime, record SessionRecord, droidSnapshot droids.Snapshot, pendingInteractions []InteractionRequest) (Snapshot, error) {
	activeRunID := loaded.activeRun
	completeActiveStream := activeRunID != "" && loaded.runs[activeRunID] != nil &&
		loaded.runs[activeRunID].completeStream && loaded.events.replayAvailable &&
		len(loaded.events.events) > 0 && loaded.events.events[0].Kind == EventRunStarted &&
		loaded.events.events[0].RunID == activeRunID
	result := Snapshot{
		Session: record, ActiveRunID: activeRunID,
		EventStreamID: loaded.events.streamID, EventCursor: loaded.events.next - 1,
		EventReplayAvailable: completeActiveStream,
		ContextTokens:        droidSnapshot.Context.Usage.EstimatedInput,
		ContextWindow:        droidSnapshot.Context.Usage.ContextWindow,
		Usage:                projectSessionUsage(droidSnapshot.Usage),
		FollowUps:            projectFollowUpQueue(loaded.followUps),
		Warnings:             append([]string(nil), loaded.configurationWarnings...),
		PendingInteractions:  pendingInteractions,
	}
	result.ProviderRetry = projectProviderRetry(activeRunID, droidSnapshot.Active)
	result.ActiveCompaction = projectActiveCompaction(activeRunID, droidSnapshot.Active)
	for _, definition := range loaded.bundle.Subagents.Catalog.Definitions() {
		result.SubagentDefinitions = append(result.SubagentDefinitions, SubagentDefinition{
			Name: definition.Name, Description: definition.Description, Model: definition.Model, Source: definition.Source,
		})
	}
	for _, diagnostic := range loaded.bundle.Subagents.Diagnostics {
		result.SubagentDiagnostics = append(result.SubagentDiagnostics, SubagentDiagnostic{
			Severity: string(diagnostic.Severity), Code: diagnostic.Code, Message: diagnostic.Message, Source: diagnostic.Source,
		})
	}
	if m.mailbox != nil {
		mailbox, err := m.mailbox.PendingMailbox(ctx, sessionID, 64)
		if err != nil {
			return Snapshot{}, err
		}
		for _, item := range mailbox {
			result.SubagentMailbox = append(result.SubagentMailbox, SubagentMailboxItem{
				ID: item.ID, ConversationID: string(item.ConversationID), TaskID: string(item.TaskID),
				AgentName: item.AgentName, State: string(item.State), Summary: item.Summary, Error: item.Error, CreatedAt: item.CreatedAt,
			})
		}
		conversations, err := m.mailbox.ListConversations(ctx, sessionID)
		if err != nil {
			return Snapshot{}, err
		}
		for _, conversation := range conversations {
			projected := SubagentConversation{
				ID: string(conversation.ID), AgentName: conversation.Agent.Name, Model: conversation.Model,
				ThinkingLevel: conversation.ThinkingLevel, State: string(conversation.State), Generation: conversation.Generation,
				ActiveTaskID: string(conversation.ActiveTaskID), QueuedTasks: conversation.QueuedTasks,
				LastCompletedTaskID: string(conversation.LastCompletedTaskID), LastResultSummary: conversation.LastResultSummary,
				UpdatedAt: conversation.UpdatedAt,
			}
			tasks, err := m.mailbox.ListTasks(ctx, conversation.ID)
			if err != nil {
				return Snapshot{}, err
			}
			if len(tasks) > 20 {
				tasks = tasks[len(tasks)-20:]
			}
			for _, task := range tasks {
				projected.Tasks = append(projected.Tasks, SubagentTask{
					ID: string(task.ID), Sequence: task.Sequence, State: string(task.State),
					CancellationGeneration: task.CancellationGeneration,
					QueuedAt:               task.QueuedAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt,
					ResultSummary: task.ResultSummary, Error: task.Error,
				})
			}
			result.SubagentConversations = append(result.SubagentConversations, projected)
		}
	}
	if loaded.bundle.PromptCommands != nil {
		for _, command := range loaded.bundle.PromptCommands.Commands() {
			result.PromptCommands = append(result.PromptCommands, PromptCommand{
				Name: command.Name, Description: command.Description, ArgumentHint: command.ArgumentHint,
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
	page, err := projectTranscriptPage(ctx, loaded.droid, 0, activeRunID, completeActiveStream)
	if err != nil {
		return Snapshot{}, err
	}
	result.Messages = page.Messages
	result.PreviousMessageCursor = page.PreviousMessageCursor
	result.HasMoreMessages = page.HasMoreMessages
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

func projectTranscriptPage(ctx context.Context, droid *droids.Droid, before uint64, activeRunID string, omitActive bool) (TranscriptPage, error) {
	var selected []droids.MessageEnvelope
	var candidate []droids.MessageEnvelope
	var candidateTurn string
	cursor := before
	turns := 0

	includeCandidate := func() bool {
		if len(candidate) == 0 {
			return true
		}
		if turns >= transcriptPageMinimumTurns && len(selected)+len(candidate) > transcriptPageMessageTarget {
			return false
		}
		selected = append(selected, candidate...)
		candidate = nil
		if candidateTurn != activeRunID {
			turns++
		}
		candidateTurn = ""
		return true
	}

	for {
		page, err := droid.History(ctx, droids.HistoryQuery{
			Before: cursor, Limit: transcriptHistoryReadLimit, Descending: true,
		})
		if err != nil {
			return TranscriptPage{}, err
		}
		for _, envelope := range page.Messages {
			if omitActive && string(envelope.TurnID) == activeRunID {
				continue
			}
			turnID := string(envelope.TurnID)
			if turnID == "" {
				turnID = string(envelope.ID)
			}
			if len(candidate) > 0 && turnID != candidateTurn {
				if !includeCandidate() {
					return projectSelectedTranscript(selected, true)
				}
			}
			if len(candidate) == 0 {
				candidateTurn = turnID
			}
			candidate = append(candidate, envelope)
		}
		if !page.HasMore {
			if !includeCandidate() {
				return projectSelectedTranscript(selected, true)
			}
			return projectSelectedTranscript(selected, false)
		}
		cursor = page.Next
	}
}

func projectSelectedTranscript(descending []droids.MessageEnvelope, hasMore bool) (TranscriptPage, error) {
	result := TranscriptPage{HasMoreMessages: hasMore}
	if hasMore && len(descending) > 0 {
		result.PreviousMessageCursor = descending[len(descending)-1].Sequence
	}
	for index := len(descending) - 1; index >= 0; index-- {
		envelope := descending[index]
		message, err := projectTranscriptMessage(envelope, int64(envelope.Sequence))
		if err != nil {
			return TranscriptPage{}, fmt.Errorf("project message %q: %w", envelope.ID, err)
		}
		result.Messages = append(result.Messages, message)
	}
	return result, nil
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
		projectToolImagePresentation(projected.Content, typed.ToolName, typed.IsError, typed.Details)
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

func projectToolImagePresentation(content []TranscriptContent, toolName string, isError bool, raw json.RawMessage) {
	details, marked := showimage.ParseDetails(raw)
	marked = marked && !isError && toolName == showimage.ToolName
	promoted := false
	for index := range content {
		block := &content[index]
		attachmentID := block.AttachmentID
		block.AttachmentID = ""
		if !promoted && marked && block.Kind == TranscriptContentImage && attachmentID == details.AttachmentID &&
			block.Filename == details.Filename && block.MediaType == details.MediaType {
			block.AttachmentID = attachmentID
			promoted = true
		}
	}
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
		case droids.AnnotationInput:
			parts = append(parts, fmt.Sprintf("[annotations: %d]", len(block.Annotations)))
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
			if block.AttachmentID != "" {
				result = append(result, TranscriptContent{Kind: TranscriptContentFile, Filename: block.Filename, MediaType: block.MediaType, AttachmentID: block.AttachmentID})
			} else if block.Text != "" {
				result = append(result, TranscriptContent{Kind: TranscriptContentText, Text: block.Text})
			}
		case droids.AnnotationInput:
			if len(block.Annotations) > 0 {
				result = append(result, TranscriptContent{Kind: TranscriptContentAnnotations, Annotations: append([]droids.SubmittedAnnotation(nil), block.Annotations...)})
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
			result = append(result, TranscriptContent{Kind: kind, Filename: block.Filename, MediaType: block.MediaType, AttachmentID: block.AttachmentID})
		case droids.FileContent:
			kind := TranscriptContentFile
			if strings.HasPrefix(strings.ToLower(block.MediaType), "image/") {
				kind = TranscriptContentImage
			}
			result = append(result, TranscriptContent{Kind: kind, Filename: block.Filename, MediaType: block.MediaType, AttachmentID: block.AttachmentID})
		default:
			return nil, fmt.Errorf("unsupported content %T", raw)
		}
	}
	return result, nil
}
