package session

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/akonwi/kit/droids"
)

// droid_interactions.go — the session droid's interactions, projected into
// Kit's interaction requests, events, and responses. The droid owns questions
// its tools raise; Kit's broker still owns those that Kit's own code asks.

// projectDroidInteraction projects a droid interaction as Kit's canonical
// request.
func projectDroidInteraction(sessionID string, interaction droids.Interaction) InteractionRequest {
	question := interaction.Question
	request := InteractionRequest{
		ID: string(interaction.ID), SessionID: sessionID,
		RunID: string(interaction.TurnID), ToolCallID: string(interaction.ToolCallID),
		Kind: InteractionKind(question.Kind), Title: question.Title, Detail: question.Detail,
		ConfirmLabel: question.ConfirmLabel, CancelLabel: question.CancelLabel,
		Placeholder: question.Placeholder, InitialValue: question.InitialValue,
		URL: question.URL, RedirectURI: question.RedirectURI, CreatedAt: interaction.CreatedAt,
	}
	if question.Default != nil {
		value := *question.Default
		request.DefaultValue = &value
	}
	if question.Filterable != nil {
		value := *question.Filterable
		request.Filterable = &value
	}
	request.Options = projectDroidOptions(question.Options)
	for _, guided := range question.Questions {
		request.Questions = append(request.Questions, InteractionQuestion{
			ID: guided.ID, Prompt: guided.Prompt, Detail: guided.Detail,
			Kind: InteractionQuestionKind(guided.Kind), Required: guided.Required,
			Options: projectDroidOptions(guided.Options),
		})
	}
	return request
}

func projectDroidOptions(options []droids.Option) []InteractionOption {
	if len(options) == 0 {
		return nil
	}
	projected := make([]InteractionOption, 0, len(options))
	for _, option := range options {
		projected = append(projected, InteractionOption{ID: option.ID, Label: option.Label, Value: option.Value, Detail: option.Detail})
	}
	return projected
}

// droidInteractionResolution names a settlement in Kit's resolution
// vocabulary: a decline, including one by steering, is user_cancelled, and a
// question its turn's end cancelled is run_abort.
func droidInteractionResolution(settlement droids.Settlement) string {
	switch settlement.Status {
	case droids.InteractionAnswered:
		if confirmed := settlement.Answer.Confirmed; confirmed != nil && !*confirmed {
			return "negative_confirmation"
		}
		return "answered"
	case droids.InteractionDeclined:
		return "user_cancelled"
	case droids.InteractionCancelled:
		return "run_abort"
	default:
		return "unavailable"
	}
}

// droidAnswer converts a validated client response to the droid's answer,
// carrying only the field that answers a question of kind.
func droidAnswer(kind InteractionKind, response InteractionResponse) droids.Answer {
	response = cloneInteractionResponse(response)
	if response.Cancelled {
		return droids.Answer{Declined: true}
	}
	switch kind {
	case InteractionConfirm:
		return droids.Answer{Confirmed: response.Confirmed}
	case InteractionInput, InteractionAuthorize:
		return droids.Answer{Value: response.Value}
	case InteractionSelect:
		return droids.Answer{OptionID: response.SelectedOptionID}
	}
	answers := make(map[string]droids.GuidedAnswer, len(response.Answers))
	for id, guided := range response.Answers {
		answers[id] = droids.GuidedAnswer{Text: guided.Text, Boolean: guided.Boolean, OptionIDs: guided.OptionIDs, Skipped: guided.Skipped}
	}
	return droids.Answer{Answers: answers}
}

// pendingInteractions merges the broker's pending requests with the droid's,
// oldest first.
func pendingInteractions(sessionID string, broker []InteractionRequest, droid []droids.Interaction) []InteractionRequest {
	requests := slices.Clone(broker)
	for _, interaction := range droid {
		requests = append(requests, projectDroidInteraction(sessionID, interaction))
	}
	slices.SortStableFunc(requests, func(a, b InteractionRequest) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return requests
}

// respondDroidInteraction settles one of the droid's pending interactions
// with a client response, validated as Kit validates its own.
func (m *Manager) respondDroidInteraction(ctx context.Context, loaded *runtime, sessionID string, response InteractionResponse) error {
	pending, err := loaded.droid.Interactions(ctx)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(pending, func(interaction droids.Interaction) bool { return string(interaction.ID) == response.RequestID })
	if index < 0 {
		return loaded.interactions.notPending(response.RequestID)
	}
	request := projectDroidInteraction(sessionID, pending[index])
	if err := validateInteractionResponse(request, response); err != nil {
		return err
	}
	err = loaded.droid.Respond(ctx, pending[index].ID, droidAnswer(request.Kind, response))
	switch {
	case err == nil:
		// Remember now rather than when the drain reaches the settlement, so
		// an immediate second response reports the question as settled.
		loaded.interactions.remember(response.RequestID)
		return nil
	case errors.Is(err, droids.ErrInteractionNotFound):
		// It was pending when listed, so something settled it since.
		return ErrInteractionSettled
	case errors.Is(err, droids.ErrInvalidInteraction):
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return err
}
