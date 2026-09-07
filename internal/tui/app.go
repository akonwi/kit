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
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
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
	Context            context.Context
	Server             sessionclient.Server
	CWD                string
	Location           string
	DefaultModel       string
	DefaultThinking    string
	AvailableProviders map[string]bool
	Authenticated      bool
	Login              DeviceLogin
	APIKeyLogin        APIKeyLogin

	appDone <-chan struct{}
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
	runContext, cancel := context.WithCancel(options.Context)
	done := make(chan struct{})
	options.Context = runContext
	options.appDone = done
	err := ui.Run(app{Options: options}, ui.WithShortcuts(nativeRootShortcuts()))
	close(done)
	cancel()
	return err
}

func nativeRootShortcuts() ui.ShortcutMap {
	return ui.ShortcutMap{"Escape": ui.DismissIntent{}}
}

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

	ctx    context.Context
	cancel context.CancelFunc

	phase                       phase
	errorText                   string
	status                      string
	composer                    string
	composerCursorEndGeneration uint64
	palette                     paletteController
	sessionExplorer             sessionExplorerController
	authReturnReady             bool
	authFilter                  string
	authSelection               int
	authProviderID              string
	authAPIKey                  string
	authPending                 bool
	session                     protocol.SessionInfo
	bound                       sessionclient.Session
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
	prompt                      *promptAdmission
	activeBash                  sessionclient.BashExecution
	activeBashID                string
	bashStarting                bool
	bashAdmission               *bashAdmission
	bashCollapsed               map[string]bool
	bashHistory                 bashHistoryController

	instructions auth.OpenAICodexDeviceInstructions
	remaining    time.Duration
	loginCancel  context.CancelFunc
	operation    uint64

	availableMu sync.RWMutex
	available   map[string]bool
}

func (s *appState) InitState() {
	options := s.Widget().(app).Options
	s.ctx, s.cancel = context.WithCancel(options.Context)
	s.available = cloneProviders(options.AvailableProviders)
	s.liveAssistant = -1
	s.liveTools = make(map[string]int)
	s.liveContent = make(map[int]liveContentBlock)
	s.activityExpanded = make(map[activityToolKey]bool)
	s.bashCollapsed = make(map[string]bool)
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

func (s *appState) TickFrame(_ time.Time) bool {
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

func (s *appState) requestTranscriptScroll() {
	s.needsScroll = true
	s.scrollPendingLayout = true
}

func (s *appState) requestActivityScroll(toEnd bool) {
	s.activityNeedsScroll = true
	s.activityPendingLayout = true
	s.activityScrollToEnd = toEnd
}

func (s *appState) Dispose() {
	if s.loginCancel != nil {
		s.loginCancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *appState) Build(ctx ui.BuildContext) ui.Widget {
	options := s.Widget().(app).Options
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
		Location:                    options.Location,
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
				s.SetState(func() { s.status = "Could not open browser: " + err.Error() })
				ctx.Notify("Could not open browser", err.Error())
				return
			}
			s.SetState(func() { s.status = "Opened browser" })
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
				s.SetState(func() { s.status = "Device code copied" })
			}
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
		SelectSession: func(_ ui.EventContext, sessionID string) {
			s.SetState(func() { s.sessionExplorer.Select(sessionID) })
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
			s.startBootstrap(options.DefaultModel, options.DefaultThinking)
		},
		Quit: func(ctx ui.EventContext) {
			if s.sessionExplorer.Open {
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
	if s.sessionExplorer.Open {
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
	options := s.Widget().(app).Options
	runtime := s.Context().Runtime()
	s.operation++
	operation := s.operation
	go func() {
		info, bound, snapshot, err := bootstrapSession(
			s.ctx, options.Server, options.CWD, defaultModel, defaultThinking, s.providerAvailable,
		)
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if operation != s.operation {
				return
			}
			if err != nil {
				s.SetState(func() {
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
				s.session = info
				s.bound = bound
				s.applySnapshot(snapshot)
				if running {
					s.status = "esc abort · ctrl+c detach"
				}
			})
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
	providerAvailable func(string) bool,
) (protocol.SessionInfo, sessionclient.Session, protocol.SessionSnapshot, error) {
	sessions, err := server.ListSessions(ctx, cwd)
	if err != nil {
		return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, fmt.Errorf("list sessions: %w", err)
	}
	var selected protocol.SessionInfo
	for _, candidate := range sessions {
		if providerAvailable(candidate.Model) {
			selected = candidate
			break
		}
	}
	if selected.ID == "" {
		if defaultModel == "" {
			return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, errors.New("no authenticated model is available")
		}
		selected, err = server.CreateSession(ctx, protocol.CreateSessionInput{
			CWD:           cwd,
			Model:         defaultModel,
			ThinkingLevel: defaultThinking,
		})
		if err != nil {
			return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, fmt.Errorf("create session: %w", err)
		}
	}
	bound, err := server.Attach(ctx, selected.ID)
	if err != nil {
		return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, fmt.Errorf("attach session: %w", err)
	}
	snapshot, err := bound.Snapshot(ctx)
	if err != nil {
		return protocol.SessionInfo{}, nil, protocol.SessionSnapshot{}, fmt.Errorf("snapshot session: %w", err)
	}
	return selected, bound, snapshot, nil
}

func (s *appState) applySnapshot(snapshot protocol.SessionSnapshot) {
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
	s.activeRunID = snapshot.ActiveRunID
	s.runPending = snapshot.ActiveRunID != ""
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

func (s *appState) applyRunEvents(events []protocol.SessionEvent) {
	transcriptChanged := false
	for _, event := range events {
		if event.Sequence <= s.liveSequence {
			continue
		}
		s.liveSequence = event.Sequence
		switch event.Kind {
		case protocol.SessionEventRunStarted:
			s.setTurnThinking("")
			s.setTurnActivity("Working…")
		case protocol.SessionEventUserMessage:
			transcriptChanged = true
			if s.turnActivity == "" {
				s.setTurnActivity("Working…")
			}
			if !s.liveHasUser {
				s.liveMessages = append(s.liveMessages, transcriptMessage{ID: "live-user:" + event.TurnID, TurnID: event.TurnID, Role: "user", Text: event.Text})
				s.liveHasUser = true
			} else {
				for index := range s.liveMessages {
					if s.liveMessages[index].Role == "user" && s.liveMessages[index].TurnID == "" {
						s.liveMessages[index].ID = "live-user:" + event.TurnID
						s.liveMessages[index].TurnID = event.TurnID
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
				if event.IsError {
					s.liveMessages[index].ToolStatus = "Failed"
				} else {
					s.liveMessages[index].ToolStatus = "Completed"
				}
			}
		case protocol.SessionEventRunFinished:
			transcriptChanged = true
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
			streamContext, cancel := context.WithCancel(s.ctx)
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
			case <-s.ctx.Done():
				return
			case events, ok := <-updates:
				if !ok {
					updates = nil
					if streamCancel != nil {
						streamCancel()
						streamCancel = nil
					}
					if stream != nil && stream.Err() != nil && s.ctx.Err() == nil {
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
						s.SetState(func() { s.applyRunEvents(batch) })
					}
				})
			case <-ticker.C:
				info, err := bound.Run(s.ctx, runID)
				if err != nil {
					if s.ctx.Err() == nil {
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
				snapshot, err := bound.Snapshot(s.ctx)
				if err != nil {
					snapshotFailures++
					if s.ctx.Err() != nil {
						return
					}
					if snapshotFailures >= 6 {
						runtime.Dispatch(func() {
							if operation == s.operation {
								s.SetState(func() { s.settleRunWithoutSnapshot(info, err) })
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
					case <-s.ctx.Done():
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
	s.operation++
	operation := s.operation
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
			if operation != s.operation {
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
			s.startBootstrap(provider.DefaultModel, options.DefaultThinking)
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
	s.operation++
	operation := s.operation
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
				if operation != s.operation {
					return
				}
				s.SetState(func() {
					s.instructions = instructions
					s.remaining = time.Until(instructions.ExpiresAt)
					s.status = "Waiting for approval…"
				})
			})
			go s.tickDeviceExpiry(loginContext, runtime, operation, instructions.ExpiresAt)
			return nil
		})
		wasCanceled := loginContext.Err() != nil
		cancel()
		if wasCanceled || s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if operation != s.operation {
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
			s.startBootstrap(codexDefaultModel, options.DefaultThinking)
		})
	}()
}

func (s *appState) tickDeviceExpiry(ctx context.Context, runtime ui.Runtime, operation uint64, expiresAt time.Time) {
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
				if operation != s.operation {
					return
				}
				s.SetState(func() { s.remaining = remaining })
			})
		}
	}
}

func (s *appState) cancelLogin() {
	s.operation++
	if s.loginCancel != nil {
		s.loginCancel()
		s.loginCancel = nil
	}
}

func (s *appState) hasActiveWork() bool {
	return s.runPending || s.bashStarting || s.activeBashID != ""
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
	if !s.palette.Open || !paletteCommandAvailable(commandID, s.hasActiveWork()) {
		return
	}
	s.SetState(func() { s.palette.Close() })
	switch commandID {
	case paletteCommandLogin:
		s.enterAuthSelect(true)
	case paletteCommandAbort:
		s.dismiss(ctx)
	case paletteCommandQuit:
		ctx.Quit()
	case paletteCommandSessions:
		s.openSessionExplorer()
	}
}

func (s *appState) openSessionExplorer() {
	if s.phase != phaseReady || s.sessionExplorer.Open || s.hasActiveWork() {
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
	if command, excludeFromContext, ok := parseDirectBash(value); ok {
		s.startDirectBash(value, command, excludeFromContext)
		return
	}
	text := strings.TrimSpace(value)
	if text == "" || s.bound == nil {
		return
	}
	if s.runPending {
		s.SetState(func() { s.status = "Run in progress · esc abort · ctrl+c detach" })
		return
	}
	admission := &promptAdmission{}
	bound := s.bound
	runtime := s.Context().Runtime()
	s.SetState(func() {
		s.composer = ""
		s.status = "esc abort · ctrl+c detach"
		s.resetLiveRun()
		s.turnActivity = "Working…"
		s.liveMessages = append(s.liveMessages, transcriptMessage{Role: "user", Text: text})
		s.liveHasUser = true
		s.requestTranscriptScroll()
		s.runPending = true
		s.prompt = admission
	})
	go func() {
		run, err := bound.StartPrompt(s.ctx, text)
		if err != nil {
			if s.ctx.Err() == nil {
				s.finishRun(runtime, protocol.PromptOutcome{}, err)
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
			s.SetState(func() {
				s.activeRun = run
				s.activeRunID = run.ID()
			})
			if admission.abort.Load() {
				go abort()
			}
			s.watchSession(bound, s.operation, run.ID())
		})
	}()
}

func (s *appState) finishRun(runtime ui.Runtime, outcome protocol.PromptOutcome, runErr error) {
	var snapshot protocol.SessionSnapshot
	var snapshotErr error
	if runErr == nil && outcome.Status == protocol.RunStatusCompleted && s.bound != nil {
		snapshot, snapshotErr = s.bound.Snapshot(s.ctx)
	}
	runtime.Dispatch(func() {
		nextRunID := ""
		nextBashID := ""
		s.SetState(func() {
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
	if s.sessionExplorer.Open {
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
