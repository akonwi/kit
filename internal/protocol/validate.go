package protocol

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
)

// Validate checks a session-creation request crossing a transport boundary.
func (input CreateSessionInput) Validate() error {
	if input.ID != "" && !identifier.Valid(input.ID, "session_") {
		return fmt.Errorf("session id %q is not canonical", input.ID)
	}
	if input.Temporary && input.ID == "" {
		return fmt.Errorf("temporary session id is required")
	}
	return nil
}

// Validate checks a session rename crossing a transport boundary.
func (input RenameSessionInput) Validate() error {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 256 || !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 {
		return fmt.Errorf("session name must be non-empty valid UTF-8 without NUL and at most 256 bytes")
	}
	return nil
}

// Validate checks reload metadata crossing a transport boundary.
func (result ReloadSessionResult) Validate() error {
	if !identifier.Valid(result.SessionID, "session_") || !identifier.Valid(result.EventStreamID, "stream_") {
		return fmt.Errorf("reload result has invalid session or event stream identity")
	}
	if len(result.Sources) == 0 || len(result.Sources) > 512 || len(result.Diagnostics) > 128 || len(result.Warnings) > 8 {
		return fmt.Errorf("reload result source, diagnostic, or warning count is invalid")
	}
	seen := make(map[string]struct{}, len(result.Sources))
	for index, source := range result.Sources {
		if err := source.validate(); err != nil {
			return fmt.Errorf("reload source %d: %w", index, err)
		}
		key := source.SectionID + "\x00" + source.ID
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("reload source %d duplicates an identity", index)
		}
		seen[key] = struct{}{}
	}
	for index, warning := range result.Warnings {
		if !validProtocolText(warning, 4096) || strings.TrimSpace(warning) == "" {
			return fmt.Errorf("reload warning %d is invalid", index)
		}
	}
	for index, diagnostic := range result.Diagnostics {
		if diagnostic.Severity != "info" && diagnostic.Severity != "warning" {
			return fmt.Errorf("reload diagnostic %d has invalid severity %q", index, diagnostic.Severity)
		}
		if !validProtocolText(diagnostic.Code, 128) || strings.TrimSpace(diagnostic.Code) != diagnostic.Code ||
			!validProtocolText(diagnostic.Message, 4096) || strings.TrimSpace(diagnostic.Message) == "" {
			return fmt.Errorf("reload diagnostic %d has invalid code or message", index)
		}
		if err := diagnostic.Source.validate(); err != nil {
			return fmt.Errorf("reload diagnostic %d source: %w", index, err)
		}
	}
	return nil
}

func (source PromptSource) validate() error {
	if !validProtocolText(source.SectionID, 128) || strings.TrimSpace(source.SectionID) != source.SectionID ||
		!validProtocolText(source.ID, 256) || strings.TrimSpace(source.ID) != source.ID {
		return fmt.Errorf("source section and identity are required")
	}
	switch source.Kind {
	case PromptSectionCore, PromptSectionFeature, PromptSectionSkillCatalog, PromptSectionPlugin, PromptSectionContext:
	default:
		return fmt.Errorf("source kind %q is invalid", source.Kind)
	}
	if source.Path != "" && (!filepath.IsAbs(source.Path) || !validProtocolText(source.Path, 4096)) {
		return fmt.Errorf("source path is not a valid bounded absolute path")
	}
	return nil
}

func validProtocolText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && strings.IndexByte(value, 0) < 0
}

// Validate checks a session projection received across a transport boundary.
func (session SessionInfo) Validate() error {
	if session.ID == "" {
		return fmt.Errorf("session id is empty")
	}
	if !filepath.IsAbs(session.CWD) {
		return fmt.Errorf("session cwd %q is not absolute", session.CWD)
	}
	provider, model, ok := strings.Cut(session.Model, "/")
	if !ok || provider == "" || model == "" {
		return fmt.Errorf("session model %q is not namespaced", session.Model)
	}
	if _, err := time.Parse(time.RFC3339Nano, session.CreatedAt); err != nil {
		return fmt.Errorf("session createdAt is invalid: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, session.UpdatedAt); err != nil {
		return fmt.Errorf("session updatedAt is invalid: %w", err)
	}
	return nil
}

// Validate checks a prompt request crossing a transport boundary.
func (input PromptInput) Validate() error {
	if strings.TrimSpace(input.Text) == "" || len(input.Text) > 128<<10 || !utf8.ValidString(input.Text) || strings.IndexByte(input.Text, 0) >= 0 {
		return fmt.Errorf("prompt must be non-empty valid UTF-8 without NUL and at most 128 KiB")
	}
	return nil
}

// Validate checks a direct bash request received across a transport boundary.
func (input BashExecutionInput) Validate() error {
	if input.ExecutionID == "" {
		return fmt.Errorf("bash execution id is empty")
	}
	if strings.TrimSpace(input.Command) == "" || len(input.Command) > 64<<10 || !utf8.ValidString(input.Command) || strings.IndexByte(input.Command, 0) >= 0 {
		return fmt.Errorf("bash command must be non-empty valid UTF-8 without NUL and at most 64 KiB")
	}
	return nil
}

// Validate checks a direct bash execution received across a transport boundary.
func (execution BashExecution) Validate() error {
	if execution.ID == "" || execution.SessionID == "" || execution.Sequence < 0 {
		return fmt.Errorf("bash execution id, session id, and non-negative sequence are required")
	}
	if strings.TrimSpace(execution.Command) == "" || len(execution.Command) > 64<<10 || !utf8.ValidString(execution.Command) || strings.IndexByte(execution.Command, 0) >= 0 {
		return fmt.Errorf("bash execution command is invalid")
	}
	if len(execution.Output) > 64<<10 || !utf8.ValidString(execution.Output) {
		return fmt.Errorf("bash execution output is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, execution.StartedAt); err != nil {
		return fmt.Errorf("bash execution startedAt is invalid: %w", err)
	}
	if execution.CompletedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, execution.CompletedAt); err != nil {
			return fmt.Errorf("bash execution completedAt is invalid: %w", err)
		}
	}
	switch execution.Status {
	case BashExecutionRunning:
		if execution.CompletedAt != "" || execution.Output != "" || execution.ExitCode != nil || execution.Truncated || execution.TimedOut || execution.ErrorMessage != "" {
			return fmt.Errorf("running bash execution carries terminal data")
		}
	case BashExecutionCompleted:
		if execution.CompletedAt == "" || execution.ErrorMessage != "" {
			return fmt.Errorf("completed bash execution has invalid terminal metadata")
		}
	case BashExecutionFailed, BashExecutionAborted, BashExecutionInterrupted:
		if execution.CompletedAt == "" || strings.TrimSpace(execution.ErrorMessage) == "" {
			return fmt.Errorf("bash execution status %q requires completion and an error", execution.Status)
		}
	default:
		return fmt.Errorf("bash execution status %q is invalid", execution.Status)
	}
	return nil
}

// Validate checks a run reservation received across a transport boundary.
func (reservation RunReservation) Validate() error {
	if reservation.SessionID == "" || reservation.TurnID == "" || reservation.RunID == "" {
		return fmt.Errorf("run reservation session, turn, and run ids are required")
	}
	if reservation.RunID != reservation.TurnID {
		return fmt.Errorf("run reservation identity must equal its droid turn identity")
	}
	return nil
}

// Validate checks a durable run projection received across a transport boundary.
func (run RunInfo) Validate() error {
	if run.SessionID == "" || run.TurnID == "" || run.RunID == "" {
		return fmt.Errorf("run info session, turn, and run ids are required")
	}
	if run.RunID != run.TurnID {
		return fmt.Errorf("run info identity must equal its droid turn identity")
	}
	switch run.Status {
	case RunStatusQueued, RunStatusRunning, RunStatusCompleted,
		RunStatusFailed, RunStatusAborted, RunStatusInterrupted:
	default:
		return fmt.Errorf("run info status %q is invalid", run.Status)
	}
	if run.Status != RunStatusQueued && run.Status != RunStatusRunning && run.Status != RunStatusCompleted && run.ErrorMessage == "" {
		return fmt.Errorf("run info status %q requires an error message", run.Status)
	}
	return nil
}

// Validate checks a terminal prompt outcome received across a transport boundary.
func (outcome PromptOutcome) Validate() error {
	if outcome.SessionID == "" || outcome.TurnID == "" || outcome.RunID == "" {
		return fmt.Errorf("prompt outcome session, turn, and run ids are required")
	}
	if outcome.RunID != outcome.TurnID {
		return fmt.Errorf("prompt outcome identity must equal its droid turn identity")
	}
	switch outcome.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusAborted, RunStatusInterrupted:
	default:
		return fmt.Errorf("prompt outcome status %q is invalid", outcome.Status)
	}
	if outcome.Status != RunStatusCompleted && outcome.ErrorMessage == "" {
		return fmt.Errorf("prompt outcome status %q requires an error message", outcome.Status)
	}
	switch outcome.ErrorKind {
	case "", ProviderErrorAuthentication, ProviderErrorEntitlement,
		ProviderErrorUsageLimit, ProviderErrorRateLimit, ProviderErrorTransport,
		ProviderErrorProtocol:
	default:
		return fmt.Errorf("prompt outcome error kind %q is invalid", outcome.ErrorKind)
	}
	if outcome.Status == RunStatusCompleted && outcome.ErrorKind != "" {
		return fmt.Errorf("completed prompt outcome has error kind %q", outcome.ErrorKind)
	}
	return nil
}
