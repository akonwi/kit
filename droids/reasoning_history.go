package droids

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const reasoningHistoryRecordKind = "reasoning_history"

// ReasoningHistory projects one durable context's original request setting and
// ordered effort changes. Empty Baseline means the request field was omitted.
// This is not an Anthropic managed-effort message or a provider response setting.
type ReasoningHistory struct {
	Baseline  string
	Effective string
	Updates   []ReasoningUpdate
}

// ReasoningUpdate takes effect immediately before a canonical user message.
// BeforeMessage indexes Request.Messages, not the expanded provider input array.
type ReasoningUpdate struct {
	BeforeMessage int
	Effort        string
}

type durableReasoningUpdate struct {
	BeforeMessage MessageID `json:"before_message"`
	Effort        string    `json:"effort"`
}

type durableReasoningHistory struct {
	Model             string                   `json:"model"`
	Baseline          string                   `json:"baseline"`
	Effective         string                   `json:"effective"`
	Requested         string                   `json:"requested"`
	Started           bool                     `json:"started"`
	DispatchedThrough MessageID                `json:"dispatched_through,omitempty"`
	Updates           []durableReasoningUpdate `json:"updates,omitempty"`
}

// Restrict this wire capability to reviewed standard public API models. Codex,
// gateways, pro and provider-managed multi-agent modes do not inherit support.
func supportsOpenAIReasoningHistory(model Model) bool {
	if !model.SupportsReasoningConfigurationUpdates || model.API != ModelAPIOpenAIResponses {
		return false
	}
	return standardOpenAIReasoningHistoryModel(model.ID)
}

func standardOpenAIReasoningHistoryModel(id string) bool {
	switch id {
	case "gpt-6-astra", "gpt-6-sol", "gpt-6-luna":
		return true
	}
	return false
}

func canonicalEffort(effort string) string {
	if effort == "off" {
		return "none"
	}
	return effort
}

func freshReasoningHistory(model Model, requested string) *durableReasoningHistory {
	if !supportsOpenAIReasoningHistory(model) {
		return nil
	}
	return &durableReasoningHistory{Model: model.Provider + "/" + model.ID, Requested: canonicalEffort(requested)}
}

func cloneReasoningHistory(source *durableReasoningHistory) *durableReasoningHistory {
	if source == nil {
		return nil
	}
	next := *source
	next.Updates = append([]durableReasoningUpdate(nil), source.Updates...)
	return &next
}

// captureReasoningHistoryLocked freezes exactly the provider request about to
// be dispatched. Reconfigure never edits an in-flight request or dispatched
// prefix. Multiple selections before an eligible user boundary coalesce here.
func (rt *sdkRuntime) captureReasoningHistoryLocked(ctx context.Context, requested string) (*ReasoningHistory, error) {
	old := rt.state.ReasoningHistory
	model := rt.droid.model
	if !supportsOpenAIReasoningHistory(model) {
		if old == nil {
			return nil, nil
		}
		rt.state.ReasoningHistory = nil
		if err := rt.commitLocked(ctx, nil, nil); err != nil {
			rt.state.ReasoningHistory = old
			return nil, err
		}
		return nil, nil
	}
	key := model.Provider + "/" + model.ID
	next := cloneReasoningHistory(old)
	if next == nil || next.Model != key {
		next = &durableReasoningHistory{Model: key}
	}
	next.Requested = canonicalEffort(requested)
	if !next.Started {
		next.Baseline, next.Effective = next.Requested, next.Requested
		next.Started = true
	} else if next.Requested != next.Effective {
		start := 0
		if next.DispatchedThrough != "" {
			start = -1
			for i, envelope := range rt.state.Context {
				if envelope.ID == next.DispatchedThrough {
					start = i + 1
					break
				}
			}
			if start < 0 {
				return nil, fmt.Errorf("droids: reasoning history cursor is missing from active context")
			}
		}
		for _, envelope := range rt.state.Context[start:] {
			if envelope.Message.Role != RoleUser || len(envelope.Message.Content) == 0 {
				continue
			}
			if next.Requested == "" {
				return nil, fmt.Errorf("droids: changing managed reasoning to an unspecified default requires a fresh context")
			}
			next.Updates = append(next.Updates, durableReasoningUpdate{BeforeMessage: envelope.ID, Effort: next.Requested})
			next.Effective = next.Requested
			break
		}
	}
	if len(rt.state.Context) > 0 {
		next.DispatchedThrough = rt.state.Context[len(rt.state.Context)-1].ID
	}
	projection, err := projectReasoningHistory(next, rt.state.Context)
	if err != nil {
		return nil, err
	}
	// Archive only newly dispatched configuration, not a growing copy of the
	// entire prefix on every tool continuation. The runtime carries the cursor.
	var record any
	if old == nil || old.Model != next.Model || !old.Started {
		record = struct {
			Model    string `json:"model"`
			Baseline string `json:"baseline"`
		}{next.Model, next.Baseline}
	} else if len(next.Updates) > len(old.Updates) {
		record = next.Updates[len(next.Updates)-1]
	}
	var mutations []EncodedMutation
	if record != nil {
		payload, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		id, err := newID("reasoning_")
		if err != nil {
			return nil, err
		}
		mutations = append(mutations, EncodedMutation{Operation: MutationAssertAbsent, RecordKind: reasoningHistoryRecordKind, RecordID: id, Scope: RecordHistory, Version: recordVersion, Payload: payload})
	}
	rt.state.ReasoningHistory = next
	if err := rt.commitLocked(ctx, mutations, nil); err != nil {
		rt.state.ReasoningHistory = old
		return nil, err
	}
	return projection, nil
}

func projectReasoningHistory(history *durableReasoningHistory, messages []wireMessageEnvelope) (*ReasoningHistory, error) {
	result := &ReasoningHistory{Baseline: history.Baseline, Effective: history.Effective}
	last := -1
	for _, update := range history.Updates {
		index := -1
		for i, envelope := range messages {
			if envelope.ID == update.BeforeMessage {
				index = i
				break
			}
		}
		if index <= last || index < 0 || messages[index].Message.Role != RoleUser {
			return nil, fmt.Errorf("droids: invalid reasoning update position")
		}
		result.Updates = append(result.Updates, ReasoningUpdate{BeforeMessage: index, Effort: update.Effort})
		last = index
	}
	return result, nil
}

// reasoningHistoryForContext projects the dispatched configuration updates of a
// durable epoch onto one candidate context for the model that would replay it.
// Pending, undispatched selections are excluded because assessment must describe
// the request Kit would send now. It returns nil when the model does not own the
// epoch or the candidate context no longer carries the dispatched anchors, which
// is the fresh baseline a compacted or replaced context receives.
func reasoningHistoryForContext(history *durableReasoningHistory, model Model, messages []wireMessageEnvelope) *ReasoningHistory {
	if history == nil || !history.Started || !supportsOpenAIReasoningHistory(model) {
		return nil
	}
	if history.Model != model.Provider+"/"+model.ID {
		return nil
	}
	projection, err := projectReasoningHistory(history, messages)
	if err != nil {
		return nil
	}
	return projection
}

// effectiveRequestReasoning returns the effort a replayed request carries: the
// dispatched effective effort of the projected epoch, or the configured effort
// when no configuration updates apply.
func effectiveRequestReasoning(history *ReasoningHistory, configured string) string {
	if history == nil {
		return configured
	}
	return history.Effective
}

// ReasoningEpochMode controls whether authoritative configuration starts a
// fresh context baseline or preserves the existing dispatched prefix.
type ReasoningEpochMode int

const (
	// PreserveReasoningEpoch reconciles requested effort, retaining the baseline.
	PreserveReasoningEpoch ReasoningEpochMode = iota
	// ResetReasoningEpoch establishes a fresh baseline without deleting history.
	ResetReasoningEpoch
)

// ReconcileReasoning explicitly applies an owner's authoritative setting after
// opening a conversation. Spawn restores durable intent instead of overwriting
// it with possibly stale bootstrap configuration. Model changes reset epochs.
func (d *Droid) ReconcileReasoning(ctx context.Context, requested string, mode ReasoningEpochMode) error {
	if d == nil || d.sdk == nil {
		return fmt.Errorf("droids: reasoning reconciliation requires a spawned droid")
	}
	if mode != PreserveReasoningEpoch && mode != ResetReasoningEpoch {
		return fmt.Errorf("droids: unknown reasoning epoch mode")
	}
	maxTokens, err := resolveRequestMaxTokens(d.model, 0, requested)
	if err != nil {
		return err
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed || rt.shutdownStarted {
		return ErrClosed
	}
	if mode == ResetReasoningEpoch && (statusRunsWork(rt.state.Status) || rt.contextFlight != nil) {
		return ErrBusy
	}
	if err := rt.reconcileReasoningLocked(ctx, requested, mode == ResetReasoningEpoch); err != nil {
		return err
	}
	base := *rt.baseRequestConfig
	base.reasoning, base.maxTokens = requested, maxTokens
	combined := *rt.currentRequestConfiguration()
	combined.reasoning, combined.maxTokens = requested, maxTokens
	rt.baseRequestConfig = &base
	rt.requestConfig.Store(&combined)
	return nil
}

func (rt *sdkRuntime) reconcileReasoningLocked(ctx context.Context, requested string, reset bool) error {
	old := rt.state.ReasoningHistory
	var next *durableReasoningHistory
	key := rt.droid.model.Provider + "/" + rt.droid.model.ID
	if supportsOpenAIReasoningHistory(rt.droid.model) {
		next = cloneReasoningHistory(old)
		if next == nil || next.Model != key || reset {
			next = &durableReasoningHistory{Model: key}
		}
		requested = canonicalEffort(requested)
		if next.Started && requested == "" && next.Effective != "" {
			return fmt.Errorf("droids: changing managed reasoning to an unspecified default requires a fresh context")
		}
		next.Requested = requested
		if !reset && old != nil && old.Model == key && old.Requested == requested {
			return nil
		}
	} else if old == nil {
		return nil
	}
	rt.state.ReasoningHistory = next
	if err := rt.commitLocked(ctx, nil, nil); err != nil {
		rt.state.ReasoningHistory = old
		return err
	}
	return nil
}

func validateOpenAIReasoningHistory(model Model, request Request) error {
	history := request.ReasoningHistory
	if !supportsOpenAIReasoningHistory(model) {
		return fmt.Errorf("droids: model %q does not support ordered reasoning history", model.ID)
	}
	if err := validateReasoning(model, history.Baseline); err != nil {
		return err
	}
	effective := canonicalEffort(history.Baseline)
	last := -1
	for _, update := range history.Updates {
		if update.BeforeMessage <= last || update.BeforeMessage < 0 || update.BeforeMessage >= len(request.Messages) {
			return fmt.Errorf("droids: invalid reasoning update position")
		}
		message, ok := request.Messages[update.BeforeMessage].(UserMessage)
		if !ok || len(message.Content) == 0 {
			return fmt.Errorf("droids: reasoning update must precede a user message")
		}
		if update.Effort == "" || update.Effort != canonicalEffort(update.Effort) {
			return fmt.Errorf("droids: reasoning update requires canonical explicit effort")
		}
		if err := validateReasoning(model, update.Effort); err != nil {
			return err
		}
		effective = update.Effort
		last = update.BeforeMessage
	}
	if effective != canonicalEffort(history.Effective) || effective != canonicalEffort(request.Reasoning) {
		return fmt.Errorf("droids: inconsistent effective reasoning history")
	}
	return nil
}

func validateDurableReasoningHistory(history *durableReasoningHistory, messages []wireMessageEnvelope) error {
	if history == nil {
		return nil
	}
	provider, model, ok := strings.Cut(history.Model, "/")
	if !ok || provider == "" || len(history.Model) > 1024 || !standardOpenAIReasoningHistoryModel(model) {
		return fmt.Errorf("invalid model identity")
	}
	for _, effort := range []string{history.Baseline, history.Effective, history.Requested} {
		switch effort {
		case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
		default:
			return fmt.Errorf("invalid effort %q", effort)
		}
	}
	if !history.Started {
		if history.Baseline != "" || history.Effective != "" || len(history.Updates) > 0 || history.DispatchedThrough != "" {
			return fmt.Errorf("unstarted epoch has dispatched configuration")
		}
		return nil
	}
	projection, err := projectReasoningHistory(history, messages)
	if err != nil {
		return err
	}
	effective := history.Baseline
	for _, update := range projection.Updates {
		switch update.Effort {
		case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		default:
			return fmt.Errorf("invalid update effort")
		}
		if update.Effort == effective {
			return fmt.Errorf("redundant effort update")
		}
		effective = update.Effort
	}
	if history.Requested == "" && history.Effective != "" {
		return fmt.Errorf("explicit effective effort requires explicit requested effort")
	}
	if effective != history.Effective {
		return fmt.Errorf("inconsistent effective effort")
	}
	cursor := -1
	for i, message := range messages {
		if message.ID == history.DispatchedThrough {
			cursor = i
			break
		}
	}
	if history.DispatchedThrough == "" || cursor < 0 {
		return fmt.Errorf("missing dispatch cursor")
	}
	if len(projection.Updates) > 0 && projection.Updates[len(projection.Updates)-1].BeforeMessage > cursor {
		return fmt.Errorf("update follows dispatch cursor")
	}
	return nil
}
