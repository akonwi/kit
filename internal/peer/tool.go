package peer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

// Service is the server-owned capability exposed to one session's model tool.
type Service interface {
	DiscoverPeers(context.Context, string, int) ([]Session, error)
	SendPeerQuery(context.Context, string, droids.ToolContext, string, string, string, string) (Request, error)
	InspectPeerQuery(context.Context, string, string) (Request, error)
	WaitPeerQuery(context.Context, string, string, time.Duration) (Request, bool, error)
}

// ToolFactory constructs the peer-query tool for one session.
type ToolFactory interface {
	Tool(string) (droids.AnyTool, error)
}

// ToolService adapts the server capability to bounded model-facing operations.
type ToolService struct{ Service Service }

func (s *ToolService) Tool(senderSessionID string) (droids.AnyTool, error) {
	if s == nil || s.Service == nil {
		return nil, errors.New("peer query tool service is not initialized")
	}
	return droids.NewTool(droids.Tool[toolArguments]{
		Name:        "peer_session",
		Description: "Consult other durable top-level sessions without copying transcripts. Actions: discover, send, inspect, wait. Send is asynchronous and returns a durable request ID; wait is bounded and does not cancel the request on timeout.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":             map[string]any{"type": "string", "enum": []string{"discover", "send", "inspect", "wait"}},
				"sessionId":          map[string]any{"type": "string", "description": "Recipient session ID for send."},
				"requestId":          map[string]any{"type": "string", "description": "Durable request ID for inspect or wait."},
				"message":            map[string]any{"type": "string", "description": "Bounded question for send."},
				"threadId":           map[string]any{"type": "string", "description": "Optional caller-owned thread identity."},
				"precedingRequestId": map[string]any{"type": "string", "description": "Optional preceding request in the thread."},
				"timeoutSeconds":     map[string]any{"type": "integer", "minimum": 1, "maximum": 30},
			},
			"required": []string{"action"}, "additionalProperties": false,
		},
		Mode: droids.ModeSequential,
		Execute: func(ctx context.Context, call droids.ToolContext, args toolArguments, _ droids.ToolUpdate) (droids.ToolResult, error) {
			return s.execute(ctx, senderSessionID, call, args), nil
		},
	})
}

type toolArguments struct {
	Action             string `json:"action"`
	SessionID          string `json:"sessionId,omitempty"`
	RequestID          string `json:"requestId,omitempty"`
	Message            string `json:"message,omitempty"`
	ThreadID           string `json:"threadId,omitempty"`
	PrecedingRequestID string `json:"precedingRequestId,omitempty"`
	TimeoutSeconds     int    `json:"timeoutSeconds,omitempty"`
}

type toolResponse struct {
	Action   string    `json:"action"`
	Sessions []Session `json:"sessions,omitempty"`
	Request  *request  `json:"request,omitempty"`
	TimedOut bool      `json:"timedOut,omitempty"`
	Error    string    `json:"error,omitempty"`
}

type request struct {
	ID                 string `json:"id"`
	SenderSessionID    string `json:"senderSessionId"`
	RecipientSessionID string `json:"recipientSessionId"`
	ThreadID           string `json:"threadId,omitempty"`
	PrecedingRequestID string `json:"precedingRequestId,omitempty"`
	State              State  `json:"state"`
	Result             string `json:"result,omitempty"`
	Error              string `json:"error,omitempty"`
}

func projectRequest(value Request) request {
	return request{ID: value.ID, SenderSessionID: value.SenderSessionID, RecipientSessionID: value.RecipientSessionID,
		ThreadID: value.ThreadID, PrecedingRequestID: value.PrecedingRequestID, State: value.State,
		Result: value.Result, Error: value.Error}
}

func (s *ToolService) execute(ctx context.Context, sender string, call droids.ToolContext, args toolArguments) droids.ToolResult {
	response := toolResponse{Action: args.Action}
	var err error
	switch args.Action {
	case "discover":
		response.Sessions, err = s.Service.DiscoverPeers(ctx, sender, 20)
	case "send":
		var value Request
		value, err = s.Service.SendPeerQuery(ctx, sender, call, strings.TrimSpace(args.SessionID), args.Message, args.ThreadID, args.PrecedingRequestID)
		if err == nil {
			projected := projectRequest(value)
			response.Request = &projected
		}
	case "inspect":
		var value Request
		value, err = s.Service.InspectPeerQuery(ctx, sender, strings.TrimSpace(args.RequestID))
		if err == nil {
			projected := projectRequest(value)
			response.Request = &projected
		}
	case "wait":
		timeout := time.Duration(args.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		if timeout > 30*time.Second {
			err = fmt.Errorf("%w: wait timeout exceeds 30 seconds", ErrInvalid)
			break
		}
		var value Request
		value, response.TimedOut, err = s.Service.WaitPeerQuery(ctx, sender, strings.TrimSpace(args.RequestID), timeout)
		if err == nil {
			projected := projectRequest(value)
			response.Request = &projected
		}
	default:
		err = fmt.Errorf("%w: unsupported action %q", ErrInvalid, args.Action)
	}
	if err != nil {
		response.Error = err.Error()
	}
	raw, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: marshalErr.Error()}}, IsError: true}
	}
	result := droids.ToolText(string(raw))
	result.IsError = err != nil
	if details, detailsErr := droids.EncodeDetails(response); detailsErr == nil {
		result.Details = details
	}
	return result
}

var _ ToolFactory = (*ToolService)(nil)
