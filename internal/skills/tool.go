package skills

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/droids"
)

const (
	// ActivateToolName is the stable model-facing skill activation tool name.
	ActivateToolName = "activate_skill"
)

type activateArgs struct {
	Name string `json:"name"`
}

type activateDetails struct {
	Name     string `json:"name"`
	Found    bool   `json:"found"`
	Source   Source `json:"source,omitempty"`
	Location string `json:"location,omitempty"`
}

// ActivateTool returns the skill activation tool bound to this registry.
func (r *Registry) ActivateTool() droids.AnyTool {
	return droids.MustTool(r.newActivateTool())
}

func (r *Registry) newActivateTool() droids.Tool[activateArgs] {
	return droids.Tool[activateArgs]{
		Name:        ActivateToolName,
		Description: "Load the full instructions for an available skill by its stable name.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Stable skill name from the available-skills catalog",
					"minLength":   1,
					"maxLength":   maxSkillNameBytes,
					"pattern":     "^[a-z0-9]+(?:-[a-z0-9]+)*$",
				},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		Mode: droids.ModeParallel,
		Execute: func(ctx context.Context, _ droids.ToolContext, args activateArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
			if err := ctx.Err(); err != nil {
				return droids.ToolResult{}, err
			}
			skill, ok := r.Lookup(args.Name)
			if !ok {
				available := r.modelInvocableSkills()
				names := make([]string, 0, len(available))
				for _, candidate := range available {
					names = append(names, candidate.Name)
				}
				message := fmt.Sprintf("Unknown skill %q.", args.Name)
				if len(names) > 0 {
					message += " Available skills: " + strings.Join(names, ", ") + "."
				}
				if len(message) > maxUnknownSkillResponseBytes {
					return droids.ToolResult{}, errors.New("unknown-skill response exceeds its configured limit")
				}
				return activationResult(message, activateDetails{Name: args.Name})
			}
			return activationResult(skill.Content, activateDetails{
				Name: skill.Name, Found: true, Source: skill.Source, Location: skill.Location,
			})
		},
	}
}

func activationResult(text string, details activateDetails) (droids.ToolResult, error) {
	encoded, err := droids.EncodeDetails(details)
	if err != nil {
		return droids.ToolResult{}, fmt.Errorf("encode skill activation details: %w", err)
	}
	return droids.ToolResult{
		Content: []droids.ResultContent{droids.TextContent{Text: text}},
		Details: encoded,
		IsError: !details.Found,
	}, nil
}
