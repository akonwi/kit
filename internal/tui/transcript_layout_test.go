package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestReadableColumnWidth(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ width, inner int }{
		{width: 180, inner: 176},
		{width: 80, inner: 76},
		{width: 4, inner: 4},
	} {
		if inner := readableColumnWidth(test.width, transcriptMinMargin); inner != test.inner {
			t.Errorf("width %d column = %d, want %d", test.width, inner, test.inner)
		}
	}
}

func layoutTranscriptMessages() []transcriptMessage {
	return []transcriptMessage{
		{ID: "user_1", TurnID: "turn_1", Role: "user", Text: "Inspect the file"},
		{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", ToolCalls: []transcriptToolCall{
			{ID: "call_1", Name: "bash", Arguments: json.RawMessage(`{"command":"go test ./internal/tui"}`)},
		}},
		{ID: "tool_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "bash", Text: "ok"},
		{ID: "assistant_2", TurnID: "turn_1", Role: "assistant", Text: "All tests pass."},
	}
}

func TestTranscriptSpansAvailableWidth(t *testing.T) {
	t.Parallel()

	const width, height = 180, 20
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: layoutTranscriptMessages(), Scroll: &ui.ScrollController{},
		InlineActivityOpen: map[string]bool{"turn-work:turn_1:assistant_1": true},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	fill := strings.Repeat("▄", 176)
	edge := strings.Repeat("▀", 176)
	userRow := findPaintedRow(rows, "Inspect the file")
	if userRow < 1 {
		t.Fatalf("user message missing:\n%s", strings.Join(rows, "\n"))
	}
	want := []string{
		"  " + fill + "  ",
		"    Inspect the file" + strings.Repeat(" ", 160),
		"  " + edge + "  ",
		strings.Repeat(" ", width),
		"  ▾ 1 tool call" + strings.Repeat(" ", 165),
		"    Run command        " + " go test ./internal/tui " + strings.Repeat(" ", 133),
		strings.Repeat(" ", width),
		"  All tests pass." + strings.Repeat(" ", 163),
	}
	for offset, expected := range want {
		if got := rows[userRow-1+offset]; got != expected {
			t.Errorf("row %d =\n%q\nwant\n%q", userRow-1+offset, got, expected)
		}
	}
}

func TestTranscriptKeepsMinimumMarginOnNarrowViewports(t *testing.T) {
	t.Parallel()

	const width, height = 60, 16
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: layoutTranscriptMessages(), Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	userRow := findPaintedRow(rows, "Inspect the file")
	if userRow < 1 {
		t.Fatalf("user message missing:\n%s", strings.Join(rows, "\n"))
	}
	want := []string{
		"  " + strings.Repeat("▄", 56) + "  ",
		"    Inspect the file" + strings.Repeat(" ", 40),
		"  " + strings.Repeat("▀", 56) + "  ",
		strings.Repeat(" ", width),
		"  ▸ 1 tool call" + strings.Repeat(" ", 45),
		strings.Repeat(" ", width),
		"  All tests pass." + strings.Repeat(" ", 43),
	}
	for offset, expected := range want {
		if got := rows[userRow-1+offset]; got != expected {
			t.Errorf("row %d =\n%q\nwant\n%q", userRow-1+offset, got, expected)
		}
	}
}

func TestTranscriptUserEntryHalfBlockEdgesBlendIntoWash(t *testing.T) {
	t.Parallel()

	theme := ui.DefaultTheme()
	fill := userMessageBackground(theme)
	app := uitest.New(transcriptUserEntry(theme, protocol.TranscriptMessage{
		ID:      "user_1",
		Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "Inspect the file"}},
	}, nil, false, nil))
	app.Pump(24, 3)
	rows := paintedRows(app, 24, 3)
	want := []string{
		strings.Repeat("▄", 24),
		"  Inspect the file      ",
		strings.Repeat("▀", 24),
	}
	for index, expected := range want {
		if rows[index] != expected {
			t.Errorf("row %d = %q, want %q", index, rows[index], expected)
		}
	}
	for _, row := range []int{0, 2} {
		cell := app.Cell(0, row)
		if cell.Foreground != fill || cell.Background != theme.Background {
			t.Errorf("edge row %d style = fg %#v bg %#v, want fg %#v bg %#v", row, cell.Foreground, cell.Background, fill, theme.Background)
		}
	}
	if got := app.Cell(0, 1).Background; got != fill {
		t.Errorf("text row padding background = %#v, want wash %#v", got, fill)
	}
}
