package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestParseDirectBash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   string
		command string
		exclude bool
		ok      bool
	}{
		{input: "!git status", command: "git status", ok: true},
		{input: "!! git status ", command: "git status", exclude: true, ok: true},
		{input: "!!!echo hi", command: "!echo hi", exclude: true, ok: true},
		{input: "!\nprintf ok", command: "printf ok", ok: true},
		{input: "!"},
		{input: "!!", exclude: true},
		{input: " !git status"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			command, exclude, ok := parseDirectBash(test.input)
			if command != test.command || exclude != test.exclude || ok != test.ok {
				t.Fatalf("parseDirectBash(%q) = %q/%v/%v, want %q/%v/%v", test.input, command, exclude, ok, test.command, test.exclude, test.ok)
			}
		})
	}
}

func TestBashHistoryRestoresPerEntryContextMode(t *testing.T) {
	t.Parallel()
	state := appState{
		composer: "!git",
		messages: []transcriptMessage{
			{ID: "bash_old", Role: "bash", Bash: bashExecution("bash_old", "git status", false, "")},
			{ID: "bash_new", Role: "bash", Bash: bashExecution("bash_new", "git log --oneline", true, "")},
		},
	}
	entries := state.bashHistoryEntries()
	if !state.bashHistory.OpenFor(entries, state.composer) {
		t.Fatal("history did not open")
	}
	if state.bashHistory.Query != "git" {
		t.Fatalf("history state = %+v", state.bashHistory)
	}
	entries = state.bashHistory.filtered()
	if len(entries) != 2 || entries[0].ID != "bash_new" {
		t.Fatalf("filtered history = %+v, want newest first", entries)
	}
	selected, ok := state.bashHistory.Selected()
	if !ok || bashHistoryComposerText(selected) != "!!git log --oneline" {
		t.Fatalf("selected history = %+v/%v", selected, ok)
	}
}

func TestReadyShellPresentsBashModeAndExecution(t *testing.T) {
	t.Parallel()
	const width, height = 80, 20
	theme := ui.DefaultTheme()
	execution := bashExecution("bash_1", "git status", false, "On branch main")
	app := uitest.New(ui.Provider[ui.Theme]{Value: theme, Child: shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Composer: "!!git status", Location: "/workspace",
		Messages: []transcriptMessage{{ID: execution.ID, Role: "bash", Bash: execution}},
		Scroll:   &ui.ScrollController{}, BashCollapsed: map[string]bool{},
	}}})
	app.Pump(width, height)
	rows := paintedRows(app, width, height)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "✓ git status") || !strings.Contains(joined, "On branch main") {
		t.Fatalf("bash transcript missing:\n%s", joined)
	}
	if !strings.Contains(rows[height-1], "bash command · result exc") {
		t.Fatalf("footer = %q", rows[height-1])
	}
	separatorRow := height - 4
	if app.Cell(0, separatorRow).Style.Foreground != theme.SuccessText {
		t.Fatalf("composer separator foreground = %v, want %v", app.Cell(0, separatorRow).Style.Foreground, theme.SuccessText)
	}
}

func bashExecution(id, command string, exclude bool, output string) *protocol.BashExecution {
	exitCode := 0
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return &protocol.BashExecution{
		ID: id, SessionID: "session_1", Command: command,
		Status: protocol.BashExecutionCompleted, Output: output, ExitCode: &exitCode,
		ExcludeFromContext: exclude, StartedAt: now, CompletedAt: now,
	}
}
