package codingtools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/akonwi/kit/internal/droids"
)

const maxEditFileBytes = 32 << 20

type exactEdit struct {
	OldText string
	NewText string
}

type editInput struct {
	OldText *string `json:"oldText"`
	NewText *string `json:"newText"`
}

type editArgs struct {
	Path    string      `json:"path"`
	Edits   []editInput `json:"edits,omitempty"`
	OldText *string     `json:"oldText,omitempty"`
	NewText *string     `json:"newText,omitempty"`
}

type editDetails struct {
	Path    string   `json:"path"`
	Applied int      `json:"applied"`
	Errors  []string `json:"errors"`
}

type editRange struct {
	exactEdit
	index int
	start int
	end   int
}

func newEditTool(cwd string) droids.Tool[editArgs] {
	return droids.Tool[editArgs]{
		Name:        EditToolName,
		Description: "Edit a file using exact text replacements. Each edit's oldText must match exactly and be unique. Multiple edits are applied to the original file simultaneously — do not include overlapping edits.",
		Parameters: objectSchema(map[string]any{
			"path": stringSchema("Path to the file to edit (relative or absolute)"),
			"edits": map[string]any{
				"type":        "array",
				"description": "One or more targeted replacements. Each edit is matched against the original file, not incrementally. Merge nearby changes into one edit instead of overlapping edits.",
				"minItems":    1,
				"items": objectSchema(map[string]any{
					"oldText": stringSchema("Exact text for one targeted replacement. Must be unique in the file and must not overlap with other edits in the same call."),
					"newText": stringSchema("Replacement text for this targeted edit."),
				}, "oldText", "newText"),
			},
		}, "path", "edits"),
		Mode: droids.ModeSequential,
		Execute: func(ctx context.Context, args editArgs) (droids.ToolResult, error) {
			if err := ctx.Err(); err != nil {
				return droids.ToolResult{}, err
			}
			path, err := resolvePath(cwd, args.Path)
			if err != nil {
				return errorResult(err, editDetails{Path: args.Path}), nil
			}
			edits, err := normalizedEdits(args)
			if err != nil {
				return editFailure(path, []string{err.Error()}), nil
			}
			var applied int
			var editErrors []string
			err = withMutationLock(ctx, path, func() error {
				if err := ctx.Err(); err != nil {
					return err
				}
				original, version, err := readRegularFileVersion(ctx, path, maxEditFileBytes)
				if err != nil {
					return err
				}
				updated, errors := applyExactEdits(string(original), edits)
				if len(errors) > 0 {
					editErrors = errors
					return nil
				}
				if updated != string(original) {
					if err := replaceRegularFile(ctx, path, []byte(updated), 0o666, &version); err != nil {
						return err
					}
				}
				applied = len(edits)
				return nil
			})
			if err != nil {
				if ctx.Err() != nil {
					return droids.ToolResult{}, ctx.Err()
				}
				return errorResult(err, editDetails{Path: path, Errors: []string{err.Error()}}), nil
			}
			if len(editErrors) > 0 {
				return editFailure(path, editErrors), nil
			}
			return textResult(
				fmt.Sprintf("Applied %d edit%s to %s", applied, plural(applied), args.Path),
				editDetails{Path: path, Applied: applied, Errors: []string{}},
			), nil
		},
	}
}

func normalizedEdits(args editArgs) ([]exactEdit, error) {
	if len(args.Edits) > 0 {
		edits := make([]exactEdit, 0, len(args.Edits))
		for index, edit := range args.Edits {
			if edit.OldText == nil {
				return nil, fmt.Errorf("edits[%d]: oldText is required", index)
			}
			if edit.NewText == nil {
				return nil, fmt.Errorf("edits[%d]: newText is required", index)
			}
			edits = append(edits, exactEdit{OldText: *edit.OldText, NewText: *edit.NewText})
		}
		return edits, nil
	}
	if args.OldText != nil && args.NewText != nil {
		return []exactEdit{{OldText: *args.OldText, NewText: *args.NewText}}, nil
	}
	return nil, fmt.Errorf("edits must contain at least one replacement")
}

func applyExactEdits(original string, edits []exactEdit) (string, []string) {
	errors := make([]string, 0)
	ranges := make([]editRange, 0, len(edits))
	for index, edit := range edits {
		if edit.OldText == "" {
			errors = append(errors, fmt.Sprintf("edits[%d]: oldText must not be empty", index))
			continue
		}
		offsets := matchOffsets(original, edit.OldText)
		switch len(offsets) {
		case 0:
			errors = append(errors, fmt.Sprintf("edits[%d]: oldText not found in file", index))
		case 1:
			ranges = append(ranges, editRange{exactEdit: edit, index: index, start: offsets[0], end: offsets[0] + len(edit.OldText)})
		default:
			errors = append(errors, fmt.Sprintf("edits[%d]: oldText matches %d locations — must be unique", index, len(offsets)))
		}
	}
	for left := 0; left < len(ranges); left++ {
		for right := left + 1; right < len(ranges); right++ {
			if ranges[left].start < ranges[right].end && ranges[right].start < ranges[left].end {
				errors = append(errors, fmt.Sprintf("edits[%d] and edits[%d] overlap — merge them into one edit", ranges[left].index, ranges[right].index))
			}
		}
	}
	if len(errors) > 0 {
		return original, errors
	}
	sort.Slice(ranges, func(left, right int) bool { return ranges[left].start > ranges[right].start })
	updated := original
	for _, replacement := range ranges {
		updated = updated[:replacement.start] + replacement.NewText + updated[replacement.end:]
	}
	return updated, nil
}

func matchOffsets(content, search string) []int {
	var offsets []int
	for from := 0; from <= len(content)-len(search); {
		index := strings.Index(content[from:], search)
		if index < 0 {
			break
		}
		index += from
		offsets = append(offsets, index)
		from = index + 1
	}
	return offsets
}

func editFailure(path string, errors []string) droids.ToolResult {
	return droids.ToolResult{
		Content: []droids.Content{droids.TextContent{Text: "Error:\n" + strings.Join(errors, "\n")}},
		Details: editDetails{Path: path, Errors: errors},
		IsError: true,
	}
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
