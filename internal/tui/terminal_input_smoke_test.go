package tui

import (
	"os"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

// TestTerminalInputOwnershipSmoke is opt-in because ui.Run requires a terminal.
// It uses only client fixtures: no daemon, credentials, or provider requests.
// The driver types a draft, opens the palette, injects a dock with F2, pastes an
// answer, closes the dock, edits at the retained draft cursor, and quits.
func TestTerminalInputOwnershipSmoke(t *testing.T) {
	if os.Getenv("KIT_TUI_TERMINAL_SMOKE") != "1" {
		t.Skip("requires an interactive PTY driver")
	}
	state := &terminalInputSmokeState{inputOwnerTransitionState: *newInputOwnerTransitionState()}
	if err := ui.Run(terminalInputSmoke{state}); err != nil {
		t.Fatal(err)
	}
	if state.answer != "answer" || state.composer != "Xdra!ft" || state.paletteAtArrival != "model" {
		t.Fatalf("terminal transition: answer=%q draft=%q palette=%q", state.answer, state.composer, state.paletteAtArrival)
	}
}

type terminalInputSmoke struct{ state *terminalInputSmokeState }

func (w terminalInputSmoke) CreateState() ui.State { return w.state }

type terminalInputSmokeState struct {
	inputOwnerTransitionState
	answer, paletteAtArrival string
}

func (s *terminalInputSmokeState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if key, ok := event.(ui.Key); ok && ctx.Phase() == ui.CapturePhase && key.MatchString("F2") {
		s.SetState(func() {
			s.paletteAtArrival = s.palette.Query
			s.pendingInteractions = []protocol.InteractionRequest{{ID: "smoke", Kind: protocol.InteractionInput, Title: "Smoke response"}}
		})
		return ui.EventHandled
	}
	return s.appState.HandleEvent(ctx, event)
}
func (s *terminalInputSmokeState) Build(ctx ui.BuildContext) ui.Widget {
	view := s.inputOwnerTransitionState.Build(ctx).(shellView)
	view.Callbacks.OpenPalette = func(ui.EventContext) { s.openPalette() }
	view.Callbacks.RespondInteraction = func(_ ui.EventContext, response protocol.InteractionResponse, done func(error)) {
		s.SetState(func() {
			if response.Value != nil {
				s.answer = *response.Value
			}
			s.pendingInteractions = nil
		})
		done(nil)
	}
	return view
}
