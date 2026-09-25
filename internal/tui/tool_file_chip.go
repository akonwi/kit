package tui

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

// toolCallFileTarget uses original arguments, never the shortened display chip.
// Edits and writes do not imply a trustworthy line anchor in the current file.
func toolCallFileTarget(call transcriptToolCall) (toolFileTarget, bool) {
	kind := strings.ToLower(call.Name)
	if call.ArgumentsTruncated || (kind != "read" && kind != "write" && kind != "edit") {
		return toolFileTarget{}, false
	}
	var args struct {
		Path   string `json:"path"`
		Offset *int   `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	if json.Unmarshal(call.Arguments, &args) != nil || strings.TrimSpace(args.Path) == "" || strings.ContainsRune(args.Path, 0) {
		return toolFileTarget{}, false
	}
	target := toolFileTarget{Path: args.Path}
	if kind == "read" {
		target.StartLine = 1
		if args.Offset != nil {
			target.StartLine = *args.Offset
		}
		if target.StartLine < 1 || (args.Limit != nil && *args.Limit < 0) {
			return toolFileTarget{}, false
		}
		if args.Limit != nil && *args.Limit > 0 {
			maxInt := int(^uint(0) >> 1)
			if *args.Limit-1 > maxInt-target.StartLine {
				return toolFileTarget{}, false
			}
			target.EndLine = target.StartLine + *args.Limit - 1
		}
	}
	return target, true
}

// toolFileChip is pointer-only navigation. It introduces no focus target,
// keyboard shortcut, row disclosure, or change to the surrounding row style.
type toolFileChip struct {
	Text      string
	Style     ui.Style
	OnPressed ui.VoidCallback
}

func (toolFileChip) CreateState() ui.State { return &toolFileChipState{} }

type toolFileChipState struct {
	ui.StateBase
	hovered bool
}

func (s *toolFileChipState) Build(ctx ui.BuildContext) ui.Widget {
	w := s.Widget().(toolFileChip)
	theme := ui.MustDepend[ui.Theme](ctx)
	style := w.Style
	if s.hovered {
		style.Background = theme.SurfaceHovered
		style.Foreground = theme.Foreground
	}
	return mouseActivator{
		OnPressed: w.OnPressed,
		OnHover: func(ui.EventContext) {
			if !s.hovered {
				s.SetState(func() { s.hovered = true })
			}
		},
		OnHoverExit: func(ui.EventContext) {
			if s.hovered {
				s.SetState(func() { s.hovered = false })
			}
		},
		Child: ui.Text{Value: w.Text, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
	}
}

// Completed calls can carry the resolved absolute path. Preserve that evidence
// rather than rebasing an old relative argument onto a changed session cwd.
func toolRowFileTarget(call transcriptToolCall, state transcriptMessage, exists bool) (toolFileTarget, bool) {
	target, ok := toolCallFileTarget(call)
	if !ok {
		return target, false
	}
	if exists && !state.Pending && state.ToolStatus != "Not run" && !state.ToolDetailsOmitted {
		var details struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(state.ToolDetails, &details) == nil && filepath.IsAbs(details.Path) && !strings.ContainsRune(details.Path, 0) {
			target.Path = details.Path
		}
	}
	if strings.EqualFold(call.Name, "read") && exists && resolveActivityToolState(state, true, false) == activityToolSucceeded {
		_, lines, _ := activityReadContent(state)
		if lines > 0 && lines-1 <= int(^uint(0)>>1)-target.StartLine {
			target.EndLine = target.StartLine + lines - 1
		}
	}
	return target, true
}
