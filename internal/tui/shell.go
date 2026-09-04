package tui

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

type shellSnapshot struct {
	Phase          phase
	Error          string
	Status         string
	Composer       string
	AuthFilter     string
	AuthSelection  int
	AuthProviderID string
	AuthAPIKey     string
	AuthPending    bool
	Session        protocol.SessionInfo
	Messages       []transcriptMessage
	Running        bool
	ContextTokens  int
	ContextWindow  int
	Scroll         *ui.ScrollController
	Instructions   auth.OpenAICodexDeviceInstructions
	Remaining      time.Duration
	Location       string
}

type providerSelectedCallback func(ui.EventContext, string)
type selectionMovedCallback func(ui.EventContext, int)

type shellCallbacks struct {
	OpenAuth              ui.VoidCallback
	SelectProvider        providerSelectedCallback
	MoveProviderSelection selectionMovedCallback
	AuthFilterChanged     ui.TextChangedCallback
	AuthAPIKeyChanged     ui.TextChangedCallback
	SubmitAPIKey          ui.TextChangedCallback
	OpenURL               ui.TextChangedCallback
	CopyCode              ui.VoidCallback
	ComposerChanged       ui.TextChangedCallback
	Submit                ui.TextChangedCallback
	Retry                 ui.VoidCallback
	Quit                  ui.VoidCallback
	Dismiss               ui.VoidCallback
}

type shellView struct {
	Snapshot  shellSnapshot
	Callbacks shellCallbacks
}

type quitIntent struct{}

func (quitIntent) IntentType() ui.IntentType { return "kit.quit" }

type copyCodeIntent struct{}

func (copyCodeIntent) IntentType() ui.IntentType { return "kit.auth.copy-code" }

type retryIntent struct{}

func (retryIntent) IntentType() ui.IntentType { return "kit.retry" }

type moveProviderIntent struct{ Delta int }

func (moveProviderIntent) IntentType() ui.IntentType { return "kit.auth.move-provider" }

func (w shellView) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	content := w.baseShell(theme)
	overlays := w.authOverlays(theme)
	root := ui.Widget(ui.Overlay{Child: content, Entries: overlays})
	root = ui.SelectionArea{Child: root}

	actions := map[ui.IntentType]ui.ActionFunc{
		quitIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.Quit != nil {
				w.Callbacks.Quit(ctx)
			}
			return ui.EventHandled
		},
	}
	shortcuts := ui.ShortcutMap{"Ctrl+c": quitIntent{}}
	if w.Snapshot.Phase == phaseAuthSelect || w.Snapshot.Phase == phaseAuthWaiting ||
		(w.Snapshot.Phase == phaseAuthAPIKey && !w.Snapshot.AuthPending) ||
		(w.Snapshot.Phase == phaseReady && w.Snapshot.Running) {
		actions[ui.DismissIntentType] = func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.Dismiss != nil {
				w.Callbacks.Dismiss(ctx)
			}
			return ui.EventHandled
		}
	}
	if w.Snapshot.Phase == phaseAuthSelect {
		shortcuts["Up"] = moveProviderIntent{Delta: -1}
		shortcuts["Down"] = moveProviderIntent{Delta: 1}
		actions[moveProviderIntent{}.IntentType()] = func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
			if w.Callbacks.MoveProviderSelection != nil {
				w.Callbacks.MoveProviderSelection(ctx, intent.(moveProviderIntent).Delta)
			}
			return ui.EventHandled
		}
	}
	if w.Snapshot.Phase == phaseAuthWaiting && w.Snapshot.Instructions.UserCode != "" {
		shortcuts["c"] = copyCodeIntent{}
		actions[copyCodeIntent{}.IntentType()] = func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.CopyCode != nil {
				w.Callbacks.CopyCode(ctx)
			}
			return ui.EventHandled
		}
	}
	if w.Snapshot.Phase == phaseFailed {
		shortcuts["r"] = retryIntent{}
		actions[retryIntent{}.IntentType()] = func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.Retry != nil {
				w.Callbacks.Retry(ctx)
			}
			return ui.EventHandled
		}
	}
	return ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}},
		ui.Actions{Bindings: actions, Child: ui.Shortcuts{Bindings: shortcuts, Child: root}},
	)
}

func (w shellView) baseShell(theme ui.Theme) ui.Widget {
	children := []ui.Widget{
		w.header(theme),
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		ui.Expanded(w.body(theme)),
	}
	if w.Snapshot.Phase == phaseReady {
		children = append(children,
			ui.Divider{Style: ui.Style{Foreground: theme.Border}},
			w.composer(theme),
		)
	}
	children = append(children,
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		w.footer(theme),
	)
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
}

func (w shellView) header(theme ui.Theme) ui.Widget {
	left := "kit"
	right := []ui.TextSpan(nil)
	if w.Snapshot.Phase == phaseReady {
		left = sessionDisplayName(w.Snapshot.Session)
		right = modelInformation(w.Snapshot, theme)
	}
	return ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Flex{
		Axis:               ui.Horizontal,
		CrossAxisAlignment: ui.CrossAxisStretch,
		Children: []ui.Widget{
			ui.ExpandedWidget{Flex: 1, Child: ui.Text{Value: left, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}},
			ui.ExpandedWidget{Flex: 2, Child: ui.RichText{Spans: right, Overflow: ui.TextOverflowEllipsis, MaxLines: 1, Align: ui.TextAlignRight}},
		},
	})}
}

func (w shellView) body(theme ui.Theme) ui.Widget {
	switch w.Snapshot.Phase {
	case phaseReady:
		return w.transcript(theme)
	case phaseFailed:
		return ui.Center(ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			ui.Text{Value: "Kit could not start", Style: ui.Style{Foreground: theme.DangerText, Attribute: ui.AttrBold}},
			ui.SizedBox{Height: 1},
			ui.ConstrainedBox{Constraints: ui.Constraints{MaxWidth: 72}, Child: ui.Text{Value: w.Snapshot.Error, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true}},
			ui.SizedBox{Height: 1},
			ui.Text{Value: "r retry · ctrl+c quit", Style: ui.Style{Foreground: theme.MutedForeground}},
		}})
	case phaseLoading:
		return emptyState(theme, "Starting Kit…", "")
	default:
		return ui.FocusScope{AutoFocus: true, Child: ui.Center(ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			wordmark(theme),
			ui.SizedBox{Height: 1},
			ui.Text{Value: "Connect an AI provider to get started.", Style: ui.Style{Foreground: theme.MutedForeground}},
			ui.SizedBox{Height: 1},
			ui.Button{Label: "Connect a provider", OnPressed: w.Callbacks.OpenAuth},
		}})}
	}
}

func (w shellView) transcript(theme ui.Theme) ui.Widget {
	if len(w.Snapshot.Messages) == 0 {
		return emptyState(theme, "Ask a question or give a task.", "")
	}
	children := make([]ui.Widget, 0, len(w.Snapshot.Messages)*2)
	for index, message := range w.Snapshot.Messages {
		if index > 0 {
			children = append(children, ui.SizedBox{Height: 1})
		}
		children = append(children, transcriptEntry(theme, message))
	}
	return ui.Scrollbar{Child: ui.ScrollView{
		Controller: w.Snapshot.Scroll,
		Child:      ui.Padding(ui.All(1), ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}),
	}}
}

func transcriptEntry(theme ui.Theme, message transcriptMessage) ui.Widget {
	style := ui.Style{Foreground: theme.Foreground}
	if message.Role == "error" {
		style.Foreground = theme.DangerText
	}
	content := ui.Text{Value: message.Text, Style: style, SoftWrap: true}
	if message.Role != "user" {
		return content
	}
	return ui.DecoratedBox(
		ui.Decoration{Border: ui.Border{Style: ui.Style{Foreground: theme.PrimaryText}, Left: true}},
		ui.Padding(ui.Insets{Left: 2}, content),
	)
}

func (w shellView) composer(theme ui.Theme) ui.Widget {
	composerTheme := theme
	composerTheme.Surface = theme.Background
	composerTheme.SurfaceHovered = theme.Background
	composerTheme.Selection = theme.Selection
	return ui.SizedBox{Height: 1, Child: ui.Provider[ui.Theme]{Value: composerTheme, Child: ui.TextField{
		Value:       w.Snapshot.Composer,
		Placeholder: "Ask kit to do something…",
		OnChanged:   w.Callbacks.ComposerChanged,
		OnSubmitted: w.Callbacks.Submit,
		Padding:     ui.Symmetric(1, 0),
		AutoFocus:   true,
	}}}
}

func (w shellView) footer(theme ui.Theme) ui.Widget {
	left := w.Snapshot.Status
	if w.Snapshot.Phase == phaseAuthGate || w.Snapshot.Phase == phaseAuthSelect {
		left = "enter connect · ctrl+c quit"
	}
	if w.Snapshot.Phase == phaseFailed {
		left = "r retry · ctrl+c quit"
	}
	return ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Flex{
		Axis:               ui.Horizontal,
		CrossAxisAlignment: ui.CrossAxisStretch,
		Children: []ui.Widget{
			ui.ExpandedWidget{Flex: 1, Child: ui.Text{Value: left, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}},
			ui.ExpandedWidget{Flex: 2, Child: ui.Text{Value: w.Snapshot.Location, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1, Align: ui.TextAlignRight}},
		},
	})}
}

func (w shellView) authOverlays(theme ui.Theme) []ui.OverlayEntry {
	switch w.Snapshot.Phase {
	case phaseAuthSelect:
		return []ui.OverlayEntry{{Child: dialogSurface(
			theme,
			"Connect a provider",
			"",
			w.providerSelectionBody(theme),
			ui.Text{Value: "↑ up · ↓ down · Enter select · Esc close", Style: ui.Style{Foreground: theme.MutedForeground}},
			false,
		)}}
	case phaseAuthWaiting:
		return []ui.OverlayEntry{{Child: dialogSurface(
			theme,
			"Complete login",
			"OpenAI Codex",
			ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: w.deviceLoginBody(theme)},
			ui.Text{Value: "C copy code · Esc cancel", Style: ui.Style{Foreground: theme.MutedForeground}},
			true,
		)}}
	case phaseAuthAPIKey:
		provider, _ := authProviderByID(w.Snapshot.AuthProviderID)
		footer := "Enter save · Esc back"
		if w.Snapshot.AuthPending {
			footer = "Saving…"
		}
		return []ui.OverlayEntry{{Child: dialogSurface(
			theme,
			"Connect "+provider.Name,
			"",
			w.apiKeyBody(theme),
			ui.Text{Value: footer, Style: ui.Style{Foreground: theme.MutedForeground}},
			false,
		)}}
	default:
		return nil
	}
}

func (w shellView) providerSelectionBody(theme ui.Theme) ui.Widget {
	fieldTheme := theme
	fieldTheme.Surface = theme.Background
	fieldTheme.SurfaceHovered = theme.Background
	children := []ui.Widget{}
	if w.Snapshot.Error != "" {
		children = append(children,
			ui.Text{Value: w.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true},
			ui.SizedBox{Height: 1},
		)
	}
	children = append(children,
		ui.Text{Value: "Filter providers", Style: ui.Style{Foreground: theme.MutedForeground}},
		ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			ui.Text{Value: ">", Style: ui.Style{Foreground: theme.Foreground}},
			ui.Expanded(ui.Provider[ui.Theme]{Value: fieldTheme, Child: ui.TextField{
				Value:       w.Snapshot.AuthFilter,
				OnChanged:   w.Callbacks.AuthFilterChanged,
				OnSubmitted: func(ctx ui.EventContext, _ string) { w.selectHighlightedProvider(ctx) },
				AutoFocus:   true,
			}}),
		}},
		ui.SizedBox{Height: 1},
	)
	providers := filteredAuthProviders(w.Snapshot.AuthFilter)
	if len(providers) == 0 {
		children = append(children, ui.Text{Value: "No results", Style: ui.Style{Foreground: theme.MutedForeground}})
	} else {
		selection := max(0, min(w.Snapshot.AuthSelection, len(providers)-1))
		for index, provider := range providers {
			provider := provider
			children = append(children, providerOptionRow(theme, provider, index == selection, func(ctx ui.EventContext) {
				if w.Callbacks.SelectProvider != nil {
					w.Callbacks.SelectProvider(ctx, provider.ID)
				}
			}))
		}
	}
	return ui.SizedBox{Height: 15, Child: ui.Flex{
		Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children,
	}}
}

func (w shellView) selectHighlightedProvider(ctx ui.EventContext) {
	providers := filteredAuthProviders(w.Snapshot.AuthFilter)
	if len(providers) == 0 || w.Callbacks.SelectProvider == nil {
		return
	}
	selection := max(0, min(w.Snapshot.AuthSelection, len(providers)-1))
	w.Callbacks.SelectProvider(ctx, providers[selection].ID)
}

func providerOptionRow(theme ui.Theme, provider authProviderOption, selected bool, onPressed ui.VoidCallback) ui.Widget {
	primary := ui.Style{Foreground: theme.Foreground, Background: theme.Background}
	secondary := ui.Style{Foreground: theme.MutedForeground, Background: theme.Background}
	if selected {
		primary = ui.Style{Foreground: theme.Background, Background: theme.Foreground}
		secondary.Background = theme.Foreground
	}
	return ui.SizedBox{Height: 1, Child: ui.DecoratedBox(
		ui.Decoration{Style: primary},
		ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.SizedBox{Width: 12, Child: ui.Text{
				Value: provider.Name, Style: primary, OnPressed: onPressed,
				ClickAffordance: ui.ClickAffordanceNone, Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
			}},
			ui.SizedBox{Width: 1},
			ui.Expanded(ui.Text{
				Value: provider.Method, Style: secondary, OnPressed: onPressed,
				ClickAffordance: ui.ClickAffordanceNone, Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
			}),
		}},
	)}
}

func (w shellView) apiKeyBody(theme ui.Theme) ui.Widget {
	fieldTheme := theme
	fieldTheme.Surface = theme.Background
	fieldTheme.SurfaceHovered = theme.Background
	children := []ui.Widget{}
	if w.Snapshot.Error != "" {
		children = append(children,
			ui.Text{Value: w.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true},
			ui.SizedBox{Height: 1},
		)
	}
	children = append(children,
		ui.Text{Value: "API key", Style: ui.Style{Foreground: theme.MutedForeground}},
		ui.Provider[ui.Theme]{Value: fieldTheme, Child: ui.TextField{
			Value: w.Snapshot.AuthAPIKey, OnChanged: w.Callbacks.AuthAPIKeyChanged,
			OnSubmitted: w.Callbacks.SubmitAPIKey, ObscureText: true, AutoFocus: true,
		}},
	)
	return ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
}

func (w shellView) deviceLoginBody(theme ui.Theme) []ui.Widget {
	instructions := w.Snapshot.Instructions
	if instructions.UserCode == "" {
		return []ui.Widget{
			ui.Text{Value: "Complete authentication in your browser.", Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true},
			ui.SizedBox{Height: 1},
			spinnerWithLabel("Starting device authorization…", ui.Style{Foreground: theme.MutedForeground}),
		}
	}
	waitStyle := ui.Style{Foreground: theme.PrimaryText}
	waitText := "Waiting for approval — expires in " + formatRemaining(w.Snapshot.Remaining)
	if w.Snapshot.Remaining <= 2*time.Minute {
		waitStyle.Foreground = theme.WarningText
		waitText = "Code expires in " + formatRemaining(w.Snapshot.Remaining) + " — esc to get a new code"
	}
	linkStyle := ui.Style{Foreground: theme.AccentText, UnderlineStyle: ui.UnderlineSingle}
	linkSpan := ui.TextSpan{Text: instructions.VerificationURI, Style: linkStyle}
	if hyperlink := safeHTTPSHyperlink(instructions.VerificationURI); hyperlink != "" {
		linkSpan.Style.Hyperlink = hyperlink
		linkSpan.Style.HyperlinkParams = "id=codex-device-login"
		if w.Callbacks.OpenURL != nil {
			linkSpan.OnPressed = func(ctx ui.EventContext) { w.Callbacks.OpenURL(ctx, hyperlink) }
		}
	}
	details := ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch,
		Children: []ui.Widget{
			ui.Text{Value: "Open this URL", Style: ui.Style{Foreground: theme.MutedForeground}},
			ui.RichText{Spans: []ui.TextSpan{linkSpan}, SoftWrap: true},
			ui.SizedBox{Height: 1},
			ui.Text{Value: "Enter this code", Style: ui.Style{Foreground: theme.MutedForeground}},
			ui.Text{Value: instructions.UserCode, Style: ui.Style{Attribute: ui.AttrBold}},
		},
	}
	return []ui.Widget{
		ui.Text{Value: "Open the link and enter the code to complete authentication.", Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true},
		ui.SizedBox{Height: 1},
		details,
		ui.SizedBox{Height: 1},
		spinnerWithLabel(waitText, waitStyle),
	}
}

func safeHTTPSHyperlink(raw string) string {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	for _, character := range raw {
		if character < 0x20 || character == 0x7f {
			return ""
		}
	}
	return raw
}

func dialogSurface(theme ui.Theme, title, meta string, body, footer ui.Widget, borderedFooter bool) ui.Widget {
	headerChildren := []ui.Widget{
		ui.Expanded(ui.Text{Value: title, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
	}
	if meta != "" {
		headerChildren = append(headerChildren, ui.Text{
			Value: meta, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		})
	}
	header := ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: headerChildren}
	bodyChildren := []ui.Widget{header, ui.SizedBox{Height: 1}, body}
	var content ui.Widget
	if footer != nil && borderedFooter {
		content = ui.Flex{
			Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch,
			Children: []ui.Widget{
				ui.Padding(ui.Insets{Top: 1, Right: 2, Bottom: 1, Left: 2}, ui.Flex{
					Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: bodyChildren,
				}),
				dialogDivider{Style: ui.Style{Foreground: theme.Border}},
				ui.Padding(ui.Insets{Right: 2, Bottom: 1, Left: 2}, footer),
			},
		}
	} else {
		if footer != nil {
			bodyChildren = append(bodyChildren, ui.SizedBox{Height: 1}, footer)
		}
		content = ui.Padding(ui.Insets{Top: 1, Right: 2, Bottom: 1, Left: 2}, ui.Flex{
			Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: bodyChildren,
		})
	}
	return proportionalWidth{Percent: 70, Min: 48, Max: 96, Child: ui.FocusScope{
		Trap: true, AutoFocus: true, Child: ui.DecoratedBox(
			ui.Decoration{
				Style:  ui.Style{Foreground: theme.Foreground, Background: theme.Background},
				Border: ui.BorderAll(ui.Style{Foreground: theme.Border}),
			},
			content,
		),
	}}
}

func emptyState(theme ui.Theme, instruction, hint string) ui.Widget {
	children := []ui.Widget{wordmark(theme), ui.SizedBox{Height: 1}, ui.Text{Value: instruction, Style: ui.Style{Foreground: theme.MutedForeground}}}
	if hint != "" {
		children = append(children, ui.SizedBox{Height: 1}, ui.Text{Value: hint, Style: ui.Style{Foreground: theme.DisabledForeground}})
	}
	return ui.Center(ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter, Children: children})
}

func wordmark(theme ui.Theme) ui.Widget {
	return ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
		ui.Text{Value: "k i t", Style: ui.Style{Attribute: ui.AttrBold}},
		ui.Text{Value: "━━━━━━━━━━━", Style: ui.Style{Foreground: theme.PrimaryText}},
	}}
}

func sessionDisplayName(session protocol.SessionInfo) string {
	if strings.TrimSpace(session.Name) != "" {
		return session.Name
	}
	return "Unnamed session"
}

func modelInformation(snapshot shellSnapshot, theme ui.Theme) []ui.TextSpan {
	label := modelDisplayName(snapshot.Session.Model)
	if thinking := strings.TrimSpace(snapshot.Session.ThinkingLevel); thinking != "" {
		label += " · thinking: " + thinking
	}
	spans := []ui.TextSpan{{Text: label, Style: ui.Style{Foreground: theme.MutedForeground}}}
	if percentage, ok := contextPercentage(snapshot.ContextTokens, snapshot.ContextWindow); ok {
		color := theme.PrimaryText
		if percentage > 90 {
			color = theme.DangerText
		} else if percentage >= 80 {
			color = theme.WarningText
		}
		spans = append(spans,
			ui.TextSpan{Text: " · ", Style: ui.Style{Foreground: theme.MutedForeground}},
			ui.TextSpan{Text: fmt.Sprintf("%d%%", percentage), Style: ui.Style{Foreground: color}},
		)
	}
	return spans
}

func contextPercentage(tokens, window int) (int, bool) {
	if tokens <= 0 || window <= 0 {
		return 0, false
	}
	percentage := int(math.Round(float64(tokens) * 100 / float64(window)))
	return min(100, max(1, percentage)), true
}

func modelDisplayName(model string) string {
	_, id, ok := strings.Cut(model, "/")
	if !ok {
		id = model
	}
	parts := strings.Split(id, "-")
	formatted := make([]string, 0, len(parts))
	for index := 0; index < len(parts); index++ {
		part := parts[index]
		if allDigits(part) && index+1 < len(parts) && allDigits(parts[index+1]) {
			part += "." + parts[index+1]
			index++
		}
		switch strings.ToLower(part) {
		case "gpt":
			part = "GPT"
		case "openai":
			part = "OpenAI"
		default:
			if part != "" && !allDigits(part) {
				part = strings.ToUpper(part[:1]) + part[1:]
			}
		}
		formatted = append(formatted, part)
	}
	return strings.Join(formatted, " ")
}

func formatRemaining(remaining time.Duration) string {
	seconds := int(math.Ceil(remaining.Seconds()))
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
