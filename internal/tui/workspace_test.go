package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
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

type workspaceTranscriptScrollHarness struct {
	State *workspaceTranscriptScrollState
}

func (w workspaceTranscriptScrollHarness) CreateState() ui.State { return w.State }

type workspaceTranscriptScrollState struct {
	ui.StateBase
	activitySelected bool
	app              appState
}

func (s *workspaceTranscriptScrollState) InitState() {
	s.app.messages = make([]transcriptMessage, 48)
	for index := range s.app.messages {
		s.app.messages[index] = transcriptMessage{ID: fmt.Sprintf("assistant_%02d", index), TurnID: "turn", Role: "assistant", Text: strings.Repeat(fmt.Sprintf("transcript line %02d ", index), 1+index%4)}
	}
}

func (s *workspaceTranscriptScrollState) Build(ctx ui.BuildContext) ui.Widget {
	view := shellView{Snapshot: shellSnapshot{Session: protocol.SessionInfo{ID: "session_test"}}}
	transcript := view.transcriptList(ui.MustDepend[ui.Theme](ctx), s.app.mainTranscriptPresentation(), true, "session:test", &s.app.scroll, &s.app.transcriptList, true, nil)
	return conversationWorkspaceHost{
		Open: true, ActivitySelected: s.activitySelected,
		Tabs: ui.Text{Value: "Agent  Diff"}, Transcript: transcript, Activity: ui.Text{Value: "DIFF CONTENT"},
		Pending: ui.SizedBox{}, ComposerSeparator: ui.SizedBox{}, Composer: ui.SizedBox{}, ComposerHeightLimit: 1,
	}
}

func (s *workspaceTranscriptScrollState) TickFrame(now time.Time) bool {
	return s.app.TickFrame(now)
}

func TestWorkspaceHostRetainsTranscriptEndAcrossPaneRoundTrip(t *testing.T) {
	state := &workspaceTranscriptScrollState{app: appState{transcriptVisible: true}}
	application := uitest.New(workspaceTranscriptScrollHarness{State: state})
	application.Pump(80, 12)
	for range 3 {
		state.app.scroll.ScrollToEnd()
		application.Pump(80, 12)
	}
	before := state.app.scroll.Metrics()
	if before.MaxScrollOffset == 0 || before.ScrollOffset != before.MaxScrollOffset {
		t.Fatalf("initial transcript metrics = %+v, want pinned end", before)
	}

	state.SetState(func() {
		state.app.syncTranscriptVisibility(false)
		state.activitySelected = true
	})
	application.Pump(80, 12)
	state.SetState(func() {
		state.app.syncTranscriptVisibility(true)
		state.activitySelected = false
	})
	for range 4 {
		application.Pump(80, 12)
	}

	after := state.app.scroll.Metrics()
	if after.ScrollOffset != after.MaxScrollOffset {
		t.Fatalf("restored transcript metrics = %+v, before %+v; want pinned end", after, before)
	}
}

func TestWorkspaceHostRetainsUnpinnedTranscriptOffsetAcrossPaneRoundTrip(t *testing.T) {
	state := &workspaceTranscriptScrollState{app: appState{transcriptVisible: true}}
	application := uitest.New(workspaceTranscriptScrollHarness{State: state})
	application.Pump(80, 12)
	for range 3 {
		state.app.scroll.ScrollToEnd()
		application.Pump(80, 12)
	}
	state.app.scroll.ScrollByLines(-12)
	application.Pump(80, 12)
	before := state.app.scroll.Metrics()
	beforeFirst, _, beforeVisible := state.app.transcriptList.VisibleRange()
	beforeOffset, beforeMeasured := state.app.transcriptList.OffsetForIndex(beforeFirst)
	if !beforeVisible || !beforeMeasured {
		t.Fatal("initial transcript anchor was not measurable")
	}
	beforeInset := before.ScrollOffset - beforeOffset

	state.SetState(func() {
		state.app.syncTranscriptVisibility(false)
		state.activitySelected = true
	})
	if state.app.transcriptPinnedOnHide || state.app.transcriptPaneAnchorID == "" {
		t.Fatalf("captured transcript anchor = pinned:%t id:%q", state.app.transcriptPinnedOnHide, state.app.transcriptPaneAnchorID)
	}
	application.Pump(80, 12)
	state.SetState(func() {
		state.app.syncTranscriptVisibility(true)
		state.activitySelected = false
	})
	for range 10 {
		application.Pump(80, 12)
		state.TickFrame(time.Now())
	}

	after := state.app.scroll.Metrics()
	targetOffset, targetMeasured := state.app.transcriptList.OffsetForIndex(beforeFirst)
	if !targetMeasured || after.ScrollOffset-targetOffset != beforeInset {
		t.Fatalf("restored transcript anchor inset = %d metrics:%+v, want inset:%d at item:%d metrics:%+v; restore=%d id=%q", after.ScrollOffset-targetOffset, after, beforeInset, beforeFirst, before, state.app.transcriptPaneRestore, state.app.transcriptPaneAnchorID)
	}
}

func TestTranscriptPaneRestoreYieldsToUserScroll(t *testing.T) {
	state := &workspaceTranscriptScrollState{app: appState{transcriptVisible: true}}
	application := uitest.New(workspaceTranscriptScrollHarness{State: state})
	application.Pump(80, 12)
	for range 3 {
		state.app.scroll.ScrollToEnd()
		application.Pump(80, 12)
	}
	state.app.scroll.ScrollByLines(-12)
	application.Pump(80, 12)
	state.SetState(func() {
		state.app.syncTranscriptVisibility(false)
		state.activitySelected = true
	})
	application.Pump(80, 12)
	state.SetState(func() {
		state.app.syncTranscriptVisibility(true)
		state.activitySelected = false
	})
	application.Pump(80, 12)
	state.TickFrame(time.Now())
	if state.app.transcriptPaneRestore != 2 {
		t.Fatalf("restore phase = %d, want 2", state.app.transcriptPaneRestore)
	}
	state.app.scroll.ScrollToStart()
	state.app.transcriptPaneInputGeneration++
	state.TickFrame(time.Now())
	if state.app.transcriptPaneRestore != 0 || state.app.scroll.Metrics().ScrollOffset != 0 {
		t.Fatalf("user scroll was overwritten: restore=%d metrics=%+v", state.app.transcriptPaneRestore, state.app.scroll.Metrics())
	}
}

func TestTranscriptPaneCaptureAdoptsActiveHistoryAnchor(t *testing.T) {
	state := &workspaceTranscriptScrollState{app: appState{transcriptVisible: true}}
	application := uitest.New(workspaceTranscriptScrollHarness{State: state})
	application.Pump(80, 12)
	state.app.scroll.ScrollToEnd()
	state.app.scroll.ScrollByLines(-4)
	state.app.transcriptHistoryRestore = 2
	state.app.transcriptHistoryAnchorID = "assistant-prose:assistant_12"
	state.app.transcriptHistoryAnchorInset = 3

	state.app.syncTranscriptVisibility(false)
	if state.app.transcriptPaneAnchorID != "assistant-prose:assistant_12" || state.app.transcriptPaneAnchorInset != 3 || state.app.transcriptHistoryRestore != 0 || state.app.transcriptHistoryAnchorID != "" {
		t.Fatalf("pane anchor did not adopt history restore: pane=%q/%d history=%q/%d", state.app.transcriptPaneAnchorID, state.app.transcriptPaneAnchorInset, state.app.transcriptHistoryAnchorID, state.app.transcriptHistoryRestore)
	}
}

func TestTranscriptPaneRestoreFallsBackWhenAnchorDisappears(t *testing.T) {
	state := &workspaceTranscriptScrollState{app: appState{transcriptVisible: true}}
	application := uitest.New(workspaceTranscriptScrollHarness{State: state})
	application.Pump(80, 12)
	for range 3 {
		state.app.scroll.ScrollToEnd()
		application.Pump(80, 12)
	}
	state.app.scroll.ScrollByLines(-12)
	application.Pump(80, 12)
	state.SetState(func() {
		state.app.syncTranscriptVisibility(false)
		state.activitySelected = true
	})
	fallback := state.app.transcriptPaneFallbackOffset
	captured := state.app.transcriptPaneAnchorID
	application.Pump(80, 12)
	state.SetState(func() {
		filtered := state.app.messages[:0]
		for _, message := range state.app.messages {
			if !strings.Contains(captured, message.ID) {
				filtered = append(filtered, message)
			}
		}
		state.app.messages = filtered
		state.app.syncTranscriptVisibility(true)
		state.activitySelected = false
	})
	application.Pump(80, 12)
	state.TickFrame(time.Now())
	metrics := state.app.scroll.Metrics()
	if state.app.transcriptPaneRestore != 0 || state.app.transcriptPaneAnchorID != "" || metrics.ScrollOffset != min(fallback, metrics.MaxScrollOffset) {
		t.Fatalf("fallback restore = state:%d id:%q metrics:%+v want offset:%d", state.app.transcriptPaneRestore, state.app.transcriptPaneAnchorID, metrics, fallback)
	}
}
