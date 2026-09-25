package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestTranscriptReadingSections(t *testing.T) {
	t.Parallel()

	document := parseMarkdownDocument(markdownView{Source: strings.Join([]string{
		"Leading context.",
		"",
		"## Findings",
		"",
		"Details.",
		"",
		"## Next steps",
	}, "\n")})
	sections := transcriptReadingSections(document)
	if len(sections) != 3 {
		t.Fatalf("sections = %+v, want 3", sections)
	}
	for index, want := range []string{"Overview", "Findings", "Next steps"} {
		if sections[index].Title != want {
			t.Errorf("section %d = %q, want %q", index, sections[index].Title, want)
		}
	}
	if transcriptReadingSections(parseMarkdownDocument(markdownView{Source: "Just prose."})) != nil {
		t.Fatal("a message without headings produced sections")
	}
}

func TestTranscriptReadingSelection(t *testing.T) {
	t.Parallel()

	offsets := []int{5, 20, 48}
	for _, test := range []struct{ viewport, want int }{
		{viewport: 0, want: 0},
		{viewport: 5, want: 0},
		{viewport: 19, want: 0},
		{viewport: 20, want: 1},
		{viewport: 60, want: 2},
	} {
		if got := transcriptReadingSelection(offsets, test.viewport); got != test.want {
			t.Errorf("viewport %d selects section %d, want %d", test.viewport, got, test.want)
		}
	}
}

func tallSectionedResponse() string {
	body := make([]string, 0, 40)
	body = append(body, "Opening context for the change.")
	for _, heading := range []string{"Findings", "Approach", "Verification"} {
		body = append(body, "", "## "+heading)
		for line := 0; line < 8; line++ {
			body = append(body, heading+" detail line")
		}
	}
	return strings.Join(body, "\n")
}

func TestTranscriptReadingStripAppearsForTallMessage(t *testing.T) {
	state := &latestFollowState{appState: appState{
		phase: phaseReady, transcriptVisible: true,
		messages: []transcriptMessage{
			{ID: "user_1", TurnID: "turn_1", Role: "user", Text: "Review the layout"},
			{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Text: tallSectionedResponse()},
		},
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
	if app.Contains("1 / 4") {
		t.Fatal("reading strip visible while following the bottom")
	}

	state.scroll.ScrollToStart()
	pump()
	rows := paintedRows(app, width, height)
	stripRow := findPaintedRow(rows, "Overview")
	if stripRow < 0 || !strings.Contains(rows[stripRow], "1 / 4") {
		t.Fatalf("reading strip missing at the top of a tall message:\n%s", strings.Join(rows, "\n"))
	}
	if strings.TrimSpace(rows[0]) != "Unnamed session" || stripRow > 3 {
		t.Errorf("reading strip row %d, want it pinned at the top of the transcript", stripRow)
	}

	column, row := findPaintedCellSequence(t, app, width, height, "↓")
	app.Send(vaxis.Mouse{Col: column, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
	pump()
	rows = paintedRows(app, width, height)
	stripRow = findPaintedRow(rows, "2 / 4")
	if stripRow < 0 || !strings.Contains(rows[stripRow], "Findings") {
		t.Fatalf("next section did not advance the strip:\n%s", strings.Join(rows, "\n"))
	}
	if findPaintedRow(rows, "Findings detail line") < 0 {
		t.Fatal("jumping to Findings did not reveal its content")
	}
	if state.needsScroll {
		t.Fatal("section jump left follow pinned")
	}
}

func TestTranscriptReadingPickerJumpsAndDismisses(t *testing.T) {
	state := &latestFollowState{appState: appState{
		phase: phaseReady, transcriptVisible: true,
		messages: []transcriptMessage{
			{ID: "user_1", TurnID: "turn_1", Role: "user", Text: "Review the layout"},
			{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Text: tallSectionedResponse()},
		},
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
	state.scroll.ScrollToStart()
	pump()

	openPicker := func() {
		t.Helper()
		column, row := findPaintedCellSequence(t, app, width, height, "Overview")
		app.Send(vaxis.Mouse{Col: column, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
		app.Pump(width, height)
	}
	openPicker()
	if !state.transcriptReadingPickerOpen || state.inputOwner() != inputReading {
		t.Fatalf("picker open %t owner %v", state.transcriptReadingPickerOpen, state.inputOwner())
	}
	if !app.Contains("2  Findings") {
		t.Fatalf("section picker did not paint:\n%s", strings.Join(paintedRows(app, width, height), "\n"))
	}
	cornerColumn, cornerRow := findPaintedCellSequence(t, app, width, height, "┌")
	pickerColumn, pickerRow := findPaintedCellSequence(t, app, width, height, "1  Overview")
	if cornerRow > 3 || cornerColumn != transcriptMinMargin || pickerRow != cornerRow+1 || pickerColumn > transcriptMinMargin+2 {
		t.Fatalf("picker corner %d,%d label %d,%d, want it over the section title", cornerColumn, cornerRow, pickerColumn, pickerRow)
	}

	app.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	app.Pump(width, height)
	if state.transcriptReadingPickerSelected != 1 {
		t.Fatalf("picker selection = %d, want 1", state.transcriptReadingPickerSelected)
	}
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	pump()
	if state.transcriptReadingPickerOpen {
		t.Fatal("picker stayed open after choosing a section")
	}
	rows := paintedRows(app, width, height)
	if findPaintedRow(rows, "2 / 4") < 0 || findPaintedRow(rows, "Findings detail line") < 0 {
		t.Fatalf("enter did not jump to Findings:\n%s", strings.Join(rows, "\n"))
	}
	if state.needsScroll {
		t.Fatal("picker jump left follow pinned")
	}

	state.scroll.ScrollToStart()
	pump()
	openPicker()
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(width, height)
	if state.transcriptReadingPickerOpen || app.Contains("2  Findings") {
		t.Fatalf("escape left the picker open:\n%s", strings.Join(paintedRows(app, width, height), "\n"))
	}
	if state.transcriptReading.Selected != 0 {
		t.Fatalf("escape moved the section to %d", state.transcriptReading.Selected)
	}
}

func TestTranscriptReadingStripRendersPosition(t *testing.T) {
	t.Parallel()

	theme := ui.DefaultTheme()
	app := uitest.New(transcriptReadingStrip{Reading: transcriptReadingSnapshot{
		Visible: true, Title: "Implementation plan", Selected: 2, Count: 7,
	}})
	app.Pump(70, 1)
	row := paintedRows(app, 70, 1)[0]
	titleAt := strings.Index(row, "Implementation plan")
	countAt := strings.Index(row, "3 / 7")
	upAt := strings.Index(row, "↑")
	downAt := strings.Index(row, "↓")
	arrowsAt := strings.Index(row, "↑ ↓")
	if titleAt < 0 || countAt < 0 || upAt < 0 || downAt < 0 || arrowsAt < 0 || !(titleAt < countAt && countAt < arrowsAt) {
		t.Fatalf("arrows should sit together after the title and count, row %q", row)
	}
	column, _ := findPaintedCellSequence(t, app, 70, 1, "Implementation plan")
	if got := app.Cell(column, 0).Foreground; got != theme.PrimaryText {
		t.Errorf("section title foreground = %#v, want primary text %#v", got, theme.PrimaryText)
	}
	if got := app.Cell(0, 0).Background; got != theme.Surface {
		t.Errorf("strip background = %#v, want surface %#v", got, theme.Surface)
	}
}
