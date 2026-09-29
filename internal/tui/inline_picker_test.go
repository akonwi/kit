package tui

import (
	"fmt"
	"strings"
	"testing"

	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

// inlinePickerBox returns the rows of the inline picker painted lowest on the
// screen, from its top border to its bottom border, cut to its width.
func inlinePickerBox(t *testing.T, rows []string) (top int, box []string) {
	t.Helper()
	bottom := -1
	for row := len(rows) - 1; row >= 0; row-- {
		if strings.HasPrefix(strings.TrimLeft(rows[row], " "), "└") {
			bottom = row
			break
		}
	}
	top = -1
	for row := bottom; row >= 0; row-- {
		if strings.HasPrefix(strings.TrimLeft(rows[row], " "), "┌") {
			top = row
			break
		}
	}
	if bottom < 0 || top < 0 {
		t.Fatalf("no inline picker painted:\n%s", strings.Join(rows, "\n"))
	}
	end := len([]rune(rows[top][:strings.Index(rows[top], "┐")])) + 1
	for row := top; row <= bottom; row++ {
		box = append(box, string([]rune(rows[row])[:end]))
	}
	return top, box
}

// assertInlinePickerRows asserts the exact rows of the painted inline picker.
func assertInlinePickerRows(t *testing.T, rows []string, want []string) {
	t.Helper()
	_, box := inlinePickerBox(t, rows)
	if strings.Join(box, "\n") != strings.Join(want, "\n") {
		t.Fatalf("inline picker rows:\n%s\nwant:\n%s", strings.Join(box, "\n"), strings.Join(want, "\n"))
	}
}

type inlinePickerHarness struct {
	Theme  ui.Theme
	Picker inlinePicker
}

// Build fills the screen so every painted cell maps to one column of the
// painted rows.
func (w inlinePickerHarness) Build(ui.BuildContext) ui.Widget {
	return ui.Provider[ui.Theme]{Value: w.Theme, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Foreground: w.Theme.Foreground, Background: w.Theme.Background}}, w.Picker,
	)}
}

func renderInlinePicker(picker inlinePicker, width, height int) (*uitest.App, []string) {
	application := uitest.New(inlinePickerHarness{Theme: ui.DefaultThemeSet().Dark, Picker: picker})
	application.Pump(width, height)
	return application, paintedRows(application, width, height)
}

func insetRows(rows int) func(int) int { return func(int) int { return rows } }

func TestInlinePickerSharesPaletteRowsAndFooterLeftAlignedAboveTheInset(t *testing.T) {
	t.Parallel()
	_, rows := renderInlinePicker(inlinePicker{
		Catalog: pickerTestItems(), Selection: "main", Footer: "↑↓ move · enter insert · esc close",
		Left: 2, BottomInset: insetRows(3),
	}, 80, 24)
	top, _ := inlinePickerBox(t, rows)
	// No title or search field: the rows start under the top border, and the
	// footer sits below a divider that joins both borders.
	assertInlinePickerRows(t, rows, []string{
		"  ┌──────────────────────────────────────────────────────────────┐",
		"  │ Working tree                    Uncommitted chang…  2 drafts │",
		"  │▌main                   736efa9  docs(backlog): re…           │",
		"  │ feat/openapi-contract  5ada7e8  feat(protocol): p…   1 draft │",
		"  │ locked                          ⊘ idle only · Nee…           │",
		"  ├──────────────────────────────────────────────────────────────┤",
		"  │ ↑↓ move · enter insert · esc close                           │",
		"  └──────────────────────────────────────────────────────────────┘",
	})
	if top != 13 {
		t.Fatalf("top border row = %d, want 13 so the bottom border sits 3 rows above the screen edge", top)
	}
}

func TestInlinePickerWidthFollowsThePalettePickerRule(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		width, left  int
		wantX, wantW int
	}{
		{name: "80 percent", width: 80, wantW: 64},
		{name: "minimum", width: 50, wantW: 48},
		{name: "maximum", width: 200, wantW: 96},
		{name: "narrow terminal", width: 40, wantW: 40},
		{name: "left edge", width: 80, left: 20, wantX: 20, wantW: 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, rows := renderInlinePicker(inlinePicker{Catalog: pickerTestItems(), Footer: "esc close", Left: test.left}, test.width, 24)
			top, _ := inlinePickerBox(t, rows)
			border := []rune(rows[top])
			want := strings.Repeat(" ", test.wantX) + "┌" + strings.Repeat("─", test.wantW-2) + "┐"
			if got := strings.TrimRight(string(border), " "); got != want {
				t.Fatalf("top border = %q, want %q", got, want)
			}
		})
	}
}

func TestInlinePickerGrowsUpwardWithAFixedBottomEdgeWhileFiltering(t *testing.T) {
	t.Parallel()
	render := func(query string) []string {
		_, rows := renderInlinePicker(inlinePicker{
			Query: query, Catalog: pickerTestItems(), Selection: "main", Footer: "esc close", BottomInset: insetRows(3),
		}, 80, 24)
		return rows
	}
	for _, test := range []struct {
		query string
		top   int
		want  []string
	}{
		{query: "", top: 13, want: []string{
			"│ Working tree                    Uncommitted chang…  2 drafts │",
			"│▌main                   736efa9  docs(backlog): re…           │",
			"│ feat/openapi-contract  5ada7e8  feat(protocol): p…   1 draft │",
			"│ locked                          ⊘ idle only · Nee…           │",
		}},
		// Columns keep the whole catalog's widths while filtered.
		{query: "main", top: 16, want: []string{
			"│▌main                   736efa9  docs(backlog): re…           │",
		}},
		{query: "unmatched", top: 16, want: []string{
			"│ No results                                                   │",
		}},
	} {
		rows := render(test.query)
		top, _ := inlinePickerBox(t, rows)
		if top != test.top {
			t.Fatalf("query %q top border row = %d, want %d", test.query, top, test.top)
		}
		want := append([]string{"┌" + strings.Repeat("─", 62) + "┐"}, test.want...)
		want = append(want,
			"├"+strings.Repeat("─", 62)+"┤",
			"│ esc close                                                    │",
			"└"+strings.Repeat("─", 62)+"┘",
		)
		assertInlinePickerRows(t, rows, want)
	}
}

func inlinePickerNumberedItems(count int) []pickerItem {
	items := make([]pickerItem, count)
	for index := range items {
		items[index] = pickerItem{Key: fmt.Sprint(index), Label: fmt.Sprintf("Item %02d", index)}
	}
	return items
}

// inlinePickerListRows returns the list rows between the top border and the
// footer divider, without their borders and trailing spaces.
func inlinePickerListRows(t *testing.T, rows []string) []string {
	t.Helper()
	_, box := inlinePickerBox(t, rows)
	list := []string{}
	for _, row := range box[1 : len(box)-3] {
		runes := []rune(row)
		list = append(list, strings.TrimRight(string(runes[1:len(runes)-1]), " "))
	}
	return list
}

func TestInlinePickerFitsTenRowsAndOverflowRowsScrollAPage(t *testing.T) {
	t.Parallel()
	items := inlinePickerNumberedItems(30)
	application, rows := renderInlinePicker(inlinePicker{Catalog: items, Selection: "0", Footer: "esc close", BottomInset: insetRows(3)}, 80, 24)
	want := []string{"▌Item 00", " Item 01", " Item 02", " Item 03", " Item 04", " Item 05", " Item 06", " Item 07", " Item 08", " " + glyphEllipsis}
	if got := inlinePickerListRows(t, rows); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("list = %q, want %q", got, want)
	}
	application.Click(2, findPaintedRow(rows, glyphEllipsis))
	application.Pump(80, 24)
	rows = paintedRows(application, 80, 24)
	want = []string{" " + glyphEllipsis, " Item 08", " Item 09", " Item 10", " Item 11", " Item 12", " Item 13", " Item 14", " Item 15", " " + glyphEllipsis}
	if got := inlinePickerListRows(t, rows); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("paged list = %q, want %q", got, want)
	}

	// A selection at the end is revealed with the hidden rows above it.
	_, rows = renderInlinePicker(inlinePicker{Catalog: items[:14], Selection: "13", Footer: "esc close"}, 80, 24)
	want = []string{" " + glyphEllipsis, " Item 05", " Item 06", " Item 07", " Item 08", " Item 09", " Item 10", " Item 11", " Item 12", "▌Item 13"}
	if got := inlinePickerListRows(t, rows); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("revealed list = %q, want %q", got, want)
	}
}

func TestInlinePickerHighlightHoverAndClicks(t *testing.T) {
	t.Parallel()
	theme := ui.DefaultThemeSet().Dark
	var activated []string
	application := uitest.New(inlinePickerHarness{Theme: theme, Picker: inlinePicker{
		Catalog: pickerTestItems(), Selection: "main", Footer: "esc close",
		OnActivate: func(_ ui.EventContext, key string) { activated = append(activated, key) },
	}})
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	focus := semanticFallback(theme).Token(kittheme.TokenPickerFocusedBackground)

	// The highlight is the shared 20% tint with the gutter bar; text keeps
	// its colors, and the current item keeps its accent label.
	tint := blendPickerColor(focus, theme.Background, pickerHighlightPercent, theme.SurfaceHovered)
	column, row := findTextCell(t, rows, "main")
	if cell := application.Cell(column, row); cell.Style.Background != tint || cell.Style.Foreground == theme.MutedForeground {
		t.Fatalf("highlighted label style = %+v, want item text on tint %v", cell.Style, tint)
	}
	if bar := application.Cell(column-1, row); bar.Character.Grapheme != glyphLeftBar || bar.Style.Foreground != focus {
		t.Fatalf("gutter = %q %+v, want %q in %v", bar.Character.Grapheme, bar.Style, glyphLeftBar, focus)
	}
	column, row = findTextCell(t, rows, "Working tree")
	if cell := application.Cell(column, row); cell.Style.Foreground != theme.PrimaryText || cell.Style.Background != theme.Background {
		t.Fatalf("current label style = %+v, want accent %v", cell.Style, theme.PrimaryText)
	}

	// Hovering tints the row 8%.
	column, row = findTextCell(t, rows, "feat/openapi-contract")
	application.Send(vaxis.Mouse{Col: column + 30, Row: row, EventType: vaxis.EventMotion})
	application.Pump(80, 24)
	hover := blendPickerColor(focus, theme.Background, pickerHoverPercent, theme.SurfaceHovered)
	if cell := application.Cell(column, row); cell.Style.Background != hover {
		t.Fatalf("hovered row background = %v, want %v", cell.Style.Background, hover)
	}

	// Enabled rows activate on click; disabled rows do not.
	application.Click(column+30, row)
	_, lockedRow := findTextCell(t, rows, "locked")
	application.Click(column, lockedRow)
	if strings.Join(activated, ",") != "feature" {
		t.Fatalf("activated = %v, want only the enabled row", activated)
	}
}

func TestInlinePickerMessagesAndStatusReplaceOneRow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		picker inlinePicker
		want   []string
	}{
		{name: "loading", picker: inlinePicker{Message: "Loading files…", MessageTone: pickerToneLoading, Footer: "↑↓ move · esc close"},
			want: []string{"│ " + spinnerFrames[0] + " Loading files…", "│ ↑↓ move · esc close"}},
		{name: "error", picker: inlinePicker{Catalog: pickerTestItems(), Message: "Could not load files: offline", MessageTone: pickerToneDanger, Footer: "esc close"},
			want: []string{"│ Could not load files: offline", "│ esc close"}},
		{name: "status", picker: inlinePicker{Catalog: pickerTestItems()[:1], Footer: "↑↓ move · enter insert · esc close", Status: "Showing first 4,000 indexed paths"},
			want: []string{"│ Working tree", "│ Showing first 4,000 indexed paths", "esc close │"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, rows := renderInlinePicker(test.picker, 80, 24)
			_, box := inlinePickerBox(t, rows)
			if len(box) != 5 {
				t.Fatalf("picker rows = %d, want one list row:\n%s", len(box), strings.Join(box, "\n"))
			}
			if !strings.HasPrefix(box[1], test.want[0]) || !strings.HasPrefix(box[3], test.want[1]) {
				t.Fatalf("picker =\n%s\nwant list %q and footer %q", strings.Join(box, "\n"), test.want[0], test.want[1])
			}
			if len(test.want) > 2 && !strings.HasSuffix(box[3], test.want[2]) {
				t.Fatalf("footer = %q, want it to end with %q", box[3], test.want[2])
			}
		})
	}
}

// inlinePickerHost rebuilds one mounted inline picker with new fields, as an
// owner does while the composer text or the catalog changes.
type inlinePickerHost struct{ state *inlinePickerHostState }

func (w inlinePickerHost) CreateState() ui.State { return w.state }

type inlinePickerHostState struct {
	ui.StateBase
	picker inlinePicker
}

func (s *inlinePickerHostState) Build(ui.BuildContext) ui.Widget {
	return inlinePickerHarness{Theme: ui.DefaultThemeSet().Dark, Picker: s.picker}
}

func (s *inlinePickerHostState) update(change func(*inlinePicker)) {
	s.SetState(func() { change(&s.picker) })
}

func TestInlinePickerKeepsItsBottomEdgeAndSelectionWhileTheCatalogChanges(t *testing.T) {
	t.Parallel()
	host := &inlinePickerHostState{picker: inlinePicker{
		Catalog: pickerTestItems(), Selection: "main", Footer: "esc close", BottomInset: insetRows(3),
	}}
	application := uitest.New(inlinePickerHost{state: host})
	pump := func() []string {
		application.Pump(80, 24)
		return paintedRows(application, 80, 24)
	}
	bottom := func(rows []string) int {
		top, box := inlinePickerBox(t, rows)
		return top + len(box) - 1
	}
	rows := pump()
	for _, query := range []string{"m", "ma", "main", "mainx", ""} {
		host.update(func(picker *inlinePicker) { picker.Query = query })
		if got := bottom(pump()); got != 20 {
			t.Fatalf("query %q bottom border row = %d, want 20", query, got)
		}
	}
	if got := bottom(rows); got != 20 {
		t.Fatalf("initial bottom border row = %d, want 20", got)
	}

	// Older rows arrive above a kept selection at the top; the list scrolls
	// just far enough to keep it in view, showing the new rows above it.
	items := inlinePickerNumberedItems(20)
	host.update(func(picker *inlinePicker) { picker.Catalog, picker.Selection = items[10:], "10" })
	if got, want := inlinePickerListRows(t, pump()), "▌Item 10"; got[0] != want {
		t.Fatalf("first row = %q, want %q", got[0], want)
	}
	host.update(func(picker *inlinePicker) { picker.Catalog = items })
	want := []string{" " + glyphEllipsis, " Item 03", " Item 04", " Item 05", " Item 06", " Item 07", " Item 08", " Item 09", "▌Item 10", " " + glyphEllipsis}
	if got := inlinePickerListRows(t, pump()); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("list after older rows = %q, want %q", got, want)
	}
}
