package droids

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
)

const (
	runtimeRecordKind               = "runtime"
	runtimeRecordID                 = "current"
	messageRecordKind               = "message"
	turnRecordKind                  = "turn"
	attemptRecordKind               = "attempt"
	toolRecordKind                  = "tool"
	checkpointKind                  = "checkpoint"
	boundaryReceiptKind             = "boundary_receipt"
	boundaryConsumptionKind         = "boundary_consumption"
	compactionIntentKind            = "compaction_intent"
	compactionReceiptKind           = "compaction_receipt"
	usageContributionKind           = "usage_contribution"
	lineageRecordKind               = "lineage"
	annotationSubmissionReceiptKind = "annotation_submission_receipt"
	lineageRecordID                 = "parent"
	recordVersion                   = 1
	eventVersion                    = 1
)

type cyclePhase string

const (
	cycleReady              cyclePhase = "ready"
	cycleModelStarted       cyclePhase = "model_started"
	cycleAssistantPersisted cyclePhase = "assistant_persisted"
	cycleSyntheticPending   cyclePhase = "synthetic_results_pending"
	cycleToolsAdmitted      cyclePhase = "tools_admitted"
)

type durableCompaction struct {
	ID     string `json:"id"`
	TurnID TurnID `json:"turn_id"`
	Forced bool   `json:"forced,omitempty"`
}

type durableRuntime struct {
	Status                  ExecutionStatus            `json:"status"`
	CyclePhase              cyclePhase                 `json:"cycle_phase,omitempty"`
	TurnID                  TurnID                     `json:"turn_id,omitempty"`
	AttemptID               AttemptID                  `json:"attempt_id,omitempty"`
	AttemptOpen             bool                       `json:"attempt_open,omitempty"`
	Context                 []wireMessageEnvelope      `json:"context,omitempty"`
	PendingSteering         []wireMessageEnvelope      `json:"pending_steering,omitempty"`
	PendingBoundaries       []durableBoundary          `json:"pending_boundaries,omitempty"`
	CheckpointID            CheckpointID               `json:"checkpoint_id,omitempty"`
	Reason                  string                     `json:"reason,omitempty"`
	Error                   *durableDroidError         `json:"error,omitempty"`
	ModelCycles             uint64                     `json:"model_cycles,omitempty"`
	RetryCount              int                        `json:"retry_count,omitempty"`
	RetryAt                 time.Time                  `json:"retry_at,omitempty"`
	Compaction              *durableCompaction         `json:"compaction,omitempty"`
	AbortRequested          bool                       `json:"abort_requested,omitempty"`
	TerminatePending        bool                       `json:"terminate_pending,omitempty"`
	Tools                   map[ToolCallID]durableTool `json:"tools,omitempty"`
	Final                   *wireMessageEnvelope       `json:"final,omitempty"`
	Usage                   Usage                      `json:"usage,omitempty"`
	SessionUsage            SessionUsage               `json:"session_usage,omitempty"`
	SessionUsageInitialized bool                       `json:"session_usage_initialized,omitempty"`
	LastTransitionID        string                     `json:"last_transition_id,omitempty"`
	AdmissionKey            string                     `json:"admission_key,omitempty"`
	AdmissionHash           string                     `json:"admission_hash,omitempty"`
	AutonomousReactions     uint8                      `json:"autonomous_reactions,omitempty"`
	BoundaryReaction        bool                       `json:"boundary_reaction,omitempty"`
	ReactionLimitDeferred   bool                       `json:"reaction_limit_deferred,omitempty"`
}

type durableBoundary struct {
	Message  BoundaryMessageWire `json:"message"`
	Accepted time.Time           `json:"accepted"`
}

type BoundaryMessageWire struct {
	ID         string             `json:"id,omitempty"`
	ReceiptIDs []string           `json:"receipt_ids,omitempty"`
	Kind       string             `json:"kind"`
	Source     string             `json:"source"`
	Content    []wireInputContent `json:"content"`
	Details    json.RawMessage    `json:"details,omitempty"`
}

type wireInputContent struct {
	Type         string                `json:"type"`
	Text         string                `json:"text,omitempty"`
	Filename     string                `json:"filename,omitempty"`
	MediaType    string                `json:"media_type,omitempty"`
	URL          string                `json:"url,omitempty"`
	AttachmentID string                `json:"attachment_id,omitempty"`
	Annotations  []SubmittedAnnotation `json:"annotations,omitempty"`
	SubmissionID string                `json:"submission_id,omitempty"`
}

type durableDroidError struct {
	Kind      DroidErrorKind `json:"kind"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
}

type toolPhase string

const (
	toolPhaseBeforeHook toolPhase = "before_hook"
	toolPhaseReady      toolPhase = "ready"
	toolPhaseExecuting  toolPhase = "executing"
	toolPhaseAfterHook  toolPhase = "after_hook"
	toolPhaseCompleted  toolPhase = "completed"
)

type durableTool struct {
	ID                 ToolCallID   `json:"id"`
	AdmissionAttemptID AttemptID    `json:"admission_attempt_id"`
	Call               wireContent  `json:"call"`
	Phase              toolPhase    `json:"phase"`
	InterruptedPhase   toolPhase    `json:"interrupted_phase,omitempty"`
	RequiresBeforeHook bool         `json:"requires_before_hook,omitempty"`
	RequiresAfterHook  bool         `json:"requires_after_hook,omitempty"`
	ValidationError    string       `json:"validation_error,omitempty"`
	RawResult          *wireMessage `json:"raw_result,omitempty"`
	Final              *wireMessage `json:"final_result,omitempty"`
}

type durableLifecycleEvent struct {
	TurnID    TurnID          `json:"turn_id,omitempty"`
	AttemptID AttemptID       `json:"attempt_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

func newDurableRuntime() durableRuntime {
	return durableRuntime{
		Status: ExecutionReady, CyclePhase: cycleReady,
		Tools: make(map[ToolCallID]durableTool), SessionUsageInitialized: true,
	}
}

func encodeRuntime(state durableRuntime) ([]byte, error) {
	return json.Marshal(state)
}

func decodeRuntime(records []EncodedRecord) (durableRuntime, error) {
	for _, record := range records {
		if record.Kind != runtimeRecordKind || record.ID != runtimeRecordID {
			continue
		}
		if record.Version != recordVersion {
			return durableRuntime{}, fmt.Errorf("droids: unsupported runtime record version %d", record.Version)
		}
		var state durableRuntime
		if err := json.Unmarshal(record.Payload, &state); err != nil {
			return durableRuntime{}, fmt.Errorf("droids: decode runtime state: %w", err)
		}
		if state.Tools == nil {
			state.Tools = make(map[ToolCallID]durableTool)
		}
		if state.CyclePhase == "" {
			state.CyclePhase = cycleReady
		}
		return state, nil
	}
	return durableRuntime{}, fmt.Errorf("droids: runtime state is missing")
}

func runtimeEncodedRecord(state durableRuntime) (EncodedRecord, error) {
	payload, err := encodeRuntime(state)
	if err != nil {
		return EncodedRecord{}, err
	}
	return EncodedRecord{
		Kind: runtimeRecordKind, ID: runtimeRecordID, Scope: RecordRuntime,
		Version: recordVersion, Payload: payload,
	}, nil
}

type durableForkLineage struct {
	Point       ForkPoint `json:"point"`
	OperationID string    `json:"operation_id"`
}

func lineageEncodedRecord(lineage durableForkLineage) (EncodedRecord, error) {
	point := lineage.Point
	if point.ConversationID == "" || point.Revision == 0 || point.LastEvent == 0 || lineage.OperationID == "" {
		return EncodedRecord{}, fmt.Errorf("droids: fork lineage is incomplete")
	}
	payload, err := json.Marshal(lineage)
	if err != nil {
		return EncodedRecord{}, err
	}
	return EncodedRecord{
		Kind: lineageRecordKind, ID: lineageRecordID, Scope: RecordRuntime,
		Version: recordVersion, Payload: payload,
	}, nil
}

func decodeLineage(records []EncodedRecord) (*durableForkLineage, error) {
	for _, record := range records {
		if record.Kind != lineageRecordKind || record.ID != lineageRecordID {
			continue
		}
		if record.Scope != RecordRuntime || record.Version != recordVersion {
			return nil, fmt.Errorf("droids: unsupported fork lineage record")
		}
		var lineage durableForkLineage
		if err := json.Unmarshal(record.Payload, &lineage); err != nil {
			return nil, fmt.Errorf("droids: decode fork lineage: %w", err)
		}
		point := lineage.Point
		if point.ConversationID == "" || point.Revision == 0 || point.LastEvent == 0 || lineage.OperationID == "" {
			return nil, fmt.Errorf("droids: persisted fork lineage is incomplete")
		}
		return &lineage, nil
	}
	return nil, nil
}

func runtimeMutation(state durableRuntime) (EncodedMutation, error) {
	payload, err := encodeRuntime(state)
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationPut, RecordKind: runtimeRecordKind, RecordID: runtimeRecordID,
		Scope: RecordRuntime, Version: recordVersion, Payload: payload,
	}, nil
}

func turnHistoryMutation(state durableRuntime, status ExecutionStatus) (EncodedMutation, error) {
	payload, err := json.Marshal(map[string]any{
		"turn_id": state.TurnID, "status": status, "final": state.Final,
		"error": state.Error, "usage": state.Usage,
	})
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: turnRecordKind,
		RecordID: string(state.TurnID), Scope: RecordHistory,
		Version: recordVersion, Payload: payload,
	}, nil
}

func attemptHistoryMutation(state durableRuntime, status ExecutionStatus) (EncodedMutation, error) {
	payload, err := json.Marshal(map[string]any{
		"attempt_id": state.AttemptID, "turn_id": state.TurnID,
		"status": status, "model_cycles": state.ModelCycles, "error": state.Error,
	})
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: attemptRecordKind,
		RecordID: string(state.AttemptID), Scope: RecordHistory,
		Version: recordVersion, Payload: payload,
	}, nil
}

func messageHistoryMutation(envelope MessageEnvelope) (EncodedMutation, error) {
	payload, err := encodeMessageEnvelope(envelope)
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: messageRecordKind,
		RecordID: string(envelope.ID), Scope: RecordHistory, Version: recordVersion,
		Payload: payload,
	}, nil
}

func lifecycleEvent(kind string, turnID TurnID, attemptID AttemptID, data any) (EncodedDurableEvent, error) {
	var raw json.RawMessage
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			return EncodedDurableEvent{}, err
		}
		raw = encoded
	}
	payload, err := json.Marshal(durableLifecycleEvent{TurnID: turnID, AttemptID: attemptID, Data: raw})
	if err != nil {
		return EncodedDurableEvent{}, err
	}
	return EncodedDurableEvent{
		Kind: kind, Version: eventVersion, Payload: payload, OccurredAt: time.Now().UTC(),
	}, nil
}

func decodeLifecycleEvent(event StoredEvent, conversationID ConversationID) (EventEnvelope, error) {
	var payload durableLifecycleEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return EventEnvelope{}, fmt.Errorf("droids: decode event %d: %w", event.Sequence, err)
	}
	var decoded Event = LifecycleEvent{Kind: event.Kind, Data: append(json.RawMessage(nil), payload.Data...)}
	if event.Kind == "usage.updated" {
		var data struct {
			SessionUsage SessionUsage `json:"session_usage"`
		}
		if err := json.Unmarshal(payload.Data, &data); err != nil {
			return EventEnvelope{}, fmt.Errorf("droids: decode usage event %d: %w", event.Sequence, err)
		}
		if err := validateUsage(data.SessionUsage); err != nil {
			return EventEnvelope{}, fmt.Errorf("droids: invalid usage event %d: %w", event.Sequence, err)
		}
		decoded = UsageUpdated{Usage: data.SessionUsage}
	}
	return EventEnvelope{
		Sequence: event.Sequence, Durable: true, OccurredAt: event.OccurredAt,
		ConversationID: conversationID, TurnID: payload.TurnID, AttemptID: payload.AttemptID,
		Event: decoded,
	}, nil
}

func validateOpenedRuntime(state durableRuntime) error {
	if state.Compaction != nil {
		encodedID := strings.TrimPrefix(state.Compaction.ID, "compact_")
		_, idErr := hex.DecodeString(encodedID)
		if !strings.HasPrefix(state.Compaction.ID, "compact_") || len(encodedID) != 32 || idErr != nil || state.Compaction.TurnID == "" || state.Compaction.TurnID != state.TurnID || isTerminalStatus(state.Status) {
			return fmt.Errorf("droids: persisted active compaction is invalid")
		}
	}
	if err := validateUsage(state.Usage); err != nil {
		return fmt.Errorf("droids: invalid persisted turn usage: %w", err)
	}
	if !state.SessionUsageInitialized {
		return fmt.Errorf("droids: persisted session usage is not initialized")
	}
	if err := validateUsage(state.SessionUsage); err != nil {
		return fmt.Errorf("droids: invalid persisted session usage: %w", err)
	}
	switch state.Status {
	case ExecutionReady, ExecutionRunning, ExecutionRetrying, ExecutionPausing,
		ExecutionPaused, ExecutionAborting, ExecutionCompleted, ExecutionFailed,
		ExecutionAborted, ExecutionInterrupted:
	default:
		return fmt.Errorf("droids: invalid persisted execution status %q", state.Status)
	}
	switch state.CyclePhase {
	case cycleReady, cycleModelStarted, cycleAssistantPersisted, cycleSyntheticPending, cycleToolsAdmitted:
	default:
		return fmt.Errorf("droids: invalid persisted cycle phase %q", state.CyclePhase)
	}
	if (state.AdmissionKey == "") != (state.AdmissionHash == "") || len(state.AdmissionKey) > 256 || len(state.AdmissionHash) > 128 {
		return fmt.Errorf("droids: persisted prompt admission identity is invalid")
	}
	if state.Status != ExecutionReady && state.TurnID == "" {
		return fmt.Errorf("droids: persisted execution has no turn id")
	}
	for providerID, tool := range state.Tools {
		if providerID == "" || tool.ID == "" || tool.Call.Type != "tool_call" || tool.Call.ID != providerID || tool.AdmissionAttemptID == "" {
			return fmt.Errorf("droids: persisted tool state is invalid")
		}
		switch tool.Phase {
		case toolPhaseBeforeHook, toolPhaseReady:
			if tool.RawResult != nil || tool.Final != nil {
				return fmt.Errorf("droids: pre-execution tool %q contains a result", tool.ID)
			}
		case toolPhaseExecuting:
			if tool.Final != nil {
				return fmt.Errorf("droids: executing tool %q contains a final result", tool.ID)
			}
		case toolPhaseAfterHook:
			if tool.RawResult == nil || tool.Final != nil {
				return fmt.Errorf("droids: after-hook tool %q has invalid results", tool.ID)
			}
		case toolPhaseCompleted:
			if tool.Final == nil {
				return fmt.Errorf("droids: completed tool %q has no final result", tool.ID)
			}
		default:
			return fmt.Errorf("droids: persisted tool %q has invalid phase %q", tool.ID, tool.Phase)
		}
		for label, result := range map[string]*wireMessage{"raw": tool.RawResult, "final": tool.Final} {
			if result == nil {
				continue
			}
			message, err := messageFromWire(*result)
			if err != nil {
				return fmt.Errorf("droids: decode persisted %s tool result %q: %w", label, tool.ID, err)
			}
			toolResult, ok := message.(ToolResultMessage)
			if !ok || toolResult.ToolCallID != tool.ID || toolResult.ProviderCallID != tool.Call.ProviderCallID {
				return fmt.Errorf("droids: persisted %s tool result %q has invalid identity", label, tool.ID)
			}
		}
	}
	return validateRuntimeContinuation(state)
}

func runtimeMessageEnvelopes(state durableRuntime) ([]MessageEnvelope, error) {
	out := make([]MessageEnvelope, 0, len(state.Context))
	for _, wire := range state.Context {
		envelope, err := messageEnvelopeFromWire(wire)
		if err != nil {
			return nil, err
		}
		if _, err := messageToWire(envelope.Message); err != nil {
			return nil, fmt.Errorf("droids: invalid persisted message %q: %w", envelope.ID, err)
		}
		out = append(out, envelope)
	}
	return out, nil
}

func appendRuntimeEnvelope(state *durableRuntime, envelope MessageEnvelope) error {
	wire, err := messageEnvelopeToWire(envelope)
	if err != nil {
		return err
	}
	state.Context = append(state.Context, wire)
	return nil
}

func inputToMessage(input Input) (UserMessage, error) {
	if len(input.Content) == 0 {
		return UserMessage{}, fmt.Errorf("droids: prompt content is required")
	}
	content := make([]InputContent, 0, len(input.Content))
	for _, block := range input.Content {
		switch value := block.(type) {
		case TextInput:
			if value.Text == "" {
				return UserMessage{}, fmt.Errorf("droids: prompt text is empty")
			}
			content = append(content, value)
		case AnnotationInput:
			if err := validateAnnotationInput(value); err != nil {
				return UserMessage{}, err
			}
			value.Annotations = append([]SubmittedAnnotation(nil), value.Annotations...)
			content = append(content, value)
		case FileInput:
			mediaType, err := validateMediaType(value.MediaType)
			if err != nil {
				return UserMessage{}, fmt.Errorf("droids: invalid file media type: %w", err)
			}
			if value.Filename == "" && !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
				return UserMessage{}, fmt.Errorf("droids: filename is required for non-image input")
			}
			if err := validateContentSource(value.URL, value.MediaType); err != nil {
				return UserMessage{}, fmt.Errorf("droids: invalid file URL: %w", err)
			}
			content = append(content, value)
		default:
			return UserMessage{}, fmt.Errorf("droids: unsupported input content %T", block)
		}
	}
	return UserMessage{Content: content, Timestamp: time.Now().UnixMilli()}, nil
}

func inputToWire(content []InputContent) ([]wireInputContent, error) {
	out := make([]wireInputContent, 0, len(content))
	for _, block := range content {
		switch value := block.(type) {
		case TextInput:
			if value.Text == "" {
				return nil, fmt.Errorf("droids: input text is empty")
			}
			out = append(out, wireInputContent{Type: "text", Text: value.Text, AttachmentID: value.AttachmentID, Filename: value.Filename, MediaType: value.MediaType})
		case AnnotationInput:
			if err := validateAnnotationInput(value); err != nil {
				return nil, err
			}
			out = append(out, wireInputContent{Type: "annotations", Text: value.Text, Annotations: append([]SubmittedAnnotation(nil), value.Annotations...), SubmissionID: value.SubmissionID})
		case FileInput:
			if _, err := NewFileInputURL(value.Filename, value.MediaType, value.URL); err != nil {
				return nil, fmt.Errorf("droids: invalid file input: %w", err)
			}
			out = append(out, wireInputContent{
				Type: "file", Filename: value.Filename, MediaType: value.MediaType, URL: value.URL, AttachmentID: value.AttachmentID,
			})
		default:
			return nil, fmt.Errorf("droids: unsupported input content %T", block)
		}
	}
	return out, nil
}

func inputFromWire(content []wireInputContent) ([]InputContent, error) {
	out := make([]InputContent, 0, len(content))
	for _, block := range content {
		switch block.Type {
		case "text":
			out = append(out, TextInput{Text: block.Text, AttachmentID: block.AttachmentID, Filename: block.Filename, MediaType: block.MediaType})
		case "annotations":
			value := AnnotationInput{SubmissionID: block.SubmissionID, Text: block.Text, Annotations: append([]SubmittedAnnotation(nil), block.Annotations...)}
			if err := validateAnnotationInput(value); err != nil {
				return nil, err
			}
			out = append(out, value)
		case "file":
			out = append(out, FileInput{Filename: block.Filename, MediaType: block.MediaType, URL: block.URL, AttachmentID: block.AttachmentID})
		default:
			return nil, fmt.Errorf("droids: unsupported input content kind %q", block.Type)
		}
	}
	return out, nil
}

func validateAnnotationInput(input AnnotationInput) error {
	if !identifier.Valid(input.SubmissionID, "annotation_submission_") || !safeAnnotationText(input.Text, false) || len(input.Text) > 256<<10 || len(input.Annotations) == 0 || len(input.Annotations) > 64 {
		return fmt.Errorf("droids: annotation input is invalid")
	}
	encodedBytes := 0
	for _, annotation := range input.Annotations {
		encoded, err := json.Marshal(annotation)
		encodedBytes += len(encoded)
		if err != nil || annotation.ID == 0 || !validSubmittedAnnotationIdentity(annotation) ||
			!validSubmittedAnnotationPath(annotation.Path) ||
			annotation.StartLine <= 0 || annotation.EndLine < annotation.StartLine || annotation.EndLine-annotation.StartLine+1 > 200 ||
			!safeAnnotationText(annotation.Body, false) || len(annotation.Body) > 16<<10 || !safeAnnotationText(annotation.Preview, true) || len(annotation.Preview) > 16<<10 {
			return fmt.Errorf("droids: submitted annotation is invalid")
		}
	}
	if encodedBytes > 256<<10 {
		return fmt.Errorf("droids: submitted annotations exceed aggregate bound")
	}
	return nil
}

func validSubmittedAnnotationIdentity(annotation SubmittedAnnotation) bool {
	switch annotation.Kind {
	case "workspace_file":
		return validAnnotationToken(annotation.WorkspaceID, "workspace_") && annotation.TargetID == "" && annotation.TargetRevision == "" && annotation.Side == "" && validAnnotationToken(annotation.FileRevision, "file_")
	case "working_tree_diff":
		return annotation.WorkspaceID == "" && validAnnotationToken(annotation.TargetID, "difftarget_") && validAnnotationToken(annotation.TargetRevision, "diffrev_") && validAnnotationToken(annotation.FileRevision, "diff_file_") && (annotation.Side == "old" || annotation.Side == "new")
	default:
		return false
	}
}

func validSubmittedAnnotationPath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "\\") || path.Clean(value) != value {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) > 64 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, character := range part {
			if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
				return false
			}
		}
	}
	return true
}

func validAnnotationToken(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) > 128 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(raw) == sha256.Size
}

func safeAnnotationText(value string, allowEmpty bool) bool {
	if !utf8.ValidString(value) || !allowEmpty && strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if character != '\n' && character != '\t' && (unicode.IsControl(character) || unicode.Is(unicode.Cf, character)) {
			return false
		}
	}
	return true
}

func boundaryToWire(message BoundaryMessage) (BoundaryMessageWire, error) {
	content, err := inputToWire(message.Content)
	if err != nil {
		return BoundaryMessageWire{}, err
	}
	if message.Kind == "" {
		return BoundaryMessageWire{}, fmt.Errorf("droids: boundary message kind is required")
	}
	if len(message.ID) > 256 || strings.IndexByte(message.ID, 0) >= 0 {
		return BoundaryMessageWire{}, fmt.Errorf("droids: boundary message id is invalid")
	}
	for _, receiptID := range message.ReceiptIDs {
		if receiptID == "" || len(receiptID) > 256 || strings.IndexByte(receiptID, 0) >= 0 {
			return BoundaryMessageWire{}, fmt.Errorf("droids: boundary receipt id is invalid")
		}
	}
	if len(message.Details) > 0 && !json.Valid(message.Details) {
		return BoundaryMessageWire{}, fmt.Errorf("droids: boundary details are not valid JSON")
	}
	if len(message.Details) > maxToolDetailsBytes {
		return BoundaryMessageWire{}, fmt.Errorf("droids: boundary details exceed %d bytes", maxToolDetailsBytes)
	}
	return BoundaryMessageWire{
		ID: message.ID, ReceiptIDs: append([]string(nil), message.ReceiptIDs...),
		Kind: message.Kind, Source: message.Source, Content: content,
		Details: append(json.RawMessage(nil), message.Details...),
	}, nil
}
