package tui

import (
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
	"strings"
	"testing"
)

func TestPluginFooterShowsStyledItemsBesideLocationAndRestoresDefault(t *testing.T) {
	footer := &protocol.PluginFooter{Items: []protocol.PluginFooterItem{{ID: "demo.status", Content: []protocol.PluginFooterSegment{{Text: "Ready", Style: protocol.PluginFooterStyle{Bold: true, FG: "toolText"}}}}}}
	app := uitest.New(ui.SizedBox{Width: 40, Height: 1, Child: pluginFooterView{Location: "~/project", Footer: footer}})
	app.Pump(40, 1)
	app.Pump(40, 1)
	rows := paintedRows(app, 40, 1)
	if got := strings.TrimSpace(rows[0]); got != "~/project · Ready" {
		t.Fatalf("footer = %q", got)
	}
	footer.LocationHidden = true
	hidden := uitest.New(ui.SizedBox{Width: 40, Height: 1, Child: pluginFooterView{Location: "~/project", Footer: footer}})
	hidden.Pump(40, 1)
	if got := strings.TrimSpace(paintedRows(hidden, 40, 1)[0]); got != "Ready" {
		t.Fatalf("first-frame replacement = %q", got)
	}
	hidden.Pump(40, 1)
	if got := strings.TrimSpace(paintedRows(hidden, 40, 1)[0]); got != "Ready" {
		t.Fatalf("replacement = %q", got)
	}
	restored := uitest.New(ui.SizedBox{Width: 40, Height: 1, Child: pluginFooterView{Location: "~/project", Footer: &protocol.PluginFooter{}}})
	restored.Pump(40, 1)
	restored.Pump(40, 1)
	if got := strings.TrimSpace(paintedRows(restored, 40, 1)[0]); got != "~/project" {
		t.Fatalf("restored = %q", got)
	}
	style := footerStyle(footer.Items[0].Content[0].Style, ui.Theme{}, SemanticTheme{Tokens: map[string]ui.Color{"toolText": ui.Color(42)}})
	if style.Attribute != ui.AttrBold || style.Foreground != ui.Color(42) {
		t.Fatalf("semantic style = %#v", style)
	}
}

func TestPluginFooterUsesLabeledOverflow(t *testing.T) {
	footer := &protocol.PluginFooter{LocationHidden: true, Items: []protocol.PluginFooterItem{{Content: []protocol.PluginFooterSegment{{Text: strings.Repeat("long", 10)}}}, {Content: []protocol.PluginFooterSegment{{Text: "Second"}}}}}
	app := uitest.New(ui.SizedBox{Width: 20, Height: 1, Child: pluginFooterView{Footer: footer}})
	app.Pump(20, 1)
	app.Pump(20, 1)
	if got := strings.TrimSpace(paintedRows(app, 20, 1)[0]); got != "… 2 more" {
		t.Fatalf("overflow = %q", got)
	}
}

func TestPluginFooterMetadataRefreshesDuringRunAndRejectsStaleBaseline(t *testing.T) {
	state := appState{session: protocol.SessionInfo{ID: "session"}, activeRunID: "run", liveSequence: 10, liveStreamID: "stream", metadataStreamID: "stream", metadataSequence: 1}
	footer := &protocol.PluginFooter{LocationHidden: true, Items: []protocol.PluginFooterItem{{ID: "demo.status", Content: []protocol.PluginFooterSegment{{Text: "Current"}}}}}
	state.applySessionMetadataBaseline(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session"}, EventStreamID: "stream", EventCursor: 5, PluginFooter: footer})
	if state.pluginFooter != footer {
		t.Fatal("active model run blocked footer metadata")
	}
	state.applySessionMetadataBaseline(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session"}, EventStreamID: "stream", EventCursor: 4, PluginFooter: &protocol.PluginFooter{}})
	if state.pluginFooter != footer {
		t.Fatal("older baseline replaced current footer")
	}
}

func TestPluginFooterReservesCompleteOverflowLabelWithLocation(t *testing.T) {
	footer := &protocol.PluginFooter{Items: []protocol.PluginFooterItem{{Content: []protocol.PluginFooterSegment{{Text: strings.Repeat("x", 20)}}}}}
	app := uitest.New(ui.SizedBox{Width: 16, Height: 1, Child: pluginFooterView{Location: "~/very-long-project", Footer: footer}})
	app.Pump(16, 1)
	app.Pump(16, 1)
	if got := strings.TrimSpace(paintedRows(app, 16, 1)[0]); got != glyphEllipsis+"ject · … 1 more" {
		t.Fatalf("reserved overflow label = %q", got)
	}
}

func TestPluginFooterReloadAdoptsNewStreamWithoutOverwritingNewerWatcher(t *testing.T) {
	stale := &protocol.PluginFooter{LocationHidden: true}
	state := appState{session: protocol.SessionInfo{ID: "session"}, metadataStreamID: "old", metadataSequence: 4, pluginFooter: stale}
	cleared := &protocol.PluginFooter{}
	state.applyPostReloadSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session"}, EventStreamID: "reloaded", EventCursor: 1, PluginFooter: cleared}, "old")
	if state.pluginFooter != cleared || state.metadataStreamID != "reloaded" {
		t.Fatal("reload retained revoked footer")
	}
	current := &protocol.PluginFooter{Items: []protocol.PluginFooterItem{{ID: "demo.current"}}}
	state.pluginFooter = current
	state.metadataStreamID = "newer"
	state.applyPostReloadSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session"}, EventStreamID: "reloaded", EventCursor: 2, PluginFooter: cleared}, "old")
	if state.pluginFooter != current || state.metadataStreamID != "newer" {
		t.Fatal("delayed reload rewound newer metadata")
	}
}

// footerPaintedRow reconstructs the single footer row with column-aligned
// cells so substring positions correspond to click coordinates.
func footerPaintedRow(app *uitest.App, width int) string {
	var row strings.Builder
	for column := 0; column < width; column++ {
		grapheme := app.Cell(column, 0).Grapheme
		if grapheme == "" {
			grapheme = " "
		}
		row.WriteString(grapheme)
	}
	return row.String()
}

func footerColumn(t *testing.T, app *uitest.App, width int, value string) int {
	t.Helper()
	row := footerPaintedRow(app, width)
	index := strings.Index(row, value)
	if index < 0 {
		t.Fatalf("%q not painted in %q", value, row)
	}
	return len([]rune(row[:index]))
}

func TestFooterOnlyPullRequestLabelIsLinked(t *testing.T) {
	const target = "https://github.com/akonwi/kit/pull/123"
	location := "~/repo (main* · PR #123)"
	var opened []string
	view := pluginFooterView{
		Location: location, LocationURL: target, LocationLinkText: "PR #123",
		OpenURL: func(_ ui.EventContext, raw string) { opened = append(opened, raw) },
	}
	app := uitest.New(ui.SizedBox{Width: 40, Height: 1, Child: view})
	app.Pump(40, 1)
	app.Pump(40, 1)
	if got := strings.TrimSpace(footerPaintedRow(app, 40)); got != location {
		t.Fatalf("footer = %q, want %q", got, location)
	}
	start := footerColumn(t, app, 40, "~/repo")
	suffix := footerColumn(t, app, 40, "PR #123")
	for column := start; column < start+len([]rune(location)); column++ {
		cell := app.Cell(column, 0)
		if column >= suffix && column < suffix+len("PR #123") {
			if cell.Hyperlink != target || cell.Style.UnderlineStyle != ui.UnderlineDouble {
				t.Fatalf("PR cell %d: %+v", column, cell)
			}
		} else if cell.Hyperlink != "" || cell.Style.UnderlineStyle != ui.UnderlineOff {
			t.Fatalf("plain location cell %d inherited link styling: %+v", column, cell)
		}
	}
	app.Click(start+2, 0)
	app.Click(suffix-3, 0)
	app.Click(suffix+len("PR #123"), 0)
	if len(opened) != 0 {
		t.Fatalf("cwd/branch/punctuation opened %v", opened)
	}
	app.Click(suffix+3, 0)
	if len(opened) != 1 || opened[0] != target {
		t.Fatalf("PR click opened %v", opened)
	}
}

func TestFooterLocationWithoutPullRequestIsNotClickable(t *testing.T) {
	var opened []string
	view := pluginFooterView{
		Location: "~/repo (main)",
		OpenURL:  func(_ ui.EventContext, raw string) { opened = append(opened, raw) },
	}
	app := uitest.New(ui.SizedBox{Width: 40, Height: 1, Child: view})
	app.Pump(40, 1)
	app.Pump(40, 1)
	start := footerColumn(t, app, 40, "~/repo")
	if got := app.Cell(start, 0).Hyperlink; got != "" {
		t.Fatalf("plain location carries hyperlink %q", got)
	}
	if got := app.Cell(start, 0).Style.UnderlineStyle; got != ui.UnderlineOff {
		t.Fatalf("plain location is underlined: %v", got)
	}
	app.Click(start+2, 0)
	if len(opened) != 0 {
		t.Fatalf("plain location click opened %v", opened)
	}
}

func TestFooterPullRequestRespectsPluginLocationHidden(t *testing.T) {
	var opened []string
	footer := &protocol.PluginFooter{LocationHidden: true}
	view := pluginFooterView{
		Location: "~/repo (main · PR #9)", LocationLinkText: "PR #9", LocationURL: "https://github.com/akonwi/kit/pull/9",
		Footer:  footer,
		OpenURL: func(_ ui.EventContext, raw string) { opened = append(opened, raw) },
	}
	app := uitest.New(ui.SizedBox{Width: 40, Height: 1, Child: view})
	app.Pump(40, 1)
	app.Pump(40, 1)
	if got := strings.TrimSpace(paintedRows(app, 40, 1)[0]); got != "" {
		t.Fatalf("hidden location painted %q", got)
	}
	app.Click(20, 0)
	if len(opened) != 0 {
		t.Fatalf("hidden location click opened %v", opened)
	}
}

func TestFooterPullRequestLocationCoexistsWithPluginItems(t *testing.T) {
	const target = "https://github.com/akonwi/kit/pull/8"
	footer := &protocol.PluginFooter{Items: []protocol.PluginFooterItem{{ID: "demo.status", Content: []protocol.PluginFooterSegment{{Text: "Ready"}}}}}
	var opened []string
	view := pluginFooterView{
		Location: "~/repo (main · PR #8)", LocationLinkText: "PR #8", LocationURL: target, Footer: footer,
		OpenURL: func(_ ui.EventContext, raw string) { opened = append(opened, raw) },
	}
	app := uitest.New(ui.SizedBox{Width: 60, Height: 1, Child: view})
	app.Pump(60, 1)
	app.Pump(60, 1)
	if got := strings.TrimSpace(footerPaintedRow(app, 60)); got != "~/repo (main · PR #8) · Ready" {
		t.Fatalf("footer = %q", got)
	}
	start := footerColumn(t, app, 60, "PR #8")
	app.Click(start+3, 0)
	if len(opened) != 1 || opened[0] != target {
		t.Fatalf("opened = %v", opened)
	}
	ready := footerColumn(t, app, 60, "Ready")
	if got := app.Cell(ready, 0).Hyperlink; got != "" {
		t.Fatalf("plugin item inherited hyperlink %q", got)
	}
	app.Click(ready+1, 0)
	if len(opened) != 1 {
		t.Fatalf("plugin item click opened %v", opened)
	}
}

func TestLocationPRLinkSurvivesOnlyCompleteLabelTruncation(t *testing.T) {
	target := "https://github.com/a/b/pull/123"
	for _, test := range []struct {
		location string
		linked   bool
	}{
		{"…/目录 (main · PR #123)", true},
		{"… PR #123)", true},
		{"…R #123)", false},
		{"~/PR #123/repo (main)", false},
	} {
		spans := locationSpans(test.location, "PR #123", target, nil, ui.Style{})
		var text, linked string
		for _, span := range spans {
			text += span.Text
			if span.Style.Hyperlink != "" {
				linked += span.Text
			}
		}
		wantLink := ""
		if test.linked {
			wantLink = "PR #123"
		}
		if text != test.location || linked != wantLink {
			t.Fatalf("location=%q text=%q link=%q want=%q", test.location, text, linked, wantLink)
		}
	}
}

func TestFooterLocationKeepsCompleteVCSMetadataAtNarrowWidths(t *testing.T) {
	const target = "https://github.com/a/b/pull/8"
	const cwd = "~/very/long/project"
	const location = cwd + " (feature* · PR #8)"
	for _, test := range []struct {
		name   string
		width  int
		footer *protocol.PluginFooter
		want   string
	}{
		{name: "no plugins", width: 28, want: glyphEllipsis + "/project (feature* · PR #8)"},
		{name: "one plugin", width: 32, footer: &protocol.PluginFooter{Items: []protocol.PluginFooterItem{{Content: []protocol.PluginFooterSegment{{Text: "Ready"}}}}}, want: glyphEllipsis + "ject (feature* · PR #8) · Ready"},
		{name: "overflow", width: 29, footer: &protocol.PluginFooter{Items: []protocol.PluginFooterItem{{Content: []protocol.PluginFooterSegment{{Text: "Very long status"}}}, {Content: []protocol.PluginFooterSegment{{Text: "Second"}}}}}, want: "(feature* · PR #8) · … 2 more"},
		{name: "suffix only", width: 18, want: "(feature* · PR #8)"},
		{name: "PR only", width: 7, want: "(PR #8)"},
		{name: "too narrow for PR", width: 6, want: glyphEllipsis},
		{name: "hidden", width: 32, footer: &protocol.PluginFooter{LocationHidden: true}, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var opened []string
			view := pluginFooterView{
				Location: location, LocationBase: cwd, LocationLinkText: "PR #8", LocationURL: target, Footer: test.footer,
				OpenURL: func(_ ui.EventContext, raw string) { opened = append(opened, raw) },
			}
			app := uitest.New(ui.SizedBox{Width: test.width, Height: 1, Child: view})
			app.Pump(test.width, 1)
			app.Pump(test.width, 1)
			if got := strings.TrimSpace(footerPaintedRow(app, test.width)); got != test.want {
				t.Fatalf("footer = %q, want %q", got, test.want)
			}
			if strings.Contains(test.want, "PR #8") {
				start := footerColumn(t, app, test.width, "PR #8")
				for column := start; column < start+len("PR #8"); column++ {
					if got := app.Cell(column, 0).Hyperlink; got != target {
						t.Fatalf("PR cell %d link = %q, want %q", column, got, target)
					}
				}
				app.Click(start+2, 0)
				if len(opened) != 1 || opened[0] != target {
					t.Fatalf("truncated PR click opened %v, want %q", opened, target)
				}
			}
		})
	}
}

func TestFooterLocationTruncatesUnicodeCWDByCellsBeforeVCS(t *testing.T) {
	const cwd = "~/世界/研究/プロジェクト"
	const suffix = " (界界* · PR #42)"
	const width = 28
	view := pluginFooterView{Location: cwd + suffix, LocationBase: cwd, LocationLinkText: "PR #42", LocationURL: "https://github.com/a/b/pull/42"}
	app := uitest.New(ui.SizedBox{Width: width, Height: 1, Child: view})
	app.Pump(width, 1)
	app.Pump(width, 1)
	if got, want := strings.TrimSpace(footerPaintedRow(app, width)), glyphEllipsis+"ロ ジ ェ ク ト  (界 界 * · PR #42)"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
}

func TestFooterLocationWithoutPRKeepsBranchWhole(t *testing.T) {
	const cwd = "~/deep/path/to/worktree"
	for _, test := range []struct {
		width int
		want  string
	}{
		{20, glyphEllipsis + "e (feature-branch*)"},
		{17, "(feature-branch*)"},
		{16, glyphEllipsis + "ath/to/worktree"},
	} {
		app := uitest.New(ui.SizedBox{Width: test.width, Height: 1, Child: pluginFooterView{Location: cwd + " (feature-branch*)", LocationBase: cwd}})
		app.Pump(test.width, 1)
		app.Pump(test.width, 1)
		if got := strings.TrimSpace(footerPaintedRow(app, test.width)); got != test.want {
			t.Fatalf("width %d: footer = %q, want %q", test.width, got, test.want)
		}
	}
}
