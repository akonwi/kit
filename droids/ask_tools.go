package droids

import (
	"context"
	"encoding/json"
	"fmt"
)

// ask_tools.go — the standard question tools. Each asks the person a declared
// question before it runs, so the question survives restarts, and returns the
// answer to the model as JSON. A question not answered returns its reason.
//
// Each result's details carry a resolution: "answered", "negative_confirmation"
// for a confirm answered no, "user_cancelled" for a declined question
// (including one declined by steering), "run_abort" for a question cancelled
// because its turn ended, and "unavailable".

type askToolArgs struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

type selectToolArgs struct {
	Title   string          `json:"title"`
	Detail  string          `json:"detail,omitempty"`
	Options []askToolOption `json:"options"`
}

type askToolOption struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Detail string `json:"detail,omitempty"`
}

type guidedToolArgs struct {
	Title     string            `json:"title"`
	Detail    string            `json:"detail,omitempty"`
	Questions []askToolQuestion `json:"questions"`
}

type askToolQuestion struct {
	ID       string             `json:"id"`
	Prompt   string             `json:"prompt"`
	Detail   string             `json:"detail,omitempty"`
	Kind     GuidedQuestionKind `json:"kind"`
	Required *bool              `json:"required,omitempty"`
	Options  []askToolOption    `json:"options,omitempty"`
}

// ConfirmTool returns a tool, named name, with which the model asks the person
// one yes-or-no question.
func ConfirmTool(name string) AnyTool {
	return MustTool(Tool[askToolArgs]{
		Name: name, Description: "Request one boolean decision from the user.",
		Parameters: askToolSchema(), Mode: ModeSequential,
		ask: func(args askToolArgs) (Question, error) {
			return Question{Kind: QuestionConfirm, Title: args.Title, Detail: args.Detail}, nil
		},
		Execute: func(ctx context.Context, _ ToolContext, _ askToolArgs, _ ToolUpdate) (ToolResult, error) {
			settlement, err := settlementFor(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			confirmed := settlement.Status == InteractionAnswered && *settlement.Answer.Confirmed
			return askToolResult(map[string]any{"confirmed": confirmed}, settlement)
		},
	})
}

// InputTool returns a tool, named name, with which the model asks the person
// for one short freeform value.
func InputTool(name string) AnyTool {
	return MustTool(Tool[askToolArgs]{
		Name: name, Description: "Request one short freeform value from the user.",
		Parameters: askToolSchema(), Mode: ModeSequential,
		ask: func(args askToolArgs) (Question, error) {
			return Question{Kind: QuestionInput, Title: args.Title, Detail: args.Detail}, nil
		},
		Execute: func(ctx context.Context, _ ToolContext, _ askToolArgs, _ ToolUpdate) (ToolResult, error) {
			settlement, err := settlementFor(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			answered := settlement.Status == InteractionAnswered
			var value *string
			if answered {
				value = settlement.Answer.Value
			}
			return askToolResult(map[string]any{"value": value, "cancelled": !answered}, settlement)
		},
	})
}

// SelectTool returns a tool, named name, with which the model asks the person
// to choose one of a bounded list of options.
func SelectTool(name string) AnyTool {
	parameters := askToolSchema()
	parameters["properties"].(map[string]any)["options"] = askToolOptionsSchema()
	parameters["required"] = []string{"title", "options"}
	return MustTool(Tool[selectToolArgs]{
		Name: name, Description: "Request one choice from a bounded list.",
		Parameters: parameters, Mode: ModeSequential,
		ask: func(args selectToolArgs) (Question, error) {
			options, err := askToolOptions(args.Options)
			if err != nil {
				return Question{}, err
			}
			return Question{Kind: QuestionSelect, Title: args.Title, Detail: args.Detail, Options: options}, nil
		},
		Execute: func(ctx context.Context, _ ToolContext, _ selectToolArgs, _ ToolUpdate) (ToolResult, error) {
			settlement, question, err := settledQuestion(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			answered := settlement.Status == InteractionAnswered
			var value, label any
			if answered {
				for _, option := range question.Options {
					if option.ID == settlement.Answer.OptionID {
						value, label = option.Value, option.Label
					}
				}
			}
			return askToolResult(map[string]any{"value": value, "label": label, "cancelled": !answered}, settlement)
		},
	})
}

// GuidedTool returns a tool, named name, with which the model asks the person
// a structured questionnaire whose questions are presented one at a time.
func GuidedTool(name string) AnyTool {
	parameters := askToolSchema()
	parameters["properties"].(map[string]any)["questions"] = askToolQuestionsSchema()
	parameters["required"] = []string{"title", "questions"}
	return MustTool(Tool[guidedToolArgs]{
		Name: name, Description: "Request a structured questionnaire whose questions are presented one at a time.",
		Parameters: parameters, Mode: ModeSequential,
		ask: func(args guidedToolArgs) (Question, error) {
			questions := make([]GuidedQuestion, 0, len(args.Questions))
			for _, item := range args.Questions {
				required := item.Required == nil || *item.Required
				var options []Option
				if len(item.Options) > 0 {
					var err error
					if options, err = askToolOptions(item.Options); err != nil {
						return Question{}, err
					}
				}
				questions = append(questions, GuidedQuestion{ID: item.ID, Prompt: item.Prompt, Detail: item.Detail, Kind: item.Kind, Required: required, Options: options})
			}
			return Question{Kind: QuestionGuided, Title: args.Title, Detail: args.Detail, Questions: questions}, nil
		},
		Execute: func(ctx context.Context, _ ToolContext, _ guidedToolArgs, _ ToolUpdate) (ToolResult, error) {
			settlement, question, err := settledQuestion(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			answered := settlement.Status == InteractionAnswered
			answers := map[string]any{}
			if answered {
				for _, guided := range question.Questions {
					answer, ok := settlement.Answer.Answers[guided.ID]
					if !ok || answer.Skipped {
						continue
					}
					answers[guided.ID] = guidedAnswerValue(guided, answer)
				}
			}
			result := map[string]any{"cancelled": !answered, "answers": answers, "answeredCount": len(answers), "totalQuestions": len(question.Questions)}
			if answered {
				result["completed"] = true
			}
			return askToolResult(result, settlement)
		},
	})
}

// settlementFor returns the settlement a standard question tool runs with.
func settlementFor(ctx context.Context) (Settlement, error) {
	settlement, ok := declaredSettlement(ctx)
	if !ok {
		return Settlement{}, fmt.Errorf("droids: the question was not asked")
	}
	return settlement, nil
}

// settledQuestion returns the settlement a standard question tool runs with,
// and the question that was asked.
func settledQuestion(ctx context.Context) (Settlement, Question, error) {
	settlement, err := settlementFor(ctx)
	if err != nil {
		return Settlement{}, Question{}, err
	}
	question, ok := ctx.Value(declaredQuestionKey{}).(Question)
	if !ok {
		return Settlement{}, Question{}, fmt.Errorf("droids: the question was not asked")
	}
	return settlement, question, nil
}

// askToolResult returns value to the model as JSON, with the reason a question
// was not answered.
func askToolResult(value map[string]any, settlement Settlement) (ToolResult, error) {
	if settlement.Status != InteractionAnswered && settlement.Reason != "" {
		value["reason"] = settlement.Reason
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ToolResult{}, err
	}
	details, err := EncodeDetails(map[string]any{"resolution": askToolResolution(settlement)})
	if err != nil {
		return ToolResult{}, err
	}
	result := ToolText(string(raw))
	result.Details = details
	return result, nil
}

func askToolResolution(settlement Settlement) string {
	switch settlement.Status {
	case InteractionAnswered:
		if confirmed := settlement.Answer.Confirmed; confirmed != nil && !*confirmed {
			return "negative_confirmation"
		}
		return "answered"
	case InteractionDeclined:
		return "user_cancelled"
	case InteractionCancelled:
		return "run_abort"
	default:
		return "unavailable"
	}
}

func askToolOptions(input []askToolOption) ([]Option, error) {
	if len(input) == 0 || len(input) > maxInteractionOptions {
		return nil, fmt.Errorf("options count is invalid")
	}
	values := make(map[string]struct{}, len(input))
	options := make([]Option, 0, len(input))
	for _, option := range input {
		if _, duplicate := values[option.Value]; duplicate {
			return nil, fmt.Errorf("duplicate option value %q", option.Value)
		}
		values[option.Value] = struct{}{}
		id, err := newID("option_")
		if err != nil {
			return nil, err
		}
		options = append(options, Option{ID: id, Label: option.Label, Value: option.Value, Detail: option.Detail})
	}
	return options, nil
}

func guidedAnswerValue(question GuidedQuestion, answer GuidedAnswer) any {
	if answer.Text != nil {
		return *answer.Text
	}
	if answer.Boolean != nil {
		return *answer.Boolean
	}
	values := make([]string, 0, len(answer.OptionIDs))
	for _, id := range answer.OptionIDs {
		for _, option := range question.Options {
			if option.ID == id {
				values = append(values, option.Value)
			}
		}
	}
	if question.Kind == GuidedSelect && len(values) == 1 {
		return values[0]
	}
	return values
}

func askToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":  map[string]any{"type": "string", "minLength": 1, "maxLength": maxInteractionTitleBytes},
			"detail": map[string]any{"type": "string", "maxLength": maxInteractionDetailBytes},
		},
		"required": []string{"title"}, "additionalProperties": false,
	}
}

func askToolOptionsSchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "maxItems": maxInteractionOptions, "items": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label":  map[string]any{"type": "string", "minLength": 1, "maxLength": maxInteractionTitleBytes},
			"value":  map[string]any{"type": "string", "minLength": 1, "maxLength": maxInteractionAnswerBytes},
			"detail": map[string]any{"type": "string", "maxLength": maxInteractionDetailBytes},
		},
		"required": []string{"label", "value"}, "additionalProperties": false,
	}}
}

func askToolQuestionsSchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "maxItems": maxGuidedQuestions, "items": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":       map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
			"prompt":   map[string]any{"type": "string", "minLength": 1, "maxLength": maxInteractionTitleBytes},
			"detail":   map[string]any{"type": "string", "maxLength": maxInteractionDetailBytes},
			"kind":     map[string]any{"type": "string", "enum": []string{"text", "select", "multiselect", "boolean"}},
			"required": map[string]any{"type": "boolean"},
			"options":  askToolOptionsSchema(),
		},
		"required": []string{"id", "prompt", "kind"}, "additionalProperties": false,
	}}
}
