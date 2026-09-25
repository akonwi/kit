package tui

import (
	"encoding/json"
	"fmt"
	"testing"

	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestToolFileTargetUsesOnlySupportedCompleteArguments(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, tool, args string
		truncated, ok    bool
		path             string
		start, end       int
	}{
		{name: "read range", tool: "read", args: `{"path":"src/main.go","offset":12,"limit":8}`, ok: true, path: "src/main.go", start: 12, end: 19},
		{name: "read default", tool: "read", args: `{"path":"src/main.go"}`, ok: true, path: "src/main.go", start: 1},
		{name: "zero limit", tool: "read", args: `{"path":"src/main.go","limit":0}`, ok: true, path: "src/main.go", start: 1},
		{name: "write", tool: "write", args: `{"path":" file.go ","content":"text"}`, ok: true, path: " file.go "},
		{name: "edit", tool: "edit", args: `{"path":"src/main.go","edits":[]}`, ok: true, path: "src/main.go"},
		{name: "search excluded", tool: "grep", args: `{"path":"src/main.go"}`},
		{name: "directory excluded", tool: "ls", args: `{"path":"src"}`},
		{name: "truncated", tool: "read", args: `{"path":"wrong.go"}`, truncated: true},
		{name: "missing path", tool: "read", args: `{}`},
		{name: "malformed", tool: "read", args: `{"path":"x"`},
		{name: "wrong path type", tool: "read", args: `{"path":42}`},
		{name: "invalid offset", tool: "read", args: `{"path":"x","offset":0}`},
		{name: "fractional offset", tool: "read", args: `{"path":"x","offset":1.5}`},
		{name: "negative limit", tool: "read", args: `{"path":"x","limit":-1}`},
		{name: "overflow", tool: "read", args: fmt.Sprintf(`{"path":"x","offset":%d,"limit":2}`, int(^uint(0)>>1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := toolCallFileTarget(transcriptToolCall{Name: test.tool, Arguments: json.RawMessage(test.args), ArgumentsTruncated: test.truncated})
			if ok != test.ok || (ok && (got.Path != test.path || got.StartLine != test.start || got.EndLine != test.end)) {
				t.Fatalf("target=%+v ok=%t", got, ok)
			}
		})
	}
}

func TestCompletedFileChipPreservesResolvedPathAndDisplayedReadRange(t *testing.T) {
	t.Parallel()
	call := transcriptToolCall{Name: "read", Arguments: json.RawMessage(`{"path":"main.go","offset":12,"limit":20}`)}
	state := transcriptMessage{Role: "tool", Text: "one\ntwo", ToolDetails: json.RawMessage(`{"path":"/old/workspace/main.go","lines":2}`)}
	got, ok := toolRowFileTarget(call, state, true)
	if !ok || got.Path != "/old/workspace/main.go" || got.StartLine != 12 || got.EndLine != 13 {
		t.Fatalf("recorded target=%+v ok=%t", got, ok)
	}
	state.Pending = true
	got, ok = toolRowFileTarget(call, state, true)
	if !ok || got.Path != "main.go" || got.StartLine != 12 || got.EndLine != 31 {
		t.Fatalf("pending result must preserve argument target: %+v", got)
	}
	state.Pending = false
	state.ToolDetailsOmitted = true
	got, ok = toolRowFileTarget(call, state, true)
	if !ok || got.Path != "main.go" {
		t.Fatalf("omitted metadata must not override arguments: %+v", got)
	}
}

func TestFileChipBecomesClickableWhenLiveArgumentsComplete(t *testing.T) {
	t.Parallel()
	state := &fileChipTransitionState{call: transcriptToolCall{ID: "call", Name: "read", Arguments: json.RawMessage(`{"path":`)}}
	application := uitest.New(fileChipTransitionHarness{state})
	application.Pump(90, 6)
	state.SetState(func() { state.call.Arguments = json.RawMessage(`{"path":"completed.go"}`) })
	application.Pump(90, 6)
	column, row := findTextCell(t, paintedRows(application, 90, 6), "completed.go")
	application.Click(column, row)
	if state.opened.Path != "completed.go" {
		t.Fatalf("completed live arguments target=%+v", state.opened)
	}
}

type fileChipTransitionHarness struct{ state *fileChipTransitionState }

func (w fileChipTransitionHarness) CreateState() ui.State { return w.state }

type fileChipTransitionState struct {
	ui.StateBase
	call   transcriptToolCall
	opened toolFileTarget
}

func (s *fileChipTransitionState) Build(ui.BuildContext) ui.Widget {
	return activityToolRowWidget{Key: activityToolKey{TurnID: "turn", ToolCallID: "call"}, Call: s.call, OnOpenFile: func(_ ui.EventContext, target toolFileTarget) { s.opened = target }}
}
