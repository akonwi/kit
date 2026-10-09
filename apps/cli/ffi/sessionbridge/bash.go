package sessionbridge

import (
	"encoding/json"
	"strconv"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
)

// Bash is the client-owned projection of one direct shell run, whether it is
// running, finished in this attachment, or persisted as session context.
type Bash struct {
	// ID is the execution ID, which a persisted run keeps as its boundary ID.
	ID      string
	Command string
	// Status is the canonical execution status: running, completed, failed,
	// aborted, or interrupted.
	Status      string
	ExitCode    int
	HasExitCode bool
	TimedOut    bool
	// Output is what the transcript shows: the command's output followed by
	// timeout and truncation notices, or the failure when there is no output.
	Output string
}

// Running reports whether the run has not settled.
func (b Bash) Running() bool { return b.Status == string(protocol.BashExecutionRunning) }

// Succeeded reports whether the run completed with exit code zero.
func (b Bash) Succeeded() bool {
	return b.Status == string(protocol.BashExecutionCompleted) && !b.TimedOut && b.HasExitCode && b.ExitCode == 0
}

// BashOf projects an execution reported by the server.
func BashOf(execution protocol.BashExecution) Bash {
	output := strings.TrimRight(execution.Output, "\r\n")
	if execution.TimedOut {
		output = withNotice(output, "[timed out]")
	}
	if execution.Truncated {
		output = withNotice(output, "[output truncated]")
	}
	if output == "" && execution.ErrorMessage != "" {
		output = execution.ErrorMessage
	}
	projected := Bash{
		ID: execution.ID, Command: execution.Command, Status: string(execution.Status),
		TimedOut: execution.TimedOut, Output: output,
	}
	if execution.ExitCode != nil {
		projected.ExitCode, projected.HasExitCode = *execution.ExitCode, true
	}
	return projected
}

func withNotice(output, notice string) string {
	if output != "" {
		output += "\n"
	}
	return output + notice
}

// bashDetails is the version 1 bash boundary record the server persists.
type bashDetails struct {
	Version      int    `json:"version"`
	Command      string `json:"command"`
	Status       string `json:"status"`
	ExitCode     *int   `json:"exitCode"`
	TimedOut     bool   `json:"timedOut"`
	ErrorMessage string `json:"errorMessage"`
}

// bashBoundary projects a persisted bash boundary: its details record the
// run, and its text is what the model read, "[bash command: <command>]" with
// any nonzero exit code, then the output and its notices on later lines.
func bashBoundary(id, kind string, content []protocol.TranscriptContent, raw json.RawMessage) (Bash, bool) {
	var details bashDetails
	if kind != "bash" || len(raw) == 0 || json.Unmarshal(raw, &details) != nil || details.Version != 1 || strings.TrimSpace(details.Command) == "" {
		return Bash{}, false
	}
	var text strings.Builder
	for _, block := range content {
		if value, ok := block.Payload.(protocol.TextContent); ok {
			text.WriteString(value.Text)
		}
	}
	heading := "[bash command: " + details.Command + "]"
	if details.ExitCode != nil && *details.ExitCode != 0 {
		heading += " (exit code: " + strconv.Itoa(*details.ExitCode) + ")"
	}
	output := ""
	if rest, ok := strings.CutPrefix(text.String(), heading); ok {
		output = strings.TrimPrefix(rest, "\n")
	}
	if details.ErrorMessage != "" {
		failure := "[" + details.Status + ": " + details.ErrorMessage + "]"
		output = strings.TrimSuffix(strings.TrimSuffix(output, failure), "\n")
		if output == "" {
			output = details.ErrorMessage
		}
	}
	projected := Bash{
		ID: id, Command: details.Command, Status: details.Status,
		TimedOut: details.TimedOut, Output: output,
	}
	if details.ExitCode != nil {
		projected.ExitCode, projected.HasExitCode = *details.ExitCode, true
	}
	return projected, true
}

// PendingBash projects the bash runs that have finished but are not yet part
// of the transcript, in the order they were accepted.
func PendingBash(boundaries []protocol.PendingBoundary) []Bash {
	result := make([]Bash, 0, len(boundaries))
	for _, boundary := range boundaries {
		if projected, ok := bashBoundary(boundary.ID, boundary.Kind, boundary.Content, boundary.Details); ok {
			result = append(result, projected)
		}
	}
	return result
}
