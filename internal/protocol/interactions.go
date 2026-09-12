package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/identifier"
)

const (
	MaxPendingInteractions    = 8
	MaxInteractionBytes       = 64 << 10
	MaxInteractionTitleBytes  = 512
	MaxInteractionDetailBytes = 8 << 10
	MaxInteractionAnswerBytes = 16 << 10
	MaxInteractionOptions     = 64
	MaxGuidedQuestions        = 32
)

// InteractionKind identifies one model-user request on the wire.
type InteractionKind string

const (
	InteractionConfirm InteractionKind = "confirm"
	InteractionInput   InteractionKind = "input"
	InteractionSelect  InteractionKind = "select"
	InteractionGuided  InteractionKind = "guided"
)

// InteractionQuestionKind identifies a guided answer shape.
type InteractionQuestionKind string

const (
	InteractionQuestionText        InteractionQuestionKind = "text"
	InteractionQuestionSelect      InteractionQuestionKind = "select"
	InteractionQuestionMultiselect InteractionQuestionKind = "multiselect"
	InteractionQuestionBoolean     InteractionQuestionKind = "boolean"
)

// InteractionOption is a bounded server-owned choice.
type InteractionOption struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// InteractionQuestion is one step of a guided questionnaire.
type InteractionQuestion struct {
	ID       string                  `json:"id"`
	Prompt   string                  `json:"prompt"`
	Detail   string                  `json:"detail,omitempty"`
	Kind     InteractionQuestionKind `json:"kind"`
	Required bool                    `json:"required"`
	Options  []InteractionOption     `json:"options,omitempty"`
}

// InteractionRequest is one pending request projected by a session.
type InteractionRequest struct {
	ID         string                `json:"id"`
	SessionID  string                `json:"sessionId"`
	RunID      string                `json:"runId"`
	ToolCallID string                `json:"toolCallId"`
	Kind       InteractionKind       `json:"kind"`
	Title      string                `json:"title"`
	Detail     string                `json:"detail,omitempty"`
	Options    []InteractionOption   `json:"options,omitempty"`
	Questions  []InteractionQuestion `json:"questions,omitempty"`
	CreatedAt  string                `json:"createdAt"`
}

// InteractionAnswer is one guided-question answer submitted by a client.
type InteractionAnswer struct {
	Text      *string  `json:"text,omitempty"`
	Boolean   *bool    `json:"boolean,omitempty"`
	OptionIDs []string `json:"optionIds,omitempty"`
	Skipped   bool     `json:"skipped,omitempty"`
}

// InteractionResponse atomically settles a pending request when valid.
type InteractionResponse struct {
	RequestID        string                       `json:"requestId"`
	Cancelled        bool                         `json:"cancelled,omitempty"`
	Confirmed        *bool                        `json:"confirmed,omitempty"`
	Value            *string                      `json:"value,omitempty"`
	SelectedOptionID string                       `json:"selectedOptionId,omitempty"`
	Answers          map[string]InteractionAnswer `json:"answers,omitempty"`
}

func (request InteractionRequest) Validate() error {
	if !identifier.Valid(request.ID, "interaction_") || request.SessionID == "" || request.RunID == "" || request.ToolCallID == "" {
		return fmt.Errorf("interaction identities are required")
	}
	if _, err := time.Parse(time.RFC3339Nano, request.CreatedAt); err != nil {
		return fmt.Errorf("interaction createdAt is invalid")
	}
	if !validInteractionText(request.Title, MaxInteractionTitleBytes, true) || !validInteractionText(request.Detail, MaxInteractionDetailBytes, false) {
		return fmt.Errorf("interaction title or detail is invalid")
	}
	if len(request.Options) > MaxInteractionOptions || len(request.Questions) > MaxGuidedQuestions {
		return fmt.Errorf("interaction collection limit exceeded")
	}
	switch request.Kind {
	case InteractionConfirm, InteractionInput:
		if len(request.Options) != 0 || len(request.Questions) != 0 {
			return fmt.Errorf("interaction payload does not match kind")
		}
	case InteractionSelect:
		if len(request.Options) == 0 || len(request.Questions) != 0 {
			return fmt.Errorf("select interaction requires options")
		}
	case InteractionGuided:
		if len(request.Questions) == 0 || len(request.Options) != 0 {
			return fmt.Errorf("guided interaction requires questions")
		}
	default:
		return fmt.Errorf("interaction kind %q is invalid", request.Kind)
	}
	if err := validateProtocolOptions(request.Options); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, question := range request.Questions {
		if strings.TrimSpace(question.ID) == "" || len(question.ID) > 128 || !validInteractionText(question.Prompt, MaxInteractionTitleBytes, true) || !validInteractionText(question.Detail, MaxInteractionDetailBytes, false) {
			return fmt.Errorf("guided question is invalid")
		}
		if _, ok := seen[question.ID]; ok {
			return fmt.Errorf("duplicate guided question %q", question.ID)
		}
		seen[question.ID] = struct{}{}
		switch question.Kind {
		case InteractionQuestionText, InteractionQuestionBoolean:
			if len(question.Options) != 0 {
				return fmt.Errorf("question %q cannot have options", question.ID)
			}
		case InteractionQuestionSelect, InteractionQuestionMultiselect:
			if len(question.Options) == 0 {
				return fmt.Errorf("question %q requires options", question.ID)
			}
		default:
			return fmt.Errorf("question %q kind is invalid", question.ID)
		}
		if err := validateProtocolOptions(question.Options); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > MaxInteractionBytes {
		return fmt.Errorf("interaction exceeds size limit")
	}
	return nil
}

// Validate checks the response's standalone wire constraints. Request-specific
// answer validation remains authoritative on the server.
func (response InteractionResponse) Validate() error {
	if !identifier.Valid(response.RequestID, "interaction_") {
		return fmt.Errorf("interaction request id is invalid")
	}
	if response.Cancelled {
		if response.Confirmed != nil || response.Value != nil || response.SelectedOptionID != "" || len(response.Answers) != 0 {
			return fmt.Errorf("cancelled interaction carries an answer")
		}
		return nil
	}
	responseShapes := 0
	if response.Confirmed != nil {
		responseShapes++
	}
	if response.Value != nil {
		responseShapes++
	}
	if response.SelectedOptionID != "" {
		responseShapes++
	}
	if response.Answers != nil {
		responseShapes++
	}
	if responseShapes != 1 {
		return fmt.Errorf("interaction response must carry exactly one answer shape")
	}
	if response.Value != nil && !validInteractionText(*response.Value, MaxInteractionAnswerBytes, true) {
		return fmt.Errorf("interaction value is invalid")
	}
	if response.SelectedOptionID != "" && !identifier.Valid(response.SelectedOptionID, "option_") {
		return fmt.Errorf("selected option id is invalid")
	}
	if len(response.Answers) > MaxGuidedQuestions {
		return fmt.Errorf("too many guided answers")
	}
	for id, answer := range response.Answers {
		if strings.TrimSpace(id) == "" || len(id) > 128 {
			return fmt.Errorf("guided answer id is invalid")
		}
		answerShapes := 0
		if answer.Text != nil {
			answerShapes++
		}
		if answer.Boolean != nil {
			answerShapes++
		}
		if answer.OptionIDs != nil {
			answerShapes++
		}
		if answer.Skipped {
			answerShapes++
		}
		if answerShapes != 1 {
			return fmt.Errorf("guided answer must carry exactly one answer shape")
		}
		if answer.Text != nil && !validInteractionText(*answer.Text, MaxInteractionAnswerBytes, true) {
			return fmt.Errorf("guided text answer is invalid")
		}
		if len(answer.OptionIDs) > MaxInteractionOptions {
			return fmt.Errorf("too many guided option answers")
		}
		for _, optionID := range answer.OptionIDs {
			if !identifier.Valid(optionID, "option_") {
				return fmt.Errorf("guided option id is invalid")
			}
		}
	}
	raw, err := json.Marshal(response)
	if err != nil || len(raw) > MaxInteractionBytes {
		return fmt.Errorf("interaction response exceeds size limit")
	}
	return nil
}

func validateProtocolOptions(options []InteractionOption) error {
	seen := map[string]struct{}{}
	for _, option := range options {
		if !identifier.Valid(option.ID, "option_") || !validInteractionText(option.Label, MaxInteractionTitleBytes, true) || !validInteractionText(option.Detail, MaxInteractionDetailBytes, false) {
			return fmt.Errorf("interaction option is invalid")
		}
		if _, ok := seen[option.ID]; ok {
			return fmt.Errorf("duplicate interaction option")
		}
		seen[option.ID] = struct{}{}
	}
	return nil
}

func validInteractionText(value string, limit int, required bool) bool {
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
