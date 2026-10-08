package tui

import (
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func pluginMessageForTest() protocol.TranscriptMessage {
	return protocol.TranscriptMessage{
		ID: "pluginmsg_1", TurnID: "turn_1", Role: "plugin",
		BoundaryKind: protocol.PluginMessageBoundaryKind, BoundarySource: "autoresearch",
		Content: []protocol.TranscriptContent{protocol.TextBlock("Continue the experiment loop.\n\nLog each result before the next run.")},
	}
}

func TestTranscriptPluginMessageEntryCollapsesToOneRow(t *testing.T) {
	t.Parallel()
	theme := ui.DefaultTheme()
	toggles := 0
	app := uitest.New(transcriptPluginMessageEntry(theme, pluginMessageForTest(), false, func(ui.EventContext) { toggles++ }))
	app.Pump(60, 1)
	if got, want := strings.TrimRight(blankCells(app, 60, 0), " "), "◆ autoresearch · Continue the experiment loop. ▸"; got != want {
		t.Fatalf("collapsed row = %q, want %q", got, want)
	}
	for column, want := range map[int]ui.Color{0: theme.AccentText, 2: theme.AccentText, 15: theme.MutedForeground, 17: theme.MutedForeground, 47: theme.MutedForeground} {
		if got := app.Cell(column, 0).Foreground; got != want {
			t.Errorf("cell %d %q foreground = %#v, want %#v", column, app.Cell(column, 0).Grapheme, got, want)
		}
	}
	app.Click(47, 0)
	if toggles != 1 {
		t.Fatalf("toggles = %d, want 1", toggles)
	}
}

func TestTranscriptPluginMessageEntryTruncatesItsSummaryToTheRow(t *testing.T) {
	t.Parallel()
	app := uitest.New(transcriptPluginMessageEntry(ui.DefaultTheme(), pluginMessageForTest(), false, nil))
	app.Pump(32, 1)
	if got, want := blankCells(app, 32, 0), "◆ autoresearch · Continue the… ▸"; got != want {
		t.Fatalf("narrow collapsed row = %q, want %q", got, want)
	}
}

func TestTranscriptPluginMessageEntryExpandsBeneathALeftRule(t *testing.T) {
	t.Parallel()
	theme := ui.DefaultTheme()
	app := uitest.New(transcriptPluginMessageEntry(theme, pluginMessageForTest(), true, nil))
	app.Pump(48, 4)
	want := []string{
		"◆ autoresearch · Continue the experiment loop. ▾",
		"  │ Continue the experiment loop.",
		"  │",
		"  │ Log each result before the next run.",
	}
	for index := range want {
		if got := strings.TrimRight(blankCells(app, 48, index), " "); got != want[index] {
			t.Errorf("expanded row %d = %q, want %q", index, got, want[index])
		}
	}
	if got := app.Cell(2, 1).Foreground; got != theme.Border {
		t.Errorf("rule foreground = %#v, want border %#v", got, theme.Border)
	}
	if got := app.Cell(4, 1).Foreground; got != theme.MutedForeground {
		t.Errorf("expanded text foreground = %#v, want muted %#v", got, theme.MutedForeground)
	}
}

// blankCells renders unpainted cells in row as spaces so leading offsets are visible.
func blankCells(app *uitest.App, width, row int) string {
	var line strings.Builder
	for column := 0; column < width; column++ {
		grapheme := app.Cell(column, row).Grapheme
		if grapheme == "" {
			grapheme = " "
		}
		line.WriteString(grapheme)
	}
	return line.String()
}

func TestPluginMessageTranscriptRowFromLiveEvent(t *testing.T) {
	t.Parallel()
	state := &appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyTurnEvents([]protocol.SessionEvent{
		{Sequence: 1, TurnID: "turn_1", Payload: protocol.TurnStartedEvent{Status: protocol.TurnStatusRunning}},
		{Sequence: 2, TurnID: "turn_1", Payload: protocol.PluginMessageAddedEvent{PluginID: "autoresearch", Text: "Continue the experiment loop."}},
	})
	want := []transcriptMessage{{ID: "live-plugin:turn_1", TurnID: "turn_1", Role: "plugin", PluginID: "autoresearch", Text: "Continue the experiment loop."}}
	if len(state.liveMessages) != 1 || state.liveMessages[0].ID != want[0].ID || state.liveMessages[0].Role != want[0].Role ||
		state.liveMessages[0].PluginID != want[0].PluginID || state.liveMessages[0].Text != want[0].Text || state.liveMessages[0].TurnID != want[0].TurnID {
		t.Fatalf("live messages = %+v, want %+v", state.liveMessages, want)
	}
	if state.turnActivity != "Working…" {
		t.Fatalf("turn activity = %q, want Working…", state.turnActivity)
	}
	assertPluginMessageShellRow(t, state.liveMessages)
}

func TestPluginMessageTranscriptRowFromPersistedContext(t *testing.T) {
	t.Parallel()
	messages := projectTranscript([]protocol.TranscriptMessage{
		{
			ID: "pluginmsg_1", TurnID: "turn_1", Sequence: 1, Role: "context",
			BoundaryID: "pluginmsg_1", BoundaryKind: protocol.PluginMessageBoundaryKind, BoundarySource: "autoresearch",
			Details: []byte(`{"version":1,"pluginId":"autoresearch"}`),
			Content: []protocol.TranscriptContent{protocol.TextBlock("Continue the experiment loop.")},
		},
		{ID: "assistant_1", TurnID: "turn_1", Sequence: 2, Role: "assistant", Content: []protocol.TranscriptContent{protocol.TextBlock("Running the next experiment.")}},
	})
	presentation := presentTranscript(messages)
	if len(presentation.Items) != 2 || presentation.Items[0].Kind != transcriptDisplaySingle || presentation.Items[0].Item.Kind != transcriptItemPlugin ||
		presentation.Items[1].Kind != transcriptDisplayAssistantProse {
		t.Fatalf("presentation items = %+v, want plugin row then assistant prose", presentation.Items)
	}
	assertPluginMessageShellRow(t, messages)
}

// assertPluginMessageShellRow renders messages in the transcript and checks
// that the plugin row toggles by turn ID, which survives the live-to-persisted
// transition.
func assertPluginMessageShellRow(t *testing.T, messages []transcriptMessage) {
	t.Helper()
	var toggled []string
	app := uitest.New(shellView{
		Snapshot: shellSnapshot{Phase: phaseReady, Messages: messages, Scroll: &ui.ScrollController{}},
		Callbacks: shellCallbacks{TogglePluginMessage: func(_ ui.EventContext, turnID string) {
			toggled = append(toggled, turnID)
		}},
	})
	app.Pump(80, 16)
	column, row := findTextCell(t, paintedRows(app, 80, 16), glyphDiamond)
	if got, want := strings.TrimSpace(blankCells(app, 80, row)), "◆ autoresearch · Continue the experiment loop. ▸"; got != want {
		t.Fatalf("transcript plugin row = %q, want %q", got, want)
	}
	app.Click(column, row)
	if len(toggled) != 1 || toggled[0] != "turn_1" {
		t.Fatalf("toggled = %v, want [turn_1]", toggled)
	}
}
