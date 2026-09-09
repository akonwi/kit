// Package tui implements Kit's native vaxis terminal client.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	defaultThinkingLevel = "medium"
	codexDefaultModel    = "openai-codex/gpt-5.6-sol"
)

// DeviceLogin performs an application-facing provider device login.
type DeviceLogin interface {
	Login(context.Context, func(auth.OpenAICodexDeviceInstructions) error) error
}

// APIKeyLogin installs an API-key credential and activates its provider.
type APIKeyLogin interface {
	Login(context.Context, string, string) error
}

// Options configures one native TUI client attached to one session.
type Options struct {
	Context              context.Context
	Server               sessionclient.Server
	CWD                  string
	Location             string
	ResolveLocation      func(context.Context, string) string
	DefaultModel         string
	DefaultThinking      string
	ResumeModelFilter    string
	ResumeThinkingFilter string
	AvailableProviders   map[string]bool
	Authenticated        bool
	SessionID            string
	NewSessionID         string
	NewSessionName       string
	TemporarySession     bool
	Login                DeviceLogin
	APIKeyLogin          APIKeyLogin

	appDone        <-chan struct{}
	terminalStatus *terminalStatusReporter
}

// Run starts the native terminal client and blocks until it exits.
func Run(options Options) error {
	if options.Server == nil {
		return errors.New("tui: session server is required")
	}
	if strings.TrimSpace(options.CWD) == "" {
		return errors.New("tui: working directory is required")
	}
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.DefaultThinking == "" {
		options.DefaultThinking = defaultThinkingLevel
	}
	if options.Authenticated && options.DefaultModel == "" {
		return errors.New("tui: default model is required when authenticated")
	}
	if options.SessionID != "" && options.NewSessionID != "" {
		return errors.New("tui: exact and new session selections are mutually exclusive")
	}
	if options.TemporarySession && options.NewSessionID == "" {
		return errors.New("tui: temporary session id is required")
	}
	if options.TemporarySession && options.SessionID != "" {
		return errors.New("tui: temporary and exact session selections are mutually exclusive")
	}
	runContext, cancel := context.WithCancel(options.Context)
	done := make(chan struct{})
	options.Context = runContext
	options.appDone = done
	options.terminalStatus = newTerminalStatusReporter()
	defer options.terminalStatus.Close()
	err := ui.Run(app{Options: options}, ui.WithShortcuts(nativeRootShortcuts()))
	close(done)
	cancel()
	return err
}

func nativeRootShortcuts() ui.ShortcutMap { return ui.ShortcutMap{} }

type phase int

const (
	phaseLoading phase = iota
	phaseAuthGate
	phaseAuthSelect
	phaseAuthWaiting
	phaseAuthAPIKey
	phaseReady
	phaseFailed
)

type transcriptMessage struct {
	ID                     string
	TurnID                 string
	Role                   string
	Text                   string
	Thinking               string
	ToolCallID             string
	ToolCalls              []transcriptToolCall
	ToolName               string
	ToolArguments          string
	ToolArgumentsTruncated bool
	ToolStatus             string
	ToolContent            []protocol.TranscriptContent
	ToolContentTruncated   bool
	ToolDetails            json.RawMessage
	ToolDetailsOmitted     bool
	IsError                bool
	Aborted                bool
	Pending                bool
	Bash                   *protocol.BashExecution
}

type liveContentBlock struct {
	kind protocol.SessionEventKind
	text string
}

type app struct{ Options Options }

type promptAdmission struct {
	abort atomic.Bool
}

func (a app) CreateState() ui.State { return &appState{} }

type appState struct {
	ui.StateBase

	ctx              context.Context
	cancel           context.CancelFunc
	attachmentCtx    context.Context
	attachmentCancel context.CancelFunc

	phase                       phase
	errorText                   string
	status                      string
	toasts                      toastController
	toastCancels                map[uint64]context.CancelFunc
	showToastOverride           func(toastInput)
	composer                    string
	composerCursorEndGeneration uint64
	palette                     paletteController
	configurationPicker         configurationPickerController
	compactPending              bool
	compactOperationID          string
	sessionDetailsOpen          bool
	sessionExplorer             sessionExplorerController
	authReturnReady             bool
	authFilter                  string
	authSelection               int
	authProviderID              string
	authAPIKey                  string
	authPending                 bool
	session                     protocol.SessionInfo
	bound                       sessionclient.Session
	location                    string
	sessionDrafts               map[string]string
	sessionSwitchCancel         context.CancelFunc
	sessionSwitchGeneration     uint64
	messages                    []transcriptMessage
	liveMessages                []transcriptMessage
	liveAssistant               int
	liveHasUser                 bool
	liveTools                   map[string]int
	liveContent                 map[int]liveContentBlock
	liveSequence                int64
	turnActivity                string
	turnThinking                string
	runStopping                 bool
	contextTokens               int
	contextWindow               int
	sessionUsage                protocol.SessionUsage
	scroll                      ui.ScrollController
	activityScroll              ui.ScrollController
	activityList                activityListController
	activityFocus               ui.FocusNode
	workspaceLayout             workspaceLayoutState
	activitySourceID            string
	activitySelected            bool
	hoveredActivityID           string
	activityExpanded            map[activityToolKey]bool
	activityCursor              activityToolKey
	activityReveal              activityToolKey
	activityRevealPending       bool
	activityRevealPendingLayout bool
	needsScroll                 bool
	scrollPendingLayout         bool
	activityNeedsScroll         bool
	activityPendingLayout       bool
	activityScrollToEnd         bool
	activeRun                   sessionclient.Run
	activeRunID                 string
	runPending                  bool
	terminalRunActive           bool
	terminalRunID               string
	terminalSettledRunID        string
	agentFeedbackPending        bool
	reloadPending               bool
	cwdPending                  bool
	prompt                      *promptAdmission
	activeBash                  sessionclient.BashExecution
	activeBashID                string
	bashStarting                bool
	bashAdmission               *bashAdmission
	bashCollapsed               map[string]bool
	bashHistory                 bashHistoryController

	instructions      auth.OpenAICodexDeviceInstructions
	remaining         time.Duration
	loginCancel       context.CancelFunc
	loginGeneration   uint64
	operation         uint64
	bootstrapModel    string
	bootstrapThinking string
	newSessionPending bool
	bootstrapTarget   protocol.SessionInfo

	availableMu    sync.RWMutex
	available      map[string]bool
	terminalCWD    string
	terminalStatus *terminalStatusReporter
}

func (s *appState) InitState() {
	options := s.Widget().(app).Options
	s.ctx, s.cancel = context.WithCancel(options.Context)
	s.resetAttachmentContext()
	s.available = cloneProviders(options.AvailableProviders)
	s.terminalCWD = options.CWD
	s.terminalStatus = options.terminalStatus
	s.liveAssistant = -1
	s.liveTools = make(map[string]int)
	s.liveContent = make(map[int]liveContentBlock)
	s.activityExpanded = make(map[activityToolKey]bool)
	s.bashCollapsed = make(map[string]bool)
	s.toastCancels = make(map[uint64]context.CancelFunc)
	s.location = options.Location
	s.sessionDrafts = make(map[string]string)
	s.newSessionPending = options.NewSessionID != ""
	if options.Authenticated {
		s.phase = phaseLoading
		s.status = "Starting Kit…"
		s.startBootstrap(options.DefaultModel, options.DefaultThinking)
	} else {
		s.phase = phaseAuthGate
	}

	runtime := s.Context().Runtime()
	eventContext := s.Context().EventContext()
	go func() {
		select {
		case <-options.appDone:
			return
		case <-options.Context.Done():
			select {
			case <-options.appDone:
				return
			default:
				runtime.Dispatch(func() { eventContext.Quit() })
			}
		}
	}()
}

func (s *appState) TickFrame(now time.Time) bool {
	if s.terminalStatus != nil {
		s.syncTerminalStatus(now, s.Context().EventContext().SetTitle)
	}
	keepTicking := false
	if s.needsScroll {
		if s.scrollPendingLayout {
			// Live events can arrive before the deferred follow-up frame. Apply the
			// latest completed layout now so repeated updates cannot starve follow.
			if s.scroll.Attached() {
				s.scroll.ScrollToEnd()
			}
			s.scrollPendingLayout = false
			keepTicking = true
		} else {
			if s.scroll.Attached() {
				s.scroll.ScrollToEnd()
			}
			s.needsScroll = false
		}
	}
	if s.activityNeedsScroll {
		if s.activityPendingLayout {
			// Keep following the last completed layout even when another live event
			// resets the post-layout request before its follow-up frame can run.
			if s.activityScroll.Attached() {
				if s.activityScrollToEnd {
					s.activityScroll.ScrollToEnd()
				} else {
					s.activityScroll.ScrollToStart()
				}
			}
			s.activityPendingLayout = false
			keepTicking = true
		} else {
			if s.activityScroll.Attached() {
				if s.activityScrollToEnd {
					s.activityScroll.ScrollToEnd()
				} else {
					s.activityScroll.ScrollToStart()
				}
			}
			s.activityNeedsScroll = false
		}
	}
	if s.sessionExplorer.TickFrame() {
		keepTicking = true
	}
	if s.activityRevealPending {
		if s.activityRevealPendingLayout {
			s.activityRevealPendingLayout = false
			keepTicking = true
		} else if s.activityList.Attached() {
			s.activityList.Reveal(s.activityReveal)
			s.activityReveal = activityToolKey{}
			s.activityRevealPending = false
		} else {
			keepTicking = true
		}
	}
	return keepTicking || s.needsScroll || s.activityNeedsScroll || s.activityRevealPending
}

func (s *appState) syncTerminalStatus(now time.Time, setTitle func(string)) {
	if s.terminalStatus == nil {
		return
	}
	name, cwd := s.session.Name, s.session.CWD
	if cwd == "" {
		cwd = s.terminalCWD
	}
	s.terminalStatus.Update(now, name, cwd, s.terminalRunID, resolveTerminalStatus(s.terminalRunActive, s.agentFeedbackPending), setTitle)
}

func (s *appState) showToast(input toastInput) {
	if s.showToastOverride != nil {
		s.showToastOverride(input)
		return
	}
	if strings.TrimSpace(input.Title) == "" {
		return
	}
	var result toastShowResult
	var toastContext context.Context
	s.SetState(func() {
		result = s.toasts.Show(input)
		for _, evicted := range result.Evicted {
			if cancel := s.toastCancels[evicted]; cancel != nil {
				cancel()
				delete(s.toastCancels, evicted)
			}
		}
		if result.Retained && !input.Persistent {
			var cancel context.CancelFunc
			toastContext, cancel = context.WithCancel(s.ctx)
			s.toastCancels[result.ID] = cancel
		}
	})
	if input.Persistent || !result.Retained {
		return
	}
	runtime := s.Context().Runtime()
	go func() {
		timer := time.NewTimer(toastLifetime)
		defer timer.Stop()
		select {
		case <-toastContext.Done():
			return
		case <-timer.C:
		}
		runtime.Dispatch(func() {
			if s.ctx.Err() == nil {
				s.dismissToast(result.ID)
			}
		})
	}()
}

func (s *appState) dismissToast(id uint64) {
	s.SetState(func() {
		if cancel := s.toastCancels[id]; cancel != nil {
			cancel()
			delete(s.toastCancels, id)
		}
		s.toasts.Dismiss(id)
	})
}

func (s *appState) requestTranscriptScroll() {
	s.needsScroll = true
	s.scrollPendingLayout = true
}

func (s *appState) requestActivityScroll(toEnd bool) {
	s.activityNeedsScroll = true
	s.activityPendingLayout = true
	s.activityScrollToEnd = toEnd
}

func (s *appState) resetAttachmentContext() {
	if s.attachmentCancel != nil {
		s.attachmentCancel()
	}
	parent := s.ctx
	if parent == nil {
		parent = context.Background()
	}
	s.attachmentCtx, s.attachmentCancel = context.WithCancel(parent)
}

func (s *appState) Dispose() {
	if s.loginCancel != nil {
		s.loginCancel()
	}
	if s.sessionSwitchCancel != nil {
		s.sessionSwitchCancel()
	}
	if s.attachmentCancel != nil {
		s.attachmentCancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *appState) Build(ctx ui.BuildContext) ui.Widget {
	presentedMessages := make([]transcriptMessage, 0, len(s.messages)+len(s.liveMessages))
	presentedMessages = append(presentedMessages, s.messages...)
	presentedMessages = append(presentedMessages, s.liveMessages...)
	snapshot := shellSnapshot{
		Phase:                       s.phase,
		Error:                       s.errorText,
		Status:                      s.status,
		Composer:                    s.composer,
		ComposerCursorEndGeneration: s.composerCursorEndGeneration,
		PaletteOpen:                 s.palette.Open,
		PaletteQuery:                s.palette.Query,
		PaletteSelection:            s.palette.Selection,
		PaletteCommands:             s.palette.Contributions,
		ConfigurationPicker:         s.configurationPicker.Snapshot(),
		SessionDetailsOpen:          s.sessionDetailsOpen,
		SessionExplorer:             s.sessionExplorer.Snapshot(),
		AuthReturnReady:             s.authReturnReady,
		AuthFilter:                  s.authFilter,
		AuthSelection:               s.authSelection,
		AuthProviderID:              s.authProviderID,
		AuthAPIKey:                  s.authAPIKey,
		AuthPending:                 s.authPending,
		Session:                     s.session,
		Messages:                    presentedMessages,
		Running:                     s.hasActiveWork(),
		AgentRunning:                s.runPending,
		TurnActivity:                s.turnActivity,
		TurnThinking:                s.turnThinking,
		ContextTokens:               s.contextTokens,
		ContextWindow:               s.contextWindow,
		SessionUsage:                s.sessionUsage,
		Scroll:                      &s.scroll,
		ActivityScroll:              &s.activityScroll,
		ActivityList:                &s.activityList,
		ActivityFocus:               &s.activityFocus,
		WorkspaceLayout:             &s.workspaceLayout,
		ActivitySourceID:            s.activitySourceID,
		ActivitySelected:            s.activitySelected,
		HoveredActivityID:           s.hoveredActivityID,
		ActivityExpanded:            s.activityExpanded,
		ActivityCursor:              s.activityCursor,
		BashRunning:                 s.activeBashID != "",
		BashStarting:                s.bashStarting,
		BashCollapsed:               s.bashCollapsed,
		BashHistory:                 s.bashHistory,
		Instructions:                s.instructions,
		Remaining:                   s.remaining,
		Location:                    s.location,
		Toasts:                      s.toasts.Snapshot(),
	}
	callbacks := shellCallbacks{
		OpenAuth: func(ui.EventContext) {
			if s.phase == phaseAuthGate {
				s.enterAuthSelect(false)
			}
		},
		SelectProvider: s.selectProvider,
		MoveProviderSelection: func(_ ui.EventContext, delta int) {
			s.moveProviderSelection(delta)
		},
		AuthFilterChanged: func(_ ui.EventContext, value string) {
			s.SetState(func() {
				s.authFilter = value
				s.authSelection = 0
			})
		},
		AuthAPIKeyChanged: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.authAPIKey = value })
		},
		SubmitAPIKey: s.submitAPIKey,
		OpenURL: func(ctx ui.EventContext, raw string) {
			if err := openExternalURL(raw); err != nil {
				s.showToast(toastInput{Title: "Could not open browser", Subtitle: err.Error(), Variant: toastError})
				ctx.Notify("Could not open browser", err.Error())
				return
			}
			s.showToast(toastInput{Title: "Opened browser", Variant: toastInfo})
		},
		HoverActivity: func(_ ui.EventContext, sourceID string) {
			if s.hoveredActivityID != sourceID {
				s.SetState(func() { s.hoveredActivityID = sourceID })
			}
		},
		OpenActivity: func(_ ui.EventContext, sourceID string) {
			s.SetState(func() {
				presentation := presentTranscript(presentedMessages)
				changed := s.activitySourceID != sourceID
				s.activitySourceID = sourceID
				s.activitySelected = !s.workspaceLayout.Wide
				if changed {
					s.activityExpanded = make(map[activityToolKey]bool)
					s.activityCursor = activityToolKey{}
					s.activityReveal = activityToolKey{}
					s.activityRevealPending = false
					s.activityRevealPendingLayout = false
				}
				if s.activitySelected {
					keys := activityToolKeys(presentation, sourceID)
					if len(keys) > 0 {
						s.activityCursor = keys[0]
					}
				}
				if changed {
					s.requestActivityScroll(transcriptActivityInProgress(presentation, sourceID))
				}
			})
		},
		ShowTranscript: func(ctx ui.EventContext) {
			if s.activityFocus.HasFocus() {
				ctx.FocusNext()
			}
			s.SetState(func() { s.activitySelected = false })
		},
		ShowActivity: func(ui.EventContext) {
			if s.activitySourceID != "" {
				s.SetState(func() {
					presentation := presentTranscript(presentedMessages)
					s.activitySelected = !s.workspaceLayout.Wide
					if s.activitySelected && s.activityCursor.ToolCallID == "" {
						keys := activityToolKeys(presentation, s.activitySourceID)
						if len(keys) > 0 {
							s.activityCursor = keys[0]
						}
					}
					s.requestActivityScroll(transcriptActivityInProgress(presentation, s.activitySourceID))
				})
			}
		},
		CloseActivity: func(ctx ui.EventContext) {
			if s.activityFocus.HasFocus() {
				ctx.FocusNext()
			}
			s.SetState(func() {
				s.activitySourceID = ""
				s.activitySelected = false
				s.hoveredActivityID = ""
				s.activityExpanded = make(map[activityToolKey]bool)
				s.activityCursor = activityToolKey{}
				s.activityReveal = activityToolKey{}
				s.activityRevealPending = false
				s.activityRevealPendingLayout = false
			})
		},
		ScrollActivity: func(_ ui.EventContext, pages int) {
			if (s.activitySelected || s.workspaceLayout.Wide) && s.activityScroll.Attached() {
				s.activityScroll.ScrollByPages(pages)
			}
		},
		ToggleActivityTool: func(_ ui.EventContext, key activityToolKey) {
			s.SetState(func() {
				expanding := !s.activityExpanded[key]
				s.activityExpanded[key] = expanding
				if expanding {
					s.activityReveal = key
					s.activityRevealPending = true
					s.activityRevealPendingLayout = true
				}
			})
		},
		SelectActivityTool: func(_ ui.EventContext, key activityToolKey) {
			s.SetState(func() { s.activityCursor = key })
		},
		MoveActivityTool: func(_ ui.EventContext, delta int) {
			s.SetState(func() {
				presentation := presentTranscript(presentedMessages)
				keys := activityToolKeys(presentation, s.activitySourceID)
				s.activityCursor = moveActivityToolCursor(keys, s.activityCursor, delta)
				if source, ok := transcriptActivitySource(presentation.Items, s.activitySourceID); ok {
					if index := activityToolListIndex(source, s.activityCursor); index >= 0 {
						s.activityList.Reveal(s.activityCursor)
					}
				}
			})
		},
		ToggleBashOutput: func(_ ui.EventContext, executionID string) {
			s.SetState(func() { s.bashCollapsed[executionID] = !s.bashCollapsed[executionID] })
		},
		OpenBashHistory: func(_ ui.EventContext, delta int) bool {
			return s.openBashHistory(delta)
		},
		BashHistoryChanged: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.bashHistory.SetQuery(value) })
		},
		SelectBashHistory: func(ctx ui.EventContext, executionID string) {
			s.selectBashHistory(ctx, executionID)
		},
		CopyCode: func(ctx ui.EventContext) {
			if s.instructions.UserCode != "" {
				ctx.Copy(s.instructions.UserCode)
				s.showToast(toastInput{Title: "Device code copied", Variant: toastInfo})
			}
		},
		CopySelection: func(string) {
			s.showToast(toastInput{Title: "Copied to clipboard", Variant: toastInfo})
		},
		DismissToast: s.dismissToast,
		ComposerPasted: func(_ ui.EventContext, value string) {
			if s.phase != phaseReady {
				return
			}
			metrics := s.scroll.Metrics()
			followTranscript := s.scroll.Attached() && metrics.ScrollOffset >= metrics.MaxScrollOffset
			s.SetState(func() {
				s.composer = value
				if followTranscript {
					s.requestTranscriptScroll()
				}
			})
		},
		ComposerChanged: func(_ ui.EventContext, value string) {
			if s.phase != phaseReady {
				return
			}
			metrics := s.scroll.Metrics()
			followTranscript := s.scroll.Attached() && metrics.ScrollOffset >= metrics.MaxScrollOffset
			s.SetState(func() {
				composer, intercepted := s.palette.HandleComposerChange(s.composer, value, s.hasActiveWork())
				if intercepted {
					return
				}
				s.composer = composer
				if followTranscript {
					s.requestTranscriptScroll()
				}
			})
		},
		OpenPalette: func(ui.EventContext) {
			s.openPalette()
		},
		PaletteQueryChanged: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.palette.SetQuery(s.hasActiveWork(), value) })
		},
		MovePaletteSelection: func(_ ui.EventContext, delta int) {
			s.movePaletteSelection(delta)
		},
		RunPaletteQuery:   s.runPaletteQuery,
		RunPaletteCommand: s.runPaletteCommand,
		OpenModel: func(ui.EventContext) {
			s.openConfigurationPicker(configurationPickerModel)
		},
		OpenThinking: func(ui.EventContext) {
			s.openConfigurationPicker(configurationPickerThinking)
		},
		ConfigurationQuery: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.configurationPicker.SetQuery(value) })
		},
		SelectConfiguration: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.configurationPicker.Select(value) })
		},
		ApplyConfiguration: func(ui.EventContext) {
			s.applyConfigurationSelection()
		},
		SelectSession: func(_ ui.EventContext, sessionID string) {
			s.SetState(func() { s.sessionExplorer.Select(sessionID) })
		},
		RenameSessionChanged: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.sessionExplorer.SetRenameText(value) })
		},
		SubmitSessionRename: func(_ ui.EventContext, value string) {
			s.renameSelectedSession(value)
		},
		Submit: func(ctx ui.EventContext, value string) {
			if s.phase != phaseReady {
				return
			}
			if s.palette.Open {
				s.runPaletteQuery(ctx, s.palette.Query)
				return
			}
			s.submit(ctx, value)
		},
		Retry: func(ui.EventContext) {
			s.SetState(func() {
				s.phase = phaseLoading
				s.errorText = ""
				s.status = "Starting Kit…"
			})
			s.startBootstrap(s.bootstrapModel, s.bootstrapThinking)
		},
		Quit: func(ctx ui.EventContext) {
			if s.configurationPicker.Mode != configurationPickerClosed {
				s.SetState(func() { s.configurationPicker.Close() })
				return
			}
			if s.sessionDetailsOpen {
				s.SetState(func() { s.sessionDetailsOpen = false })
				return
			}
			if s.sessionExplorer.Open {
				if s.sessionExplorer.RenamePending || s.sessionExplorer.DeletePending {
					return
				}
				s.cancelSessionSwitch()
				s.SetState(func() { s.sessionExplorer.Close() })
				return
			}
			if s.bashHistory.Open {
				s.SetState(func() { s.bashHistory.Close() })
				return
			}
			if s.palette.Open {
				s.SetState(func() { s.palette.Close() })
				return
			}
			if s.composer != "" && s.phase == phaseReady {
				s.SetState(func() { s.composer = "" })
				return
			}
			ctx.Quit()
		},
		Dismiss: s.dismiss,
	}
	return shellView{Snapshot: snapshot, Callbacks: callbacks}
}

func (s *appState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase {
		return ui.EventIgnored
	}
	key, ok := event.(ui.Key)
	if !ok {
		return ui.EventIgnored
	}
	if s.configurationPicker.Mode != configurationPickerClosed {
		var apply, handled bool
		s.SetState(func() {
			apply, handled = s.configurationPicker.HandleKey(key)
			if !handled {
				handled = s.configurationPicker.HandleEditorKey(key)
			}
		})
		if !handled {
			return ui.EventIgnored
		}
		if apply {
			s.applyConfigurationSelection()
		}
		return ui.EventHandled
	}
	if s.sessionExplorer.Open {
		if s.sessionExplorer.DeleteOpen {
			if key.EventType == ui.EventRelease {
				return ui.EventHandled
			}
			if key.EventType != vaxis.EventPaste && key.MatchString("Enter") {
				s.deleteSelectedSession()
				return ui.EventHandled
			}
			if key.MatchString("Escape") || key.MatchString("Ctrl+c") {
				return ui.EventIgnored
			}
			return ui.EventHandled
		}
		if s.sessionExplorer.RenameOpen {
			if key.EventType == ui.EventRelease {
				return ui.EventHandled
			}
			if key.EventType != vaxis.EventPaste && key.MatchString("Enter") {
				s.renameSelectedSession(s.sessionExplorer.RenameText)
				return ui.EventHandled
			}
			if key.MatchString("Escape") || key.MatchString("Ctrl+c") {
				return ui.EventIgnored
			}
			if s.sessionExplorer.RenamePending {
				return ui.EventHandled
			}
			return ui.EventIgnored
		}
		if key.EventType != ui.EventRelease && key.EventType != vaxis.EventPaste && key.MatchString("Enter") {
			s.switchSelectedSession()
			return ui.EventHandled
		}
		var handled bool
		s.SetState(func() { handled = s.sessionExplorer.HandleKey(key) })
		if handled {
			return ui.EventHandled
		}
	}
	if s.bashHistory.Open {
		var entry bashHistoryEntry
		var selectEntry, handled bool
		s.SetState(func() {
			entry, selectEntry, handled = s.bashHistory.HandleKey(key)
			if !handled {
				handled = s.bashHistory.HandleEditorKey(key)
			}
		})
		if !handled {
			return ui.EventIgnored
		}
		if selectEntry {
			s.selectBashHistory(ctx, entry.ID)
		}
		return ui.EventHandled
	}
	if !s.palette.Open {
		return ui.EventIgnored
	}
	var command paletteCommand
	var run, handled bool
	s.SetState(func() {
		command, run, handled = s.palette.HandleKey(s.hasActiveWork(), key)
		if !handled {
			handled = s.palette.HandleEditorKey(s.hasActiveWork(), key)
		}
	})
	if !handled {
		return ui.EventIgnored
	}
	if run {
		s.runPaletteCommand(ctx, command.ID)
	}
	return ui.EventHandled
}

func (s *appState) startBootstrap(defaultModel, defaultThinking string) {
	s.bootstrapModel = defaultModel
	s.bootstrapThinking = defaultThinking
	options := s.Widget().(app).Options
	newSession := s.newSessionPending
	newSessionID := ""
	if newSession {
		newSessionID = options.NewSessionID
	}
	target := s.bootstrapTarget
	runtime := s.Context().Runtime()
	s.operation++
	operation := s.operation
	go func() {
		info, bound, snapshot, err := bootstrapSession(
			s.ctx, options.Server, options.CWD, defaultModel, defaultThinking,
			options.ResumeModelFilter, options.ResumeThinkingFilter,
			options.SessionID, newSession, newSessionID, options.NewSessionName,
			options.TemporarySession, target, s.providerAvailable,
		)
		if s.ctx.Err() != nil {
			return
		}
		location := options.Location
		if err == nil && info.CWD != "" && options.ResolveLocation != nil {
			location = options.ResolveLocation(s.ctx, info.CWD)
		}
		runtime.Dispatch(func() {
			if operation != s.operation {
				return
			}
			if err != nil {
				s.SetState(func() {
					if newSession && info.ID != "" {
						s.newSessionPending = false
						s.bootstrapTarget = info
					}
					s.phase = phaseFailed
					s.errorText = err.Error()
					s.status = ""
				})
				return
			}
			running := snapshot.ActiveRunID != ""
			activeBashID := snapshot.ActiveBashExecutionID
			s.SetState(func() {
				s.phase = phaseReady
				s.newSessionPending = false
				s.bootstrapTarget = protocol.SessionInfo{}
				s.session = info
				s.bound = bound
				s.location = location
				s.applySnapshot(snapshot)
				if running {
					s.status = "esc abort · ctrl+c detach"
				}
			})
			for _, warning := range snapshot.Warnings {
				s.showToast(toastInput{Title: "Configuration adjusted", Subtitle: warning, Variant: toastWarning})
			}
			if running {
				s.watchSession(bound, operation, snapshot.ActiveRunID)
			}
			if activeBashID != "" {
				s.resumeBash(bound, operation, activeBashID)
			}
		})
	}()
}

func bootstrapSession(
	ctx context.Context,
	server sessionclient.Server,
	cwd, defaultModel, defaultThinking string,
	resumeModelFilter, resumeThinkingFilter string,
	sessionSelector string,
	newSession bool,
	newSessionID string,
	newSessionName string,
	temporary bool,
	target protocol.SessionInfo,
	providerAvailable func(string) bool,
) (protocol.SessionInfo, sessionclient.Session, protocol.SessionSnapshot, error) {
	if newSession && newSessionID == "" {
		return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, errors.New("new session id is required")
	}
	selected := target
	if selected.ID == "" && sessionSelector != "" {
		var err error
		selected, err = sessionclient.ResolveSession(ctx, server, sessionSelector)
		if err != nil {
			return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, fmt.Errorf("resolve session: %w", err)
		}
	}
	if selected.ID == "" && !newSession {
		sessions, err := server.ListSessions(ctx, cwd)
		if err != nil {
			return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, fmt.Errorf("list sessions: %w", err)
		}
		for _, candidate := range sessions {
			if providerAvailable(candidate.Model) &&
				(resumeModelFilter == "" || candidate.Model == resumeModelFilter) &&
				(resumeThinkingFilter == "" || candidate.ThinkingLevel == resumeThinkingFilter) {
				selected = candidate
				break
			}
		}
	}
	var err error
	if selected.ID == "" {
		if defaultModel == "" {
			return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, errors.New("no authenticated model is available")
		}
		if !providerAvailable(defaultModel) {
			return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, fmt.Errorf("model provider for %q is not authenticated", defaultModel)
		}
		selected, err = server.CreateSession(ctx, protocol.CreateSessionInput{
			ID:            newSessionID,
			CWD:           cwd,
			Name:          newSessionName,
			Model:         defaultModel,
			ThinkingLevel: defaultThinking,
			Temporary:     temporary,
		})
		if err != nil {
			return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, fmt.Errorf("create session: %w", err)
		}
		if newSessionID != "" && selected.ID != newSessionID {
			return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, errors.New("create session returned a different session id")
		}
	}
	bound, err := server.Attach(ctx, selected.ID)
	if err != nil {
		return selected, nil, protocol.SessionSnapshot{}, fmt.Errorf("attach session: %w", err)
	}
	snapshot, err := bound.Snapshot(ctx)
	if err != nil {
		return selected, nil, protocol.SessionSnapshot{}, fmt.Errorf("snapshot session: %w", err)
	}
	return selected, bound, snapshot, nil
}

func (s *appState) applySessionMetadataSnapshot(snapshot protocol.SessionSnapshot) {
	if snapshot.Session.ID != "" {
		s.session = snapshot.Session
	}
	s.palette.SetContributions(promptPaletteCommands(snapshot.PromptCommands), s.hasActiveWork())
	s.contextTokens = snapshot.ContextTokens
	s.contextWindow = snapshot.ContextWindow
	s.sessionUsage = snapshot.Usage
}

func (s *appState) applySnapshot(snapshot protocol.SessionSnapshot) {
	s.palette.SetContributions(promptPaletteCommands(snapshot.PromptCommands), s.hasActiveWork())
	if snapshot.Session.ID != "" {
		s.session = snapshot.Session
	}
	currentActiveBash, hasCurrentActiveBash := findBashExecution(s.messages, s.liveMessages, s.activeBashID)
	projected := projectTranscript(snapshot.Messages)
	for index := range projected {
		if projected[index].Bash == nil || projected[index].Bash.Status != protocol.BashExecutionRunning {
			continue
		}
		if current, ok := findBashExecution(s.messages, s.liveMessages, projected[index].ID); ok && current.Status != protocol.BashExecutionRunning {
			copy := cloneBashExecution(current)
			projected[index].Bash = &copy
			projected[index].Pending = false
		}
	}
	if hasCurrentActiveBash {
		if _, found := findBashExecution(projected, nil, currentActiveBash.ID); !found {
			copy := cloneBashExecution(currentActiveBash)
			projected = append(projected, transcriptMessage{
				ID: copy.ID, Role: "bash", Bash: &copy,
				Pending: copy.Status == protocol.BashExecutionRunning,
			})
		}
	}
	s.messages = projected
	s.resetLiveRun()
	if s.activitySourceID != "" {
		presentation := presentTranscript(s.messages)
		if source, ok := transcriptActivitySource(presentation.Items, s.activitySourceID); ok {
			valid := make(map[activityToolKey]bool)
			for _, call := range displayItemToolCalls(source) {
				valid[activityToolKey{TurnID: source.TurnID, ToolCallID: call.ID}] = true
			}
			for key := range s.activityExpanded {
				if !valid[key] {
					delete(s.activityExpanded, key)
				}
			}
			if !valid[s.activityCursor] {
				s.activityCursor = activityToolKey{}
				if s.activitySelected {
					keys := activityToolKeys(presentation, s.activitySourceID)
					if len(keys) > 0 {
						s.activityCursor = keys[0]
					}
				}
			}
			s.requestActivityScroll(false)
		} else {
			s.activitySourceID = ""
			s.activitySelected = false
			s.hoveredActivityID = ""
			s.activityExpanded = make(map[activityToolKey]bool)
			s.activityCursor = activityToolKey{}
			s.activityReveal = activityToolKey{}
			s.activityRevealPending = false
			s.activityRevealPendingLayout = false
		}
	}
	s.requestTranscriptScroll()
	s.contextTokens = snapshot.ContextTokens
	s.contextWindow = snapshot.ContextWindow
	s.sessionUsage = snapshot.Usage
	s.activeRunID = snapshot.ActiveRunID
	s.runPending = snapshot.ActiveRunID != ""
	if snapshot.ActiveRunID != "" && snapshot.ActiveRunID != s.terminalSettledRunID {
		s.markTerminalRunStarted(snapshot.ActiveRunID)
	}
	if snapshot.ActiveBashExecutionID != "" || s.activeBash == nil {
		s.activeBashID = snapshot.ActiveBashExecutionID
	} else if execution, found := findBashExecution(s.messages, nil, s.activeBashID); found && execution.Status != protocol.BashExecutionRunning {
		s.activeBash = nil
		s.activeBashID = ""
	}
	if s.runPending {
		s.turnActivity = "Working…"
	}
	if !s.runPending {
		s.activeRun = nil
		s.prompt = nil
		s.status = ""
	}
}

func projectTranscript(messages []protocol.TranscriptMessage) []transcriptMessage {
	result := make([]transcriptMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role == "bash" {
			if message.Bash == nil {
				continue
			}
			execution := cloneBashExecution(*message.Bash)
			result = append(result, transcriptMessage{
				ID: message.ID, Role: "bash", Bash: &execution,
				Pending: execution.Status == protocol.BashExecutionRunning,
			})
			continue
		}
		var textParts, thinkingParts []string
		calls := make([]transcriptToolCall, 0)
		for _, block := range message.Content {
			switch block.Kind {
			case protocol.TranscriptContentText:
				textParts = append(textParts, block.Text)
			case protocol.TranscriptContentThinking:
				thinkingParts = append(thinkingParts, block.Text)
			case protocol.TranscriptContentToolCall:
				calls = append(calls, transcriptToolCall{
					ID: block.ToolCallID, Name: block.ToolName,
					Arguments:          append(json.RawMessage(nil), block.Arguments...),
					ArgumentsTruncated: block.ArgumentsTruncated,
				})
			case protocol.TranscriptContentImage:
				textParts = append(textParts, "[image]")
			case protocol.TranscriptContentFile:
				textParts = append(textParts, "[file: "+block.Filename+"]")
			}
		}
		text := strings.Join(textParts, "\n")
		thinking := strings.Join(thinkingParts, "\n")
		if text == "" && message.ErrorMessage != "" {
			text = message.ErrorMessage
		}
		if strings.TrimSpace(text) == "" && strings.TrimSpace(thinking) == "" && message.ToolName == "" && len(calls) == 0 {
			continue
		}
		status := ""
		if message.Role == "tool" {
			if message.IsError {
				status = "Failed"
			} else {
				status = "Completed"
			}
		}
		result = append(result, transcriptMessage{
			ID: message.ID, TurnID: message.TurnID, Role: message.Role,
			Text: text, Thinking: thinking, ToolCallID: message.ToolCallID, ToolCalls: calls,
			ToolName: message.ToolName, ToolStatus: status,
			ToolContent: append([]protocol.TranscriptContent(nil), message.Content...),
			ToolDetails: append(json.RawMessage(nil), message.Details...), IsError: message.IsError,
			Aborted: message.StopReason == "aborted",
		})
	}
	return result
}

func (s *appState) resetLiveRun() {
	s.liveMessages = nil
	s.liveAssistant = -1
	s.liveHasUser = false
	s.liveTools = make(map[string]int)
	s.liveContent = make(map[int]liveContentBlock)
	s.liveSequence = 0
	s.turnActivity = ""
	s.turnThinking = ""
	s.runStopping = false
	s.terminalRunActive = false
	s.terminalRunID = ""
	s.agentFeedbackPending = false
}

func (s *appState) markTerminalRunStarted(runID string) {
	if runID != "" && runID == s.terminalSettledRunID {
		return
	}
	s.terminalRunActive = true
	s.terminalRunID = runID
}

func (s *appState) markTerminalRunSettled(runID string) {
	if runID != "" && s.terminalRunID != "" && runID != s.terminalRunID {
		return
	}
	if runID == "" {
		runID = s.terminalRunID
	}
	s.terminalRunActive = false
	s.terminalRunID = ""
	if runID != "" {
		s.terminalSettledRunID = runID
	}
	s.agentFeedbackPending = false
}

func (s *appState) setTurnActivity(activity string) {
	if s.runStopping && activity != "" {
		return
	}
	s.turnActivity = activity
}

func (s *appState) setTurnThinking(thinking string) {
	if s.runStopping && thinking != "" {
		return
	}
	s.turnThinking = thinking
}

type cwdToolDetails struct {
	CWD     string `json:"cwd"`
	Changed bool   `json:"changed"`
}

func (s *appState) applyRunEvents(events []protocol.SessionEvent) string {
	transcriptChanged := false
	changedCWD := ""
	for _, event := range events {
		if event.Sequence <= s.liveSequence {
			continue
		}
		s.liveSequence = event.Sequence
		switch event.Kind {
		case protocol.SessionEventRunStarted:
			s.markTerminalRunStarted(event.RunID)
			s.setTurnThinking("")
			s.setTurnActivity("Working…")
		case protocol.SessionEventUserMessage:
			transcriptChanged = true
			if s.turnActivity == "" {
				s.setTurnActivity("Working…")
			}
			if !s.liveHasUser {
				s.liveMessages = append(s.liveMessages, transcriptMessage{
					ID: "live-user:" + event.TurnID, TurnID: event.TurnID, Role: "user", Text: event.Text,
				})
				s.liveHasUser = true
			} else {
				for index := range s.liveMessages {
					if s.liveMessages[index].Role == "user" && s.liveMessages[index].TurnID == "" {
						s.liveMessages[index].ID = "live-user:" + event.TurnID
						s.liveMessages[index].TurnID = event.TurnID
						s.liveMessages[index].Text = event.Text
						break
					}
				}
			}
		case protocol.SessionEventAssistantStarted:
			transcriptChanged = transcriptChanged || event.Thinking != ""
			s.setTurnActivity("Working…")
			if event.Text == "" {
				s.setTurnThinking(event.Thinking)
			} else {
				s.setTurnThinking("")
			}
			s.liveAssistant = len(s.liveMessages)
			s.liveContent = make(map[int]liveContentBlock)
			if event.Thinking != "" {
				s.liveContent[-2] = liveContentBlock{kind: protocol.SessionEventThinkingDelta, text: event.Thinking}
			}
			if event.Text != "" {
				s.liveContent[-1] = liveContentBlock{kind: protocol.SessionEventAssistantTextDelta, text: event.Text}
			}
			s.liveMessages = append(s.liveMessages, transcriptMessage{
				ID: event.MessageID, TurnID: event.TurnID, Role: "assistant",
				Text: event.Text, Thinking: event.Thinking, Pending: true,
			})
		case protocol.SessionEventAssistantTextDelta, protocol.SessionEventThinkingDelta:
			index := s.ensureLiveAssistant(event.MessageID, event.TurnID)
			block, exists := s.liveContent[event.ContentIndex]
			if exists && block.kind != event.Kind {
				continue
			}
			block.kind = event.Kind
			block.text += event.Delta
			s.liveContent[event.ContentIndex] = block
			s.syncLiveAssistant(index)
			if event.Kind == protocol.SessionEventThinkingDelta {
				transcriptChanged = true
				s.setTurnThinking(s.liveMessages[index].Thinking)
				s.setTurnActivity(latestThinkingLine(s.liveMessages[index].Thinking))
			} else {
				s.setTurnThinking("")
				s.setTurnActivity("Working…")
			}
		case protocol.SessionEventAssistantCompleted:
			transcriptChanged = true
			index := s.ensureLiveAssistant(event.MessageID, event.TurnID)
			if event.Text != "" {
				s.liveMessages[index].Text = event.Text
			}
			if event.Thinking != "" {
				s.liveMessages[index].Thinking = event.Thinking
			}
			s.liveMessages[index].Pending = false
			if s.liveMessages[index].Text == "" && s.liveMessages[index].Thinking == "" && len(s.liveMessages[index].ToolCalls) == 0 {
				s.removeLiveMessage(index)
			}
			s.liveAssistant = -1
			s.liveContent = make(map[int]liveContentBlock)
			s.setTurnThinking("")
			s.setTurnActivity("Working…")
		case protocol.SessionEventToolPlanned, protocol.SessionEventToolStarted:
			transcriptChanged = true
			s.setTurnThinking("")
			s.setTurnActivity("Working…")
			s.ensureLiveAssistantToolCall(event)
			index := s.ensureLiveTool(event.TurnID, event.ToolCallID, event.ToolName)
			s.liveMessages[index].Pending = true
			s.liveMessages[index].ToolArguments = event.Arguments
			s.liveMessages[index].ToolArgumentsTruncated = event.ArgumentsTruncated
			if event.Kind == protocol.SessionEventToolPlanned {
				s.liveMessages[index].ToolStatus = "Planned"
			} else {
				s.liveMessages[index].ToolStatus = "Running…"
			}
		case protocol.SessionEventToolUpdated, protocol.SessionEventToolCompleted:
			s.setTurnThinking("")
			s.setTurnActivity("Working…")
			s.ensureLiveAssistantToolCall(event)
			index := s.ensureLiveTool(event.TurnID, event.ToolCallID, event.ToolName)
			text := toolResultContentText(event.Content)
			if event.Kind == protocol.SessionEventToolUpdated {
				appendLiveToolContent(&s.liveMessages[index], event.Content)
			} else {
				s.liveMessages[index].Text = text
				s.liveMessages[index].ToolContent = append([]protocol.TranscriptContent(nil), event.Content...)
				s.liveMessages[index].ToolContentTruncated = event.ContentTruncated
				s.liveMessages[index].ToolDetails = append(json.RawMessage(nil), event.Details...)
				s.liveMessages[index].ToolDetailsOmitted = event.DetailsOmitted
				if event.ContentTruncated {
					s.liveMessages[index].Text = appendToolNotice(s.liveMessages[index].Text, "… live output truncated")
				}
				if event.DetailsOmitted {
					s.liveMessages[index].Text = appendToolNotice(s.liveMessages[index].Text, "… tool details omitted")
				}
			}
			s.liveMessages[index].IsError = event.IsError
			if event.Kind == protocol.SessionEventToolCompleted {
				transcriptChanged = true
				s.liveMessages[index].Pending = false
				if event.ToolName == "change_cwd" && !event.IsError && !event.DetailsOmitted {
					var details cwdToolDetails
					if json.Unmarshal(event.Details, &details) == nil && details.Changed && details.CWD != "" {
						s.session.CWD = details.CWD
						s.location = details.CWD
						changedCWD = details.CWD
					}
				}
				if event.IsError {
					s.liveMessages[index].ToolStatus = "Failed"
				} else {
					s.liveMessages[index].ToolStatus = "Completed"
				}
			}
		case protocol.SessionEventCompactionStarted:
			s.setTurnThinking("")
			s.setTurnActivity("Compacting session…")
		case protocol.SessionEventCompactionCompleted:
			s.setTurnActivity("Working…")
			s.showToast(toastInput{Title: "Session compacted", Subtitle: "Session context was compacted.", Variant: toastInfo})
		case protocol.SessionEventCompactionFailed:
			s.setTurnActivity("Working…")
			s.showToast(toastInput{Title: "Auto-compaction failed", Subtitle: event.ErrorMessage, Variant: toastError})
		case protocol.SessionEventUsageUpdated:
			if event.Usage != nil && !sessionUsageDecreased(s.sessionUsage, *event.Usage) {
				s.sessionUsage = *event.Usage
			}
		case protocol.SessionEventRunFinished:
			transcriptChanged = true
			s.markTerminalRunSettled(event.RunID)
			for _, index := range s.liveTools {
				if index >= 0 && index < len(s.liveMessages) && s.liveMessages[index].ToolStatus == "Planned" {
					s.liveMessages[index].Pending = false
					s.liveMessages[index].ToolStatus = "Not run"
				}
			}
			s.runStopping = false
			s.setTurnThinking("")
			s.setTurnActivity("")
		}
	}
	if len(events) > 0 {
		if transcriptChanged {
			s.requestTranscriptScroll()
		}
		if s.activitySourceID != "" {
			messages := make([]transcriptMessage, 0, len(s.messages)+len(s.liveMessages))
			messages = append(messages, s.messages...)
			messages = append(messages, s.liveMessages...)
			presentation := presentTranscript(messages)
			if source, ok := transcriptActivitySource(presentation.Items, s.activitySourceID); ok {
				for _, event := range events {
					if event.TurnID == source.TurnID {
						s.requestActivityScroll(true)
						break
					}
				}
			}
		}
	}
	return changedCWD
}

func sessionUsageDecreased(before, after protocol.SessionUsage) bool {
	return after.Input < before.Input || after.Output < before.Output ||
		after.CacheRead < before.CacheRead || after.CacheWrite < before.CacheWrite ||
		after.Reasoning < before.Reasoning || after.TotalTokens < before.TotalTokens ||
		after.Cost.Input < before.Cost.Input || after.Cost.Output < before.Cost.Output ||
		after.Cost.CacheRead < before.Cost.CacheRead || after.Cost.CacheWrite < before.Cost.CacheWrite ||
		after.Cost.Total < before.Cost.Total
}

func (s *appState) ensureLiveAssistantToolCall(event protocol.SessionEvent) {
	for index := range s.liveMessages {
		message := &s.liveMessages[index]
		if message.Role != "assistant" || message.TurnID != event.TurnID {
			continue
		}
		for _, call := range message.ToolCalls {
			if call.ID == event.ToolCallID {
				return
			}
		}
		if event.MessageID == "" || message.ID == event.MessageID {
			message.ToolCalls = append(message.ToolCalls, transcriptToolCall{
				ID: event.ToolCallID, Name: event.ToolName,
				Arguments:          append(json.RawMessage(nil), event.Arguments...),
				ArgumentsTruncated: event.ArgumentsTruncated,
			})
			return
		}
	}
	messageID := event.MessageID
	if messageID == "" {
		messageID = "live-assistant:" + event.ToolCallID
	}
	s.liveMessages = append(s.liveMessages, transcriptMessage{
		ID: messageID, TurnID: event.TurnID, Role: "assistant",
		ToolCalls: []transcriptToolCall{{
			ID: event.ToolCallID, Name: event.ToolName,
			Arguments:          append(json.RawMessage(nil), event.Arguments...),
			ArgumentsTruncated: event.ArgumentsTruncated,
		}},
	})
}

func (s *appState) ensureLiveTool(turnID, callID, name string) int {
	if index, ok := s.liveTools[callID]; ok {
		return index
	}
	index := len(s.liveMessages)
	s.liveTools[callID] = index
	s.liveMessages = append(s.liveMessages, transcriptMessage{
		ID: "live-tool:" + callID, TurnID: turnID, Role: "tool", ToolCallID: callID, ToolName: name,
	})
	return index
}

func (s *appState) removeLiveMessage(index int) {
	copy(s.liveMessages[index:], s.liveMessages[index+1:])
	s.liveMessages = s.liveMessages[:len(s.liveMessages)-1]
	for callID, toolIndex := range s.liveTools {
		if toolIndex > index {
			s.liveTools[callID] = toolIndex - 1
		}
	}
}

func (s *appState) ensureLiveAssistant(messageID, turnID string) int {
	if s.liveAssistant >= 0 && s.liveAssistant < len(s.liveMessages) &&
		s.liveMessages[s.liveAssistant].ID == messageID {
		if s.liveMessages[s.liveAssistant].TurnID == "" {
			s.liveMessages[s.liveAssistant].TurnID = turnID
		}
		return s.liveAssistant
	}
	for index := len(s.liveMessages) - 1; index >= 0; index-- {
		if s.liveMessages[index].Role == "assistant" && s.liveMessages[index].ID == messageID {
			s.liveAssistant = index
			if s.liveMessages[index].TurnID == "" {
				s.liveMessages[index].TurnID = turnID
			}
			return index
		}
	}
	s.liveAssistant = len(s.liveMessages)
	s.liveContent = make(map[int]liveContentBlock)
	s.liveMessages = append(s.liveMessages, transcriptMessage{ID: messageID, TurnID: turnID, Role: "assistant", Pending: true})
	return s.liveAssistant
}

func (s *appState) syncLiveAssistant(index int) {
	indexes := make([]int, 0, len(s.liveContent))
	for contentIndex := range s.liveContent {
		indexes = append(indexes, contentIndex)
	}
	sort.Ints(indexes)
	var text, thinking []string
	for _, contentIndex := range indexes {
		block := s.liveContent[contentIndex]
		switch block.kind {
		case protocol.SessionEventAssistantTextDelta:
			text = append(text, block.text)
		case protocol.SessionEventThinkingDelta:
			thinking = append(thinking, block.text)
		}
	}
	s.liveMessages[index].Text = strings.Join(text, "\n")
	s.liveMessages[index].Thinking = strings.Join(thinking, "\n")
}

const (
	maxLiveToolPreviewBytes  = 60 << 10
	maxLiveToolPreviewBlocks = 128
)

func appendLiveToolContent(message *transcriptMessage, content []protocol.TranscriptContent) {
	if message.ToolContentTruncated {
		return
	}
	used := 0
	for _, block := range message.ToolContent {
		used += liveToolPreviewBlockSize(block)
	}
	accepted := make([]protocol.TranscriptContent, 0, len(content))
	remaining := maxLiveToolPreviewBytes - used
	for _, block := range content {
		if len(message.ToolContent)+len(accepted) == maxLiveToolPreviewBlocks {
			message.ToolContentTruncated = true
			break
		}
		size := liveToolPreviewBlockSize(block)
		if size <= remaining {
			accepted = append(accepted, block)
			remaining -= size
			continue
		}
		if block.Kind == protocol.TranscriptContentText && remaining > 0 {
			end := min(len(block.Text), remaining)
			for end > 0 && end < len(block.Text) && !utf8.RuneStart(block.Text[end]) {
				end--
			}
			if end > 0 {
				block.Text = block.Text[:end]
				accepted = append(accepted, block)
			}
		}
		message.ToolContentTruncated = true
		break
	}
	message.ToolContent = append(message.ToolContent, accepted...)
	message.Text += toolResultContentText(accepted)
	if message.ToolContentTruncated {
		message.Text = appendToolNotice(message.Text, "… live output truncated")
	}
}

func liveToolPreviewBlockSize(block protocol.TranscriptContent) int {
	return len(block.Kind) + len(block.Text) + len(block.Filename) + len(block.MediaType)
}

func appendToolNotice(text, notice string) string {
	if text == "" {
		return notice
	}
	return text + "\n" + notice
}

func toolResultContentText(content []protocol.TranscriptContent) string {
	parts := make([]string, 0, len(content))
	for _, block := range content {
		switch block.Kind {
		case protocol.TranscriptContentText:
			parts = append(parts, block.Text)
		case protocol.TranscriptContentImage:
			parts = append(parts, "[image]")
		case protocol.TranscriptContentFile:
			parts = append(parts, "[file: "+block.Filename+"]")
		}
	}
	return strings.Join(parts, "\n")
}

func latestThinkingLine(thinking string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(thinking, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimRight(lines[index], " \t")
		if line != "" {
			return line
		}
	}
	return "Thinking…"
}

func (s *appState) settleRunWithoutSnapshot(info protocol.RunInfo, snapshotErr error) {
	s.markTerminalRunSettled(info.RunID)
	for index := range s.liveMessages {
		s.liveMessages[index].Pending = false
		if s.liveMessages[index].Role == "tool" && s.liveMessages[index].ToolStatus != "" {
			s.liveMessages[index].ToolStatus = "Finished"
		}
	}
	s.messages = append(s.messages, s.liveMessages...)
	s.resetLiveRun()
	if info.Status != protocol.RunStatusCompleted {
		message := info.ErrorMessage
		if message == "" {
			message = "Run " + string(info.Status)
		}
		s.messages = append(s.messages, transcriptMessage{Role: "error", Text: message})
	}
	s.activeRun = nil
	s.activeRunID = ""
	s.runPending = false
	s.prompt = nil
	s.status = "Transcript refresh failed; the next turn will retry · " + snapshotErr.Error()
	s.requestTranscriptScroll()
}

func (s *appState) watchSession(bound sessionclient.Session, operation uint64, runID string) {
	runtime := s.Context().Runtime()
	attachmentCtx := s.attachmentCtx
	if attachmentCtx == nil {
		attachmentCtx = s.ctx
	}
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var stream sessionclient.EventStream
		var streamCancel context.CancelFunc
		var updates <-chan []protocol.SessionEvent
		connect := func() {
			if streamCancel != nil {
				streamCancel()
			}
			streamContext, cancel := context.WithCancel(attachmentCtx)
			connected, err := bound.Stream(streamContext, runID)
			if err != nil {
				cancel()
				streamCancel = nil
				return
			}
			stream = connected
			streamCancel = cancel
			updates = connected.Updates()
		}
		defer func() {
			if streamCancel != nil {
				streamCancel()
			}
		}()
		connect()
		snapshotFailures := 0
		for {
			select {
			case <-attachmentCtx.Done():
				return
			case events, ok := <-updates:
				if !ok {
					updates = nil
					if streamCancel != nil {
						streamCancel()
						streamCancel = nil
					}
					if stream != nil && stream.Err() != nil && attachmentCtx.Err() == nil {
						runtime.Dispatch(func() {
							if operation == s.operation {
								s.SetState(func() { s.status = "Reconnecting activity… · esc abort · ctrl+c detach" })
							}
						})
					}
					continue
				}
				batch := append([]protocol.SessionEvent(nil), events...)
				runtime.Dispatch(func() {
					if operation == s.operation {
						changedCWD := ""
						s.SetState(func() { changedCWD = s.applyRunEvents(batch) })
						if changedCWD != "" {
							s.showToast(cwdChangeToast(changedCWD))
							s.refreshLocation(changedCWD)
						}
					}
				})
			case <-ticker.C:
				info, err := bound.Run(attachmentCtx, runID)
				if err != nil {
					if attachmentCtx.Err() == nil {
						runtime.Dispatch(func() {
							if operation == s.operation {
								s.SetState(func() { s.status = "Reconnecting… · esc abort · ctrl+c detach" })
							}
						})
					}
					continue
				}
				if info.Status == protocol.RunStatusQueued || info.Status == protocol.RunStatusRunning {
					if updates == nil {
						connect()
					}
					continue
				}
				settledRunID := runID
				runtime.Dispatch(func() {
					if operation == s.operation {
						s.SetState(func() { s.markTerminalRunSettled(settledRunID) })
					}
				})
				snapshot, err := bound.Snapshot(attachmentCtx)
				if err != nil {
					snapshotFailures++
					if attachmentCtx.Err() != nil {
						return
					}
					if snapshotFailures >= 6 {
						runtime.Dispatch(func() {
							if operation == s.operation {
								s.SetState(func() { s.settleRunWithoutSnapshot(info, err) })
								s.notifyTurnSettled(info.Status)
							}
						})
						return
					}
					runtime.Dispatch(func() {
						if operation == s.operation {
							s.SetState(func() { s.status = "Run finished · reconnecting transcript… · ctrl+c detach" })
						}
					})
					delay := 100 * time.Millisecond * time.Duration(1<<min(snapshotFailures-1, 4))
					timer := time.NewTimer(delay)
					select {
					case <-attachmentCtx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
					continue
				}
				snapshotFailures = 0
				nextRunID := snapshot.ActiveRunID
				terminalErrorPersisted := false
				for _, message := range snapshot.Messages {
					if message.TurnID == info.TurnID && message.IsError {
						terminalErrorPersisted = true
						break
					}
				}
				runtime.Dispatch(func() {
					if operation != s.operation {
						return
					}
					s.SetState(func() {
						s.applySnapshot(snapshot)
						if info.Status != protocol.RunStatusCompleted && !terminalErrorPersisted {
							message := info.ErrorMessage
							if message == "" {
								message = "Run " + string(info.Status)
							}
							s.messages = append(s.messages, transcriptMessage{Role: "error", Text: message})
						}
						if nextRunID != "" {
							s.status = "esc abort · ctrl+c detach"
						}
					})
					s.notifyTurnSettled(info.Status)
				})
				if nextRunID == "" {
					return
				}
				runID = nextRunID
				snapshotFailures = 0
				stream = nil
				updates = nil
				connect()
			}
		}
	}()
}

func (s *appState) notifyTurnSettled(status protocol.RunStatus) {
	if s.terminalStatus != nil {
		s.terminalStatus.Bell()
	}
	message := "Agent turn complete"
	if status == protocol.RunStatusFailed || status == protocol.RunStatusInterrupted {
		message = "Agent turn failed"
	}
	s.Context().EventContext().Notify("Kit", message)
}

func (s *appState) selectProvider(ctx ui.EventContext, providerID string) {
	provider, ok := authProviderByID(providerID)
	if !ok {
		s.SetState(func() { s.errorText = "Provider is unavailable" })
		return
	}
	if provider.ID == auth.OpenAICodexProviderID {
		s.startLogin(ctx)
		return
	}
	s.SetState(func() {
		s.phase = phaseAuthAPIKey
		s.authProviderID = provider.ID
		s.authAPIKey = ""
		s.errorText = ""
		s.status = ""
	})
}

func (s *appState) moveProviderSelection(delta int) {
	providers := filteredAuthProviders(s.authFilter)
	if len(providers) == 0 {
		return
	}
	s.SetState(func() {
		s.authSelection = (s.authSelection + delta) % len(providers)
		if s.authSelection < 0 {
			s.authSelection += len(providers)
		}
	})
}

func (s *appState) submitAPIKey(_ ui.EventContext, value string) {
	options := s.Widget().(app).Options
	provider, ok := authProviderByID(s.authProviderID)
	if !ok || provider.ID == auth.OpenAICodexProviderID {
		s.SetState(func() { s.errorText = "API-key provider is unavailable" })
		return
	}
	if options.APIKeyLogin == nil {
		s.SetState(func() { s.errorText = provider.Name + " API-key login is unavailable" })
		return
	}
	if s.authPending {
		return
	}
	s.cancelLogin()
	loginContext, cancel := context.WithCancel(s.ctx)
	s.loginCancel = cancel
	generation := s.loginGeneration
	runtime := s.Context().Runtime()
	s.SetState(func() {
		s.authPending = true
		s.errorText = ""
		s.status = "Saving " + provider.Name + " API key…"
	})

	go func() {
		err := options.APIKeyLogin.Login(loginContext, provider.ID, value)
		wasCanceled := loginContext.Err() != nil
		cancel()
		if wasCanceled || s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if generation != s.loginGeneration {
				return
			}
			s.loginCancel = nil
			if err != nil {
				s.SetState(func() {
					s.authPending = false
					s.authAPIKey = ""
					s.errorText = err.Error()
					s.status = ""
				})
				return
			}
			s.availableMu.Lock()
			s.available[provider.ID] = true
			s.availableMu.Unlock()
			if s.authReturnReady {
				s.SetState(func() {
					s.phase = phaseReady
					s.authReturnReady = false
					s.authPending = false
					s.authProviderID = ""
					s.authAPIKey = ""
					s.status = "Connected to " + provider.Name
				})
				return
			}
			s.SetState(func() {
				s.phase = phaseLoading
				s.authPending = false
				s.authProviderID = ""
				s.authAPIKey = ""
				s.status = "Connected to " + provider.Name
			})
			s.startBootstrap(preferredStartupModel(options.DefaultModel, provider.DefaultModel), options.DefaultThinking)
		})
	}()
}

func (s *appState) startLogin(_ ui.EventContext) {
	options := s.Widget().(app).Options
	if options.Login == nil {
		s.SetState(func() { s.errorText = "OpenAI Codex login is unavailable" })
		return
	}
	s.cancelLogin()
	loginContext, cancel := context.WithCancel(s.ctx)
	s.loginCancel = cancel
	generation := s.loginGeneration
	runtime := s.Context().Runtime()
	s.SetState(func() {
		s.phase = phaseAuthWaiting
		s.errorText = ""
		s.status = "Starting OpenAI Codex sign-in…"
		s.instructions = auth.OpenAICodexDeviceInstructions{}
		s.authFilter = ""
		s.authProviderID = ""
		s.authAPIKey = ""
		s.authPending = true
	})

	go func() {
		err := options.Login.Login(loginContext, func(instructions auth.OpenAICodexDeviceInstructions) error {
			runtime.Dispatch(func() {
				if generation != s.loginGeneration {
					return
				}
				s.SetState(func() {
					s.instructions = instructions
					s.remaining = time.Until(instructions.ExpiresAt)
					s.status = "Waiting for approval…"
				})
			})
			go s.tickDeviceExpiry(loginContext, runtime, generation, instructions.ExpiresAt)
			return nil
		})
		wasCanceled := loginContext.Err() != nil
		cancel()
		if wasCanceled || s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if generation != s.loginGeneration {
				return
			}
			s.loginCancel = nil
			if err != nil {
				s.SetState(func() {
					s.phase = phaseAuthSelect
					s.authPending = false
					s.errorText = err.Error()
					s.status = ""
				})
				return
			}
			s.availableMu.Lock()
			s.available[auth.OpenAICodexProviderID] = true
			s.availableMu.Unlock()
			if s.authReturnReady {
				s.SetState(func() {
					s.phase = phaseReady
					s.authReturnReady = false
					s.authPending = false
					s.status = "Connected to OpenAI Codex"
					s.instructions = auth.OpenAICodexDeviceInstructions{}
				})
				return
			}
			s.SetState(func() {
				s.phase = phaseLoading
				s.authPending = false
				s.status = "Connected to OpenAI Codex"
				s.instructions = auth.OpenAICodexDeviceInstructions{}
			})
			s.startBootstrap(preferredStartupModel(options.DefaultModel, codexDefaultModel), options.DefaultThinking)
		})
	}()
}

func preferredStartupModel(requested, fallback string) string {
	if requested != "" {
		return requested
	}
	return fallback
}

func (s *appState) tickDeviceExpiry(ctx context.Context, runtime ui.Runtime, generation uint64, expiresAt time.Time) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			remaining := time.Until(expiresAt)
			if remaining < 0 {
				remaining = 0
			}
			runtime.Dispatch(func() {
				if generation != s.loginGeneration {
					return
				}
				s.SetState(func() { s.remaining = remaining })
			})
		}
	}
}

func (s *appState) cancelLogin() {
	s.loginGeneration++
	if s.loginCancel != nil {
		s.loginCancel()
		s.loginCancel = nil
	}
}

func (s *appState) hasActiveWork() bool {
	return s.runPending || s.reloadPending || s.cwdPending || s.compactPending || s.configurationPicker.Pending || s.bashStarting || s.activeBashID != ""
}

func (s *appState) openPalette() {
	if s.phase != phaseReady || s.palette.Open || s.bashHistory.Open || s.sessionExplorer.Open {
		return
	}
	s.SetState(func() { s.palette.OpenFor(s.hasActiveWork()) })
}

func (s *appState) movePaletteSelection(delta int) {
	if !s.palette.Open {
		return
	}
	s.SetState(func() { s.palette.Move(s.hasActiveWork(), delta) })
}

func (s *appState) runPaletteQuery(ctx ui.EventContext, query string) {
	command, ok := s.palette.Selected(s.hasActiveWork(), query)
	if !ok {
		return
	}
	s.runPaletteCommand(ctx, command.ID)
}

func (s *appState) runPaletteCommand(ctx ui.EventContext, commandID paletteCommandID) {
	if !s.palette.Open || !paletteCommandExists(commandID, s.palette.Contributions) {
		return
	}
	if toast, disabled := paletteCommandDisabledToast(commandID, s.hasActiveWork()); disabled {
		s.showToast(toast)
		return
	}
	_, args := splitPaletteQuery(s.palette.Query)
	s.SetState(func() { s.palette.Close() })
	if name, ok := promptPaletteCommandName(commandID); ok {
		s.submitPromptCommand(name, args)
		return
	}
	switch commandID {
	case paletteCommandCD:
		s.changeCWD(args)
	case paletteCommandCompact:
		s.compactSession()
	case paletteCommandLogin:
		s.enterAuthSelect(true)
	case paletteCommandModel:
		s.openConfigurationPicker(configurationPickerModel)
	case paletteCommandQuit:
		ctx.Quit()
	case paletteCommandReload:
		s.reloadSession()
	case paletteCommandDebug:
		s.SetState(func() { s.sessionDetailsOpen = true })
	case paletteCommandSessions:
		s.openSessionExplorer()
	case paletteCommandThinking:
		s.openConfigurationPicker(configurationPickerThinking)
	}
}

func (s *appState) openConfigurationPicker(mode configurationPickerMode) {
	if s.phase != phaseReady || s.bound == nil || s.configurationPicker.Mode != configurationPickerClosed {
		return
	}
	busyTransition := s.reloadPending || s.cwdPending || s.compactPending || s.configurationPicker.Pending
	if busyTransition || (mode == configurationPickerModel && s.hasActiveWork()) {
		s.showToast(toastInput{Title: "Session is busy", Subtitle: "Wait for active work before changing configuration.", Variant: toastWarning})
		return
	}
	server := s.Widget().(app).Options.Server
	generation := uint64(0)
	s.SetState(func() {
		generation = s.configurationPicker.Begin(mode, s.session.Model, s.session.ThinkingLevel)
	})
	runtime := s.Context().Runtime()
	go func() {
		catalog, err := server.Models(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			s.SetState(func() { s.configurationPicker.Resolve(generation, catalog, err) })
		})
	}()
}

func (s *appState) applyConfigurationSelection() {
	if s.phase != phaseReady || s.bound == nil {
		return
	}
	mode := s.configurationPicker.Mode
	busyTransition := s.reloadPending || s.cwdPending || s.compactPending || s.configurationPicker.Pending
	if busyTransition || (mode == configurationPickerModel && s.hasActiveWork()) {
		return
	}
	selection := s.configurationPicker.Selection
	if mode == configurationPickerModel {
		index := modelCapabilityIndex(s.configurationPicker.Models, selection)
		if index < 0 || !s.configurationPicker.Models[index].Available {
			s.SetState(func() { s.configurationPicker.Error = "The selected provider is not authenticated." })
			return
		}
	}
	var generation uint64
	var ok bool
	s.SetState(func() { generation, selection, ok = s.configurationPicker.BeginApply() })
	if !ok {
		return
	}
	input := configurationInputForSelection(s.session, mode, selection)
	bound := s.bound
	operation := s.operation
	runtime := s.Context().Runtime()
	s.SetState(func() { s.status = "Applying session configuration…" })
	go func() {
		configureContext, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
		result, configureErr := bound.Configure(configureContext, input)
		cancel()
		var snapshot protocol.SessionSnapshot
		var snapshotErr error
		if configureErr == nil {
			snapshotContext, cancelSnapshot := context.WithTimeout(s.ctx, 10*time.Second)
			snapshot, snapshotErr = bound.Snapshot(snapshotContext)
			cancelSnapshot()
		} else {
			inspectContext, cancelInspect := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
			snapshot, snapshotErr = bound.Snapshot(inspectContext)
			cancelInspect()
		}
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if operation != s.operation || generation != s.configurationPicker.generation {
				return
			}
			if snapshotErr == nil {
				s.SetState(func() {
					if mode == configurationPickerThinking {
						s.applySessionMetadataSnapshot(snapshot)
					} else {
						s.applySnapshot(snapshot)
					}
					s.configurationPicker.CurrentModel = snapshot.Session.Model
					s.configurationPicker.CurrentThinking = snapshot.Session.ThinkingLevel
				})
			}
			finalErr := configureErr
			if finalErr == nil && snapshotErr != nil {
				s.SetState(func() { s.session = result.Session })
				finalErr = fmt.Errorf("configuration applied but snapshot refresh failed: %w", snapshotErr)
			}
			s.SetState(func() {
				s.status = ""
				if s.runPending {
					s.status = "esc abort · ctrl+c detach"
				}
				s.configurationPicker.ResolveApply(generation, finalErr)
			})
			if configureErr != nil {
				s.showToast(toastInput{Title: "Configuration failed", Subtitle: configureErr.Error(), Variant: toastError})
				return
			}
			for _, warning := range result.Warnings {
				s.showToast(toastInput{Title: "Configuration adjusted", Subtitle: warning, Variant: toastWarning})
			}
			if snapshotErr != nil {
				s.showToast(toastInput{Title: "Configuration applied", Subtitle: snapshotErr.Error(), Variant: toastWarning})
				return
			}
			s.showToast(toastInput{Title: "Configuration applied", Subtitle: result.Session.Model + " · " + result.Session.ThinkingLevel, Variant: toastInfo})
		})
	}()
}

func configurationInputForSelection(session protocol.SessionInfo, mode configurationPickerMode, selection string) protocol.ConfigureSessionInput {
	input := protocol.ConfigureSessionInput{ExpectedRevision: session.ConfigurationRevision, Model: session.Model}
	if mode == configurationPickerModel {
		input.Model = selection
	} else {
		level := protocol.ThinkingLevel(selection)
		input.ThinkingLevel = &level
	}
	return input
}

func (s *appState) compactSession() {
	if s.compactPending {
		s.showToast(toastInput{Title: "Compaction failed", Subtitle: "Compaction already in progress.", Variant: toastError})
		return
	}
	if s.phase != phaseReady || s.bound == nil || s.hasActiveWork() || s.reloadPending {
		s.showToast(toastInput{Title: "Compaction failed", Subtitle: "Cannot compact while the agent is running.", Variant: toastError})
		return
	}
	operationID := s.compactOperationID
	if operationID == "" {
		var err error
		operationID, err = identifier.New("compact_")
		if err != nil {
			s.showToast(toastInput{Title: "Compaction failed", Subtitle: err.Error(), Variant: toastError})
			return
		}
	}
	bound := s.bound
	operation := s.operation
	runtime := s.Context().Runtime()
	s.SetState(func() {
		s.compactPending = true
		s.compactOperationID = operationID
		s.status = "Compacting session…"
	})
	go func() {
		compactContext, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
		result, compactErr := bound.Compact(compactContext, protocol.CompactSessionInput{OperationID: operationID})
		cancel()
		var snapshot protocol.SessionSnapshot
		var snapshotErr error
		if compactErr == nil {
			snapshotContext, cancelSnapshot := context.WithTimeout(s.ctx, 10*time.Second)
			snapshot, snapshotErr = bound.Snapshot(snapshotContext)
			cancelSnapshot()
		}
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if operation != s.operation || operationID != s.compactOperationID {
				return
			}
			s.SetState(func() {
				s.compactPending = false
				s.status = ""
				if compactErr == nil {
					s.compactOperationID = ""
				}
				if snapshotErr == nil && compactErr == nil {
					s.applySnapshot(snapshot)
				}
			})
			s.showToast(compactionToast(result, compactErr, snapshotErr))
		})
	}()
}

func compactionToast(result protocol.CompactSessionResult, compactErr, snapshotErr error) toastInput {
	if compactErr != nil {
		return toastInput{Title: "Compaction failed", Subtitle: compactErr.Error(), Variant: toastError}
	}
	if snapshotErr != nil {
		return toastInput{Title: "Session compacted", Subtitle: "Session context was compacted. " + snapshotErr.Error(), Variant: toastWarning}
	}
	if result.Compacted {
		return toastInput{Title: "Session compacted", Subtitle: "Session context was compacted.", Variant: toastInfo}
	}
	return toastInput{Title: "Compaction failed", Subtitle: "Not enough turns to compact.", Variant: toastError}
}

func (s *appState) changeCWD(target string) {
	if s.phase != phaseReady || s.bound == nil || s.hasActiveWork() {
		return
	}
	target = strings.TrimSpace(target)
	if target == "" {
		s.showToast(toastInput{Title: "Usage: /cd <path>", Variant: toastWarning})
		return
	}
	bound := s.bound
	previousCWD := s.session.CWD
	operation := s.operation
	runtime := s.Context().Runtime()
	s.SetState(func() {
		s.cwdPending = true
		s.status = "Changing working directory…"
	})
	go func() {
		changeContext, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		info, err := bound.ChangeCWD(changeContext, target)
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if operation != s.operation {
				return
			}
			s.SetState(func() {
				s.cwdPending = false
				s.status = ""
				if err == nil {
					s.session = info
					s.location = info.CWD
				}
			})
			if err != nil {
				s.showToast(toastInput{Title: "Failed to change directory", Subtitle: err.Error(), Variant: toastError})
				return
			}
			if info.CWD == previousCWD {
				s.showToast(toastInput{Title: "Already in directory", Subtitle: info.CWD, Variant: toastInfo})
			} else {
				s.showToast(cwdChangeToast(info.CWD))
			}
			s.refreshLocation(info.CWD)
		})
	}()
}

func cwdChangeToast(cwd string) toastInput {
	return toastInput{
		Title: "Working directory changed", Subtitle: "Now " + cwd + " · run /reload to refresh agent context", Variant: toastWarning,
	}
}

func (s *appState) refreshLocation(cwd string) {
	resolve := s.Widget().(app).Options.ResolveLocation
	if resolve == nil {
		return
	}
	operation := s.operation
	runtime := s.Context().Runtime()
	go func() {
		location := resolve(s.ctx, cwd)
		runtime.Dispatch(func() {
			if operation == s.operation && s.session.CWD == cwd {
				s.SetState(func() { s.location = location })
			}
		})
	}()
}

func (s *appState) reloadSession() {
	if s.phase != phaseReady || s.bound == nil || s.reloadPending || s.cwdPending || s.compactPending || s.configurationPicker.Pending {
		return
	}
	bound := s.bound
	operation := s.operation
	runtime := s.Context().Runtime()
	s.SetState(func() {
		s.reloadPending = true
		s.status = "Reloading session context…"
	})
	go func() {
		reloadContext, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		result, reloadErr := bound.Reload(reloadContext)
		if s.ctx.Err() != nil {
			return
		}
		snapshotContext, cancelSnapshot := context.WithTimeout(s.ctx, 5*time.Second)
		snapshot, snapshotErr := bound.Snapshot(snapshotContext)
		cancelSnapshot()
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if operation != s.operation {
				return
			}
			s.SetState(func() {
				s.reloadPending = false
				s.status = ""
				if snapshotErr == nil {
					s.applySessionMetadataSnapshot(snapshot)
				}
				if s.runPending {
					s.status = "esc abort · ctrl+c detach"
				}
			})
			s.showToast(reloadToast(result, reloadErr, snapshotErr))
		})
	}()
}

func reloadToast(result protocol.ReloadSessionResult, reloadErr, snapshotErr error) toastInput {
	if reloadErr != nil {
		toast := toastInput{Title: "Session reload failed", Subtitle: reloadErr.Error(), Variant: toastError}
		if snapshotErr != nil {
			toast.Subtitle += " · transcript refresh failed: " + snapshotErr.Error()
		}
		return toast
	}
	toast := toastInput{Title: "Session context reloaded", Variant: toastInfo}
	details := append([]string(nil), result.Warnings...)
	warning := len(result.Warnings) > 0
	for _, diagnostic := range result.Diagnostics {
		details = append(details, diagnostic.Message)
		warning = warning || diagnostic.Severity == "warning"
	}
	if snapshotErr != nil {
		details = append([]string{"Transcript refresh failed: " + snapshotErr.Error()}, details...)
		warning = true
	}
	if warning {
		toast.Variant = toastWarning
	}
	if len(details) > 0 {
		toast.Subtitle = details[0]
		if len(details) > 1 {
			toast.Subtitle += fmt.Sprintf(" (+%d more)", len(details)-1)
		}
	}
	return toast
}

func (s *appState) openSessionExplorer() {
	if s.phase != phaseReady || s.sessionExplorer.Open {
		return
	}
	server := s.Widget().(app).Options.Server
	currentSessionID := s.session.ID
	runtime := s.Context().Runtime()
	var generation uint64
	s.SetState(func() { generation = s.sessionExplorer.Begin(currentSessionID) })
	go func() {
		listContext, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		defer cancel()
		sessions, err := listSessionExplorerSessions(listContext, server)
		items := projectSessionExplorerItems(sessions)
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			s.SetState(func() { s.sessionExplorer.Resolve(generation, items, err) })
		})
	}()
}

func listSessionExplorerSessions(ctx context.Context, server sessionclient.Server) ([]protocol.SessionInfo, error) {
	return server.ListSessions(ctx, "")
}

func (s *appState) renameSelectedSession(value string) {
	var generation uint64
	var sessionID, name string
	var started bool
	s.SetState(func() {
		s.sessionExplorer.SetRenameText(value)
		generation, sessionID, name, started = s.sessionExplorer.BeginRenameSave()
	})
	if !started {
		return
	}
	server := s.Widget().(app).Options.Server
	runtime := s.Context().Runtime()
	go func() {
		renameContext, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		renamed, err := server.RenameSession(renameContext, sessionID, name)
		cancel()
		if err == nil && renamed.ID != sessionID {
			err = errors.New("renamed session identity mismatch")
		}
		if err == nil && renamed.Name != name {
			err = errors.New("renamed session name mismatch")
		}
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			accepted := false
			s.SetState(func() {
				accepted = s.sessionExplorer.ResolveRename(generation, renamed, err)
				if accepted && err == nil && s.session.ID == sessionID {
					s.session = renamed
				}
			})
		})
	}()
}

func (s *appState) deleteSelectedSession() {
	var generation uint64
	var sessionID string
	var started bool
	s.SetState(func() {
		generation, sessionID, started = s.sessionExplorer.BeginDeleteConfirm()
	})
	if !started {
		return
	}
	server := s.Widget().(app).Options.Server
	runtime := s.Context().Runtime()
	go func() {
		deleteContext, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		err := server.DeleteSession(deleteContext, sessionID)
		cancel()
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			s.SetState(func() { s.sessionExplorer.ResolveDelete(generation, err) })
		})
	}()
}

func (s *appState) switchSelectedSession() {
	if !s.sessionExplorer.Open {
		return
	}
	target, activatable := s.sessionExplorer.ActivatableSelection()
	if !activatable {
		return
	}
	if target == s.session.ID {
		s.cancelSessionSwitch()
		s.SetState(func() { s.sessionExplorer.Close() })
		return
	}
	var generation uint64
	var targetSessionID string
	var started bool
	s.SetState(func() {
		generation, targetSessionID, started = s.sessionExplorer.BeginSwitch()
	})
	if !started {
		return
	}

	s.cancelSessionSwitch()
	switchContext, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	s.sessionSwitchCancel = cancel
	s.sessionSwitchGeneration = generation
	options := s.Widget().(app).Options
	runtime := s.Context().Runtime()
	go func() {
		bound, snapshot, location, err := attachSessionForSwitch(
			switchContext, options.Server, targetSessionID, options.ResolveLocation,
		)
		cancel()
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if generation != s.sessionSwitchGeneration {
				return
			}
			s.sessionSwitchCancel = nil
			s.sessionSwitchGeneration = 0
			if err != nil {
				s.SetState(func() { s.sessionExplorer.ResolveSwitch(generation, err) })
				return
			}
			var operation uint64
			nextRunID := snapshot.ActiveRunID
			nextBashID := snapshot.ActiveBashExecutionID
			s.SetState(func() {
				if !s.sessionExplorer.ResolveSwitch(generation, nil) {
					return
				}
				s.installSession(bound, snapshot, location)
				operation = s.operation
			})
			if operation == 0 {
				return
			}
			for _, warning := range snapshot.Warnings {
				s.showToast(toastInput{Title: "Configuration adjusted", Subtitle: warning, Variant: toastWarning})
			}
			if nextRunID != "" {
				s.watchSession(bound, operation, nextRunID)
			}
			if nextBashID != "" {
				s.resumeBash(bound, operation, nextBashID)
			}
		})
	}()
}

func attachSessionForSwitch(
	ctx context.Context,
	server sessionclient.Server,
	sessionID string,
	resolveLocation func(context.Context, string) string,
) (sessionclient.Session, protocol.SessionSnapshot, string, error) {
	bound, err := server.Attach(ctx, sessionID)
	if err != nil {
		return nil, protocol.SessionSnapshot{}, "", fmt.Errorf("attach session: %w", err)
	}
	if bound.ID() != sessionID {
		return nil, protocol.SessionSnapshot{}, "", errors.New("attached session identity mismatch")
	}
	snapshot, err := bound.Snapshot(ctx)
	if err != nil {
		return nil, protocol.SessionSnapshot{}, "", fmt.Errorf("snapshot session: %w", err)
	}
	if snapshot.Session.ID != sessionID {
		return nil, protocol.SessionSnapshot{}, "", errors.New("session snapshot identity mismatch")
	}
	location := snapshot.Session.CWD
	if resolveLocation != nil {
		location = resolveLocation(ctx, snapshot.Session.CWD)
	}
	if err := ctx.Err(); err != nil {
		return nil, protocol.SessionSnapshot{}, "", err
	}
	return bound, snapshot, location, nil
}

func (s *appState) installSession(bound sessionclient.Session, snapshot protocol.SessionSnapshot, location string) {
	if s.session.ID != "" {
		s.sessionDrafts[s.session.ID] = s.composer
	}
	s.resetAttachmentContext()
	s.operation++
	s.terminalSettledRunID = ""
	s.session = snapshot.Session
	s.bound = bound
	s.location = location
	s.composer = s.sessionDrafts[snapshot.Session.ID]
	s.composerCursorEndGeneration++
	s.messages = nil
	s.configurationPicker = configurationPickerController{}
	s.cwdPending = false
	s.reloadPending = false
	s.compactPending = false
	s.compactOperationID = ""
	s.resetLiveRun()
	s.contextTokens = 0
	s.contextWindow = 0
	s.scroll = ui.ScrollController{}
	s.activityScroll = ui.ScrollController{}
	s.activityList = activityListController{}
	s.activitySourceID = ""
	s.activitySelected = false
	s.hoveredActivityID = ""
	s.activityExpanded = make(map[activityToolKey]bool)
	s.activityCursor = activityToolKey{}
	s.activityReveal = activityToolKey{}
	s.activityRevealPending = false
	s.activityRevealPendingLayout = false
	s.activityNeedsScroll = false
	s.activityPendingLayout = false
	s.activityScrollToEnd = false
	s.activeRun = nil
	s.activeRunID = ""
	s.runPending = false
	s.prompt = nil
	s.activeBash = nil
	s.activeBashID = ""
	s.bashStarting = false
	s.bashAdmission = nil
	s.bashCollapsed = make(map[string]bool)
	s.bashHistory = bashHistoryController{}
	s.status = ""
	s.applySnapshot(snapshot)
	if snapshot.ActiveRunID != "" {
		s.status = "esc abort · ctrl+c detach"
	}
}

func (s *appState) cancelSessionSwitch() {
	if s.sessionSwitchCancel != nil {
		s.sessionSwitchCancel()
	}
	s.sessionSwitchCancel = nil
	s.sessionSwitchGeneration = 0
}

func (s *appState) enterAuthSelect(returnReady bool) {
	s.SetState(func() {
		s.phase = phaseAuthSelect
		s.authReturnReady = returnReady
		s.errorText = ""
		s.authFilter = ""
		s.authSelection = 0
	})
}

func (s *appState) submit(_ ui.EventContext, value string) {
	if s.reloadPending {
		s.SetState(func() { s.status = "Session context is reloading…" })
		return
	}
	if s.compactPending || s.configurationPicker.Pending {
		s.SetState(func() { s.status = "Session configuration is changing…" })
		return
	}
	if command, excludeFromContext, ok := parseDirectBash(value); ok {
		s.startDirectBash(value, command, excludeFromContext)
		return
	}
	text := strings.TrimSpace(value)
	if text == "" || s.bound == nil {
		return
	}
	bound := s.bound
	s.startPromptSubmission(text, func(ctx context.Context) (sessionclient.Run, error) {
		return bound.StartPrompt(ctx, text)
	})
}

func (s *appState) submitPromptCommand(name, args string) {
	if s.bound == nil {
		return
	}
	display := "/" + name
	if trimmed := strings.TrimSpace(args); trimmed != "" {
		display += " " + trimmed
	}
	bound := s.bound
	s.startPromptSubmission(display, func(ctx context.Context) (sessionclient.Run, error) {
		return bound.StartPromptCommand(ctx, name, args)
	})
}

func (s *appState) startPromptSubmission(display string, start func(context.Context) (sessionclient.Run, error)) {
	if s.runPending {
		s.SetState(func() { s.status = "Run in progress · esc abort · ctrl+c detach" })
		return
	}
	admission := &promptAdmission{}
	bound := s.bound
	operation := s.operation
	runtime := s.Context().Runtime()
	s.SetState(func() {
		s.composer = ""
		s.status = "esc abort · ctrl+c detach"
		s.resetLiveRun()
		s.turnActivity = "Working…"
		s.liveMessages = append(s.liveMessages, transcriptMessage{Role: "user", Text: display})
		s.liveHasUser = true
		s.requestTranscriptScroll()
		s.runPending = true
		s.markTerminalRunStarted("")
		s.prompt = admission
	})
	go func() {
		run, err := start(s.ctx)
		if err != nil {
			if s.ctx.Err() == nil {
				s.finishRun(runtime, operation, protocol.PromptOutcome{}, err)
			}
			return
		}
		abort := func() {
			abortContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = run.Abort(abortContext)
		}
		if admission.abort.Load() {
			abort()
		}
		if s.ctx.Err() != nil {
			if admission.abort.Load() {
				abort()
			}
			return
		}
		runtime.Dispatch(func() {
			accepted := false
			s.SetState(func() { accepted = s.acceptPromptAdmission(operation, run) })
			if !accepted {
				return
			}
			if admission.abort.Load() {
				go abort()
			}
			s.watchSession(bound, operation, run.ID())
		})
	}()
}

func (s *appState) acceptPromptAdmission(operation uint64, run sessionclient.Run) bool {
	if operation != s.operation {
		return false
	}
	s.activeRun = run
	s.activeRunID = run.ID()
	if s.terminalRunActive && s.terminalRunID == "" {
		s.terminalRunID = run.ID()
	}
	return true
}

func (s *appState) finishRun(runtime ui.Runtime, operation uint64, outcome protocol.PromptOutcome, runErr error) {
	var snapshot protocol.SessionSnapshot
	var snapshotErr error
	if runErr == nil && outcome.Status == protocol.RunStatusCompleted && s.bound != nil {
		snapshot, snapshotErr = s.bound.Snapshot(s.ctx)
	}
	runtime.Dispatch(func() {
		if operation != s.operation {
			return
		}
		nextRunID := ""
		nextBashID := ""
		s.SetState(func() {
			s.markTerminalRunSettled(s.activeRunID)
			s.activeRun = nil
			s.activeRunID = ""
			s.runPending = false
			s.prompt = nil
			s.status = ""
			s.requestTranscriptScroll()
			if runErr != nil {
				s.messages = append(s.messages, s.liveMessages...)
				s.resetLiveRun()
				s.messages = append(s.messages, transcriptMessage{Role: "error", Text: runErr.Error()})
				return
			}
			if outcome.Status != protocol.RunStatusCompleted {
				message := outcome.ErrorMessage
				if message == "" {
					message = "Run " + string(outcome.Status)
				}
				s.messages = append(s.messages, transcriptMessage{Role: "error", Text: message})
				return
			}
			if snapshotErr == nil && snapshot.Session.ID != "" {
				s.applySnapshot(snapshot)
				nextRunID = snapshot.ActiveRunID
				if s.activeBash == nil {
					nextBashID = snapshot.ActiveBashExecutionID
				}
				return
			}
			s.messages = append(s.messages, transcriptMessage{Role: "assistant", Text: outcome.Text})
		})
		if nextRunID != "" && s.bound != nil {
			s.watchSession(s.bound, s.operation, nextRunID)
		}
		if nextBashID != "" && s.bound != nil {
			s.resumeBash(s.bound, s.operation, nextBashID)
		}
	})
}

func (s *appState) dismiss(_ ui.EventContext) {
	if s.configurationPicker.Mode != configurationPickerClosed {
		s.SetState(func() { s.configurationPicker.Close() })
		return
	}
	if s.sessionDetailsOpen {
		s.SetState(func() { s.sessionDetailsOpen = false })
		return
	}
	if s.sessionExplorer.Open {
		if s.sessionExplorer.DeleteOpen {
			if !s.sessionExplorer.DeletePending {
				s.SetState(func() { s.sessionExplorer.CancelDelete() })
			}
			return
		}
		if s.sessionExplorer.RenameOpen {
			if !s.sessionExplorer.RenamePending {
				s.SetState(func() { s.sessionExplorer.CancelRename() })
			}
			return
		}
		s.cancelSessionSwitch()
		if s.sessionExplorer.Switching {
			s.SetState(func() { s.sessionExplorer.CancelSwitch() })
		} else {
			s.SetState(func() { s.sessionExplorer.Close() })
		}
		return
	}
	if s.bashHistory.Open {
		s.SetState(func() { s.bashHistory.Close() })
		return
	}
	if s.palette.Open {
		s.SetState(func() { s.palette.Close() })
		return
	}
	switch s.phase {
	case phaseAuthSelect:
		s.SetState(func() {
			s.phase = authSelectionDismissTarget(s.authReturnReady)
			s.authReturnReady = false
			s.errorText = ""
			s.status = ""
			s.authFilter = ""
			s.authSelection = 0
		})
	case phaseAuthAPIKey:
		if s.authPending {
			return
		}
		fallthrough
	case phaseAuthWaiting:
		s.cancelLogin()
		s.SetState(func() {
			s.phase = phaseAuthSelect
			s.errorText = ""
			s.status = ""
			s.instructions = auth.OpenAICodexDeviceInstructions{}
			s.authProviderID = ""
			s.authAPIKey = ""
			s.authPending = false
		})
	case phaseReady:
		if s.activeBashID != "" {
			s.abortBash()
			return
		}
		if s.bashStarting {
			if s.bashAdmission != nil {
				s.bashAdmission.abort.Store(true)
			}
			s.SetState(func() { s.status = "Stopping bash…" })
			return
		}
		if !s.runPending {
			return
		}
		run := s.activeRun
		runID := s.activeRunID
		bound := s.bound
		admission := s.prompt
		if admission != nil {
			admission.abort.Store(true)
		}
		s.SetState(func() {
			s.runStopping = true
			s.turnActivity = "Stopping…"
			s.turnThinking = ""
			s.status = "esc abort · ctrl+c detach"
		})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if run != nil {
				_ = run.Abort(ctx)
				return
			}
			if bound != nil && runID != "" {
				_ = bound.Abort(ctx, runID)
			}
		}()
	}
}

func authSelectionDismissTarget(returnReady bool) phase {
	if returnReady {
		return phaseReady
	}
	return phaseAuthGate
}

func (s *appState) providerAvailable(model string) bool {
	provider := modelProvider(model)
	s.availableMu.RLock()
	defer s.availableMu.RUnlock()
	if len(s.available) == 0 {
		return true
	}
	return s.available[provider]
}

func modelProvider(model string) string {
	provider, _, ok := strings.Cut(model, "/")
	if !ok {
		return ""
	}
	return provider
}

func cloneProviders(input map[string]bool) map[string]bool {
	output := make(map[string]bool, len(input)+1)
	for provider, available := range input {
		output[provider] = available
	}
	return output
}
