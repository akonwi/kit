package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/securefs"
	"github.com/akonwi/kit/internal/subagent"
)

// ChildRuntimeFactory constructs isolated persistent droids for subagent conversations.
type ChildRuntimeFactory struct {
	providers     droids.Providers
	bundleBuilder RuntimeBundleBuilder
	directory     string
}

// NewChildRuntimeFactory constructs the child runtime boundary. bundleBuilder
// must omit the parent-facing subagent tool to prohibit nested delegation.
func NewChildRuntimeFactory(providers droids.Providers, bundleBuilder RuntimeBundleBuilder, droidDirectory string) (*ChildRuntimeFactory, error) {
	if providers == nil {
		return nil, errors.New("child runtime providers are required")
	}
	if nilRuntimeBundleBuilder(bundleBuilder) {
		return nil, errors.New("child runtime bundle builder is required")
	}
	if strings.TrimSpace(droidDirectory) == "" {
		return nil, errors.New("child droid directory is required")
	}
	absolute, err := filepath.Abs(droidDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve child droid directory: %w", err)
	}
	if err := securefs.MakePrivateDir(absolute); err != nil {
		return nil, fmt.Errorf("create child droid directory: %w", err)
	}
	return &ChildRuntimeFactory{providers: providers, bundleBuilder: bundleBuilder, directory: absolute}, nil
}

// Delete removes a closed child conversation store and SQLite sidecars.
func (f *ChildRuntimeFactory) Delete(_ context.Context, conversation subagent.Conversation) error {
	if f == nil || !identifier.Valid(string(conversation.ID), "subagent_") {
		return fmt.Errorf("invalid child conversation id %q", conversation.ID)
	}
	path := filepath.Join(f.directory, string(conversation.ID)+".db")
	var result error
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	return result
}

// Open opens one isolated child conversation without resuming interrupted work.
func (f *ChildRuntimeFactory) Open(ctx context.Context, conversation subagent.Conversation) (subagent.ChildRuntime, error) {
	if f == nil || f.providers == nil || f.bundleBuilder == nil {
		return nil, errors.New("child runtime factory is not initialized")
	}
	if !identifier.Valid(string(conversation.ID), "subagent_") {
		return nil, fmt.Errorf("invalid child conversation id %q", conversation.ID)
	}
	if conversation.DismissedAt != nil {
		return nil, subagent.ErrDismissed
	}
	cwd := filepath.Clean(conversation.CWD)
	bundle, err := f.bundleBuilder.Build(ctx, SessionRecord{
		ID: string(conversation.ID), CWD: cwd, Persistent: true,
		ModelProvider: modelProvider(conversation.Model), ModelID: modelID(conversation.Model),
		ThinkingLevel: conversation.ThinkingLevel,
	}, func() string { return cwd })
	if err != nil {
		return nil, fmt.Errorf("build child runtime bundle: %w", err)
	}
	if len(bundle.Subagents.Catalog.Definitions()) != 0 {
		return nil, errors.New("child runtime bundle includes nested subagents")
	}
	systemPrompt := strings.TrimSpace(bundle.Prompt.Prompt) + "\n\n" + childInstructions(conversation.Agent)
	path := filepath.Join(f.directory, string(conversation.ID)+".db")
	if conversation.DroidInitializedAt != nil {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("initialized child droid store is missing: %s", conversation.ID)
			}
			return nil, err
		}
	}
	store, err := sqlitestore.Open(ctx, sqlitestore.Options{Path: path})
	if err != nil {
		return nil, fmt.Errorf("open child droid store: %w", err)
	}
	droid, err := droids.Open(ctx, droids.ConversationID(conversation.ID), droids.Config{
		Store: store, Providers: f.providers, Model: conversation.Model,
		Reasoning: conversation.ThinkingLevel, SystemPrompt: systemPrompt, Tools: bundle.Tools,
	})
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("open child droid: %w", err)
	}
	state, err := droid.WaitQuiescent(ctx)
	if err != nil {
		_ = droid.Close()
		_ = store.Close()
		return nil, err
	}
	if state.Execution != nil && (state.Execution.Status == droids.ExecutionInterrupted || state.Execution.Status == droids.ExecutionPaused) {
		// The harness has already classified the prior task. Explicitly discard its
		// recoverable droid turn; never resume it automatically before a queued follow-up.
		if err := droid.Abort(ctx); err != nil {
			_ = droid.Close()
			_ = store.Close()
			return nil, fmt.Errorf("settle prior child turn: %w", err)
		}
		if _, err := droid.WaitQuiescent(ctx); err != nil {
			_ = droid.Close()
			_ = store.Close()
			return nil, fmt.Errorf("wait for prior child settlement: %w", err)
		}
	}
	return &childRuntime{droid: droid, store: store}, nil
}

func childInstructions(definition subagent.Definition) string {
	return "You are the " + definition.Name + " subagent.\n\n" +
		"Your role: " + definition.Description + "\n\n" +
		"Follow these child-specific instructions:\n\n" + strings.TrimSpace(definition.Instructions) +
		"\n\nDo not attempt to delegate to another subagent. Return a concise result to the parent."
}

func modelProvider(exact string) string {
	provider, _, _ := strings.Cut(exact, "/")
	return provider
}

func modelID(exact string) string {
	_, id, _ := strings.Cut(exact, "/")
	return id
}

type childRuntime struct {
	droid *droids.Droid
	store *sqlitestore.Store

	mu     sync.Mutex
	closed bool
}

func (r *childRuntime) Run(ctx context.Context, task subagent.Task, admitted func(string) error, emit func(subagent.LiveEvent)) (subagent.ChildOutcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot, err := r.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		return subagent.ChildOutcome{}, err
	}
	subscription, err := r.droid.Subscribe(context.Background(), droids.SubscribeOptions{After: snapshot.LastEvent, IncludeTransient: true, Buffer: 256})
	if err != nil {
		return subagent.ChildOutcome{}, err
	}
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for envelope := range subscription.Events() {
			if emit != nil {
				if event, ok := projectChildLiveEvent(envelope); ok {
					emit(event)
				}
			}
		}
	}()
	handle, err := r.droid.Prompt(ctx, droids.Input{Content: []droids.InputContent{droids.TextInput{Text: task.Message}}}, droids.PromptOptions{AdmissionKey: string(task.ID)})
	if err != nil {
		subscription.Close()
		<-drainDone
		return subagent.ChildOutcome{}, err
	}
	turnID := string(handle.TurnID())
	if admitted != nil {
		if err := admitted(turnID); err != nil {
			_ = r.droid.Abort(context.Background())
			_, _ = handle.Wait(context.Background())
			return subagent.ChildOutcome{TurnID: turnID, State: subagent.TaskAborted}, err
		}
	}
	settled := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = r.droid.Abort(context.Background())
		case <-settled:
		}
	}()
	outcome, waitErr := handle.Wait(context.Background())
	close(settled)
	subscription.Close()
	<-drainDone
	result := subagent.ChildOutcome{TurnID: turnID, State: projectChildTaskState(outcome.Status)}
	if outcome.FinalMessage != nil {
		if assistant, ok := outcome.FinalMessage.Message.(droids.AssistantMessage); ok {
			result.Text = assistant.Text()
			result.Error = assistant.ErrorMessage
		}
	}
	if outcome.Error != nil && result.Error == "" {
		result.Error = outcome.Error.Message
	}
	return result, waitErr
}

func projectChildLiveEvent(envelope droids.EventEnvelope) (subagent.LiveEvent, bool) {
	result := subagent.LiveEvent{TurnID: string(envelope.TurnID)}
	switch event := envelope.Event.(type) {
	case droids.LifecycleEvent:
		result.Kind = event.Kind
	case droids.MessageDelta:
		result.MessageID = event.MessageID
		switch delta := event.Stream.(type) {
		case droids.StreamTextDelta:
			result.Kind, result.ContentIndex, result.Delta = "message.text.delta", delta.ContentIndex, delta.Delta
		case droids.StreamThinkingDelta:
			result.Kind, result.ContentIndex, result.Delta = "message.thinking.delta", delta.ContentIndex, delta.Delta
		case droids.StreamToolCallStart:
			result.Kind, result.ContentIndex, result.ToolCallID, result.ToolName = "tool.planned", delta.ContentIndex, delta.ID, delta.Name
		case droids.StreamToolCallEnd:
			result.Kind, result.ContentIndex = "tool.planned", delta.ContentIndex
			result.ToolCallID, result.ToolName = string(delta.ToolCall.ID), delta.ToolCall.Name
		default:
			return subagent.LiveEvent{}, false
		}
	case droids.ToolExecutionStart:
		result.Kind, result.ToolCallID, result.ToolName = "tool.started", string(event.ToolCallID), event.ToolName
	case droids.ToolExecutionUpdate:
		result.Kind, result.ToolCallID, result.ToolName = "tool.updated", string(event.ToolCallID), event.ToolName
		result.Text = childResultText(event.Delta.Content)
		result.IsError = event.Delta.IsError
	case droids.ToolExecutionEnd:
		result.Kind, result.ToolCallID, result.ToolName = "tool.completed", string(event.ToolCallID), event.ToolName
		result.Text, result.IsError = childResultText(event.Result.Content), event.IsError || event.Result.IsError
	case droids.MessageEnd:
		result.Kind = "message.completed"
	default:
		return subagent.LiveEvent{}, false
	}
	return result, result.Kind != ""
}

func childResultText(content []droids.ResultContent) string {
	parts := make([]string, 0, len(content))
	for _, raw := range content {
		if text, ok := raw.(droids.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func projectChildTaskState(status droids.ExecutionStatus) subagent.TaskState {
	switch status {
	case droids.ExecutionCompleted:
		return subagent.TaskCompleted
	case droids.ExecutionFailed:
		return subagent.TaskFailed
	case droids.ExecutionAborted:
		return subagent.TaskAborted
	default:
		return subagent.TaskInterrupted
	}
}

func (r *childRuntime) Abort(ctx context.Context) error {
	return r.droid.Abort(ctx)
}

func (r *childRuntime) Transcript(ctx context.Context) (subagent.Transcript, error) {
	result := subagent.Transcript{}
	var cursor uint64
	var sequence int64
	var totalBytes int
	for {
		page, err := r.droid.History(ctx, droids.HistoryQuery{After: cursor, Limit: 1000})
		if err != nil {
			return subagent.Transcript{}, err
		}
		for _, envelope := range page.Messages {
			message, err := projectChildTranscriptMessage(envelope, sequence)
			if err != nil {
				return subagent.Transcript{}, err
			}
			result.ConversationID = subagent.ConversationID(envelope.ConversationID)
			result.Messages = append(result.Messages, message)
			totalBytes += childTranscriptMessageBytes(message)
			sequence++
			if len(result.Messages) > 1000 || totalBytes > 8<<20 {
				return subagent.Transcript{}, errors.New("child transcript exceeds synchronization bounds")
			}
		}
		cursor = page.Next
		if !page.HasMore {
			return result, nil
		}
	}
}

func childTranscriptMessageBytes(message subagent.TranscriptMessage) int {
	total := len(message.ID) + len(message.TurnID) + len(message.ErrorMessage) + len(message.Details)
	for _, block := range message.Content {
		total += len(block.Text) + len(block.Arguments) + len(block.Filename) + len(block.MediaType)
	}
	return total
}

func projectChildTranscriptMessage(envelope droids.MessageEnvelope, sequence int64) (subagent.TranscriptMessage, error) {
	message := subagent.TranscriptMessage{
		ID: string(envelope.ID), TurnID: string(envelope.TurnID), Sequence: sequence, CreatedAt: envelope.CreatedAt,
	}
	var content any
	switch typed := envelope.Message.(type) {
	case droids.UserMessage:
		message.Role, content = "user", typed.Content
	case droids.AssistantMessage:
		message.Role, content = "assistant", typed.Content
		message.StopReason, message.ErrorMessage = string(typed.StopReason), typed.ErrorMessage
		message.IsError = typed.StopReason == droids.StopReasonError || typed.StopReason == droids.StopReasonAborted
	case droids.ToolResultMessage:
		message.Role, content = "tool", typed.Content
		message.ToolCallID, message.ToolName = string(typed.ToolCallID), typed.ToolName
		message.Details, message.IsError = append(json.RawMessage(nil), typed.Details...), typed.IsError
	case droids.ContextMessage:
		message.Role, content = "context", typed.Content
		message.BoundaryID, message.BoundaryKind, message.BoundarySource = typed.BoundaryID, typed.Kind, typed.Source
	default:
		return subagent.TranscriptMessage{}, fmt.Errorf("unsupported child message %T", envelope.Message)
	}
	var projected []TranscriptContent
	var err error
	switch blocks := content.(type) {
	case []droids.InputContent:
		projected, err = projectDroidContent(blocks)
	case []droids.AssistantContent:
		projected, err = projectDroidContent(blocks)
	case []droids.ResultContent:
		projected, err = projectDroidContent(blocks)
	}
	if err != nil {
		return subagent.TranscriptMessage{}, err
	}
	for _, block := range projected {
		message.Content = append(message.Content, subagent.TranscriptContent{
			Kind: string(block.Kind), Text: block.Text,
			ToolCallID: block.ToolCallID, ToolName: block.ToolName,
			Arguments: block.Arguments, ArgumentsTruncated: block.ArgumentsTruncated,
			Filename: block.Filename, MediaType: block.MediaType,
		})
	}
	return message, nil
}

func (r *childRuntime) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if err := r.droid.Shutdown(ctx); err != nil {
		// Droids continues cleanup after a deadline. Leave the Store open so a
		// later Close call can observe settlement before releasing it.
		return err
	}
	if err := r.store.Close(); err != nil {
		return err
	}
	r.closed = true
	return nil
}

var _ subagent.ChildRuntimeFactory = (*ChildRuntimeFactory)(nil)
var _ subagent.ChildRuntime = (*childRuntime)(nil)
