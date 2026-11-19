package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/droids"
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

// InteractionKind identifies one server-owned user interaction.
type InteractionKind string

const (
	InteractionConfirm InteractionKind = "confirm"
	InteractionInput   InteractionKind = "input"
	InteractionSelect  InteractionKind = "select"
	InteractionGuided  InteractionKind = "guided"
	// InteractionAuthorize asks the user to open URL in a browser, approve
	// access, and answer with the URL the browser was redirected to, or the
	// code the page showed. The answering client may receive the redirect on
	// RedirectURI itself.
	InteractionAuthorize InteractionKind = "authorize"
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

// PluginInteractionOwner identifies one session-local plugin generation.
type PluginInteractionOwner struct {
	PluginID string
	Instance string
}

// InteractionRequest is the canonical pending request projected to clients.
type InteractionRequest struct {
	Plugin       *PluginInteractionOwner
	ConfirmLabel string
	CancelLabel  string
	DefaultValue *bool
	Placeholder  string
	InitialValue string
	Filterable   *bool
	ID           string
	SessionID    string
	RunID        string
	ToolCallID   string
	Kind         InteractionKind
	Title        string
	Detail       string
	Options      []InteractionOption
	Questions    []InteractionQuestion
	// URL and RedirectURI belong to authorize requests only.
	URL         string
	RedirectURI string
	CreatedAt   time.Time
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
	ctx       context.Context
	available func() bool
	request   InteractionRequest
	result    chan interactionSettlement
}

type interactionBroker struct {
	closed    bool
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
		if pending.active() {
			requests = append(requests, cloneInteractionRequest(pending.request))
		}
	}
	return requests
}

func (b *interactionBroker) request(ctx context.Context, request InteractionRequest) (interactionSettlement, error) {
	return b.requestOwned(ctx, request, nil)
}

func (b *interactionBroker) requestOwned(ctx context.Context, request InteractionRequest, available func() bool) (interactionSettlement, error) {
	if err := validateInteractionRequest(request); err != nil {
		return interactionSettlement{}, err
	}
	pending := &pendingInteraction{ctx: ctx, available: available, request: cloneInteractionRequest(request), result: make(chan interactionSettlement, 1)}
	if err := b.lockRequestAuthority(ctx); err != nil {
		return interactionSettlement{}, err
	}
	b.mu.Lock()
	if b.closed || !pending.active() {
		b.mu.Unlock()
		b.unlockAuthority()
		if ctx.Err() != nil {
			return interactionSettlement{}, ctx.Err()
		}
		return interactionSettlement{}, ErrClosed
	}
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
		reason := "run_abort"
		// A plugin that went away, or a request that waited too long, ends
		// without the run ending.
		if request.Plugin != nil || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "unavailable"
		}
		return b.cancelPending(pending, reason), nil
	}
}

func (b *interactionBroker) respond(response InteractionResponse) error {
	b.lockAuthority()
	b.mu.Lock()
	index := -1
	for i, pending := range b.pending {
		if pending.request.ID == response.RequestID {
			if !pending.active() {
				b.mu.Unlock()
				b.unlockAuthority()
				return ErrInteractionSettled
			}
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

func (b *interactionBroker) settleUnderAuthority(requestID string, response InteractionResponse, reason string) (interactionSettlement, bool) {
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
		return interactionSettlement{}, false
	}
	pending := b.pending[index]
	b.pending = append(b.pending[:index], b.pending[index+1:]...)
	b.rememberSettledLocked(pending.request.ID)
	b.mu.Unlock()
	settlement := interactionSettlement{response: cloneInteractionResponse(response), reason: reason}
	b.emit(NewEvent{SessionID: pending.request.SessionID, TurnID: pending.request.RunID, RunID: pending.request.RunID, Kind: EventInteractionResolved, InteractionID: pending.request.ID, InteractionResolution: reason})
	pending.result <- settlement
	return settlement, true
}

// cancelAll is called while the runtime authority lock is held.
func (b *interactionBroker) cancelAll(reason string) {
	b.mu.Lock()
	b.closed = true
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

// remember records that the droid settled interaction id, so a late
// response reports it as settled.
func (b *interactionBroker) remember(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rememberSettledLocked(id)
}

// notPending reports why id cannot be answered: it was settled recently, or it
// was never pending.
func (b *interactionBroker) notPending(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if slices.Contains(b.settled, id) {
		return ErrInteractionSettled
	}
	return ErrInteractionNotFound
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
	if request.Plugin != nil {
		owner := *request.Plugin
		request.Plugin = &owner
	}
	if request.DefaultValue != nil {
		value := *request.DefaultValue
		request.DefaultValue = &value
	}
	if request.Filterable != nil {
		value := *request.Filterable
		request.Filterable = &value
	}
	request.Options = append([]InteractionOption(nil), request.Options...)
	request.Questions = append([]InteractionQuestion(nil), request.Questions...)
	for i := range request.Questions {
		request.Questions[i].Options = append([]InteractionOption(nil), request.Questions[i].Options...)
	}
	return request
}

func cloneInteractionResponse(response InteractionResponse) InteractionResponse {
	if response.Confirmed != nil {
		value := *response.Confirmed
		response.Confirmed = &value
	}
	if response.Value != nil {
		value := *response.Value
		response.Value = &value
	}
	if response.Answers != nil {
		answers := make(map[string]InteractionAnswer, len(response.Answers))
		for id, answer := range response.Answers {
			if answer.Text != nil {
				value := *answer.Text
				answer.Text = &value
			}
			if answer.Boolean != nil {
				value := *answer.Boolean
				answer.Boolean = &value
			}
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
	if !identifier.Valid(request.ID, "interaction_") || request.SessionID == "" || request.CreatedAt.IsZero() {
		return fmt.Errorf("%w: interaction identities and creation time are required", ErrInvalidInput)
	}
	if request.Plugin == nil {
		if request.RunID == "" || request.ToolCallID == "" {
			return fmt.Errorf("%w: interaction model ownership is required", ErrInvalidInput)
		}
	} else if request.RunID != "" || request.ToolCallID != "" || !validPluginInteractionOwner(*request.Plugin) || request.Kind == InteractionGuided {
		return fmt.Errorf("%w: invalid plugin interaction ownership", ErrInvalidInput)
	}
	if err := validateInteractionPresentation(request); err != nil {
		return err
	}
	if !boundedInteractionText(request.Title, maxInteractionTitleBytes, true) || !boundedInteractionText(request.Detail, maxInteractionDetailBytes, false) {
		return fmt.Errorf("%w: interaction title or detail is invalid", ErrInvalidInput)
	}
	if len(request.Options) > maxInteractionOptions || len(request.Questions) > maxGuidedQuestions {
		return fmt.Errorf("%w: interaction has too many options or questions", ErrInvalidInput)
	}
	if request.Kind != InteractionConfirm && request.Kind != InteractionInput && request.Kind != InteractionSelect && request.Kind != InteractionGuided && request.Kind != InteractionAuthorize {
		return fmt.Errorf("%w: unsupported interaction kind %q", ErrInvalidInput, request.Kind)
	}
	if request.Kind == InteractionAuthorize {
		if request.Plugin != nil || !validAuthorizationURL(request.URL) || !validRedirectURI(request.RedirectURI) {
			return fmt.Errorf("%w: authorization request is invalid", ErrInvalidInput)
		}
	} else if request.URL != "" || request.RedirectURI != "" {
		return fmt.Errorf("%w: authorization fields on other interaction", ErrInvalidInput)
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
	case InteractionInput, InteractionAuthorize:
		if response.Value == nil || !boundedInteractionText(*response.Value, maxInteractionAnswerBytes, request.Plugin == nil) || response.Confirmed != nil || response.SelectedOptionID != "" || len(response.Answers) != 0 {
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
	if err := loaded.interactions.respond(response); !errors.Is(err, ErrInteractionNotFound) {
		return err
	}
	return m.respondDroidInteraction(ctx, loaded, sessionID, response)
}

// interactionTools returns the tools with which the model asks the user. The
// droid asks their questions before they run, so the questions survive
// restarts.
func interactionTools() []droids.AnyTool {
	return []droids.AnyTool{
		droids.ConfirmTool(ConfirmInteractionToolName),
		droids.InputTool(InputInteractionToolName),
		droids.SelectTool(SelectInteractionToolName),
		droids.GuidedTool(GuidedInteractionToolName),
	}
}

const interactionPromptGuidance = "\n\nWhen user input is required, use confirm_from_user for one boolean decision, input_from_user for one short freeform value, select_from_user for one bounded choice, and guided_questions for multiple missing inputs. Do not ask these as ordinary chat questions when a structured interaction tool fits."

func executeInteraction(ctx context.Context, sessionID string, broker *interactionBroker, call droids.ToolContext, request InteractionRequest) (interactionSettlement, error) {
	id, err := newInteractionID("interaction_")
	if err != nil {
		return interactionSettlement{}, err
	}
	request.ID, request.SessionID, request.RunID, request.ToolCallID, request.CreatedAt = id, sessionID, string(call.TurnID), string(call.ToolCallID), time.Now().UTC()
	return broker.request(ctx, request)
}

func (p *pendingInteraction) active() bool {
	return (p.ctx == nil || p.ctx.Err() == nil) && (p.available == nil || p.available())
}

// A plugin handler must remain cancellable while runtime shutdown owns authority.
// Keep the short authority mutex; bounded polling avoids spawning lock waiters
// that can outlive the RPC or deadlock plugin cleanup under runtime shutdown.
func (b *interactionBroker) lockRequestAuthority(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.authority == nil {
		return nil
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if b.authority.TryLock() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (b *interactionBroker) cancelPending(pending *pendingInteraction, reason string) interactionSettlement {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case result := <-pending.result:
			return result
		default:
		}
		if b.authority == nil || b.authority.TryLock() {
			result, won := b.settleUnderAuthority(pending.request.ID, InteractionResponse{RequestID: pending.request.ID, Cancelled: true}, reason)
			b.unlockAuthority()
			if won {
				return result
			}
			return <-pending.result
		}
		select {
		case result := <-pending.result:
			return result
		case <-ticker.C:
		}
	}
}
