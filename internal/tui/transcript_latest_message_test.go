package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestTranscriptLatestButtonWaitsUntilLatestMessageLeavesView(t *testing.T) {
	// The final message is taller than the viewport, so leaving the bottom of
	// the content must not surface the shortcut while any of it remains visible.
	lines := make([]string, 12)
	for index := range lines {
		lines[index] = "line " + strings.Repeat("x", 20)
	}
	earlier := make([]transcriptMessage, 12)
	for index := range earlier {
		earlier[index] = transcriptMessage{
			ID: fmt.Sprintf("user_%d", index), TurnID: fmt.Sprintf("turn_%d", index),
			Role: "user", Text: fmt.Sprintf("Earlier question %02d", index),
		}
	}
	state := &latestFollowState{appState: appState{
		phase: phaseReady, transcriptVisible: true,
		messages: append(earlier, transcriptMessage{
			ID: "assistant_1", TurnID: "turn_final", Role: "assistant", Text: strings.Join(lines, "\n"),
		}),
	}}
	state.resetTranscriptHistoryFromSnapshot(protocol.SessionSnapshot{})
	app := uitest.New(latestFollowHarness{state})
	const width, height = 80, 24
	pump := func() {
		for range 6 {
			app.Pump(width, height)
			state.TickFrame(time.Now())
		}
		app.Pump(width, height)
	}
	pump()

	state.scroll.ScrollToOffset(40)
	pump()
	if app.Contains("↓ Latest") {
		t.Fatalf("shortcut appeared while the tall latest message was still visible:\n%s", app.Text())
	}

	state.scroll.ScrollToStart()
	pump()
	if !app.Contains("↓ Latest") {
		t.Fatalf("shortcut missing after scrolling past the latest message:\n%s", app.Text())
	}
	if app.Contains("line ") {
		t.Fatal("latest message still visible after scrolling to the start")
	}
}
