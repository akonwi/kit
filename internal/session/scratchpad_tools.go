package session

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/scratchpad"
)

const (
	ReadScratchpadToolName = "read_scratchpad"
	EditScratchpadToolName = "edit_scratchpad"
)

type readScratchpadArgs struct{}

type scratchpadEditInput struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

type editScratchpadArgs struct {
	Edits []scratchpadEditInput `json:"edits"`
}

type scratchpadToolDetails struct {
	OwnerSessionID string   `json:"ownerSessionId,omitempty"`
	Revision       string   `json:"revision,omitempty"`
	UpdatedAt      string   `json:"updatedAt,omitempty"`
	Applied        int      `json:"applied,omitempty"`
	Errors         []string `json:"errors,omitempty"`
}

func (m *Manager) boundSessionTools(record SessionRecord, scope *workspaceScope) []droids.AnyTool {
	tools := []droids.AnyTool{m.changeCWDTool(record.ID, scope)}
	if record.Persistent && m.scratchpads != nil {
		tools = append(tools, m.scratchpadTools(record.ID)...)
	}
	return tools
}

func (m *Manager) scratchpadTools(sessionID string) []droids.AnyTool {
	return []droids.AnyTool{
		droids.MustTool(droids.Tool[readScratchpadArgs]{
			Name:        ReadScratchpadToolName,
			Description: "Read the shared scratchpad for this persistent session family. Scratchpad content is not included in prompts automatically.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false, "properties": map[string]any{},
			},
			Mode: droids.ModeParallel,
			Execute: func(ctx context.Context, _ droids.ToolContext, _ readScratchpadArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
				record, err := m.Scratchpad(ctx, sessionID)
				if err != nil {
					return scratchpadToolFailure(ctx, err, nil)
				}
				details, err := droids.EncodeDetails(projectScratchpadToolDetails(record, 0, nil))
				if err != nil {
					return droids.ToolResult{}, fmt.Errorf("encode scratchpad read details: %w", err)
				}
				text := fmt.Sprintf("Scratchpad is empty (revision %d).", record.Revision)
				if record.Content != "" {
					text = fmt.Sprintf("Scratchpad revision %d:\n\n%s", record.Revision, record.Content)
				}
				return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: text}}, Details: details}, nil
			},
		}),
		droids.MustTool(droids.Tool[editScratchpadArgs]{
			Name:        EditScratchpadToolName,
			Description: "Edit the shared scratchpad using exact text replacements. Each oldText must match exactly and uniquely. Edits are applied simultaneously to the latest committed content. To initialize an empty scratchpad, use one edit with empty oldText.",
			Parameters: map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"edits": map[string]any{
					"type": "array", "minItems": 1, "maxItems": scratchpad.MaxEditCount,
					"items": map[string]any{
						"type": "object", "additionalProperties": false,
						"properties": map[string]any{
							"oldText": map[string]any{"type": "string", "maxLength": scratchpad.MaxEditFieldBytes, "description": "Exact text to replace; must be unique in the current scratchpad."},
							"newText": map[string]any{"type": "string", "maxLength": scratchpad.MaxEditFieldBytes, "description": "Replacement text."},
						},
						"required": []string{"oldText", "newText"},
					},
				}},
				"required": []string{"edits"},
			},
			Mode: droids.ModeSequential,
			Execute: func(ctx context.Context, _ droids.ToolContext, args editScratchpadArgs, _ droids.ToolUpdate) (droids.ToolResult, error) {
				edits := make([]scratchpad.Edit, len(args.Edits))
				for index, edit := range args.Edits {
					edits[index] = scratchpad.Edit{OldText: edit.OldText, NewText: edit.NewText}
				}
				record, applied, err := m.EditScratchpad(ctx, sessionID, edits)
				if err != nil {
					return scratchpadToolFailure(ctx, err, nil)
				}
				details, err := droids.EncodeDetails(projectScratchpadToolDetails(record, applied, nil))
				if err != nil {
					return droids.ToolResult{}, fmt.Errorf("encode scratchpad edit details: %w", err)
				}
				text := fmt.Sprintf("Applied %d edit%s to the shared scratchpad. Revision: %d.", applied, pluralScratchpadEdits(applied), record.Revision)
				return droids.ToolResult{Content: []droids.ResultContent{droids.TextContent{Text: text}}, Details: details}, nil
			},
		}),
	}
}

func projectScratchpadToolDetails(record scratchpad.Record, applied int, problems []string) scratchpadToolDetails {
	return scratchpadToolDetails{
		OwnerSessionID: record.OwnerSessionID,
		Revision:       strconv.FormatInt(record.Revision, 10),
		UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Applied:        applied,
		Errors:         problems,
	}
}

func scratchpadToolFailure(ctx context.Context, err error, record *scratchpad.Record) (droids.ToolResult, error) {
	if ctx.Err() != nil {
		return droids.ToolResult{}, ctx.Err()
	}
	problems := []string{"scratchpad operation failed"}
	var editErr *scratchpad.EditError
	switch {
	case errors.As(err, &editErr):
		problems = append([]string(nil), editErr.Problems...)
	case errors.Is(err, scratchpad.ErrContentTooLarge):
		problems = []string{"edited scratchpad would exceed 64 KiB"}
	case errors.Is(err, scratchpad.ErrInvalidContent):
		problems = []string{"edited scratchpad content is invalid"}
	case errors.Is(err, scratchpad.ErrRevisionExhausted):
		problems = []string{"scratchpad revision is exhausted"}
	case errors.Is(err, scratchpad.ErrMigrationRequired):
		problems = []string{"scratchpad migration is required"}
	case errors.Is(err, scratchpad.ErrUnsupported):
		problems = []string{"scratchpad is unavailable for this session"}
	}
	detailsValue := scratchpadToolDetails{Errors: problems}
	if record != nil {
		detailsValue = projectScratchpadToolDetails(*record, 0, problems)
	}
	details, encodeErr := droids.EncodeDetails(detailsValue)
	if encodeErr != nil {
		return droids.ToolResult{}, fmt.Errorf("encode scratchpad error details: %w", encodeErr)
	}
	return droids.ToolResult{
		Content: []droids.ResultContent{droids.TextContent{Text: "Error:\n" + strings.Join(problems, "\n")}},
		Details: details, IsError: true,
	}, nil
}

func pluralScratchpadEdits(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
