package session

import (
	"errors"
	"testing"
)

func TestGuidedInteractionRequiresExplicitAnswerOrSkipForEveryQuestion(t *testing.T) {
	t.Parallel()
	request := InteractionRequest{
		ID: "interaction_0123456789abcdef0123456789abcdef", Kind: InteractionGuided,
		Questions: []InteractionQuestion{
			{ID: "required", Kind: InteractionQuestionText, Required: true},
			{ID: "optional", Kind: InteractionQuestionText, Required: false},
		},
	}
	text := "answer"
	response := InteractionResponse{RequestID: request.ID, Answers: map[string]InteractionAnswer{
		"required": {Text: &text},
	}}
	if err := validateInteractionResponse(request, response); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing optional answer error = %v", err)
	}
	response.Answers["optional"] = InteractionAnswer{Skipped: true}
	if err := validateInteractionResponse(request, response); err != nil {
		t.Fatalf("explicit optional skip: %v", err)
	}
}

func TestCloneInteractionResponsePreservesGuidedAnswers(t *testing.T) {
	t.Parallel()
	text := "answer"
	original := InteractionResponse{Answers: map[string]InteractionAnswer{
		"text":  {Text: &text},
		"multi": {OptionIDs: []string{"one", "two"}},
	}}
	cloned := cloneInteractionResponse(original)
	if len(cloned.Answers) != 2 || cloned.Answers["text"].Text == nil || *cloned.Answers["text"].Text != text {
		t.Fatalf("cloned answers = %#v", cloned.Answers)
	}
	clonedAnswer := cloned.Answers["multi"]
	clonedAnswer.OptionIDs[0] = "changed"
	if original.Answers["multi"].OptionIDs[0] != "one" {
		t.Fatal("cloned option IDs alias the original response")
	}
}

func TestGuidedInteractionRejectsEmptyAnswerSet(t *testing.T) {
	t.Parallel()
	request := InteractionRequest{
		ID: "interaction_0123456789abcdef0123456789abcdef", Kind: InteractionGuided,
		Questions: []InteractionQuestion{{ID: "required", Kind: InteractionQuestionBoolean, Required: true}},
	}
	response := InteractionResponse{RequestID: request.ID, Answers: map[string]InteractionAnswer{}}
	if err := validateInteractionResponse(request, response); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty guided answer error = %v", err)
	}
}
