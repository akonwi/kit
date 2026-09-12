package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
)

const (
	ConfirmInteractionToolName = "confirm_from_user"
	InputInteractionToolName   = "input_from_user"
	SelectInteractionToolName  = "select_from_user"
	GuidedInteractionToolName  = "guided_questions"

	maxPendingInteractions    = 8
	maxInteractionBytes       = 64 << 10
	maxInteractionTitleBytes  = 512
	maxInteractionDetailBytes = 8 << 10
	maxInteractionAnswerBytes = 16 << 10
	maxInteractionOptions     = 64
	maxGuidedQuestions        = 32
)

var (
	ErrInteractionNotFound = errors.New("interaction is not pending")
	ErrInteractionSettled  = errors.New("interaction is already settled")
	ErrInteractionCapacity = errors.New("session has too many pending interactions")
)

// InteractionKind identifies one server-owned model-user request.
type InteractionKind string

const (
	InteractionConfirm InteractionKind = "confirm"
	InteractionInput   InteractionKind = "input"
	InteractionSelect  InteractionKind = "select"
	InteractionGuided  InteractionKind = "guided"
)

// InteractionQuestionKind identifies the answer shape of a guided question.
type InteractionQuestionKind string

const (
	InteractionQuestionText        InteractionQuestionKind = "text"
	InteractionQuestionSelect      InteractionQuestionKind = "select"
	InteractionQuestionMultiselect InteractionQuestionKind = "multiselect"
	InteractionQuestionBoolean     InteractionQuestionKind = "boolean"
)

// InteractionOption is a server-owned bounded choice. ID is transport-only;
// Value is returned to the model after settlement.
type InteractionOption struct {
	ID     string
	Label  string
	Value  string
	Detail string
}

// InteractionQuestion is one guided-questionnaire step.
type InteractionQuestion struct {
	ID       string
	Prompt   string
	Detail   string
	Kind     InteractionQuestionKind
	Required bool
	Options  []InteractionOption
}

// InteractionRequest is the canonical pending request projected to clients.
type InteractionRequest struct {
	ID         string
	SessionID  string
	RunID      string
	ToolCallID string
	Kind       InteractionKind
	Title      string
	Detail     string
	Options    []InteractionOption
	Questions  []InteractionQuestion
	CreatedAt  time.Time
}

// InteractionAnswer is one renderer response to a guided question.
type InteractionAnswer struct {
	Text      *string
	Boolean   *bool
	OptionIDs []string
	Skipped   bool
}

// InteractionResponse is submitted by a client. Cancelled responses must not
// carry answers.
type InteractionResponse struct {
	RequestID        string
	Cancelled        bool
	Confirmed        *bool
	Value            *string
	SelectedOptionID string
	Answers          map[string]InteractionAnswer
}

type interactionSettlement struct {
	response InteractionResponse
	reason   string
}

type pendingInteraction struct {
	request InteractionRequest
	result  chan interactionSettlement
}

type interactionBroker struct {
	mu        sync.Mutex
	pending   []*pendingInteraction
	settled   []string
	authority *sync.Mutex
	publish   func(NewEvent)
}

func newInteractionBroker(publish func(NewEvent)) *interactionBroker {
	return &interactionBroker{publish: publish}
}

func (b *interactionBroker) snapshot() []InteractionRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	requests := make([]InteractionRequest, 0, len(b.pending))
	for _, pending := range b.pending {
		requests = append(requests, cloneInteractionRequest(pending.request))
	}
	return requests
}

func (b *interactionBroker) request(ctx context.Context, request InteractionRequest) (interactionSettlement, error) {
	if err := validateInteractionRequest(request); err != nil {
		return interactionSettlement{}, err
	}
	pending := &pendingInteraction{request: cloneInteractionRequest(request), result: make(chan interactionSettlement, 1)}
	b.lockAuthority()
	b.mu.Lock()
	if len(b.pending) >= maxPendingInteractions {
		b.mu.Unlock()
		b.unlockAuthority()
		return interactionSettlement{}, ErrInteractionCapacity
	}
	b.pending = append(b.pending, pending)
	b.mu.Unlock()
	b.emit(NewEvent{SessionID: request.SessionID, TurnID: request.RunID, RunID: request.RunID, Kind: EventInteractionRequested, Interaction: ptrInteractionRequest(request)})
	b.unlockAuthority()

	select {
	case settlement := <-pending.result:
		return settlement, nil
	case <-ctx.Done():
		settlement, won := b.settle(request.ID, InteractionResponse{RequestID: request.ID, Cancelled: true}, "run_abort")
		if won {
			return settlement, nil
		}
		return <-pending.result, nil
	}
}

func (b *interactionBroker) respond(response InteractionResponse) error {
	b.lockAuthority()
	b.mu.Lock()
	index := -1
	for i, pending := range b.pending {
		if pending.request.ID == response.RequestID {
			index = i
			if err := validateInteractionResponse(pending.request, response); err != nil {
				b.mu.Unlock()
				b.unlockAuthority()
				return err
			}
			break
		}
	}
	if index < 0 {
		for _, id := range b.settled {
			if id == response.RequestID {
				b.mu.Unlock()
				b.unlockAuthority()
				return ErrInteractionSettled
			}
		}
		b.mu.Unlock()
		b.unlockAuthority()
		return ErrInteractionNotFound
	}
	pending := b.pending[index]
	b.pending = append(b.pending[:index], b.pending[index+1:]...)
	b.rememberSettledLocked(pending.request.ID)
	b.mu.Unlock()
	settlement := interactionSettlement{response: cloneInteractionResponse(response), reason: responseReason(response)}
	b.emit(NewEvent{SessionID: pending.request.SessionID, TurnID: pending.request.RunID, RunID: pending.request.RunID, Kind: EventInteractionResolved, InteractionID: pending.request.ID, InteractionResolution: settlement.reason})
	b.unlockAuthority()
	pending.result <- settlement
	return nil
}

func (b *interactionBroker) settle(requestID string, response InteractionResponse, reason string) (interactionSettlement, bool) {
	b.lockAuthority()
	b.mu.Lock()
	index := -1
	for i, pending := range b.pending {
		if pending.request.ID == requestID {
			index = i
			break
		}
	}
	if index < 0 {
		b.mu.Unlock()
		b.unlockAuthority()
		return interactionSettlement{}, false
	}
	pending := b.pending[index]
	b.pending = append(b.pending[:index], b.pending[index+1:]...)
	b.rememberSettledLocked(pending.request.ID)
	b.mu.Unlock()
	settlement := interactionSettlement{response: cloneInteractionResponse(response), reason: reason}
	b.emit(NewEvent{SessionID: pending.request.SessionID, TurnID: pending.request.RunID, RunID: pending.request.RunID, Kind: EventInteractionResolved, InteractionID: pending.request.ID, InteractionResolution: reason})
	b.unlockAuthority()
	pending.result <- settlement
	return settlement, true
}

// cancelAll is called while the runtime authority lock is held.
func (b *interactionBroker) cancelAll(reason string) {
	b.mu.Lock()
	pending := b.pending
	b.pending = nil
	b.mu.Unlock()
	for _, item := range pending {
		response := InteractionResponse{RequestID: item.request.ID, Cancelled: true}
		b.mu.Lock()
		b.rememberSettledLocked(item.request.ID)
		b.mu.Unlock()
		b.emit(NewEvent{SessionID: item.request.SessionID, TurnID: item.request.RunID, RunID: item.request.RunID, Kind: EventInteractionResolved, InteractionID: item.request.ID, InteractionResolution: reason})
		item.result <- interactionSettlement{response: response, reason: reason}
	}
}

func (b *interactionBroker) lockAuthority() {
	if b.authority != nil {
		b.authority.Lock()
	}
}
func (b *interactionBroker) unlockAuthority() {
	if b.authority != nil {
		b.authority.Unlock()
	}
}
func (b *interactionBroker) rememberSettledLocked(id string) {
	b.settled = append(b.settled, id)
	if len(b.settled) > 64 {
		b.settled = append([]string(nil), b.settled[len(b.settled)-64:]...)
	}
}

func (b *interactionBroker) emit(event NewEvent) {
	if b.publish != nil {
		b.publish(event)
	}
}

func ptrInteractionRequest(request InteractionRequest) *InteractionRequest {
	copy := cloneInteractionRequest(request)
	return &copy
}

func cloneInteractionRequest(request InteractionRequest) InteractionRequest {
	request.Options = append([]InteractionOption(nil), request.Options...)
	request.Questions = append([]InteractionQuestion(nil), request.Questions...)
	for i := range request.Questions {
		request.Questions[i].Options = append([]InteractionOption(nil), request.Questions[i].Options...)
	}
	return request
}

func cloneInteractionResponse(response InteractionResponse) InteractionResponse {
	if response.Answers != nil {
		answers := make(map[string]InteractionAnswer, len(response.Answers))
		for id, answer := range response.Answers {
			answer.OptionIDs = append([]string(nil), answer.OptionIDs...)
			answers[id] = answer
		}
		response.Answers = answers
	}
	return response
}

func responseReason(response InteractionResponse) string {
	if response.Cancelled {
		return "user_cancelled"
	}
	if response.Confirmed != nil && !*response.Confirmed {
		return "negative_confirmation"
	}
	return "answered"
}

func validateInteractionRequest(request InteractionRequest) error {
	if !identifier.Valid(request.ID, "interaction_") || request.SessionID == "" || request.RunID == "" || request.ToolCallID == "" || request.CreatedAt.IsZero() {
		return fmt.Errorf("%w: interaction identities and creation time are required", ErrInvalidInput)
	}
	if !boundedInteractionText(request.Title, maxInteractionTitleBytes, true) || !boundedInteractionText(request.Detail, maxInteractionDetailBytes, false) {
		return fmt.Errorf("%w: interaction title or detail is invalid", ErrInvalidInput)
	}
	if len(request.Options) > maxInteractionOptions || len(request.Questions) > maxGuidedQuestions {
		return fmt.Errorf("%w: interaction has too many options or questions", ErrInvalidInput)
	}
	if request.Kind != InteractionConfirm && request.Kind != InteractionInput && request.Kind != InteractionSelect && request.Kind != InteractionGuided {
		return fmt.Errorf("%w: unsupported interaction kind %q", ErrInvalidInput, request.Kind)
	}
	if request.Kind == InteractionSelect && len(request.Options) == 0 || request.Kind != InteractionSelect && len(request.Options) != 0 {
		return fmt.Errorf("%w: interaction options do not match kind", ErrInvalidInput)
	}
	if request.Kind == InteractionGuided && len(request.Questions) == 0 || request.Kind != InteractionGuided && len(request.Questions) != 0 {
		return fmt.Errorf("%w: interaction questions do not match kind", ErrInvalidInput)
	}
	if err := validateInteractionOptions(request.Options); err != nil {
		return err
	}
	seenQuestions := map[string]struct{}{}
	for _, question := range request.Questions {
		if strings.TrimSpace(question.ID) == "" || len(question.ID) > 128 || !boundedInteractionText(question.Prompt, maxInteractionTitleBytes, true) || !boundedInteractionText(question.Detail, maxInteractionDetailBytes, false) {
			return fmt.Errorf("%w: guided question is invalid", ErrInvalidInput)
		}
		if _, exists := seenQuestions[question.ID]; exists {
			return fmt.Errorf("%w: duplicate guided question %q", ErrInvalidInput, question.ID)
		}
		seenQuestions[question.ID] = struct{}{}
		switch question.Kind {
		case InteractionQuestionText, InteractionQuestionBoolean:
			if len(question.Options) != 0 {
				return fmt.Errorf("%w: question %q cannot have options", ErrInvalidInput, question.ID)
			}
		case InteractionQuestionSelect, InteractionQuestionMultiselect:
			if len(question.Options) == 0 {
				return fmt.Errorf("%w: question %q requires options", ErrInvalidInput, question.ID)
			}
		default:
			return fmt.Errorf("%w: question %q has invalid kind", ErrInvalidInput, question.ID)
		}
		if err := validateInteractionOptions(question.Options); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > maxInteractionBytes {
		return fmt.Errorf("%w: interaction exceeds size limit", ErrInvalidInput)
	}
	return nil
}

func validateInteractionOptions(options []InteractionOption) error {
	ids, values := map[string]struct{}{}, map[string]struct{}{}
	for _, option := range options {
		if !identifier.Valid(option.ID, "option_") || !boundedInteractionText(option.Label, maxInteractionTitleBytes, true) || !boundedInteractionText(option.Value, maxInteractionAnswerBytes, true) || !boundedInteractionText(option.Detail, maxInteractionDetailBytes, false) {
			return fmt.Errorf("%w: interaction option is invalid", ErrInvalidInput)
		}
		if _, found := ids[option.ID]; found {
			return fmt.Errorf("%w: duplicate option id", ErrInvalidInput)
		}
		if _, found := values[option.Value]; found {
			return fmt.Errorf("%w: duplicate option value %q", ErrInvalidInput, option.Value)
		}
		ids[option.ID], values[option.Value] = struct{}{}, struct{}{}
	}
	return nil
}

func validateInteractionResponse(request InteractionRequest, response InteractionResponse) error {
	if response.RequestID != request.ID {
		return fmt.Errorf("%w: interaction identity mismatch", ErrInvalidInput)
	}
	if response.Cancelled {
		if response.Confirmed != nil || response.Value != nil || response.SelectedOptionID != "" || len(response.Answers) != 0 {
			return fmt.Errorf("%w: cancelled interaction carries an answer", ErrInvalidInput)
		}
		return nil
	}
	switch request.Kind {
	case InteractionConfirm:
		if response.Confirmed == nil || response.Value != nil || response.SelectedOptionID != "" || len(response.Answers) != 0 {
			return fmt.Errorf("%w: confirmation answer is invalid", ErrInvalidInput)
		}
	case InteractionInput:
		if response.Value == nil || !boundedInteractionText(*response.Value, maxInteractionAnswerBytes, true) || response.Confirmed != nil || response.SelectedOptionID != "" || len(response.Answers) != 0 {
			return fmt.Errorf("%w: input answer is invalid", ErrInvalidInput)
		}
	case InteractionSelect:
		if response.SelectedOptionID == "" || response.Confirmed != nil || response.Value != nil || len(response.Answers) != 0 || findInteractionOption(request.Options, response.SelectedOptionID) == nil {
			return fmt.Errorf("%w: selection answer is invalid", ErrInvalidInput)
		}
	case InteractionGuided:
		if response.Confirmed != nil || response.Value != nil || response.SelectedOptionID != "" || len(response.Answers) > len(request.Questions) {
			return fmt.Errorf("%w: guided answer is invalid", ErrInvalidInput)
		}
		for _, question := range request.Questions {
			answer, found := response.Answers[question.ID]
			if !found {
				return fmt.Errorf("%w: question %q is unanswered", ErrInvalidInput, question.ID)
			}
			if err := validateGuidedAnswer(question, answer); err != nil {
				return err
			}
		}
		for id := range response.Answers {
			found := false
			for _, question := range request.Questions {
				if question.ID == id {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%w: unknown question %q", ErrInvalidInput, id)
			}
		}
	}
	return nil
}

func validateGuidedAnswer(question InteractionQuestion, answer InteractionAnswer) error {
	if answer.Skipped {
		if question.Required || answer.Text != nil || answer.Boolean != nil || len(answer.OptionIDs) != 0 {
			return fmt.Errorf("%w: question %q cannot be skipped", ErrInvalidInput, question.ID)
		}
		return nil
	}
	switch question.Kind {
	case InteractionQuestionText:
		if answer.Text == nil || !boundedInteractionText(*answer.Text, maxInteractionAnswerBytes, true) || answer.Boolean != nil || len(answer.OptionIDs) != 0 {
			return fmt.Errorf("%w: text answer for %q is invalid", ErrInvalidInput, question.ID)
		}
	case InteractionQuestionBoolean:
		if answer.Boolean == nil || answer.Text != nil || len(answer.OptionIDs) != 0 {
			return fmt.Errorf("%w: boolean answer for %q is invalid", ErrInvalidInput, question.ID)
		}
	case InteractionQuestionSelect:
		if answer.Text != nil || answer.Boolean != nil || len(answer.OptionIDs) != 1 || findInteractionOption(question.Options, answer.OptionIDs[0]) == nil {
			return fmt.Errorf("%w: selection answer for %q is invalid", ErrInvalidInput, question.ID)
		}
	case InteractionQuestionMultiselect:
		if answer.Text != nil || answer.Boolean != nil || len(answer.OptionIDs) == 0 {
			return fmt.Errorf("%w: multiselect answer for %q is invalid", ErrInvalidInput, question.ID)
		}
		seen := map[string]struct{}{}
		for _, id := range answer.OptionIDs {
			if findInteractionOption(question.Options, id) == nil {
				return fmt.Errorf("%w: unknown option for %q", ErrInvalidInput, question.ID)
			}
			if _, ok := seen[id]; ok {
				return fmt.Errorf("%w: duplicate option for %q", ErrInvalidInput, question.ID)
			}
			seen[id] = struct{}{}
		}
	}
	return nil
}

func findInteractionOption(options []InteractionOption, id string) *InteractionOption {
	for i := range options {
		if options[i].ID == id {
			return &options[i]
		}
	}
	return nil
}

func boundedInteractionText(value string, limit int, required bool) bool {
	if !utf8.ValidString(value) || len(value) > limit {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return !required || strings.TrimSpace(value) != ""
}

func newInteractionID(prefix string) (string, error) { return identifier.New(prefix) }

// RespondInteraction validates and atomically settles one pending request.
func (m *Manager) RespondInteraction(ctx context.Context, sessionID string, response InteractionResponse) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	if err := ctx.Err(); err != nil {
		return err
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return err
	}
	return loaded.interactions.respond(response)
}

func interactionTools(sessionID string, broker *interactionBroker) []droids.AnyTool {
	return []droids.AnyTool{
		droids.MustTool(confirmInteractionTool(sessionID, broker)),
		droids.MustTool(inputInteractionTool(sessionID, broker)),
		droids.MustTool(selectInteractionTool(sessionID, broker)),
		droids.MustTool(guidedInteractionTool(sessionID, broker)),
	}
}

const interactionPromptGuidance = "\n\nWhen user input is required, use confirm_from_user for one boolean decision, input_from_user for one short freeform value, select_from_user for one bounded choice, and guided_questions for multiple missing inputs. Do not ask these as ordinary chat questions when a structured interaction tool fits."

type basicInteractionArgs struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}
type selectInteractionArgs struct {
	Title   string                   `json:"title"`
	Detail  string                   `json:"detail,omitempty"`
	Options []modelInteractionOption `json:"options"`
}
type modelInteractionOption struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Detail string `json:"detail,omitempty"`
}
type guidedInteractionArgs struct {
	Title     string                     `json:"title"`
	Detail    string                     `json:"detail,omitempty"`
	Questions []modelInteractionQuestion `json:"questions"`
}
type modelInteractionQuestion struct {
	ID       string                   `json:"id"`
	Prompt   string                   `json:"prompt"`
	Detail   string                   `json:"detail,omitempty"`
	Kind     InteractionQuestionKind  `json:"kind"`
	Required *bool                    `json:"required,omitempty"`
	Options  []modelInteractionOption `json:"options,omitempty"`
}

func basicInteractionSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string", "minLength": 1, "maxLength": maxInteractionTitleBytes}, "detail": map[string]any{"type": "string", "maxLength": maxInteractionDetailBytes}}, "required": []string{"title"}, "additionalProperties": false}
}

func confirmInteractionTool(sessionID string, broker *interactionBroker) droids.Tool[basicInteractionArgs] {
	return droids.Tool[basicInteractionArgs]{Name: ConfirmInteractionToolName, Description: "Request one boolean decision from the user.", Parameters: basicInteractionSchema(), Mode: droids.ModeSequential, Execute: func(ctx context.Context, call droids.ToolContext, args basicInteractionArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
		settlement, err := executeInteraction(ctx, sessionID, broker, call, InteractionRequest{Kind: InteractionConfirm, Title: args.Title, Detail: args.Detail})
		if err != nil {
			return droids.ToolResult{}, err
		}
		confirmed := !settlement.response.Cancelled && settlement.response.Confirmed != nil && *settlement.response.Confirmed
		return interactionToolResult(map[string]any{"confirmed": confirmed}, settlement.reason)
	}}
}

func inputInteractionTool(sessionID string, broker *interactionBroker) droids.Tool[basicInteractionArgs] {
	return droids.Tool[basicInteractionArgs]{Name: InputInteractionToolName, Description: "Request one short freeform value from the user.", Parameters: basicInteractionSchema(), Mode: droids.ModeSequential, Execute: func(ctx context.Context, call droids.ToolContext, args basicInteractionArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
		settlement, err := executeInteraction(ctx, sessionID, broker, call, InteractionRequest{Kind: InteractionInput, Title: args.Title, Detail: args.Detail})
		if err != nil {
			return droids.ToolResult{}, err
		}
		return interactionToolResult(map[string]any{"value": settlement.response.Value, "cancelled": settlement.response.Cancelled}, settlement.reason)
	}}
}

func selectInteractionTool(sessionID string, broker *interactionBroker) droids.Tool[selectInteractionArgs] {
	params := basicInteractionSchema()
	props := params["properties"].(map[string]any)
	props["options"] = modelOptionsSchema()
	params["required"] = []string{"title", "options"}
	return droids.Tool[selectInteractionArgs]{Name: SelectInteractionToolName, Description: "Request one choice from a bounded list.", Parameters: params, Mode: droids.ModeSequential, Execute: func(ctx context.Context, call droids.ToolContext, args selectInteractionArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
		options, err := makeInteractionOptions(args.Options)
		if err != nil {
			return droids.ToolResult{}, err
		}
		request := InteractionRequest{Kind: InteractionSelect, Title: args.Title, Detail: args.Detail, Options: options}
		settlement, err := executeInteraction(ctx, sessionID, broker, call, request)
		if err != nil {
			return droids.ToolResult{}, err
		}
		var value, label any
		if !settlement.response.Cancelled {
			option := findInteractionOption(options, settlement.response.SelectedOptionID)
			if option != nil {
				value, label = option.Value, option.Label
			}
		}
		return interactionToolResult(map[string]any{"value": value, "label": label, "cancelled": settlement.response.Cancelled}, settlement.reason)
	}}
}

func guidedInteractionTool(sessionID string, broker *interactionBroker) droids.Tool[guidedInteractionArgs] {
	params := basicInteractionSchema()
	props := params["properties"].(map[string]any)
	props["questions"] = guidedQuestionsSchema()
	params["required"] = []string{"title", "questions"}
	return droids.Tool[guidedInteractionArgs]{Name: GuidedInteractionToolName, Description: "Request a structured questionnaire whose questions are presented one at a time.", Parameters: params, Mode: droids.ModeSequential, Execute: func(ctx context.Context, call droids.ToolContext, args guidedInteractionArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
		questions, err := makeInteractionQuestions(args.Questions)
		if err != nil {
			return droids.ToolResult{}, err
		}
		settlement, err := executeInteraction(ctx, sessionID, broker, call, InteractionRequest{Kind: InteractionGuided, Title: args.Title, Detail: args.Detail, Questions: questions})
		if err != nil {
			return droids.ToolResult{}, err
		}
		answers := map[string]any{}
		answered := 0
		if !settlement.response.Cancelled {
			for _, question := range questions {
				answer, ok := settlement.response.Answers[question.ID]
				if !ok || answer.Skipped {
					continue
				}
				answered++
				answers[question.ID] = modelGuidedAnswer(question, answer)
			}
		}
		result := map[string]any{"cancelled": settlement.response.Cancelled, "answers": answers, "answeredCount": answered, "totalQuestions": len(questions)}
		if !settlement.response.Cancelled {
			result["completed"] = true
		}
		return interactionToolResult(result, settlement.reason)
	}}
}

func executeInteraction(ctx context.Context, sessionID string, broker *interactionBroker, call droids.ToolContext, request InteractionRequest) (interactionSettlement, error) {
	id, err := newInteractionID("interaction_")
	if err != nil {
		return interactionSettlement{}, err
	}
	request.ID, request.SessionID, request.RunID, request.ToolCallID, request.CreatedAt = id, sessionID, string(call.TurnID), string(call.ToolCallID), time.Now().UTC()
	return broker.request(ctx, request)
}

func interactionToolResult(value any, reason string) (droids.ToolResult, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return droids.ToolResult{}, err
	}
	details, err := droids.EncodeDetails(map[string]any{"resolution": reason})
	if err != nil {
		return droids.ToolResult{}, err
	}
	result := droids.ToolText(string(raw))
	result.Details = details
	return result, nil
}

func makeInteractionOptions(options []modelInteractionOption) ([]InteractionOption, error) {
	if len(options) == 0 || len(options) > maxInteractionOptions {
		return nil, fmt.Errorf("%w: options count is invalid", ErrInvalidInput)
	}
	result := make([]InteractionOption, 0, len(options))
	for _, option := range options {
		id, err := newInteractionID("option_")
		if err != nil {
			return nil, err
		}
		result = append(result, InteractionOption{ID: id, Label: option.Label, Value: option.Value, Detail: option.Detail})
	}
	return result, nil
}

func makeInteractionQuestions(input []modelInteractionQuestion) ([]InteractionQuestion, error) {
	if len(input) == 0 || len(input) > maxGuidedQuestions {
		return nil, fmt.Errorf("%w: questions count is invalid", ErrInvalidInput)
	}
	result := make([]InteractionQuestion, 0, len(input))
	for _, item := range input {
		required := true
		if item.Required != nil {
			required = *item.Required
		}
		options := []InteractionOption(nil)
		var err error
		if len(item.Options) > 0 {
			options, err = makeInteractionOptions(item.Options)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, InteractionQuestion{ID: item.ID, Prompt: item.Prompt, Detail: item.Detail, Kind: item.Kind, Required: required, Options: options})
	}
	return result, nil
}

func modelGuidedAnswer(question InteractionQuestion, answer InteractionAnswer) any {
	if answer.Text != nil {
		return *answer.Text
	}
	if answer.Boolean != nil {
		return *answer.Boolean
	}
	values := make([]string, 0, len(answer.OptionIDs))
	for _, id := range answer.OptionIDs {
		if option := findInteractionOption(question.Options, id); option != nil {
			values = append(values, option.Value)
		}
	}
	if question.Kind == InteractionQuestionSelect && len(values) == 1 {
		return values[0]
	}
	return values
}

func modelOptionsSchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "maxItems": maxInteractionOptions, "items": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "minLength": 1, "maxLength": maxInteractionTitleBytes}, "value": map[string]any{"type": "string", "minLength": 1, "maxLength": maxInteractionAnswerBytes}, "detail": map[string]any{"type": "string", "maxLength": maxInteractionDetailBytes}}, "required": []string{"label", "value"}, "additionalProperties": false}}
}
func guidedQuestionsSchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "maxItems": maxGuidedQuestions, "items": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128}, "prompt": map[string]any{"type": "string", "minLength": 1, "maxLength": maxInteractionTitleBytes}, "detail": map[string]any{"type": "string", "maxLength": maxInteractionDetailBytes}, "kind": map[string]any{"type": "string", "enum": []string{"text", "select", "multiselect", "boolean"}}, "required": map[string]any{"type": "boolean"}, "options": modelOptionsSchema()}, "required": []string{"id", "prompt", "kind"}, "additionalProperties": false}}
}
