package session

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
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
// available is checked under broker authority to fence generation revocation.
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

func (b *interactionBroker) requestPlugin(ctx context.Context, sessionID string, input PluginInteractionInput, available func() bool) (PluginInteractionResult, error) {
	id, err := newInteractionID("interaction_")
	if err != nil {
		return PluginInteractionResult{}, err
	}
	owner := input.Owner
	request := InteractionRequest{ID: id, SessionID: sessionID, Plugin: &owner, Kind: input.Kind, Title: input.Title, Detail: input.Detail, ConfirmLabel: input.ConfirmLabel, CancelLabel: input.CancelLabel, DefaultValue: input.DefaultValue, Placeholder: input.Placeholder, InitialValue: input.InitialValue, CreatedAt: time.Now().UTC()}
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
	settlement, err := b.requestOwned(ctx, request, available)
	if err != nil {
		return PluginInteractionResult{}, err
	}
	if ctx.Err() != nil {
		return PluginInteractionResult{}, ctx.Err()
	}
	response := settlement.response
	if response.Cancelled {
		if settlement.reason != "user_cancelled" {
			return PluginInteractionResult{}, ErrClosed
		}
		return PluginInteractionResult{Cancelled: true}, nil
	}
	switch request.Kind {
	case InteractionConfirm:
		return PluginInteractionResult{Confirmed: *response.Confirmed}, nil
	case InteractionInput:
		return PluginInteractionResult{Text: *response.Value}, nil
	case InteractionSelect:
		for index, option := range request.Options {
			if option.ID == response.SelectedOptionID {
				return PluginInteractionResult{OptionIndex: index}, nil
			}
		}
	}
	return PluginInteractionResult{}, errors.New("invalid plugin interaction settlement")
}
