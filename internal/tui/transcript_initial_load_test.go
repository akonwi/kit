package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type initialTranscriptPager struct {
	fakeSession
	requested chan struct{}
}

func (p *initialTranscriptPager) TranscriptPage(ctx context.Context, _ string) (protocol.TranscriptPage, error) {
	p.requested <- struct{}{}
	<-ctx.Done()
	return protocol.TranscriptPage{}, ctx.Err()
}

type initialTranscriptHarness struct{ state *initialTranscriptState }

func (w initialTranscriptHarness) CreateState() ui.State { return w.state }

type initialTranscriptState struct{ appState }

func (*initialTranscriptState) InitState() {}
func (*initialTranscriptState) Dispose()   {}
func (s *initialTranscriptState) Build(ui.BuildContext) ui.Widget {
	return shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: s.messages, Scroll: &s.scroll, TranscriptList: &s.transcriptList,
		TranscriptInitialLoading:     s.transcriptInitialLoading,
		TranscriptHistoryInitialized: s.transcriptHistoryInitialized,
		TranscriptHistoryHasMore:     s.transcriptHistoryHasMore,
		TranscriptHistoryLoading:     s.transcriptHistoryLoading,
	}, Callbacks: shellCallbacks{TranscriptHistoryScrollUp: s.noteTranscriptHistoryScrollUp}}
}

func TestInitialTranscriptSettlesBeforePagination(t *testing.T) {
	for _, resize := range []bool{false, true} {
		t.Run(fmt.Sprint("resize=", resize), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			pager := &initialTranscriptPager{requested: make(chan struct{}, 1)}
			state := &initialTranscriptState{appState: appState{
				phase: phaseReady, transcriptVisible: true, bound: pager, attachmentCtx: ctx,
			}}
			for i := 0; i < 40; i++ {
				state.messages = append(state.messages, transcriptMessage{ID: fmt.Sprint("message_", i), TurnID: fmt.Sprint("turn_", i), Role: "assistant", Text: strings.Repeat("A variable-height paragraph with enough text to wrap.\n\n", 1+i%7)})
			}
			state.messages[len(state.messages)-1].Text += "LATEST MESSAGE END"
			state.resetTranscriptHistoryFromSnapshot(protocol.SessionSnapshot{PreviousMessageCursor: "10", HasMoreMessages: true})
			app := uitest.New(initialTranscriptHarness{state})
			width, height := 80, 24
			app.Pump(width, height)
			if !app.Contains("Loading conversation…") {
				t.Fatalf("missing initial loading cover:\n%s", app.Text())
			}
			for frame := 0; frame < 100 && state.transcriptInitialLoading; frame++ {
				if resize && frame == 1 {
					width, height = 60, 16
				}
				state.TickFrame(time.Now())
				if state.transcriptHistoryLoading {
					t.Fatal("pagination started during initial positioning")
				}
				app.Pump(width, height)
			}
			if state.transcriptInitialLoading {
				t.Fatal("initial layout did not settle")
			}
			metrics := state.scroll.Metrics()
			if metrics.ScrollOffset != metrics.MaxScrollOffset || !app.Contains("LATEST MESSAGE END") {
				t.Fatalf("initial tail not visible at bottom: %+v\n%s", metrics, app.Text())
			}
			// Extra frames and layout corrections alone must not request older history.
			for range 5 {
				state.TickFrame(time.Now())
				app.Pump(width, height)
			}
			select {
			case <-pager.requested:
				t.Fatal("unsolicited initial history request")
			default:
			}
			// A hidden transcript cannot load older history, even with upward intent.
			state.transcriptVisible = false
			state.transcriptHistoryUserScroll = true
			state.scroll.ScrollToStart()
			for range 6 {
				app.Pump(width, height)
				state.TickFrame(time.Now())
			}
			if state.transcriptHistoryLoading {
				t.Fatal("hidden transcript paginated")
			}
			state.transcriptVisible = true
			state.transcriptHistoryUserScroll = false
			state.transcriptHistoryScrollInput = false
			state.transcriptHistoryLastOffset = state.scroll.Metrics().ScrollOffset
			state.TickFrame(time.Now())
			if state.transcriptHistoryLoading {
				t.Fatal("programmatic positioning paginated")
			}
			// User upward scrolling now arms a single request.
			state.scroll.ScrollToEnd()
			for range 6 {
				app.Pump(width, height)
			}
			state.transcriptHistoryLastOffset = state.scroll.Metrics().ScrollOffset
			state.transcriptHistoryScrollInput = true
			state.scroll.ScrollToStart()
			for range 6 {
				app.Pump(width, height)
			}
			state.TickFrame(time.Now())
			if !state.transcriptHistoryLoading {
				t.Fatal("upward user scroll did not request history")
			}
			select {
			case <-pager.requested:
			case <-time.After(time.Second):
				t.Fatal("history request did not start")
			}
			state.TickFrame(time.Now())
			select {
			case <-pager.requested:
				t.Fatal("duplicate history request")
			default:
			}
		})
	}
}

func TestShortInitialTranscriptWaitsForUpwardScroll(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pager := &initialTranscriptPager{requested: make(chan struct{}, 1)}
	state := &initialTranscriptState{appState: appState{
		phase: phaseReady, transcriptVisible: true, bound: pager, attachmentCtx: ctx,
		messages: []transcriptMessage{{ID: "last", TurnID: "turn", Role: "user", Text: "Latest short message"}},
	}}
	state.resetTranscriptHistoryFromSnapshot(protocol.SessionSnapshot{PreviousMessageCursor: "10", HasMoreMessages: true})
	app := uitest.New(initialTranscriptHarness{state})
	for range 12 {
		app.Pump(80, 24)
		state.TickFrame(time.Now())
	}
	app.Pump(80, 24)
	if state.transcriptInitialLoading || !app.Contains("Latest short message") {
		t.Fatal("short transcript did not settle")
	}
	if state.transcriptHistoryLoading {
		t.Fatal("short transcript automatically paginated")
	}
	rows := paintedRows(app, 80, 24)
	messageRow := findPaintedRow(rows, "Latest short message")
	composerRow := findPaintedRow(rows, "Ask kit to do something")
	if messageRow != composerRow-5 {
		t.Fatalf("short transcript not bottom anchored: message row %d, composer row %d\n%s", messageRow, composerRow, strings.Join(rows, "\n"))
	}
	app.Send(vaxis.Key{Keycode: vaxis.KeyHome})
	app.Pump(80, 24)
	state.TickFrame(time.Now())
	app.Send(vaxis.Mouse{Col: 10, Row: findPaintedRow(paintedRows(app, 80, 24), "Ask kit to do something"), Button: vaxis.MouseWheelUp, EventType: vaxis.EventPress})
	app.Pump(80, 24)
	state.TickFrame(time.Now())
	if state.transcriptHistoryLoading {
		t.Fatal("composer input triggered history pagination")
	}
	// A first upward wheel must both cancel a deferred pane-restoration follow
	// and retain its request for earlier history at offset zero.
	state.requestTranscriptScroll()
	app.Send(vaxis.Mouse{Col: 10, Row: findPaintedRow(paintedRows(app, 80, 24), "Latest short message"), Button: vaxis.MouseWheelUp, EventType: vaxis.EventPress})
	state.TickFrame(time.Now())
	if !state.transcriptHistoryLoading {
		t.Fatal("upward scroll at offset zero did not paginate")
	}
	select {
	case <-pager.requested:
	case <-time.After(time.Second):
		t.Fatal("history request did not start")
	}
}

func TestShortTranscriptGrowsUpwardAndReanchorsOnResize(t *testing.T) {
	state := &initialTranscriptState{appState: appState{
		phase: phaseReady, transcriptVisible: true,
		messages: []transcriptMessage{{ID: "first", TurnID: "turn_1", Role: "user", Text: "First message"}},
	}}
	state.resetTranscriptHistoryFromSnapshot(protocol.SessionSnapshot{})
	app := uitest.New(initialTranscriptHarness{state})
	pumpInitialTranscript(t, app, state, 80, 24)
	assertTranscriptMessageAtBottom(t, app, 80, 24, "First message", 5)

	state.SetState(func() {
		state.messages = append(state.messages, transcriptMessage{ID: "second", TurnID: "turn_2", Role: "user", Text: "Second message"})
		state.requestTranscriptScroll()
	})
	for range 6 {
		app.Pump(80, 24)
		state.TickFrame(time.Now())
	}
	app.Pump(80, 24)
	assertTranscriptMessageAtBottom(t, app, 80, 24, "Second message", 5)
	rows := paintedRows(app, 80, 24)
	if first, second := findPaintedRow(rows, "First message"), findPaintedRow(rows, "Second message"); first < 0 || first >= second {
		t.Fatalf("messages did not grow upward: first row %d, second row %d\n%s", first, second, strings.Join(rows, "\n"))
	}

	state.SetState(func() {
		for i := range 12 {
			state.messages = append(state.messages, transcriptMessage{ID: fmt.Sprint("overflow_", i), TurnID: fmt.Sprint("overflow_turn_", i), Role: "user", Text: fmt.Sprintf("Overflow message %02d", i)})
		}
		state.requestTranscriptScroll()
	})
	for range 12 {
		app.Pump(80, 24)
		state.TickFrame(time.Now())
	}
	app.Pump(80, 24)
	if metrics := state.scroll.Metrics(); metrics.MaxScrollOffset <= 0 || metrics.ScrollOffset != metrics.MaxScrollOffset {
		t.Fatalf("growth past viewport did not remain pinned: %+v", metrics)
	}
	assertTranscriptMessageAtBottom(t, app, 80, 24, "Overflow message 11", 5)

	for range 4 {
		app.Pump(60, 16)
		state.TickFrame(time.Now())
	}
	app.Pump(60, 16)
	assertTranscriptMessageAtBottom(t, app, 60, 16, "Overflow message 11", 5)
}

func TestInitialTranscriptReleasesScrollingAfterPositioning(t *testing.T) {
	state := &initialTranscriptState{appState: appState{phase: phaseReady, transcriptVisible: true}}
	for i := range 20 {
		state.messages = append(state.messages, transcriptMessage{ID: fmt.Sprint("message_", i), TurnID: fmt.Sprint("turn_", i), Role: "user", Text: fmt.Sprintf("Message %02d", i)})
	}
	state.resetTranscriptHistoryFromSnapshot(protocol.SessionSnapshot{})
	app := uitest.New(initialTranscriptHarness{state})
	pumpInitialTranscript(t, app, state, 60, 16)
	if !state.scroll.ScrollToStart() {
		t.Fatal("long transcript did not have a scrollable initial offset")
	}
	for range 4 {
		app.Pump(60, 16)
		state.TickFrame(time.Now())
	}
	if offset := state.scroll.Metrics().ScrollOffset; offset != 0 {
		t.Fatalf("user scroll was overwritten after initialization: offset %d", offset)
	}
	rows := strings.Join(paintedRows(app, 60, 16), "\n")
	if !strings.Contains(rows, "Message 00") {
		t.Fatalf("scrolled transcript did not remain at the beginning:\n%s", rows)
	}
}

func pumpInitialTranscript(t *testing.T, app *uitest.App, state *initialTranscriptState, width, height int) {
	t.Helper()
	for frame := 0; frame < 100 && state.transcriptInitialLoading; frame++ {
		app.Pump(width, height)
		state.TickFrame(time.Now())
	}
	for range 3 {
		app.Pump(width, height)
		state.TickFrame(time.Now())
	}
	if state.transcriptInitialLoading {
		t.Fatal("initial transcript did not settle")
	}
}

func assertTranscriptMessageAtBottom(t *testing.T, app *uitest.App, width, height int, message string, rowsAboveComposer int) {
	t.Helper()
	rows := paintedRows(app, width, height)
	messageRow := findPaintedRow(rows, message)
	composerRow := findPaintedRow(rows, "Ask kit to do something")
	if messageRow != composerRow-rowsAboveComposer {
		t.Fatalf("%q not bottom anchored: message row %d, composer row %d\n%s", message, messageRow, composerRow, strings.Join(rows, "\n"))
	}
}

type zeroHeightTranscriptHarness struct{ state *zeroHeightTranscriptState }

func (w zeroHeightTranscriptHarness) CreateState() ui.State { return w.state }

type zeroHeightTranscriptState struct {
	initialTranscriptState
	height int
}

func (s *zeroHeightTranscriptState) Build(ui.BuildContext) ui.Widget {
	count := 0
	if s.height > 0 {
		count = 1
	}
	return ui.Align{Child: ui.ConstrainedBox{Constraints: ui.Constraints{MinHeight: s.height, MaxHeight: s.height, MaxWidth: 80}, Child: ui.CustomScrollView{Controller: &s.scroll, Slivers: []ui.Widget{
		ui.SliverListBuilder{Controller: &s.transcriptList, Count: count, ItemExtent: 1, Builder: func(ui.BuildContext, int) ui.Widget { return ui.Text{Value: "Latest message"} }},
	}}}}
}
func TestInitialTranscriptSuspendsAtZeroHeight(t *testing.T) {
	state := &zeroHeightTranscriptState{initialTranscriptState: initialTranscriptState{appState: appState{phase: phaseReady, transcriptVisible: true,
		messages: []transcriptMessage{{ID: "one", TurnID: "turn", Role: "user", Text: "Latest message"}},
	}}}
	state.resetTranscriptHistoryFromSnapshot(protocol.SessionSnapshot{})
	state.requestTranscriptScroll()
	app := uitest.New(zeroHeightTranscriptHarness{state})
	app.Pump(80, 24)
	if state.TickFrame(time.Now()) {
		t.Fatalf("unmeasurable viewport kept ticking: %+v", state.scroll.Metrics())
	}
	if !state.transcriptInitialLoading {
		t.Fatal("unmeasurable viewport prematurely settled")
	}
	state.SetState(func() { state.height = 10 })
	for range 12 {
		app.Pump(80, 24)
		state.TickFrame(time.Now())
	}
	if state.transcriptInitialLoading {
		t.Fatal("resize did not resume settlement")
	}
}
