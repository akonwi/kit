package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type toolFileActivityHarness struct {
	ui.StateBase
	window inlineActivityWindow
	before *ui.FocusNode
	after  *ui.FocusNode
}

func (s *toolFileActivityHarness) Build(ui.BuildContext) ui.Widget {
	if s.before == nil || s.after == nil {
		return s.window
	}
	return ui.Flex{Axis: ui.Vertical, Children: []ui.Widget{
		ui.Focus(s.before, ui.Text{Value: "before"}),
		s.window,
		ui.Focus(s.after, ui.Text{Value: "after"}),
	}}
}

func toolFileActivityWindow(call transcriptToolCall, result transcriptMessage, opened func(toolFileTarget)) inlineActivityWindow {
	messages := []transcriptMessage{
		{ID: "assistant", TurnID: "turn", Role: "assistant", ToolCalls: []transcriptToolCall{call}},
		result,
	}
	presentation := presentTranscript(messages)
	return inlineActivityWindow{
		ID: "turn-work:turn:assistant", Source: presentation.Items[0], States: presentation.ToolStates,
		Expanded:   map[activityToolKey]bool{{TurnID: "turn", ToolCallID: call.ID}: true},
		OnOpenFile: func(_ ui.EventContext, target toolFileTarget) { opened(target) },
	}
}

func TestToolFileActivityLiveAndRestoredChipsOpenReadWriteEditTargets(t *testing.T) {
	for _, test := range []struct {
		name  string
		call  transcriptToolCall
		state transcriptMessage
		want  toolFileTarget
	}{
		{
			name:  "live read",
			call:  transcriptToolCall{ID: "read", Name: "read", Arguments: json.RawMessage(`{"path":"src/live.go","offset":4,"limit":3}`)},
			state: transcriptMessage{ID: "read-result", TurnID: "turn", Role: "tool", ToolCallID: "read", ToolName: "read", ToolStatus: "Running…", Pending: true},
			want:  toolFileTarget{Path: "src/live.go", StartLine: 4, EndLine: 6},
		},
		{
			name:  "live write",
			call:  transcriptToolCall{ID: "write-live", Name: "write", Arguments: json.RawMessage(`{"path":"src/live-write.go","content":"package src"}`)},
			state: transcriptMessage{ID: "write-live-result", TurnID: "turn", Role: "tool", ToolCallID: "write-live", ToolName: "write", ToolStatus: "Running…", Pending: true},
			want:  toolFileTarget{Path: "src/live-write.go"},
		},
		{
			name:  "live edit",
			call:  transcriptToolCall{ID: "edit-live", Name: "edit", Arguments: json.RawMessage(`{"path":"src/live-edit.go","edits":[]}`)},
			state: transcriptMessage{ID: "edit-live-result", TurnID: "turn", Role: "tool", ToolCallID: "edit-live", ToolName: "edit", ToolStatus: "Running…", Pending: true},
			want:  toolFileTarget{Path: "src/live-edit.go"},
		},
		{
			name:  "restored read",
			call:  transcriptToolCall{ID: "read-restored", Name: "read", Arguments: json.RawMessage(`{"path":"src/restored-read.go"}`)},
			state: transcriptMessage{ID: "read-restored-result", TurnID: "turn", Role: "tool", ToolCallID: "read-restored", ToolName: "read", ToolStatus: "Completed", ToolDetails: json.RawMessage(`{"path":"/repo/src/restored-read.go"}`)},
			want:  toolFileTarget{Path: "/repo/src/restored-read.go"},
		},
		{
			name:  "restored write",
			call:  transcriptToolCall{ID: "write", Name: "write", Arguments: json.RawMessage(`{"path":"src/restored.go","content":"package src"}`)},
			state: transcriptMessage{ID: "write-result", TurnID: "turn", Role: "tool", ToolCallID: "write", ToolName: "write", ToolStatus: "Completed", ToolDetails: json.RawMessage(`{"path":"/repo/src/restored.go"}`)},
			want:  toolFileTarget{Path: "/repo/src/restored.go"},
		},
		{
			name:  "restored edit",
			call:  transcriptToolCall{ID: "edit", Name: "edit", Arguments: json.RawMessage(`{"path":"src/edit.go","edits":[]}`)},
			state: transcriptMessage{ID: "edit-result", TurnID: "turn", Role: "tool", ToolCallID: "edit", ToolName: "edit", ToolStatus: "Completed", ToolDetails: json.RawMessage(`{"path":"/repo/src/edit.go"}`)},
			want:  toolFileTarget{Path: "/repo/src/edit.go"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var opened []toolFileTarget
			state := &toolFileActivityHarness{window: toolFileActivityWindow(test.call, test.state, func(target toolFileTarget) { opened = append(opened, target) })}
			app := uitest.New(state)
			app.Pump(100, 8)
			if len(opened) != 0 {
				t.Fatalf("tool chip opened automatically: %+v", opened)
			}
			column, row := findTextCell(t, paintedRows(app, 100, 8), displayToolPath(test.call))
			app.Click(column, row)
			app.Pump(100, 8)
			if len(opened) != 1 || opened[0].Path != test.want.Path {
				t.Fatalf("opened targets = %+v, want path %q", opened, test.want.Path)
			}
		})
	}
}

func displayToolPath(call transcriptToolCall) string {
	var args struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(call.Arguments, &args)
	return args.Path
}

func TestToolFileActivityChipHoverDoesNotHighlightRowOrAddFocus(t *testing.T) {
	call := transcriptToolCall{ID: "read", Name: "read", Arguments: json.RawMessage(`{"path":"src/hover.go"}`)}
	result := transcriptMessage{ID: "result", TurnID: "turn", Role: "tool", ToolCallID: "read", ToolName: "read", ToolStatus: "Completed"}
	before, after := &ui.FocusNode{}, &ui.FocusNode{}
	state := &toolFileActivityHarness{window: toolFileActivityWindow(call, result, func(toolFileTarget) {}), before: before, after: after}
	app := uitest.New(state)
	app.Pump(100, 8)
	before.RequestFocus()
	app.Pump(100, 8)
	if !before.HasFocus() {
		t.Fatal("mounted leading focus control did not receive focus")
	}
	column, row := findTextCell(t, paintedRows(app, 100, 8), "src/hover.go")
	rowBackground := app.Cell(0, row).Style.Background
	chipBackground := app.Cell(column, row).Style.Background

	app.Send(ui.Mouse{Col: column, Row: row, EventType: ui.EventMotion})
	app.Pump(100, 8)
	if app.Cell(0, row).Style.Background != rowBackground {
		t.Fatal("hovering the file chip highlighted the activity row")
	}
	if app.Cell(column, row).Style.Background == chipBackground {
		t.Fatal("hovering the file chip did not show chip-only hover feedback")
	}
	app.Click(column, row)
	app.Pump(100, 8)
	if !before.HasFocus() || after.HasFocus() {
		t.Fatal("clicking the file chip changed focus")
	}
	app.Send(ui.Key{Keycode: vaxis.KeyTab})
	app.Pump(100, 8)
	if !after.HasFocus() {
		t.Fatal("Tab did not skip the file chip and focus the mounted trailing control")
	}
}

func TestToolFileActivityNarrowChipTruncatesDisplayButPreservesOriginalPath(t *testing.T) {
	path := "docs/design/a-very-long-original-tool-path.go"
	call := transcriptToolCall{ID: "read", Name: "read", Arguments: json.RawMessage(`{"path":"` + path + `"}`)}
	result := transcriptMessage{ID: "result", TurnID: "turn", Role: "tool", ToolCallID: "read", ToolName: "read", ToolStatus: "Completed"}
	var opened toolFileTarget
	state := &toolFileActivityHarness{window: toolFileActivityWindow(call, result, func(target toolFileTarget) { opened = target })}
	app := uitest.New(state)
	app.Pump(36, 8)
	text := strings.Join(paintedRows(app, 36, 8), "\n")
	if strings.Contains(text, path) || !strings.Contains(text, "…") {
		t.Fatalf("narrow chip was not truncated:\n%s", text)
	}
	column, row := findTextCell(t, paintedRows(app, 36, 8), "docs/design")
	app.Click(column, row)
	app.Pump(36, 8)
	if opened.Path != path {
		t.Fatalf("narrow chip changed callback path = %q, want %q", opened.Path, path)
	}
}

func TestToolFileActivityRightClickIsIgnored(t *testing.T) {
	call := transcriptToolCall{ID: "read", Name: "read", Arguments: json.RawMessage(`{"path":"src/right-click.go"}`)}
	result := transcriptMessage{ID: "result", TurnID: "turn", Role: "tool", ToolCallID: "read", ToolName: "read", ToolStatus: "Completed"}
	opened := 0
	before, after := &ui.FocusNode{}, &ui.FocusNode{}
	state := &toolFileActivityHarness{window: toolFileActivityWindow(call, result, func(toolFileTarget) { opened++ }), before: before, after: after}
	app := uitest.New(state)
	app.Pump(100, 8)
	before.RequestFocus()
	app.Pump(100, 8)
	column, row := findTextCell(t, paintedRows(app, 100, 8), "src/right-click.go")
	app.Send(ui.Mouse{Col: column, Row: row, Button: ui.MouseRightButton, EventType: ui.EventPress})
	app.Pump(100, 8)
	app.Send(ui.Key{Keycode: vaxis.KeyTab})
	app.Pump(100, 8)
	if opened != 0 {
		t.Fatalf("right click opened file %d times", opened)
	}
	if !after.HasFocus() || before.HasFocus() {
		t.Fatal("right click did not preserve focus traversal around the file chip")
	}
}
