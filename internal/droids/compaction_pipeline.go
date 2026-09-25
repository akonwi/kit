package droids

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	compactionKeepRecentTokens   = 20_000
	maxCompactionSummaryAttempts = 3
)

type compactedContext struct {
	prefixEnd int
	context   []wireMessageEnvelope
	summary   wireMessageEnvelope
	after     ContextUsage
}

func (result compactedContext) retainedFrom() string {
	if len(result.context) > 1 {
		return string(result.context[1].ID)
	}
	return ""
}

type unsuitableCompactionCandidate struct{ cause error }

func (err *unsuitableCompactionCandidate) Error() string { return err.cause.Error() }
func (err *unsuitableCompactionCandidate) Unwrap() error { return err.cause }
func unsuitableCandidate(err error) error                { return &unsuitableCompactionCandidate{cause: err} }

// compactionPrefixEnds prefers a recent 20K-token suffix. The remaining cuts
// allow smaller models, forced short-context compaction, and replay adaptation
// to retain less when necessary. Cuts are independent of user-turn boundaries;
// an assistant tool-call message and all its results are always one unit.
func compactionPrefixEnds(messages []Message, protectedFrom int) ([]int, error) {
	if err := validateMessageSequence(messages); err != nil {
		return nil, fmt.Errorf("droids: invalid compaction context: %w", err)
	}
	if protectedFrom < 0 || protectedFrom > len(messages) {
		return nil, fmt.Errorf("droids: invalid protected compaction tail")
	}
	start, tokens := len(messages), 0
	for start > 0 && tokens < compactionKeepRecentTokens {
		start--
		tokens += EstimateMessagesTokens(messages[start : start+1])
	}
	start = min(start, protectedFrom)
	for start > 0 {
		if _, isResult := messages[start].(ToolResultMessage); !isResult {
			break
		}
		start--
	}
	var ends []int
	for index := max(1, start); index <= protectedFrom; index++ {
		if index < len(messages) {
			if _, isResult := messages[index].(ToolResultMessage); isResult {
				continue
			}
		}
		ends = append(ends, index)
	}
	return ends, nil
}

// unconsumedCompactionTail protects input that the next model response must see.
// A completed tool batch is not consumed merely because it is durable: its
// calling assistant and results remain protected until a later model response.
func unconsumedCompactionTail(messages []Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if assistant, ok := messages[index].(AssistantMessage); ok {
			if len(assistant.ToolCalls()) > 0 {
				return index
			}
			return index + 1
		}
	}
	return 0
}

// compactContext is shared by in-turn recovery/pressure compaction and settled
// explicit compaction. Callers own admission, operation identity, and the atomic
// checkpoint commit; this pipeline never replaces active context itself.
func (rt *sdkRuntime) compactContext(
	ctx context.Context,
	target resolvedContextTarget,
	contextWire []wireMessageEnvelope,
	before, currentBefore ContextUsage,
	configuration *runtimeRequestConfiguration,
	usageTurnID TurnID,
) (compactedContext, error) {
	messages, err := messagesFromWireContext(contextWire)
	if err != nil {
		return compactedContext{}, err
	}
	protectedFrom := len(messages)
	if usageTurnID != "" {
		protectedFrom = unconsumedCompactionTail(messages)
	}
	ends, err := compactionPrefixEnds(messages, protectedFrom)
	if err != nil {
		return compactedContext{}, err
	}
	var lastErr error
	var previous *compactedContext
	attempts := 0
	for _, prefixEnd := range ends {
		if err := contextError(ctx); err != nil {
			return compactedContext{}, err
		}
		result, err := rt.compactCandidate(ctx, target, contextWire, messages, prefixEnd, before, currentBefore, configuration, usageTurnID, previous)
		if err == nil {
			return result, nil
		}
		var unsuitable *unsuitableCompactionCandidate
		if !errors.As(err, &unsuitable) {
			return compactedContext{}, err
		}
		lastErr = err
		if len(result.context) > 0 {
			// A generated summary was too large for this suffix. Carry its memory
			// forward instead of paying to summarize the same original prefix again.
			previous = &result
			attempts++
			if attempts >= maxCompactionSummaryAttempts {
				break
			}
		}
	}
	return compactedContext{}, errors.Join(ErrContextNotAdaptable, lastErr)
}

func (rt *sdkRuntime) compactCandidate(
	ctx context.Context,
	target resolvedContextTarget,
	contextWire []wireMessageEnvelope,
	messages []Message,
	prefixEnd int,
	before, currentBefore ContextUsage,
	configuration *runtimeRequestConfiguration,
	usageTurnID TurnID,
	previous *compactedContext,
) (compactedContext, error) {
	prefix, suffix := messages[:prefixEnd], messages[prefixEnd:]
	turnID := compactionSummaryTurnID(contextWire[:prefixEnd])
	if turnID == "" {
		return compactedContext{}, fmt.Errorf("droids: compaction prefix has no message provenance")
	}
	// A standalone textual request does not replay old provider metadata. Target
	// adaptation must not depend on the provider being left behind being online.
	provider, model := target.provider, target.model
	if !rt.config.Compaction.Model.IsZero() {
		model = cloneModel(rt.config.Compaction.Model)
		provider = model.boundProvider()
		if provider == nil || provider.ID() != model.Provider {
			return compactedContext{}, fmt.Errorf("droids: compaction Model must be resolved")
		}
	}
	reservedSummary, err := resolveRequestMaxTokens(model, 0, "")
	if err != nil {
		return compactedContext{}, err
	}
	reservedSummary = compactionSummaryHeadroom(reservedSummary, before, currentBefore, configuration)
	var placeholder Message = ContextMessage{
		Kind: "summary", Source: "compaction",
		Content: []InputContent{TextInput{Text: "[context summary]\n" + strings.Repeat("x", 2*reservedSummary)}},
	}
	if previous != nil {
		envelope, err := messageEnvelopeFromWire(previous.summary)
		if err != nil {
			return compactedContext{}, err
		}
		// Unlike hypothetical headroom, this is a real summary. Skip cuts that
		// cannot shrink context at its observed size without another paid call.
		if _, err := rt.validateCompactionReplacement(ctx, target, append([]Message{envelope.Message}, suffix...), before, currentBefore, configuration, true); err != nil {
			return compactedContext{}, err
		}
		prefix = append([]Message{envelope.Message}, messages[previous.prefixEnd:prefixEnd]...)
		if EstimateMessagesTokens([]Message{envelope.Message}) > EstimateMessagesTokens([]Message{placeholder}) {
			placeholder = envelope.Message
		}
	}
	candidate := append([]Message{placeholder}, suffix...)
	// Reserve useful summary headroom and reject unsuitable suffixes before
	// making a paid request. After an oversized result, its measured shape also
	// gates subsequent cuts, rather than retrying one source message at a time.
	if _, err := rt.validateCompactionReplacement(ctx, target, candidate, before, currentBefore, configuration, false); err != nil {
		return compactedContext{}, err
	}

	summary, err := rt.summarizeCompaction(ctx, provider, model, prefix, usageTurnID)
	if err != nil {
		return compactedContext{}, err
	}
	if err := contextError(ctx); err != nil {
		return compactedContext{}, err
	}
	messageID, err := newMessageID()
	if err != nil {
		return compactedContext{}, err
	}
	envelope := MessageEnvelope{
		ID: messageID, ConversationID: rt.conversation, TurnID: turnID,
		CreatedAt: time.Now().UTC(),
		Message: ContextMessage{
			Kind: "summary", Source: "compaction",
			Content: []InputContent{TextInput{Text: "[context summary]\n" + summary}},
		},
	}
	wire, err := messageEnvelopeToWire(envelope)
	if err != nil {
		return compactedContext{}, err
	}
	candidate[0] = envelope.Message
	after, err := rt.validateCompactionReplacement(ctx, target, candidate, before, currentBefore, configuration, true)
	return compactedContext{
		prefixEnd: prefixEnd,
		context:   append([]wireMessageEnvelope{wire}, contextWire[prefixEnd:]...),
		summary:   wire, after: after,
	}, err
}

// Reserve modest summary space without excluding short forced compactions or
// small target models. An actual larger summary replaces this estimate on the
// next candidate, while the number of paid candidate attempts remains bounded.
func compactionSummaryHeadroom(outputLimit int, target, current ContextUsage, configuration *runtimeRequestConfiguration) int {
	headroom := min(256, outputLimit)
	overhead := estimateRequestTokens(Request{SystemPrompt: configuration.systemPrompt, Tools: configuration.toolSchemas})
	for _, usage := range []ContextUsage{target, current} {
		available, limited := 0, false
		if usage.ContextWindow > 0 {
			available = int(float64(usage.ContextWindow)*defaultCompactionTriggerFraction) - usage.ReservedOutput
			limited = true
		}
		if usage.MaxInputTokens > 0 {
			inputLimit := int(float64(usage.MaxInputTokens) * defaultCompactionTriggerFraction)
			if !limited || inputLimit < available {
				available = inputLimit
			}
			limited = true
		}
		if limited {
			headroom = min(headroom, max(1, (available-overhead)/2))
		}
	}
	return headroom
}

func (rt *sdkRuntime) validateCompactionReplacement(
	ctx context.Context,
	target resolvedContextTarget,
	messages []Message,
	before, currentBefore ContextUsage,
	configuration *runtimeRequestConfiguration,
	requireReduction bool,
) (ContextUsage, error) {
	if err := validateMessageSequence(messages); err != nil {
		return ContextUsage{}, fmt.Errorf("droids: invalid compacted context: %w", err)
	}
	for _, replay := range []struct {
		provider Provider
		model    Model
	}{
		{target.provider, target.model},
		{rt.provider, rt.droid.model},
	} {
		if err := validateContextReplay(ctx, replay.provider, replay.model, messages); err != nil {
			if contextError(ctx) != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return ContextUsage{}, err
			}
			return ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compacted context is not replayable by %s/%s: %w", replay.model.Provider, replay.model.ID, err))
		}
	}
	after, err := rt.measureContextWithConfiguration(ctx, target.provider, target.model, target.public.Reasoning, target.maxTokens, messages, configuration)
	if err != nil {
		return ContextUsage{}, err
	}
	if requireReduction && after.EstimatedInput >= before.EstimatedInput {
		return ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compaction did not reduce target context"))
	}
	if sdkShouldCompact(after) {
		return ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compacted context remains above the target budget"))
	}
	currentAfter, err := rt.measureContextWithConfiguration(ctx, rt.provider, rt.droid.model, configuration.reasoning, configuration.maxTokens, messages, configuration)
	if err != nil {
		return ContextUsage{}, err
	}
	if !contextCanRun(currentAfter) || (requireReduction && currentAfter.EstimatedInput > currentBefore.EstimatedInput) {
		return ContextUsage{}, unsuitableCandidate(fmt.Errorf("droids: compacted context is not safe for the current model"))
	}
	return after, nil
}
