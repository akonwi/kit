package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func testPluginCommand() protocol.PluginCommand {
	return protocol.PluginCommand{ID: "plugin-demo.echo", LocalID: "echo", PluginID: "plugin-demo", Instance: "pluginhost_first:1", Description: "Echo a message", ArgName: "message", Category: "Workspace"}
}

func TestPluginCommandPalettePresentationAndLiteralArguments(t *testing.T) {
	command := testPluginCommand()
	state := &paletteHarnessState{}
	state.palette.SetContributions(pluginPaletteCommands([]protocol.PluginCommand{command}), true)
	state.palette.OpenFor(true)
	state.palette.SetQuery(true, `plugin-demo.echo  "two words" $(literal)  `)
	selected, ok := state.palette.Selected(true, state.palette.Query)
	if !ok || selected.Plugin == nil || *selected.Plugin != command || !paletteCommandAvailable(selected.ID, true, state.palette.Contributions) {
		t.Fatalf("selected = %#v", selected)
	}
	if args := pluginPaletteArgs(state.palette.Query); args != ` "two words" $(literal)  ` {
		t.Fatalf("literal args = %q", args)
	}
	application := uitest.New(paletteHarness{State: state})
	application.Pump(100, 24)
	rows := paintedRows(application, 100, 24)
	row := findPaintedRow(rows, "Echo a message")
	if row < 0 || !strings.Contains(rows[row], "plugin-demo.echo <message>") {
		t.Fatalf("plugin row =\n%s", strings.Join(rows, "\n"))
	}
	application.Click(40, row)
	application.Pump(100, 24)
	if state.executed != selected.ID || state.palette.Open {
		t.Fatalf("mouse activation = %q, open %v", state.executed, state.palette.Open)
	}
}

func TestPluginPaletteReloadRequiresExplicitReselection(t *testing.T) {
	command := testPluginCommand()
	var palette paletteController
	palette.SetContributions(pluginPaletteCommands([]protocol.PluginCommand{command}), false)
	palette.OpenFor(false)
	palette.SetQuery(false, "plugin-demo.echo notes.txt")
	previous := palette.Selection
	command.Instance = "pluginhost_replacement:2"
	palette.SetContributions(pluginPaletteCommands([]protocol.PluginCommand{command}), false)
	if palette.Selection != previous {
		t.Fatal("reload silently retargeted selected command")
	}
	if _, ok := palette.Selected(false, palette.Query); ok {
		t.Fatal("stale owner remained executable")
	}
	rejected, run, handled := palette.HandleKey(false, ui.Key{Keycode: vaxis.KeyEnter})
	if !run || !handled || rejected.ID != previous {
		t.Fatalf("stale enter must route to unavailable feedback: %#v %v %v", rejected, run, handled)
	}
	palette.Move(false, 1)
	selected, ok := palette.Selected(false, palette.Query)
	if !ok || selected.Plugin.Instance != command.Instance {
		t.Fatalf("explicit reselection = %#v", selected)
	}
}

type pluginCommandTestSession struct {
	fakeSession
	execute func(context.Context, protocol.PluginCommandInput) error
}

func (s *pluginCommandTestSession) ExecutePluginCommand(ctx context.Context, input protocol.PluginCommandInput) error {
	return s.execute(ctx, input)
}

type pluginExecutionHarness struct{ state *pluginExecutionState }

func (h pluginExecutionHarness) CreateState() ui.State { return h.state }

type pluginExecutionState struct{ appState }

func (*pluginExecutionState) InitState()                        {}
func (*pluginExecutionState) Dispose()                          {}
func (s *pluginExecutionState) Build(ui.BuildContext) ui.Widget { return ui.Text{Value: s.composer} }

func mountPluginExecution(t *testing.T, execute func(context.Context, protocol.PluginCommandInput) error) (*pluginExecutionState, chan func(), *[]toastInput) {
	t.Helper()
	state := &pluginExecutionState{}
	state.ctx = t.Context()
	state.resetAttachmentContext()
	t.Cleanup(state.attachmentCancel)
	state.bound = &pluginCommandTestSession{fakeSession: fakeSession{id: "attached"}, execute: execute}
	state.phase = phaseReady
	state.composer = "Keep this composer draft unchanged"
	state.session = protocol.SessionInfo{ID: "attached"}
	notices := []toastInput{}
	state.showToastOverride = func(input toastInput) { notices = append(notices, input) }
	completions := make(chan func(), 8)
	app := newAbortTestApp(t, pluginExecutionHarness{state}, func(f func()) { completions <- f })
	app.Pump(120, 36)
	if !strings.Contains(app.Text(), state.composer) {
		t.Fatalf("draft presentation = %s", app.Text())
	}
	return state, completions, &notices
}

func TestPluginCommandExecutionPreservesDraftAndDoesNotStartModelRun(t *testing.T) {
	received := make(chan protocol.PluginCommandInput, 1)
	release := make(chan struct{})
	state, completions, notices := mountPluginExecution(t, func(ctx context.Context, input protocol.PluginCommandInput) error {
		received <- input
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	command := testPluginCommand()
	state.palette.SetContributions(pluginPaletteCommands([]protocol.PluginCommand{command}), false)
	state.palette.OpenFor(false)
	state.palette.SetQuery(false, command.ID+"  literal args ")
	state.runPaletteCommand(ui.EventContext{}, state.palette.Selection)
	select {
	case input := <-received:
		if input != (protocol.PluginCommandInput{ID: command.ID, Instance: command.Instance, Args: " literal args "}) {
			t.Fatalf("dispatch = %#v", input)
		}
	case <-time.After(time.Second):
		t.Fatal("command not dispatched")
	}
	if state.composer != "Keep this composer draft unchanged" || state.runPending || state.palette.Open || state.pluginCommandID != command.ID {
		t.Fatalf("execution changed draft/model: %q, running=%v, palette=%v", state.composer, state.runPending, state.palette.Open)
	}
	state.runPluginCommandWithDispatch(command, "not replayed", func(f func()) { completions <- f })
	if last := (*notices)[len(*notices)-1]; last.Title != "Plugin command in progress" || last.Variant != toastWarning {
		t.Fatalf("bounded admission feedback = %#v", last)
	}
	close(release)
	select {
	case complete := <-completions:
		complete()
	case <-time.After(time.Second):
		t.Fatal("completion not dispatched")
	}
	if state.pluginCommandID != "" || state.composer != "Keep this composer draft unchanged" || state.runPending {
		t.Fatal("completion changed model/draft state")
	}
}

func TestPluginCommandAttachmentCancellationSuppressesOldFeedback(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	state, completions, notices := mountPluginExecution(t, func(ctx context.Context, _ protocol.PluginCommandInput) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	state.runPluginCommandWithDispatch(testPluginCommand(), "", func(f func()) { completions <- f })
	<-started
	state.resetAttachmentContext()
	defer state.attachmentCancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("attachment replacement did not cancel command")
	}
	if state.pluginCommandID != "" || !reflect.DeepEqual(*notices, []toastInput{{Title: "Running plugin command", Subtitle: "plugin-demo.echo", Variant: toastInfo}}) {
		t.Fatalf("old attachment feedback = %#v", *notices)
	}
}

func TestPluginCommandFailureFeedback(t *testing.T) {
	if got := pluginCommandResultToast("demo.run", &protocol.PluginCommandError{Code: protocol.PluginCommandUnavailable, Message: "Unavailable"}); got != stalePluginCommandToast() {
		t.Fatalf("stale feedback = %#v", got)
	}
	got := pluginCommandResultToast("demo.run", errors.New("cancelled"))
	want := toastInput{Title: "Plugin command failed", Subtitle: "demo.run: cancelled Effects may have partially completed; the command was not retried.", Variant: toastError}
	if got != want {
		t.Fatalf("failure feedback = %#v", got)
	}
}

func TestPluginPaletteContributionsPreserveCapabilityAndNamespace(t *testing.T) {
	command := testPluginCommand()
	snapshot := protocol.SessionSnapshot{PluginCommands: []protocol.PluginCommand{command}, PromptCommands: []protocol.PromptCommand{{Name: command.ID, Description: "Template"}}}
	state := appState{bound: &pluginCommandTestSession{}}
	commands := paletteCommands(state.sessionPaletteCommands(snapshot))
	for _, entry := range commands {
		if entry.Name == command.ID {
			if entry.Plugin == nil {
				t.Fatal("prompt template shadowed namespaced plugin command")
			}
			return
		}
	}
	t.Fatal("plugin command missing")
}

func TestPluginCatalogUpdatesDuringActiveRunMetadataBaseline(t *testing.T) {
	state := appState{bound: &pluginCommandTestSession{}, runPending: true, activeRunID: "running", composer: "Preserve draft"}
	command := testPluginCommand()
	snapshot := protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "attached", CWD: "/repo"}, ActiveRunID: "running", EventStreamID: "stream-first", PluginCommands: []protocol.PluginCommand{command}}
	state.applySessionMetadataBaseline(snapshot)
	state.palette.OpenFor(true)
	state.palette.SetQuery(true, command.ID)
	selected, ok := state.palette.Selected(true, state.palette.Query)
	if !ok || selected.Plugin == nil || *selected.Plugin != command {
		t.Fatalf("active run catalog = %#v", selected)
	}
	snapshot.EventStreamID = "stream-second"
	snapshot.PluginCommands[0].Instance = "pluginhost_replacement:2"
	state.applySessionMetadataBaseline(snapshot)
	if _, ok := state.palette.Selected(true, state.palette.Query); ok {
		t.Fatal("active-run reload retargeted old selection")
	}
	state.palette.Move(true, 1)
	selected, ok = state.palette.Selected(true, state.palette.Query)
	if !ok || selected.Plugin.Instance != "pluginhost_replacement:2" {
		t.Fatalf("active-run replacement = %#v", selected)
	}
	if !state.runPending || state.activeRunID != "running" || state.composer != "Preserve draft" {
		t.Fatal("metadata update disturbed active run or draft")
	}
}

func TestPendingPluginPaletteRowShowsDisabledReason(t *testing.T) {
	state := &paletteHarnessState{}
	commands := pluginPaletteCommands([]protocol.PluginCommand{testPluginCommand()})
	commands[0].DisabledReason = "command running"
	state.palette.SetContributions(commands, false)
	state.palette.OpenFor(false)
	state.palette.SetQuery(false, "plugin-demo.echo")
	application := uitest.New(paletteHarness{State: state})
	application.Pump(100, 24)
	rows := paintedRows(application, 100, 24)
	row := findPaintedRow(rows, glyphCircleSlash+" command running · Echo a message")
	if row < 0 {
		t.Fatalf("disabled plugin row =\n%s", strings.Join(rows, "\n"))
	}
	if paletteCommandAvailable(commands[0].ID, false, commands) {
		t.Fatal("pending row is executable")
	}
	application.Click(40, row)
	application.Pump(100, 24)
	if !state.palette.Open || state.executed != "" {
		t.Fatal("disabled mouse click executed plugin")
	}
}
