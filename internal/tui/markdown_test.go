package tui

import (
	"strings"
	"testing"

	kitmarkdown "github.com/akonwi/kit/internal/markdown"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestShellRendersTranscriptAndActivityMarkdown(t *testing.T) {
	t.Parallel()

	messages := []transcriptMessage{
		{ID: "user_1", TurnID: "turn_1", Role: "user", Text: "**inspect** this"},
		{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Text: "## Plan\n\n- read\n- report", Thinking: "### Reasoning\n\n- inspect first", ToolCalls: []transcriptToolCall{{ID: "call_1", Name: "read"}}},
	}
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: messages, Scroll: &ui.ScrollController{},
	}})
	app.Pump(60, 16)
	rows := paintedRows(app, 60, 16)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{"inspect this", "Plan", "• read", "• report"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("transcript Markdown %q missing:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "**inspect**") || strings.Contains(text, "## Plan") {
		t.Fatalf("Markdown source markers leaked into transcript:\n%s", text)
	}

	layout := workspaceLayoutState{}
	app = uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: messages, Scroll: &ui.ScrollController{},
		ActivitySourceID: "turn-work:turn_1:assistant_1", ActivitySelected: true,
		ActivityScroll: &ui.ScrollController{}, ActivityList: &activityListController{},
		WorkspaceLayout: &layout, ActivityExpanded: map[activityToolKey]bool{},
	}})
	app.Pump(80, 28)
	activity := strings.Join(paintedRows(app, 80, 28), "\n")
	for _, expected := range []string{"Thinking", "Reasoning", "• inspect first", "Plan", "• read"} {
		if !strings.Contains(activity, expected) {
			t.Fatalf("Activity Markdown %q missing:\n%s", expected, activity)
		}
	}
	if strings.Contains(activity, "### Reasoning") || strings.Contains(activity, "## Plan") {
		t.Fatalf("Activity Markdown source markers leaked:\n%s", activity)
	}
}

func TestMarkdownViewRendersRichDocumentSemantics(t *testing.T) {
	t.Parallel()

	const source = "# Heading\n\nNormal **bold** *italic* ~~gone~~ and `code`.\n\n- first item\n- [x] done\n\n> quoted\n\n```go\nfmt.Println(1)\n```"
	app := uitest.New(markdownTestSurface(ui.SelectionArea{Child: markdownView{ID: "document", Source: source}}))
	app.Pump(48, 18)
	rows := paintedRows(app, 48, 18)

	expected := []string{
		"Heading",
		"Normal bold italic gone and code.",
		"• first item",
		"☑ done",
		"│ quoted",
		"fmt.Println(1)",
	}
	for _, text := range expected {
		if findPaintedRow(rows, text) < 0 {
			t.Errorf("rendered Markdown missing %q:\n%s", text, strings.Join(rows, "\n"))
		}
	}

	headingRow, headingColumn := markdownCellPosition(rows, "Heading")
	if headingRow < 0 || app.Cell(headingColumn, headingRow).Attribute&ui.AttrBold == 0 {
		t.Fatalf("heading is not bold at %d,%d", headingColumn, headingRow)
	}
	for text, attribute := range map[string]vaxis.AttributeMask{
		"bold": ui.AttrBold, "italic": ui.AttrItalic, "gone": ui.AttrStrikethrough,
	} {
		row, column := markdownCellPosition(rows, text)
		if row < 0 || app.Cell(column, row).Attribute&attribute == 0 {
			t.Errorf("%q missing attribute %v", text, attribute)
		}
	}
	row, column := markdownCellPosition(rows, "code")
	if row < 0 || app.Cell(column, row).Background == 0 {
		t.Errorf("inline code has no surface background")
	}
}

func TestMarkdownCodeBlockStartsWithSourceWithoutLanguageRow(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID: "code-language", Source: "```go\nfmt.Println(1)\n```",
	}))
	app.Pump(32, 4)
	rows := paintedRows(app, 32, 4)
	if got := strings.TrimSpace(rows[0]); got != "fmt.Println(1)" {
		t.Fatalf("first code-block row = %q, want source", got)
	}
}

func TestMarkdownViewUsesHangingListIndentAndAlignedOrderedMarkers(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID:     "list",
		Source: "9. a long list item that wraps at the edge\n10. another item",
	}))
	app.Pump(24, 8)
	rows := paintedRows(app, 24, 8)

	first := findPaintedRow(rows, "9. a long")
	second := findPaintedRow(rows, "10. another")
	if first < 0 || second < 0 {
		t.Fatalf("ordered list markers missing:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[first], " 9. ") || !strings.HasPrefix(rows[second], "10. ") {
		t.Fatalf("ordered markers are not right-aligned:\n%s", strings.Join(rows, "\n"))
	}
	if first+1 >= len(rows) || !strings.HasPrefix(rows[first+1], "    that wraps") {
		t.Fatalf("wrapped continuation lacks hanging indent:\n%s", strings.Join(rows, "\n"))
	}
}

func TestMarkdownSpacingUsesDeepestSharedList(t *testing.T) {
	t.Parallel()

	outer := kitmarkdown.Container{Kind: kitmarkdown.ContainerListItem, ListID: 1}
	inner := kitmarkdown.Container{Kind: kitmarkdown.ContainerListItem, ListID: 2, Loose: true}
	previous := kitmarkdown.Block{Containers: []kitmarkdown.Container{outer, inner}}
	current := kitmarkdown.Block{Containers: []kitmarkdown.Container{outer, inner}}
	if !markdownBlocksNeedGap(previous, current) {
		t.Fatal("loose nested list spacing was suppressed by tight outer list")
	}
}

func TestMarkdownViewKeepsLooseListParagraphSpacing(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID: "loose-list", Source: "- first\n\n  second paragraph\n\n- third",
	}))
	app.Pump(32, 10)
	rows := paintedRows(app, 32, 10)
	first := findPaintedRow(rows, "• first")
	second := findPaintedRow(rows, "second paragraph")
	third := findPaintedRow(rows, "• third")
	if first < 0 || second <= first+1 || third <= second+1 {
		t.Fatalf("loose list spacing missing:\n%s", strings.Join(rows, "\n"))
	}
}

func TestMarkdownViewKeepsMarkersForQuotedAndCodeListItems(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID: "block-list", Source: "- > quoted\n- ```go\n  code\n  ```",
	}))
	app.Pump(32, 10)
	rows := paintedRows(app, 32, 10)
	text := strings.Join(rows, "\n")
	if strings.Count(text, glyphBullet) != 2 || !strings.Contains(text, "quoted") || !strings.Contains(text, "code") {
		t.Fatalf("block list semantics missing:\n%s", text)
	}
}

func TestMarkdownViewPreservesContainerOrderAndOrderedTaskMarker(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID: "container-order", Source: "- > list quote\n\n> - quote list\n\n1. [x] task",
	}))
	app.Pump(40, 12)
	rows := paintedRows(app, 40, 12)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{"• │ list quote", "│ • quote list", "1. ☑ task"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("ordered container rendering %q missing:\n%s", expected, text)
		}
	}
}

func TestMarkdownViewShowsLiteralTargetsAndOnlyStylesSafeLinks(t *testing.T) {
	t.Parallel()

	const safe = "https://example.com/docs"
	const unsafe = "file:///tmp/private"
	app := uitest.New(markdownTestSurface(markdownView{
		ID: "links", Source: "[docs **here**](" + safe + ") and [private](" + unsafe + ")",
	}))
	app.Pump(96, 4)
	rows := paintedRows(app, 96, 4)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{"docs here (" + safe + ")", "private (" + unsafe + ")"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("literal link target %q missing:\n%s", expected, text)
		}
	}

	for _, label := range []string{"docs", "here", safe} {
		row, column := markdownCellPosition(rows, label)
		cell := app.Cell(column, row)
		if row < 0 || cell.Hyperlink != safe || cell.UnderlineStyle == ui.UnderlineOff {
			t.Fatalf("safe link %q cell = %#v", label, cell)
		}
	}
	row, column := markdownCellPosition(rows, unsafe)
	cell := app.Cell(column, row)
	if row < 0 || cell.Hyperlink != "" || cell.UnderlineStyle != ui.UnderlineOff ||
		cell.Foreground != ui.DefaultThemeSet().Dark.Foreground {
		t.Fatalf("unsafe link cell looks interactive: %#v", cell)
	}
}

func TestMarkdownViewRendersDestinationOnlyLinksAndImages(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID: "empty-links", Source: "[](https://example.com) ![](https://example.com/image.png)",
	}))
	app.Pump(96, 4)
	text := strings.Join(paintedRows(app, 96, 4), "\n")
	for _, expected := range []string{
		"link (https://example.com)", "image (https://example.com/image.png)",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("destination fallback %q missing:\n%s", expected, text)
		}
	}
}

func TestMarkdownViewRendersGFMTableWithinWidth(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID:     "table",
		Source: "| Name | Value |\n| :--- | ---: |\n| first item | 42 |",
	}))
	app.Pump(30, 6)
	app.Pump(30, 6)
	rows := paintedRows(app, 30, 6)
	if findPaintedRow(rows, "Name") < 0 || findPaintedRow(rows, "Value") < 0 ||
		findPaintedRow(rows, "first item") < 0 || findPaintedRow(rows, "42") < 0 {
		t.Fatalf("table content missing:\n%s", strings.Join(rows, "\n"))
	}
	valueRow, valueColumn := markdownCellPosition(rows, "Value")
	numberRow, numberColumn := markdownCellPosition(rows, "42")
	if valueRow < 0 || numberRow < 0 || valueColumn+len("Value") != numberColumn+len("42") {
		t.Fatalf("right-aligned column mismatch:\n%s", strings.Join(rows, "\n"))
	}
	for _, row := range rows {
		if len([]rune(row)) != 30 {
			t.Fatalf("table row width = %d, want 30: %q", len([]rune(row)), row)
		}
	}
}

func TestMarkdownViewKeepsCompactWideTableColumnsDense(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID:     "dense-table",
		Source: "| Flag | Description |\n|---|---|\n| -v | verbose output with a much longer explanation of behavior |",
	}))
	app.Pump(80, 8)
	app.Pump(80, 8)
	rows := paintedRows(app, 80, 8)
	_, descriptionColumn := markdownCellPosition(rows, "Description")
	if descriptionColumn < 0 || descriptionColumn > 12 {
		t.Fatalf("description column starts at %d, want dense placement:\n%s", descriptionColumn, strings.Join(rows, "\n"))
	}
	if findPaintedRow(rows, "verbose output with a much longer explanation of behavior") < 0 {
		t.Fatalf("wide description wrapped unnecessarily:\n%s", strings.Join(rows, "\n"))
	}
}

func TestMarkdownViewKeepsEveryNarrowTableColumnVisible(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID:     "narrow-table",
		Source: "| A1 | B2 | C3 | D4 | E5 | F6 |\n|---|---|---|---|---|---|\n| a | b | c | d | e | f |",
	}))
	app.Pump(8, 20)
	text := strings.Join(paintedRows(app, 8, 20), "\n")
	for _, expected := range []string{"A1", "B2", "C3", "D4", "E5", "F6", "a", "b", "c", "d", "e", "f"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("narrow table omitted %q:\n%s", expected, text)
		}
	}
}

func TestMarkdownViewKeepsQuoteGutterThroughParagraphSpacing(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID: "quote-spacing", Source: "> first paragraph\n>\n> second paragraph",
	}))
	app.Pump(40, 6)
	rows := paintedRows(app, 40, 6)
	first := findPaintedRow(rows, "first paragraph")
	second := findPaintedRow(rows, "second paragraph")
	if first < 0 || second != first+2 || !strings.Contains(rows[first+1], "│") {
		t.Fatalf("quote gutter is not continuous:\n%s", strings.Join(rows, "\n"))
	}
}

func TestMarkdownViewCapsPresentationalQuoteDepth(t *testing.T) {
	t.Parallel()

	app := uitest.New(markdownTestSurface(markdownView{
		ID: "deep-quote", Source: strings.Repeat("> ", 32) + "deep",
	}))
	app.Pump(40, 8)
	rows := paintedRows(app, 40, 8)
	row := findPaintedRow(rows, "deep")
	if row < 0 {
		t.Fatalf("deep quote content missing:\n%s", strings.Join(rows, "\n"))
	}
	if bars := strings.Count(rows[row], "│"); bars != maxMarkdownQuoteDepth {
		t.Fatalf("quote bars = %d, want capped %d: %q", bars, maxMarkdownQuoteDepth, rows[row])
	}
}

func TestMarkdownDocumentCacheCoalescesStreamingUpdates(t *testing.T) {
	t.Parallel()

	parses := 0
	config := markdownView{
		ID: "cached", Source: "**hello**",
		parseSource: func(source string) kitmarkdown.Document {
			parses++
			return kitmarkdown.Parse(source)
		},
	}
	cache := newMarkdownDocumentCache(config)
	if parses != 1 {
		t.Fatalf("initial parses = %d, want 1", parses)
	}
	cache.update(config.Source)
	if cache.flush(config) || parses != 1 {
		t.Fatalf("unchanged flush = %t, parses = %d", cache.parsePending, parses)
	}
	cache.update("**hello w")
	cache.update("**hello world**")
	if !cache.flush(config) || parses != 2 {
		t.Fatalf("coalesced flush parses = %d, want 2", parses)
	}
	if got := cache.document.Blocks[0].Runs[0]; got.Text != "hello world" || !got.Bold {
		t.Fatalf("coalesced document = %#v", cache.document)
	}
}

func TestMountedMarkdownViewUpdatesStreamingSourceWithoutLifecyclePanic(t *testing.T) {
	t.Parallel()

	state := &markdownLifecycleHarnessState{source: "**hello**"}
	app := uitest.New(markdownTestSurface(markdownLifecycleHarness{State: state}))
	app.Pump(30, 4)
	state.SetState(func() { state.source = "**hello world**" })
	app.Pump(30, 4)
	rows := paintedRows(app, 30, 4)
	row, column := markdownCellPosition(rows, "hello world")
	if row < 0 || app.Cell(column, row).Attribute&ui.AttrBold == 0 {
		t.Fatalf("updated Markdown missing:\n%s", strings.Join(rows, "\n"))
	}
}

func TestMarkdownViewPreservesMutedBaseStyleAcrossBlocks(t *testing.T) {
	t.Parallel()

	theme := ui.DefaultThemeSet().Dark
	base := ui.Style{Foreground: theme.DangerText}
	app := uitest.New(markdownThemedTestSurface(theme, markdownView{
		ID: "error", Source: "# Failure\n\n> details\n\n`code`", BaseStyle: base,
	}))
	app.Pump(30, 8)
	rows := paintedRows(app, 30, 8)
	for _, text := range []string{"Failure", "details", "code"} {
		row, column := markdownCellPosition(rows, text)
		if row < 0 || app.Cell(column, row).Foreground != base.Foreground {
			t.Errorf("%q foreground = %v, want base %v", text, app.Cell(column, row).Foreground, base.Foreground)
		}
	}
}

type markdownLifecycleHarness struct {
	State *markdownLifecycleHarnessState
}

func (w markdownLifecycleHarness) CreateState() ui.State { return w.State }

type markdownLifecycleHarnessState struct {
	ui.StateBase
	source string
}

func (s *markdownLifecycleHarnessState) Build(ui.BuildContext) ui.Widget {
	return markdownView{ID: "lifecycle", Source: s.source}
}

func markdownTestSurface(child ui.Widget) ui.Widget {
	return markdownThemedTestSurface(ui.DefaultThemeSet().Dark, child)
}

func markdownThemedTestSurface(theme ui.Theme, child ui.Widget) ui.Widget {
	return ui.Provider[ui.Theme]{
		Value: theme,
		Child: ui.DecoratedBox(
			ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}},
			child,
		),
	}
}

func markdownCellPosition(rows []string, text string) (int, int) {
	for row, line := range rows {
		byteOffset := strings.Index(line, text)
		if byteOffset >= 0 {
			return row, len([]rune(line[:byteOffset]))
		}
	}
	return -1, -1
}
