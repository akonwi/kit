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

func TestResolveListWindowSpendsRowsOnOverflowOnlyWhenItemsAreHidden(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                     string
		count, rows, start, show int
		want                     listWindow
	}{
		{name: "fits", count: 3, rows: 5, show: 2, want: listWindow{End: 3}},
		{name: "top", count: 10, rows: 5, show: 0, want: listWindow{End: 4, Below: true}},
		{name: "middle", count: 10, rows: 5, show: 4, want: listWindow{Start: 2, End: 5, Above: true, Below: true}},
		{name: "bottom fills back to the top edge", count: 10, rows: 5, show: 9, want: listWindow{Start: 6, End: 10, Above: true}},
		{name: "scrolled past the end pulls items back", count: 10, rows: 5, start: 9, show: -1, want: listWindow{Start: 6, End: 10, Above: true}},
		{name: "reveal above", count: 10, rows: 5, start: 6, show: 3, want: listWindow{Start: 3, End: 6, Above: true, Below: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveListWindow(test.count, test.rows, test.start, test.show); got != test.want {
				t.Fatalf("window = %+v, want %+v", got, test.want)
			}
		})
	}
}

type pickerTestHarness struct {
	Theme  ui.Theme
	Picker picker
}

// Build fills the screen so every painted cell maps to one column of the
// painted rows.
func (w pickerTestHarness) Build(ui.BuildContext) ui.Widget {
	return ui.Provider[ui.Theme]{Value: w.Theme, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Foreground: w.Theme.Foreground, Background: w.Theme.Background}}, w.Picker,
	)}
}

func pickerTestItems() []pickerItem {
	return []pickerItem{
		{Key: "working", Label: "Working tree", Description: "Uncommitted changes", Meta: "2 drafts", Current: true},
		{Key: "main", Label: "main", Hint: "736efa9", Description: "docs(backlog): remove completed composer and session items"},
		{Key: "feature", Label: "feat/openapi-contract", Hint: "5ada7e8", Description: "feat(protocol): publish OpenAPI contract", Meta: "1 draft"},
		{Key: "locked", Label: "locked", Description: "Needs a clean tree", DisabledReason: "idle only"},
	}
}

func TestPickerRowsAlignUniformColumns(t *testing.T) {
	t.Parallel()
	application := uitest.New(pickerTestHarness{Theme: ui.DefaultThemeSet().Dark, Picker: picker{
		Title: "Select diff target", TitleMeta: "4 targets", Search: &textInputConfig{Placeholder: "Filter targets…"},
		Items: pickerTestItems(), Selection: "main", Footer: "↑↓ move · enter select · esc close",
	}})
	application.Pump(80, 24)
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	_, searchRow := assertPickerSearchField(t, rows, "Filter targets…")
	assertPickerTitleSpacing(t, rows, "Select diff target", searchRow)
	assertDialogRow(t, rows, "Select diff target", "│ Select diff target                                 4 targets │")
	// Label, hint, description, and metadata columns line up on every row.
	assertDialogRow(t, rows, "Working tree", "│ Working tree                    Uncommitted chang…  2 drafts │")
	assertDialogRow(t, rows, "736efa9", "│▌main                   736efa9  docs(backlog): re…           │")
	assertDialogRow(t, rows, "5ada7e8", "│ feat/openapi-contract  5ada7e8  feat(protocol): p…   1 draft │")
	assertDialogRow(t, rows, "locked", "│ locked                          ⊘ idle only · Nee…           │")
	assertPickerFooter(t, rows, "↑↓ move · enter select · esc close")
}

func TestPickerHighlightKeepsTextColorsAndCurrentAccent(t *testing.T) {
	t.Parallel()
	theme := ui.DefaultThemeSet().Light
	focus := semanticFallback(theme).Token(kittheme.TokenPickerFocusedBackground)
	tint := blendPickerColor(focus, theme.Background, pickerHighlightPercent, theme.SurfaceHovered)
	render := func(selection string) (*uitest.App, []string) {
		application := uitest.New(pickerTestHarness{Theme: theme, Picker: picker{Items: pickerTestItems(), Selection: selection}})
		application.Pump(80, 24)
		application.Pump(80, 24)
		return application, paintedRows(application, 80, 24)
	}

	// The current item is marked by its accent label alone.
	application, rows := render("main")
	column, row := findTextCell(t, rows, "Working tree")
	if cell := application.Cell(column, row); cell.Style.Foreground != theme.PrimaryText || cell.Style.Background != theme.Background {
		t.Fatalf("current label style = %+v, want accent %v on the dialog background", cell.Style, theme.PrimaryText)
	}
	// Highlighting adds a tinted fill and a gutter bar; text colors do not change.
	column, row = findTextCell(t, rows, "main")
	label, hint := application.Cell(column, row), application.Cell(column+23, row)
	if label.Style.Background != tint || label.Style.Attribute != 0 {
		t.Fatalf("highlighted label style = %+v, want regular weight on tint %v", label.Style, tint)
	}
	if hint.Character.Grapheme != "7" || hint.Style.Foreground != theme.MutedForeground || hint.Style.Background != tint {
		t.Fatalf("highlighted hint cell = %q %+v, want muted text on tint", hint.Character.Grapheme, hint.Style)
	}
	if bar := application.Cell(column-1, row); bar.Character.Grapheme != glyphLeftBar || bar.Style.Foreground != focus || bar.Style.Background != theme.Background {
		t.Fatalf("gutter = %q %+v, want %q in %v", bar.Character.Grapheme, bar.Style, glyphLeftBar, focus)
	}

	// A highlighted current item keeps its accent label.
	application, rows = render("working")
	column, row = findTextCell(t, rows, "Working tree")
	if cell := application.Cell(column, row); cell.Style.Foreground != theme.PrimaryText || cell.Style.Background != tint {
		t.Fatalf("highlighted current label style = %+v, want accent %v on tint %v", cell.Style, theme.PrimaryText, tint)
	}
}

func TestPickerRowsActivateOnClickAndIgnoreDisabledRows(t *testing.T) {
	t.Parallel()
	theme := ui.DefaultThemeSet().Dark
	var activated []string
	application := uitest.New(pickerTestHarness{Theme: theme, Picker: picker{
		Items: pickerTestItems(), Selection: "working",
		OnActivate: func(_ ui.EventContext, key string) { activated = append(activated, key) },
	}})
	application.Pump(80, 24)
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)

	// Hovering tints the whole row without changing its text color.
	column, row := findTextCell(t, rows, "feat/openapi-contract")
	application.Send(vaxis.Mouse{Col: column + 40, Row: row, EventType: vaxis.EventMotion})
	application.Pump(80, 24)
	focus := semanticFallback(theme).Token(kittheme.TokenPickerFocusedBackground)
	hover := blendPickerColor(focus, theme.Background, pickerHoverPercent, theme.SurfaceHovered)
	if cell := application.Cell(column, row); cell.Style.Background != hover {
		t.Fatalf("hovered row background = %v, want %v", cell.Style.Background, hover)
	}

	application.Click(column+40, row)
	_, lockedRow := findTextCell(t, rows, "locked")
	application.Click(column, lockedRow)
	application.Pump(80, 24)
	if strings.Join(activated, ",") != "feature" {
		t.Fatalf("activated = %v, want only the enabled row", activated)
	}
}

func TestPickerOverflowRowsScrollAPageWhenClicked(t *testing.T) {
	t.Parallel()
	items := make([]pickerItem, 30)
	for index := range items {
		items[index] = pickerItem{Key: fmt.Sprint(index), Label: fmt.Sprintf("Item %02d", index)}
	}
	application := uitest.New(pickerTestHarness{Theme: ui.DefaultThemeSet().Dark, Picker: picker{Items: items, Selection: "0"}})
	pump := func() []string {
		application.Pump(80, 24)
		application.Pump(80, 24)
		return paintedRows(application, 80, 24)
	}
	listText := func(rows []string) []string {
		_, _, top := paletteBorder(rows)
		footer := dialogBottom(rows) - 2
		result := []string{}
		for row := top + 1; row < footer; row++ {
			result = append(result, dialogRowText(rows, row))
		}
		return result
	}
	want := func(prefix []string, from, to int, suffix []string) []string {
		result := append([]string{}, prefix...)
		for index := from; index < to; index++ {
			label := items[index].Label
			if index == 0 {
				label = glyphLeftBar + label
			}
			result = append(result, label)
		}
		return append(result, suffix...)
	}

	// The list starts at the top with a trailing ⋯ for the hidden items.
	rows := pump()
	if got, expected := listText(rows), want(nil, 0, 15, []string{glyphEllipsis}); strings.Join(got, "|") != strings.Join(expected, "|") {
		t.Fatalf("initial list = %q, want %q", got, expected)
	}
	// Clicking the trailing ⋯ scrolls one page; both edges now mark hidden items.
	application.Click(12, findPaintedRow(rows, glyphEllipsis))
	rows = pump()
	if got, expected := listText(rows), want([]string{glyphEllipsis}, 14, 28, []string{glyphEllipsis}); strings.Join(got, "|") != strings.Join(expected, "|") {
		t.Fatalf("paged list = %q, want %q", got, expected)
	}
}

func TestPickerMessagesReplaceTheListAndStatusKeepsTheCloseHint(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		picker picker
		want   []string
	}{
		{name: "empty", picker: picker{Footer: "↑↓ move · esc close"}, want: []string{"│ No results", "│ ↑↓ move · esc close"}},
		{name: "error", picker: picker{Message: "Could not load models.", MessageTone: pickerToneDanger, Footer: "esc close"}, want: []string{"│ Could not load models.", "│ esc close"}},
		{name: "status", picker: picker{Items: pickerTestItems(), Footer: "↑↓ move · enter apply · esc close", Status: "Apply failed: offline", StatusTone: pickerToneDanger}, want: []string{"│ Apply failed: offline", "esc close │"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := uitest.New(pickerTestHarness{Theme: ui.DefaultThemeSet().Dark, Picker: test.picker})
			application.Pump(80, 24)
			text := application.Text()
			for _, expected := range test.want {
				if !strings.Contains(text, expected) {
					t.Fatalf("picker missing %q:\n%s", expected, text)
				}
			}
		})
	}
}
