// Package sessiontool exposes bounded top-level session creation to models.
package sessiontool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/akonwi/kit/internal/droids"
)

const ToolName = "create_session"

// CreateInput is the server-owned session creation request accepted from the tool.
type CreateInput struct {
	OwnerSessionID string
	CWD            string
	Name           string
	Prompt         string
}

// Session is the bounded result projected to the model.
type Session struct {
	ID            string `json:"id"`
	CWD           string `json:"cwd"`
	Name          string `json:"name,omitempty"`
	Model         string `json:"model"`
	ThinkingLevel string `json:"thinkingLevel"`
	RunID         string `json:"runId,omitempty"`
}

// Service owns authoritative creation and optional initial prompt admission.
type Service interface {
	CreateModelSession(context.Context, CreateInput) (Session, error)
}

// ToolFactory constructs a session-creation tool bound to its owner.
type ToolFactory interface {
	Tool(string) (droids.AnyTool, error)
}

// ToolService adapts the server capability to the model-facing tool.
type ToolService struct{ Service Service }

func (s *ToolService) Tool(ownerSessionID string) (droids.AnyTool, error) {
	if s == nil || s.Service == nil {
		return nil, errors.New("create session tool service is not initialized")
	}
	return droids.NewTool(droids.Tool[arguments]{
		Name:        ToolName,
		Description: "Create a named persistent top-level Kit session using application model defaults. The cwd and name are required. An optional initial prompt starts asynchronously. The new session does not replace the current session.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cwd":    map[string]any{"type": "string", "description": "Existing absolute working directory for the new session."},
				"name":   map[string]any{"type": "string", "minLength": 1, "description": "Required session name."},
				"prompt": map[string]any{"type": "string", "description": "Optional initial prompt to start asynchronously."},
			},
			"required": []string{"cwd", "name"}, "additionalProperties": false,
		},
		Mode: droids.ModeSequential,
		Execute: func(ctx context.Context, _ droids.ToolContext, args arguments, _ droids.ToolUpdate) (droids.ToolResult, error) {
			return s.execute(ctx, ownerSessionID, args), nil
		},
	})
}

func (s *ToolService) execute(ctx context.Context, ownerSessionID string, args arguments) droids.ToolResult {
	created, err := s.Service.CreateModelSession(ctx, CreateInput{
		OwnerSessionID: ownerSessionID,
		CWD:            strings.TrimSpace(args.CWD), Name: strings.TrimSpace(args.Name),
		Prompt: args.Prompt,
	})
	response := response{Session: &created}
	if err != nil {
		response.Session = nil
		response.Error = err.Error()
	}
	raw, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		result := droids.ToolText(marshalErr.Error())
		result.IsError = true
		return result
	}
	result := droids.ToolText(string(raw))
	result.IsError = err != nil
	if details, detailsErr := droids.EncodeDetails(response); detailsErr == nil {
		result.Details = details
	}
	return result
}

type arguments struct {
	CWD    string `json:"cwd"`
	Name   string `json:"name,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

type response struct {
	Session *Session `json:"session,omitempty"`
	Error   string   `json:"error,omitempty"`
}

var _ ToolFactory = (*ToolService)(nil)
