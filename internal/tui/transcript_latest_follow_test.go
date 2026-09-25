package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

// latestFollowHarness mounts the real app state so the resume shortcut is
// driven by observed scroll metrics rather than a snapshot flag.
type latestFollowHarness struct{ state *latestFollowState }

func (w latestFollowHarness) CreateState() ui.State { return w.state }

type latestFollowState struct{ appState }

func (*latestFollowState) InitState() {}

func TestTranscriptLatestButtonResumesFollow(t *testing.T) {
	messages := make([]transcriptMessage, 40)
	for index := range messages {
		messages[index] = transcriptMessage{
			ID: fmt.Sprintf("user_%d", index), TurnID: fmt.Sprintf("turn_%d", index),
			Role: "user", Text: fmt.Sprintf("Message %02d", index),
		}
	}
	state := &latestFollowState{appState: appState{phase: phaseReady, transcriptVisible: true, messages: messages}}
	state.resetTranscriptHistoryFromSnapshot(protocol.SessionSnapshot{})
	app := uitest.New(latestFollowHarness{state})
	const width, height = 80, 24
	for range 8 {
		app.Pump(width, height)
		state.TickFrame(time.Now())
	}
	app.Pump(width, height)
	if app.Contains("↓ Latest") {
		t.Fatal("latest button visible while pinned to the bottom")
	}

	state.scroll.ScrollToStart()
	for range 4 {
		app.Pump(width, height)
		state.TickFrame(time.Now())
	}
	app.Pump(width, height)
	if !app.Contains("↓ Latest") {
		t.Fatalf("latest button missing after scrolling away:\n%s", app.Text())
	}
	if !state.transcriptLatestOutOfView {
		t.Fatal("scroll observation did not record the latest content as out of view")
	}

	column, row := findPaintedCellSequence(t, app, width, height, "↓ Latest")
	app.Send(vaxis.Mouse{Col: column, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	for range 4 {
		app.Pump(width, height)
		state.TickFrame(time.Now())
	}
	app.Pump(width, height)
	metrics := state.scroll.Metrics()
	if metrics.ScrollOffset != metrics.MaxScrollOffset {
		t.Fatalf("resume click scroll = %+v, want pinned to the end", metrics)
	}
	if app.Contains("↓ Latest") {
		t.Fatalf("latest button stayed visible after resuming follow:\n%s", strings.Join(paintedRows(app, width, height), "\n"))
	}
	rows := paintedRows(app, width, height)
	if findPaintedRow(rows, "Message 39") < 0 {
		t.Fatal("latest message not visible after resuming follow")
	}
}
