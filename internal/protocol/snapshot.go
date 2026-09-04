package protocol

import (
	"encoding/json"
	"fmt"
	"mime"
	"strings"
	"time"
)

// Validate checks a session snapshot received across a transport boundary.
func (snapshot SessionSnapshot) Validate() error {
	if err := snapshot.Session.Validate(); err != nil {
		return fmt.Errorf("snapshot session: %w", err)
	}
	if snapshot.ContextTokens < 0 || snapshot.ContextWindow < 0 {
		return fmt.Errorf("snapshot context usage cannot be negative")
	}
	if snapshot.ContextWindow == 0 && snapshot.ContextTokens != 0 {
		return fmt.Errorf("snapshot context tokens require a context window")
	}
	previousSequence := int64(-1)
	messageIDs := make(map[string]struct{})
	closedTurns := make(map[string]struct{})
	currentTurn := ""
	toolCallsByTurn := make(map[string]map[string]string)
	toolResultsByTurn := make(map[string]map[string]struct{})
	for index, message := range snapshot.Messages {
		if message.ID == "" || message.TurnID == "" {
			return fmt.Errorf("snapshot message %d requires message and turn ids", index)
		}
		if _, duplicate := messageIDs[message.ID]; duplicate {
			return fmt.Errorf("snapshot message %d duplicates message id %q", index, message.ID)
		}
		messageIDs[message.ID] = struct{}{}
		if message.TurnID != currentTurn {
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
		case "user", "assistant", "tool":
		default:
			return fmt.Errorf("snapshot message %d role %q is invalid", index, message.Role)
		}
		if err := message.validate(); err != nil {
			return fmt.Errorf("snapshot message %d: %w", index, err)
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
	return nil
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
		if message.StopReason != "" || message.ErrorMessage != "" || message.ToolCallID != "" || message.ToolName != "" || message.Details != nil || message.IsError {
			return fmt.Errorf("user message carries assistant or tool metadata")
		}
	case "assistant":
		switch message.StopReason {
		case "", "stop", "length", "toolUse", "contextWindow", "error", "aborted":
		default:
			return fmt.Errorf("assistant stop reason %q is invalid", message.StopReason)
		}
		if message.ToolCallID != "" || message.ToolName != "" || message.Details != nil {
			return fmt.Errorf("assistant message carries tool-result metadata")
		}
		shouldBeError := message.StopReason == "error" || message.StopReason == "aborted"
		if message.IsError != shouldBeError {
			return fmt.Errorf("assistant error state does not match stop reason")
		}
	case "tool":
		if message.ToolCallID == "" || message.ToolName == "" {
			return fmt.Errorf("tool result requires call id and name")
		}
		if message.StopReason != "" || message.ErrorMessage != "" {
			return fmt.Errorf("tool result carries assistant metadata")
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
		if block.Text != "" || hasToolData || block.Filename != "" || !validMediaType(block.MediaType, true) {
			return fmt.Errorf("image content requires an image media type only")
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
	case "user":
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
