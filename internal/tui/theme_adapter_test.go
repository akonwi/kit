package tui

import (
	"strings"
	"testing"

	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

var adapterHarnessColor = kittheme.Color{R: 1, G: 2, B: 3, A: 255}

type themeAdapterHarness struct{}

func (themeAdapterHarness) Build(ctx ui.BuildContext) ui.Widget {
	control := ui.MustDepend[ThemeControl](ctx)
	theme := ui.MustDepend[ui.Theme](ctx)
	label := "system"
	if theme.Background == ui.RGB(1, 2, 3) {
		label = "custom"
	}
	return ui.ListTile{Title: ui.Text{Value: label}, OnPressed: func(ui.EventContext) {
		control.Apply(kittheme.Definition{Tokens: map[string]kittheme.Color{
			kittheme.TokenBackground: adapterHarnessColor,
		}})
	}}
}

func TestThemeAdapterControlAppliesPreview(t *testing.T) {
	t.Parallel()

	application := uitest.New(themeAdapter{Child: themeAdapterHarness{}})
	application.Pump(30, 5)
	if text := strings.Join(paintedRows(application, 30, 5), "\n"); !strings.Contains(text, "system") {
		t.Fatalf("initial render = %q", text)
	}
	application.Click(0, 0)
	application.Pump(30, 5)
	if text := strings.Join(paintedRows(application, 30, 5), "\n"); !strings.Contains(text, "custom") {
		t.Fatalf("preview render = %q", text)
	}
}

func TestSemanticFallbackContainsEveryCompatibleRole(t *testing.T) {
	t.Parallel()

	fallback := semanticFallback(ui.DefaultTheme())
	for _, role := range kittheme.TokenRoles() {
		if _, ok := fallback.Tokens[role]; !ok {
			t.Errorf("token fallback %q is missing", role)
		}
	}
	for _, role := range kittheme.SyntaxRoles() {
		if _, ok := fallback.SyntaxPalette[role]; !ok {
			t.Errorf("syntax fallback %q is missing", role)
		}
	}
}

func TestProjectThemeWithoutOverridesPreservesVaxisTheme(t *testing.T) {
	t.Parallel()

	base := ui.DefaultTheme()
	base.Background = 0
	projected, _ := projectTheme(base, kittheme.Definition{})
	if projected != base {
		t.Fatalf("projectTheme() changed system theme without overrides\n got: %#v\nwant: %#v", projected, base)
	}
}

func TestProjectThemeMapsVaxisAndSemanticRoles(t *testing.T) {
	t.Parallel()

	base := ui.DefaultTheme()
	definition := kittheme.Definition{
		Tokens: map[string]kittheme.Color{
			kittheme.TokenBackground:                {R: 1, G: 2, B: 3, A: 255},
			kittheme.TokenTextPrimary:               {R: 4, G: 5, B: 6, A: 255},
			kittheme.TokenBorderDefault:             {R: 7, G: 8, B: 9, A: 255},
			kittheme.TokenDiffCursorAddedBackground: {R: 10, G: 11, B: 12, A: 255},
			kittheme.TokenPickerFocusedBackground:   {R: 19, G: 20, B: 21, A: 255},
			kittheme.TokenToolText:                  {R: 22, G: 23, B: 24, A: 255},
			kittheme.TokenErrorText:                 {R: 25, G: 26, B: 27, A: 255},
			kittheme.TokenDiffRemovedBackground:     {R: 28, G: 29, B: 30, A: 255},
			kittheme.TokenBackgroundAccent:          {R: 31, G: 32, B: 33, A: 255},
			"futureRole":                            {R: 13, G: 14, B: 15, A: 255},
		},
		SyntaxPalette: map[string]kittheme.Color{
			kittheme.SyntaxKeyword: {R: 16, G: 17, B: 18, A: 255},
		},
	}
	projected, semantic := projectTheme(base, definition)
	if projected.Background != ui.RGB(1, 2, 3) {
		t.Fatalf("Background = %v", projected.Background)
	}
	if projected.Foreground != ui.RGB(4, 5, 6) {
		t.Fatalf("Foreground = %v", projected.Foreground)
	}
	if projected.Border != ui.RGB(7, 8, 9) {
		t.Fatalf("Border = %v", projected.Border)
	}
	if projected.Primary != ui.RGB(19, 20, 21) || projected.Selection != projected.Primary {
		t.Fatalf("picker fills = primary:%v selection:%v", projected.Primary, projected.Selection)
	}
	if projected.SuccessText != ui.RGB(22, 23, 24) {
		t.Fatalf("SuccessText = %v", projected.SuccessText)
	}
	if projected.DangerText != ui.RGB(25, 26, 27) || projected.Danger != ui.RGB(28, 29, 30) {
		t.Fatalf("danger colors = text:%v fill:%v", projected.DangerText, projected.Danger)
	}
	if projected.Accent != ui.RGB(31, 32, 33) || projected.PrimaryPressed != projected.Accent {
		t.Fatalf("accent fills = accent:%v primary pressed:%v", projected.Accent, projected.PrimaryPressed)
	}
	if got := semantic.Tokens[kittheme.TokenDiffCursorAddedBackground]; got != ui.RGB(10, 11, 12) {
		t.Fatalf("diff role = %#v", got)
	}
	if got := semantic.Tokens["futureRole"]; got != ui.RGB(13, 14, 15) {
		t.Fatalf("future role = %#v", got)
	}
	if got := semantic.SyntaxPalette[kittheme.SyntaxKeyword]; got != ui.RGB(16, 17, 18) {
		t.Fatalf("syntax keyword = %#v", got)
	}
}

func TestProjectThemeCompositesAlphaAgainstRenderedSurfaces(t *testing.T) {
	t.Parallel()

	definition := kittheme.Definition{Tokens: map[string]kittheme.Color{
		kittheme.TokenBackground:                 {R: 10, G: 20, B: 30, A: 255},
		kittheme.TokenBackgroundSurface:          {R: 255, G: 255, B: 255, A: 128},
		kittheme.TokenErrorText:                  {R: 210, G: 120, B: 40, A: 128},
		kittheme.TokenDiffAddedBackground:        {R: 30, G: 200, B: 70, A: 128},
		kittheme.TokenDiffAddedContentBackground: {R: 200, G: 220, B: 240, A: 128},
	}}
	_, semantic := projectTheme(ui.DefaultTheme(), definition)
	if got := semantic.Token(kittheme.TokenBackgroundSurface); got != ui.RGB(133, 138, 143) {
		t.Fatalf("surface = %v, want %v", got, ui.RGB(133, 138, 143))
	}
	if got := semantic.Token(kittheme.TokenErrorText); got != ui.RGB(110, 70, 35) {
		t.Fatalf("error text = %v, want %v", got, ui.RGB(110, 70, 35))
	}
	if got := semantic.Token(kittheme.TokenDiffAddedContentBackground); got != ui.RGB(110, 165, 145) {
		t.Fatalf("diff content = %v, want %v", got, ui.RGB(110, 165, 145))
	}
}

func TestProjectColorCompositesPartialAlpha(t *testing.T) {
	t.Parallel()

	got := projectColor(kittheme.Color{R: 200, G: 100, B: 50, A: 128}, ui.RGB(20, 40, 60))
	if got != ui.RGB(110, 70, 55) {
		t.Fatalf("projectColor() = %v, want %v", got, ui.RGB(110, 70, 55))
	}
}

func TestProjectColorTreatsFullTransparencyAsInheritance(t *testing.T) {
	t.Parallel()

	fallback := ui.RGB(20, 40, 60)
	if got := projectColor(kittheme.Color{}, fallback); got != fallback {
		t.Fatalf("projectColor() = %v, want %v", got, fallback)
	}
}
