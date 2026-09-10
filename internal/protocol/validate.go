package protocol

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode"
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
	name := strings.TrimSpace(input.Name)
	if name != "" && !ValidSessionName(name) {
		return fmt.Errorf("session name must be renderer-safe UTF-8 and at most 256 bytes")
	}
	provider, model, ok := strings.Cut(input.Model, "/")
	if !ok || !validRendererText(provider, 128) || !validRendererText(model, 256) {
		return fmt.Errorf("session model must use an exact provider/model id")
	}
	if input.ThinkingLevel != "" {
		if err := ThinkingLevel(input.ThinkingLevel).Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks a session cwd change crossing a transport boundary.
func (level ThinkingLevel) Validate() error {
	switch level {
	case ThinkingOff, ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingXHigh, ThinkingMax:
		return nil
	default:
		return fmt.Errorf("thinking level %q is invalid", level)
	}
}

// Validate checks a selectable model capability crossing a transport boundary.
func (model ModelCapability) Validate() error {
	provider, modelID, ok := strings.Cut(model.ID, "/")
	if !ok || provider != model.Provider || !validRendererText(provider, 128) || !validRendererText(modelID, 256) ||
		!validRendererText(model.Name, 256) || !validRendererText(model.Provider, 128) {
		return fmt.Errorf("model identity, name, or provider is invalid")
	}
	switch model.API {
	case "openai-responses", "openai-codex-responses", "anthropic-messages":
	default:
		return fmt.Errorf("model API %q is invalid", model.API)
	}
	if model.ContextWindow <= 0 || model.MaxInputTokens < 0 || model.MaxOutputTokens < 0 ||
		model.MaxInputTokens > model.ContextWindow || model.MaxOutputTokens > model.ContextWindow {
		return fmt.Errorf("model token limits are invalid")
	}
	if len(model.ThinkingLevels) == 0 || len(model.ThinkingLevels) > 7 || len(model.Inputs) == 0 || len(model.Inputs) > 2 {
		return fmt.Errorf("model thinking or input capabilities are invalid")
	}
	seenThinking := map[ThinkingLevel]bool{}
	for _, level := range model.ThinkingLevels {
		if err := level.Validate(); err != nil || seenThinking[level] {
			return fmt.Errorf("model thinking levels are invalid")
		}
		seenThinking[level] = true
	}
	seenInputs := map[ModelInputKind]bool{}
	for _, input := range model.Inputs {
		if input != ModelInputText && input != ModelInputImage || seenInputs[input] {
			return fmt.Errorf("model input capabilities are invalid")
		}
		seenInputs[input] = true
	}
	if !seenInputs[ModelInputText] {
		return fmt.Errorf("model must support text input")
	}
	return nil
}

// Validate checks a model catalog crossing a transport boundary.
func (catalog ModelCatalog) Validate() error {
	if len(catalog.Models) > 2048 {
		return fmt.Errorf("model catalog size is invalid")
	}
	seen := map[string]bool{}
	for index, model := range catalog.Models {
		if err := model.Validate(); err != nil {
			return fmt.Errorf("model %d: %w", index, err)
		}
		if seen[model.ID] {
			return fmt.Errorf("model %d duplicates %q", index, model.ID)
		}
		seen[model.ID] = true
	}
	return nil
}

// Validate checks a session configuration request crossing a transport boundary.
func (input ConfigureSessionInput) Validate() error {
	provider, model, ok := strings.Cut(input.Model, "/")
	if input.ExpectedRevision == 0 || input.ExpectedRevision > math.MaxInt64 || !ok || !validRendererText(provider, 128) || !validRendererText(model, 256) {
		return fmt.Errorf("expected revision and exact provider/model are required")
	}
	if input.ThinkingLevel != nil {
		return input.ThinkingLevel.Validate()
	}
	return nil
}

// Validate checks an applied session configuration result.
func (result ConfigureSessionResult) Validate() error {
	if err := result.Session.Validate(); err != nil {
		return err
	}
	if result.Session.ThinkingLevel == "" {
		return fmt.Errorf("configuration result thinking level is missing")
	}
	if !identifier.Valid(result.EventStreamID, "stream_") || len(result.Warnings) > 8 {
		return fmt.Errorf("configuration result stream or warning count is invalid")
	}
	if result.Compacted && result.CheckpointID == "" {
		return fmt.Errorf("compacted configuration result requires a checkpoint")
	}
	if result.CheckpointID != "" && !identifier.Valid(result.CheckpointID, "checkpoint_") {
		return fmt.Errorf("configuration checkpoint id is invalid")
	}
	for _, warning := range result.Warnings {
		if !validRendererText(warning, 4096) || strings.TrimSpace(warning) == "" {
			return fmt.Errorf("configuration warning is invalid")
		}
	}
	return nil
}

// ValidateApplied checks a configuration response against its initiating request.
func (result ConfigureSessionResult) ValidateApplied(input ConfigureSessionInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	if err := result.Validate(); err != nil {
		return err
	}
	if result.Session.Model != input.Model || result.Session.ConfigurationRevision < input.ExpectedRevision ||
		result.Session.ConfigurationRevision > input.ExpectedRevision+1 {
		return fmt.Errorf("configuration result does not match the requested model or revision")
	}
	if input.ThinkingLevel != nil && result.Session.ThinkingLevel != string(*input.ThinkingLevel) {
		return fmt.Errorf("configuration result does not match the requested thinking level")
	}
	return nil
}

// Validate checks an explicit compaction request.
func (input CompactSessionInput) Validate() error {
	if !validRendererText(input.OperationID, 256) {
		return fmt.Errorf("compaction operation id is invalid")
	}
	return nil
}

// Validate checks an explicit compaction result.
func (result CompactSessionResult) Validate() error {
	if err := (CompactSessionInput{OperationID: result.OperationID}).Validate(); err != nil {
		return err
	}
	if !identifier.Valid(result.EventStreamID, "stream_") {
		return fmt.Errorf("compaction event stream id is invalid")
	}
	if result.Compacted && result.CheckpointID == "" {
		return fmt.Errorf("compacted result requires a checkpoint")
	}
	if result.CheckpointID != "" && !identifier.Valid(result.CheckpointID, "checkpoint_") {
		return fmt.Errorf("compaction checkpoint id is invalid")
	}
	return nil
}

// ValidateApplied checks a compaction response against its initiating request.
func (result CompactSessionResult) ValidateApplied(input CompactSessionInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	if err := result.Validate(); err != nil {
		return err
	}
	if result.OperationID != input.OperationID {
		return fmt.Errorf("compaction result operation id does not match the request")
	}
	return nil
}

// Validate checks a session cwd change crossing a transport boundary.
func (input ChangeCWDInput) Validate() error {
	if !identifier.Valid(input.MutationID, "cwd_") {
		return fmt.Errorf("cwd mutation id is not canonical")
	}
	path := strings.TrimSpace(input.Path)
	if !validPathText(path) {
		return fmt.Errorf("working directory path must be non-empty renderer-safe UTF-8 and at most 4096 bytes")
	}
	return nil
}

// Validate checks a session rename crossing a transport boundary.
func (input RenameSessionInput) Validate() error {
	name := strings.TrimSpace(input.Name)
	if name == "" || !ValidSessionName(name) {
		return fmt.Errorf("session name must be non-empty renderer-safe UTF-8 and at most 256 bytes")
	}
	return nil
}

// Validate checks reload metadata crossing a transport boundary.
func (result ReloadSessionResult) Validate() error {
	if !identifier.Valid(result.SessionID, "session_") || !identifier.Valid(result.EventStreamID, "stream_") {
		return fmt.Errorf("reload result has invalid session or event stream identity")
	}
	if len(result.Sources) == 0 || len(result.Sources) > 512 || len(result.Diagnostics) > 256 || len(result.Warnings) > 8 {
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

func validPathText(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func validProtocolText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && strings.IndexByte(value, 0) < 0
}

func validRendererText(value string, maximum int) bool {
	if !validProtocolText(value, maximum) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

// ValidSessionName reports whether an optional session display name is safe to
// project into renderer-owned text surfaces.
func ValidSessionName(name string) bool {
	return name == "" || validRendererText(name, 256)
}

// Validate checks a session projection received across a transport boundary.
func (session SessionInfo) Validate() error {
	if session.ID == "" {
		return fmt.Errorf("session id is empty")
	}
	if !ValidSessionName(session.Name) {
		return fmt.Errorf("session name is not renderer-safe UTF-8")
	}
	if !filepath.IsAbs(session.CWD) || !validPathText(session.CWD) {
		return fmt.Errorf("session cwd %q is not a safe bounded absolute path", session.CWD)
	}
	provider, model, ok := strings.Cut(session.Model, "/")
	if !ok || !validRendererText(provider, 128) || !validRendererText(model, 256) {
		return fmt.Errorf("session model %q is not namespaced", session.Model)
	}
	if session.ConfigurationRevision == 0 || session.ConfigurationRevision > math.MaxInt64 {
		return fmt.Errorf("session configuration revision is invalid")
	}
	if session.ThinkingLevel != "" {
		if err := ThinkingLevel(session.ThinkingLevel).Validate(); err != nil {
			return err
		}
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

// Validate checks a prompt-command execution request crossing a transport boundary.
func (input PromptCommandInput) Validate() error {
	if !validPromptCommandName(input.Name) {
		return fmt.Errorf("prompt command name is invalid")
	}
	if len(input.Args) > 128<<10 || !utf8.ValidString(input.Args) || strings.IndexByte(input.Args, 0) >= 0 {
		return fmt.Errorf("prompt command arguments are invalid or oversized")
	}
	return nil
}

func validPromptCommandName(name string) bool {
	if !validProtocolText(name, 128) {
		return false
	}
	for _, character := range name {
		if unicode.IsSpace(character) || unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || character == '/' || character == '\\' {
			return false
		}
	}
	return true
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
