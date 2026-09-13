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
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

type shellSnapshot struct {
	Phase                        phase
	Error                        string
	Status                       string
	Composer                     string
	ComposerAttachments          []stagedAttachment
	ComposerCursorEndGeneration  uint64
	ComposerCursorOffset         int
	ComposerCursorGeneration     uint64
	PaletteOpen                  bool
	PaletteQuery                 string
	PaletteSelection             paletteCommandID
	PaletteCommands              []paletteCommand
	ThemePicker                  themePickerSnapshot
	ConfigurationPicker          configurationPickerSnapshot
	SessionDetailsOpen           bool
	SessionRename                sessionRenameSnapshot
	SessionExplorer              sessionExplorerSnapshot
	AuthReturnReady              bool
	AuthFilter                   string
	AuthSelection                int
	AuthProviderID               string
	AuthAPIKey                   string
	AuthCode                     string
	AuthPending                  bool
	Session                      protocol.SessionInfo
	Messages                     []transcriptMessage
	Attachments                  sessionclient.AttachmentSession
	Running                      bool
	AgentRunning                 bool
	TurnActivity                 string
	TurnThinking                 string
	FollowUps                    protocol.FollowUpQueue
	PendingInteractions          []protocol.InteractionRequest
	ContextTokens                int
	ContextWindow                int
	SessionUsage                 protocol.SessionUsage
	Scroll                       *ui.ScrollController
	TranscriptList               *ui.SliverListController
	TranscriptHistoryInitialized bool
	TranscriptHistoryHasMore     bool
	TranscriptHistoryLoading     bool
	TranscriptHistoryError       string
	ActivityScroll               *ui.ScrollController
	ActivityList                 *activityListController
	ActivityFocus                *ui.FocusNode
	WorkspaceLayout              *workspaceLayoutState
	ActivitySourceID             string
	ActivityConversationID       string
	ActivitySelected             bool
	SubagentsOpen                bool
	SubagentDefinitions          []protocol.SubagentDefinition
	SubagentDiagnostics          []protocol.SubagentDiagnostic
	SubagentConversations        []protocol.SubagentConversation
	SubagentSelection            string
	SubagentPaneID               string
	SubagentTranscripts          map[string]protocol.SubagentTranscript
	SubagentTranscriptErrors     map[string]string
	SubagentTranscriptOrder      []string
	SubagentScroll               *ui.ScrollController
	SubagentLive                 map[string]protocol.SubagentLiveEventPage
	SubagentDismissID            string
	SubagentDismissName          string
	SubagentDismissPending       bool
	SubagentDismissError         string
	InlineActivityOpen           map[string]bool
	ActivityExpanded             map[activityToolKey]bool
	ActivityCursor               activityToolKey
	BashRunning                  bool
	BashStarting                 bool
	BashCollapsed                map[string]bool
	BashHistory                  bashHistoryController
	FileMention                  fileMentionController
	Instructions                 auth.OpenAICodexDeviceInstructions
	BrowserInstructions          auth.AnthropicLoginInstructions
	Remaining                    time.Duration
	Location                     string
	Toasts                       []toastRecord
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
	ShowTranscript             ui.VoidCallback
	CloseActivity              ui.VoidCallback
	RetryTranscriptHistory     ui.VoidCallback
	CancelSubagentTask         func(ui.EventContext, string, uint64)
	DismissSubagent            func(ui.EventContext, string, uint64)
	SelectSubagent             func(ui.EventContext, string)
	MoveSubagentSelection      selectionMovedCallback
	ShowSubagentRoster         ui.VoidCallback
	OpenSubagentConversation   func(ui.EventContext, string)
	OpenSubagentActivity       func(ui.EventContext, string, string)
	OpenSubagentFromTool       func(ui.EventContext, string)
	CloseSubagentConversation  func(ui.EventContext, string)
	ScrollSubagentTranscript   func(ui.EventContext, string, int)
	ScrollActivity             func(ui.EventContext, int)
	SelectActivityTool         func(ui.EventContext, activityToolKey)
	ToggleBashOutput           func(ui.EventContext, string)
	OpenBashHistory            func(ui.EventContext, int) bool
	BashHistoryChanged         ui.TextChangedCallback
	SelectBashHistory          func(ui.EventContext, string)
	SelectFileMention          func(ui.EventContext, string)
	ComposerChanged            ui.TextChangedCallback
	ComposerPasted             ui.TextChangedCallback
	RemoveAttachment           func(ui.EventContext, int)
	RestoreFollowUps           ui.VoidCallback
	RespondInteraction         func(ui.EventContext, protocol.InteractionResponse, func(error))
	CopySelection              func(string)
	DismissToast               func(uint64)
	OpenPalette                ui.VoidCallback
	PaletteQueryChanged        ui.TextChangedCallback
	MovePaletteSelection       selectionMovedCallback
	RunPaletteQuery            ui.TextChangedCallback
	RunPaletteCommand          func(ui.EventContext, paletteCommandID)
	SelectTheme                func(ui.EventContext, int)
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

type movePaletteIntent struct{ Delta int }

func (movePaletteIntent) IntentType() ui.IntentType { return "kit.command-palette.move" }

type moveSubagentIntent struct{ Delta int }

func (moveSubagentIntent) IntentType() ui.IntentType { return "kit.subagents.move" }

type openSubagentIntent struct{}

func (openSubagentIntent) IntentType() ui.IntentType { return "kit.subagents.open" }

type cancelSubagentIntent struct{}

func (cancelSubagentIntent) IntentType() ui.IntentType { return "kit.subagents.cancel" }

type dismissSubagentIntent struct{}

func (dismissSubagentIntent) IntentType() ui.IntentType { return "kit.subagents.dismiss" }

func (w shellView) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	w.presentation = presentTranscript(w.Snapshot.Messages)
	content := ui.Widget(ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}},
		ui.SelectionArea{Child: w.baseShell(theme)},
	))
	overlays := w.authOverlays(theme)
	if w.Snapshot.Phase == phaseReady && w.Snapshot.FileMention.Open {
		controller := w.Snapshot.FileMention
		composerHeight := min(composerMaxHeight, max(1, strings.Count(w.Snapshot.Composer, "\n")+1))
		primaryPercent := 100
		if w.Snapshot.WorkspaceLayout != nil && w.Snapshot.WorkspaceLayout.Wide {
			primaryPercent = 60
		}
		overlays = append(overlays, ui.OverlayEntry{Child: fileMentionSurface{
			Controller: &controller, Composer: w.Snapshot.Composer,
			BottomInset: composerHeight + 4, PrimaryPercent: primaryPercent,
			OnSelect: w.Callbacks.SelectFileMention,
		}})
	}
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
	if w.Snapshot.Phase == phaseReady && w.Snapshot.SubagentDismissID != "" {
		overlays = append(overlays, modalDialogEntry(subagentDismissSurface{
			Name: w.Snapshot.SubagentDismissName, Pending: w.Snapshot.SubagentDismissPending, Error: w.Snapshot.SubagentDismissError,
		}))
	}
	if w.Snapshot.Phase == phaseReady && w.Snapshot.ThemePicker.Open {
		overlays = append(overlays, ui.OverlayEntry{Modal: true, Barrier: clearModalBarrier{}, Child: themePickerSurface{
			Snapshot:  w.Snapshot.ThemePicker,
			Callbacks: themePickerCallbacks{Select: w.Callbacks.SelectTheme},
		}})
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

	actions := map[ui.IntentType]ui.ActionFunc{
		quitIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.Quit != nil {
				w.Callbacks.Quit(ctx)
			}
			return ui.EventHandled
		},
		ui.NextFocusIntentType: func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			ctx.FocusNext()
			return ui.EventHandled
		},
		ui.PreviousFocusIntentType: func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			ctx.FocusPrevious()
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
			activityFocused := w.Snapshot.ActivityFocus == nil || w.Snapshot.ActivityFocus.HasFocus()
			if w.Snapshot.SubagentsOpen && w.Snapshot.ActivitySelected && activityFocused && !w.Snapshot.PaletteOpen && w.Snapshot.SubagentDismissID == "" {
				if w.Snapshot.SubagentPaneID != "" && w.Callbacks.ShowSubagentRoster != nil {
					w.Callbacks.ShowSubagentRoster(ctx)
				} else if w.Callbacks.CloseActivity != nil {
					w.Callbacks.CloseActivity(ctx)
				}
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
		workspaceOpen := w.Snapshot.SubagentsOpen
		secondaryPane := w.subagentsPane(theme)
		if w.Snapshot.SubagentPaneID != "" {
			secondaryPane = w.subagentTranscriptPane(theme, w.Snapshot.SubagentPaneID)
		}
		pending := w.pendingSlot(theme)
		pendingHeight := 1 + min(3, w.Snapshot.FollowUps.Count)
		if len(w.Snapshot.ComposerAttachments) > 0 {
			rows := make([]ui.Widget, 0, len(w.Snapshot.ComposerAttachments)+1)
			rows = append(rows, pending)
			for index, attachment := range w.Snapshot.ComposerAttachments {
				rows = append(rows, composerAttachmentRow(theme, attachment, index, w.Callbacks.RemoveAttachment))
			}
			pending = ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}
			pendingHeight += len(w.Snapshot.ComposerAttachments)
		}
		composer := w.composer(theme)
		composerHeightLimit := composerMaxHeight
		if len(w.Snapshot.PendingInteractions) > 0 {
			request := w.Snapshot.PendingInteractions[0]
			pending = ui.SizedBox{}
			pendingHeight = 0
			composerHeightLimit = 14
			composer = ui.Stack{Children: []ui.Widget{
				ui.Positioned{Left: 0, Top: 0, Child: ui.SizedBox{Height: 0, Child: composer}},
				interactionDock{Request: request, QueueLength: len(w.Snapshot.PendingInteractions), OnRespond: w.Callbacks.RespondInteraction},
			}}
		}
		body = ui.Expanded(conversationWorkspaceHost{
			Open: workspaceOpen, ActivitySelected: w.Snapshot.ActivitySelected,
			Tabs: w.workspaceTabs(theme), Transcript: w.body(theme),
			Pending: pending, PendingHeight: pendingHeight,
			ComposerSeparator: ui.Divider{Style: ui.Style{Foreground: w.composerSeparatorColor(theme), Background: theme.Background}},
			Composer:          composer, ComposerHeightLimit: composerHeightLimit,
			PaneSeparator: ui.Divider{Axis: ui.Vertical, Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
			Activity:      secondaryPane, SeparatorStyle: ui.Style{Foreground: theme.Border, Background: theme.Background},
			LayoutState: w.Snapshot.WorkspaceLayout,
		})
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		w.header(theme),
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
		body,
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
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
	var leading ui.Widget
	if w.Snapshot.TranscriptHistoryInitialized {
		status := ui.Widget(ui.Text{Value: "Beginning of conversation", Style: ui.Style{Foreground: theme.MutedForeground}})
		switch {
		case w.Snapshot.TranscriptHistoryLoading:
			status = spinnerWithLabel("Loading earlier messages…", ui.Style{Foreground: theme.MutedForeground})
		case w.Snapshot.TranscriptHistoryError != "":
			status = plainButton{Label: "Could not load earlier messages · retry", OnPressed: w.Callbacks.RetryTranscriptHistory}
		case w.Snapshot.TranscriptHistoryHasMore:
			status = ui.Text{Value: "↑ Scroll for earlier messages", Style: ui.Style{Foreground: theme.MutedForeground}}
		}
		leading = ui.SizedBox{Height: 1, Child: ui.Padding(ui.Insets{Left: 1, Right: 1}, status)}
	}
	return w.transcriptList(theme, presentation, true, "session:"+w.Snapshot.Session.ID, w.Snapshot.Scroll, w.Snapshot.TranscriptList, true, leading)
}

func (w shellView) transcriptList(theme ui.Theme, presentation transcriptPresentation, interactiveWork bool, identity string, controller *ui.ScrollController, listController *ui.SliverListController, followOutput bool, leading ui.Widget) ui.Widget {
	// Measured sliver extents are indexed, so include the first stable item in
	// the key. Appends retain measurements while session replacement and
	// compaction remount the list instead of applying stale heights to new rows.
	listKey := identity
	if len(presentation.Items) > 0 {
		listKey += ":" + presentation.Items[0].ID
	}
	slivers := make([]ui.Widget, 0, 2)
	if leading != nil {
		slivers = append(slivers, ui.SliverToBox{Child: leading})
	}
	slivers = append(slivers, keyedTranscriptItem{ID: "transcript-list:" + listKey, Child: ui.SliverListBuilder{
		Controller: listController, Count: len(presentation.Items), EstimatedItemExtent: 4, Overscan: 2,
		Builder: func(_ ui.BuildContext, index int) ui.Widget {
			item := presentation.Items[index]
			child := w.transcriptRow(theme, presentation, item, interactiveWork)
			insets := ui.Insets{Top: 1, Left: 1, Right: 1}
			if index == len(presentation.Items)-1 {
				insets.Bottom = 1
			}
			return keyedTranscriptItem{ID: item.ID, Child: ui.Padding(insets, child)}
		},
	}})
	return keyedTranscriptItem{ID: "transcript-viewport:" + identity, Child: ui.Scrollbar{Child: ui.CustomScrollView{
		Controller: controller, FollowOutput: followOutput, Slivers: slivers,
	}}}
}

func (w shellView) transcriptRow(theme ui.Theme, presentation transcriptPresentation, item transcriptDisplayItem, interactiveWork bool) ui.Widget {
	switch item.Kind {
	case transcriptDisplaySingle:
		if item.Item.Kind == transcriptItemBash && item.Item.Message.Bash != nil {
			execution := *item.Item.Message.Bash
			return transcriptBashEntry(theme, execution, w.Snapshot.BashCollapsed[execution.ID], func(ctx ui.EventContext) {
				if w.Callbacks.ToggleBashOutput != nil {
					w.Callbacks.ToggleBashOutput(ctx, execution.ID)
				}
			})
		}
		return transcriptUserEntry(theme, item.Item.Message, w.Snapshot.Attachments)
	case transcriptDisplayAssistantProse:
		return transcriptAssistantEntry(theme, item.Item.Message)
	case transcriptDisplayTurnWork:
		workView := w
		if !interactiveWork {
			workView.Snapshot.InlineActivityOpen = nil
			workView.Callbacks.OpenActivity = nil
		}
		return workView.transcriptWorkEntry(theme, item, presentation.ToolStates)
	}
	return nil
}

type keyedTranscriptItem struct {
	ID    string
	Child ui.Widget
}

func (w keyedTranscriptItem) WidgetKey() ui.KeyValue { return ui.KeyValue(w.ID) }

func (w keyedTranscriptItem) Build(ui.BuildContext) ui.Widget { return w.Child }

func (w shellView) transcriptWorkEntry(theme ui.Theme, item transcriptDisplayItem, toolStates map[transcriptToolStateKey]transcriptMessage) ui.Widget {
	children := []ui.Widget{w.transcriptWorkChip(theme, item, toolStates)}
	if w.Snapshot.Attachments == nil {
		return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStart, MainAxisSize: ui.MainAxisSizeMin, Children: children}
	}
	for _, workItem := range item.Items {
		for _, call := range assistantToolCalls(workItem.Message) {
			state, ok := toolStates[transcriptToolStateKey{TurnID: workItem.TurnID, ToolCallID: call.ID}]
			if !ok {
				continue
			}
			for _, block := range state.ToolContent {
				if block.Kind == protocol.TranscriptContentImage && block.AttachmentID != "" {
					children = append(children, ui.Padding(ui.Insets{Top: 1}, keyedTranscriptItem{
						ID:    "tool-image:" + block.AttachmentID,
						Child: attachmentPreview{Attachment: block, Loader: w.Snapshot.Attachments},
					}))
				}
			}
		}
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStart, MainAxisSize: ui.MainAxisSizeMin, Children: children}
}

func transcriptUserEntry(theme ui.Theme, message protocol.TranscriptMessage, attachments sessionclient.AttachmentSession) ui.Widget {
	children := make([]ui.Widget, 0, len(message.Content)+1)
	if text := message.TextContent(); text != "" {
		children = append(children, markdownView{ID: "transcript-user:" + message.ID, Source: text, BaseStyle: ui.Style{Foreground: theme.Foreground, Background: theme.Background}})
	}
	for _, block := range message.Content {
		if block.Kind == protocol.TranscriptContentImage && block.AttachmentID != "" && attachments != nil {
			children = append(children, attachmentPreview{Attachment: block, Loader: attachments})
		}
	}
	return ui.DecoratedBox(
		ui.Decoration{
			Style:  ui.Style{Background: theme.Background},
			Border: ui.Border{Style: ui.Style{Foreground: theme.PrimaryText, Background: theme.Background}, Left: true},
		},
		ui.Padding(ui.Insets{Left: 2}, ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStart, MainAxisSize: ui.MainAxisSizeMin, Children: children}),
	)
}

func transcriptAssistantEntry(theme ui.Theme, message protocol.TranscriptMessage) ui.Widget {
	style := ui.Style{Foreground: theme.Foreground, Background: theme.Background}
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
	failedCount := 0
	for _, step := range item.Items {
		aborted = aborted || step.Aborted
	}
	for _, call := range calls {
		state, exists := toolStates[transcriptToolStateKey{TurnID: item.TurnID, ToolCallID: call.ID}]
		resolved := resolveActivityToolState(state, exists, aborted)
		if resolved == activityToolFailed {
			failedCount++
		}
		if !aborted && (!exists || state.Pending) {
			inProgress = true
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
	expanded := w.Snapshot.InlineActivityOpen[item.ID]
	mutedStyle := ui.Style{Foreground: theme.MutedForeground, Background: theme.Background}
	rowStyle := ui.Style{Background: theme.Background}
	prefix := ui.Widget(ui.Text{Value: glyphChevronRight, Style: mutedStyle})
	if expanded {
		prefix = ui.Text{Value: glyphTriangleDown, Style: mutedStyle}
	} else if inProgress {
		prefix = spinner{Style: mutedStyle}
	}
	countSpans := []ui.TextSpan{{Text: countLabel, Style: mutedStyle}}
	if failedCount > 0 {
		failureLabel := fmt.Sprintf("%d failed", failedCount)
		if failedCount == 1 {
			failureLabel = "1 failed"
		}
		countSpans = append(countSpans, ui.TextSpan{
			Text:  " " + glyphMiddleDot + " " + failureLabel,
			Style: ui.Style{Foreground: theme.DangerText, Background: theme.Background},
		})
	}
	row := ui.DecoratedBox(ui.Decoration{Style: rowStyle}, ui.Padding(ui.Symmetric(1, 0), ui.Flex{
		Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			prefix,
			ui.SizedBox{Width: 1},
			ui.RichText{Spans: countSpans},
			ui.Expanded(ui.SizedBox{}),
		},
	}))
	activate := func(ctx ui.EventContext) {
		if w.Callbacks.OpenActivity != nil {
			w.Callbacks.OpenActivity(ctx, item.ID)
		}
	}
	header := mouseActivator{
		Child:     ui.SizedBox{Height: 1, Child: row},
		OnPressed: activate,
	}
	children := []ui.Widget{header}
	if expanded {
		children = append(children, inlineActivityWindow{
			ID: item.ID, Source: item, States: toolStates,
			List:     w.Snapshot.ActivityList,
			Expanded: w.Snapshot.ActivityExpanded, Cursor: w.Snapshot.ActivityCursor,
			OuterScroll:           w.Snapshot.Scroll,
			SubagentConversations: w.Snapshot.SubagentConversations,
			OnSelectTool:          w.Callbacks.SelectActivityTool,
			OnOpenSubagent:        w.Callbacks.OpenSubagentFromTool,
		})
	}
	return ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
}

func (w shellView) workspaceTabs(theme ui.Theme) ui.Widget {
	tabs := []ui.Widget{workspaceTab{Label: "Transcript", Selected: !w.Snapshot.ActivitySelected, OnSelect: w.Callbacks.ShowTranscript}}
	if w.Snapshot.SubagentsOpen {
		tabs = append(tabs, workspaceTab{
			Label: "Subagents", Selected: w.Snapshot.ActivitySelected && w.Snapshot.SubagentPaneID == "", Closable: true,
			OnSelect: w.Callbacks.ShowSubagentRoster, OnClose: w.Callbacks.CloseActivity,
		})
		for _, conversationID := range w.Snapshot.SubagentTranscriptOrder {
			conversationID := conversationID
			label := subagentConversationLabel(w.Snapshot.SubagentConversations, conversationID)
			tabs = append(tabs, workspaceTab{
				Label: label, Selected: w.Snapshot.ActivitySelected && w.Snapshot.SubagentPaneID == conversationID, Closable: true,
				OnSelect: func(ctx ui.EventContext) {
					if w.Callbacks.OpenSubagentConversation != nil {
						w.Callbacks.OpenSubagentConversation(ctx, conversationID)
					}
				},
				OnClose: func(ctx ui.EventContext) {
					if w.Callbacks.CloseSubagentConversation != nil {
						w.Callbacks.CloseSubagentConversation(ctx, conversationID)
					}
				},
			})
		}
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Height: 1, Child: ui.DecoratedBox(
			ui.Decoration{Style: ui.Style{Background: theme.Background}},
			ui.Flex{Axis: ui.Horizontal, Children: tabs},
		)},
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
	}}
}

func pendingActivityRow(theme ui.Theme, thinking, activity string) ui.Widget {
	style := ui.Style{Foreground: theme.MutedForeground}
	statusChildren := []ui.Widget(nil)
	var status ui.Widget
	if strings.TrimSpace(thinking) != "" {
		status = markdownInlineView{Source: latestThinkingLine(thinking), BaseStyle: style}
	} else if activity != "" {
		status = ui.Text{Value: activity, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}
	}
	if status != nil {
		statusChildren = []ui.Widget{spinner{Style: style}, ui.SizedBox{Width: 1}, ui.Expanded(status)}
	}
	return ui.SizedBox{Height: 1, Child: ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStart, Children: statusChildren}}
}

func (w shellView) pendingSlot(theme ui.Theme) ui.Widget {
	style := ui.Style{Foreground: theme.MutedForeground}
	rows := []ui.Widget{pendingActivityRow(theme, w.Snapshot.TurnThinking, w.Snapshot.TurnActivity)}
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
		CursorOffset:        w.Snapshot.ComposerCursorOffset,
		CursorGeneration:    w.Snapshot.ComposerCursorGeneration,
	}
	content := ui.Widget(ui.Provider[ui.Theme]{Value: composerTheme, Child: composer})
	if w.Snapshot.ActivitySelected {
		content = mouseActivator{Child: content, OnPrimaryDownCapture: w.Callbacks.ShowTranscript}
	}
	return content
}

func composerAttachmentRow(theme ui.Theme, attachment stagedAttachment, index int, remove func(ui.EventContext, int)) ui.Widget {
	label := attachment.Filename
	meta := "uploading…"
	style := ui.Style{Foreground: theme.MutedForeground}
	if attachment.Error != "" {
		meta = "upload failed"
		style.Foreground = theme.DangerText
	} else if !attachment.Uploading && attachment.Info.Size > 0 {
		meta = formatAttachmentBytes(attachment.Info.Size)
		if attachment.Info.MediaType != "" {
			meta += " " + glyphMiddleDot + " " + attachment.Info.MediaType
		}
	}
	removeControl := ui.Widget(ui.Text{Value: glyphTimes, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	if remove != nil {
		removeControl = mouseActivator{Child: removeControl, OnPressed: func(ctx ui.EventContext) { remove(ctx, index) }}
	}
	return ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Flex{
		Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			ui.Text{Value: "attachment", Style: ui.Style{Foreground: theme.AccentText}, MaxLines: 1},
			ui.Text{Value: " " + label + " ", Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
			ui.Expanded(ui.Text{Value: meta, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
			removeControl,
		},
	})}
}

func formatAttachmentBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(size)/(1024*1024))
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
				dialogDivider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
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
			Border: ui.BorderAll(ui.Style{Foreground: theme.Border, Background: theme.Background}),
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
