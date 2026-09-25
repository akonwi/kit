package droids

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// SessionUsage is the cumulative canonical provider usage for one conversation.
// It includes ordinary assistant responses and droids-owned compaction requests.
type SessionUsage = Usage

func validateUsage(usage Usage) error {
	if err := validateUsageTokens(usage); err != nil {
		return err
	}
	for name, value := range map[string]float64{
		"input_cost": usage.Cost.Input, "output_cost": usage.Cost.Output,
		"cache_read_cost": usage.Cost.CacheRead, "cache_write_cost": usage.Cost.CacheWrite,
		"total_cost": usage.Cost.Total,
	} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("usage %s is invalid", name)
		}
	}
	return nil
}

func validateUsageTokens(usage Usage) error {
	for name, value := range map[string]int{
		"input": usage.Input, "output": usage.Output,
		"cache_read": usage.CacheRead, "cache_write": usage.CacheWrite,
		"reasoning": usage.Reasoning, "total": usage.TotalTokens,
	} {
		if value < 0 {
			return fmt.Errorf("usage %s is negative", name)
		}
	}
	return nil
}

func mergeUsage(total, delta Usage) (Usage, error) {
	if err := validateUsage(total); err != nil {
		return Usage{}, err
	}
	if err := validateUsage(delta); err != nil {
		return Usage{}, err
	}
	var result Usage
	var err error
	if result.Input, err = addUsageInt(total.Input, delta.Input); err != nil {
		return Usage{}, fmt.Errorf("usage input: %w", err)
	}
	if result.Output, err = addUsageInt(total.Output, delta.Output); err != nil {
		return Usage{}, fmt.Errorf("usage output: %w", err)
	}
	if result.CacheRead, err = addUsageInt(total.CacheRead, delta.CacheRead); err != nil {
		return Usage{}, fmt.Errorf("usage cache read: %w", err)
	}
	if result.CacheWrite, err = addUsageInt(total.CacheWrite, delta.CacheWrite); err != nil {
		return Usage{}, fmt.Errorf("usage cache write: %w", err)
	}
	if result.Reasoning, err = addUsageInt(total.Reasoning, delta.Reasoning); err != nil {
		return Usage{}, fmt.Errorf("usage reasoning: %w", err)
	}
	if result.TotalTokens, err = addUsageInt(total.TotalTokens, delta.TotalTokens); err != nil {
		return Usage{}, fmt.Errorf("usage total: %w", err)
	}
	if result.Cost.Input, err = addUsageCost(total.Cost.Input, delta.Cost.Input); err != nil {
		return Usage{}, fmt.Errorf("usage input cost: %w", err)
	}
	if result.Cost.Output, err = addUsageCost(total.Cost.Output, delta.Cost.Output); err != nil {
		return Usage{}, fmt.Errorf("usage output cost: %w", err)
	}
	if result.Cost.CacheRead, err = addUsageCost(total.Cost.CacheRead, delta.Cost.CacheRead); err != nil {
		return Usage{}, fmt.Errorf("usage cache read cost: %w", err)
	}
	if result.Cost.CacheWrite, err = addUsageCost(total.Cost.CacheWrite, delta.Cost.CacheWrite); err != nil {
		return Usage{}, fmt.Errorf("usage cache write cost: %w", err)
	}
	if result.Cost.Total, err = addUsageCost(total.Cost.Total, delta.Cost.Total); err != nil {
		return Usage{}, fmt.Errorf("usage total cost: %w", err)
	}
	return result, nil
}

func addUsageInt(left, right int) (int, error) {
	if right > 0 && left > int(^uint(0)>>1)-right {
		return 0, fmt.Errorf("token count overflow")
	}
	return left + right, nil
}

func addUsageCost(left, right float64) (float64, error) {
	result := left + right
	if result < 0 || math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, fmt.Errorf("cost overflow")
	}
	return result, nil
}

func applyUsageContribution(
	state *durableRuntime,
	contributionID string,
	source string,
	usage Usage,
	includeTurn bool,
) (EncodedMutation, EncodedDurableEvent, bool, error) {
	if contributionID == "" || source == "" {
		return EncodedMutation{}, EncodedDurableEvent{}, false, fmt.Errorf("droids: usage contribution identity is required")
	}
	if usageIsZero(usage) {
		return EncodedMutation{}, EncodedDurableEvent{}, false, nil
	}
	nextSession, err := mergeUsage(state.SessionUsage, usage)
	if err != nil {
		return EncodedMutation{}, EncodedDurableEvent{}, false, err
	}
	nextTurn := state.Usage
	if includeTurn {
		nextTurn, err = mergeUsage(state.Usage, usage)
		if err != nil {
			return EncodedMutation{}, EncodedDurableEvent{}, false, err
		}
	}
	payload, err := json.Marshal(map[string]any{
		"id": contributionID, "source": source, "usage": usage,
	})
	if err != nil {
		return EncodedMutation{}, EncodedDurableEvent{}, false, err
	}
	turnID, attemptID := TurnID(""), AttemptID("")
	if includeTurn {
		turnID, attemptID = state.TurnID, state.AttemptID
	}
	event, err := lifecycleEvent("usage.updated", turnID, attemptID, map[string]any{
		"session_usage": nextSession,
	})
	if err != nil {
		return EncodedMutation{}, EncodedDurableEvent{}, false, err
	}
	state.SessionUsage = nextSession
	state.SessionUsageInitialized = true
	state.Usage = nextTurn
	return EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: usageContributionKind,
		RecordID: contributionID, Scope: RecordHistory, Version: recordVersion,
		Payload: payload,
	}, event, true, nil
}

func (rt *sdkRuntime) persistObservedUsageContribution(
	ctx context.Context,
	contributionID string,
	source string,
	usage Usage,
	turnID TurnID,
) error {
	persistenceContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return rt.persistUsageContribution(persistenceContext, contributionID, source, usage, turnID)
}

func (rt *sdkRuntime) accountCompactionResponse(ctx context.Context, model Model, response *AssistantMessage, turnID TurnID) error {
	if err := validateUsageTokens(response.Usage); err != nil {
		return fmt.Errorf("droids: compaction model returned invalid usage: %w", err)
	}
	calculateCost(model, &response.Usage)
	normalizeProviderError(response)
	if err := validateProviderTerminal(model, *response); err != nil {
		return fmt.Errorf("droids: invalid compaction response: %w", err)
	}
	usageID, err := newID("usage_")
	if err != nil {
		return err
	}
	return rt.persistObservedUsageContribution(ctx, usageID, "compaction", response.Usage, turnID)
}

func (rt *sdkRuntime) persistUsageContribution(
	ctx context.Context,
	contributionID string,
	source string,
	usage Usage,
	turnID TurnID,
) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	includeTurn := turnID != ""
	if includeTurn && rt.state.TurnID != turnID {
		return ErrConflict
	}
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	mutation, event, changed, err := applyUsageContribution(&rt.state, contributionID, source, usage, includeTurn)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err := rt.commitLocked(ctx, []EncodedMutation{mutation}, []EncodedDurableEvent{event}); err != nil {
		rt.state = before
		return err
	}
	return nil
}

func rebuildSessionUsage(ctx context.Context, store Store, conversation ConversationID) (SessionUsage, error) {
	var total SessionUsage
	var after uint64
	for {
		page, err := store.Records(ctx, RecordQuery{After: after, Limit: 1000, Kind: messageRecordKind})
		if err != nil {
			return SessionUsage{}, fmt.Errorf("droids: rebuild session usage: %w", err)
		}
		for _, record := range page.Records {
			if record.Scope != RecordHistory || record.Version != recordVersion || record.ID == "" {
				return SessionUsage{}, fmt.Errorf("droids: invalid message record while rebuilding session usage")
			}
			envelope, err := decodeMessageEnvelope(record.Payload)
			if err != nil {
				return SessionUsage{}, err
			}
			if envelope.ConversationID != conversation || string(envelope.ID) != record.ID {
				return SessionUsage{}, fmt.Errorf("droids: message identity mismatch while rebuilding session usage")
			}
			assistant, ok := envelope.Message.(AssistantMessage)
			if !ok || usageIsZero(assistant.Usage) {
				continue
			}
			total, err = mergeUsage(total, assistant.Usage)
			if err != nil {
				return SessionUsage{}, fmt.Errorf("droids: rebuild session usage from message %q: %w", envelope.ID, err)
			}
		}
		after = page.Next
		if !page.HasMore {
			return total, nil
		}
		if len(page.Records) == 0 {
			return SessionUsage{}, fmt.Errorf("droids: session usage history pagination did not advance")
		}
	}
}

func usageIsZero(usage Usage) bool {
	return usage.Input == 0 && usage.Output == 0 && usage.CacheRead == 0 &&
		usage.CacheWrite == 0 && usage.Reasoning == 0 && usage.TotalTokens == 0 &&
		usage.Cost.Input == 0 && usage.Cost.Output == 0 && usage.Cost.CacheRead == 0 &&
		usage.Cost.CacheWrite == 0 && usage.Cost.Total == 0
}
