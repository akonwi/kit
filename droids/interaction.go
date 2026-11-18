package droids

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// interaction.go — questions for the person. A declared interaction is raised
// by droids before a tool runs and survives restarts; a live interaction is
// asked by code that is already running and is settled as unavailable when
// that code cannot outlive a restart.

const (
	interactionKind           = "interaction"
	settledInteractionKind    = "interaction_settlement"
	interactionIDPrefix       = "interaction_"
	maxPendingInteractions    = 8
	maxInteractionBytes       = 64 << 10
	maxInteractionTitleBytes  = 512
	maxInteractionDetailBytes = 8 << 10
	maxInteractionLabelBytes  = 128
	maxInteractionAnswerBytes = 16 << 10
	maxInteractionOptions     = 64
	maxGuidedQuestions        = 32
	maxInteractionMetadata    = 16 << 10

	reasonDeclined    = "the user declined"
	reasonSteered     = "the user sent a message instead of answering"
	reasonUnavailable = "the question could not be answered"
	reasonCancelled   = "the turn ended before the question was answered"
)

var (
	// ErrInteractionNotFound reports an interaction that is not pending.
	ErrInteractionNotFound = errors.New("interaction is not pending")
	// ErrInvalidInteraction reports a question or answer that droids cannot
	// accept.
	ErrInvalidInteraction = errors.New("invalid interaction")
)

// InteractionID identifies one question asked of the person.
type InteractionID string

// QuestionKind identifies the shape of a question and of its answer.
type QuestionKind string

const (
	// QuestionConfirm asks yes or no.
	QuestionConfirm QuestionKind = "confirm"
	// QuestionInput asks for text.
	QuestionInput QuestionKind = "input"
	// QuestionSelect asks for one of Options.
	QuestionSelect QuestionKind = "select"
	// QuestionGuided asks Questions one at a time.
	QuestionGuided QuestionKind = "guided"
	// QuestionAuthorize asks the person to open URL, approve access, and
	// answer with the URL the browser was redirected to, or the code the page
	// showed.
	QuestionAuthorize QuestionKind = "authorize"
)

// GuidedQuestionKind identifies the answer shape of a guided question.
type GuidedQuestionKind string

const (
	GuidedText        GuidedQuestionKind = "text"
	GuidedSelect      GuidedQuestionKind = "select"
	GuidedMultiselect GuidedQuestionKind = "multiselect"
	GuidedBoolean     GuidedQuestionKind = "boolean"
)

// Option is one bounded choice. An answer names it by ID.
type Option struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Value  string `json:"value"`
	Detail string `json:"detail,omitempty"`
}

// GuidedQuestion is one step of a guided question.
type GuidedQuestion struct {
	ID       string             `json:"id"`
	Prompt   string             `json:"prompt"`
	Detail   string             `json:"detail,omitempty"`
	Kind     GuidedQuestionKind `json:"kind"`
	Required bool               `json:"required,omitempty"`
	Options  []Option           `json:"options,omitempty"`
}

// Question is what the person is asked. Fields beyond Kind, Title, and Detail
// belong to particular kinds: labels and Default to confirm, InitialValue to
// input, Placeholder to input and select, Filterable and Options to select,
// Questions to guided, and URL and RedirectURI to authorize.
type Question struct {
	Kind         QuestionKind     `json:"kind"`
	Title        string           `json:"title"`
	Detail       string           `json:"detail,omitempty"`
	ConfirmLabel string           `json:"confirm_label,omitempty"`
	CancelLabel  string           `json:"cancel_label,omitempty"`
	Default      *bool            `json:"default,omitempty"`
	Placeholder  string           `json:"placeholder,omitempty"`
	InitialValue string           `json:"initial_value,omitempty"`
	Filterable   *bool            `json:"filterable,omitempty"`
	Options      []Option         `json:"options,omitempty"`
	Questions    []GuidedQuestion `json:"questions,omitempty"`
	URL          string           `json:"url,omitempty"`
	RedirectURI  string           `json:"redirect_uri,omitempty"`
}

// GuidedAnswer answers one guided question.
type GuidedAnswer struct {
	Text      *string  `json:"text,omitempty"`
	Boolean   *bool    `json:"boolean,omitempty"`
	OptionIDs []string `json:"option_ids,omitempty"`
	Skipped   bool     `json:"skipped,omitempty"`
}

// Answer is the person's response. Declined answers nothing; otherwise the
// field matching the question's kind is set: Confirmed for confirm, Value for
// input and authorize, OptionID for select, and Answers for guided.
type Answer struct {
	Declined  bool                    `json:"declined,omitempty"`
	Confirmed *bool                   `json:"confirmed,omitempty"`
	Value     *string                 `json:"value,omitempty"`
	OptionID  string                  `json:"option_id,omitempty"`
	Answers   map[string]GuidedAnswer `json:"answers,omitempty"`
}

// InteractionStatus is how an interaction was settled.
type InteractionStatus string

const (
	// InteractionAnswered: the person answered.
	InteractionAnswered InteractionStatus = "answered"
	// InteractionDeclined: the person declined, or steered the turn instead
	// of answering.
	InteractionDeclined InteractionStatus = "declined"
	// InteractionUnavailable: the code waiting for a live interaction stopped,
	// or a restart ended it.
	InteractionUnavailable InteractionStatus = "unavailable"
	// InteractionCancelled: the turn that owned the interaction ended.
	InteractionCancelled InteractionStatus = "cancelled"
)

// Settlement is the outcome of an interaction.
type Settlement struct {
	Status InteractionStatus `json:"status"`
	// Answer is the person's answer when Status is InteractionAnswered.
	Answer Answer `json:"answer,omitzero"`
	// Reason explains, in words a model can read, why an interaction that was
	// not answered ended.
	Reason string `json:"reason,omitempty"`
}

// Interaction is one pending question.
type Interaction struct {
	ID InteractionID
	// TurnID and ToolCallID identify the turn and tool call that own the
	// interaction. Both are empty for a question asked outside any turn.
	TurnID     TurnID
	ToolCallID ToolCallID
	// Live reports that running code asked the question and waits for it, so
	// a restart settles it as unavailable. Otherwise droids raised it before a
	// tool ran, and it survives restarts.
	Live      bool
	Question  Question
	Metadata  json.RawMessage
	CreatedAt time.Time
}

// AskOptions configures a live question.
type AskOptions struct {
	// Call attaches the question to a tool call of the running turn, so that
	// steering declines it and the turn ending cancels it. Questions asked
	// through the package-level Ask are attached to their call.
	Call *ToolContext
	// Metadata is bounded application JSON returned with the interaction.
	Metadata json.RawMessage
}

type durableInteraction struct {
	ID         InteractionID   `json:"id"`
	TurnID     TurnID          `json:"turn_id,omitempty"`
	ToolCallID ToolCallID      `json:"tool_call_id,omitempty"`
	Live       bool            `json:"live,omitempty"`
	Question   Question        `json:"question"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

type durableSettledInteraction struct {
	durableInteraction
	Settlement Settlement `json:"settlement"`
	SettledAt  time.Time  `json:"settled_at"`
}

func (i durableInteraction) public() Interaction {
	return Interaction{
		ID: i.ID, TurnID: i.TurnID, ToolCallID: i.ToolCallID, Live: i.Live,
		Question: cloneQuestion(i.Question), Metadata: append(json.RawMessage(nil), i.Metadata...),
		CreatedAt: i.CreatedAt,
	}
}

// Interactions returns the pending interactions, oldest first.
func (d *Droid) Interactions(ctx context.Context) ([]Interaction, error) {
	if d == nil || d.sdk == nil {
		return nil, fmt.Errorf("droids: Interactions requires a droid opened with droids.Spawn")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.pendingInteractionsLocked(), nil
}

func (rt *sdkRuntime) pendingInteractionsLocked() []Interaction {
	result := make([]Interaction, 0, len(rt.state.Interactions))
	for _, id := range rt.state.Interactions {
		if interaction, ok := rt.interactions[id]; ok {
			result = append(result, interaction.public())
		}
	}
	return result
}

// Respond settles a pending interaction with the person's answer. It returns
// ErrInteractionNotFound when the interaction is not pending and
// ErrInvalidInteraction when the answer does not fit the question. Answering
// the last question a waiting turn waits for continues the turn.
func (d *Droid) Respond(ctx context.Context, id InteractionID, answer Answer) error {
	if d == nil || d.sdk == nil {
		return fmt.Errorf("droids: Respond requires a droid opened with droids.Spawn")
	}
	rt := d.sdk
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return ErrClosed
	}
	interaction, ok := rt.interactions[id]
	if !ok {
		return ErrInteractionNotFound
	}
	if err := validateAnswer(interaction.Question, answer); err != nil {
		return err
	}
	settlement := Settlement{Status: InteractionAnswered, Answer: cloneAnswer(answer)}
	if answer.Declined {
		settlement = Settlement{Status: InteractionDeclined, Reason: reasonDeclined}
	}
	return rt.settleInteractionsLocked(ctx, map[InteractionID]Settlement{id: settlement}, nil, nil)
}

// Ask asks the person a live question on behalf of the tool call that ctx
// belongs to, and waits for the answer. Call it from a tool's Execute or from
// a before-tool hook. It returns the settlement, with ctx's error when ctx
// ended first.
func Ask(ctx context.Context, question Question, metadata json.RawMessage) (Settlement, error) {
	scope, ok := ctx.Value(askScopeKey{}).(askScope)
	if !ok || scope.runtime == nil {
		return Settlement{}, fmt.Errorf("%w: Ask needs the context of a tool call", ErrInvalidInteraction)
	}
	call := scope.call
	return scope.runtime.ask(ctx, question, AskOptions{Call: &call, Metadata: metadata})
}

// Ask asks the person a live question and waits for the answer. Without
// options.Call the question belongs to no turn: steering and turns ending do
// not settle it. It returns the settlement, with ctx's error when ctx ended
// first, and ErrBusy when too many interactions are pending.
func (d *Droid) Ask(ctx context.Context, question Question, options AskOptions) (Settlement, error) {
	if d == nil || d.sdk == nil {
		return Settlement{}, fmt.Errorf("droids: Ask requires a droid opened with droids.Spawn")
	}
	return d.sdk.ask(ctx, question, options)
}

type askScopeKey struct{}

// declaredSettlementKey carries a standard question tool's settlement to its
// Execute.
type declaredSettlementKey struct{}

// declaredQuestionKey carries the question a standard question tool asked to
// its Execute.
type declaredQuestionKey struct{}

func declaredSettlement(ctx context.Context) (Settlement, bool) {
	settlement, ok := ctx.Value(declaredSettlementKey{}).(Settlement)
	return settlement, ok
}

type askScope struct {
	runtime *sdkRuntime
	call    ToolContext
}

func withAskScope(ctx context.Context, rt *sdkRuntime, call ToolContext) context.Context {
	return context.WithValue(ctx, askScopeKey{}, askScope{runtime: rt, call: call})
}

func (rt *sdkRuntime) ask(ctx context.Context, question Question, options AskOptions) (Settlement, error) {
	if err := validateQuestion(question); err != nil {
		return Settlement{}, err
	}
	if err := validateInteractionMetadata(options.Metadata); err != nil {
		return Settlement{}, err
	}
	id, err := newID(interactionIDPrefix)
	if err != nil {
		return Settlement{}, err
	}
	interaction := durableInteraction{
		ID: InteractionID(id), Live: true, Question: cloneQuestion(question),
		Metadata: append(json.RawMessage(nil), options.Metadata...), CreatedAt: time.Now().UTC(),
	}
	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return Settlement{}, ErrClosed
	}
	if call := options.Call; call != nil {
		if _, admitted := rt.state.Tools[call.ToolCallID]; !admitted || call.TurnID != rt.state.TurnID || !isOccupied(rt.state.Status) {
			rt.mu.Unlock()
			return Settlement{}, fmt.Errorf("%w: tool call %q is not running", ErrInvalidInteraction, call.ToolCallID)
		}
		interaction.TurnID, interaction.ToolCallID = call.TurnID, call.ToolCallID
	}
	if len(rt.state.Interactions) >= maxPendingInteractions {
		rt.mu.Unlock()
		return Settlement{}, fmt.Errorf("%w: too many interactions are pending", ErrBusy)
	}
	waiter := make(chan Settlement, 1)
	if err := rt.requestInteractionLocked(ctx, interaction, nil); err != nil {
		rt.mu.Unlock()
		return Settlement{}, err
	}
	rt.liveWaiters[interaction.ID] = waiter
	rt.mu.Unlock()

	select {
	case settlement := <-waiter:
		return settlement, nil
	case <-ctx.Done():
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	select {
	case settlement := <-waiter:
		return settlement, nil
	default:
	}
	delete(rt.liveWaiters, interaction.ID)
	settlement := Settlement{Status: InteractionUnavailable, Reason: reasonUnavailable}
	if _, pending := rt.interactions[interaction.ID]; pending && !rt.closed {
		// A failed commit leaves it pending; Spawn settles it after a restart.
		_ = rt.settleInteractionsLocked(context.WithoutCancel(ctx), map[InteractionID]Settlement{interaction.ID: settlement}, nil, nil)
	}
	return settlement, ctx.Err()
}

// requestInteractionLocked durably records a pending interaction, with any
// extra mutations, and announces it. The caller holds rt.mu.
func (rt *sdkRuntime) requestInteractionLocked(ctx context.Context, interaction durableInteraction, extra []EncodedMutation) error {
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	mutation, err := interactionMutation(interaction)
	if err != nil {
		return err
	}
	rt.state.Interactions = append(rt.state.Interactions, interaction.ID)
	event, _ := lifecycleEvent("interaction.requested", interaction.TurnID, rt.state.AttemptID, map[string]any{
		"interaction_id": interaction.ID, "tool_call_id": interaction.ToolCallID,
		"live": interaction.Live, "kind": interaction.Question.Kind,
	})
	if err := rt.commitLocked(ctx, append([]EncodedMutation{mutation}, extra...), []EncodedDurableEvent{event}); err != nil {
		rt.state = before
		return err
	}
	rt.interactions[interaction.ID] = interaction
	return nil
}

// settleInteractionsLocked durably settles pending interactions in one commit
// with extra mutations and events, wakes the code waiting for live ones, and
// continues a waiting turn whose questions are all settled. The caller holds
// rt.mu and has already applied to rt.state any changes extra records.
func (rt *sdkRuntime) settleInteractionsLocked(ctx context.Context, settlements map[InteractionID]Settlement, extra []EncodedMutation, extraEvents []EncodedDurableEvent) error {
	before, err := cloneDurableRuntime(rt.state)
	if err != nil {
		return err
	}
	mutations, events, err := rt.settleInteractionMutationsLocked(settlements)
	if err != nil {
		rt.state = before
		return err
	}
	mutations = append(mutations, extra...)
	events = append(events, extraEvents...)
	continuing := rt.state.Status == ExecutionWaiting && !rt.awaitingAnswersLocked()
	if continuing {
		rt.state.Status = ExecutionRunning
		continued, _ := lifecycleEvent("execution.continued", rt.state.TurnID, rt.state.AttemptID, nil)
		events = append(events, continued)
	}
	if err := rt.commitLocked(ctx, mutations, events); err != nil {
		rt.state = before
		return err
	}
	rt.finishSettlingLocked(settlements)
	if continuing {
		rt.startRunLocked()
	}
	return nil
}

// settleInteractionMutationsLocked moves settled interactions out of pending
// state and returns the records and events that persist it. A declared
// interaction's settlement is recorded on its tool call. The caller commits
// them and then calls finishSettlingLocked.
func (rt *sdkRuntime) settleInteractionMutationsLocked(settlements map[InteractionID]Settlement) ([]EncodedMutation, []EncodedDurableEvent, error) {
	var mutations []EncodedMutation
	var events []EncodedDurableEvent
	now := time.Now().UTC()
	// Settle in request order, so records and events are deterministic.
	for _, id := range slices.Clone(rt.state.Interactions) {
		settlement, ok := settlements[id]
		if !ok {
			continue
		}
		interaction, known := rt.interactions[id]
		if !known {
			return nil, nil, fmt.Errorf("droids: pending interaction %q has no record", id)
		}
		rt.state.Interactions = slices.DeleteFunc(rt.state.Interactions, func(pending InteractionID) bool { return pending == id })
		payload, err := json.Marshal(durableSettledInteraction{durableInteraction: interaction, Settlement: settlement, SettledAt: now})
		if err != nil {
			return nil, nil, err
		}
		mutations = append(mutations,
			EncodedMutation{Operation: MutationDelete, RecordKind: interactionKind, RecordID: string(id), Scope: RecordRuntime, Version: recordVersion},
			EncodedMutation{Operation: MutationAssertAbsent, RecordKind: settledInteractionKind, RecordID: string(id), Scope: RecordHistory, Version: recordVersion, Payload: payload},
		)
		if !interaction.Live {
			tool, admitted := rt.state.Tools[interaction.ToolCallID]
			if admitted && tool.Phase == toolPhaseAwaiting && tool.Interaction == id {
				recorded := settlement
				tool.Settlement = &recorded
				rt.state.Tools[tool.ID] = tool
				toolPayload, err := json.Marshal(tool)
				if err != nil {
					return nil, nil, err
				}
				mutations = append(mutations, EncodedMutation{
					Operation: MutationPut, RecordKind: toolRecordKind, RecordID: string(tool.ID),
					Scope: RecordRuntime, Version: recordVersion, Payload: toolPayload,
				})
			}
		}
		event, _ := lifecycleEvent("interaction.settled", interaction.TurnID, rt.state.AttemptID, map[string]any{
			"interaction_id": id, "tool_call_id": interaction.ToolCallID, "status": settlement.Status,
		})
		events = append(events, event)
	}
	return mutations, events, nil
}

// finishSettlingLocked forgets committed settlements and wakes the code
// waiting for live ones.
func (rt *sdkRuntime) finishSettlingLocked(settlements map[InteractionID]Settlement) {
	for id, settlement := range settlements {
		delete(rt.interactions, id)
		if waiter, ok := rt.liveWaiters[id]; ok {
			delete(rt.liveWaiters, id)
			waiter <- settlement
		}
	}
}

// turnInteractionsLocked returns the pending interactions the current turn
// owns, settled as status with reason.
func (rt *sdkRuntime) turnInteractionsLocked(status InteractionStatus, reason string) map[InteractionID]Settlement {
	settlements := make(map[InteractionID]Settlement)
	if rt.state.TurnID == "" {
		return settlements
	}
	for _, id := range rt.state.Interactions {
		if interaction, ok := rt.interactions[id]; ok && interaction.TurnID == rt.state.TurnID {
			settlements[id] = Settlement{Status: status, Reason: reason}
		}
	}
	return settlements
}

// awaitingAnswersLocked reports whether a tool call of the current turn waits
// for a declared interaction that is not settled.
func (rt *sdkRuntime) awaitingAnswersLocked() bool {
	for _, tool := range rt.state.Tools {
		if tool.Phase == toolPhaseAwaiting && tool.Settlement == nil {
			return true
		}
	}
	return false
}

// wakeLiveWaitersLocked ends every live wait as unavailable without
// recording it, for a closing runtime; Spawn records it after a restart.
func (rt *sdkRuntime) wakeLiveWaitersLocked() {
	for id, waiter := range rt.liveWaiters {
		delete(rt.liveWaiters, id)
		waiter <- Settlement{Status: InteractionUnavailable, Reason: reasonUnavailable}
	}
}

func interactionMutation(interaction durableInteraction) (EncodedMutation, error) {
	payload, err := json.Marshal(interaction)
	if err != nil {
		return EncodedMutation{}, err
	}
	return EncodedMutation{
		Operation: MutationAssertAbsent, RecordKind: interactionKind, RecordID: string(interaction.ID),
		Scope: RecordRuntime, Version: recordVersion, Payload: payload,
	}, nil
}

// loadInteractions loads and validates the pending interactions from a
// conversation's runtime records.
func loadInteractions(records []EncodedRecord, pending []InteractionID) (map[InteractionID]durableInteraction, error) {
	if len(pending) > maxPendingInteractions {
		return nil, fmt.Errorf("droids: persisted interactions are too many")
	}
	bodies := make(map[InteractionID]durableInteraction)
	for _, record := range records {
		if record.Kind != interactionKind {
			continue
		}
		var interaction durableInteraction
		if record.Scope != RecordRuntime || record.Version != recordVersion || json.Unmarshal(record.Payload, &interaction) != nil || string(interaction.ID) != record.ID || validateQuestion(interaction.Question) != nil {
			return nil, fmt.Errorf("droids: persisted interaction %q is invalid", record.ID)
		}
		bodies[interaction.ID] = interaction
	}
	for _, id := range pending {
		if _, ok := bodies[id]; !ok {
			return nil, fmt.Errorf("droids: pending interaction %q has no record", id)
		}
	}
	return bodies, nil
}

// settleRestoredLiveLocked settles live interactions a restart ended. The
// caller holds rt.mu.
func (rt *sdkRuntime) settleRestoredLiveLocked(ctx context.Context) error {
	settlements := make(map[InteractionID]Settlement)
	for _, id := range rt.state.Interactions {
		if rt.interactions[id].Live {
			settlements[id] = Settlement{Status: InteractionUnavailable, Reason: reasonUnavailable}
		}
	}
	if len(settlements) == 0 {
		return nil
	}
	return rt.settleInteractionsLocked(ctx, settlements, nil, nil)
}

func validateInteractionMetadata(metadata json.RawMessage) error {
	if len(metadata) > 0 && !json.Valid(metadata) {
		return fmt.Errorf("%w: metadata is not valid JSON", ErrInvalidInteraction)
	}
	if len(metadata) > maxInteractionMetadata {
		return fmt.Errorf("%w: metadata exceeds %d bytes", ErrInvalidInteraction, maxInteractionMetadata)
	}
	return nil
}

func validateQuestion(question Question) error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidInteraction, reason) }
	if !boundedText(question.Title, maxInteractionTitleBytes, true) || !boundedText(question.Detail, maxInteractionDetailBytes, false) {
		return invalid("title or detail is invalid")
	}
	if !boundedText(question.ConfirmLabel, maxInteractionLabelBytes, false) || !boundedText(question.CancelLabel, maxInteractionLabelBytes, false) ||
		!boundedText(question.Placeholder, maxInteractionTitleBytes, false) || !boundedText(question.InitialValue, maxInteractionAnswerBytes, false) {
		return invalid("presentation text is invalid")
	}
	kind := question.Kind
	if kind != QuestionConfirm && (question.ConfirmLabel != "" || question.CancelLabel != "" || question.Default != nil) {
		return invalid("labels and default belong to confirm questions")
	}
	if kind != QuestionInput && question.InitialValue != "" {
		return invalid("an initial value belongs to input questions")
	}
	if kind != QuestionInput && kind != QuestionSelect && question.Placeholder != "" {
		return invalid("a placeholder belongs to input and select questions")
	}
	if kind != QuestionSelect && (question.Filterable != nil || len(question.Options) > 0) {
		return invalid("options belong to select questions")
	}
	if kind != QuestionGuided && len(question.Questions) > 0 {
		return invalid("questions belong to guided questions")
	}
	if kind != QuestionAuthorize && (question.URL != "" || question.RedirectURI != "") {
		return invalid("a URL belongs to authorize questions")
	}
	switch kind {
	case QuestionConfirm, QuestionInput:
	case QuestionSelect:
		if len(question.Options) == 0 {
			return invalid("a select question needs options")
		}
		if err := validateOptions(question.Options); err != nil {
			return err
		}
	case QuestionGuided:
		if len(question.Questions) == 0 || len(question.Questions) > maxGuidedQuestions {
			return invalid("a guided question needs between 1 and 32 questions")
		}
		seen := make(map[string]struct{}, len(question.Questions))
		for _, guided := range question.Questions {
			if !boundedText(guided.ID, maxInteractionLabelBytes, true) || !boundedText(guided.Prompt, maxInteractionTitleBytes, true) || !boundedText(guided.Detail, maxInteractionDetailBytes, false) {
				return invalid("a guided question's text is invalid")
			}
			if _, duplicate := seen[guided.ID]; duplicate {
				return invalid("guided question IDs must be unique")
			}
			seen[guided.ID] = struct{}{}
			switch guided.Kind {
			case GuidedText, GuidedBoolean:
				if len(guided.Options) > 0 {
					return invalid("options belong to select questions")
				}
			case GuidedSelect, GuidedMultiselect:
				if len(guided.Options) == 0 {
					return invalid("a select question needs options")
				}
				if err := validateOptions(guided.Options); err != nil {
					return err
				}
			default:
				return invalid(fmt.Sprintf("guided question kind %q is unsupported", guided.Kind))
			}
		}
	case QuestionAuthorize:
		if !boundedText(question.URL, maxInteractionDetailBytes, true) || !boundedText(question.RedirectURI, 2<<10, true) {
			return invalid("an authorize question needs a URL and a redirect URI")
		}
	default:
		return invalid(fmt.Sprintf("question kind %q is unsupported", kind))
	}
	encoded, err := json.Marshal(question)
	if err != nil || len(encoded) > maxInteractionBytes {
		return invalid(fmt.Sprintf("a question exceeds %d bytes", maxInteractionBytes))
	}
	return nil
}

func validateOptions(options []Option) error {
	if len(options) > maxInteractionOptions {
		return fmt.Errorf("%w: too many options", ErrInvalidInteraction)
	}
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		if !boundedText(option.ID, maxInteractionLabelBytes, true) || !boundedText(option.Label, maxInteractionTitleBytes, true) ||
			!boundedText(option.Value, maxInteractionAnswerBytes, true) || !boundedText(option.Detail, maxInteractionDetailBytes, false) {
			return fmt.Errorf("%w: an option is invalid", ErrInvalidInteraction)
		}
		if _, duplicate := seen[option.ID]; duplicate {
			return fmt.Errorf("%w: option IDs must be unique", ErrInvalidInteraction)
		}
		seen[option.ID] = struct{}{}
	}
	return nil
}

func validateAnswer(question Question, answer Answer) error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidInteraction, reason) }
	if answer.Declined {
		if answer.Confirmed != nil || answer.Value != nil || answer.OptionID != "" || answer.Answers != nil {
			return invalid("a declined answer carries no answer")
		}
		return nil
	}
	only := func(confirmed, value, option, answers bool) bool {
		return (answer.Confirmed != nil) == confirmed && (answer.Value != nil) == value &&
			(answer.OptionID != "") == option && (answer.Answers != nil) == answers
	}
	switch question.Kind {
	case QuestionConfirm:
		if !only(true, false, false, false) {
			return invalid("a confirm answer is yes or no")
		}
	case QuestionInput, QuestionAuthorize:
		if !only(false, true, false, false) || !boundedText(*answer.Value, maxInteractionAnswerBytes, question.Kind == QuestionAuthorize) {
			return invalid("the answer needs text")
		}
	case QuestionSelect:
		if !only(false, false, true, false) || !hasOption(question.Options, answer.OptionID) {
			return invalid("the answer must name one of the options")
		}
	case QuestionGuided:
		if !only(false, false, false, true) {
			return invalid("a guided answer answers its questions")
		}
		for id := range answer.Answers {
			if !slices.ContainsFunc(question.Questions, func(guided GuidedQuestion) bool { return guided.ID == id }) {
				return invalid(fmt.Sprintf("guided question %q does not exist", id))
			}
		}
		for _, guided := range question.Questions {
			response, answered := answer.Answers[guided.ID]
			if !answered || response.Skipped {
				if guided.Required {
					return invalid(fmt.Sprintf("guided question %q is required", guided.ID))
				}
				if answered && (response.Text != nil || response.Boolean != nil || response.OptionIDs != nil) {
					return invalid("a skipped question carries no answer")
				}
				continue
			}
			valid := false
			switch guided.Kind {
			case GuidedText:
				valid = response.Text != nil && response.Boolean == nil && response.OptionIDs == nil && boundedText(*response.Text, maxInteractionAnswerBytes, false)
			case GuidedBoolean:
				valid = response.Boolean != nil && response.Text == nil && response.OptionIDs == nil
			case GuidedSelect:
				valid = response.Text == nil && response.Boolean == nil && len(response.OptionIDs) == 1 && hasOption(guided.Options, response.OptionIDs[0])
			case GuidedMultiselect:
				valid = response.Text == nil && response.Boolean == nil && len(response.OptionIDs) <= len(guided.Options)
				seen := make(map[string]struct{}, len(response.OptionIDs))
				for _, option := range response.OptionIDs {
					_, duplicate := seen[option]
					seen[option] = struct{}{}
					valid = valid && !duplicate && hasOption(guided.Options, option)
				}
			}
			if !valid {
				return invalid(fmt.Sprintf("the answer to guided question %q does not fit it", guided.ID))
			}
		}
	default:
		return invalid(fmt.Sprintf("question kind %q is unsupported", question.Kind))
	}
	if encoded, err := json.Marshal(answer); err != nil || len(encoded) > maxInteractionBytes {
		return invalid(fmt.Sprintf("an answer exceeds %d bytes", maxInteractionBytes))
	}
	return nil
}

func hasOption(options []Option, id string) bool {
	return slices.ContainsFunc(options, func(option Option) bool { return option.ID == id })
}

// boundedText accepts valid UTF-8 without NUL bytes of at most maximum bytes,
// and, when required, with visible content.
func boundedText(value string, maximum int, required bool) bool {
	if len(value) > maximum || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return false
	}
	return !required || strings.TrimSpace(value) != ""
}

func cloneQuestion(question Question) Question {
	if question.Default != nil {
		value := *question.Default
		question.Default = &value
	}
	if question.Filterable != nil {
		value := *question.Filterable
		question.Filterable = &value
	}
	question.Options = slices.Clone(question.Options)
	question.Questions = slices.Clone(question.Questions)
	for index := range question.Questions {
		question.Questions[index].Options = slices.Clone(question.Questions[index].Options)
	}
	return question
}

func cloneAnswer(answer Answer) Answer {
	if answer.Confirmed != nil {
		value := *answer.Confirmed
		answer.Confirmed = &value
	}
	if answer.Value != nil {
		value := *answer.Value
		answer.Value = &value
	}
	if answer.Answers != nil {
		answers := make(map[string]GuidedAnswer, len(answer.Answers))
		for id, response := range answer.Answers {
			if response.Text != nil {
				value := *response.Text
				response.Text = &value
			}
			if response.Boolean != nil {
				value := *response.Boolean
				response.Boolean = &value
			}
			response.OptionIDs = slices.Clone(response.OptionIDs)
			answers[id] = response
		}
		answer.Answers = answers
	}
	return answer
}
