package tui

import (
	"fmt"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestWorkspaceHostUsesSameFullWidthTabbedGeometryAtEveryWidth(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 140} {
		width := width
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			t.Parallel()
			layout := &workspaceLayoutState{}
			application := uitest.New(conversationWorkspaceHost{
				Open: true, ActivitySelected: true,
				Tabs: ui.Text{Value: "Agent  [reviewer ×]"}, Transcript: ui.Text{Value: "AGENT CONTENT"},
				Activity: ui.Text{Value: "SUBAGENT CONTENT"}, Pending: ui.Text{Value: "PENDING"}, PendingHeight: 1,
				ComposerSeparator: ui.Text{Value: strings.Repeat("─", width)},
				Composer:          ui.Text{Value: "COMPOSER"}, ComposerHeightLimit: 1, LayoutState: layout,
			})
			application.Pump(width, 10)
			rows := paintedRows(application, width, 10)
			want := map[int]string{
				0: "Agent  [reviewer ×]",
				2: "SUBAGENT CONTENT",
				7: "PENDING",
				8: strings.Repeat("─", width),
				9: "COMPOSER",
			}
			for row, prefix := range want {
				if !strings.HasPrefix(rows[row], prefix) {
					t.Fatalf("row %d = %q, want prefix %q\n%s", row, rows[row], prefix, strings.Join(rows, "\n"))
				}
			}
			if layout.TranscriptVisible || !layout.ActivityVisible {
				t.Fatalf("layout state = %+v, want one full-width activity surface", *layout)
			}
		})
	}
}

func TestWorkspaceHostOmitsTabsForAgentOnlyShell(t *testing.T) {
	t.Parallel()
	layout := &workspaceLayoutState{}
	application := uitest.New(conversationWorkspaceHost{
		Tabs: ui.Text{Value: "UNEXPECTED TAB STRIP"}, Transcript: ui.Text{Value: "AGENT CONTENT"},
		Activity: ui.Text{Value: "UNEXPECTED ACTIVITY"}, Pending: ui.Text{Value: "PENDING"}, PendingHeight: 1,
		ComposerSeparator: ui.Text{Value: strings.Repeat("─", 80)},
		Composer:          ui.Text{Value: "COMPOSER"}, ComposerHeightLimit: 1, LayoutState: layout,
	})
	application.Pump(80, 8)
	rows := paintedRows(application, 80, 8)
	want := map[int]string{
		0: "AGENT CONTENT",
		5: "PENDING",
		6: strings.Repeat("─", 80),
		7: "COMPOSER",
	}
	for row, prefix := range want {
		if !strings.HasPrefix(rows[row], prefix) {
			t.Fatalf("row %d = %q, want prefix %q\n%s", row, rows[row], prefix, strings.Join(rows, "\n"))
		}
	}
	if !layout.TranscriptVisible || layout.ActivityVisible {
		t.Fatalf("layout state = %+v, want Agent only", *layout)
	}
}

type workspaceOffstageTranscriptHarness struct {
	State *workspaceOffstageTranscriptState
}

func (w workspaceOffstageTranscriptHarness) CreateState() ui.State { return w.State }

type workspaceOffstageTranscriptState struct {
	ui.StateBase
	activitySelected bool
	scroll           ui.ScrollController
	list             ui.SliverListController
	messages         []transcriptMessage
	activityScrolls  int
}

func (s *workspaceOffstageTranscriptState) InitState() {
	s.messages = make([]transcriptMessage, 48)
	for index := range s.messages {
		s.messages[index] = transcriptMessage{ID: fmt.Sprintf("assistant_%02d", index), TurnID: "turn", Role: "assistant", Text: strings.Repeat(fmt.Sprintf("transcript line %02d ", index), 1+index%4)}
	}
}

func (s *workspaceOffstageTranscriptState) Build(ctx ui.BuildContext) ui.Widget {
	view := shellView{}
	transcript := view.transcriptList(ui.MustDepend[ui.Theme](ctx), presentTranscript(s.messages), true, "session:test", &s.scroll, &s.list, true, nil)
	activity := mouseActivator{Child: ui.Text{Value: "DIFF CONTENT"}, OnScroll: func(ui.EventContext, ui.Mouse) ui.EventResult {
		s.SetState(func() { s.activityScrolls++ })
		return ui.EventHandled
	}}
	return conversationWorkspaceHost{
		Open: true, ActivitySelected: s.activitySelected,
		Tabs: ui.Text{Value: "Agent  Diff"}, Transcript: transcript, Activity: activity,
		Pending: ui.SizedBox{}, ComposerSeparator: ui.SizedBox{}, Composer: ui.SizedBox{}, ComposerHeightLimit: 1,
	}
}

func TestWorkspaceHostKeepsTranscriptLaidOutOffstage(t *testing.T) {
	state := &workspaceOffstageTranscriptState{}
	application := uitest.New(workspaceOffstageTranscriptHarness{State: state})
	application.Pump(80, 12)
	for range 3 {
		state.scroll.ScrollToEnd()
		application.Pump(80, 12)
	}
	state.scroll.ScrollByLines(-12)
	application.Pump(80, 12)
	before := state.scroll.Metrics()
	first, _, visible := state.list.VisibleRange()
	offset, measured := state.list.OffsetForIndex(first)
	if !visible || !measured || before.ViewportHeight == 0 {
		t.Fatalf("initial transcript geometry = metrics:%+v first:%d visible:%t measured:%t", before, first, visible, measured)
	}
	inset := before.ScrollOffset - offset

	state.SetState(func() { state.activitySelected = true })
	application.Pump(80, 12)
	hidden := state.scroll.Metrics()
	if hidden.ViewportHeight != before.ViewportHeight || hidden.ViewportWidth != before.ViewportWidth {
		t.Fatalf("offstage transcript viewport = %+v, want retained %+v", hidden, before)
	}
	if application.Contains("transcript line") || !application.Contains("DIFF CONTENT") {
		t.Fatalf("offstage transcript painted or replaced active pane:\n%s", application.Text())
	}
	application.Send(ui.Mouse{Col: 2, Row: 3, Button: ui.MouseWheelDown, EventType: ui.EventPress})
	application.Pump(80, 12)
	if state.activityScrolls != 1 {
		t.Fatalf("active pane mouse scrolls = %d, want 1", state.activityScrolls)
	}

	state.SetState(func() { state.activitySelected = false })
	application.Pump(80, 12)
	after := state.scroll.Metrics()
	restoredOffset, restored := state.list.OffsetForIndex(first)
	if !restored || after.ScrollOffset-restoredOffset != inset {
		t.Fatalf("restored transcript anchor inset = %d metrics:%+v, want %d from %+v", after.ScrollOffset-restoredOffset, after, inset, before)
	}
}
