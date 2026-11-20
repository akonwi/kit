package session

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/akonwi/kit/droids"
)

// PluginInteractionOption exposes choice metadata, not the plugin's opaque value.
type PluginInteractionOption struct{ Label, Detail string }

// PluginInteractionInput is the session-owned port for a plugin's user request.
type PluginInteractionInput struct {
	Owner                                                               PluginInteractionOwner
	Kind                                                                InteractionKind
	Title, Detail, ConfirmLabel, CancelLabel, Placeholder, InitialValue string
	DefaultValue                                                        *bool
	Filterable                                                          bool
	Options                                                             []PluginInteractionOption
}

// PluginInteractionResult returns selected intent to the plugin adapter.
type PluginInteractionResult struct {
	Cancelled, Confirmed bool
	Text                 string
	OptionIndex          int
}

// PluginInteractionHost installs a session-owned observer before starting a host.
// available fences the plugin generation: once it reports false, the dialog
// is hidden and refuses answers.
type PluginInteractionHost interface {
	SetInteractionObserver(func(context.Context, PluginInteractionInput, func() bool) (PluginInteractionResult, error))
}

var pluginInteractionDomain = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
var pluginInteractionInstance = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)

func validPluginInteractionOwner(owner PluginInteractionOwner) bool {
	return pluginInteractionDomain.MatchString(owner.PluginID) && pluginInteractionInstance.MatchString(owner.Instance)
}

func validateInteractionPresentation(request InteractionRequest) error {
	if !boundedInteractionText(request.ConfirmLabel, 128, false) || !boundedInteractionText(request.CancelLabel, 128, false) || !boundedInteractionText(request.Placeholder, maxInteractionTitleBytes, false) || !boundedInteractionText(request.InitialValue, maxInteractionAnswerBytes, false) {
		return fmt.Errorf("%w: invalid interaction presentation", ErrInvalidInput)
	}
	if request.Kind != InteractionConfirm && (request.ConfirmLabel != "" || request.CancelLabel != "" || request.DefaultValue != nil) {
		return fmt.Errorf("%w: confirm presentation on other interaction", ErrInvalidInput)
	}
	if request.Kind != InteractionInput && request.InitialValue != "" {
		return fmt.Errorf("%w: initial value on other interaction", ErrInvalidInput)
	}
	if request.Kind != InteractionSelect && request.Filterable != nil {
		return fmt.Errorf("%w: filtering on other interaction", ErrInvalidInput)
	}
	if request.Kind != InteractionSelect && request.Kind != InteractionInput && request.Placeholder != "" {
		return fmt.Errorf("%w: placeholder on other interaction", ErrInvalidInput)
	}
	return nil
}

// askPlugin asks the user a plugin's question. A dialog the plugin opens for
// a before-execute check of the session's own tool call belongs to that
// call's turn: steering declines it and the turn ending cancels it. Any other
// dialog belongs to no turn. available fences the plugin generation: once it
// reports false, the dialog is hidden and refuses answers.
func (s *sessionInteractions) askPlugin(ctx context.Context, input PluginInteractionInput, available func() bool) (PluginInteractionResult, error) {
	id, err := newInteractionID("interaction_")
	if err != nil {
		return PluginInteractionResult{}, err
	}
	owner := input.Owner
	request := InteractionRequest{ID: id, SessionID: s.sessionID, Plugin: &owner, Kind: input.Kind, Title: input.Title, Detail: input.Detail, ConfirmLabel: input.ConfirmLabel, CancelLabel: input.CancelLabel, DefaultValue: input.DefaultValue, Placeholder: input.Placeholder, InitialValue: input.InitialValue, CreatedAt: time.Now().UTC()}
	if input.Kind == InteractionSelect {
		filterable := input.Filterable
		request.Filterable = &filterable
	}
	if len(input.Options) > maxInteractionOptions {
		return PluginInteractionResult{}, ErrInteractionCapacity
	}
	for index, option := range input.Options {
		id, err := newInteractionID("option_")
		if err != nil {
			return PluginInteractionResult{}, err
		}
		request.Options = append(request.Options, InteractionOption{ID: id, Label: option.Label, Detail: option.Detail, Value: strconv.Itoa(index)})
	}
	if err := validateInteractionRequest(request); err != nil {
		return PluginInteractionResult{}, err
	}
	release := s.fence(owner, available)
	defer release()
	if available != nil && !available() {
		return PluginInteractionResult{}, ErrClosed
	}
	options := droids.AskOptions{Metadata: pluginMetadata(owner)}
	if call, ok := hookCall(ctx); ok && call.ConversationID == droids.ConversationID(s.sessionID) {
		options.Call = &call
	}
	settlement, err := s.ask(ctx, droidQuestion(request), options)
	if err != nil {
		return PluginInteractionResult{}, err
	}
	if ctx.Err() != nil {
		return PluginInteractionResult{}, ctx.Err()
	}
	switch settlement.Status {
	case droids.InteractionDeclined:
		return PluginInteractionResult{Cancelled: true}, nil
	case droids.InteractionAnswered:
	default:
		return PluginInteractionResult{}, ErrClosed
	}
	answer := settlement.Answer
	switch request.Kind {
	case InteractionConfirm:
		if answer.Confirmed != nil {
			return PluginInteractionResult{Confirmed: *answer.Confirmed}, nil
		}
	case InteractionInput:
		if answer.Value != nil {
			return PluginInteractionResult{Text: *answer.Value}, nil
		}
	case InteractionSelect:
		for index, option := range request.Options {
			if option.ID == answer.OptionID {
				return PluginInteractionResult{OptionIndex: index}, nil
			}
		}
	}
	return PluginInteractionResult{}, errors.New("invalid plugin interaction settlement")
}

// droidQuestion is the droid question for a validated request.
func droidQuestion(request InteractionRequest) droids.Question {
	question := droids.Question{
		Kind: droids.QuestionKind(request.Kind), Title: request.Title, Detail: request.Detail,
		ConfirmLabel: request.ConfirmLabel, CancelLabel: request.CancelLabel, Default: request.DefaultValue,
		Placeholder: request.Placeholder, InitialValue: request.InitialValue, Filterable: request.Filterable,
		URL: request.URL, RedirectURI: request.RedirectURI,
	}
	for _, option := range request.Options {
		question.Options = append(question.Options, droids.Option{ID: option.ID, Label: option.Label, Value: option.Value, Detail: option.Detail})
	}
	return question
}
