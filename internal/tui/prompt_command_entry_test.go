package tui

import (
	"reflect"
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func promptCommandMessageForTest() protocol.TranscriptMessage {
	return protocol.TranscriptMessage{
		ID: "message_command", TurnID: "turn_command", Role: "user",
		Content: []protocol.TranscriptContent{protocol.NewTranscriptContent(protocol.PromptCommandContent{
			Name: "claude-fix", Arguments: "123 high", Source: protocol.PromptCommandSourceClaudeProject,
			Text: "Pretend to fix issue #123 at high priority.",
		})},
	}
}

func TestPromptCommandUserMessageShowsItsInvocation(t *testing.T) {
	t.Parallel()
	theme := ui.DefaultTheme()
	app := uitest.New(transcriptUserEntry(theme, promptCommandMessageForTest(), nil, false, nil))
	app.Pump(40, 3)
	// The wash's half-block edges frame one content row of ordinary user text.
	if got, want := strings.TrimRight(blankCells(app, 40, 1), " "), "  /claude-fix 123 high"; got != want {
		t.Fatalf("invocation row = %q, want %q", got, want)
	}
	for _, column := range []int{2, 14} {
		if got := app.Cell(column, 1).Style; got.Foreground != theme.Foreground || got.Background != userMessageBackground(theme) {
			t.Errorf("cell %d %q style = %#v, want user message text", column, app.Cell(column, 1).Grapheme, got)
		}
	}
}

func TestMessageHistoryRecallsPromptCommandInvocations(t *testing.T) {
	t.Parallel()
	typed := protocol.TranscriptMessage{ID: "message_typed", Role: "user", Content: []protocol.TranscriptContent{protocol.TextBlock("typed prompt")}}
	entries := messageHistoryEntries([]protocol.TranscriptMessage{promptCommandMessageForTest(), typed})
	want := []messageHistoryEntry{{ID: "message_command", Text: "/claude-fix 123 high"}, {ID: "message_typed", Text: "typed prompt"}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("history entries = %#v, want %#v", entries, want)
	}
}

func TestComposerPromptCommandParsesDiscoveredInvocations(t *testing.T) {
	t.Parallel()
	contributions := promptPaletteCommands([]protocol.PromptCommand{{Name: "claude-fix", Description: "Fix", Source: protocol.PromptCommandSourceClaudeProject, Location: "/repo/.claude/commands/claude-fix.md"}})
	cases := []struct {
		text, name, args string
		ok               bool
	}{
		{"/claude-fix 123 high", "claude-fix", "123 high", true},
		{"/claude-fix", "claude-fix", "", true},
		{"/claude-fix\n\"auth module\" now", "claude-fix", "\"auth module\" now", true},
		{"/unknown 123", "", "", false},
		{"claude-fix 123", "", "", false},
		{"/ claude-fix", "", "", false},
	}
	for _, test := range cases {
		name, args, ok := composerPromptCommand(test.text, contributions)
		if name != test.name || args != test.args || ok != test.ok {
			t.Errorf("composerPromptCommand(%q) = %q, %q, %v; want %q, %q, %v", test.text, name, args, ok, test.name, test.args, test.ok)
		}
	}
}
