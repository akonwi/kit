package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestInteractionOptionMarker(t *testing.T) {
	t.Parallel()
	for index, want := range map[int]string{0: "A", 1: "B", 25: "Z", 26: "AA", 27: "AB"} {
		if got := interactionOptionMarker(index); got != want {
			t.Errorf("marker %d = %q, want %q", index, got, want)
		}
	}
}

func TestInteractionDockKeyboardSelect(t *testing.T) {
	t.Parallel()
	var response protocol.InteractionResponse
	app := uitest.New(interactionDock{
		Request: protocol.InteractionRequest{
			ID: "request", Kind: protocol.InteractionSelect, Title: "Choose",
			Options: []protocol.InteractionOption{{ID: "one", Label: "One"}, {ID: "two", Label: "Two"}},
		},
		OnRespond: func(_ ui.EventContext, got protocol.InteractionResponse, done func(error)) {
			response = got
			done(nil)
		},
	})
	app.Pump(60, 12)
	app.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if response.SelectedOptionID != "two" {
		t.Fatalf("selected option = %q, want two", response.SelectedOptionID)
	}
}

func TestGuidedInteractionKeepsKeyboardNavigationAfterTextStep(t *testing.T) {
	t.Parallel()
	var response protocol.InteractionResponse
	app := uitest.New(interactionDock{
		Request: protocol.InteractionRequest{
			ID: "request", Kind: protocol.InteractionGuided, Title: "Guided",
			Questions: []protocol.InteractionQuestion{
				{ID: "text", Kind: protocol.InteractionQuestionText, Prompt: "Answer", Required: true},
				{ID: "select", Kind: protocol.InteractionQuestionSelect, Prompt: "Choose", Required: true, Options: []protocol.InteractionOption{{ID: "one", Label: "One"}, {ID: "two", Label: "Two"}}},
			},
		},
		OnRespond: func(_ ui.EventContext, got protocol.InteractionResponse, done func(error)) { response = got; done(nil) },
	})
	app.Pump(60, 14)
	app.Send(vaxis.Key{Keycode: 'a', Text: "a"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(60, 14)
	app.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	app.Send(vaxis.Key{Keycode: vaxis.KeySpace})
	if response.Cancelled {
		t.Fatal("Space cancelled the guided interaction")
	}
	if got := response.Answers["select"].OptionIDs; len(got) != 1 || got[0] != "two" {
		t.Fatalf("selected options = %#v, want two", got)
	}
}

func TestGuidedInteractionTabNavigationRevisitsAnswers(t *testing.T) {
	t.Parallel()
	var response protocol.InteractionResponse
	app := uitest.New(interactionDock{
		Request: protocol.InteractionRequest{
			ID: "request", Kind: protocol.InteractionGuided, Title: "Guided",
			Questions: []protocol.InteractionQuestion{
				{ID: "first", Kind: protocol.InteractionQuestionText, Prompt: "First", Required: true},
				{ID: "choice", Kind: protocol.InteractionQuestionSelect, Prompt: "Choose", Required: true, Options: []protocol.InteractionOption{{ID: "one", Label: "One"}, {ID: "two", Label: "Two"}}},
				{ID: "last", Kind: protocol.InteractionQuestionText, Prompt: "Last", Required: true},
			},
		},
		OnRespond: func(_ ui.EventContext, got protocol.InteractionResponse, done func(error)) { response = got; done(nil) },
	})
	app.Pump(60, 14)
	app.Send(vaxis.Key{Keycode: 'a', Text: "a"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(60, 14)
	app.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	app.Pump(60, 14)
	app.Send(vaxis.Key{Keycode: vaxis.KeyTab, Modifiers: vaxis.ModShift})
	app.Pump(60, 14)
	app.Send(vaxis.Key{Keycode: vaxis.KeyUp})
	app.Send(vaxis.Key{Keycode: vaxis.KeyTab})
	app.Pump(60, 14)
	app.Send(vaxis.Key{Keycode: 'z', Text: "z"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if got := response.Answers["choice"].OptionIDs; len(got) != 1 || got[0] != "one" {
		t.Fatalf("revisited choice = %#v, want one", got)
	}
}

func TestGuidedInteractionTabDoesNotAnswerBoolean(t *testing.T) {
	t.Parallel()
	responded := false
	app := uitest.New(interactionDock{
		Request:   protocol.InteractionRequest{ID: "request", Kind: protocol.InteractionGuided, Title: "Guided", Questions: []protocol.InteractionQuestion{{ID: "confirm", Kind: protocol.InteractionQuestionBoolean, Prompt: "Continue?", Required: true}}},
		OnRespond: func(_ ui.EventContext, _ protocol.InteractionResponse, done func(error)) { responded = true; done(nil) },
	})
	app.Pump(60, 10)
	app.Send(vaxis.Key{Keycode: vaxis.KeyTab})
	if responded {
		t.Fatal("Tab implicitly answered an unanswered boolean")
	}
}

func TestInteractionDockKeyboardConfirm(t *testing.T) {
	t.Parallel()
	var response protocol.InteractionResponse
	app := uitest.New(interactionDock{
		Request: protocol.InteractionRequest{ID: "request", Kind: protocol.InteractionConfirm, Title: "Continue?"},
		OnRespond: func(_ ui.EventContext, got protocol.InteractionResponse, done func(error)) {
			response = got
			done(nil)
		},
	})
	app.Pump(60, 8)
	app.Send(vaxis.Key{Keycode: 'y', Text: "y"})
	if response.Confirmed == nil || !*response.Confirmed {
		t.Fatalf("confirmed = %#v, want true", response.Confirmed)
	}
}

func TestPluginInteractionInputShowsInitialValueAndAcceptsEmptyAnswer(t *testing.T) {
	initial := "An initial value wider than a compact input"
	var response protocol.InteractionResponse
	app := uitest.New(interactionDock{Request: protocol.InteractionRequest{ID: "request", Plugin: &protocol.PluginInteractionOwner{PluginID: "ui-api-demo", Instance: "owner:1"}, Kind: protocol.InteractionInput, Title: "Plugin input", InitialValue: initial, Placeholder: "Your note"}, OnRespond: func(_ ui.EventContext, got protocol.InteractionResponse, done func(error)) { response = got; done(nil) }})
	app.Pump(100, 14)
	app.Pump(100, 14)
	if row := findPaintedRow(paintedRows(app, 100, 14), initial); row < 0 {
		t.Fatalf("initial text not visible: %v", paintedRows(app, 100, 14))
	}
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if response.Value == nil || *response.Value != initial {
		t.Fatalf("initial submission = %#v", response)
	}
	empty := uitest.New(interactionDock{Request: protocol.InteractionRequest{ID: "empty", Plugin: &protocol.PluginInteractionOwner{PluginID: "ui-api-demo", Instance: "owner:1"}, Kind: protocol.InteractionInput, Title: "Optional note", Placeholder: "Your note"}, OnRespond: func(_ ui.EventContext, got protocol.InteractionResponse, done func(error)) { response = got; done(nil) }})
	empty.Pump(100, 14)
	empty.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if response.Value == nil || *response.Value != "" || response.Cancelled {
		t.Fatalf("empty input should remain a value: %#v", response)
	}
}

func TestPluginInteractionSelectUsesPlainListEvenWhenFilterableIsRequested(t *testing.T) {
	filterable := true
	var response protocol.InteractionResponse
	app := uitest.New(interactionDock{Request: protocol.InteractionRequest{ID: "select", Plugin: &protocol.PluginInteractionOwner{PluginID: "ui-api-demo", Instance: "owner:1"}, Kind: protocol.InteractionSelect, Title: "Choose target", Filterable: &filterable, Placeholder: "Filter scopes...", Options: []protocol.InteractionOption{{ID: "file", Label: "Current file"}, {ID: "project", Label: "Whole project"}}}, OnRespond: func(_ ui.EventContext, got protocol.InteractionResponse, done func(error)) { response = got; done(nil) }})
	app.Pump(100, 18)
	rows := paintedRows(app, 100, 18)
	for _, label := range []string{"A. Current file", "B. Whole project", "↑↓ move · enter/space select · esc cancel"} {
		if findPaintedRow(rows, label) < 0 {
			t.Fatalf("missing %q in plain selection list: %v", label, rows)
		}
	}
	app.Send(vaxis.Key{Keycode: 'j', Text: "j"})
	app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
	if response.SelectedOptionID != "project" {
		t.Fatalf("plain-list keyboard selection = %#v", response)
	}
}

func TestPluginConfirmationHonorsLabelsDefaultAndFocusTraversal(t *testing.T) {
	for _, defaultValue := range []bool{false, true} {
		for _, tab := range []bool{false, true} {
			var response protocol.InteractionResponse
			app := uitest.New(interactionDock{Request: protocol.InteractionRequest{ID: "confirm", Plugin: &protocol.PluginInteractionOwner{PluginID: "ui-api-demo", Instance: "owner:1"}, Kind: protocol.InteractionConfirm, Title: "Confirm plugin action", ConfirmLabel: "Show toast", CancelLabel: "Do not show", DefaultValue: &defaultValue}, OnRespond: func(_ ui.EventContext, got protocol.InteractionResponse, done func(error)) { response = got; done(nil) }})
			app.Pump(100, 14)
			rows := paintedRows(app, 100, 14)
			for _, label := range []string{"Show toast", "Do not show", "ui-api-demo"} {
				if findPaintedRow(rows, label) < 0 {
					t.Fatalf("missing %s: %v", label, rows)
				}
			}
			if tab {
				app.Send(vaxis.Key{Keycode: vaxis.KeyTab})
				app.Pump(100, 14)
			}
			app.Send(vaxis.Key{Keycode: vaxis.KeyEnter})
			want := defaultValue
			if tab {
				want = !want
			}
			if response.Confirmed == nil || *response.Confirmed != want {
				t.Fatalf("default=%v tab=%v result=%#v", defaultValue, tab, response)
			}
		}
	}
}

func TestPluginDialogMetadataHasOneEventOwnerDuringModelRun(t *testing.T) {
	state := appState{session: protocol.SessionInfo{ID: "session"}, activeRunID: "run", liveAssistant: -1, liveTools: make(map[string]int)}
	request := protocol.InteractionRequest{ID: "plugin_request", Plugin: &protocol.PluginInteractionOwner{PluginID: "demo", Instance: "host:1"}, Kind: protocol.InteractionConfirm, Title: "Plugin action"}
	requested := protocol.SessionEvent{SessionID: "session", StreamID: "stream", Sequence: 1, Kind: protocol.SessionEventInteractionRequested, Interaction: &request}
	resolved := protocol.SessionEvent{SessionID: "session", StreamID: "stream", Sequence: 2, Kind: protocol.SessionEventInteractionResolved, InteractionID: request.ID, InteractionResolution: "answered"}
	state.applySessionMetadataEvents([]protocol.SessionEvent{requested})
	if len(state.pendingInteractions) != 1 || state.pendingInteractions[0].ID != request.ID || !state.agentFeedbackPending {
		t.Fatalf("plugin dialog during model run = %#v", state.pendingInteractions)
	}
	state.applySessionMetadataEvents([]protocol.SessionEvent{resolved})
	state.applyRunEvents([]protocol.SessionEvent{requested})
	if len(state.pendingInteractions) != 0 || state.agentFeedbackPending {
		t.Fatalf("delayed run watcher reopened plugin dialog: %#v", state.pendingInteractions)
	}
	model := request
	model.Plugin = nil
	model.ID = "model_request"
	model.RunID = "run"
	model.ToolCallID = "tool"
	requested.Interaction = &model
	requested.RunID = "run"
	requested.Sequence = 3
	resolved.RunID = "run"
	resolved.InteractionID = model.ID
	resolved.Sequence = 4
	state.applyRunEvents([]protocol.SessionEvent{requested, resolved})
	state.applySessionMetadataEvents([]protocol.SessionEvent{requested})
	if len(state.pendingInteractions) != 0 || state.agentFeedbackPending {
		t.Fatalf("delayed metadata watcher reopened model dialog: %#v", state.pendingInteractions)
	}
}
