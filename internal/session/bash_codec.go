package session

import (
	"encoding/json"
	"fmt"

	"github.com/akonwi/kit/internal/droids"
)

const bashBoundaryVersion = 1

type bashBoundaryDetails struct {
	Version      int                 `json:"version"`
	Command      string              `json:"command"`
	Status       BashExecutionStatus `json:"status"`
	ExitCode     *int                `json:"exitCode,omitempty"`
	Truncated    bool                `json:"truncated,omitempty"`
	TimedOut     bool                `json:"timedOut,omitempty"`
	ErrorMessage string              `json:"errorMessage,omitempty"`
	StartedAt    string              `json:"startedAt"`
	CompletedAt  string              `json:"completedAt"`
}

func encodeBashDetails(execution BashExecution) ([]byte, error) {
	if execution.CompletedAt == nil {
		return nil, fmt.Errorf("bash boundary requires terminal execution")
	}
	return json.Marshal(bashBoundaryDetails{
		Version: bashBoundaryVersion, Command: execution.Command, Status: execution.Status,
		ExitCode: cloneInt(execution.ExitCode), Truncated: execution.Truncated, TimedOut: execution.TimedOut,
		ErrorMessage: execution.ErrorMessage,
		StartedAt:    execution.StartedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		CompletedAt:  execution.CompletedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	})
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
	return droids.UserMessage{Content: []droids.InputContent{droids.TextInput{Text: text}}}
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
