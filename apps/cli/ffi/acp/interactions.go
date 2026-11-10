package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
)

// Property names of the single-question forms Kit asks with.
const (
	formConfirmed = "confirmed"
	formValue     = "value"
	formChoice    = "choice"
)

// presentInteraction asks the client to answer request and relays the
// answer. ctx ends when the request is resolved elsewhere or the session is
// closed. A request the client cannot present stays pending for another
// attached client.
func (s *session) presentInteraction(ctx context.Context, request protocol.InteractionRequest) {
	capabilities := s.agent.clientCapabilities()
	form := capabilities.Elicitation != nil && advertised(capabilities.Elicitation.Form)
	var response protocol.InteractionResponse
	var err error
	switch {
	case request.Kind == protocol.InteractionConfirm && request.Plugin == nil && request.ToolCallID != "":
		response, err = s.askPermission(ctx, request)
	case request.Kind == protocol.InteractionAuthorize:
		s.leavePending(request, "the client cannot open authorization pages for Kit")
		return
	case form:
		response, err = s.askForm(ctx, request)
	default:
		s.leavePending(request, "the client does not support elicitation forms")
		return
	}
	if err != nil {
		if ctx.Err() == nil {
			s.leavePending(request, err.Error())
		}
		return
	}
	response.RequestID = request.ID
	// The answer is sent even if the request is resolved meanwhile, which
	// the server reports as a conflict, but not after the session closes.
	respondContext, cancel := context.WithTimeout(s.ctx, requestTimeout)
	defer cancel()
	if err := s.handle.RespondInteraction(respondContext, response); err != nil && ctx.Err() == nil {
		s.agent.logger.Warn("answer interaction", "session", s.id, "interaction", request.ID, "err", err)
	}
}

func (s *session) leavePending(request protocol.InteractionRequest, reason string) {
	s.agent.logger.Warn("interaction left for another Kit client", "session", s.id, "interaction", request.ID, "kind", request.Kind, "title", request.Title, "reason", reason)
}

func (s *session) askPermission(ctx context.Context, request protocol.InteractionRequest) (protocol.InteractionResponse, error) {
	allow, reject := request.ConfirmLabel, request.CancelLabel
	if allow == "" {
		allow = "Yes"
	}
	if reject == "" {
		reject = "No"
	}
	update := toolCallUpdate{ToolCallID: request.ToolCallID, Title: request.Title}
	if request.Detail != "" {
		update.Content = []toolCallContent{textContent(request.Detail)}
	}
	raw, err := s.agent.conn.call(ctx, methodRequestPermission, requestPermissionParams{
		SessionID: s.id, ToolCall: update,
		Options: []permissionOption{
			{OptionID: permissionOptionAllow, Name: allow, Kind: "allow_once"},
			{OptionID: permissionOptionReject, Name: reject, Kind: "reject_once"},
		},
	})
	if err != nil {
		return protocol.InteractionResponse{}, err
	}
	var result requestPermissionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return protocol.InteractionResponse{}, errors.New("malformed permission response")
	}
	confirmed := false
	switch {
	case result.Outcome.Outcome == permissionOutcomeChosen && result.Outcome.OptionID == permissionOptionAllow:
		confirmed = true
	case result.Outcome.Outcome == permissionOutcomeChosen && result.Outcome.OptionID == permissionOptionReject:
	default:
		// A cancelled or unrecognized outcome is never an approval.
		return protocol.InteractionResponse{Cancelled: true}, nil
	}
	return protocol.InteractionResponse{Confirmed: &confirmed}, nil
}

func (s *session) askForm(ctx context.Context, request protocol.InteractionRequest) (protocol.InteractionResponse, error) {
	schema := interactionSchema(request)
	message := request.Title
	if request.Detail != "" {
		message += "\n\n" + request.Detail
	}
	raw, err := s.agent.conn.call(ctx, methodElicitationCreate, elicitationParams{
		SessionID: s.id, ToolCallID: request.ToolCallID, Mode: elicitationModeForm,
		Message: message, RequestedSchema: schema,
	})
	if err != nil {
		return protocol.InteractionResponse{}, err
	}
	var result elicitationResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return protocol.InteractionResponse{}, errors.New("malformed elicitation response")
	}
	if result.Action != elicitationActionAccept {
		if result.Action == "decline" && request.Kind == protocol.InteractionConfirm {
			confirmed := false
			return protocol.InteractionResponse{Confirmed: &confirmed}, nil
		}
		return protocol.InteractionResponse{Cancelled: true}, nil
	}
	response, ok := interactionAnswer(request, result.Content)
	if !ok {
		return protocol.InteractionResponse{Cancelled: true}, nil
	}
	return response, nil
}

// interactionSchema is the form that asks request: one property per guided
// question, or a single property for the other kinds.
func interactionSchema(request protocol.InteractionRequest) elicitationSchema {
	schema := elicitationSchema{Type: "object", Properties: map[string]elicitationProperty{}}
	switch request.Kind {
	case protocol.InteractionConfirm:
		property := elicitationProperty{Type: "boolean", Title: request.Title}
		if request.DefaultValue != nil {
			property.Default = *request.DefaultValue
		}
		schema.Properties[formConfirmed] = property
		schema.Required = []string{formConfirmed}
	case protocol.InteractionInput:
		property := elicitationProperty{Type: "string", Title: request.Title, Description: request.Placeholder}
		if request.InitialValue != "" {
			property.Default = request.InitialValue
		}
		schema.Properties[formValue] = property
		schema.Required = []string{formValue}
	case protocol.InteractionSelect:
		schema.Properties[formChoice] = elicitationProperty{Type: "string", Title: request.Title, OneOf: enumOptions(request.Options)}
		schema.Required = []string{formChoice}
	case protocol.InteractionGuided:
		for _, question := range request.Questions {
			property := elicitationProperty{Title: question.Prompt, Description: question.Detail}
			switch question.Kind {
			case protocol.InteractionQuestionText:
				property.Type = "string"
			case protocol.InteractionQuestionBoolean:
				property.Type = "boolean"
			case protocol.InteractionQuestionSelect:
				property.Type, property.OneOf = "string", enumOptions(question.Options)
			case protocol.InteractionQuestionMultiselect:
				property.Type, property.Items = "array", &elicitationItems{AnyOf: enumOptions(question.Options)}
			}
			schema.Properties[question.ID] = property
			if question.Required {
				schema.Required = append(schema.Required, question.ID)
			}
		}
	}
	return schema
}

func enumOptions(options []protocol.InteractionOption) []enumOption {
	result := make([]enumOption, 0, len(options))
	for _, option := range options {
		title := option.Label
		if option.Detail != "" {
			title += " — " + option.Detail
		}
		result = append(result, enumOption{Const: option.ID, Title: title})
	}
	return result
}

// interactionAnswer reads an accepted form. It reports false when the
// content does not answer the request.
func interactionAnswer(request protocol.InteractionRequest, content map[string]json.RawMessage) (protocol.InteractionResponse, bool) {
	switch request.Kind {
	case protocol.InteractionConfirm:
		var confirmed bool
		if json.Unmarshal(content[formConfirmed], &confirmed) != nil {
			return protocol.InteractionResponse{}, false
		}
		return protocol.InteractionResponse{Confirmed: &confirmed}, true
	case protocol.InteractionInput:
		var value string
		if json.Unmarshal(content[formValue], &value) != nil {
			return protocol.InteractionResponse{}, false
		}
		return protocol.InteractionResponse{Value: &value}, true
	case protocol.InteractionSelect:
		var choice string
		if json.Unmarshal(content[formChoice], &choice) != nil || choice == "" {
			return protocol.InteractionResponse{}, false
		}
		return protocol.InteractionResponse{SelectedOptionID: choice}, true
	case protocol.InteractionGuided:
		answers := map[string]protocol.InteractionAnswer{}
		for _, question := range request.Questions {
			raw, present := content[question.ID]
			if !present || string(raw) == "null" {
				if question.Required {
					return protocol.InteractionResponse{}, false
				}
				answers[question.ID] = protocol.InteractionAnswer{Skipped: true}
				continue
			}
			var answer protocol.InteractionAnswer
			switch question.Kind {
			case protocol.InteractionQuestionText:
				var text string
				if json.Unmarshal(raw, &text) != nil {
					return protocol.InteractionResponse{}, false
				}
				if strings.TrimSpace(text) == "" && !question.Required {
					answer.Skipped = true
				} else {
					answer.Text = &text
				}
			case protocol.InteractionQuestionBoolean:
				var value bool
				if json.Unmarshal(raw, &value) != nil {
					return protocol.InteractionResponse{}, false
				}
				answer.Boolean = &value
			case protocol.InteractionQuestionSelect:
				var choice string
				if json.Unmarshal(raw, &choice) != nil || choice == "" {
					return protocol.InteractionResponse{}, false
				}
				answer.OptionIDs = []string{choice}
			case protocol.InteractionQuestionMultiselect:
				var choices []string
				if json.Unmarshal(raw, &choices) != nil {
					return protocol.InteractionResponse{}, false
				}
				answer.OptionIDs = choices
			}
			answers[question.ID] = answer
		}
		return protocol.InteractionResponse{Answers: answers}, true
	}
	return protocol.InteractionResponse{}, false
}
