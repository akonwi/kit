package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestReadyShellIsViewportNativeAndPreservesChromeOwnership(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase:         phaseReady,
		Status:        "esc abort · ctrl+c detach",
		TurnActivity:  "Working…",
		Location:      "~/Developer/agent/kit-v2 (kit-v2)",
		ContextTokens: 112,
		ContextWindow: 200,
		Session: protocol.SessionInfo{
			ID:            "session_1",
			Name:          "Auth refresh race",
			Model:         "openai-codex/gpt-5.6-sol",
			ThinkingLevel: "medium",
		},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)

	if !strings.Contains(rows[0], "Auth refresh race") {
		t.Fatalf("header left = %q, want session name", rows[0])
	}
	if !strings.Contains(rows[0], "GPT 5.6 Sol · thinking: medium · 56%") {
		t.Fatalf("header right = %q, want model and context information", rows[0])
	}
	if strings.Contains(strings.Join(rows, "\n"), "┌") || strings.Contains(strings.Join(rows, "\n"), "┐") {
		t.Fatalf("shell unexpectedly drew an outer frame:\n%s", strings.Join(rows, "\n"))
	}
	if strings.TrimSpace(rows[1]) != strings.Repeat("─", width) {
		t.Fatalf("header separator = %q, want full-width structural rule", rows[1])
	}
	if got := strings.TrimSpace(rows[height-5]); got != "⠋ Working…" {
		t.Fatalf("turn slot = %q, want running activity", got)
	}
	if !strings.Contains(rows[height-1], "esc abort · ctrl+c detach") {
		t.Fatalf("footer left = %q, want run guidance", rows[height-1])
	}
	if !strings.HasSuffix(strings.TrimSpace(rows[height-1]), "~/Developer/agent/kit-v2 (kit-v2)") {
		t.Fatalf("footer right = %q, want cwd and git", rows[height-1])
	}
}

func TestTurnActivityUsesFixedSlotWhileResponseStreamsInTranscript(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, Kind: protocol.SessionEventRunStarted},
		{Sequence: 2, Kind: protocol.SessionEventUserMessage, Text: "Inspect the file"},
		{Sequence: 3, MessageID: "message_test", Kind: protocol.SessionEventAssistantStarted},
		{Sequence: 4, MessageID: "message_test", Kind: protocol.SessionEventThinkingDelta, ContentIndex: 0, Delta: "Planning the inspection\nChecking retries"},
	})
	if state.turnActivity != "Checking retries" {
		t.Fatalf("thinking activity = %q, want latest line", state.turnActivity)
	}

	const width, height = 80, 20
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, TurnActivity: state.turnActivity,
		Messages: append([]transcriptMessage(nil), state.liveMessages...), Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if got := strings.TrimSpace(rows[height-5]); got != "⠋ Checking retries" {
		t.Fatalf("thinking slot = %q, want spinner and latest thinking", got)
	}

	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 5, MessageID: "message_test", Kind: protocol.SessionEventAssistantTextDelta, ContentIndex: 1, Delta: "I’ll inspect it now."},
	})
	if state.turnActivity != "Working…" {
		t.Fatalf("response activity = %q, want Working…", state.turnActivity)
	}
	app = uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, TurnActivity: state.turnActivity,
		Messages: append([]transcriptMessage(nil), state.liveMessages...), Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	if got := strings.TrimSpace(rows[height-5]); got != "⠋ Working…" {
		t.Fatalf("response slot = %q, want turn spinner", got)
	}
	if text := strings.Join(rows, "\n"); !strings.Contains(text, "I’ll inspect it now.") {
		t.Fatalf("streaming response missing from transcript:\n%s", text)
	}

	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 6, MessageID: "message_test", Kind: protocol.SessionEventAssistantCompleted},
		{Sequence: 7, Kind: protocol.SessionEventToolStarted, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
		{Sequence: 8, Kind: protocol.SessionEventToolCompleted, ToolCallID: "call_1", ToolName: "read", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "file contents"}}},
		{Sequence: 9, Kind: protocol.SessionEventRunFinished},
	})
	tool := state.liveMessages[2]
	if tool.ToolName != "read" || tool.ToolArguments != `{"path":"README.md"}` || tool.ToolStatus != "Completed" || tool.Text != "file contents" || tool.Pending {
		t.Fatalf("live tool = %+v", tool)
	}
	app = uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, TurnActivity: state.turnActivity,
		Messages: append([]transcriptMessage(nil), state.liveMessages...), Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	if got := strings.TrimSpace(rows[height-5]); got != "" {
		t.Fatalf("completed turn slot = %q, want reserved blank row", got)
	}
	text := strings.Join(rows, "\n")
	for _, expected := range []string{"I’ll inspect it now.", "› 1 tool call read"} {
		if !strings.Contains(text, expected) {
			t.Errorf("completed activity missing %q:\n%s", expected, text)
		}
	}
}

func TestComposerSpansFullWidthBelowReservedTurnSlot(t *testing.T) {
	t.Parallel()

	const width, height = 40, 12
	composer := strings.Repeat("x", width-2)
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Composer: composer, Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if got := strings.TrimSpace(rows[height-5]); got != "" {
		t.Fatalf("idle turn slot = %q, want reserved blank row", got)
	}
	composerCells := []rune(rows[height-3])
	if composerCells[0] != ' ' || composerCells[1] != 'x' || composerCells[width-2] != 'x' || composerCells[width-1] != ' ' {
		t.Fatalf("full-width composer row = %q", rows[height-3])
	}

	app = uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Composer: composer, TurnActivity: "Working…", Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	if got := strings.TrimSpace(rows[height-5]); got != "⠋ Working…" {
		t.Fatalf("running turn slot = %q, want spinner state", got)
	}
	composerCells = []rune(rows[height-3])
	if composerCells[1] != 'x' || composerCells[width-2] != 'x' {
		t.Fatalf("running full-width composer row = %q", rows[height-3])
	}
}

func TestComposerKeepsFocusAndFillsWidthAcrossActivityAndResize(t *testing.T) {
	t.Parallel()

	state := &shellHarnessState{}
	app := uitest.New(shellHarness{State: state})
	app.Pump(40, 12)
	app.Pump(40, 12)
	app.Key("a")
	app.Pump(40, 12)
	if state.composer != "a" {
		t.Fatalf("composer after first key = %q", state.composer)
	}

	state.SetState(func() { state.activity = "Working…" })
	app.Pump(40, 12)
	app.Key("b")
	app.Pump(40, 12)
	if state.composer != "ab" {
		t.Fatalf("composer after activity change = %q, want retained focus and text", state.composer)
	}

	state.SetState(func() { state.composer = strings.Repeat("x", 80) })
	app.Pump(20, 12)
	narrow := []rune(paintedRows(app, 20, 12)[5])
	if narrow[1] != 'x' || narrow[18] != 'x' {
		t.Fatalf("narrow composer first row = %q", string(narrow))
	}
	app.Pump(50, 12)
	wide := []rune(paintedRows(app, 50, 12)[8])
	if wide[1] != 'x' || wide[48] != 'x' {
		t.Fatalf("wide composer first row = %q", string(wide))
	}
}

func TestComposerGrowsWithNewlinesAndEnterSubmits(t *testing.T) {
	t.Parallel()

	const width, height = 40, 12
	state := &shellHarnessState{}
	app := uitest.New(shellHarness{State: state})
	app.Pump(width, height)
	if got := strings.TrimSpace(paintedRows(app, width, height)[height-3]); got != "Ask kit to do something…" {
		t.Fatalf("initial composer = %q, want one-line placeholder", got)
	}

	app.Key("a")
	app.Pump(width, height)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter, Modifiers: vaxis.ModShift})
	app.Pump(width, height)
	app.Key("b")
	app.Pump(width, height)
	if state.composer != "a\nb" {
		t.Fatalf("multiline composer = %q, want a\\nb", state.composer)
	}
	rows := paintedRows(app, width, height)
	if strings.TrimSpace(rows[height-4]) != "a" || strings.TrimSpace(rows[height-3]) != "b" {
		t.Fatalf("grown composer rows = %q / %q", rows[height-4], rows[height-3])
	}

	app.Enter()
	app.Pump(width, height)
	if len(state.submitted) != 1 || state.submitted[0] != "a\nb" {
		t.Fatalf("submitted prompts = %#v, want multiline prompt", state.submitted)
	}
	if state.composer != "" {
		t.Fatalf("composer after submit = %q, want cleared", state.composer)
	}
	if got := strings.TrimSpace(paintedRows(app, width, height)[height-3]); got != "Ask kit to do something…" {
		t.Fatalf("collapsed composer = %q, want one-line placeholder", got)
	}
}

func TestComposerGrowthKeepsChromeVisibleInShortViewport(t *testing.T) {
	t.Parallel()

	const width, height = 30, 10
	lines := make([]string, 20)
	for index := range lines {
		lines[index] = fmt.Sprintf("line %02d", index+1)
	}
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Composer: strings.Join(lines, "\n"),
		Status: "composer active", Location: "~/kit-v2", Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if !strings.Contains(rows[height-1], "composer…") || !strings.Contains(rows[height-1], "~/kit-v2") {
		t.Fatalf("footer moved outside viewport: %q", rows[height-1])
	}
	if strings.TrimSpace(rows[height-6]) != strings.Repeat("─", width) {
		t.Fatalf("composer separator = %q", rows[height-6])
	}
	for offset, expected := range []string{"line 01", "line 02", "line 03"} {
		if got := strings.TrimSpace(rows[height-5+offset]); got != expected {
			t.Fatalf("composer row %d = %q, want %q", offset, got, expected)
		}
	}
}

func TestComposerRemainsVisibleAtMinimumShellHeight(t *testing.T) {
	t.Parallel()

	const width, height = 20, 5
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Composer: "still visible", TurnActivity: "Working…",
		Status: "active", Location: "~/kit", Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if got := strings.TrimSpace(rows[2]); got != "still visible" {
		t.Fatalf("minimum-height composer = %q", got)
	}
	if !strings.Contains(rows[height-1], "active") || !strings.Contains(rows[height-1], "~/kit") {
		t.Fatalf("minimum-height footer = %q", rows[height-1])
	}
}

func TestComposerPlaceholderPreservesNarrowHorizontalInsets(t *testing.T) {
	t.Parallel()

	const width, height = 12, 10
	app := uitest.New(shellView{Snapshot: shellSnapshot{Phase: phaseReady, Scroll: &ui.ScrollController{}}})
	app.Pump(width, height)
	cells := []rune(paintedRows(app, width, height)[height-3])
	if cells[0] != ' ' || cells[1] != 'A' || cells[width-2] != '…' || cells[width-1] != ' ' {
		t.Fatalf("narrow placeholder row = %q", string(cells))
	}
}

func TestComposerResyncsWhenParentInterceptsSlash(t *testing.T) {
	t.Parallel()

	state := &interceptedComposerHarnessState{}
	app := uitest.New(interceptedComposerHarness{State: state})
	app.Pump(20, 4)
	app.Key("/")
	app.Pump(20, 4)
	app.Key("x")
	app.Pump(20, 4)
	if state.value != "x" {
		t.Fatalf("composer value after intercepted slash = %q, want x", state.value)
	}
}

func TestComposerPlaceholderPassesClicksToTextArea(t *testing.T) {
	t.Parallel()

	state := &composerClickHarnessState{}
	app := uitest.New(composerClickHarness{State: state})
	app.Pump(20, 4)
	app.ShiftTab()
	app.Click(2, 1)
	app.Key("x")
	app.Pump(20, 4)
	if state.value != "x" {
		t.Fatalf("composer value after placeholder click = %q, want x", state.value)
	}
}

type interceptedComposerHarness struct {
	State *interceptedComposerHarnessState
}

func (w interceptedComposerHarness) CreateState() ui.State { return w.State }

type interceptedComposerHarnessState struct {
	ui.StateBase
	value string
}

func (s *interceptedComposerHarnessState) Build(ui.BuildContext) ui.Widget {
	return messageComposer{
		Value: s.value,
		OnChanged: func(_ ui.EventContext, value string) {
			if value == "/" {
				s.SetState(func() {})
				return
			}
			s.SetState(func() { s.value = value })
		},
	}
}

type composerClickHarness struct {
	State *composerClickHarnessState
}

func (w composerClickHarness) CreateState() ui.State { return w.State }

type composerClickHarnessState struct {
	ui.StateBase
	value string
}

func (s *composerClickHarnessState) Build(ui.BuildContext) ui.Widget {
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Button{Label: "other"},
		messageComposer{
			Value:       s.value,
			Placeholder: "Ask kit to do something…",
			OnChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.value = value })
			},
		},
	}}
}

type shellHarness struct {
	State *shellHarnessState
}

func (w shellHarness) CreateState() ui.State { return w.State }

type shellHarnessState struct {
	ui.StateBase
	composer  string
	activity  string
	submitted []string
}

func (s *shellHarnessState) Build(ui.BuildContext) ui.Widget {
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Composer: s.composer, TurnActivity: s.activity,
			Scroll: &ui.ScrollController{},
		},
		Callbacks: shellCallbacks{
			ComposerChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer = value })
			},
			Submit: func(_ ui.EventContext, value string) {
				s.SetState(func() {
					s.submitted = append(s.submitted, value)
					s.composer = ""
				})
			},
		},
	}
}

type activityHarness struct{ State *activityHarnessState }

func (w activityHarness) CreateState() ui.State { return w.State }

type activityHarnessState struct {
	ui.StateBase
	messages        []transcriptMessage
	sourceID        string
	selected        bool
	composer        string
	turnActivity    string
	location        string
	transcript      ui.ScrollController
	activity        ui.ScrollController
	activityFocus   ui.FocusNode
	workspaceLayout workspaceLayoutState
}

func (s *activityHarnessState) Build(ui.BuildContext) ui.Widget {
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Messages: s.messages, Composer: s.composer, TurnActivity: s.turnActivity,
			Location: s.location, Scroll: &s.transcript, ActivityScroll: &s.activity, ActivityFocus: &s.activityFocus,
			WorkspaceLayout: &s.workspaceLayout, ActivitySourceID: s.sourceID, ActivitySelected: s.selected,
		},
		Callbacks: shellCallbacks{
			OpenActivity: func(_ ui.EventContext, sourceID string) {
				s.SetState(func() { s.sourceID, s.selected = sourceID, !s.workspaceLayout.Wide })
			},
			ShowTranscript: func(ui.EventContext) {
				s.SetState(func() { s.selected = false })
			},
			ShowActivity: func(ui.EventContext) {
				s.SetState(func() { s.selected = !s.workspaceLayout.Wide })
			},
			CloseActivity: func(ctx ui.EventContext) {
				if s.activityFocus.HasFocus() {
					ctx.FocusNext()
				}
				s.SetState(func() { s.sourceID, s.selected = "", false })
			},
			ComposerChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer = value })
			},
		},
	}
}

func activityHarnessMessages() []transcriptMessage {
	return []transcriptMessage{
		{ID: "user_1", TurnID: "turn_1", Role: "user", Text: "Inspect README"},
		{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Text: "I’ll inspect it.", ToolCalls: []transcriptToolCall{{
			ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`),
		}}},
		{ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read",
			ToolStatus: "Completed", Text: "README contents", ToolContent: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "README contents"}}},
		{ID: "assistant_2", TurnID: "turn_1", Role: "assistant", Text: "Done."},
	}
}

func findPaintedRow(rows []string, value string) int {
	for index, row := range rows {
		if strings.Contains(row, value) {
			return index
		}
	}
	return -1
}

func TestWideActivityDividerSpansThinkingAndComposerRows(t *testing.T) {
	t.Parallel()

	const width, height = 140, 24
	state := &activityHarnessState{
		messages: activityHarnessMessages(), sourceID: "turn-work:turn_1:call_1", selected: true,
		turnActivity: "Thinking…",
	}
	app := uitest.New(activityHarness{State: state})
	app.Pump(width, height)
	state.SetState(func() { state.composer = "draft" })
	app.Pump(width, height)
	rows := paintedRows(app, width, height)

	const dividerColumn = 83
	for row := 2; row <= height-3; row++ {
		want := "│"
		switch row {
		case 3:
			want = glyphTeeRight
		case height - 4:
			want = glyphCrossJunction
		}
		if got := app.Cell(dividerColumn, row).Character.Grapheme; got != want {
			t.Fatalf("workspace divider at row %d = %q, want %q:\n%s", row, got, want, strings.Join(rows, "\n"))
		}
	}
	if !strings.Contains(rows[height-5], "⠋ Thinking…") {
		t.Fatalf("thinking row is not scoped to the primary column: %q", rows[height-5])
	}
	if !strings.Contains(rows[height-3], "draft") || !strings.Contains(rows[height-3], "page up/down scroll") {
		t.Fatalf("wide bottom row does not place composer beside Activity footer: %q", rows[height-3])
	}
}

func TestWideActivityClipsPaneChromeInShortViewport(t *testing.T) {
	t.Parallel()

	const width, height = 140, 5
	state := &activityHarnessState{
		messages: activityHarnessMessages(), sourceID: "turn-work:turn_1:call_1", location: "~/kit-v2",
	}
	app := uitest.New(activityHarness{State: state})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if got := app.Cell(83, 2).Character.Grapheme; got != "│" {
		t.Fatalf("short workspace divider = %q, want clipped vertical divider", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(rows[height-1]), "~/kit-v2") {
		t.Fatalf("short shell footer geometry changed: %q", rows[height-1])
	}
}

func TestWideActivityJunctionsTrackGrowingComposer(t *testing.T) {
	t.Parallel()

	const width, height = 140, 24
	state := &activityHarnessState{
		messages: activityHarnessMessages(), sourceID: "turn-work:turn_1:call_1",
		composer: "a\nb\nc",
	}
	app := uitest.New(activityHarness{State: state})
	app.Pump(width, height)

	const dividerColumn = 83
	for row, want := range map[int]string{
		3:          glyphTeeRight,
		height - 6: glyphTeeLeft,
		height - 4: glyphTeeRight,
	} {
		if got := app.Cell(dividerColumn, row).Character.Grapheme; got != want {
			t.Fatalf("workspace junction at row %d = %q, want %q", row, got, want)
		}
	}
}

func TestActivityWorkspaceOpensBesideTranscriptAtWideWidths(t *testing.T) {
	t.Parallel()

	const width, height = 140, 24
	state := &activityHarnessState{messages: activityHarnessMessages()}
	app := uitest.New(activityHarness{State: state})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	chipRow := findPaintedRow(rows, "› 1 tool call read")
	if chipRow < 0 {
		t.Fatalf("tool-work chip missing:\n%s", strings.Join(rows, "\n"))
	}
	app.Click(4, chipRow)
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	if state.sourceID != "turn-work:turn_1:call_1" || state.selected {
		t.Fatalf("wide activity source = %q narrow-selection %v", state.sourceID, state.selected)
	}
	if findPaintedRow(rows, "Inspect README") < 0 || !strings.Contains(rows[2], "1 tool call · 1 step") {
		t.Fatalf("wide workspace did not retain transcript beside Activity metadata:\n%s", strings.Join(rows, "\n"))
	}
	if app.Cell(83, 2).Character.Grapheme != "│" {
		t.Fatalf("wide workspace separator = %q, want vertical rule at column 83", app.Cell(83, 2).Character.Grapheme)
	}
	if findPaintedRow(rows, "1 tool call · 1 step") < 0 || findPaintedRow(rows, "read  README.md") < 0 ||
		findPaintedRow(rows, "README contents") < 0 {
		t.Fatalf("activity metadata, row, or output missing:\n%s", strings.Join(rows, "\n"))
	}
	app.Click(width-2, 2)
	app.Pump(width, height)
	if state.sourceID != "" || state.selected {
		t.Fatalf("wide Activity close control left source %q selected %v", state.sourceID, state.selected)
	}
}

func TestOpeningAnotherChipReplacesTheSingletonActivitySource(t *testing.T) {
	t.Parallel()

	const width, height = 140, 28
	messages := append(activityHarnessMessages(),
		transcriptMessage{ID: "user_2", TurnID: "turn_2", Role: "user", Text: "Write notes"},
		transcriptMessage{ID: "assistant_3", TurnID: "turn_2", Role: "assistant", ToolCalls: []transcriptToolCall{{
			ID: "call_2", Name: "write", Arguments: json.RawMessage(`{"path":"notes.txt"}`),
		}}},
		transcriptMessage{ID: "result_2", TurnID: "turn_2", Role: "tool", ToolCallID: "call_2", ToolName: "write", ToolStatus: "Completed"},
	)
	state := &activityHarnessState{messages: messages}
	app := uitest.New(activityHarness{State: state})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	readRow := findPaintedRow(rows, "1 tool call read")
	app.Click(4, readRow)
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	writeRow := findPaintedRow(rows, "1 tool call write")
	app.Click(4, writeRow)
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	if state.sourceID != "turn-work:turn_2:call_2" {
		t.Fatalf("replacement activity source = %q", state.sourceID)
	}
	if findPaintedRow(rows, "write  notes.txt") < 0 {
		t.Fatalf("replacement Activity content missing:\n%s", strings.Join(rows, "\n"))
	}
	activityHeaders := 0
	for _, row := range rows {
		activityHeaders += strings.Count(row, "1 tool call · 1 step")
	}
	if activityHeaders != 1 {
		t.Fatalf("Activity metadata header count = %d, want singleton", activityHeaders)
	}
}

func TestWorkspaceTabWidthUsesTerminalCellsAndClamps(t *testing.T) {
	t.Parallel()

	if got := workspaceTabWidth("A", false); got != workspaceTabMinWidth {
		t.Fatalf("minimum tab width = %d, want %d", got, workspaceTabMinWidth)
	}
	if got := workspaceTabWidth(strings.Repeat("x", 40), true); got != workspaceTabMaxWidth {
		t.Fatalf("maximum tab width = %d, want %d", got, workspaceTabMaxWidth)
	}
	if got := workspaceTextWidth("界a"); got != 3 {
		t.Fatalf("wide-character text width = %d, want 3", got)
	}
	if got := truncateWorkspaceTabLabel("界界a", 4); got != "界…" {
		t.Fatalf("cell-aware truncated label = %q, want %q", got, "界…")
	}
}

func TestNarrowWorkspaceTabsMatchCanonicalStrip(t *testing.T) {
	t.Parallel()

	const width, height = 100, 22
	state := &activityHarnessState{
		messages: activityHarnessMessages(), sourceID: "turn-work:turn_1:call_1", selected: false,
	}
	app := uitest.New(activityHarness{State: state})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if got, want := strings.TrimRight(rows[2], " "), " Transcript  Activity  ×"; got != want {
		t.Fatalf("narrow tab row = %q, want %q", got, want)
	}
	if got := strings.TrimSpace(rows[3]); got != strings.Repeat("─", width) {
		t.Fatalf("narrow tab separator = %q, want full-width rule", got)
	}
	selected := app.Cell(1, 2).Style
	unselected := app.Cell(13, 2).Style
	strip := app.Cell(width-1, 2).Style
	if selected.Foreground == unselected.Foreground || selected.Background != strip.Background || unselected.Background != strip.Background || selected.Attribute != strip.Attribute {
		t.Fatalf("tab styles = selected %+v unselected %+v strip %+v", selected, unselected, strip)
	}
	app.Send(vaxis.Mouse{Col: 13, Row: 2, EventType: vaxis.EventMotion})
	app.Pump(width, height)
	hoveredBackground := app.Cell(13, 2).Style.Background
	app.Send(vaxis.Mouse{Col: 50, Row: 4, EventType: vaxis.EventMotion})
	app.Pump(width, height)
	if hoveredBackground == strip.Background || app.Cell(13, 2).Style.Background != strip.Background {
		t.Fatalf("tab hover backgrounds = hovered %v restored %v base %v", hoveredBackground, app.Cell(13, 2).Style.Background, strip.Background)
	}
	app.Click(13, 2)
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	if strings.Count(strings.Join(rows, "\n"), glyphTimes) != 1 {
		t.Fatalf("narrow Activity rendered duplicate close controls:\n%s", strings.Join(rows, "\n"))
	}
	app.Click(22, 2)
	app.Pump(width, height)
	if state.sourceID != "" || state.selected {
		t.Fatalf("Activity tab close left source %q selected %v", state.sourceID, state.selected)
	}
}

func TestActivityWorkspaceUsesLabeledTabsAtNarrowWidths(t *testing.T) {
	t.Parallel()

	const width, height = 100, 22
	state := &activityHarnessState{
		messages: activityHarnessMessages(), sourceID: "turn-work:turn_1:call_1", selected: true,
	}
	app := uitest.New(activityHarness{State: state})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if !strings.Contains(rows[2], "Transcript") || !strings.Contains(rows[2], "Activity") {
		t.Fatalf("narrow workspace tabs = %q", rows[2])
	}
	if findPaintedRow(rows, "1 tool call · 1 step") < 0 || findPaintedRow(rows, "read  README.md") < 0 {
		t.Fatalf("narrow Activity pane missing:\n%s", strings.Join(rows, "\n"))
	}
	app.Click(3, 2)
	app.Pump(width, height)
	if state.selected {
		t.Fatal("Transcript tab did not select the transcript")
	}
	app.Click(14, 2)
	app.Pump(width, height)
	if !state.selected {
		t.Fatal("Activity tab did not restore the retained pane")
	}
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(width, height)
	rows = paintedRows(app, width, height)
	if state.selected || state.sourceID != "" || findPaintedRow(rows, "Inspect README") < 0 {
		t.Fatalf("escape did not close Activity and return to transcript:\n%s", strings.Join(rows, "\n"))
	}
}

func TestWideActivityOpeningPreservesComposerFocus(t *testing.T) {
	t.Parallel()

	const width, height = 140, 22
	state := &activityHarnessState{messages: activityHarnessMessages()}
	app := uitest.New(activityHarness{State: state})
	app.Pump(width, height)
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	chipRow := findPaintedRow(rows, "› 1 tool call read")
	app.Click(4, chipRow)
	app.Pump(width, height)
	app.Key("z")
	app.Pump(width, height)
	if state.composer != "z" || state.selected {
		t.Fatalf("wide Activity open changed composer focus or narrow selection: composer %q selected %v", state.composer, state.selected)
	}
}

func TestActivityChipMouseRoutePreservesFocus(t *testing.T) {
	t.Parallel()

	const width, height = 100, 22
	state := &activityHarnessState{messages: activityHarnessMessages()}
	app := uitest.New(activityHarness{State: state})
	app.Pump(width, height)
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	chipRow := findPaintedRow(rows, "› 1 tool call read")
	app.Click(4, chipRow)
	app.Pump(width, height)
	app.Key("z")
	app.Tab()
	app.Key("y")
	app.Pump(width, height)
	if state.composer != "" {
		t.Fatalf("composer changed while Activity owned focus: %q", state.composer)
	}
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(width, height)
	app.Key("z")
	app.Pump(width, height)
	if state.composer != "z" {
		t.Fatalf("composer focus was not restored after closing Activity: %q", state.composer)
	}
	rows = paintedRows(app, width, height)
	chipRow = findPaintedRow(rows, "› 1 tool call read")
	app.Click(4, chipRow)
	app.Pump(width, height)
	if state.sourceID != "turn-work:turn_1:call_1" || !state.selected {
		t.Fatalf("reopened Activity source = %q selected %v", state.sourceID, state.selected)
	}
	app.Click(5, height-3)
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	app.Pump(width, height)
	app.Key("q")
	app.Pump(width, height)
	if state.composer != "zq" {
		t.Fatalf("closing Activity advanced focus away from composer: %q", state.composer)
	}
}

func TestActivityOutputPreviewBoundsLinesAndLongRows(t *testing.T) {
	t.Parallel()

	lines := make([]string, 20)
	for index := range lines {
		lines[index] = fmt.Sprintf("line %02d", index+1)
	}
	preview, count, truncated := activityOutputPreview(strings.Join(lines, "\n"))
	if count != 20 || !truncated || strings.Count(preview, "\n") != 13 || !strings.Contains(preview, "line 14") {
		t.Fatalf("multiline preview = count %d truncated %v text %q", count, truncated, preview)
	}
	preview, count, truncated = activityOutputPreview(strings.Repeat("x", 300))
	if count != 1 || !truncated || len([]rune(preview)) != 240 || !strings.HasSuffix(preview, "…") {
		t.Fatalf("long-line preview = count %d truncated %v runes %d", count, truncated, len([]rune(preview)))
	}
}

func TestActivityShowsAbortedMissingResultsAndBoundedOutput(t *testing.T) {
	t.Parallel()

	lines := make([]string, 20)
	for index := range lines {
		lines[index] = fmt.Sprintf("line %02d", index+1)
	}
	messages := activityHarnessMessages()
	messages[1].Aborted = true
	messages[1].ToolCalls = append(messages[1].ToolCalls, transcriptToolCall{
		ID: "call_2", Name: "write", Arguments: json.RawMessage(`{"path":"notes.txt"}`),
	})
	messages[2].Text = strings.Join(lines, "\n")
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: messages, ActivitySourceID: "turn-work:turn_1:call_1", ActivitySelected: true,
		Scroll: &ui.ScrollController{}, ActivityScroll: &ui.ScrollController{},
	}})
	app.Pump(140, 40)
	rows := paintedRows(app, 140, 40)
	if findPaintedRow(rows, "20 lines") < 0 || findPaintedRow(rows, "line 14") < 0 {
		t.Fatalf("bounded tool output well or line metadata missing:\n%s", strings.Join(rows, "\n"))
	}
	if findPaintedRow(rows, "⊘ write  notes.txt") < 0 {
		t.Fatalf("aborted missing result glyph missing:\n%s", strings.Join(rows, "\n"))
	}
}

func TestRunAbortTakesPrecedenceOverClosingActivity(t *testing.T) {
	t.Parallel()

	closed, dismissed := 0, 0
	view := func(running bool) shellView {
		return shellView{
			Snapshot: shellSnapshot{
				Phase: phaseReady, Running: running, Messages: activityHarnessMessages(),
				ActivitySourceID: "turn-work:turn_1:call_1", ActivitySelected: true,
				Scroll: &ui.ScrollController{}, ActivityScroll: &ui.ScrollController{},
			},
			Callbacks: shellCallbacks{
				CloseActivity: func(ui.EventContext) { closed++ },
				Dismiss:       func(ui.EventContext) { dismissed++ },
			},
		}
	}
	runningApp := uitest.New(view(true))
	runningApp.Pump(140, 20)
	if rows := paintedRows(runningApp, 140, 20); findPaintedRow(rows, "page up/down scroll · esc abort") < 0 {
		t.Fatalf("running Activity hint did not advertise abort:\n%s", strings.Join(rows, "\n"))
	}
	runningApp.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if dismissed != 1 || closed != 0 {
		t.Fatalf("running escape = dismissed %d closed %d", dismissed, closed)
	}
	idleApp := uitest.New(view(false))
	idleApp.Pump(140, 20)
	idleApp.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if dismissed != 1 || closed != 1 {
		t.Fatalf("idle escape = dismissed %d closed %d", dismissed, closed)
	}
	paletteView := view(false)
	paletteView.Snapshot.PaletteOpen = true
	paletteApp := uitest.New(paletteView)
	paletteApp.Pump(140, 20)
	paletteApp.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if dismissed != 2 || closed != 1 {
		t.Fatalf("palette escape = dismissed %d closed %d", dismissed, closed)
	}
}

func TestActivityUsesGlyphOnlyForPlannedTools(t *testing.T) {
	t.Parallel()

	messages := activityHarnessMessages()
	messages[2].Pending = true
	messages[2].ToolStatus = "Planned"
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: messages,
		ActivitySourceID: "turn-work:turn_1:call_1", ActivitySelected: true,
		Scroll: &ui.ScrollController{}, ActivityScroll: &ui.ScrollController{},
	}})
	app.Pump(140, 20)
	rows := paintedRows(app, 140, 20)
	if findPaintedRow(rows, "› read  README.md") < 0 {
		t.Fatalf("planned tool glyph row missing:\n%s", strings.Join(rows, "\n"))
	}
}

func TestActivityPageKeysUseKeyboardScrollRoute(t *testing.T) {
	t.Parallel()

	pages := 0
	app := uitest.New(shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Messages: activityHarnessMessages(),
			ActivitySourceID: "turn-work:turn_1:call_1", ActivitySelected: true,
			Scroll: &ui.ScrollController{}, ActivityScroll: &ui.ScrollController{},
		},
		Callbacks: shellCallbacks{ScrollActivity: func(_ ui.EventContext, delta int) { pages += delta }},
	})
	app.Pump(100, 20)
	app.Send(vaxis.Key{Keycode: vaxis.KeyPgDown})
	app.Send(vaxis.Key{Keycode: vaxis.KeyPgUp})
	app.Send(vaxis.Key{Keycode: vaxis.KeyPgDown})
	if pages != 1 {
		t.Fatalf("activity page delta = %d, want 1", pages)
	}
}

func TestTurnWorkChipShowsSpinnerEightNamesAndOverflow(t *testing.T) {
	t.Parallel()

	calls := make([]transcriptToolCall, 10)
	for index := range calls {
		calls[index] = transcriptToolCall{
			ID: fmt.Sprintf("call_%d", index+1), Name: fmt.Sprintf("tool%d", index+1), Arguments: json.RawMessage(`{}`),
		}
	}
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady,
		Messages: []transcriptMessage{{
			ID: "assistant_1", TurnID: "turn_1", Role: "assistant", ToolCalls: calls,
		}},
		Scroll: &ui.ScrollController{},
	}})
	app.Pump(160, 16)
	rows := paintedRows(app, 160, 16)
	row := findPaintedRow(rows, "10 tool calls")
	if row < 0 {
		t.Fatalf("running chip missing:\n%s", strings.Join(rows, "\n"))
	}
	for _, expected := range []string{"⠋ 10 tool calls", "tool1 · tool2 · tool3 · tool4 · tool5 · tool6 · tool7 · tool8 · +2 more"} {
		if !strings.Contains(rows[row], expected) {
			t.Errorf("chip row %q missing %q", rows[row], expected)
		}
	}
}

func TestWorkspaceSwitchesAt125Columns(t *testing.T) {
	t.Parallel()

	state := &activityHarnessState{
		messages: activityHarnessMessages(), sourceID: "turn-work:turn_1:call_1", selected: true,
	}
	app := uitest.New(activityHarness{State: state})
	app.Pump(125, 20)
	wideRows := paintedRows(app, 125, 20)
	if strings.Contains(wideRows[2], "Transcript") || app.Cell(71, 2).Character.Grapheme != "│" {
		t.Fatalf("125-column workspace did not use split layout:\n%s", strings.Join(wideRows, "\n"))
	}
	app.Pump(124, 20)
	narrowRows := paintedRows(app, 124, 20)
	if !strings.Contains(narrowRows[2], "Transcript") || !strings.Contains(narrowRows[2], "Activity") {
		t.Fatalf("124-column workspace did not use tabs: %q", narrowRows[2])
	}
}

func TestReadyShellAtNarrowWidthKeepsModelAndLocationOnRight(t *testing.T) {
	t.Parallel()

	const width, height = 46, 20
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase:    phaseReady,
		Location: "~/Developer/agent/kit-v2 (kit-v2)",
		Session: protocol.SessionInfo{
			ID:            "session_1",
			Name:          "A deliberately long session name",
			Model:         "openai-codex/gpt-5.6-sol",
			ThinkingLevel: "medium",
		},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)

	if !strings.Contains(rows[0], "GPT 5.6 Sol") {
		t.Fatalf("narrow header = %q, want visible model", rows[0])
	}
	if !strings.Contains(rows[height-1], "kit-v2") {
		t.Fatalf("narrow footer = %q, want visible location", rows[height-1])
	}
	for index, row := range rows {
		if strings.ContainsAny(row, "┌┐└┘") {
			t.Fatalf("row %d unexpectedly contains outer-frame corners: %q", index, row)
		}
	}
}

func TestAuthGateUsesShellFooterAndDeviceDialog(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase:    phaseAuthGate,
		Location: "~/Developer/agent/kit-v2 (kit-v2)",
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	text := strings.Join(rows, "\n")
	if !strings.Contains(text, "Connect an AI provider to get started.") {
		t.Fatalf("auth gate missing instruction:\n%s", text)
	}
	actionRendered := false
	for _, row := range rows {
		if strings.TrimSpace(row) == "Connect a provider" {
			actionRendered = true
			break
		}
	}
	if !actionRendered {
		t.Fatalf("auth gate action label is not rendered exactly as expected:\n%s", text)
	}
	if !strings.Contains(rows[height-1], "enter connect") || !strings.Contains(rows[height-1], "kit-v2") {
		t.Fatalf("auth footer = %q, want action left and location right", rows[height-1])
	}

	app = uitest.New(shellView{Snapshot: shellSnapshot{
		Phase:    phaseAuthWaiting,
		Location: "~/Developer/agent/kit-v2 (kit-v2)",
		Instructions: auth.OpenAICodexDeviceInstructions{
			VerificationURI: "https://example.test/device",
			UserCode:        "ABCD-EFGH",
			ExpiresAt:       time.Now().Add(10 * time.Minute),
		},
		Remaining: 10 * time.Minute,
	}})
	app.Pump(width, height)
	text = strings.Join(paintedRows(app, width, height), "\n")
	if !strings.Contains(text, "ABCD-EFGH") || !strings.Contains(text, "expires in 10:00") {
		t.Fatalf("device dialog missing instructions:\n%s", text)
	}
}

func TestCodexDeviceURLIsAPlainClickableLink(t *testing.T) {
	t.Parallel()

	const (
		width  = 100
		height = 24
		link   = "https://auth.openai.com/codex/device"
	)
	opened := ""
	app := uitest.New(shellView{
		Snapshot: shellSnapshot{
			Phase: phaseAuthWaiting,
			Instructions: auth.OpenAICodexDeviceInstructions{
				VerificationURI: link, UserCode: "TAN4-TMNGX", ExpiresAt: time.Now().Add(10 * time.Minute),
			},
			Remaining: 10 * time.Minute,
		},
		Callbacks: shellCallbacks{OpenURL: func(_ ui.EventContext, raw string) { opened = raw }},
	})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{"Open this URL", link, "Enter this code", "TAN4-TMNGX", "⠋ Waiting for approval"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Codex dialog missing %q:\n%s", expected, text)
		}
	}
	if strings.Count(text, "┌") != 1 || strings.Count(text, "└") != 1 {
		t.Fatalf("Codex details add an unnecessary nested border:\n%s", text)
	}
	if strings.Count(text, "├") != 1 || strings.Count(text, "┤") != 1 {
		t.Fatalf("Codex action footer is not a distinct bordered region:\n%s", text)
	}
	for row, line := range rows {
		byteOffset := strings.Index(line, link)
		if byteOffset < 0 {
			continue
		}
		column := len([]rune(line[:byteOffset]))
		for offset := range len(link) {
			if got := app.Cell(column+offset, row).Hyperlink; got != link {
				t.Fatalf("link cell %d (%q) has hyperlink %q, want %q", offset, app.Cell(column+offset, row).Grapheme, got, link)
			}
		}
		app.Click(column, row)
		if opened != link {
			t.Fatalf("click opened %q, want %q", opened, link)
		}
		return
	}
	t.Fatal("linked URL row not found")
}

func TestSafeHTTPSHyperlinkRejectsUnsafeTargets(t *testing.T) {
	t.Parallel()
	if got := safeHTTPSHyperlink("https://example.test/device"); got == "" {
		t.Fatal("safe HTTPS link was rejected")
	}
	for _, raw := range []string{"http://example.test/device", "https://user@example.test/device", "https://example.test/\nunsafe", "not a URL"} {
		if got := safeHTTPSHyperlink(raw); got != "" {
			t.Errorf("safeHTTPSHyperlink(%q) = %q, want empty", raw, got)
		}
	}
}

func TestAuthGateEnterOpensProviderSelection(t *testing.T) {
	t.Parallel()

	opened := false
	app := uitest.New(shellView{
		Snapshot:  shellSnapshot{Phase: phaseAuthGate},
		Callbacks: shellCallbacks{OpenAuth: func(ui.EventContext) { opened = true }},
	})
	app.Pump(46, 20)
	app.Enter()
	if !opened {
		t.Fatal("Enter did not activate the auth gate")
	}
}

func TestProviderDialogMatchesMainBranchStructure(t *testing.T) {
	t.Parallel()

	const width, height = 100, 30
	app := uitest.New(shellView{Snapshot: shellSnapshot{Phase: phaseAuthSelect}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{
		"Connect a provider", "Filter providers", ">",
		"OpenAI Codex", "ChatGPT plan · device code",
		"Anthropic", "API key", "OpenAI",
		"↑↓ move · enter select · esc close",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("provider dialog missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "login option") {
		t.Fatalf("provider dialog includes a noisy option count:\n%s", text)
	}
	for _, row := range rows {
		left := strings.Index(row, "┌")
		right := strings.LastIndex(row, "┐")
		if left < 0 || right < left {
			continue
		}
		if got := len([]rune(row[left : right+len("┐")])); got != 70 {
			t.Fatalf("dialog width = %d, want 70%% of %d", got, width)
		}
		return
	}
	t.Fatal("provider dialog border not found")
}

func TestProviderDialogEnterSelectsFocusedResult(t *testing.T) {
	t.Parallel()

	selected := ""
	app := uitest.New(shellView{
		Snapshot: shellSnapshot{Phase: phaseAuthSelect},
		Callbacks: shellCallbacks{SelectProvider: func(_ ui.EventContext, providerID string) {
			selected = providerID
		}},
	})
	app.Pump(80, 30)
	app.Enter()
	if selected != auth.OpenAICodexProviderID {
		t.Fatalf("selected provider = %q, want %q", selected, auth.OpenAICodexProviderID)
	}
}

func TestProviderDialogArrowKeysMoveSelection(t *testing.T) {
	t.Parallel()

	moved := 0
	app := uitest.New(shellView{
		Snapshot: shellSnapshot{Phase: phaseAuthSelect},
		Callbacks: shellCallbacks{MoveProviderSelection: func(_ ui.EventContext, delta int) {
			moved += delta
		}},
	})
	app.Pump(80, 30)
	app.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	if moved != 1 {
		t.Fatalf("selection delta = %d, want 1", moved)
	}
	app.Send(vaxis.Key{Keycode: vaxis.KeyUp})
	if moved != 0 {
		t.Fatalf("selection delta after Up = %d, want 0", moved)
	}
}

func TestProviderDialogSelectsHighlightedAPIKeyProvider(t *testing.T) {
	t.Parallel()

	selected := ""
	app := uitest.New(shellView{
		Snapshot: shellSnapshot{Phase: phaseAuthSelect, AuthSelection: 1},
		Callbacks: shellCallbacks{SelectProvider: func(_ ui.EventContext, providerID string) {
			selected = providerID
		}},
	})
	app.Pump(80, 30)
	app.Enter()
	if selected != auth.AnthropicProviderID {
		t.Fatalf("selected provider = %q, want %q", selected, auth.AnthropicProviderID)
	}
}

func TestAPIKeyDialogObscuresSecret(t *testing.T) {
	t.Parallel()

	const secret = "secret-api-key"
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseAuthAPIKey, AuthProviderID: auth.OpenAIProviderID, AuthAPIKey: secret,
	}})
	app.Pump(80, 20)
	text := strings.Join(paintedRows(app, 80, 20), "\n")
	if !strings.Contains(text, "Connect OpenAI") || !strings.Contains(text, "API key") {
		t.Fatalf("API-key dialog missing provider context:\n%s", text)
	}
	if strings.Contains(text, secret) {
		t.Fatalf("API-key dialog exposed secret:\n%s", text)
	}
}

func TestAPIKeySaveCannotBeVisuallyCanceledAfterCommitStarts(t *testing.T) {
	t.Parallel()

	dismissed := false
	app := uitest.New(shellView{
		Snapshot: shellSnapshot{
			Phase: phaseAuthAPIKey, AuthProviderID: auth.OpenAIProviderID, AuthPending: true,
		},
		Callbacks: shellCallbacks{Dismiss: func(ui.EventContext) { dismissed = true }},
	})
	app.Pump(80, 20)
	text := strings.Join(paintedRows(app, 80, 20), "\n")
	if !strings.Contains(text, "Saving…") || strings.Contains(text, "esc cancel") || strings.Contains(text, "esc back") {
		t.Fatalf("pending API-key footer offers misleading cancellation:\n%s", text)
	}
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if dismissed {
		t.Fatal("Escape dismissed API-key save after commit started")
	}
}

func TestPaletteLaunchedAuthBlocksConversationInput(t *testing.T) {
	t.Parallel()

	const width, height = 80, 20
	state := &authModalHarnessState{}
	app := uitest.New(authModalHarness{State: state})
	app.Pump(width, height)
	app.Click(2, height-3)
	app.Key("x")
	app.Enter()
	app.Pump(width, height)
	if state.composer != "" || state.submissions != 0 {
		t.Fatalf("background composer=%q submissions=%d", state.composer, state.submissions)
	}
	if state.filter != "x" {
		t.Fatalf("provider filter = %q, want focused overlay input", state.filter)
	}
}

func TestWaitingAuthModalRetainsFocusWithoutInteractiveInstructions(t *testing.T) {
	t.Parallel()

	const width, height = 80, 20
	ready := &authWaitingFocusHarnessState{returnReady: true}
	app := uitest.New(authWaitingFocusHarness{State: ready})
	app.Pump(width, height)
	app.Key("x")
	app.Pump(width, height)
	if ready.composer != "" {
		t.Fatalf("ready background composer = %q", ready.composer)
	}

	firstRun := &authWaitingFocusHarnessState{}
	app = uitest.New(authWaitingFocusHarness{State: firstRun})
	app.Pump(width, height)
	app.Enter()
	if firstRun.openAuth != 0 {
		t.Fatalf("background auth action count = %d", firstRun.openAuth)
	}
}

func TestAuthDialogDoesNotScrimBackground(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	gate := uitest.New(shellView{Snapshot: shellSnapshot{Phase: phaseAuthGate, Location: "~/repo"}})
	gate.Pump(width, height)
	dialog := uitest.New(shellView{Snapshot: shellSnapshot{Phase: phaseAuthSelect, Location: "~/repo"}})
	dialog.Pump(width, height)

	for _, point := range [][2]int{{2, 0}, {2, 4}, {2, height - 1}} {
		column, row := point[0], point[1]
		if got, want := dialog.Cell(column, row).Style, gate.Cell(column, row).Style; got != want {
			t.Fatalf("background style at %d,%d = %+v with dialog, want %+v", column, row, got, want)
		}
	}

	left, top := -1, -1
	for row := 0; row < height && left < 0; row++ {
		for column := 0; column < width; column++ {
			if dialog.Cell(column, row).Grapheme == "┌" {
				left, top = column, row
				break
			}
		}
	}
	if left < 0 {
		t.Fatal("dialog border not found")
	}
	if got, want := dialog.Cell(left+1, top+1).Style.Background, gate.Cell(left+1, top+1).Style.Background; got != want {
		t.Fatalf("dialog interior background = %v, want shell background %v", got, want)
	}
}

type authWaitingFocusHarness struct{ State *authWaitingFocusHarnessState }

func (w authWaitingFocusHarness) CreateState() ui.State { return w.State }

type authWaitingFocusHarnessState struct {
	ui.StateBase
	returnReady bool
	composer    string
	openAuth    int
	scroll      ui.ScrollController
}

func (s *authWaitingFocusHarnessState) Build(ui.BuildContext) ui.Widget {
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseAuthWaiting, AuthReturnReady: s.returnReady,
			Composer: s.composer, Scroll: &s.scroll,
			Session: protocol.SessionInfo{Name: "Attached", Model: "openai/gpt-5.3-codex"},
		},
		Callbacks: shellCallbacks{
			ComposerChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer = value })
			},
			OpenAuth: func(ui.EventContext) {
				s.SetState(func() { s.openAuth++ })
			},
		},
	}
}

type authModalHarness struct{ State *authModalHarnessState }

func (w authModalHarness) CreateState() ui.State { return w.State }

type authModalHarnessState struct {
	ui.StateBase
	composer    string
	filter      string
	submissions int
	scroll      ui.ScrollController
}

func (s *authModalHarnessState) Build(ui.BuildContext) ui.Widget {
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseAuthSelect, AuthReturnReady: true,
			Composer: s.composer, AuthFilter: s.filter, Scroll: &s.scroll,
			Session: protocol.SessionInfo{Name: "Attached", Model: "openai/gpt-5.3-codex"},
		},
		Callbacks: shellCallbacks{
			ComposerChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer = value })
			},
			Submit: func(ui.EventContext, string) {
				s.SetState(func() { s.submissions++ })
			},
			AuthFilterChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.filter = value })
			},
		},
	}
}

func TestAuthDialogsFitNarrowViewport(t *testing.T) {
	t.Parallel()

	const width, height = 46, 20
	for name, snapshot := range map[string]shellSnapshot{
		"provider": {Phase: phaseAuthSelect},
		"device": {
			Phase: phaseAuthWaiting,
			Instructions: auth.OpenAICodexDeviceInstructions{
				VerificationURI: "https://example.test/a/long/device/path",
				UserCode:        "ABCD-EFGH",
				ExpiresAt:       time.Now().Add(10 * time.Minute),
			},
			Remaining: 10 * time.Minute,
		},
	} {
		t.Run(name, func(t *testing.T) {
			app := uitest.New(shellView{Snapshot: snapshot})
			app.Pump(width, height)
			rows := paintedRows(app, width, height)
			text := strings.Join(rows, "\n")
			if !strings.Contains(text, "OpenAI Codex") {
				t.Fatalf("narrow dialog lost title/content:\n%s", text)
			}
			for index, row := range rows {
				if len([]rune(row)) != width {
					t.Fatalf("row %d width = %d, want %d", index, len([]rune(row)), width)
				}
			}
		})
	}
}

func TestCtrlCRequestsQuit(t *testing.T) {
	t.Parallel()

	app := uitest.New(shellView{
		Snapshot:  shellSnapshot{Phase: phaseReady},
		Callbacks: shellCallbacks{Quit: func(ctx ui.EventContext) { ctx.Quit() }},
	})
	app.Pump(46, 20)
	app.Send(vaxis.Key{Text: "c", Keycode: 'c', Modifiers: vaxis.ModCtrl})
	if !app.ShouldQuit() {
		t.Fatal("Ctrl+C did not request quit")
	}
}

func TestContextPercentage(t *testing.T) {
	t.Parallel()

	if _, ok := contextPercentage(0, 272_000); ok {
		t.Fatal("empty context percentage was visible")
	}
	if got, ok := contextPercentage(110_000, 272_000); !ok || got != 40 {
		t.Fatalf("context percentage = %d, %v; want 40, true", got, ok)
	}
	if got, ok := contextPercentage(300_000, 272_000); !ok || got != 100 {
		t.Fatalf("clamped context percentage = %d, %v; want 100, true", got, ok)
	}
}

func TestModelDisplayName(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"openai-codex/gpt-5.6-sol":    "GPT 5.6 Sol",
		"anthropic/claude-sonnet-4-6": "Claude Sonnet 4.6",
		"openai/gpt-5.3-codex":        "GPT 5.3 Codex",
	} {
		if got := modelDisplayName(input); got != want {
			t.Errorf("modelDisplayName(%q) = %q, want %q", input, got, want)
		}
	}
}

func paintedRows(app *uitest.App, width, height int) []string {
	rows := make([]string, height)
	for row := 0; row < height; row++ {
		var line strings.Builder
		for column := 0; column < width; column++ {
			line.WriteString(app.Cell(column, row).Grapheme)
		}
		rows[row] = line.String()
	}
	return rows
}
