package tui

import (
	"fmt"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type progressiveActivityHarness struct{ state *progressiveActivityState }

func (w progressiveActivityHarness) CreateState() ui.State { return w.state }

type progressiveActivityState struct {
	ui.StateBase
	count     int
	running   bool
	overrides map[string]bool
}

func (s *progressiveActivityState) presentation() transcriptPresentation {
	calls := make([]transcriptToolCall, s.count)
	messages := []transcriptMessage{{ID: "assistant", TurnID: "turn", Role: "assistant"}}
	for i := range calls {
		calls[i] = transcriptToolCall{ID: fmt.Sprint(i), Name: "read", Arguments: []byte(fmt.Sprintf(`{"path":"file%d.go"}`, i))}
		messages = append(messages, transcriptMessage{ID: "result" + fmt.Sprint(i), TurnID: "turn", Role: "tool", ToolCallID: calls[i].ID, ToolStatus: "Completed"})
	}
	messages[0].ToolCalls = calls
	return presentTranscript(messages)
}
func (s *progressiveActivityState) Build(ctx ui.BuildContext) ui.Widget {
	p := s.presentation()
	view := shellView{Snapshot: shellSnapshot{InlineActivityOpen: s.overrides, ActiveToolSourceID: activeToolSource(p, s.running)}}
	view.Callbacks.OpenActivity = func(_ ui.EventContext, id string) {
		s.SetState(func() { s.overrides[id] = !activitySourceIsOpen(s.overrides, s.presentation(), id, s.running) })
	}
	return view.transcriptWorkChip(ui.MustDepend[ui.Theme](ctx), p.Items[0], p.ToolStates)
}

func TestProgressiveToolGroupExpansion(t *testing.T) {
	state := &progressiveActivityState{count: 1, running: true, overrides: map[string]bool{}}
	app := uitest.New(progressiveActivityHarness{state})
	check := func(header string, toolRows int) {
		t.Helper()
		app.Pump(80, 16)
		rows := paintedRows(app, 80, 16)
		if got := strings.TrimSpace(rows[0]); got != header {
			t.Fatalf("header = %q, want %q\n%s", got, header, strings.Join(rows, "\n"))
		}
		for i := 0; i < toolRows; i++ {
			if got := strings.TrimSpace(rows[i+1]); got != fmt.Sprintf("Read file           file%d.go · empty", i) {
				t.Fatalf("tool row %d = %q", i, got)
			}
		}
		if got := strings.TrimSpace(rows[toolRows+1]); got != "" {
			t.Fatalf("row after group = %q, want blank", got)
		}
	}
	check("▾ 1 tool call", 1)
	click := func() {
		app.Send(vaxis.Mouse{Col: 3, Row: 0, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
		app.Send(vaxis.Mouse{Col: 3, Row: 0, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
	}
	click()
	check("⠋ 1 tool call", 0)
	click()
	check("▾ 1 tool call", 1)
	state.SetState(func() { state.overrides = map[string]bool{} })
	state.SetState(func() { state.count = 5 })
	check("▾ 5 tool calls", 5)
	state.SetState(func() { state.count = 6 })
	check("⠋ 6 tool calls", 0)
	click()
	check("▾ 6 tool calls", 6)
	state.SetState(func() { state.count = 7 })
	check("▾ 7 tool calls", 7)
	state.SetState(func() { state.overrides = map[string]bool{}; state.running = false })
	check("› 7 tool calls", 0)
}

func TestManualCollapseOverridesSmallLiveGroup(t *testing.T) {
	state := &progressiveActivityState{count: 1, running: true, overrides: map[string]bool{}}
	p := state.presentation()
	id := p.Items[0].ID
	if !activitySourceIsOpen(state.overrides, p, id, true) {
		t.Fatal("small live group should start open")
	}
	state.overrides[id] = !activitySourceIsOpen(state.overrides, p, id, true)
	if activitySourceIsOpen(state.overrides, p, id, true) {
		t.Fatal("manual collapse should stay closed")
	}
	state.overrides[id] = !activitySourceIsOpen(state.overrides, p, id, true)
	if !activitySourceIsOpen(state.overrides, p, id, true) {
		t.Fatal("manual expansion should open")
	}
}

func TestOnlyLatestWorkGroupStaysActiveBetweenCalls(t *testing.T) {
	state := &progressiveActivityState{count: 1}
	p := state.presentation()
	first := p.Items[0]
	second := first
	second.ID = "later-work"
	p.Items = append(p.Items, transcriptDisplayItem{Kind: transcriptDisplayAssistantProse, TurnID: first.TurnID}, second)
	active := activeToolSource(p, true)
	if active != second.ID {
		t.Fatalf("active source = %q, want %q", active, second.ID)
	}
	if toolGroupInProgress(first, p.ToolStates, active) {
		t.Fatal("completed earlier group remained active")
	}
	if !toolGroupInProgress(second, p.ToolStates, active) {
		t.Fatal("latest group should stay active between calls")
	}
	p.Items = append(p.Items, transcriptDisplayItem{Kind: transcriptDisplaySingle, TurnID: "next-turn"})
	if got := activeToolSource(p, true); got != "" {
		t.Fatalf("new turn inherited source %q", got)
	}
}
