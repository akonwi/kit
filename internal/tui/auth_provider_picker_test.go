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

const (
	authPickerCodexRow    = "│▌OpenAI Codex  ChatGPT plan · device code                     │"
	authPickerAnthropic   = "│ Anthropic     API key                                        │"
	authPickerOpenAI      = "│ OpenAI        API key                                        │"
	authPickerOpenCodeGo  = "│ OpenCode Go   API key                                        │"
	authPickerClaude      = "│ Claude        Pro or Max plan · browser                      │"
	authPickerClaudeFocus = "│▌Claude        Pro or Max plan · browser                      │"
)

func TestAuthProviderPickerUsesPalettePickerPresentation(t *testing.T) {
	t.Parallel()

	pickerTheme := ui.DefaultThemeSet().Dark
	application := uitest.New(ui.Provider[ui.Theme]{Value: pickerTheme, Child: shellView{Snapshot: shellSnapshot{
		Phase: phaseAuthSelect, AuthSelection: auth.OpenAICodexProviderID,
	}}})
	application.Pump(80, 24)
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	searchColumn, searchRow := assertPickerSearchField(t, rows, "Search providers…")
	assertPickerTitleSpacing(t, rows, "Connect a provider", searchRow)
	if titleColumn, _ := findTextCell(t, rows, "Connect a provider"); titleColumn != searchColumn {
		t.Fatalf("search column = %d, want title column %d:\n%s", searchColumn, titleColumn, strings.Join(rows, "\n"))
	}
	for index, want := range []string{authPickerCodexRow, authPickerAnthropic, authPickerOpenAI, authPickerOpenCodeGo, authPickerClaude} {
		left, right, _ := paletteBorder(rows)
		if got := string([]rune(rows[searchRow+2+index])[left : right+1]); got != want {
			t.Fatalf("provider row %d = %q, want %q:\n%s", index, got, want, strings.Join(rows, "\n"))
		}
	}
	assertPickerFooter(t, rows, "↑↓ move · enter select · esc close")

	column, row := findTextCell(t, rows, "OpenAI Codex")
	if got := application.Cell(column-1, row).Grapheme; got != glyphLeftBar {
		t.Fatalf("highlight gutter = %q, want %q", got, glyphLeftBar)
	}
	if got, want := application.Cell(column, row).Style.Background, blendPickerColor(pickerTheme.Selection, pickerTheme.Background, pickerHighlightPercent, pickerTheme.SurfaceHovered); got != want {
		t.Fatalf("highlighted provider background = %v, want tinted %v", got, want)
	}
	methodColumn, methodRow := findTextCell(t, rows, "Pro or Max plan · browser")
	if got, want := application.Cell(methodColumn, methodRow).Style.Foreground, pickerTheme.MutedForeground; got != want {
		t.Fatalf("method foreground = %v, want muted %v", got, want)
	}
}

func TestAuthProviderPickerPlacesLoginStateInFooterStatus(t *testing.T) {
	t.Parallel()

	pickerTheme := ui.DefaultThemeSet().Dark
	for _, test := range []struct {
		name     string
		snapshot shellSnapshot
		text     string
		row      string
		color    ui.Color
	}{
		{
			name:     "error",
			snapshot: shellSnapshot{Phase: phaseAuthSelect, AuthSelection: auth.OpenAICodexProviderID, Error: "device login expired"},
			text:     "device login expired", row: "│ device login expired                               esc close │", color: pickerTheme.DangerText,
		},
		{
			name:     "pending",
			snapshot: shellSnapshot{Phase: phaseAuthSelect, AuthSelection: auth.OpenAICodexProviderID, AuthPending: true},
			text:     "Connecting…", row: "│ Connecting…                                        esc close │", color: pickerTheme.MutedForeground,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := uitest.New(ui.Provider[ui.Theme]{Value: pickerTheme, Child: shellView{Snapshot: test.snapshot}})
			application.Pump(80, 24)
			rows := paintedRows(application, 80, 24)
			assertDialogRow(t, rows, test.text, test.row)
			if bottom := dialogBottom(rows); findPaintedRow(rows, test.text) != bottom-1 {
				t.Fatalf("status %q is not in the footer row %d:\n%s", test.text, bottom-1, strings.Join(rows, "\n"))
			}
			column, row := findTextCell(t, rows, test.text)
			if got := application.Cell(column, row).Style.Foreground; got != test.color {
				t.Fatalf("status foreground = %v, want %v", got, test.color)
			}
			assertDialogRow(t, rows, "OpenAI Codex", authPickerCodexRow)
		})
	}
}

func mountAuthModalHarness(returnReady bool) (*uitest.App, *authModalHarnessState) {
	state := newAuthModalHarnessState(returnReady)
	application := uitest.New(authModalHarness{State: state})
	application.Pump(80, 24)
	application.Pump(80, 24)
	return application, state
}

func assertAuthPickerRows(t *testing.T, application *uitest.App, want ...string) {
	t.Helper()
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	_, searchRow := findTextCell(t, rows, "Connect a provider")
	searchRow += 2
	left, right, _ := paletteBorder(rows)
	for index, expected := range want {
		if got := string([]rune(rows[searchRow+2+index])[left : right+1]); got != expected {
			t.Fatalf("provider row %d = %q, want %q:\n%s", index, got, expected, strings.Join(rows, "\n"))
		}
	}
	blank := "│" + strings.Repeat(" ", right-left-1) + "│"
	if got := string([]rune(rows[searchRow+2+len(want)])[left : right+1]); got != blank {
		t.Fatalf("row after the %d matches = %q, want an empty list row:\n%s", len(want), got, strings.Join(rows, "\n"))
	}
}

func TestAuthProviderPickerFiltersByNameAndMethod(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		query     string
		selection string
		rows      []string
	}{
		{query: "browser", selection: anthropicOAuthOptionID, rows: []string{authPickerClaudeFocus}},
		{query: "claude", selection: anthropicOAuthOptionID, rows: []string{authPickerClaudeFocus}},
		{query: "device", selection: auth.OpenAICodexProviderID, rows: []string{authPickerCodexRow}},
		{query: "anth", selection: auth.AnthropicProviderID, rows: []string{
			"│▌Anthropic     API key                                        │",
		}},
		{query: "api key", selection: auth.AnthropicProviderID, rows: []string{
			"│▌Anthropic     API key                                        │", authPickerOpenAI, authPickerOpenCodeGo,
		}},
	} {
		t.Run(test.query, func(t *testing.T) {
			application, state := mountAuthModalHarness(true)
			application.Key(test.query)
			if state.authPicker.Query != test.query || state.authPicker.Selection != test.selection {
				t.Fatalf("query=%q selection=%q, want %q highlighting %q", state.authPicker.Query, state.authPicker.Selection, test.query, test.selection)
			}
			assertAuthPickerRows(t, application, test.rows...)
			assertPickerSearchField(t, paintedRows(application, 80, 24), test.query)
		})
	}
}

func TestAuthProviderPickerArrowsWrapOverVisibleProviders(t *testing.T) {
	t.Parallel()

	application, state := mountAuthModalHarness(true)
	application.Send(ui.Key{Keycode: ui.KeyUp})
	if state.authPicker.Selection != anthropicOAuthOptionID {
		t.Fatalf("Up from the first provider selected %q, want %q", state.authPicker.Selection, anthropicOAuthOptionID)
	}
	application.Send(ui.Key{Keycode: ui.KeyDown})
	application.Send(ui.Key{Keycode: ui.KeyDown})
	if state.authPicker.Selection != auth.AnthropicProviderID {
		t.Fatalf("Down twice selected %q, want %q", state.authPicker.Selection, auth.AnthropicProviderID)
	}
	assertAuthPickerRows(t, application,
		"│ OpenAI Codex  ChatGPT plan · device code                     │",
		"│▌Anthropic     API key                                        │",
		authPickerOpenAI, authPickerOpenCodeGo, authPickerClaude,
	)
}

func TestAuthProviderPickerEnterAndClickSelectProviderOption(t *testing.T) {
	t.Parallel()

	t.Run("enter", func(t *testing.T) {
		application, state := mountAuthModalHarness(true)
		application.Send(ui.Key{Keycode: ui.KeyDown})
		application.Enter()
		if state.phase != phaseAuthAPIKey || state.authProviderID != auth.AnthropicProviderID {
			t.Fatalf("Enter phase=%v provider=%q, want the Anthropic API-key step", state.phase, state.authProviderID)
		}
	})
	t.Run("enter after filtering", func(t *testing.T) {
		application, state := mountAuthModalHarness(true)
		application.Key("opencode")
		application.Enter()
		if state.phase != phaseAuthAPIKey || state.authProviderID != auth.OpenCodeGoProviderID {
			t.Fatalf("Enter phase=%v provider=%q, want the OpenCode Go API-key step", state.phase, state.authProviderID)
		}
	})
	t.Run("enter without matches", func(t *testing.T) {
		application, state := mountAuthModalHarness(true)
		application.Key("zzz")
		application.Enter()
		if state.phase != phaseAuthSelect || state.errorText != "" {
			t.Fatalf("Enter without matches phase=%v error=%q, want the picker unchanged", state.phase, state.errorText)
		}
	})
	t.Run("click", func(t *testing.T) {
		application, state := mountAuthModalHarness(true)
		column, row := findTextCell(t, paintedRows(application, 80, 24), "OpenAI        API key")
		application.Click(column+20, row)
		if state.phase != phaseAuthAPIKey || state.authProviderID != auth.OpenAIProviderID || state.authPicker.Selection != auth.OpenAIProviderID {
			t.Fatalf("click phase=%v provider=%q selection=%q, want the OpenAI API-key step", state.phase, state.authProviderID, state.authPicker.Selection)
		}
	})
}

// TestAuthProviderPickerKeepsAnthropicOptionsDistinct selects both options
// that connect the anthropic provider: the API key and the Claude plan.
func TestAuthProviderPickerKeepsAnthropicOptionsDistinct(t *testing.T) {
	t.Parallel()

	application, state := mountAuthModalHarness(true)
	column, row := findTextCell(t, paintedRows(application, 80, 24), "Anthropic")
	application.Click(column, row)
	if state.phase != phaseAuthAPIKey || state.authProviderID != auth.AnthropicProviderID {
		t.Fatalf("Anthropic click phase=%v provider=%q, want the Anthropic API-key step", state.phase, state.authProviderID)
	}

	codes := make(chan string, 1)
	ready := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	full := uitest.New(app{Options: Options{
		Context: ctx, Server: &fakeServer{}, CWD: "/repo",
		BrowserLogin: fakeBrowserLogin{codes: codes, ready: ready},
	}})
	full.Pump(80, 24)
	full.Enter()
	full.Pump(80, 24)
	full.Key("browser")
	full.Enter()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("Claude browser login did not start")
	}
	full.Pump(80, 24)
	rows := paintedRows(full, 80, 24)
	if findPaintedRow(rows, "Claude Pro or Max") < 0 {
		t.Fatalf("Enter on Claude did not open the browser step:\n%s", strings.Join(rows, "\n"))
	}
}

func TestAuthProviderPickerEscapeDismissesToOpeningSurface(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		returnReady bool
		want        phase
	}{
		{name: "logged in", returnReady: true, want: phaseReady},
		{name: "first run", returnReady: false, want: phaseAuthGate},
	} {
		t.Run(test.name, func(t *testing.T) {
			application, state := mountAuthModalHarness(test.returnReady)
			application.Key("api")
			state.SetState(func() { state.errorText = "login failed" })
			application.Send(ui.Key{Keycode: vaxis.KeyEsc, Text: "\x1b"})
			if state.phase != test.want || state.authReturnReady || state.errorText != "" || state.authPicker.Query != "" || state.authPicker.Selection != "" {
				t.Fatalf("Escape phase=%v returnReady=%t error=%q picker=%+v, want %v with the picker reset",
					state.phase, state.authReturnReady, state.errorText, state.authPicker, test.want)
			}
		})
	}
}

func TestAuthProviderPickerPendingLoginOnlyDismisses(t *testing.T) {
	t.Parallel()

	application, state := mountAuthModalHarness(true)
	state.SetState(func() { state.authPending = true })
	application.Pump(80, 24)
	application.Key("api")
	application.Send(ui.Key{Keycode: ui.KeyDown})
	application.Enter()
	if state.phase != phaseAuthSelect || state.authPicker.Query != "" || state.authPicker.Selection != auth.OpenAICodexProviderID {
		t.Fatalf("pending picker phase=%v picker=%+v, want keys consumed without change", state.phase, state.authPicker)
	}
	application.Send(ui.Key{Keycode: vaxis.KeyEsc, Text: "\x1b"})
	if state.phase != phaseReady {
		t.Fatalf("pending Escape phase = %v, want ready", state.phase)
	}
}
