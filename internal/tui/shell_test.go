package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestHeaderControlsOwnExactHitRegions(t *testing.T) {
	t.Parallel()

	namePresses, modelPresses, thinkingPresses := 0, 0, 0
	view := shellView{
		Snapshot: shellSnapshot{
			Phase:         phaseReady,
			Session:       protocol.SessionInfo{Name: "Focused work", Model: "test/gpt-test", ThinkingLevel: "high"},
			ContextTokens: 64_000, ContextWindow: 128_000,
		},
		Callbacks: shellCallbacks{
			OpenSessionRename: func(ui.EventContext) { namePresses++ },
			OpenModel:         func(ui.EventContext) { modelPresses++ },
			OpenThinking:      func(ui.EventContext) { thinkingPresses++ },
		},
	}
	application := uitest.New(view)
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	nameColumn, row := findTextCell(t, rows, "Focused work")
	modelColumn, _ := findTextCell(t, rows, "GPT Test")
	thinkingColumn, _ := findTextCell(t, rows, "(high)")
	separatorColumn := modelColumn + len("GPT Test")

	baseBackground := application.Cell(modelColumn, row).Style.Background
	application.Send(vaxis.Mouse{Col: modelColumn, Row: row, EventType: vaxis.EventMotion})
	application.Pump(80, 24)
	hoveredBackground := application.Cell(modelColumn, row).Style.Background
	application.Send(vaxis.Mouse{Col: 1, Row: 3, EventType: vaxis.EventMotion})
	application.Pump(80, 24)
	if application.Cell(modelColumn, row).Style.Background != baseBackground {
		t.Fatal("model control hover did not clear")
	}

	application.Click(nameColumn, row)
	application.Click(nameColumn+len("Focused work")+1, row)
	application.Click(modelColumn, row)
	application.Click(thinkingColumn, row)
	application.Click(separatorColumn, row)
	application.Send(vaxis.Mouse{Col: modelColumn, Row: row, Button: vaxis.MouseRightButton, EventType: vaxis.EventPress})
	application.Pump(80, 24)
	if namePresses != 1 || modelPresses != 1 || thinkingPresses != 1 || hoveredBackground == baseBackground {
		t.Fatalf("header controls = name:%d model:%d thinking:%d base:%v hovered:%v", namePresses, modelPresses, thinkingPresses, baseBackground, hoveredBackground)
	}

	narrow := uitest.New(view)
	narrow.Pump(24, 12)
	narrowText := strings.Join(paintedRows(narrow, 24, 12), "\n")
	if !strings.Contains(narrowText, "GPT Test (high)") {
		t.Fatalf("narrow header did not show the compact model information:\n%s", narrowText)
	}
	veryNarrow := uitest.New(view)
	veryNarrow.Pump(9, 8)
	if text := strings.Join(paintedRows(veryNarrow, 9, 8), "\n"); strings.Contains(text, "GPT Test") || strings.Contains(text, "thinking") {
		t.Fatalf("very narrow header exposed partial controls:\n%s", text)
	}
}

func TestConfigurationPickersShowAuthenticatedCapabilitiesAndSupportedThinking(t *testing.T) {
	t.Parallel()

	catalog := []protocol.ModelCapability{
		{ID: "openai/gpt-large", Name: "GPT Large", Provider: "openai", ContextWindow: 128_000, Available: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingHigh}},
		{ID: "openai/o", Name: "O", Provider: "openai", ContextWindow: 1_000_000, Available: true, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingOff}},
		{ID: "anthropic/claude", Name: "Claude", Provider: "anthropic", ContextWindow: 200_000, Available: false, ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingLow}},
	}
	modelApp := uitest.New(configurationPickerSurface{Snapshot: configurationPickerSnapshot{
		Mode: configurationPickerModel, Models: catalog, CurrentModel: "openai/gpt-large", Selection: "openai/gpt-large",
	}})
	modelApp.Pump(100, 24)
	modelText := modelApp.Text()
	for _, expected := range []string{"Select model", "Search models…", "✓ GPT Large", "openai/gpt-large", "128k context"} {
		if !strings.Contains(modelText, expected) {
			t.Fatalf("model picker missing %q:\n%s", expected, modelText)
		}
	}
	if strings.Contains(modelText, "Claude") || strings.Contains(modelText, "sign in required") {
		t.Fatalf("model picker showed an unauthenticated provider:\n%s", modelText)
	}
	contextColumns := make([]int, 0, 2)
	for _, line := range paintedRows(modelApp, 100, 24) {
		if column := strings.Index(line, "128k context"); column >= 0 {
			contextColumns = append(contextColumns, utf8.RuneCountInString(line[:column]))
		}
		if column := strings.Index(line, "1.0M context"); column >= 0 {
			contextColumns = append(contextColumns, utf8.RuneCountInString(line[:column]))
		}
	}
	if len(contextColumns) != 2 || contextColumns[0] != contextColumns[1] {
		t.Fatalf("model context columns = %v, want one aligned column:\n%s", contextColumns, modelText)
	}
	thinkingTheme := ui.DefaultThemeSet().Light
	thinkingApp := uitest.New(markdownThemedTestSurface(thinkingTheme, configurationPickerSurface{Snapshot: configurationPickerSnapshot{
		Mode: configurationPickerThinking, Models: catalog, CurrentModel: "openai/gpt-large",
		CurrentThinking: "high", Selection: "high",
	}}))
	thinkingApp.Pump(80, 24)
	thinkingText := thinkingApp.Text()
	for _, expected := range []string{"Thinking level", "off", "✓ high"} {
		if !strings.Contains(thinkingText, expected) {
			t.Fatalf("thinking picker missing %q:\n%s", expected, thinkingText)
		}
	}
	if strings.Contains(thinkingText, "low") {
		t.Fatalf("thinking picker offered a level unsupported by the active model:\n%s", thinkingText)
	}
	if strings.Contains(thinkingText, "Reasoning effort") {
		t.Fatalf("thinking picker restored the removed redundant descriptions:\n%s", thinkingText)
	}
	thinkingRows := paintedRows(thinkingApp, 80, 24)
	dialogWidth := 0
	for _, row := range thinkingRows {
		trimmed := strings.TrimSpace(row)
		if strings.HasPrefix(trimmed, "┌") {
			dialogWidth = len([]rune(trimmed))
			break
		}
	}
	if dialogWidth != 48 {
		t.Fatalf("thinking picker width = %d, want compact 48-cell dialog", dialogWidth)
	}
	selectedColumn, selectedRow := findTextCell(t, thinkingRows, "✓ high")
	selectedStyle := thinkingApp.Cell(selectedColumn, selectedRow).Style
	if selectedStyle.Foreground != thinkingTheme.Background || selectedStyle.Background != thinkingTheme.Foreground {
		t.Fatalf("selected thinking label style = %+v, want foreground %v on background %v", selectedStyle, thinkingTheme.Background, thinkingTheme.Foreground)
	}
	longQuery := "anthropic-model-query-with-full-width"
	queryApp := uitest.New(configurationPickerSurface{Snapshot: configurationPickerSnapshot{
		Mode: configurationPickerModel, Models: catalog, Query: longQuery,
	}})
	queryApp.Pump(100, 24)
	queryApp.Pump(100, 24)
	if !strings.Contains(queryApp.Text(), longQuery) {
		t.Fatalf("model query input was shrink-wrapped:\n%s", queryApp.Text())
	}
}

func TestSessionDetailsShowsAuthoritativeCumulativeUsage(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, SessionDetailsOpen: true,
		Session: protocol.SessionInfo{
			ID: "session_1", Name: "Usage audit", Model: "openai-codex/gpt-5.6-sol", ThinkingLevel: "high",
		},
		ContextTokens: 41_000, ContextWindow: 128_000,
		SessionUsage: protocol.SessionUsage{
			Input: 120_000, Output: 8_000, CacheRead: 52_000, CacheWrite: 3_000,
			Reasoning: 2_500, TotalTokens: 128_000,
			Cost: protocol.SessionUsageCost{Total: 1.2345},
		},
	}})
	application.Pump(width, height)
	rows := paintedRows(application, width, height)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{
		"Session details", "Usage audit", "Configuration",
		"Model         openai-codex/gpt-5.6-sol", "Thinking      high",
		"Context       41,000 / 128,000 tokens (32%)", "Cumulative usage",
		"Input         120,000", "Output        8,000", "Cache read    52,000",
		"Cache write   3,000", "Reasoning     2,500", "Total         128,000",
		"Cost          $1.23", "esc close",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("session details missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(rows[0], "120,000") || strings.Contains(rows[0], "$1.23") {
		t.Fatalf("cumulative usage leaked into persistent header: %q", rows[0])
	}
}

func TestSessionUsageLiveUpdateIsAbsoluteAndMonotonic(t *testing.T) {
	t.Parallel()

	state := appState{sessionUsage: protocol.SessionUsage{Input: 10, TotalTokens: 10}}
	updated := protocol.SessionUsage{Input: 20, Output: 5, TotalTokens: 25, Cost: protocol.SessionUsageCost{Total: 0.25}}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 1, Kind: protocol.SessionEventUsageUpdated, Usage: &updated}})
	if state.sessionUsage != updated {
		t.Fatalf("session usage = %+v, want %+v", state.sessionUsage, updated)
	}
	regressed := protocol.SessionUsage{Input: 19, Output: 5, TotalTokens: 24, Cost: protocol.SessionUsageCost{Total: 0.24}}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 2, Kind: protocol.SessionEventUsageUpdated, Usage: &regressed}})
	if state.sessionUsage != updated {
		t.Fatalf("regressive usage update applied: %+v", state.sessionUsage)
	}
}

func TestReadyShellIsViewportNativeAndPreservesChromeOwnership(t *testing.T) {
	t.Parallel()

	const width, height = 80, 24
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase:         phaseReady,
		Status:        "esc abort · ctrl+c detach",
		TurnActivity:  "Working…",
		Location:      "~/Developer/agent/kit-v2 (kit-v2*)",
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
	if !strings.Contains(rows[0], "GPT 5.6 Sol (medium) · 56%") {
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
	if !strings.HasSuffix(strings.TrimSpace(rows[height-1]), "~/Developer/agent/kit-v2 (kit-v2*)") {
		t.Fatalf("footer right = %q, want cwd and git", rows[height-1])
	}
}

func TestAutomaticCompactionUsesVisibleTurnSlot(t *testing.T) {
	t.Parallel()

	state := appState{}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 1, Kind: protocol.SessionEventCompactionStarted}})
	const width, height = 80, 20
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, TurnActivity: state.turnActivity, Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if got := strings.TrimSpace(rows[height-5]); got != "⠋ Compacting session…" {
		t.Fatalf("automatic compaction slot = %q, want visible pending feedback", got)
	}
}

func TestFollowUpsRenderAboveComposerWithRestoreHint(t *testing.T) {
	t.Parallel()

	const width, height = 80, 20
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady,
		FollowUps: protocol.FollowUpQueue{
			Count: 4, Previews: []string{"check the tests", "then update the docs", "publish the result", "notify me"},
		},
		Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if got := strings.TrimSpace(rows[height-8]); got != "" {
		t.Fatalf("follow-up status row = %q, want reserved empty activity row", got)
	}
	if got := strings.TrimSpace(rows[height-7]); got != "Follow-up 1: check the tests" {
		t.Fatalf("first follow-up row = %q", got)
	}
	if got := strings.TrimSpace(rows[height-6]); got != "Follow-up 2: then update the docs" {
		t.Fatalf("second follow-up row = %q", got)
	}
	if got := strings.TrimSpace(rows[height-5]); got != "+2 more follow-ups" {
		t.Fatalf("follow-up overflow row = %q", got)
	}
	if got := strings.TrimSpace(rows[height-1]); got != "4 queued · ↑ restore" {
		t.Fatalf("follow-up footer = %q", got)
	}
}

func TestTurnActivityUsesFixedSlotWhileResponseIsBuffered(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, Kind: protocol.SessionEventRunStarted},
		{Sequence: 2, Kind: protocol.SessionEventUserMessage, Text: "Inspect the file"},
		{Sequence: 3, MessageID: "message_test", Kind: protocol.SessionEventAssistantStarted},
		{Sequence: 4, MessageID: "message_test", Kind: protocol.SessionEventThinkingDelta, ContentIndex: 0, Delta: "Planning the inspection\n**Checking retries**"},
	})
	if state.turnActivity != "**Checking retries**" || state.turnThinking != "Planning the inspection\n**Checking retries**" {
		t.Fatalf("thinking state = activity %q content %q", state.turnActivity, state.turnThinking)
	}

	const width, height = 80, 20
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, TurnActivity: state.turnActivity, TurnThinking: state.turnThinking,
		Messages: append([]transcriptMessage(nil), state.liveMessages...), Scroll: &ui.ScrollController{},
	}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	if got := strings.TrimSpace(rows[height-5]); got != "⠋ Checking retries" {
		t.Fatalf("thinking slot = %q, want Markdown-rendered latest thinking", got)
	}
	if text := strings.Join(rows, "\n"); strings.Contains(text, "1 step") || strings.Contains(text, "0 tool calls") {
		t.Fatalf("thinking-only turn rendered a tool drawer:\n%s", text)
	}
	_, thinkingColumn := markdownCellPosition([]string{rows[height-5]}, "Checking retries")
	if thinkingColumn < 0 || app.Cell(thinkingColumn, height-5).Attribute&ui.AttrBold == 0 {
		t.Fatalf("thinking Markdown is not bold: %q", rows[height-5])
	}

	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 5, MessageID: "message_test", Kind: protocol.SessionEventAssistantTextDelta, ContentIndex: 1, Delta: "I’ll inspect it now."},
	})
	if state.turnActivity != "Working…" || state.turnThinking != "" {
		t.Fatalf("response state = activity %q thinking %q, want Working…", state.turnActivity, state.turnThinking)
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
	if text := strings.Join(rows, "\n"); strings.Contains(text, "I’ll inspect it now.") {
		t.Fatalf("pending response rendered before completion:\n%s", text)
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

func TestLiveToolCallAppearsBeforeTurnFinishes(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, TurnID: "turn_1", Kind: protocol.SessionEventRunStarted},
		{Sequence: 2, TurnID: "turn_1", Kind: protocol.SessionEventUserMessage, Text: "Inspect it"},
		{Sequence: 3, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventAssistantStarted},
		{Sequence: 4, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventAssistantCompleted},
		{Sequence: 5, TurnID: "turn_1", Kind: protocol.SessionEventToolStarted, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
	})
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Running: true, TurnActivity: state.turnActivity,
		Messages: state.liveMessages, Scroll: &ui.ScrollController{},
	}})
	app.Pump(80, 18)
	rows := paintedRows(app, 80, 18)
	if findPaintedRow(rows, "⠋ 1 tool call read") < 0 {
		t.Fatalf("running tool call chip missing before turn completion:\n%s", strings.Join(rows, "\n"))
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

func TestComposerAcceptsBracketedPasteWithoutTriggeringShortcuts(t *testing.T) {
	t.Parallel()

	state := &shellHarnessState{}
	app := uitest.New(shellHarness{State: state})
	app.Pump(40, 12)
	for _, key := range []vaxis.Key{
		{Text: "/", Keycode: '/', EventType: vaxis.EventPaste},
		{Text: "hello", Keycode: 'h', EventType: vaxis.EventPaste},
		{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPaste},
		{Text: "world", Keycode: 'w', EventType: vaxis.EventPaste},
	} {
		app.Send(key)
	}
	if state.composer != "/hello\nworld" {
		t.Fatalf("pasted composer text = %q", state.composer)
	}
	if len(state.submitted) != 0 {
		t.Fatalf("paste submitted composer: %#v", state.submitted)
	}
}

func TestTranscriptAndComposerTextAreMouseSelectable(t *testing.T) {
	t.Parallel()

	state := &shellHarnessState{composer: "select composer"}
	copied := ""
	application := uitest.New(shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Composer: state.composer, Scroll: &ui.ScrollController{},
			Messages: []transcriptMessage{{Role: "assistant", Text: "select transcript"}},
		},
		Callbacks: shellCallbacks{CopySelection: func(text string) { copied = text }},
	})
	application.Pump(60, 16)
	assertMouseSelectionPaints := func(text string) {
		t.Helper()
		rows := paintedRows(application, 60, 16)
		row, col := -1, -1
		for index, value := range rows {
			if offset := strings.Index(value, text); offset >= 0 {
				row, col = index, offset
				break
			}
		}
		if row < 0 {
			t.Fatalf("selectable text %q not painted:\n%s", text, strings.Join(rows, "\n"))
		}
		application.Send(vaxis.Mouse{Col: col, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
		application.Send(vaxis.Mouse{Col: col + 6, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventMotion})
		application.Send(vaxis.Mouse{Col: col + 6, Row: row, Button: vaxis.MouseLeftButton, EventType: vaxis.EventRelease})
		application.Pump(60, 16)
		if got, want := application.Cell(col+1, row).Background, ui.DefaultTheme().Selection; got != want {
			t.Fatalf("%q selection background = %#v, want %#v", text, got, want)
		}
		copied = ""
		application.Send(vaxis.Key{Text: "c", Keycode: 'c', Modifiers: vaxis.ModSuper})
		if copied == "" {
			t.Fatalf("Super+C did not copy selected %q text", text)
		}
	}
	assertMouseSelectionPaints("select transcript")
	assertMouseSelectionPaints("select composer")
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
	messages              []transcriptMessage
	sourceID              string
	composer              string
	turnActivity          string
	location              string
	transcript            ui.ScrollController
	expanded              map[activityToolKey]bool
	cursor                activityToolKey
	subagentConversations []protocol.SubagentConversation
	openedSubagent        string
}

func (s *activityHarnessState) Build(ui.BuildContext) ui.Widget {
	if s.expanded == nil {
		s.expanded = make(map[activityToolKey]bool)
	}
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Messages: s.messages, Composer: s.composer, TurnActivity: s.turnActivity,
			Location: s.location, Scroll: &s.transcript, ActivitySourceID: s.sourceID,
			InlineActivityOpen: map[string]bool{s.sourceID: s.sourceID != ""},
			ActivityExpanded:   s.expanded, ActivityCursor: s.cursor,
			SubagentConversations: s.subagentConversations,
		},
		Callbacks: shellCallbacks{
			OpenActivity: func(_ ui.EventContext, sourceID string) {
				s.SetState(func() { s.sourceID = sourceID })
			},
			ToggleActivityTool: func(_ ui.EventContext, key activityToolKey) {
				s.SetState(func() { s.expanded[key] = !s.expanded[key] })
			},
			SelectActivityTool: func(_ ui.EventContext, key activityToolKey) {
				s.SetState(func() { s.cursor = key })
			},
			OpenSubagentFromTool: func(_ ui.EventContext, agentName string) {
				s.SetState(func() { s.openedSubagent = agentName })
			},
			ComposerChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer = value })
			},
		},
	}
}

func TestSubagentToolNameOpensConversationFromTranscriptChip(t *testing.T) {
	t.Parallel()
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	state := &activityHarnessState{
		messages: []transcriptMessage{
			{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", ToolCalls: []transcriptToolCall{
				{ID: "call_0", Name: "bash", Arguments: json.RawMessage(`{"command":"pwd"}`)},
				{ID: "call_1", Name: "subagent", Arguments: json.RawMessage(`{"action":"start","agent":"reviewer","message":"inspect"}`)},
				{ID: "call_2", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)},
			}},
			{ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "subagent", ToolStatus: "Completed", Text: "queued",
				ToolDetails: json.RawMessage(`{"conversation":{"id":"` + conversationID + `","agent":"reviewer"}}`)},
		},
	}
	application := uitest.New(activityHarness{State: state})
	application.Pump(120, 20)
	rows := paintedRows(application, 120, 20)
	if findPaintedRow(rows, "bash · reviewer · read") < 0 {
		t.Fatalf("transcript tool order changed:\n%s", strings.Join(rows, "\n"))
	}
	column, row := findTextCell(t, rows, "reviewer")
	application.Click(column, row)
	if state.openedSubagent != "reviewer" || state.sourceID != "" {
		t.Fatalf("transcript chip click = opened:%q activity:%q", state.openedSubagent, state.sourceID)
	}
}

func TestSubagentToolNameOpensConversationFromActivityRow(t *testing.T) {
	t.Parallel()
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	key := activityToolKey{TurnID: "turn_1", ToolCallID: "call_1"}
	state := &activityHarnessState{
		messages: []transcriptMessage{
			{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", ToolCalls: []transcriptToolCall{{
				ID: "call_1", Name: "subagent", Arguments: json.RawMessage(`{"action":"start","agent":"reviewer","message":"inspect"}`),
			}}},
			{ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "subagent", ToolStatus: "Completed", Text: "queued"},
		},
		sourceID:              "turn-work:turn_1:assistant_1",
		subagentConversations: []protocol.SubagentConversation{{ID: conversationID, AgentName: "reviewer", State: "running"}},
	}
	application := uitest.New(activityHarness{State: state})
	application.Pump(120, 20)
	rows := paintedRows(application, 120, 20)
	column, row := findTextCell(t, rows, "reviewer")
	application.Click(column, row)
	if state.openedSubagent != "reviewer" || state.cursor != (activityToolKey{}) || state.expanded[key] {
		t.Fatalf("activity name click = opened:%q cursor:%+v expanded:%t", state.openedSubagent, state.cursor, state.expanded[key])
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

func TestInlineActivityDoesNotInstallPageKeyBindings(t *testing.T) {
	t.Parallel()

	pages := 0
	app := uitest.New(shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Messages: activityHarnessMessages(),
			ActivitySourceID: "turn-work:turn_1:assistant_1", ActivitySelected: true,
			Scroll: &ui.ScrollController{}, ActivityScroll: &ui.ScrollController{},
		},
		Callbacks: shellCallbacks{ScrollActivity: func(_ ui.EventContext, delta int) { pages += delta }},
	})
	app.Pump(100, 20)
	app.Send(vaxis.Key{Keycode: vaxis.KeyPgDown})
	app.Send(vaxis.Key{Keycode: vaxis.KeyPgUp})
	app.Send(vaxis.Key{Keycode: vaxis.KeyPgDown})
	if pages != 0 {
		t.Fatalf("inline activity handled page keys %d times, want 0", pages)
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

func TestInlineActivityDoesNotOpenWorkspacePane(t *testing.T) {
	t.Parallel()

	state := &activityHarnessState{
		messages: activityHarnessMessages(), sourceID: "turn-work:turn_1:assistant_1",
	}
	app := uitest.New(activityHarness{State: state})
	for _, width := range []int{125, 124} {
		app.Pump(width, 20)
		rows := paintedRows(app, width, 20)
		text := strings.Join(rows, "\n")
		if !strings.Contains(text, "▾ 1 tool call") || !strings.Contains(text, "I’ll inspect it.") {
			t.Fatalf("%d-column shell did not show expanded inline activity:\n%s", width, text)
		}
		if strings.Contains(text, "Transcript  Activity") {
			t.Fatalf("%d-column shell still rendered Activity workspace tabs:\n%s", width, text)
		}
	}
}

func TestExpandedInlineActivityShowsToolDetail(t *testing.T) {
	t.Parallel()

	command := "grep -R TerminalColors app | head -10; grep DEFAULT app | head"
	key := activityToolKey{TurnID: "turn_1", ToolCallID: "call_1"}
	state := &activityHarnessState{
		messages: []transcriptMessage{
			{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", ToolCalls: []transcriptToolCall{{
				ID: "call_1", Name: "bash", Arguments: json.RawMessage(`{"command":"` + command + `"}`),
			}}},
			{ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "bash", ToolStatus: "Completed", Text: "done"},
		},
		sourceID: "turn-work:turn_1:assistant_1",
	}
	app := uitest.New(activityHarness{State: state})
	app.Pump(200, 24)
	rows := paintedRows(app, 200, 24)
	column, row := findTextCell(t, rows, "bash grep → head · grep → head")
	app.Click(column, row)
	app.Pump(200, 24)
	app.Pump(200, 24)
	if !state.expanded[key] {
		t.Fatal("inline activity tool did not expand")
	}
	rows = paintedRows(app, 200, 24)
	for _, expected := range []string{"✓ ▾ bash grep → head · grep → head", command, "done"} {
		if findPaintedRow(rows, expected) < 0 {
			t.Fatalf("expanded inline activity detail %q missing:\n%s", expected, strings.Join(rows, "\n"))
		}
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

func TestClaudeBrowserLoginDialogShowsFallbackInput(t *testing.T) {
	t.Parallel()

	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseAuthBrowser,
		BrowserInstructions: auth.AnthropicLoginInstructions{
			AuthorizationURL: "https://claude.ai/oauth/authorize?client_id=test",
			RedirectURI:      "http://localhost:53692/callback",
		},
		AuthCode: "http://localhost:53692/callback?code=manual-code",
		Status:   "Waiting for browser approval…",
	}})
	app.Pump(90, 24)
	app.Pump(90, 24)
	text := strings.Join(paintedRows(app, 90, 24), "\n")
	for _, expected := range []string{"Claude Pro or Max", "Open this URL", "https://claude.ai/oauth/authorize", "paste the final redirect", "URL or authorization code", "http://localhost:53692/callback?code=manual-code", "Waiting for browser approval", "enter submit · esc cancel"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Claude login dialog missing %q:\n%s", expected, text)
		}
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

func TestSafeExternalHyperlinkAllowsOnlyNavigablePublicSchemes(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"https://example.test/docs", "http://example.test/docs", "mailto:hello@example.test",
	} {
		if got := safeExternalHyperlink(raw); got != raw {
			t.Errorf("safeExternalHyperlink(%q) = %q", raw, got)
		}
	}
	for _, raw := range []string{
		"file:///tmp/private", "javascript:alert(1)", "https://user@example.test/docs",
		"https://example.test/\u009bunsafe", "https://example.test/\u202eunsafe",
		"https://example.test/\x9c\x9b2J", "not a URL",
	} {
		if got := safeExternalHyperlink(raw); got != "" {
			t.Errorf("safeExternalHyperlink(%q) = %q, want empty", raw, got)
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

func TestCtrlCRequestsQuitAndSuperCCopies(t *testing.T) {
	t.Parallel()

	app := uitest.New(shellView{
		Snapshot:  shellSnapshot{Phase: phaseReady},
		Callbacks: shellCallbacks{Quit: func(ctx ui.EventContext) { ctx.Quit() }},
	})
	app.Pump(46, 20)
	app.Send(vaxis.Key{Text: "c", Keycode: 'c', Modifiers: vaxis.ModSuper})
	if app.ShouldQuit() {
		t.Fatal("Super+C requested quit instead of copy")
	}
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
