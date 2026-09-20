package session

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
)

var errConfigurationOutcomeUnknown = errors.New("session configuration write outcome is unknown")

// ModelCapability is renderer-neutral selectable model metadata owned by the
// session manager's provider registry.
type ModelCapability struct {
	ID              string
	Name            string
	Provider        string
	API             string
	ContextWindow   int
	MaxInputTokens  int
	MaxOutputTokens int
	ThinkingLevels  []string
	Inputs          []string
}

// ModelCapabilities returns a stable snapshot of the configured provider catalog.
func (m *Manager) ModelCapabilities(ctx context.Context) ([]ModelCapability, error) {
	if err := m.beginOperation(); err != nil {
		return nil, err
	}
	defer m.ops.Done()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	models := m.providers.Models()
	result := make([]ModelCapability, 0, len(models))
	for _, model := range models {
		selector := model.Provider + "/" + model.ID
		model = m.applyModelContextWindow(selector, model)
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = model.ID
		}
		inputs := append([]string(nil), model.Input...)
		if len(inputs) == 0 {
			inputs = []string{"text"}
		}
		result = append(result, ModelCapability{
			ID: selector, Name: name, Provider: model.Provider, API: string(model.API),
			ContextWindow: model.ContextWindow, MaxInputTokens: model.MaxInputTokens, MaxOutputTokens: model.MaxOutputTokens,
			ThinkingLevels: append([]string(nil), supportedThinkingLevels(model)...), Inputs: inputs,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// ConfigureSessionInput requests one exact model/thinking transition from the
// configuration revision observed by the caller. Nil ThinkingLevel preserves or
// safely clamps the saved level for the target model.
type ConfigureSessionInput struct {
	ExpectedRevision uint64
	Model            string
	ThinkingLevel    *string
}

// ConfigureSessionResult is the authoritative configuration and runtime stream
// applied by ConfigureSession.
type ConfigureSessionResult struct {
	Session       SessionRecord
	EventStreamID string
	Compacted     bool
	CheckpointID  string
	Warnings      []string
}

// CompactSessionResult describes one explicit droids-owned context compaction.
type CompactSessionResult struct {
	OperationID   string
	Compacted     bool
	CheckpointID  string
	EventStreamID string
}

// ConfigureSession applies an exact model and valid thinking level. A
// thinking-only change reconfigures the live droid; model changes require a
// quiescent session and compact droid context first when required.
func (m *Manager) ConfigureSession(ctx context.Context, sessionID string, input ConfigureSessionInput) (ConfigureSessionResult, error) {
	if err := m.beginOperation(); err != nil {
		return ConfigureSessionResult{}, err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(sessionID) == "" || input.ExpectedRevision == 0 || input.ExpectedRevision > math.MaxInt64 {
		return ConfigureSessionResult{}, fmt.Errorf("%w: session id and expected configuration revision are required", ErrInvalidInput)
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return ConfigureSessionResult{}, err
	}
	targetModel, err := m.resolveExactModel(input.Model)
	if err != nil {
		return ConfigureSessionResult{}, err
	}
	initialRecord, err := m.sessionRecord(ctx, sessionID)
	if err != nil {
		return ConfigureSessionResult{}, err
	}
	if initialRecord.ModelProvider == targetModel.Provider && initialRecord.ModelID == targetModel.ID {
		return m.configureLiveThinking(ctx, sessionID, loaded, targetModel, input)
	}
	if !loaded.transitionMu.TryLock() {
		return ConfigureSessionResult{}, ErrConfigureBusy
	}
	defer loaded.transitionMu.Unlock()
	if !loaded.admissionMu.TryLock() {
		return ConfigureSessionResult{}, ErrConfigureBusy
	}
	defer loaded.admissionMu.Unlock()
	if !loaded.mu.TryLock() {
		return ConfigureSessionResult{}, ErrConfigureBusy
	}
	defer loaded.mu.Unlock()
	if err := m.requireQuiescentTransition(ctx, sessionID, loaded, ErrConfigureBusy); err != nil {
		return ConfigureSessionResult{}, err
	}
	workspaceCWD, workspaceGeneration := loaded.workspace.snapshot()
	record, err := m.sessionRecord(ctx, sessionID)
	if err != nil {
		return ConfigureSessionResult{}, err
	}
	if record.CWD != workspaceCWD {
		return ConfigureSessionResult{}, fmt.Errorf("%w: session cwd changed while configuration was starting", ErrConfigureBusy)
	}
	if record.ConfigurationRevision != input.ExpectedRevision {
		return ConfigureSessionResult{}, &ConfigurationConflictError{Expected: input.ExpectedRevision, Actual: record.ConfigurationRevision}
	}
	targetModel, err = m.resolveExactModel(input.Model)
	if err != nil {
		return ConfigureSessionResult{}, err
	}
	effectiveThinking, warnings, err := resolveConfigurationThinking(targetModel, record.ThinkingLevel, input.ThinkingLevel)
	if err != nil {
		return ConfigureSessionResult{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	targetRecord := record
	targetRecord.ModelProvider = targetModel.Provider
	targetRecord.ModelID = targetModel.ID
	targetRecord.ThinkingLevel = effectiveThinking
	changed := targetRecord.ModelProvider != record.ModelProvider || targetRecord.ModelID != record.ModelID || targetRecord.ThinkingLevel != record.ThinkingLevel
	if !changed {
		applied, err := m.persistSessionConfigurationReconciled(ctx, record, ConfigurationUpdate{
			SessionID: sessionID, ExpectedRevision: input.ExpectedRevision,
			ModelProvider: targetRecord.ModelProvider, ModelID: targetRecord.ModelID, ThinkingLevel: targetRecord.ThinkingLevel,
		})
		if err != nil {
			if errors.Is(err, errConfigurationOutcomeUnknown) {
				m.quarantineRuntime(sessionID, loaded, nil)
			}
			return ConfigureSessionResult{}, err
		}
		loaded.configurationWarnings = nil
		return ConfigureSessionResult{Session: applied, EventStreamID: loaded.events.streamID}, nil
	}
	targetRecord.ConfigurationRevision = record.ConfigurationRevision + 1
	targetSelector := targetModel.Provider + "/" + targetModel.ID
	target := droids.ContextTarget{Model: targetModel, Reasoning: effectiveThinking}
	assessment, err := loaded.droid.AssessContext(ctx, target)
	if err != nil {
		return ConfigureSessionResult{}, fmt.Errorf("assess session %q context for %q: %w", sessionID, targetSelector, err)
	}
	var compaction droids.CompactContextResult
	if assessment.RequiresCompaction {
		operationID, err := identifier.New("compact_")
		if err != nil {
			return ConfigureSessionResult{}, err
		}
		compaction, err = loaded.droid.CompactContext(ctx, droids.CompactContextOptions{OperationID: operationID, Target: target})
		if err != nil {
			return ConfigureSessionResult{}, fmt.Errorf("adapt session %q context for %q: %w", sessionID, targetSelector, err)
		}
	}
	replacement, err := m.bundleBuilder.Build(ctx, targetRecord, loaded.workspace.CWD)
	if err != nil {
		return ConfigureSessionResult{}, fmt.Errorf("build replacement runtime bundle for session %q: %w", sessionID, err)
	}
	replacement.Prompt.Prompt += interactionPromptGuidance
	replacement.Tools = append(replacement.Tools, interactionTools(sessionID, loaded.interactions)...)
	replacement.Tools = append(replacement.Tools, m.changeCWDTool(sessionID, loaded.workspace))
	nextEvents, err := newEventLog()
	if err != nil {
		return ConfigureSessionResult{}, fmt.Errorf("prepare replacement event stream for session %q: %w", sessionID, err)
	}
	if m.isClosed() {
		return ConfigureSessionResult{}, ErrClosed
	}
	if m.sessionDeleting(sessionID) {
		return ConfigureSessionResult{}, ErrDeleteBusy
	}

	transitionContext, cancelTransition := context.WithTimeout(context.WithoutCancel(ctx), runtimeTransitionTimeout)
	defer cancelTransition()
	replacementDroid, snapshot, effectiveModel, err := m.openDroid(transitionContext, targetRecord, loaded.store, replacement)
	if err != nil {
		return ConfigureSessionResult{}, fmt.Errorf("open replacement droid: %w", err)
	}
	currentCWD, currentGeneration := loaded.workspace.snapshot()
	if currentGeneration != workspaceGeneration || currentCWD != workspaceCWD {
		_ = replacementDroid.Close()
		return ConfigureSessionResult{}, fmt.Errorf("%w: session cwd changed during configuration", ErrConfigureBusy)
	}
	applied, err := m.persistSessionConfigurationReconciled(transitionContext, record, ConfigurationUpdate{
		SessionID: sessionID, ExpectedRevision: input.ExpectedRevision,
		ModelProvider: targetRecord.ModelProvider, ModelID: targetRecord.ModelID, ThinkingLevel: targetRecord.ThinkingLevel,
	})
	if err != nil {
		if errors.Is(err, errConfigurationOutcomeUnknown) {
			m.quarantineRuntime(sessionID, loaded, replacementDroid)
		} else {
			_ = replacementDroid.Close()
		}
		return ConfigureSessionResult{}, err
	}
	closeErr := loaded.droid.Shutdown(transitionContext)
	loaded.droid = replacementDroid
	loaded.model = effectiveModel
	loaded.bundle = cloneRuntimeBundle(replacement)
	loaded.eventCursor = snapshot.LastEvent
	loaded.events.replace(nextEvents)
	loaded.configurationWarnings = append([]string(nil), warnings...)
	result := ConfigureSessionResult{
		Session: applied, EventStreamID: nextEvents.streamID,
		Compacted: compaction.Compacted, CheckpointID: string(compaction.CheckpointID),
		Warnings: append([]string(nil), warnings...),
	}
	if closeErr != nil {
		result.Warnings = append(result.Warnings, boundedReloadWarning("Previous runtime shutdown reported: "+closeErr.Error()))
	}
	return result, nil
}

func (m *Manager) configureLiveThinking(ctx context.Context, sessionID string, loaded *runtime, targetModel droids.Model, input ConfigureSessionInput) (ConfigureSessionResult, error) {
	if !loaded.transitionMu.TryLock() {
		return ConfigureSessionResult{}, ErrConfigureBusy
	}
	defer loaded.transitionMu.Unlock()
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	if m.sessionDeleting(sessionID) {
		return ConfigureSessionResult{}, ErrDeleteBusy
	}
	record, err := m.sessionRecordAtWorkspace(ctx, sessionID, loaded.workspace)
	if err != nil {
		return ConfigureSessionResult{}, err
	}
	if record.ConfigurationRevision != input.ExpectedRevision {
		return ConfigureSessionResult{}, &ConfigurationConflictError{Expected: input.ExpectedRevision, Actual: record.ConfigurationRevision}
	}
	if record.ModelProvider != targetModel.Provider || record.ModelID != targetModel.ID {
		return ConfigureSessionResult{}, &ConfigurationConflictError{Expected: input.ExpectedRevision, Actual: record.ConfigurationRevision}
	}
	effectiveThinking, warnings, err := resolveConfigurationThinking(targetModel, record.ThinkingLevel, input.ThinkingLevel)
	if err != nil {
		return ConfigureSessionResult{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	applied, err := m.persistSessionConfigurationReconciled(ctx, record, ConfigurationUpdate{
		SessionID: record.ID, ExpectedRevision: input.ExpectedRevision,
		ModelProvider: record.ModelProvider, ModelID: record.ModelID, ThinkingLevel: effectiveThinking,
	})
	if err != nil {
		if errors.Is(err, errConfigurationOutcomeUnknown) {
			m.quarantineRuntime(record.ID, loaded, nil)
		}
		return ConfigureSessionResult{}, err
	}
	if err := loaded.droid.Reconfigure(droids.RequestConfiguration{
		SystemPrompt: loaded.bundle.Prompt.Prompt, Reasoning: effectiveThinking, ContextWindow: loaded.model.ContextWindow, Tools: loaded.bundle.Tools,
	}); err != nil {
		m.quarantineRuntime(record.ID, loaded, nil)
		return ConfigureSessionResult{}, fmt.Errorf("apply session %q thinking: %w", record.ID, err)
	}
	loaded.configurationWarnings = append([]string(nil), warnings...)
	return ConfigureSessionResult{
		Session: applied, EventStreamID: loaded.events.streamID, Warnings: append([]string(nil), warnings...),
	}, nil
}

// CompactSession forces one idempotent explicit compaction against the current
// persisted model and thinking configuration, independent of automatic pressure
// thresholds.
func (m *Manager) CompactSession(ctx context.Context, sessionID, operationID string) (CompactSessionResult, error) {
	if err := m.beginOperation(); err != nil {
		return CompactSessionResult{}, err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(sessionID) == "" || !validConfigurationOperationID(operationID) {
		return CompactSessionResult{}, fmt.Errorf("%w: session id and valid compaction operation id are required", ErrInvalidInput)
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	if !loaded.transitionMu.TryLock() {
		return CompactSessionResult{}, ErrConfigureBusy
	}
	defer loaded.transitionMu.Unlock()
	if !loaded.admissionMu.TryLock() {
		return CompactSessionResult{}, ErrConfigureBusy
	}
	defer loaded.admissionMu.Unlock()
	if !loaded.mu.TryLock() {
		return CompactSessionResult{}, ErrConfigureBusy
	}
	defer loaded.mu.Unlock()
	if err := m.requireQuiescentTransition(ctx, sessionID, loaded, ErrConfigureBusy); err != nil {
		return CompactSessionResult{}, err
	}
	record, err := m.sessionRecordAtWorkspace(ctx, sessionID, loaded.workspace)
	if err != nil {
		return CompactSessionResult{}, err
	}
	result, err := loaded.droid.CompactContext(ctx, droids.CompactContextOptions{
		OperationID: operationID,
		Target:      droids.ContextTarget{Model: loaded.model, Reasoning: record.ThinkingLevel},
		Force:       true,
	})
	if err != nil {
		return CompactSessionResult{}, err
	}
	settlementContext, cancelSettlement := context.WithTimeout(context.WithoutCancel(ctx), runtimeTransitionTimeout)
	defer cancelSettlement()
	after, err := loaded.droid.Snapshot(settlementContext, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		return CompactSessionResult{}, err
	}
	needsPublication := after.LastEvent != loaded.eventCursor
	var nextEvents *eventLog
	if result.Compacted && needsPublication {
		nextEvents, err = newEventLog()
		if err != nil {
			return CompactSessionResult{}, err
		}
	}
	if needsPublication {
		if err := m.touchSessionActivity(settlementContext, sessionID, time.Now().UTC()); err != nil {
			return CompactSessionResult{}, err
		}
	}
	loaded.eventCursor = after.LastEvent
	if nextEvents != nil {
		loaded.events.replace(nextEvents)
	}
	return CompactSessionResult{
		OperationID: result.OperationID, Compacted: result.Compacted,
		CheckpointID: string(result.CheckpointID), EventStreamID: loaded.events.streamID,
	}, nil
}

func validConfigurationOperationID(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func (m *Manager) requireQuiescentTransition(ctx context.Context, sessionID string, loaded *runtime, busy error) error {
	if m.sessionDeleting(sessionID) {
		return ErrDeleteBusy
	}
	if loaded.activeRun != "" {
		return busy
	}
	m.bashMu.Lock()
	bashActive := m.bashActive[sessionID] != nil
	m.bashMu.Unlock()
	if bashActive {
		return busy
	}
	if _, err := loaded.droid.WaitQuiescent(ctx); err != nil {
		if errors.Is(err, droids.ErrClosed) {
			return err
		}
		return fmt.Errorf("wait for session %q to become quiescent: %w", sessionID, err)
	}
	return nil
}

func (m *Manager) resolveExactModel(selector string) (droids.Model, error) {
	model, err := m.providers.Resolve(selector)
	if err != nil {
		return droids.Model{}, fmt.Errorf("%w: unknown model %q", ErrInvalidInput, selector)
	}
	exact := model.Provider + "/" + model.ID
	if selector != exact || model.Provider == "" || model.ID == "" {
		return droids.Model{}, fmt.Errorf("%w: model must use exact provider/model id, got %q", ErrInvalidInput, selector)
	}
	return model, nil
}

func resolveConfigurationThinking(model droids.Model, saved string, requested *string) (string, []string, error) {
	if requested != nil {
		value := canonicalThinkingLevel(strings.TrimSpace(*requested))
		if value == "" {
			return "", nil, fmt.Errorf("thinking level is required when specified")
		}
		if err := validateThinkingLevel(model, value); err != nil {
			return "", nil, err
		}
		return value, nil, nil
	}
	effective, adjusted, err := resolveSavedThinkingLevel(model, saved)
	if err != nil {
		return "", nil, err
	}
	if !adjusted {
		return effective, nil, nil
	}
	return effective, []string{boundedReloadWarning(fmt.Sprintf(
		"Thinking level %q is unavailable for %s/%s; using %q.", saved, model.Provider, model.ID, effective,
	))}, nil
}

// ResolveCompatibleThinkingLevel returns a valid effective thinking level for a model and saved configuration.
func ResolveCompatibleThinkingLevel(model droids.Model, saved string) (string, error) {
	effective, _, err := resolveSavedThinkingLevel(model, saved)
	return effective, err
}

func resolveSavedThinkingLevel(model droids.Model, saved string) (string, bool, error) {
	supported := supportedThinkingLevels(model)
	if len(supported) == 0 {
		return "", false, fmt.Errorf("model %q has no supported thinking configuration", model.ID)
	}
	original := strings.TrimSpace(saved)
	canonical := canonicalThinkingLevel(original)
	if canonical == "" {
		for _, level := range supported {
			if level == "medium" {
				return level, false, nil
			}
		}
		return supported[0], false, nil
	}
	for _, level := range supported {
		if level == canonical {
			return level, original != canonical, nil
		}
	}
	rank, known := thinkingLevelRank(canonical)
	if !known {
		return supported[0], true, nil
	}
	candidate := ""
	for _, level := range supported {
		levelRank, _ := thinkingLevelRank(level)
		if levelRank <= rank {
			candidate = level
		}
	}
	if candidate == "" {
		candidate = supported[0]
	}
	return candidate, true, nil
}

func supportedThinkingLevels(model droids.Model) []string {
	levels := make([]string, 0, len(canonicalThinkingLevels))
	for _, level := range canonicalThinkingLevels {
		if validateThinkingLevel(model, level) == nil {
			levels = append(levels, level)
		}
	}
	return levels
}

var canonicalThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

func canonicalThinkingLevel(level string) string {
	if level == "none" {
		return "off"
	}
	return level
}

func thinkingLevelRank(level string) (int, bool) {
	for index, candidate := range canonicalThinkingLevels {
		if candidate == level {
			return index, true
		}
	}
	return 0, false
}

func (m *Manager) quarantineRuntime(sessionID string, loaded *runtime, replacement *droids.Droid) {
	m.mu.Lock()
	if m.runtimes[sessionID] == loaded {
		delete(m.runtimes, sessionID)
	}
	m.mu.Unlock()
	loaded.events.invalidate()
	if replacement != nil {
		_ = replacement.Close()
	}
	closeContext, cancel := context.WithTimeout(context.Background(), runtimeTransitionTimeout)
	defer cancel()
	_ = loaded.droid.Shutdown(closeContext)
	_ = loaded.closeStore()
}

func (m *Manager) persistSessionConfigurationReconciled(ctx context.Context, previous SessionRecord, update ConfigurationUpdate) (SessionRecord, error) {
	applied, err := m.persistSessionConfiguration(ctx, previous, update)
	if err == nil {
		return applied, nil
	}
	reconcileContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeTransitionTimeout)
	defer cancel()
	authoritative, loadErr := m.sessionRecord(reconcileContext, update.SessionID)
	if loadErr != nil {
		return SessionRecord{}, errors.Join(errConfigurationOutcomeUnknown, err, loadErr)
	}
	expectedRevision := update.ExpectedRevision
	if previous.ModelProvider != update.ModelProvider || previous.ModelID != update.ModelID || previous.ThinkingLevel != update.ThinkingLevel {
		expectedRevision++
	}
	if authoritative.ConfigurationRevision == expectedRevision && authoritative.ModelProvider == update.ModelProvider &&
		authoritative.ModelID == update.ModelID && authoritative.ThinkingLevel == update.ThinkingLevel {
		return authoritative, nil
	}
	return SessionRecord{}, err
}

func (m *Manager) normalizeSessionThinking(ctx context.Context, record SessionRecord) (SessionRecord, []string, error) {
	model, err := m.resolveExactModel(record.ModelProvider + "/" + record.ModelID)
	if err != nil {
		return SessionRecord{}, nil, fmt.Errorf("restore session %q model: %w", record.ID, err)
	}
	effective, warnings, err := resolveConfigurationThinking(model, record.ThinkingLevel, nil)
	if err != nil {
		return SessionRecord{}, nil, fmt.Errorf("restore session %q thinking: %w", record.ID, err)
	}
	if effective == record.ThinkingLevel {
		return record, nil, nil
	}
	updated, err := m.persistSessionConfigurationReconciled(ctx, record, ConfigurationUpdate{
		SessionID: record.ID, ExpectedRevision: record.ConfigurationRevision,
		ModelProvider: record.ModelProvider, ModelID: record.ModelID, ThinkingLevel: effective,
	})
	if err != nil {
		return SessionRecord{}, nil, fmt.Errorf("persist session %q restored thinking: %w", record.ID, err)
	}
	return updated, warnings, nil
}

func (m *Manager) persistSessionConfiguration(ctx context.Context, previous SessionRecord, update ConfigurationUpdate) (SessionRecord, error) {
	if previous.Persistent {
		return m.store.UpdateSessionConfiguration(ctx, update)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return SessionRecord{}, ErrClosed
	}
	if m.deleting[previous.ID] {
		return SessionRecord{}, ErrDeleteBusy
	}
	current, ok := m.temporary[previous.ID]
	if !ok {
		return SessionRecord{}, fmt.Errorf("session %q: %w", previous.ID, ErrNotFound)
	}
	if current.ConfigurationRevision != update.ExpectedRevision {
		return SessionRecord{}, &ConfigurationConflictError{Expected: update.ExpectedRevision, Actual: current.ConfigurationRevision}
	}
	changed := current.ModelProvider != update.ModelProvider || current.ModelID != update.ModelID || current.ThinkingLevel != update.ThinkingLevel
	current.ModelProvider, current.ModelID, current.ThinkingLevel = update.ModelProvider, update.ModelID, update.ThinkingLevel
	if changed {
		if current.ConfigurationRevision >= math.MaxInt64 {
			return SessionRecord{}, fmt.Errorf("session configuration revision overflow")
		}
		current.ConfigurationRevision++
	}
	now := time.Now().UTC()
	if now.After(current.UpdatedAt) {
		current.UpdatedAt = now
	}
	m.temporary[previous.ID] = current
	return current, nil
}
