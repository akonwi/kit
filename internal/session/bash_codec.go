package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

const persistedBashExecutionVersion = 1

type persistedBashExecution struct {
	Version             int                 `json:"version"`
	Type                string              `json:"type"`
	Command             string              `json:"command"`
	CWD                 string              `json:"cwd"`
	Status              BashExecutionStatus `json:"status"`
	Output              string              `json:"output,omitempty"`
	ExitCode            *int                `json:"exitCode,omitempty"`
	ExcludeFromContext  bool                `json:"excludeFromContext,omitempty"`
	Truncated           bool                `json:"truncated,omitempty"`
	TimedOut            bool                `json:"timedOut,omitempty"`
	ErrorMessage        string              `json:"errorMessage,omitempty"`
	ContextBeforeTurnID string              `json:"contextBeforeTurnId,omitempty"`
	StartedAt           string              `json:"startedAt"`
	CompletedAt         string              `json:"completedAt,omitempty"`
}

func encodeRunningBashExecution(command, cwd string, exclude bool, startedAt time.Time) ([]byte, error) {
	return encodePersistedBashExecution(persistedBashExecution{
		Version: persistedBashExecutionVersion, Type: "bash", Command: command, CWD: cwd,
		Status: BashExecutionRunning, ExcludeFromContext: exclude,
		StartedAt: startedAt.UTC().Format(time.RFC3339Nano),
	})
}

func encodeCompletedBashExecution(execution BashExecution, result BashExecutionResult) ([]byte, error) {
	completedAt := result.CompletedAt.UTC()
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	return encodePersistedBashExecution(persistedBashExecution{
		Version: persistedBashExecutionVersion, Type: "bash", Command: execution.Command, CWD: execution.CWD,
		Status: result.Status, Output: result.Output, ExitCode: cloneInt(result.ExitCode),
		ExcludeFromContext: execution.ExcludeFromContext, Truncated: result.Truncated,
		TimedOut: result.TimedOut, ErrorMessage: result.ErrorMessage,
		ContextBeforeTurnID: execution.ContextBeforeTurnID,
		StartedAt:           execution.StartedAt.UTC().Format(time.RFC3339Nano),
		CompletedAt:         completedAt.Format(time.RFC3339Nano),
	})
}

func encodePersistedBashExecution(payload persistedBashExecution) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode bash execution: %w", err)
	}
	return body, nil
}

func decodeBashExecution(record MessageRecord) (BashExecution, error) {
	if record.Role != "bash" {
		return BashExecution{}, fmt.Errorf("message role %q is not bash", record.Role)
	}
	var payload persistedBashExecution
	if err := json.Unmarshal(record.PayloadJSON, &payload); err != nil {
		return BashExecution{}, fmt.Errorf("decode persisted bash execution: %w", err)
	}
	if payload.Version != persistedBashExecutionVersion || payload.Type != "bash" {
		return BashExecution{}, fmt.Errorf("unsupported persisted bash execution version %d type %q", payload.Version, payload.Type)
	}
	if strings.TrimSpace(payload.Command) == "" || payload.CWD == "" {
		return BashExecution{}, fmt.Errorf("persisted bash execution command and cwd are required")
	}
	startedAt, err := time.Parse(time.RFC3339Nano, payload.StartedAt)
	if err != nil {
		return BashExecution{}, fmt.Errorf("parse bash startedAt: %w", err)
	}
	var completedAt *time.Time
	if payload.CompletedAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, payload.CompletedAt)
		if err != nil {
			return BashExecution{}, fmt.Errorf("parse bash completedAt: %w", err)
		}
		completedAt = &parsed
	}
	execution := BashExecution{
		ID: record.ID, SessionID: record.SessionID, Sequence: record.Sequence,
		Command: payload.Command, CWD: payload.CWD, Status: payload.Status,
		Output: payload.Output, ExitCode: cloneInt(payload.ExitCode),
		ExcludeFromContext: payload.ExcludeFromContext, Truncated: payload.Truncated,
		TimedOut: payload.TimedOut, ErrorMessage: payload.ErrorMessage,
		ContextBeforeTurnID: payload.ContextBeforeTurnID,
		StartedAt:           startedAt, CompletedAt: completedAt,
	}
	if err := validateBashExecution(execution); err != nil {
		return BashExecution{}, err
	}
	return execution, nil
}

func validateBashExecution(execution BashExecution) error {
	switch execution.Status {
	case BashExecutionRunning:
		if execution.CompletedAt != nil || execution.Output != "" || execution.ExitCode != nil || execution.Truncated || execution.TimedOut || execution.ErrorMessage != "" {
			return fmt.Errorf("running bash execution carries terminal data")
		}
	case BashExecutionCompleted:
		if execution.CompletedAt == nil || execution.ErrorMessage != "" {
			return fmt.Errorf("completed bash execution has invalid terminal metadata")
		}
	case BashExecutionFailed, BashExecutionAborted, BashExecutionInterrupted:
		if execution.CompletedAt == nil || strings.TrimSpace(execution.ErrorMessage) == "" {
			return fmt.Errorf("bash execution status %q requires completion and an error", execution.Status)
		}
	default:
		return fmt.Errorf("bash execution status %q is invalid", execution.Status)
	}
	return nil
}

func bashContextMessage(execution BashExecution) droids.UserMessage {
	exitInfo := ""
	if execution.ExitCode != nil && *execution.ExitCode != 0 {
		exitInfo = fmt.Sprintf(" (exit code: %d)", *execution.ExitCode)
	}
	output := execution.Output
	appendNotice := func(notice string) {
		if output != "" {
			output += "\n"
		}
		output += notice
	}
	if execution.TimedOut {
		appendNotice("[timed out]")
	}
	if execution.Truncated {
		appendNotice("[output truncated]")
	}
	if execution.ErrorMessage != "" {
		appendNotice("[" + string(execution.Status) + ": " + execution.ErrorMessage + "]")
	}
	text := "[bash command: " + execution.Command + "]" + exitInfo
	if output != "" {
		text += "\n" + output
	}
	return droids.UserMessage{
		Content:   []droids.Content{droids.TextContent{Text: text}},
		Timestamp: execution.StartedAt.UnixMilli(),
	}
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
