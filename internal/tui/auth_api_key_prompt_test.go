package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/auth"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestAuthAPIKeyPromptUsesPalettePickerPrompt(t *testing.T) {
	t.Parallel()

	pickerTheme := ui.DefaultThemeSet().Dark
	application := uitest.New(ui.Provider[ui.Theme]{Value: pickerTheme, Child: shellView{Snapshot: shellSnapshot{
		Phase: phaseAuthAPIKey, AuthProviderID: auth.OpenAIProviderID, AuthAPIKey: "sk-secret", Error: "invalid API key",
	}}})
	application.Pump(80, 24)
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	// The key is obscured, one bullet per character.
	inputColumn, inputRow := assertPickerSearchField(t, rows, "•••••••••")
	assertPickerTitleSpacing(t, rows, "Connect OpenAI", inputRow)
	if titleColumn, _ := findTextCell(t, rows, "Connect OpenAI"); titleColumn != inputColumn {
		t.Fatalf("input column = %d, want title column %d:\n%s", inputColumn, titleColumn, strings.Join(rows, "\n"))
	}
	assertDialogRow(t, rows, "Connect OpenAI", "│ Connect OpenAI                                               │")
	assertDialogRow(t, rows, "•••", "│ •••••••••                                                    │")
	assertDialogRow(t, rows, "invalid API key", "│ invalid API key                                              │")
	if errorRow := findPaintedRow(rows, "invalid API key"); errorRow != inputRow+2 {
		t.Fatalf("error row = %d, want %d directly below the input divider:\n%s", errorRow, inputRow+2, strings.Join(rows, "\n"))
	}
	column, row := findTextCell(t, rows, "invalid API key")
	if got := application.Cell(column, row).Style.Foreground; got != pickerTheme.DangerText {
		t.Fatalf("error foreground = %v, want danger %v", got, pickerTheme.DangerText)
	}
	assertPickerFooter(t, rows, "enter save · esc back")
}

func TestAuthAPIKeyPromptShowsPlaceholderAndPendingSave(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		snapshot shellSnapshot
		title    string
		input    string
		footer   string
	}{
		{
			name:     "empty",
			snapshot: shellSnapshot{Phase: phaseAuthAPIKey, AuthProviderID: auth.AnthropicProviderID},
			title:    "│ Connect Anthropic                                            │",
			input:    "Paste API key…", footer: "│ enter save · esc back                                        │",
		},
		{
			// A save cannot be canceled once it starts, so the footer offers nothing.
			name:     "pending",
			snapshot: shellSnapshot{Phase: phaseAuthAPIKey, AuthProviderID: auth.AnthropicProviderID, AuthAPIKey: "abc", AuthPending: true},
			title:    "│ Connect Anthropic                                  ⠋ saving… │",
			input:    "•••", footer: "│                                                              │",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := uitest.New(shellView{Snapshot: test.snapshot})
			application.Pump(80, 24)
			rows := paintedRows(application, 80, 24)
			_, inputRow := assertPickerSearchField(t, rows, test.input)
			assertDialogRow(t, rows, "Connect Anthropic", test.title)
			left, right, _ := paletteBorder(rows)
			if got := string([]rune(rows[dialogBottom(rows)-1])[left : right+1]); got != test.footer {
				t.Fatalf("footer = %q, want %q:\n%s", got, test.footer, strings.Join(rows, "\n"))
			}
			if inputRow != findPaintedRow(rows, "Connect Anthropic")+2 {
				t.Fatalf("input row = %d, want two rows below the title:\n%s", inputRow, strings.Join(rows, "\n"))
			}
		})
	}
}

func mountAPIKeyPromptHarness() (*uitest.App, *authModalHarnessState) {
	state := newAuthModalHarnessState(true)
	state.phase, state.authProviderID = phaseAuthAPIKey, auth.OpenAIProviderID
	application := uitest.New(authModalHarness{State: state})
	application.Pump(80, 24)
	application.Pump(80, 24)
	return application, state
}

// TestAuthAPIKeyPromptEditsKeyThroughTheAppInputPath types, deletes, and
// pastes before the prompt's first frame; every edit reaches the key.
func TestAuthAPIKeyPromptEditsKeyThroughTheAppInputPath(t *testing.T) {
	t.Parallel()

	state := newAuthModalHarnessState(true)
	application := uitest.New(authModalHarness{State: state})
	application.Pump(80, 24)
	state.SetState(func() { state.phase, state.authProviderID = phaseAuthAPIKey, auth.OpenAIProviderID })

	application.Key("sk-x")
	application.Send(ui.Key{Keycode: vaxis.KeyBackspace})
	application.Key(" stray")
	application.Send(ui.Key{Keycode: vaxis.KeyBackspace, Modifiers: vaxis.ModCtrl})
	application.Send(ui.Key{Keycode: vaxis.KeyBackspace})
	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "secret", Keycode: 's', EventType: vaxis.EventPaste})
	application.Send(vaxis.PasteEndEvent{})
	if state.authAPIKey != "sk-secret" {
		t.Fatalf("API key before paint = %q, want %q", state.authAPIKey, "sk-secret")
	}
	application.Pump(80, 24)
	application.Pump(80, 24)
	assertDialogRow(t, paintedRows(application, 80, 24), "•••", "│ •••••••••                                                    │")
}

func TestAuthAPIKeyPromptEscapeReturnsToProviderPicker(t *testing.T) {
	t.Parallel()

	application, state := mountAPIKeyPromptHarness()
	application.Key("sk-secret")
	application.Send(ui.Key{Keycode: vaxis.KeyEsc, Text: "\x1b"})
	if state.phase != phaseAuthSelect || state.authAPIKey != "" || state.authProviderID != "" {
		t.Fatalf("Escape phase=%v key=%q provider=%q, want the provider picker with the key cleared", state.phase, state.authAPIKey, state.authProviderID)
	}
	application.Pump(80, 24)
	assertPickerSearchField(t, paintedRows(application, 80, 24), "Search providers…")
}

func TestAuthAPIKeyPromptPendingSaveConsumesKeys(t *testing.T) {
	t.Parallel()

	application, state := mountAPIKeyPromptHarness()
	state.SetState(func() { state.authAPIKey, state.authPending = "sk-secret", true })
	application.Pump(80, 24)
	application.Key("x")
	application.Send(ui.Key{Keycode: vaxis.KeyBackspace})
	application.Send(ui.Key{Keycode: vaxis.KeyEsc, Text: "\x1b"})
	application.Enter()
	if state.phase != phaseAuthAPIKey || state.authAPIKey != "sk-secret" || !state.authPending {
		t.Fatalf("pending save phase=%v key=%q pending=%t, want keys consumed without change", state.phase, state.authAPIKey, state.authPending)
	}
}

// TestAuthAPIKeyPromptEnterSubmitsKeyTypedBeforePaint selects a provider and
// types its key without an intervening frame, then saves it with Enter.
func TestAuthAPIKeyPromptEnterSubmitsKeyTypedBeforePaint(t *testing.T) {
	t.Parallel()

	calls := make(chan apiKeyLoginCall, 1)
	application := uitest.New(app{Options: Options{
		Context: context.Background(), Server: &fakeServer{}, CWD: "/repo",
		APIKeyLogin: fakeAPIKeyLogin{calls: calls},
	}})
	application.Pump(80, 24)
	application.Enter()
	application.Pump(80, 24)
	application.Key("opencode")
	application.Enter()
	application.Key("oc-")
	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "secret", Keycode: 's', EventType: vaxis.EventPaste})
	application.Send(vaxis.PasteEndEvent{})
	application.Enter()

	select {
	case call := <-calls:
		if call.providerID != auth.OpenCodeGoProviderID || call.apiKey != "oc-secret" {
			t.Fatalf("API-key login call = %#v", call)
		}
	case <-time.After(time.Second):
		t.Fatal("API-key login was not submitted")
	}
}
