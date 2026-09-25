package tui

import (
	"fmt"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestTranscriptLatestMessageOutOfView(t *testing.T) {
	t.Parallel()

	// Content is 60 rows, the viewport shows 20, and the latest message is 25
	// rows tall, so it occupies content rows 35 through 59.
	metrics := func(offset int) ui.ScrollMetrics {
		return ui.ScrollMetrics{ScrollOffset: offset, ViewportHeight: 20, ContentHeight: 60}
	}
	for _, test := range []struct {
		name      string
		offset    int
		itemStart int
		count     int
		measured  bool
		outOfView bool
	}{
		{name: "latest message fills the viewport", offset: 40, itemStart: 35, count: 2, measured: true},
		{name: "latest message starts above the viewport", offset: 45, itemStart: 35, count: 2, measured: true},
		{name: "viewport shows the end of the latest message", offset: 20, itemStart: 35, count: 2, measured: true},
		{name: "viewport scrolled past the latest message", offset: 0, itemStart: 35, count: 2, measured: true, outOfView: true},
		{name: "latest message starts below the viewport", offset: 0, itemStart: 35, count: 2, measured: true, outOfView: true},
		{name: "unmeasured item", offset: 0, itemStart: 35, count: 2},
		{name: "empty transcript", offset: 0, count: 0, measured: true},
	} {
		if test.name == "latest message starts below the viewport" {
			test.offset = 0
			test.itemStart = 25
		}
		got := transcriptLatestMessageOutOfView(metrics(test.offset), test.itemStart, test.count, test.measured)
		if got != test.outOfView {
			t.Errorf("%s: out of view = %t, want %t", test.name, got, test.outOfView)
		}
	}
	// A single item is the whole content, so it is out of view only once the
	// viewport moves entirely past it.
	if transcriptLatestMessageOutOfView(metrics(0), 0, 1, true) {
		t.Error("single item at the top reported out of view")
	}
	if !transcriptLatestMessageOutOfView(metrics(60), 0, 1, true) {
		t.Error("single item scrolled entirely past reported in view")
	}
}

func longTranscriptMessages() []transcriptMessage {
	messages := make([]transcriptMessage, 30)
	for index := range messages {
		messages[index] = transcriptMessage{
			ID: fmt.Sprintf("user_%d", index), TurnID: fmt.Sprintf("turn_%d", index),
			Role: "user", Text: fmt.Sprintf("Message %02d", index),
		}
	}
	return messages
}

func TestTranscriptLatestButtonAppearsWhenScrolledAway(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: longTranscriptMessages(), Scroll: &ui.ScrollController{},
		TranscriptLatestOutOfView: true,
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	composerRow := findPaintedRow(rows, "Ask kit to do something")
	buttonRow := findPaintedRow(rows, "↓ Latest")
	if buttonRow != composerRow-4 {
		t.Fatalf("latest button row %d, want row %d above the composer separator\n%s", buttonRow, composerRow-3, strings.Join(rows, "\n"))
	}
	column, row := findPaintedCellSequence(t, app, width, height, "↓ Latest")
	// The scrollbar owns the final column, so the button ends one cell in.
	if column != width-10 {
		t.Errorf("latest button column = %d, want %d at the trailing edge", column, width-11)
	}
	theme := ui.DefaultTheme()
	if got := app.Cell(column, row).Background; got != theme.Surface {
		t.Errorf("latest button background = %#v, want surface %#v", got, theme.Surface)
	}
	app.Send(vaxis.Mouse{Col: column, Row: row, EventType: vaxis.EventMotion})
	app.Pump(width, height)
	if got := app.Cell(column, row).Background; got != theme.SurfaceHovered {
		t.Errorf("hovered latest button background = %#v, want %#v", got, theme.SurfaceHovered)
	}
}

func TestTranscriptLatestButtonHiddenAtBottom(t *testing.T) {
	t.Parallel()

	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: longTranscriptMessages(), Scroll: &ui.ScrollController{},
	}})
	app.Pump(80, 24)
	if app.Contains("↓ Latest") {
		t.Fatalf("latest button visible while following the bottom:\n%s", app.Text())
	}
}
