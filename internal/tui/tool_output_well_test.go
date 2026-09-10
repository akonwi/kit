package tui

import (
	"fmt"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type outputWellHarness struct{ State *outputWellHarnessState }

func (w outputWellHarness) CreateState() ui.State { return w.State }

type outputWellHarnessState struct {
	ui.StateBase
	outer  ui.ScrollController
	output string
	sticky bool
	rich   bool
}

func (s *outputWellHarnessState) Build(ui.BuildContext) ui.Widget {
	output := s.output
	if output == "" {
		lines := make([]string, 20)
		for index := range lines {
			lines[index] = fmt.Sprintf("output %02d", index+1)
		}
		output = strings.Join(lines, "\n")
	}
	well := toolOutputWell{
		Key:    activityToolKey{TurnID: "turn_1", ToolCallID: "call_1"},
		Output: output, OuterScroll: &s.outer, StickyBottom: s.sticky,
	}
	if s.rich {
		well.Content = codePresentation{Path: "output.txt", Lines: splitActivityLines(output)}
		well.Rich = true
	}
	children := []ui.Widget{well}
	for index := range 10 {
		children = append(children, ui.Text{Value: fmt.Sprintf("outer %02d", index+1)})
	}
	return ui.ScrollView{
		Controller: &s.outer,
		Child:      ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children},
	}
}

func TestEstimateToolOutputRowsAccountsForWrapping(t *testing.T) {
	t.Parallel()

	if got := estimateToolOutputRows("short\n123456789", 4); got != 5 {
		t.Fatalf("estimated wrapped rows = %d, want 5", got)
	}
	if got := estimateToolOutputRows("界界界", 4); got != 2 {
		t.Fatalf("wide-character wrapped rows = %d, want 2", got)
	}
}

func TestLiveToolOutputPreservesScrollWhenUserLeavesEnd(t *testing.T) {
	t.Parallel()

	const width, height = 30, 16
	state := &outputWellHarnessState{sticky: true}
	app := uitest.New(outputWellHarness{State: state})
	app.Pump(width, height)
	for range 6 {
		app.Send(vaxis.Mouse{Col: 2, Row: 2, Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress})
		app.Pump(width, height)
	}
	if rows := paintedRows(app, width, height); !strings.Contains(rows[0], "output 07") {
		t.Fatalf("live output did not reach end:\n%s", strings.Join(rows, "\n"))
	}

	app.Send(vaxis.Mouse{Col: 2, Row: 2, Button: vaxis.MouseWheelUp, EventType: vaxis.EventPress})
	app.Pump(width, height)
	if rows := paintedRows(app, width, height); !strings.Contains(rows[0], "output 06") {
		t.Fatalf("live output did not scroll away from end:\n%s", strings.Join(rows, "\n"))
	}

	state.SetState(func() { state.output = strings.Join(append(outputLines(20), "output 21"), "\n") })
	app.Pump(width, height)
	app.Pump(width, height)
	if rows := paintedRows(app, width, height); !strings.Contains(rows[0], "output 06") {
		t.Fatalf("live output update reset user scroll:\n%s", strings.Join(rows, "\n"))
	}
}

func outputLines(count int) []string {
	lines := make([]string, count)
	for index := range lines {
		lines[index] = fmt.Sprintf("output %02d", index+1)
	}
	return lines
}

func TestToolOutputWellCapsRowsAndHandsWheelToOuterScroll(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		rich bool
	}{{name: "plain"}, {name: "enriched", rich: true}} {
		t.Run(test.name, func(t *testing.T) {
			const width, height = 30, 6
			state := &outputWellHarnessState{rich: test.rich}
			app := uitest.New(outputWellHarness{State: state})
			app.Pump(width, height)
			app.Pump(width, height)
			rows := paintedRows(app, width, height)
			if !strings.Contains(rows[0], "output 01") || state.outer.Metrics().ScrollOffset != 0 {
				t.Fatalf("initial output well = outer %+v\n%s", state.outer.Metrics(), strings.Join(rows, "\n"))
			}

			app.Send(vaxis.Mouse{Col: 2, Row: 2, Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress})
			app.Pump(width, height)
			rows = paintedRows(app, width, height)
			if !strings.Contains(rows[0], "output 02") || state.outer.Metrics().ScrollOffset != 0 {
				t.Fatalf("inner wheel did not stay in output well = outer %+v\n%s", state.outer.Metrics(), strings.Join(rows, "\n"))
			}

			for range 30 {
				app.Send(vaxis.Mouse{Col: 2, Row: 2, Button: vaxis.MouseWheelDown, EventType: vaxis.EventPress})
				app.Pump(width, height)
			}
			if state.outer.Metrics().ScrollOffset == 0 {
				t.Fatalf("wheel at output edge did not hand off to outer scroll: %+v", state.outer.Metrics())
			}
		})
	}
}
