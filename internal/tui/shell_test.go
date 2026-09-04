package tui

import (
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
		Status:        "Working…",
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
	if !strings.Contains(rows[height-1], "Working…") {
		t.Fatalf("footer left = %q, want status", rows[height-1])
	}
	if !strings.HasSuffix(strings.TrimSpace(rows[height-1]), "~/Developer/agent/kit-v2 (kit-v2)") {
		t.Fatalf("footer right = %q, want cwd and git", rows[height-1])
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
		"↑ up · ↓ down · Enter select · Esc close",
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
	if !strings.Contains(text, "Saving…") || strings.Contains(text, "Esc cancel") || strings.Contains(text, "Esc back") {
		t.Fatalf("pending API-key footer offers misleading cancellation:\n%s", text)
	}
	app.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if dismissed {
		t.Fatal("Escape dismissed API-key save after commit started")
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
