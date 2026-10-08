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
	// PluginInterceptors must be set before the supervisor starts. Child tools
	// use their owning session policy rather than bypassing it.
	PluginInterceptors func(context.Context, string) (PluginInterceptorHost, error)
	// MCPTools must be set before the supervisor starts. A child borrows its
	// owning session's MCP namespaces instead of starting duplicate server
	// processes, and never closes them.
	MCPTools           func(context.Context, string) ([]droids.AnyTool, error)
	SiblingTools       *subagent.SiblingToolService
	providers          droids.Providers
	bundleBuilder      RuntimeBundleBuilder
	directory          string
	modelContextWindow func(string) int
}

// NewChildRuntimeFactory constructs the child runtime boundary. bundleBuilder
// must omit the parent-facing subagent tool to prohibit nested delegation.
func NewChildRuntimeFactory(providers droids.Providers, bundleBuilder RuntimeBundleBuilder, droidDirectory string, modelContextWindow func(string) int) (*ChildRuntimeFactory, error) {
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
	return &ChildRuntimeFactory{providers: providers, bundleBuilder: bundleBuilder, directory: absolute, modelContextWindow: modelContextWindow}, nil
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
	if bundle.MCP != nil {
		return nil, errors.New("child runtime bundle owns MCP namespaces")
	}
	tools := bundle.Tools
	if f.SiblingTools != nil {
		siblings, err := f.SiblingTools.Tools(conversation)
		if err != nil {
			return nil, fmt.Errorf("build child sibling tools: %w", err)
		}
		tools = append(append([]droids.AnyTool(nil), tools...), siblings...)
	}
	if f.MCPTools != nil {
		borrowed, err := f.MCPTools(ctx, conversation.OwnerSessionID)
		if err != nil {
			return nil, fmt.Errorf("borrow owner MCP tools: %w", err)
		}
		tools = append(append([]droids.AnyTool(nil), tools...), borrowed...)
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
	model, err := f.providers.Resolve(conversation.Model)
	if err == nil && f.modelContextWindow != nil {
		if contextWindow := f.modelContextWindow(conversation.Model); contextWindow > 0 {
			model = model.WithContextWindow(contextWindow)
		}
	}
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("resolve child model: %w", err)
	}
	var interception *pluginInterceptorBridge
	if f.PluginInterceptors != nil {
		host, err := f.PluginInterceptors(ctx, conversation.OwnerSessionID)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		if host != nil {
			interception = &pluginInterceptorBridge{}
			interception.binding.Store(&pluginInterceptorBinding{host: host})
		}
	}
	config := droids.Config{
		Store: store, Model: model,
		Reasoning: conversation.ThinkingLevel, SystemPrompt: systemPrompt, Tools: tools,
	}
	if interception != nil {
		config.BeforeToolCall = interception.before
		config.BeforeToolCallIdentity = interception.identity
	}
	droid, err := droids.Spawn(ctx, droids.ConversationID(conversation.ID), config)
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
	if err := droid.ReconcileReasoning(ctx, conversation.ThinkingLevel, droids.PreserveReasoningEpoch); err != nil {
		_ = droid.Close()
		_ = store.Close()
		return nil, fmt.Errorf("reconcile child reasoning: %w", err)
	}
	return &childRuntime{
		droid: droid, store: store,
		requestConfiguration: droids.RequestConfiguration{
			SystemPrompt: systemPrompt, Reasoning: conversation.ThinkingLevel,
			ContextWindow: model.ContextWindow, Tools: tools,
		},
	}, nil
}

// OpenTranscript opens durable child history without rebuilding the execution
// bundle, so archived transcripts remain readable after their working directory
// or configured tools are no longer available.
func (f *ChildRuntimeFactory) OpenTranscript(ctx context.Context, conversation subagent.Conversation) (subagent.ChildTranscriptReader, error) {
	if f == nil {
		return nil, errors.New("child runtime factory is not initialized")
	}
	if !identifier.Valid(string(conversation.ID), "subagent_") {
		return nil, fmt.Errorf("invalid child conversation id %q", conversation.ID)
	}
	reader := &childTranscriptReader{conversation: droids.ConversationID(conversation.ID)}
	if conversation.DroidInitializedAt == nil {
		return reader, nil
	}
	path := filepath.Join(f.directory, string(conversation.ID)+".db")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("initialized child droid store is missing: %s", conversation.ID)
		}
		return nil, err
	}
	store, err := sqlitestore.Open(ctx, sqlitestore.Options{Path: path, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("open child transcript store: %w", err)
	}
	state, err := store.State(ctx)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("read child transcript store: %w", err)
	}
	if state.ID != reader.conversation {
		_ = store.Close()
		return nil, errors.New("child transcript store belongs to another conversation")
	}
	reader.store = store
	return reader, nil
}

// PrepareConfiguration validates and, when necessary, compacts initialized
// settled child context for a target model. The next worker opens that context
// with the persisted target after the supervisor commits it.
func (f *ChildRuntimeFactory) PrepareConfiguration(ctx context.Context, conversation subagent.Conversation, configuration subagent.Configuration) (compacted bool, checkpointID string, resultErr error) {
	if configuration.Model == conversation.Model || conversation.DroidInitializedAt == nil {
		return false, "", nil
	}
	raw, err := f.Open(ctx, conversation)
	if err != nil {
		return false, "", err
	}
	runtime, ok := raw.(*childRuntime)
	if !ok {
		_ = raw.Close(context.Background())
		return false, "", errors.New("unexpected child runtime implementation")
	}
	defer func() {
		if closeErr := runtime.Close(context.Background()); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close prepared child runtime: %w", closeErr)
		}
	}()
	target, err := f.providers.Resolve(configuration.Model)
	if err == nil && f.modelContextWindow != nil {
		if contextWindow := f.modelContextWindow(configuration.Model); contextWindow > 0 {
			target = target.WithContextWindow(contextWindow)
		}
	}
	if err != nil {
		return false, "", fmt.Errorf("resolve target child model: %w", err)
	}
	contextTarget := droids.ContextTarget{Model: target, Reasoning: configuration.ThinkingLevel}
	assessment, err := runtime.droid.AssessContext(ctx, contextTarget)
	if err != nil {
		return false, "", fmt.Errorf("assess child context: %w", err)
	}
	if !assessment.RequiresCompaction {
		return false, "", nil
	}
	operationID, err := identifier.New("compact_")
	if err != nil {
		return false, "", err
	}
	result, err := runtime.droid.CompactContext(ctx, droids.CompactContextOptions{OperationID: operationID, Target: contextTarget})
	if err != nil {
		return false, "", fmt.Errorf("%w: adapt child context: %v", ErrCompactionFailed, err)
	}
	return result.Compacted, string(result.CheckpointID), nil
}

func childInstructions(definition subagent.Definition) string {
	return "You are the " + definition.Name + " subagent.\n\n" +
		"Your role: " + definition.Description + "\n\n" +
		"Follow these child-specific instructions:\n\n" + strings.TrimSpace(definition.Instructions) +
		"\n\nYou may send requests to configured siblings (including ones not yet started) and explicitly reply to requests in your inbox. " +
		"Do not directly start, cancel, or dismiss another subagent. Return a concise result to the parent for parent-assigned work."
}

func modelProvider(exact string) string {
	provider, _, _ := strings.Cut(exact, "/")
	return provider
}

func modelID(exact string) string {
	_, id, _ := strings.Cut(exact, "/")
	return id
}

type childTranscriptReader struct {
	conversation droids.ConversationID
	store        *sqlitestore.Store
}

func (r *childTranscriptReader) Transcript(ctx context.Context, before uint64) (subagent.Transcript, error) {
	if r.store == nil {
		return subagent.Transcript{ConversationID: subagent.ConversationID(r.conversation)}, nil
	}
	history := func(ctx context.Context, query droids.HistoryQuery) (droids.MessagePage, error) {
		return droids.ReadHistory(ctx, r.store, r.conversation, query)
	}
	if before != 0 {
		if err := validateChildTranscriptCursor(ctx, history, before); err != nil {
			return subagent.Transcript{}, err
		}
	}
	return collectChildTranscript(ctx, history, before)
}

func (r *childTranscriptReader) Close(context.Context) error {
	if r.store == nil {
		return nil
	}
	return r.store.Close()
}

type childRuntime struct {
	droid *droids.Droid
	store *sqlitestore.Store

	mu                   sync.Mutex
	closed               bool
	requestConfiguration droids.RequestConfiguration
}

func (r *childRuntime) Steer(ctx context.Context, message string) error {
	_, err := r.droid.Prompt(ctx, droids.Input{Content: []droids.InputContent{droids.TextInput{Text: message}}}, droids.PromptOptions{Steer: true})
	if errors.Is(err, droids.ErrConflict) {
		return subagent.ErrConflict
	}
	return err
}

func (r *childRuntime) ConfigureThinking(ctx context.Context, thinking string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return droids.ErrClosed
	}
	next := r.requestConfiguration
	next.Reasoning = thinking
	if err := r.droid.ReconfigureContext(ctx, next); err != nil {
		return err
	}
	r.requestConfiguration = next
	return nil
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
	var handle droids.ExecutionHandle
	if task.Origin == subagent.TaskOriginParent {
		handle, err = r.droid.Prompt(ctx, droids.Input{Content: []droids.InputContent{droids.TextInput{Text: task.Message}}}, droids.PromptOptions{AdmissionKey: string(task.ID)})
	} else {
		boundaryID := "inbox_" + string(task.ID)
		details, encodeErr := droids.EncodeDetails(struct {
			Version int    `json:"version"`
			Receipt string `json:"receipt"`
			Kind    string `json:"kind"`
		}{Version: 1, Receipt: task.RequestID, Kind: string(task.Origin)})
		if encodeErr != nil {
			err = encodeErr
		} else {
			err = r.droid.Inform(ctx, droids.BoundaryMessage{
				ID: boundaryID, Kind: "subagent_inbox", Source: "subagent",
				Content: []droids.InputContent{droids.TextInput{Text: task.Message}}, Details: details,
			})
			if err != nil {
				if received, reconcileErr := r.droid.BoundaryReceived(context.Background(), boundaryID); reconcileErr == nil && received {
					err = nil
				}
			}
			if err == nil {
				key := "inbox:" + string(task.ID)
				handle, _, err = r.droid.ReactUncounted(ctx, key)
				if err != nil {
					status, reconcileErr := r.droid.BoundaryStatus(context.Background(), boundaryID)
					if reconcileErr == nil && status.TurnID != "" {
						handle, _, err = r.droid.ReactUncounted(context.Background(), key)
					}
				}
			}
		}
	}
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
		switch event.Kind {
		case "execution.started":
			result.Kind = "turn.started"
		case "execution.settled", "execution.interrupted":
			result.Kind = "turn.settled"
		default:
			return subagent.LiveEvent{}, false
		}
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
		result.Kind, result.ToolCallID, result.ToolName = "tool.output.delta", string(event.ToolCallID), event.ToolName
		result.Text = childResultText(event.Delta.Content)
		result.IsError = event.Delta.IsError
	case droids.ToolExecutionEnd:
		result.Kind, result.ToolCallID, result.ToolName = "tool.completed", string(event.ToolCallID), event.ToolName
		result.Text, result.IsError = childResultText(event.Result.Content), event.IsError || event.Result.IsError
	case droids.MessageEnd:
		result.Kind = "message.completed"
		if assistant, ok := event.Message.(droids.AssistantMessage); ok {
			result.MessageID = assistant.ID
		}
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

// maxChildTranscriptMessages bounds one child transcript page. Pages are
// bounded by message count and complete turns, not by encoded size.
const maxChildTranscriptMessages = 1000

func (r *childRuntime) Transcript(ctx context.Context, before uint64) (subagent.Transcript, error) {
	if before != 0 {
		if err := validateChildTranscriptCursor(ctx, r.droid.History, before); err != nil {
			return subagent.Transcript{}, err
		}
	}
	return collectChildTranscript(ctx, r.droid.History, before)
}

func validateChildTranscriptCursor(ctx context.Context, history func(context.Context, droids.HistoryQuery) (droids.MessagePage, error), before uint64) error {
	anchor, err := history(ctx, droids.HistoryQuery{After: before - 1, Limit: 1})
	if err != nil {
		return err
	}
	if len(anchor.Messages) != 1 || anchor.Messages[0].Sequence != before {
		return subagent.ErrTranscriptCursorUnavailable
	}
	preceding, err := history(ctx, droids.HistoryQuery{Before: before, Limit: 1, Descending: true})
	if err != nil {
		return err
	}
	if len(preceding.Messages) == 1 && anchor.Messages[0].TurnID != "" && preceding.Messages[0].TurnID == anchor.Messages[0].TurnID {
		return subagent.ErrTranscriptCursorUnavailable
	}
	return nil
}

// collectChildTranscript returns one complete-turn page preceding before.
// A zero before selects the newest page. Sequences stay durable so the oldest
// included sequence can request the preceding page. Pages are bounded by
// message count, not encoded size; a single turn with more than
// maxChildTranscriptMessages messages is an error.
func collectChildTranscript(ctx context.Context, history func(context.Context, droids.HistoryQuery) (droids.MessagePage, error), before uint64) (subagent.Transcript, error) {
	var selected []droids.MessageEnvelope
	var candidate []droids.MessageEnvelope
	var candidateTurn string
	var conversationID subagent.ConversationID
	cursor := before
	turns := 0

	includeCandidate := func() (bool, error) {
		if len(candidate) == 0 {
			return true, nil
		}
		if len(selected)+len(candidate) > maxChildTranscriptMessages {
			if len(selected) == 0 {
				return false, errors.New("child transcript exceeds synchronization bounds")
			}
			return false, nil
		}
		if turns >= transcriptPageMinimumTurns && len(selected)+len(candidate) > transcriptPageMessageTarget {
			return false, nil
		}
		selected = append(selected, candidate...)
		candidate = nil
		candidateTurn = ""
		turns++
		return true, nil
	}

	for {
		page, err := history(ctx, droids.HistoryQuery{Before: cursor, Limit: transcriptHistoryReadLimit, Descending: true})
		if err != nil {
			return subagent.Transcript{}, err
		}
		for _, envelope := range page.Messages {
			if conversationID == "" {
				conversationID = subagent.ConversationID(envelope.ConversationID)
			}
			turnID := string(envelope.TurnID)
			if turnID == "" {
				turnID = string(envelope.ID)
			}
			if len(candidate) > 0 && turnID != candidateTurn {
				ok, err := includeCandidate()
				if err != nil {
					return subagent.Transcript{}, err
				}
				if !ok {
					return projectChildTranscriptPage(conversationID, selected, true)
				}
			}
			if len(candidate) == 0 {
				candidateTurn = turnID
			}
			candidate = append(candidate, envelope)
		}
		if !page.HasMore || page.Next == 0 {
			ok, err := includeCandidate()
			if err != nil {
				return subagent.Transcript{}, err
			}
			return projectChildTranscriptPage(conversationID, selected, !ok)
		}
		cursor = page.Next
	}
}

func projectChildTranscriptPage(conversationID subagent.ConversationID, descending []droids.MessageEnvelope, hasMore bool) (subagent.Transcript, error) {
	result := subagent.Transcript{ConversationID: conversationID, HasMoreMessages: hasMore}
	if hasMore && len(descending) > 0 {
		result.PreviousMessageCursor = descending[len(descending)-1].Sequence
	}
	for index := len(descending) - 1; index >= 0; index-- {
		envelope := descending[index]
		message, err := projectChildTranscriptMessage(envelope, int64(envelope.Sequence))
		if err != nil {
			return subagent.Transcript{}, err
		}
		result.Messages = append(result.Messages, message)
	}
	return result, nil
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
