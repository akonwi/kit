package protocol

import (
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/identifier"
)

// Validate checks cumulative usage received across a transport boundary.
func (usage SessionUsage) Validate() error {
	for name, value := range map[string]int{
		"input": usage.Input, "output": usage.Output,
		"cache read": usage.CacheRead, "cache write": usage.CacheWrite,
		"reasoning": usage.Reasoning, "total": usage.TotalTokens,
	} {
		if value < 0 {
			return fmt.Errorf("%s tokens cannot be negative", name)
		}
	}
	for name, value := range map[string]float64{
		"input": usage.Cost.Input, "output": usage.Cost.Output,
		"cache read": usage.Cost.CacheRead, "cache write": usage.Cost.CacheWrite,
		"total": usage.Cost.Total,
	} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%s cost is invalid", name)
		}
	}
	return nil
}

// Validate checks a session snapshot received across a transport boundary.
func (snapshot SessionSnapshot) Validate() error {
	if err := snapshot.Session.Validate(); err != nil {
		return fmt.Errorf("snapshot session: %w", err)
	}
	if snapshot.Session.ThinkingLevel == "" {
		return fmt.Errorf("snapshot session thinking level is missing")
	}
	if snapshot.ContextTokens < 0 || snapshot.ContextWindow < 0 || snapshot.EventCursor < 0 || snapshot.EventReplayFrom < 0 {
		return fmt.Errorf("snapshot context usage cannot be negative")
	}
	if snapshot.ContextWindow == 0 && snapshot.ContextTokens != 0 {
		return fmt.Errorf("snapshot context tokens require a context window")
	}
	if err := snapshot.Usage.Validate(); err != nil {
		return fmt.Errorf("snapshot usage: %w", err)
	}
	if snapshot.EventReplayAvailable && (snapshot.ActiveRunID == "" || snapshot.EventStreamID == "" || snapshot.EventReplayFrom > snapshot.EventCursor) {
		return fmt.Errorf("snapshot replay metadata is incomplete")
	}
	if len(snapshot.PromptCommands) > 128 || len(snapshot.Warnings) > 8 || len(snapshot.PendingInteractions) > MaxPendingInteractions ||
		len(snapshot.SubagentDefinitions) > 128 || len(snapshot.SubagentDiagnostics) > 128 || len(snapshot.SubagentConversations) > 128 || len(snapshot.SubagentMailbox) > 64 {
		return fmt.Errorf("snapshot has too many commands, warnings, or subagent records")
	}
	if err := validateSubagentSnapshot(snapshot); err != nil {
		return err
	}
	if err := snapshot.FollowUps.Validate(); err != nil {
		return fmt.Errorf("snapshot follow-ups: %w", err)
	}
	seenInteractions := make(map[string]struct{}, len(snapshot.PendingInteractions))
	for index, interaction := range snapshot.PendingInteractions {
		if err := interaction.Validate(); err != nil {
			return fmt.Errorf("snapshot interaction %d: %w", index, err)
		}
		if interaction.SessionID != snapshot.Session.ID {
			return fmt.Errorf("snapshot interaction %d session identity mismatch", index)
		}
		if _, duplicate := seenInteractions[interaction.ID]; duplicate {
			return fmt.Errorf("snapshot interaction %d is duplicated", index)
		}
		seenInteractions[interaction.ID] = struct{}{}
	}
	for index, warning := range snapshot.Warnings {
		if !validRendererText(warning, 4096) || strings.TrimSpace(warning) == "" {
			return fmt.Errorf("snapshot warning %d is invalid", index)
		}
	}
	seenCommands := make(map[string]struct{}, len(snapshot.PromptCommands))
	for index, command := range snapshot.PromptCommands {
		if !validPromptCommandName(command.Name) || !validRendererText(command.Description, 1024) ||
			(command.Source != "user" && command.Source != "project") || !filepath.IsAbs(command.Location) || !validRendererText(command.Location, 4096) {
			return fmt.Errorf("snapshot prompt command %d is invalid", index)
		}
		if _, duplicate := seenCommands[command.Name]; duplicate {
			return fmt.Errorf("snapshot prompt command %d duplicates %q", index, command.Name)
		}
		seenCommands[command.Name] = struct{}{}
	}
	previousSequence := int64(-1)
	messageIDs := make(map[string]struct{})
	closedTurns := make(map[string]struct{})
	currentTurn := ""
	toolCallsByTurn := make(map[string]map[string]string)
	toolResultsByTurn := make(map[string]map[string]struct{})
	activeBashID := ""
	for index, boundary := range snapshot.PendingBoundaries {
		if boundary.ID == "" || boundary.Kind == "" || len(boundary.Content) == 0 {
			return fmt.Errorf("pending boundary %d requires identity, kind, and content", index)
		}
		if err := validateBoundaryDetails(boundary.Kind, boundary.Details); err != nil {
			return fmt.Errorf("pending boundary %d: %w", index, err)
		}
		if _, err := time.Parse(time.RFC3339Nano, boundary.AcceptedAt); err != nil {
			return fmt.Errorf("pending boundary %d acceptedAt is invalid: %w", index, err)
		}
		for contentIndex, block := range boundary.Content {
			if err := block.validate(); err != nil || !contentAllowedForRole("context", block.Kind) {
				return fmt.Errorf("pending boundary %d content %d is invalid", index, contentIndex)
			}
		}
	}
	for index, message := range snapshot.Messages {
		if message.ID == "" || message.Role != "bash" && message.TurnID == "" {
			return fmt.Errorf("snapshot message %d requires its message and turn identities", index)
		}
		if _, duplicate := messageIDs[message.ID]; duplicate {
			return fmt.Errorf("snapshot message %d duplicates message id %q", index, message.ID)
		}
		messageIDs[message.ID] = struct{}{}
		if message.Role != "bash" && message.TurnID != currentTurn {
			if _, reused := closedTurns[message.TurnID]; reused {
				return fmt.Errorf("snapshot message %d reopens noncontiguous turn %q", index, message.TurnID)
			}
			if currentTurn != "" {
				closedTurns[currentTurn] = struct{}{}
			}
			currentTurn = message.TurnID
		}
		if message.Sequence <= previousSequence {
			return fmt.Errorf("snapshot message %d sequence %d is not increasing", index, message.Sequence)
		}
		previousSequence = message.Sequence
		switch message.Role {
		case "user", "assistant", "tool", "context", "bash":
		default:
			return fmt.Errorf("snapshot message %d role %q is invalid", index, message.Role)
		}
		if err := message.validate(); err != nil {
			return fmt.Errorf("snapshot message %d: %w", index, err)
		}
		if message.Role == "bash" {
			if message.Bash.ID != message.ID || message.Bash.SessionID != snapshot.Session.ID || message.Bash.Sequence != message.Sequence {
				return fmt.Errorf("snapshot message %d bash identity mismatch", index)
			}
			if message.Bash.Status == BashExecutionRunning {
				if activeBashID != "" {
					return fmt.Errorf("snapshot has multiple running bash executions")
				}
				activeBashID = message.Bash.ID
			}
		}
		if message.Role == "assistant" {
			calls := toolCallsByTurn[message.TurnID]
			if calls == nil {
				calls = make(map[string]string)
				toolCallsByTurn[message.TurnID] = calls
			}
			for _, block := range message.Content {
				if block.Kind != TranscriptContentToolCall {
					continue
				}
				if _, duplicate := calls[block.ToolCallID]; duplicate {
					return fmt.Errorf("snapshot message %d duplicates tool call %q in turn", index, block.ToolCallID)
				}
				calls[block.ToolCallID] = block.ToolName
			}
		}
		if message.Role == "tool" {
			callName, found := toolCallsByTurn[message.TurnID][message.ToolCallID]
			if !found {
				return fmt.Errorf("snapshot message %d has no preceding tool call %q in turn", index, message.ToolCallID)
			}
			if callName != message.ToolName {
				return fmt.Errorf("snapshot message %d tool name %q does not match call name %q", index, message.ToolName, callName)
			}
			results := toolResultsByTurn[message.TurnID]
			if results == nil {
				results = make(map[string]struct{})
				toolResultsByTurn[message.TurnID] = results
			}
			if _, duplicate := results[message.ToolCallID]; duplicate {
				return fmt.Errorf("snapshot message %d duplicates result for tool call %q", index, message.ToolCallID)
			}
			results[message.ToolCallID] = struct{}{}
		}
		if _, err := time.Parse(time.RFC3339Nano, message.CreatedAt); err != nil {
			return fmt.Errorf("snapshot message %d createdAt is invalid: %w", index, err)
		}
	}
	if activeBashID != "" && snapshot.ActiveBashExecutionID != activeBashID {
		return fmt.Errorf("snapshot active bash execution does not match included bash message")
	}
	return nil
}

func validateSubagentSnapshot(snapshot SessionSnapshot) error {
	seenDefinitions := make(map[string]struct{}, len(snapshot.SubagentDefinitions))
	for index, definition := range snapshot.SubagentDefinitions {
		if !validRendererText(definition.Name, 128) || strings.ContainsAny(definition.Name, " /\\\t\r\n") ||
			!validRendererText(definition.Description, 1024) || !validSubagentSource(definition.Source) {
			return fmt.Errorf("snapshot subagent definition %d is invalid", index)
		}
		if definition.Model != "" && !validRendererText(definition.Model, 256) {
			return fmt.Errorf("snapshot subagent definition %d model is invalid", index)
		}
		if _, duplicate := seenDefinitions[definition.Name]; duplicate {
			return fmt.Errorf("snapshot subagent definition %d duplicates %q", index, definition.Name)
		}
		seenDefinitions[definition.Name] = struct{}{}
	}
	for index, diagnostic := range snapshot.SubagentDiagnostics {
		if diagnostic.Severity != "warning" || !validRendererText(diagnostic.Code, 256) ||
			!validRendererText(diagnostic.Message, 4096) || !validSubagentSource(diagnostic.Source) {
			return fmt.Errorf("snapshot subagent diagnostic %d is invalid", index)
		}
	}
	for index, item := range snapshot.SubagentMailbox {
		if !identifier.Valid(item.ID, "mail_") || !identifier.Valid(item.ConversationID, "subagent_") ||
			!identifier.Valid(item.TaskID, "task_") || !validRendererText(item.AgentName, 128) ||
			!validSubagentTaskState(item.State) || item.State == "queued" || item.State == "running" || len(item.Summary) > 16<<10 || len(item.Error) > 16<<10 {
			return fmt.Errorf("snapshot subagent mailbox item %d is invalid", index)
		}
		if _, err := time.Parse(time.RFC3339Nano, item.CreatedAt); err != nil {
			return fmt.Errorf("snapshot subagent mailbox item %d createdAt is invalid", index)
		}
	}
	seenConversations := make(map[string]struct{}, len(snapshot.SubagentConversations))
	for index, conversation := range snapshot.SubagentConversations {
		if !identifier.Valid(conversation.ID, "subagent_") || !validRendererText(conversation.AgentName, 128) ||
			!validRendererText(conversation.Model, 256) || strings.Count(conversation.Model, "/") != 1 ||
			conversation.Generation == 0 || conversation.QueuedTasks < 0 ||
			(conversation.ActiveTaskID != "" && !identifier.Valid(conversation.ActiveTaskID, "task_")) ||
			(conversation.LastCompletedTaskID != "" && !identifier.Valid(conversation.LastCompletedTaskID, "task_")) ||
			!validSubagentConversationState(conversation.State) || len(conversation.Tasks) > 20 {
			return fmt.Errorf("snapshot subagent conversation %d is invalid", index)
		}
		if _, err := time.Parse(time.RFC3339Nano, conversation.UpdatedAt); err != nil {
			return fmt.Errorf("snapshot subagent conversation %d updatedAt is invalid", index)
		}
		if _, duplicate := seenConversations[conversation.ID]; duplicate {
			return fmt.Errorf("snapshot subagent conversation %d is duplicated", index)
		}
		seenConversations[conversation.ID] = struct{}{}
		var previous uint64
		for taskIndex, task := range conversation.Tasks {
			if !identifier.Valid(task.ID, "task_") || task.Sequence == 0 || task.Sequence <= previous || task.QueuedAt == "" ||
				task.CancellationGeneration == 0 || !validSubagentTaskState(task.State) {
				return fmt.Errorf("snapshot subagent conversation %d task %d is invalid", index, taskIndex)
			}
			previous = task.Sequence
			for name, value := range map[string]string{"queuedAt": task.QueuedAt, "startedAt": task.StartedAt, "finishedAt": task.FinishedAt} {
				if value != "" {
					if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
						return fmt.Errorf("snapshot subagent conversation %d task %d %s is invalid", index, taskIndex, name)
					}
				}
			}
			queuedAt, _ := time.Parse(time.RFC3339Nano, task.QueuedAt)
			if task.StartedAt != "" {
				startedAt, _ := time.Parse(time.RFC3339Nano, task.StartedAt)
				if startedAt.Before(queuedAt) {
					return fmt.Errorf("snapshot subagent conversation %d task %d starts before queue admission", index, taskIndex)
				}
			}
			if terminalSubagentTaskState(task.State) != (task.FinishedAt != "") {
				return fmt.Errorf("snapshot subagent conversation %d task %d terminal timestamp mismatch", index, taskIndex)
			}
		}
	}
	return nil
}

func validSubagentSource(source SubagentSource) bool {
	if source.Kind != "user" && source.Kind != "project" && source.Kind != "plugin" {
		return false
	}
	if !validRendererText(source.Path, 4096) {
		return false
	}
	if source.Kind != "plugin" && !filepath.IsAbs(source.Path) {
		return false
	}
	if source.Kind == "plugin" {
		return validRendererText(source.PluginID, 256)
	}
	return source.PluginID == ""
}

func validSubagentConversationState(state string) bool {
	switch state {
	case "idle", "running", "failed", "aborted", "interrupted":
		return true
	default:
		return false
	}
}

func terminalSubagentTaskState(state string) bool {
	return state == "completed" || state == "failed" || state == "aborted" || state == "interrupted"
}

func validSubagentTaskState(state string) bool {
	switch state {
	case "queued", "running", "completed", "failed", "aborted", "interrupted":
		return true
	default:
		return false
	}
}

func (message TranscriptMessage) validate() error {
	if message.Details != nil && !json.Valid(message.Details) {
		return fmt.Errorf("details are not valid JSON")
	}
	for index, block := range message.Content {
		if err := block.validate(); err != nil {
			return fmt.Errorf("content block %d: %w", index, err)
		}
		if !contentAllowedForRole(message.Role, block.Kind) {
			return fmt.Errorf("content block %d kind %q is invalid for role %q", index, block.Kind, message.Role)
		}
	}
	switch message.Role {
	case "user":
		if message.Bash != nil || message.StopReason != "" || message.ErrorMessage != "" || message.ToolCallID != "" || message.ToolName != "" || message.BoundaryID != "" || message.BoundaryKind != "" || message.BoundarySource != "" || message.Details != nil || message.IsError {
			return fmt.Errorf("user message carries assistant, bash, or tool metadata")
		}
	case "assistant":
		if message.Bash != nil {
			return fmt.Errorf("assistant message carries bash metadata")
		}
		switch message.StopReason {
		case "", "stop", "length", "toolUse", "contextWindow", "error", "aborted":
		default:
			return fmt.Errorf("assistant stop reason %q is invalid", message.StopReason)
		}
		if message.ToolCallID != "" || message.ToolName != "" || message.BoundaryID != "" || message.BoundaryKind != "" || message.BoundarySource != "" || message.Details != nil {
			return fmt.Errorf("assistant message carries tool-result metadata")
		}
		shouldBeError := message.StopReason == "error" || message.StopReason == "aborted"
		if message.IsError != shouldBeError {
			return fmt.Errorf("assistant error state does not match stop reason")
		}
	case "context":
		if message.Bash != nil || message.StopReason != "" || message.ErrorMessage != "" || message.ToolCallID != "" || message.ToolName != "" || message.IsError {
			return fmt.Errorf("context message carries unrelated metadata")
		}
		if message.BoundaryKind == "" || message.BoundaryKind != "summary" && message.BoundaryID == "" {
			return fmt.Errorf("context message requires kind and external boundaries require identity")
		}
		if err := validateBoundaryDetails(message.BoundaryKind, message.Details); err != nil {
			return err
		}
	case "tool":
		if message.Bash != nil {
			return fmt.Errorf("tool result carries bash metadata")
		}
		if message.ToolCallID == "" || message.ToolName == "" {
			return fmt.Errorf("tool result requires call id and name")
		}
		if message.StopReason != "" || message.ErrorMessage != "" || message.BoundaryID != "" || message.BoundaryKind != "" || message.BoundarySource != "" {
			return fmt.Errorf("tool result carries assistant metadata")
		}
	case "bash":
		if message.TurnID != "" || len(message.Content) != 0 || message.Bash == nil || message.StopReason != "" || message.ErrorMessage != "" || message.ToolCallID != "" || message.ToolName != "" || message.BoundaryID != "" || message.BoundaryKind != "" || message.BoundarySource != "" || message.Details != nil || message.IsError {
			return fmt.Errorf("bash message has invalid transcript fields")
		}
		if err := message.Bash.Validate(); err != nil {
			return fmt.Errorf("bash execution: %w", err)
		}
	}
	return nil
}

func (block TranscriptContent) validate() error {
	hasToolData := block.ToolCallID != "" || block.ToolName != "" || block.Arguments != "" || block.ArgumentsTruncated
	hasFileData := block.Filename != "" || block.MediaType != ""
	switch block.Kind {
	case TranscriptContentText, TranscriptContentThinking:
		if block.Text == "" {
			return fmt.Errorf("%s content requires text", block.Kind)
		}
		if hasToolData || hasFileData {
			return fmt.Errorf("%s content carries unrelated metadata", block.Kind)
		}
	case TranscriptContentToolCall:
		if block.Text != "" || block.ToolCallID == "" || block.ToolName == "" || hasFileData {
			return fmt.Errorf("tool call requires call id and name only")
		}
		if (block.Arguments == "") == !block.ArgumentsTruncated {
			return fmt.Errorf("tool call requires either complete or explicitly truncated arguments")
		}
	case TranscriptContentImage:
		if block.Text != "" || hasToolData || !validMediaType(block.MediaType, true) {
			return fmt.Errorf("image content requires an image media type without text or tool metadata")
		}
	case TranscriptContentFile:
		if block.Text != "" || hasToolData || strings.TrimSpace(block.Filename) == "" || !validMediaType(block.MediaType, false) {
			return fmt.Errorf("file content requires filename and media type only")
		}
	default:
		return fmt.Errorf("kind %q is invalid", block.Kind)
	}
	return nil
}

func contentAllowedForRole(role string, kind TranscriptContentKind) bool {
	switch role {
	case "user", "context":
		return kind == TranscriptContentText || kind == TranscriptContentImage || kind == TranscriptContentFile
	case "assistant":
		return kind == TranscriptContentText || kind == TranscriptContentThinking || kind == TranscriptContentToolCall
	case "tool":
		return kind == TranscriptContentText || kind == TranscriptContentImage || kind == TranscriptContentFile
	default:
		return false
	}
}

func validMediaType(raw string, imageOnly bool) bool {
	parsed, _, err := mime.ParseMediaType(raw)
	parts := strings.Split(parsed, "/")
	if err != nil || len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == "*" || parts[1] == "*" {
		return false
	}
	return !imageOnly || strings.EqualFold(parts[0], "image")
}

func validateBoundaryDetails(kind string, details json.RawMessage) error {
	if len(details) == 0 {
		return nil // Grandfathered boundary records predate structured details.
	}
	if len(details) > 64<<10 || !json.Valid(details) {
		return fmt.Errorf("boundary details are invalid or exceed 64 KiB")
	}
	if kind != "bash" {
		return nil
	}
	var payload struct {
		Version      int    `json:"version"`
		Command      string `json:"command"`
		Status       string `json:"status"`
		ExitCode     *int   `json:"exitCode"`
		ErrorMessage string `json:"errorMessage"`
		StartedAt    string `json:"startedAt"`
		CompletedAt  string `json:"completedAt"`
	}
	if err := json.Unmarshal(details, &payload); err != nil {
		return fmt.Errorf("decode bash boundary details: %w", err)
	}
	if payload.Version != 1 || strings.TrimSpace(payload.Command) == "" {
		return fmt.Errorf("bash boundary details require version 1 and command")
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.StartedAt); err != nil {
		return fmt.Errorf("bash boundary startedAt is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.CompletedAt); err != nil {
		return fmt.Errorf("bash boundary completedAt is invalid")
	}
	switch payload.Status {
	case "completed":
		if payload.ErrorMessage != "" {
			return fmt.Errorf("completed bash boundary carries an error")
		}
	case "failed", "aborted":
		if strings.TrimSpace(payload.ErrorMessage) == "" {
			return fmt.Errorf("bash boundary status %q requires an error", payload.Status)
		}
	default:
		return fmt.Errorf("bash boundary status %q is invalid", payload.Status)
	}
	return nil
}
