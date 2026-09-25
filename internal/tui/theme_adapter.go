package tui

import (
	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis/ui"
)

// SemanticTheme exposes Kit roles that are not represented by ui.Theme.
type SemanticTheme struct {
	Tokens        map[string]ui.Color
	SyntaxPalette map[string]ui.Color
}

// Token returns a resolved semantic token.
func (t SemanticTheme) Token(role string) ui.Color { return t.Tokens[role] }

// Syntax returns a resolved syntax color.
func (t SemanticTheme) Syntax(role string) ui.Color { return t.SyntaxPalette[role] }

// ThemeControl applies an in-memory definition to the mounted theme provider.
// Persistence remains the responsibility of the application composition root.
type ThemeControl struct {
	Apply func(kittheme.Definition)
}

type themeAdapter struct {
	Definition kittheme.Definition
	Child      ui.Widget
}

func (w themeAdapter) CreateState() ui.State { return &themeAdapterState{} }

type themeAdapterState struct {
	ui.StateBase
	definition kittheme.Definition
}

func (s *themeAdapterState) InitState() {
	s.definition = s.Widget().(themeAdapter).Definition
}

func (s *themeAdapterState) Build(ctx ui.BuildContext) ui.Widget {
	w := s.Widget().(themeAdapter)
	projected, semantic := projectTheme(ui.MustDepend[ui.Theme](ctx), s.definition)
	control := ThemeControl{Apply: func(definition kittheme.Definition) {
		s.SetState(func() { s.definition = definition })
	}}
	return ui.Provider[ui.Theme]{
		Value: projected,
		Child: ui.Provider[SemanticTheme]{
			Value: semantic,
			Child: ui.Provider[ThemeControl]{Value: control, Child: w.Child},
		},
	}
}

func projectTheme(base ui.Theme, definition kittheme.Definition) (ui.Theme, SemanticTheme) {
	semantic := semanticFallback(base)
	for _, role := range kittheme.TokenRoles() {
		if color, ok := definition.Tokens[role]; ok {
			semantic.Tokens[role] = projectColor(color, tokenBackdrop(role, base, semantic))
		}
	}
	for role, color := range definition.Tokens {
		if !kittheme.IsKnownToken(role) {
			semantic.Tokens[role] = projectColor(color, semantic.Token(kittheme.TokenBackground))
		}
	}
	for role, color := range definition.SyntaxPalette {
		semantic.SyntaxPalette[role] = projectColor(color, semantic.Token(kittheme.TokenBackground))
	}
	result := base
	apply := func(target *ui.Color, role string) {
		if _, ok := definition.Tokens[role]; ok {
			*target = semantic.Token(role)
		}
	}
	apply(&result.Background, kittheme.TokenBackground)
	apply(&result.Foreground, kittheme.TokenTextPrimary)
	apply(&result.Surface, kittheme.TokenBackgroundSurface)
	apply(&result.SurfaceRaised, kittheme.TokenPickerBackground)
	apply(&result.SurfaceHovered, kittheme.TokenBackgroundMuted)
	apply(&result.SurfacePressed, kittheme.TokenBackgroundAccent)
	apply(&result.Primary, kittheme.TokenPickerFocusedBackground)
	apply(&result.PrimaryText, kittheme.TokenBorderAccent)
	apply(&result.PrimaryHovered, kittheme.TokenPickerFocusedBackground)
	apply(&result.PrimaryPressed, kittheme.TokenBackgroundAccent)
	apply(&result.Accent, kittheme.TokenBackgroundAccent)
	apply(&result.AccentText, kittheme.TokenBorderAccent)
	apply(&result.Success, kittheme.TokenDiffAddedBackground)
	apply(&result.SuccessText, kittheme.TokenToolText)
	apply(&result.WarningText, kittheme.TokenWarningText)
	apply(&result.Danger, kittheme.TokenDiffRemovedBackground)
	apply(&result.DangerText, kittheme.TokenErrorText)
	apply(&result.MutedForeground, kittheme.TokenTextMuted)
	apply(&result.DisabledForeground, kittheme.TokenTextPlaceholder)
	apply(&result.Selection, kittheme.TokenPickerFocusedBackground)
	apply(&result.Border, kittheme.TokenBorderDefault)
	return result, semantic
}

func tintUIColor(base, accent ui.Color, opacity float64) ui.Color {
	baseRGB, accentRGB := base.Params(), accent.Params()
	if len(baseRGB) != 3 || len(accentRGB) != 3 {
		return base
	}
	mix := func(from, to uint8) uint8 {
		return uint8(float64(from)*(1-opacity) + float64(to)*opacity + 0.5)
	}
	return ui.RGB(mix(baseRGB[0], accentRGB[0]), mix(baseRGB[1], accentRGB[1]), mix(baseRGB[2], accentRGB[2]))
}

func semanticFallback(base ui.Theme) SemanticTheme {
	tokens := make(map[string]ui.Color, len(kittheme.TokenRoles()))
	set := func(color ui.Color, roles ...string) {
		for _, role := range roles {
			tokens[role] = color
		}
	}
	set(base.Background, kittheme.TokenBackground, kittheme.TokenBackgroundTransparent)
	set(base.Surface, kittheme.TokenBackgroundSurface)
	set(base.SurfaceRaised, kittheme.TokenPickerBackground)
	set(base.SurfaceHovered, kittheme.TokenBackgroundMuted, kittheme.TokenPickerBorder,
		kittheme.TokenPickerScrollTrack, kittheme.TokenScrollbarBackground)
	set(base.SurfacePressed, kittheme.TokenBackgroundAccent, kittheme.TokenScrollbarForeground,
		kittheme.TokenDiffCursorGutterBackground)
	set(base.Border, kittheme.TokenBorderDefault, kittheme.TokenBorderStatus)
	set(base.MutedForeground, kittheme.TokenBorderFocused, kittheme.TokenTextSecondary,
		kittheme.TokenTextMuted, kittheme.TokenTextDebug, kittheme.TokenPickerScrollThumb,
		kittheme.TokenPanelText)
	set(base.PrimaryText, kittheme.TokenBorderAccent, kittheme.TokenUserText,
		kittheme.TokenUserBorder, kittheme.TokenProgressNormal)
	set(base.AccentText, kittheme.TokenBorderDebug, kittheme.TokenReviewText,
		kittheme.TokenSubagentText, kittheme.TokenDebugLabel, kittheme.TokenMetaText)
	set(base.SuccessText, kittheme.TokenComposerBashBorder, kittheme.TokenComposerBashExcludedBorder,
		kittheme.TokenToolText)
	set(base.Foreground, kittheme.TokenTextPrimary, kittheme.TokenAssistantText,
		kittheme.TokenCursor, kittheme.TokenPickerItemText)
	set(base.DisabledForeground, kittheme.TokenTextPlaceholder)
	set(base.PrimaryHovered, kittheme.TokenUserTextFocused)
	set(base.DangerText, kittheme.TokenErrorText, kittheme.TokenAttachmentText,
		kittheme.TokenProgressCritical)
	set(base.WarningText, kittheme.TokenWarningText, kittheme.TokenProgressWarning)
	set(base.Selection, kittheme.TokenPickerFocusedBackground, kittheme.TokenDiffCursorBackground)
	set(base.Background, kittheme.TokenPickerFocusedText)
	set(base.Primary, kittheme.TokenToggleOn)
	set(tintUIColor(base.Background, base.Success, 0.20), kittheme.TokenDiffAddedBackground)
	set(tintUIColor(base.Background, base.Danger, 0.20), kittheme.TokenDiffRemovedBackground)
	set(tintUIColor(base.Background, base.Success, 0.20), kittheme.TokenDiffAddedContentBackground)
	set(tintUIColor(base.Background, base.Danger, 0.20), kittheme.TokenDiffRemovedContentBg)
	set(tintUIColor(base.Background, base.Success, 0.08), kittheme.TokenDiffAddedLineNumberBg)
	set(tintUIColor(base.Background, base.Danger, 0.08), kittheme.TokenDiffRemovedLineNumberBg)
	set(tintUIColor(base.Background, base.Success, 0.24), kittheme.TokenDiffCursorAddedBackground)
	set(tintUIColor(base.Background, base.Danger, 0.24), kittheme.TokenDiffCursorRemovedBg)

	syntax := make(map[string]ui.Color, len(kittheme.SyntaxRoles()))
	setSyntax := func(color ui.Color, roles ...string) {
		for _, role := range roles {
			syntax[role] = color
		}
	}
	setSyntax(base.Foreground, kittheme.SyntaxText, kittheme.SyntaxBold, kittheme.SyntaxCodeBlock,
		kittheme.SyntaxOperator, kittheme.SyntaxVariable)
	setSyntax(base.PrimaryText, kittheme.SyntaxHeading, kittheme.SyntaxLink, kittheme.SyntaxList,
		kittheme.SyntaxFunction, kittheme.SyntaxMember, kittheme.SyntaxLabel)
	setSyntax(base.WarningText, kittheme.SyntaxItalic, kittheme.SyntaxQuote, kittheme.SyntaxNumber,
		kittheme.SyntaxKeywordType, kittheme.SyntaxType, kittheme.SyntaxAttribute)
	setSyntax(base.SuccessText, kittheme.SyntaxCodeInline, kittheme.SyntaxString)
	setSyntax(base.MutedForeground, kittheme.SyntaxStrikethrough, kittheme.SyntaxComment,
		kittheme.SyntaxPunctuation, kittheme.SyntaxTagDelimiter)
	setSyntax(base.DisabledForeground, kittheme.SyntaxConceal)
	setSyntax(base.AccentText, kittheme.SyntaxEscape, kittheme.SyntaxKeyword, kittheme.SyntaxTagAttribute)
	setSyntax(base.DangerText, kittheme.SyntaxBuiltin, kittheme.SyntaxTag)
	return SemanticTheme{Tokens: tokens, SyntaxPalette: syntax}
}

func tokenBackdrop(role string, base ui.Theme, semantic SemanticTheme) ui.Color {
	switch role {
	case kittheme.TokenBackground:
		return base.Background
	case kittheme.TokenPickerFocusedText:
		return semantic.Token(kittheme.TokenPickerFocusedBackground)
	case kittheme.TokenPickerBorder, kittheme.TokenPickerItemText,
		kittheme.TokenPickerScrollThumb, kittheme.TokenPickerScrollTrack:
		return semantic.Token(kittheme.TokenPickerBackground)
	case kittheme.TokenDiffAddedContentBackground, kittheme.TokenDiffAddedLineNumberBg,
		kittheme.TokenDiffCursorAddedBackground:
		return semantic.Token(kittheme.TokenDiffAddedBackground)
	case kittheme.TokenDiffRemovedContentBg, kittheme.TokenDiffRemovedLineNumberBg,
		kittheme.TokenDiffCursorRemovedBg:
		return semantic.Token(kittheme.TokenDiffRemovedBackground)
	default:
		return semantic.Token(kittheme.TokenBackground)
	}
}

func projectColor(color kittheme.Color, fallback ui.Color) ui.Color {
	if color.A == 0 {
		return fallback
	}
	if color.A == 255 {
		return ui.RGB(color.R, color.G, color.B)
	}
	params := fallback.Params()
	if len(params) != 3 {
		return ui.RGB(color.R, color.G, color.B)
	}
	alpha := uint32(color.A)
	blend := func(foreground, background uint8) uint8 {
		return uint8((uint32(foreground)*alpha + uint32(background)*(255-alpha) + 127) / 255)
	}
	return ui.RGB(blend(color.R, params[0]), blend(color.G, params[1]), blend(color.B, params[2]))
}
