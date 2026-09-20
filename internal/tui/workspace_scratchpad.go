package tui

import (
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

const scratchpadAutosaveDelayMilliseconds = 250

type scratchpadSaveState uint8

const (
	scratchpadLoading scratchpadSaveState = iota
	scratchpadSaved
	scratchpadUnsaved
	scratchpadSaving
	scratchpadConflict
	scratchpadSaveFailed
)

type scratchpadEditorState struct {
	Authoritative protocol.Scratchpad
	Draft         string
	State         scratchpadSaveState
	Conflict      *protocol.Scratchpad
	Review        bool
	Failure       string
	Initialized   bool
}

func (s *scratchpadEditorState) reset(record *protocol.Scratchpad) {
	*s = scratchpadEditorState{State: scratchpadLoading}
	if record != nil {
		s.Authoritative = *record
		s.Draft = record.Content
		s.State = scratchpadSaved
		s.Initialized = true
	}
}

// reconcile applies only a newer authoritative record and never discards a
// divergent local draft.
func (s *scratchpadEditorState) reconcile(record protocol.Scratchpad) bool {
	if s.Initialized && record.Revision <= s.Authoritative.Revision {
		return false
	}
	if !s.Initialized {
		s.Authoritative = record
		s.Draft = record.Content
		s.State = scratchpadSaved
		s.Initialized = true
		return true
	}
	dirty := s.Draft != s.Authoritative.Content
	s.Authoritative = record
	if !dirty || s.Draft == record.Content {
		s.Draft = record.Content
		s.State = scratchpadSaved
		s.Conflict = nil
		s.Review = false
		s.Failure = ""
		return true
	}
	copy := record
	s.Conflict = &copy
	s.State = scratchpadConflict
	s.Failure = ""
	return true
}

func (s *scratchpadEditorState) edit(content string) {
	s.Draft = content
	s.Review = false
	s.Failure = ""
	if s.Initialized && content == s.Authoritative.Content {
		s.State = scratchpadSaved
		s.Conflict = nil
		return
	}
	if s.Conflict != nil {
		s.State = scratchpadConflict
		return
	}
	s.State = scratchpadUnsaved
}

func (s *scratchpadEditorState) useShared() {
	if s.Conflict == nil {
		return
	}
	s.Authoritative = *s.Conflict
	s.Draft = s.Conflict.Content
	s.State = scratchpadSaved
	s.Conflict = nil
	s.Review = false
	s.Failure = ""
}

func scratchpadWorkspacePane() workspacePaneDescriptor {
	return workspacePaneDescriptor{Kind: workspacePaneScratchpad}
}

type workspaceScratchpadPane struct {
	Presentation workspacePanePresentation
	Scratchpad   sessionclient.ScratchpadSession
	Editor       scratchpadEditorState
	OnChanged    ui.TextChangedCallback
	OnRetry      ui.VoidCallback
	OnReview     ui.VoidCallback
	OnKeep       ui.VoidCallback
	OnUseShared  ui.VoidCallback
	OnReplace    ui.VoidCallback
	OnFocus      ui.VoidCallback
}

func (w workspaceScratchpadPane) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	body := ui.Widget(ui.Center(ui.Text{Value: "Loading scratchpad…", Style: ui.Style{Foreground: theme.MutedForeground}}))
	if w.Editor.Initialized {
		if w.Editor.Review && w.Editor.Conflict != nil {
			body = scratchpadConflictReview(theme, w.Editor.Conflict.Content, w.Editor.Draft, w.OnKeep, w.OnUseShared, w.OnReplace)
		} else {
			body = scratchpadEditor(theme, w)
		}
	}
	return workspacePanelLayout{
		Header: ui.SizedBox{},
		Body:   body,
		Footer: scratchpadFooter(theme, w.Editor, w.OnRetry),
	}
}

func scratchpadEditor(theme ui.Theme, w workspaceScratchpadPane) ui.Widget {
	editor := ui.Widget(controlFocusScope{Child: ui.TextArea{
		Value: w.Editor.Draft, OnChanged: w.OnChanged,
		Padding: ui.Symmetric(1, 0), MinHeight: 1, MaxHeight: 65536,
		SoftWrap: true, AutoFocus: w.Presentation.Active && w.Presentation.Focused,
	}})
	if w.OnFocus != nil {
		editor = mouseActivator{OnPressed: w.OnFocus, Child: editor}
	}
	children := []ui.Widget{ui.Expanded(editor)}
	if w.Editor.State == scratchpadConflict {
		children = append([]ui.Widget{ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			ui.Expanded(ui.Text{Value: "Shared scratchpad changed. Autosave is paused.", Style: ui.Style{Foreground: theme.WarningText}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}),
			plainButton{Label: "Review changes", OnPressed: w.OnReview},
		}})}, children...)
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
}

func scratchpadConflictReview(theme ui.Theme, shared, draft string, keep, useShared, replace ui.VoidCallback) ui.Widget {
	lines, ok := buildActivityDiff(shared, draft)
	if !ok {
		lines = make([]activityDiffLine, 0, strings.Count(shared, "\n")+strings.Count(draft, "\n")+2)
		for _, line := range splitActivityDiffLines(shared) {
			lines = append(lines, activityDiffLine{Kind: activityDiffDelete, Text: line})
		}
		for _, line := range splitActivityDiffLines(draft) {
			lines = append(lines, activityDiffLine{Kind: activityDiffAdd, Text: line})
		}
	}
	diff := ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Text{Value: "--- shared\n+++ mine", Style: ui.Style{Foreground: theme.MutedForeground}},
		unifiedDiffPresentation{Lines: lines},
	}}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Expanded(ui.Scrollbar{Child: ui.ScrollView{Child: ui.Padding(ui.Symmetric(1, 0), ui.SelectionArea{Child: diff})}}),
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			plainButton{Label: "Keep editing", OnPressed: keep}, ui.SizedBox{Width: 1},
			plainButton{Label: "Use shared", OnPressed: useShared}, ui.Expanded(ui.SizedBox{}),
			plainButton{Label: "Replace shared with mine", OnPressed: replace},
		}}),
	}}
}

func scratchpadFooter(theme ui.Theme, editor scratchpadEditorState, retry ui.VoidCallback) ui.Widget {
	status := "Loading"
	switch editor.State {
	case scratchpadSaved:
		status = ""
	case scratchpadUnsaved:
		status = "Unsaved"
	case scratchpadSaving:
		status = "Saving…"
	case scratchpadConflict:
		status = "Conflict"
	case scratchpadSaveFailed:
		status = "Save failed"
		if editor.Failure != "" {
			status += ": " + editor.Failure
		}
	}
	left := ui.Widget(ui.Text{Value: status, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	if editor.State == scratchpadConflict {
		left = ui.Text{Value: status, Style: ui.Style{Foreground: theme.WarningText}, MaxLines: 1}
	} else if editor.State == scratchpadSaveFailed {
		left = ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			ui.Flexible(ui.Text{Value: status, Style: ui.Style{Foreground: theme.DangerText}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}),
			ui.SizedBox{Width: 1}, plainButton{Label: "Retry", OnPressed: retry},
		}}
	}
	return workspacePanelFooterWidget(theme, left, "tab composer · shift+tab scratchpad")
}

func workspacePanelFooterWidget(theme ui.Theme, left ui.Widget, right string) ui.Widget {
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			ui.Expanded(left),
			ui.Text{Value: right, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis},
		}}),
	}}
}
