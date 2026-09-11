package tui

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

type shellSnapshot struct {
	Phase                       phase
	Error                       string
	Status                      string
	Composer                    string
	ComposerCursorEndGeneration uint64
	PaletteOpen                 bool
	PaletteQuery                string
	PaletteSelection            paletteCommandID
	PaletteCommands             []paletteCommand
	ConfigurationPicker         configurationPickerSnapshot
	SessionDetailsOpen          bool
	SessionRename               sessionRenameSnapshot
	SessionExplorer             sessionExplorerSnapshot
	AuthReturnReady             bool
	AuthFilter                  string
	AuthSelection               int
	AuthProviderID              string
	AuthAPIKey                  string
	AuthCode                    string
	AuthPending                 bool
	Session                     protocol.SessionInfo
	Messages                    []transcriptMessage
	Running                     bool
	AgentRunning                bool
	TurnActivity                string
	TurnThinking                string
	FollowUps                   protocol.FollowUpQueue
	ContextTokens               int
	ContextWindow               int
	SessionUsage                protocol.SessionUsage
	Scroll                      *ui.ScrollController
	ActivityScroll              *ui.ScrollController
	ActivityList                *activityListController
	ActivityFocus               *ui.FocusNode
	WorkspaceLayout             *workspaceLayoutState
	ActivitySourceID            string
	ActivitySelected            bool
	HoveredActivityID           string
	ActivityExpanded            map[activityToolKey]bool
	ActivityCursor              activityToolKey
	BashRunning                 bool
	BashStarting                bool
	BashCollapsed               map[string]bool
	BashHistory                 bashHistoryController
	Instructions                auth.OpenAICodexDeviceInstructions
	BrowserInstructions         auth.AnthropicLoginInstructions
	Remaining                   time.Duration
	Location                    string
	Toasts                      []toastRecord
}

type providerSelectedCallback func(ui.EventContext, string)
type selectionMovedCallback func(ui.EventContext, int)

type shellCallbacks struct {
	OpenAuth                   ui.VoidCallback
	SelectProvider             providerSelectedCallback
	MoveProviderSelection      selectionMovedCallback
	AuthFilterChanged          ui.TextChangedCallback
	AuthAPIKeyChanged          ui.TextChangedCallback
	SubmitAPIKey               ui.TextChangedCallback
	AuthCodeChanged            ui.TextChangedCallback
	SubmitAuthCode             ui.TextChangedCallback
	OpenURL                    ui.TextChangedCallback
	CopyCode                   ui.VoidCallback
	OpenActivity               func(ui.EventContext, string)
	HoverActivity              func(ui.EventContext, string)
	ShowTranscript             ui.VoidCallback
	ShowActivity               ui.VoidCallback
	CloseActivity              ui.VoidCallback
	ScrollActivity             func(ui.EventContext, int)
	ToggleActivityTool         func(ui.EventContext, activityToolKey)
	SelectActivityTool         func(ui.EventContext, activityToolKey)
	MoveActivityTool           func(ui.EventContext, int)
	ToggleBashOutput           func(ui.EventContext, string)
	OpenBashHistory            func(ui.EventContext, int) bool
	BashHistoryChanged         ui.TextChangedCallback
	SelectBashHistory          func(ui.EventContext, string)
	ComposerChanged            ui.TextChangedCallback
	ComposerPasted             ui.TextChangedCallback
	RestoreFollowUps           ui.VoidCallback
	CopySelection              func(string)
	DismissToast               func(uint64)
	OpenPalette                ui.VoidCallback
	PaletteQueryChanged        ui.TextChangedCallback
	MovePaletteSelection       selectionMovedCallback
	RunPaletteQuery            ui.TextChangedCallback
	RunPaletteCommand          func(ui.EventContext, paletteCommandID)
	OpenSessionRename          ui.VoidCallback
	OpenModel                  ui.VoidCallback
	OpenThinking               ui.VoidCallback
	ConfigurationQuery         ui.TextChangedCallback
	SelectConfiguration        func(ui.EventContext, string)
	ApplyConfiguration         ui.VoidCallback
	SelectSession              func(ui.EventContext, string)
	SessionRenameChanged       ui.TextChangedCallback
	SubmitCurrentSessionRename ui.TextChangedCallback
	RenameSessionChanged       ui.TextChangedCallback
	SubmitSessionRename        ui.TextChangedCallback
	Submit                     ui.TextChangedCallback
	Retry                      ui.VoidCallback
	Quit                       ui.VoidCallback
	Dismiss                    ui.VoidCallback
}

type shellView struct {
	Snapshot     shellSnapshot
	Callbacks    shellCallbacks
	presentation transcriptPresentation
}

type quitIntent struct{}

func (quitIntent) IntentType() ui.IntentType { return "kit.quit" }

type copyCodeIntent struct{}

func (copyCodeIntent) IntentType() ui.IntentType { return "kit.auth.copy-code" }

type retryIntent struct{}

func (retryIntent) IntentType() ui.IntentType { return "kit.retry" }

type moveProviderIntent struct{ Delta int }

func (moveProviderIntent) IntentType() ui.IntentType { return "kit.auth.move-provider" }

type openPaletteIntent struct{}

func (openPaletteIntent) IntentType() ui.IntentType { return "kit.command-palette.open" }

type scrollActivityIntent struct{ Pages int }

func (scrollActivityIntent) IntentType() ui.IntentType { return "kit.activity.scroll" }

type moveActivityToolIntent struct{ Delta int }

func (moveActivityToolIntent) IntentType() ui.IntentType { return "kit.activity.move-tool" }

type toggleActivityToolIntent struct{}

func (toggleActivityToolIntent) IntentType() ui.IntentType { return "kit.activity.toggle-tool" }

type movePaletteIntent struct{ Delta int }

func (movePaletteIntent) IntentType() ui.IntentType { return "kit.command-palette.move" }

func (w shellView) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	w.presentation = presentTranscript(w.Snapshot.Messages)
	content := ui.Widget(ui.SelectionArea{Child: w.baseShell(theme)})
	overlays := w.authOverlays(theme)
	if w.Snapshot.Phase == phaseReady && w.Snapshot.BashHistory.Open {
		controller := w.Snapshot.BashHistory
		composerHeight := min(composerMaxHeight, max(1, strings.Count(w.Snapshot.Composer, "\n")+1))
		primaryPercent := 100
		if w.Snapshot.WorkspaceLayout != nil && w.Snapshot.WorkspaceLayout.Wide {
			primaryPercent = 60
		}
		overlays = append(overlays, ui.OverlayEntry{
			Modal: true, Barrier: clearModalBarrier{},
			Child: bashHistorySurface{
				Controller: &controller, BottomInset: composerHeight + 4, PrimaryPercent: primaryPercent,
				OnQuery:  w.Callbacks.BashHistoryChanged,
				OnSelect: w.Callbacks.SelectBashHistory,
			},
		})
	}
	if w.Snapshot.Phase == phaseReady && w.Snapshot.ConfigurationPicker.Mode != configurationPickerClosed {
		overlays = append(overlays, modalDialogEntry(configurationPickerSurface{
			Snapshot: w.Snapshot.ConfigurationPicker, QueryChanged: w.Callbacks.ConfigurationQuery,
			Select: w.Callbacks.SelectConfiguration, Apply: w.Callbacks.ApplyConfiguration,
		}))
	}
	if w.Snapshot.Phase == phaseReady && w.Snapshot.SessionDetailsOpen {
		overlays = append(overlays, modalDialogEntry(sessionDetailsSurface{
			Session: w.Snapshot.Session, ContextTokens: w.Snapshot.ContextTokens,
			ContextWindow: w.Snapshot.ContextWindow, Usage: w.Snapshot.SessionUsage,
		}))
	}
	if w.Snapshot.Phase == phaseReady && w.Snapshot.SessionRename.Open {
		overlays = append(overlays, modalDialogEntry(sessionRenameSurface{
			Snapshot: w.Snapshot.SessionRename,
			Callbacks: sessionRenameCallbacks{
				Changed: w.Callbacks.SessionRenameChanged, Submitted: w.Callbacks.SubmitCurrentSessionRename,
			},
		}))
	}
	if w.Snapshot.Phase == phaseReady && w.Snapshot.SessionExplorer.Open {
		overlays = append(overlays, modalDialogEntry(sessionExplorerSurface{
			Snapshot:  w.Snapshot.SessionExplorer,
			Callbacks: sessionExplorerCallbacks{Select: w.Callbacks.SelectSession},
		}))
		if w.Snapshot.SessionExplorer.RenameOpen {
			overlays = append(overlays, modalDialogEntry(sessionRenameSurface{
				Snapshot: sessionExplorerRenameSnapshot(w.Snapshot.SessionExplorer),
				Callbacks: sessionRenameCallbacks{
					Changed: w.Callbacks.RenameSessionChanged, Submitted: w.Callbacks.SubmitSessionRename,
				},
			}))
		}
		if w.Snapshot.SessionExplorer.DeleteOpen {
			overlays = append(overlays, modalDialogEntry(sessionDeleteSurface{Snapshot: w.Snapshot.SessionExplorer}))
		}
	}
	if w.Snapshot.Phase == phaseReady && w.Snapshot.PaletteOpen {
		overlays = append(overlays, ui.OverlayEntry{
			Modal: true, Barrier: clearModalBarrier{},
			Child: commandPaletteSurface{
				Snapshot: paletteSnapshot{
					Query: w.Snapshot.PaletteQuery, Selection: w.Snapshot.PaletteSelection,
					Running: w.Snapshot.Running, Contributions: w.Snapshot.PaletteCommands,
				},
				Callbacks: paletteCallbacks{
					QueryChanged: w.Callbacks.PaletteQueryChanged,
					RunQuery:     w.Callbacks.RunPaletteQuery,
					RunCommand:   w.Callbacks.RunPaletteCommand,
				},
			},
		})
	}
	if len(w.Snapshot.Toasts) > 0 {
		overlays = append(overlays, ui.OverlayEntry{Child: toastStack{
			Toasts: w.Snapshot.Toasts, OnDismiss: w.Callbacks.DismissToast, Animate: true,
		}})
	}
	root := ui.Widget(ui.Overlay{Child: content, Entries: overlays})

	workspaceWide := w.Snapshot.WorkspaceLayout != nil && w.Snapshot.WorkspaceLayout.Wide
	actions := map[ui.IntentType]ui.ActionFunc{
		quitIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.Quit != nil {
				w.Callbacks.Quit(ctx)
			}
			return ui.EventHandled
		},
		ui.NextFocusIntentType: func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Snapshot.ActivitySourceID == "" || workspaceWide || w.Snapshot.ActivitySelected {
				ctx.FocusNext()
			}
			return ui.EventHandled
		},
		ui.PreviousFocusIntentType: func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Snapshot.ActivitySourceID == "" || workspaceWide || w.Snapshot.ActivitySelected {
				ctx.FocusPrevious()
			}
			return ui.EventHandled
		},
	}
	shortcuts := ui.ShortcutMap{
		"Escape":  ui.DismissIntent{},
		"Ctrl+c":  quitIntent{},
		"Super+c": ui.CopySelectionTextIntent{OnCopied: w.Callbacks.CopySelection},
		"Tab":     ui.NextFocusIntent{}, "Shift+Tab": ui.PreviousFocusIntent{},
	}
	if w.Snapshot.Phase == phaseReady {
		shortcuts["Ctrl+p"] = openPaletteIntent{}
		actions[openPaletteIntent{}.IntentType()] = func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.OpenPalette != nil {
				w.Callbacks.OpenPalette(ctx)
			}
			return ui.EventHandled
		}
	}
	activityKeyboardActive := w.Snapshot.ActivitySelected || workspaceWide
	if w.Snapshot.ActivitySourceID != "" && activityKeyboardActive {
		shortcuts["Page_Up"] = scrollActivityIntent{Pages: -1}
		shortcuts["Page_Down"] = scrollActivityIntent{Pages: 1}
		actions[scrollActivityIntent{}.IntentType()] = func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
			if w.Callbacks.ScrollActivity != nil {
				w.Callbacks.ScrollActivity(ctx, intent.(scrollActivityIntent).Pages)
			}
			return ui.EventHandled
		}
		if w.Snapshot.ActivitySelected && !workspaceWide && !w.Snapshot.PaletteOpen {
			shortcuts["Up"] = moveActivityToolIntent{Delta: -1}
			shortcuts["Down"] = moveActivityToolIntent{Delta: 1}
			shortcuts["Enter"] = toggleActivityToolIntent{}
			actions[moveActivityToolIntent{}.IntentType()] = func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
				if w.Callbacks.MoveActivityTool != nil {
					w.Callbacks.MoveActivityTool(ctx, intent.(moveActivityToolIntent).Delta)
				}
				return ui.EventHandled
			}
			actions[toggleActivityToolIntent{}.IntentType()] = func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
				if w.Callbacks.ToggleActivityTool != nil && w.Snapshot.ActivityCursor.ToolCallID != "" {
					w.Callbacks.ToggleActivityTool(ctx, w.Snapshot.ActivityCursor)
				}
				return ui.EventHandled
			}
		}
	}
	if w.Snapshot.PaletteOpen {
		shortcuts["Up"] = movePaletteIntent{Delta: -1}
		shortcuts["Down"] = movePaletteIntent{Delta: 1}
		actions[movePaletteIntent{}.IntentType()] = func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
			if w.Callbacks.MovePaletteSelection != nil {
				w.Callbacks.MovePaletteSelection(ctx, intent.(movePaletteIntent).Delta)
			}
			return ui.EventHandled
		}
	}
	if w.Snapshot.Phase == phaseReady || w.Snapshot.Phase == phaseAuthSelect || w.Snapshot.Phase == phaseAuthWaiting || w.Snapshot.Phase == phaseAuthBrowser ||
		(w.Snapshot.Phase == phaseAuthAPIKey && !w.Snapshot.AuthPending) {
		actions[ui.DismissIntentType] = func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Snapshot.Phase == phaseReady && !w.Snapshot.PaletteOpen && !w.Snapshot.SessionExplorer.Open && !w.Snapshot.BashHistory.Open && !w.Snapshot.Running && w.Snapshot.ActivitySourceID != "" && w.Callbacks.CloseActivity != nil {
				w.Callbacks.CloseActivity(ctx)
			} else if w.Callbacks.Dismiss != nil {
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
		ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: shortcuts, Child: root}},
	)
}

func (w shellView) conversationVisible() bool {
	return w.Snapshot.Phase == phaseReady || w.Snapshot.AuthReturnReady
}

func (w shellView) baseShell(theme ui.Theme) ui.Widget {
	body := ui.Widget(ui.Expanded(w.body(theme)))
	if w.conversationVisible() {
		activityOpen := w.Snapshot.ActivitySourceID != ""
		activityPane := w.activityPane(theme)
		workspaceWide := w.Snapshot.WorkspaceLayout != nil && w.Snapshot.WorkspaceLayout.Wide
		if activityOpen && w.Snapshot.ActivityFocus != nil {
			activityPane = ui.Focus(w.Snapshot.ActivityFocus, activityPane)
		}
		body = ui.Expanded(conversationWorkspaceHost{
			Open: activityOpen, ActivitySelected: w.Snapshot.ActivitySelected,
			Tabs: ui.SelectionContainer{Disabled: true, Child: w.workspaceTabs(theme)},
			Transcript: workspaceSelectionGate{
				LayoutState: w.Snapshot.WorkspaceLayout,
				Child: ui.FocusScope{
					AutoFocus: activityOpen && !workspaceWide && !w.Snapshot.ActivitySelected,
					Child:     w.body(theme),
				},
			},
			Pending:           w.pendingSlot(theme),
			PendingHeight:     1 + min(3, w.Snapshot.FollowUps.Count),
			ComposerSeparator: ui.Divider{Style: ui.Style{Foreground: w.composerSeparatorColor(theme)}},
			Composer:          w.composer(theme),
			PaneSeparator:     ui.Divider{Axis: ui.Vertical, Style: ui.Style{Foreground: theme.Border}},
			SeparatorStyle:    ui.Style{Foreground: theme.Border},
			LayoutState:       w.Snapshot.WorkspaceLayout,
			Activity: workspaceSelectionGate{
				LayoutState: w.Snapshot.WorkspaceLayout, Activity: true,
				Child: ui.FocusScope{
					Trap:      activityOpen && !workspaceWide && w.Snapshot.ActivitySelected,
					AutoFocus: activityOpen && !workspaceWide && w.Snapshot.ActivitySelected,
					Child:     activityPane,
				},
			},
		})
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		w.header(theme),
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		body,
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		w.footer(theme),
	}}
}

func (w shellView) header(theme ui.Theme) ui.Widget {
	left := ui.Widget(ui.Text{Value: "kit", Overflow: ui.TextOverflowEllipsis, MaxLines: 1})
	right := ui.Widget(ui.SizedBox{})
	if w.conversationVisible() {
		left = ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{ui.Flexible(headerControl{
			Label: sessionDisplayName(w.Snapshot.Session), Primary: true, OnPressed: w.Callbacks.OpenSessionRename,
		})}}
		right = w.modelInformationControls(theme)
	}
	return ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Flex{
		Axis:               ui.Horizontal,
		CrossAxisAlignment: ui.CrossAxisStretch,
		Children: []ui.Widget{
			ui.ExpandedWidget{Flex: 1, Child: left},
			ui.ExpandedWidget{Flex: 2, Child: right},
		},
	})}
}

func (w shellView) modelInformationControls(theme ui.Theme) ui.Widget {
	children := []ui.Widget{
		headerControl{Label: modelDisplayName(w.Snapshot.Session.Model), OnPressed: w.Callbacks.OpenModel},
	}
	if thinking := strings.TrimSpace(w.Snapshot.Session.ThinkingLevel); thinking != "" {
		children = append(children,
			ui.SizedBox{Width: 1},
			headerControl{Label: "(" + thinking + ")", OnPressed: w.Callbacks.OpenThinking},
		)
	}
	if percentage, ok := contextPercentage(w.Snapshot.ContextTokens, w.Snapshot.ContextWindow); ok {
		color := theme.MutedForeground
		if percentage > 90 {
			color = theme.DangerText
		} else if percentage >= 80 {
			color = theme.WarningText
		}
		children = append(children,
			ui.Text{Value: " · ", Style: ui.Style{Foreground: theme.MutedForeground}},
			ui.Text{Value: fmt.Sprintf("%d%%", percentage), Style: ui.Style{Foreground: color}},
		)
	}
	return ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: append([]ui.Widget{ui.Expanded(ui.SizedBox{})}, children...)}
}

func (w shellView) body(theme ui.Theme) ui.Widget {
	if w.conversationVisible() {
		return w.transcript(theme)
	}
	switch w.Snapshot.Phase {
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
			plainButton{Label: "Connect a provider", OnPressed: w.Callbacks.OpenAuth},
		}})}
	}
}

func (w shellView) transcript(theme ui.Theme) ui.Widget {
	presentation := w.presentation
	if len(presentation.Items) == 0 {
		return emptyState(theme, "Ask a question or give a task.", "")
	}
	children := make([]ui.Widget, 0, len(presentation.Items)*2)
	for index, item := range presentation.Items {
		if index > 0 {
			children = append(children, keyedTranscriptItem{ID: "transcript-gap:" + item.ID, Child: ui.SizedBox{Height: 1}})
		}
		var child ui.Widget
		switch item.Kind {
		case transcriptDisplaySingle:
			if item.Item.Kind == transcriptItemBash && item.Item.Message.Bash != nil {
				execution := *item.Item.Message.Bash
				child = transcriptBashEntry(theme, execution, w.Snapshot.BashCollapsed[execution.ID], func(ctx ui.EventContext) {
					if w.Callbacks.ToggleBashOutput != nil {
						w.Callbacks.ToggleBashOutput(ctx, execution.ID)
					}
				})
			} else {
				child = transcriptUserEntry(theme, item.Item.Message)
			}
		case transcriptDisplayAssistantProse:
			child = transcriptAssistantEntry(theme, item.Item.Message)
		case transcriptDisplayTurnWork:
			child = w.transcriptWorkChip(theme, item, presentation.ToolStates)
		}
		if child != nil {
			children = append(children, keyedTranscriptItem{ID: item.ID, Child: child})
		}
	}
	return ui.Scrollbar{Child: ui.ScrollView{
		Controller: w.Snapshot.Scroll,
		Child:      ui.Padding(ui.All(1), ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}),
	}}
}

type keyedTranscriptItem struct {
	ID    string
	Child ui.Widget
}

func (w keyedTranscriptItem) WidgetKey() ui.KeyValue { return ui.KeyValue(w.ID) }

func (w keyedTranscriptItem) Build(ui.BuildContext) ui.Widget { return w.Child }

func transcriptUserEntry(theme ui.Theme, message protocol.TranscriptMessage) ui.Widget {
	content := markdownView{
		ID: "transcript-user:" + message.ID, Source: message.TextContent(),
		BaseStyle: ui.Style{Foreground: theme.Foreground},
	}
	return ui.DecoratedBox(
		ui.Decoration{Border: ui.Border{Style: ui.Style{Foreground: theme.PrimaryText}, Left: true}},
		ui.Padding(ui.Insets{Left: 2}, content),
	)
}

func transcriptAssistantEntry(theme ui.Theme, message protocol.TranscriptMessage) ui.Widget {
	style := ui.Style{Foreground: theme.Foreground}
	if message.StopReason == "aborted" {
		style.Foreground = theme.MutedForeground
	} else if message.IsError {
		style.Foreground = theme.DangerText
	}
	return markdownView{
		ID: "transcript-assistant:" + message.ID, Source: assistantProse(message),
		BaseStyle: style,
	}
}

func (w shellView) transcriptWorkChip(theme ui.Theme, item transcriptDisplayItem, toolStates map[transcriptToolStateKey]transcriptMessage) ui.Widget {
	calls := displayItemToolCalls(item)
	aborted := false
	inProgress := false
	for _, step := range item.Items {
		aborted = aborted || step.Aborted
	}
	for _, call := range calls {
		state, exists := toolStates[transcriptToolStateKey{TurnID: item.TurnID, ToolCallID: call.ID}]
		if !aborted && (!exists || state.Pending) {
			inProgress = true
			break
		}
	}
	countLabel := fmt.Sprintf("%d tool calls", len(calls))
	if len(calls) == 1 {
		countLabel = "1 tool call"
	}
	if len(calls) == 0 {
		countLabel = fmt.Sprintf("%d steps", len(item.Items))
		if len(item.Items) == 1 {
			countLabel = "1 step"
		}
	}
	visible := min(8, len(calls))
	names := make([]string, 0, visible+1)
	for _, call := range calls[:visible] {
		names = append(names, toolDisplayName(call))
	}
	if len(calls) > visible {
		names = append(names, fmt.Sprintf("+%d more", len(calls)-visible))
	}
	background := theme.Surface
	if item.ID == w.Snapshot.ActivitySourceID {
		background = theme.SurfacePressed
	} else if item.ID == w.Snapshot.HoveredActivityID {
		background = theme.SurfaceHovered
	}
	prefix := ui.Widget(ui.Text{Value: glyphChevronRight, Style: ui.Style{Foreground: theme.MutedForeground}})
	if inProgress {
		prefix = spinner{Style: ui.Style{Foreground: theme.MutedForeground}}
	}
	row := ui.DecoratedBox(ui.Decoration{Style: ui.Style{Background: background}}, ui.Padding(ui.Symmetric(1, 0), ui.Flex{
		Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			prefix,
			ui.SizedBox{Width: 1},
			ui.Text{Value: countLabel, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
			ui.SizedBox{Width: 1},
			ui.Expanded(ui.Text{
				Value: strings.Join(names, " "+glyphMiddleDot+" "), Style: ui.Style{Foreground: theme.DisabledForeground},
				Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
			}),
		},
	}))
	return mouseActivator{
		Child: ui.SizedBox{Height: 1, Child: row},
		OnPressed: func(ctx ui.EventContext) {
			if w.Callbacks.OpenActivity != nil {
				w.Callbacks.OpenActivity(ctx, item.ID)
			}
		},
		OnHover: func(ctx ui.EventContext) {
			if w.Callbacks.HoverActivity != nil {
				w.Callbacks.HoverActivity(ctx, item.ID)
			}
		},
		OnHoverExit: func(ctx ui.EventContext) {
			if w.Callbacks.HoverActivity != nil {
				w.Callbacks.HoverActivity(ctx, "")
			}
		},
	}
}

func (w shellView) workspaceTabs(theme ui.Theme) ui.Widget {
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Height: 1, Child: ui.DecoratedBox(
			ui.Decoration{Style: ui.Style{Background: theme.Background}},
			ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
				workspaceTab{Label: "Transcript", Selected: !w.Snapshot.ActivitySelected, OnSelect: w.Callbacks.ShowTranscript},
				workspaceTab{
					Label: "Activity", Selected: w.Snapshot.ActivitySelected, Closable: true,
					OnSelect: w.Callbacks.ShowActivity, OnClose: w.Callbacks.CloseActivity,
				},
			}},
		)},
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
	}}
}

func (w shellView) pendingSlot(theme ui.Theme) ui.Widget {
	style := ui.Style{Foreground: theme.MutedForeground}
	statusChildren := []ui.Widget(nil)
	var status ui.Widget
	if strings.TrimSpace(w.Snapshot.TurnThinking) != "" {
		status = markdownInlineView{Source: latestThinkingLine(w.Snapshot.TurnThinking), BaseStyle: style}
	} else if w.Snapshot.TurnActivity != "" {
		status = ui.Text{Value: w.Snapshot.TurnActivity, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}
	}
	if status != nil {
		statusChildren = []ui.Widget{spinner{Style: style}, ui.SizedBox{Width: 1}, ui.Expanded(status)}
	}
	rows := []ui.Widget{ui.SizedBox{Height: 1, Child: ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStart, Children: statusChildren}}}
	visible := min(3, len(w.Snapshot.FollowUps.Previews))
	if w.Snapshot.FollowUps.Count > 3 {
		visible = min(2, visible)
	}
	for index, preview := range w.Snapshot.FollowUps.Previews[:visible] {
		rows = append(rows, ui.SizedBox{Height: 1, Child: ui.Text{
			Value: fmt.Sprintf("Follow-up %d: %s", index+1, preview), Style: style,
			Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		}})
	}
	if w.Snapshot.FollowUps.Count > visible {
		rows = append(rows, ui.SizedBox{Height: 1, Child: ui.Text{Value: fmt.Sprintf("+%d more follow-ups", w.Snapshot.FollowUps.Count-visible), Style: style}})
	}
	return ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows})
}

func (w shellView) composerSeparatorColor(theme ui.Theme) ui.Color {
	if strings.HasPrefix(w.Snapshot.Composer, "!") {
		return theme.SuccessText
	}
	return theme.Border
}

func (w shellView) composer(theme ui.Theme) ui.Widget {
	composerTheme := theme
	composerTheme.Surface = theme.Background
	composerTheme.SurfaceHovered = theme.Background
	composerTheme.Selection = theme.Selection
	var restoreFollowUps ui.VoidCallback
	if w.Snapshot.FollowUps.Count > 0 {
		restoreFollowUps = w.Callbacks.RestoreFollowUps
	}
	composer := messageComposer{
		Value:               w.Snapshot.Composer,
		Placeholder:         "Ask kit to do something…",
		OnChanged:           w.Callbacks.ComposerChanged,
		OnPasted:            w.Callbacks.ComposerPasted,
		OnSubmitted:         w.Callbacks.Submit,
		OpenPalette:         w.Callbacks.OpenPalette,
		OpenBashHistory:     w.Callbacks.OpenBashHistory,
		RestoreFollowUps:    restoreFollowUps,
		CursorEndGeneration: w.Snapshot.ComposerCursorEndGeneration,
	}
	content := ui.Widget(ui.Provider[ui.Theme]{Value: composerTheme, Child: composer})
	if w.Snapshot.ActivitySelected {
		content = mouseActivator{Child: content, OnPrimaryDownCapture: w.Callbacks.ShowTranscript}
	}
	return content
}

func (w shellView) footer(theme ui.Theme) ui.Widget {
	left := w.Snapshot.Status
	leftStyle := ui.Style{Foreground: theme.MutedForeground}
	bashError := strings.HasPrefix(w.Snapshot.Status, "Bash failed:") || strings.HasPrefix(w.Snapshot.Status, "Bash update failed:") || strings.HasPrefix(w.Snapshot.Status, "Could not resume bash:") || w.Snapshot.Status == "A bash command is already running"
	if bashError {
		leftStyle.Foreground = theme.DangerText
	} else if w.Snapshot.FollowUps.Count > 0 {
		left = fmt.Sprintf("%d queued %s ↑ restore", w.Snapshot.FollowUps.Count, glyphMiddleDot)
	} else if strings.HasPrefix(w.Snapshot.Composer, "!!") {
		left = "bash command " + glyphMiddleDot + " result excluded from context"
		leftStyle.Foreground = theme.SuccessText
	} else if strings.HasPrefix(w.Snapshot.Composer, "!") {
		left = "bash command " + glyphMiddleDot + " result will be added to context"
		leftStyle.Foreground = theme.SuccessText
	} else if w.Snapshot.BashStarting {
		left = "starting bash…"
	} else if w.Snapshot.BashRunning {
		left = "running bash " + glyphMiddleDot + " esc cancel"
		if w.Snapshot.AgentRunning {
			left += " " + glyphMiddleDot + " agent running"
		}
	}
	if !w.Snapshot.AuthReturnReady && (w.Snapshot.Phase == phaseAuthGate || w.Snapshot.Phase == phaseAuthSelect) {
		left = "enter connect · ctrl+c quit"
	}
	if w.Snapshot.Phase == phaseFailed {
		left = "r retry · ctrl+c quit"
	}
	return ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Flex{
		Axis:               ui.Horizontal,
		CrossAxisAlignment: ui.CrossAxisStretch,
		Children: []ui.Widget{
			ui.ExpandedWidget{Flex: 1, Child: ui.Text{Value: left, Style: leftStyle, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}},
			ui.ExpandedWidget{Flex: 2, Child: ui.Text{Value: w.Snapshot.Location, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1, Align: ui.TextAlignRight}},
		},
	})}
}

func (w shellView) authOverlays(theme ui.Theme) []ui.OverlayEntry {
	switch w.Snapshot.Phase {
	case phaseAuthSelect:
		return []ui.OverlayEntry{modalDialogEntry(dialogSurface(
			theme,
			"Connect a provider",
			"",
			w.providerSelectionBody(theme),
			ui.Text{Value: "↑↓ move · enter select · esc close", Style: ui.Style{Foreground: theme.MutedForeground}},
			false,
		))}
	case phaseAuthWaiting:
		return []ui.OverlayEntry{modalDialogEntry(dialogSurface(
			theme,
			"Complete login",
			"OpenAI Codex",
			ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: w.deviceLoginBody(theme)},
			ui.Text{Value: "c copy code · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground}},
			true,
		))}
	case phaseAuthBrowser:
		return []ui.OverlayEntry{modalDialogEntry(dialogSurface(
			theme,
			"Complete login",
			"Claude Pro or Max",
			w.browserLoginBody(theme),
			ui.Text{Value: "enter submit · esc cancel", Style: ui.Style{Foreground: theme.MutedForeground}},
			false,
		))}
	case phaseAuthAPIKey:
		provider, _ := authProviderByID(w.Snapshot.AuthProviderID)
		footer := "enter save · esc back"
		if w.Snapshot.AuthPending {
			footer = "Saving…"
		}
		return []ui.OverlayEntry{modalDialogEntry(dialogSurface(
			theme,
			"Connect "+provider.Name,
			"",
			w.apiKeyBody(theme),
			ui.Text{Value: footer, Style: ui.Style{Foreground: theme.MutedForeground}},
			false,
		))}
	default:
		return nil
	}
}

func modalDialogEntry(child ui.Widget) ui.OverlayEntry {
	return ui.OverlayEntry{
		Modal: true, Barrier: clearModalBarrier{},
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: ui.Stack{
			Children: []ui.Widget{ui.SelectionArea{Child: child}, modalFocusAnchor{}},
		}},
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
			textInput(fieldTheme, textInputConfig{
				Value:       w.Snapshot.AuthFilter,
				OnChanged:   w.Callbacks.AuthFilterChanged,
				OnSubmitted: func(ctx ui.EventContext, _ string) { w.selectHighlightedProvider(ctx) },
				AutoFocus:   true,
			}),
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
		ui.Flex{Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, Children: []ui.Widget{
			textInput(fieldTheme, textInputConfig{
				Value: w.Snapshot.AuthAPIKey, OnChanged: w.Callbacks.AuthAPIKeyChanged,
				OnSubmitted: w.Callbacks.SubmitAPIKey, ObscureText: true, AutoFocus: true,
			}),
		}},
	)
	return ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
}

func (w shellView) browserLoginBody(theme ui.Theme) ui.Widget {
	instructions := w.Snapshot.BrowserInstructions
	children := []ui.Widget{}
	if w.Snapshot.Error != "" {
		children = append(children, ui.Text{Value: w.Snapshot.Error, Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true}, ui.SizedBox{Height: 1})
	}
	if instructions.AuthorizationURL == "" {
		children = append(children, spinnerWithLabel("Starting browser authorization…", ui.Style{Foreground: theme.MutedForeground}))
		return ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
	}
	linkStyle := ui.Style{Foreground: theme.AccentText, UnderlineStyle: ui.UnderlineSingle}
	link := ui.TextSpan{Text: "https://claude.ai/oauth/authorize", Style: linkStyle}
	if safe := safeHTTPSHyperlink(instructions.AuthorizationURL); safe != "" {
		link.Style.Hyperlink = safe
		link.Style.HyperlinkParams = "id=anthropic-oauth-login"
		if w.Callbacks.OpenURL != nil {
			link.OnPressed = func(ctx ui.EventContext) { w.Callbacks.OpenURL(ctx, safe) }
		}
	}
	fieldTheme := theme
	fieldTheme.Surface, fieldTheme.SurfaceHovered = theme.Background, theme.Background
	children = append(children,
		ui.Text{Value: "Complete authentication in your browser.", Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true},
		ui.Text{Value: "Open this URL", Style: ui.Style{Foreground: theme.MutedForeground}},
		ui.RichText{Spans: []ui.TextSpan{link}, SoftWrap: true},
		ui.SizedBox{Height: 1},
		ui.Text{Value: "If the callback does not complete, paste the final redirect URL or authorization code:", Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true},
		ui.Flex{Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMax, Children: []ui.Widget{
			textInput(fieldTheme, textInputConfig{Value: w.Snapshot.AuthCode, OnChanged: w.Callbacks.AuthCodeChanged, OnSubmitted: w.Callbacks.SubmitAuthCode, AutoFocus: true}),
		}},
		ui.SizedBox{Height: 1},
		spinnerWithLabel(w.Snapshot.Status, ui.Style{Foreground: theme.PrimaryText}),
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
	safe := safeExternalHyperlink(raw)
	if safe == "" {
		return ""
	}
	parsed, _ := url.ParseRequestURI(safe)
	if parsed.Scheme != "https" {
		return ""
	}
	return safe
}

func safeExternalHyperlink(raw string) string {
	if raw == "" || len(raw) > 4096 || !utf8.ValidString(raw) || raw != strings.TrimSpace(raw) {
		return ""
	}
	for _, character := range raw {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) ||
			unicode.Is(unicode.Zl, character) || unicode.Is(unicode.Zp, character) {
			return ""
		}
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	switch parsed.Scheme {
	case "http", "https":
		if parsed.Host == "" {
			return ""
		}
	case "mailto":
		if parsed.Opaque == "" {
			return ""
		}
	default:
		return ""
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
	return proportionalWidth{Percent: 70, Min: 48, Max: 96, Child: ui.DecoratedBox(
		ui.Decoration{
			Style:  ui.Style{Foreground: theme.Foreground, Background: theme.Background},
			Border: ui.BorderAll(ui.Style{Foreground: theme.Border}),
		},
		content,
	)}
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
