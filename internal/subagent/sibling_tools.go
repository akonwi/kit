package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/droids"
)

// SiblingToolService binds request and reply capabilities to one child.
type SiblingToolService struct {
	Repository RequestRepository
	Supervisor *Supervisor
	// ResolveRecipient authorizes and resolves only an owner's configured,
	// never-started recipient. The model cannot provide a definition or model.
	ResolveRecipient func(context.Context, string, string) (Definition, Owner, string, string, error)
}

type siblingSendArgs struct {
	Agent   string `json:"agent"`
	Message string `json:"message"`
}

type siblingReplyArgs struct {
	Receipt string `json:"receipt"`
	Message string `json:"message"`
}

type siblingInspectArgs struct {
	Receipt string `json:"receipt"`
}

type siblingInboxArgs struct {
	After string `json:"after,omitempty"`
}

// Tools creates child-bound sibling operations without lifecycle authority.
func (s *SiblingToolService) Tools(conversation Conversation) ([]droids.AnyTool, error) {
	if s == nil || s.Repository == nil || s.Supervisor == nil {
		return nil, errors.New("sibling request tool service is not initialized")
	}
	send, err := droids.NewTool(droids.Tool[siblingSendArgs]{
		Name: "subagent_send", Description: "Send a reply-capable request to a configured sibling under your parent. An unstarted sibling is initialized to receive it. Returns an opaque receipt immediately; you can finish this turn and receive the reply in a later inbox turn. You cannot control or message yourself.",
		Parameters: objectParameters(map[string]any{"agent": stringParameter("Configured sibling name."), "message": stringParameter("Bounded message for the sibling.")}, "agent", "message"),
		Mode:       droids.ModeSequential,
		Execute: func(ctx context.Context, call droids.ToolContext, args siblingSendArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			name := strings.TrimSpace(args.Agent)
			admission := RequestAdmission{
				OwnerSessionID: conversation.OwnerSessionID, SenderConversationID: conversation.ID,
				RecipientName: name, Message: args.Message, DeliveryMode: "send",
				CallIdentity: siblingCallIdentity(call), Now: time.Now().UTC(),
			}
			request, err := s.Supervisor.SendRequest(ctx, admission)
			if errors.Is(err, ErrNotFound) {
				// A committed retry or existing recipient is handled above even if
				// its definition was subsequently removed from the catalog.
				definition, owner, model, thinking, resolveErr := s.resolveUnstartedSibling(ctx, conversation.OwnerSessionID, name)
				if resolveErr != nil {
					return siblingResult(nil, resolveErr), nil
				}
				admission.NewRecipient, admission.CWD, admission.Model, admission.ThinkingLevel = &definition, owner.CWD, model, thinking
				request, err = s.Supervisor.SendRequest(ctx, admission)
			}
			return siblingResult(struct {
				Receipt string       `json:"receipt"`
				State   RequestState `json:"state"`
			}{request.ID, request.State}, err), nil
		},
	})
	if err != nil {
		return nil, err
	}
	reply, err := droids.NewTool(droids.Tool[siblingReplyArgs]{
		Name: "subagent_reply", Description: "Explicitly reply once to a request addressed to you, using its receipt. Your turn ending does not send an implicit answer.",
		Parameters: objectParameters(map[string]any{"receipt": stringParameter("Incoming request receipt."), "message": stringParameter("Bounded answer to the sender.")}, "receipt", "message"),
		Mode:       droids.ModeSequential,
		Execute: func(ctx context.Context, call droids.ToolContext, args siblingReplyArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			request, err := s.Supervisor.ReplyRequest(ctx, conversation.OwnerSessionID, conversation.ID, args.Receipt, siblingCallIdentity(call), args.Message)
			return siblingResult(struct {
				Receipt string       `json:"receipt"`
				State   RequestState `json:"state"`
			}{request.ID, request.State}, err), nil
		},
	})
	if err != nil {
		return nil, err
	}
	inspect, err := droids.NewTool(droids.Tool[siblingInspectArgs]{
		Name: "subagent_inspect", Description: "Inspect an outgoing sibling request or an incoming request addressed to you by receipt.",
		Parameters: objectParameters(map[string]any{"receipt": stringParameter("Request receipt.")}, "receipt"),
		Mode:       droids.ModeSequential,
		Execute: func(ctx context.Context, _ droids.ToolContext, args siblingInspectArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			request, err := s.Repository.InspectSubagentRequest(ctx, conversation.OwnerSessionID, conversation.ID, args.Receipt)
			return siblingRequestResult(request, err), nil
		},
	})
	if err != nil {
		return nil, err
	}
	inbox, err := droids.NewTool(droids.Tool[siblingInboxArgs]{
		Name: "subagent_inbox", Description: "List up to 32 outstanding requests addressed to you. Use after to read another page.",
		Parameters: objectParameters(map[string]any{"after": stringParameter("Optional receipt from the previous page.")}),
		Mode:       droids.ModeSequential,
		Execute: func(ctx context.Context, _ droids.ToolContext, args siblingInboxArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			requests, err := s.Repository.ListSubagentInbox(ctx, conversation.OwnerSessionID, conversation.ID, args.After, 32)
			if err != nil {
				return siblingResult(nil, err), nil
			}
			views := make([]siblingRequestView, 0, len(requests))
			for _, request := range requests {
				views = append(views, projectSiblingRequest(request))
			}
			return siblingResult(views, nil), nil
		},
	})
	if err != nil {
		return nil, err
	}
	return []droids.AnyTool{send, reply, inspect, inbox}, nil
}

func (s *SiblingToolService) resolveUnstartedSibling(ctx context.Context, ownerID, name string) (Definition, Owner, string, string, error) {
	if s.ResolveRecipient == nil {
		return Definition{}, Owner{}, "", "", ErrNotFound
	}
	return s.ResolveRecipient(ctx, ownerID, name)
}

type siblingRequestView struct {
	Receipt  string       `json:"receipt"`
	From     string       `json:"from"`
	To       string       `json:"to"`
	Message  string       `json:"message"`
	State    RequestState `json:"state"`
	Reply    string       `json:"reply,omitempty"`
	Failure  string       `json:"failure,omitempty"`
	Deadline time.Time    `json:"deadline"`
}

func projectSiblingRequest(request Request) siblingRequestView {
	return siblingRequestView{
		Receipt: request.ID, From: request.SenderName, To: request.RecipientName,
		Message: request.Message, State: request.State, Reply: request.Reply,
		Failure: request.Failure, Deadline: request.DeadlineAt,
	}
}

func siblingRequestResult(request Request, err error) droids.ToolResult {
	return siblingResult(projectSiblingRequest(request), err)
}

func siblingCallIdentity(call droids.ToolContext) string {
	return string(call.TurnID) + "/" + string(call.ToolCallID)
}

func stringParameter(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func objectParameters(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func siblingResult(value any, err error) droids.ToolResult {
	if err != nil {
		result := droids.ToolText(fmt.Sprintf("subagent request: %v", err))
		result.IsError = true
		return result
	}
	raw, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		result := droids.ToolText(marshalErr.Error())
		result.IsError = true
		return result
	}
	return droids.ToolText(string(raw))
}
