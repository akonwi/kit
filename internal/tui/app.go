// Package tui implements Kit's native vaxis terminal client.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	kittheme "github.com/akonwi/kit/internal/theme"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	defaultThinkingLevel = "medium"
	codexDefaultModel    = "openai-codex/gpt-5.6-sol"
	subagentReadTimeout  = 10 * time.Second
)

// DeviceLogin performs an application-facing provider device login.
type DeviceLogin interface {
	Login(context.Context, func(auth.OpenAICodexDeviceInstructions) error) error
}

// BrowserLogin performs Claude subscription browser OAuth.
type BrowserLogin interface {
	Login(context.Context, <-chan string, func(auth.AnthropicLoginInstructions) error) error
}

// APIKeyLogin installs an API-key credential and activates its provider.
type APIKeyLogin interface {
	Login(context.Context, string, string) error
}

// DiffPreferenceService persists working-tree diff presentation preferences.
type DiffPreferenceService interface {
	SetWrapLines(bool) error
}

// Options configures one native TUI client attached to one session.
type Options struct {
	Context               context.Context
	Server                sessionclient.Server
	CWD                   string
	Location              string
	ResolveLocation       func(context.Context, string) string
	DefaultModel          string
	DefaultThinking       string
	ResumeModelFilter     string
	ResumeThinkingFilter  string
	AvailableProviders    map[string]bool
	Authenticated         bool
	SessionID             string
	NewSessionID          string
	NewSessionName        string
	TemporarySession      bool
	Login                 DeviceLogin
	BrowserLogin          BrowserLogin
	APIKeyLogin           APIKeyLogin
	ThemeName             string
	ThemeDefinition       kittheme.Definition
	ThemeService          ThemeService
	ModelOverrideService  ModelOverrideService
	DiffWrapLines         bool
	DiffPreferenceService DiffPreferenceService

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
	err := ui.Run(
		themeAdapter{Definition: options.ThemeDefinition, Child: app{Options: options}},
		ui.WithShortcuts(nativeRootShortcuts()),
	)
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
	phaseAuthBrowser
	phaseAuthAPIKey
	phaseReady
	phaseFailed
)

type stagedAttachment struct {
	Token      uint64
	Info       protocol.AttachmentInfo
	Filename   string
	Uploading  bool
	Error      string
	PreserveID bool
}

type transcriptMessage struct {
	ID                     string
	Sequence               int64
	TurnID                 string
	Role                   string
	Text                   string
	Thinking               string
	Content                []protocol.TranscriptContent
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
	StopReason             string
	ErrorMessage           string
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

type subagentDiagnosticToastKey struct {
	SessionID string
	Severity  string
	Code      string
	Message   string
	Kind      string
	Path      string
	PluginID  string
}

func (a app) CreateState() ui.State { return &appState{} }

type appState struct {
	inputControl       *controlFocusState
	pasteControl       *controlFocusState
	inputReturn        shellFocusReturn
	paneInput          paneInputOwner
	replacingPalette   bool
	renderedInput      inputToken
	inputGeneration    uint64
	previousInputOwner inputOwner
	pasteOwner         inputToken
	ui.StateBase

	ctx                     context.Context
	cancel                  context.CancelFunc
	attachmentCtx           context.Context
	attachmentCancel        context.CancelFunc
	runWatchCancel          context.CancelFunc
	runWatchID              string
	runWatchGeneration      uint64
	notifiedRunIDs          map[string]bool
	notifiedRunOrder        []string
	sessionWatchCancel      context.CancelFunc
	deferredSessionSnapshot *protocol.SessionSnapshot
	subagentWatchCancel     context.CancelFunc

	phase                            phase
	errorText                        string
	status                           string
	toasts                           toastController
	toastCancels                     map[uint64]context.CancelFunc
	eventToasts                      []toastInput
	showToastOverride                func(toastInput)
	composer                         string
	composerCursorEndGeneration      uint64
	composerDraftGeneration          uint64
	composerCursorOffset             int
	composerCursorGeneration         uint64
	palette                          paletteController
	themePicker                      themePickerController
	themeName                        string
	themeDefinition                  kittheme.Definition
	themeGeneration                  uint64
	themeLoadActive                  bool
	applyTheme                       func(kittheme.Definition)
	paste                            pasteCoalescer
	fileMention                      fileMentionController
	sessionMention                   sessionMentionController
	sessionMentions                  sessionMentionSource
	sessionMentionCancel             context.CancelFunc
	indexedFiles                     indexedFileSource
	configurationPicker              configurationPickerController
	compactPending                   bool
	compactOperationID               string
	sessionDetailsOpen               bool
	sessionRename                    currentSessionRenameController
	annotationPicker                 annotationPickerController
	sessionExplorer                  sessionExplorerController
	authReturnReady                  bool
	authFilter                       string
	authSelection                    int
	authProviderID                   string
	authAPIKey                       string
	authPending                      bool
	session                          protocol.SessionInfo
	bound                            sessionclient.Session
	location                         string
	locationBase                     string
	vcsStatus                        *protocol.VCSStatus
	vcsContext                       context.Context
	vcsCancel                        context.CancelFunc
	sessionDrafts                    map[string]string
	sessionDraftAttachments          map[string][]stagedAttachment
	sessionDraftAttachmentIDs        map[string][]string
	sessionSwitchCancel              context.CancelFunc
	sessionSwitchGeneration          uint64
	sessionCreateCancel              context.CancelFunc
	sessionCreateGeneration          uint64
	sessionCreatePending             bool
	messages                         []transcriptMessage
	liveMessages                     []transcriptMessage
	transcriptList                   ui.SliverListController
	transcriptHistoryInitialized     bool
	transcriptHistoryCursor          string
	transcriptHistoryHasMore         bool
	transcriptHistoryLoading         bool
	transcriptHistoryError           string
	transcriptHistoryGeneration      uint64
	transcriptHistoryAnchorID        string
	transcriptHistoryAnchorInset     int
	transcriptHistoryAnchorExpected  int
	transcriptHistoryAnchorInput     uint64
	transcriptHistoryInputGeneration uint64
	transcriptInitialLoading         bool
	transcriptInitialPositioned      bool
	transcriptInitialStable          bool
	transcriptInitialMetrics         ui.ScrollMetrics
	transcriptHistoryUserScroll      bool
	transcriptHistoryScrollInput     bool
	transcriptHistoryLastOffset      int
	transcriptHistoryAnchorEnd       bool
	transcriptHistoryRestore         int
	liveAssistant                    int
	liveHasUser                      bool
	liveTools                        map[string]int
	liveContent                      map[int]liveContentBlock
	liveSequence                     int64
	liveStreamID                     string
	metadataStreamID                 string
	metadataSequence                 int64
	turnActivity                     string
	turnThinking                     string
	followUps                        protocol.FollowUpQueue
	composerAttachmentIDs            []string
	composerAttachments              []stagedAttachment
	annotations                      []protocol.AnnotationSummary
	attachmentUploadGeneration       uint64
	pendingInteractions              []protocol.InteractionRequest
	followUpMutationPending          bool
	runStopping                      bool
	providerRetry                    *protocol.ProviderRetry
	activeCompactionID               string
	compactionOutcomeIDs             map[string]struct{}
	compactionOutcomeOrder           []string
	contextTokens                    int
	contextWindow                    int
	sessionUsage                     protocol.SessionUsage
	scroll                           ui.ScrollController
	activityScroll                   ui.ScrollController
	activityList                     activityListController
	activityFocus                    ui.FocusNode
	subagentFocuses                  map[string]*ui.FocusNode
	workspace                        workspaceController
	workspaceMouse                   workspaceMouseGestureController
	diffWrapLines                    bool
	diffPreferenceMu                 sync.Mutex
	diffPreferenceDesired            bool
	diffPreferenceGeneration         uint64
	diffPreferenceWriting            bool
	diffPreferenceWrites             sync.WaitGroup
	workspaceID                      string
	toolFileNavigationGeneration     uint64
	toolFileNavigationCancel         context.CancelFunc
	workspaceFilePicker              workspaceFilePickerController
	workspaceFilePickerScroll        ui.ScrollController
	workspaceFilePickerContext       context.Context
	workspaceFilePickerCancel        context.CancelFunc
	filePickerRefreshHook            func()
	filePickerLoadHook               func(string, string)
	workspaceFilePickerRevealPending bool
	workspaceFilePickerRevealOffset  int
	workspacePickerOpen              bool
	workspacePickerQuery             string
	workspacePickerSelection         int
	workspacePickerScroll            ui.ScrollController
	workspacePickerRevealPending     bool
	workspacePickerRevealOffset      int
	workspaceLayout                  workspaceLayoutState
	activitySourceID                 string
	activityConversationID           string
	activitySelected                 bool
	subagentsOpen                    bool
	subagentFilter                   string
	subagentDefinitions              []protocol.SubagentDefinition
	subagentDiagnostics              []protocol.SubagentDiagnostic
	subagentDiagnosticToasts         map[subagentDiagnosticToastKey]struct{}
	subagentConversations            []protocol.SubagentConversation
	subagentSelection                string
	subagentPendingAgent             string
	subagentPaneID                   string
	subagentTranscripts              map[string]protocol.SubagentTranscript
	subagentTranscriptErrors         map[string]string
	subagentTranscriptLoads          map[string]uint64
	subagentTranscriptLoading        map[string]bool
	subagentTranscriptOrder          []string
	subagentScrolls                  map[string]*ui.ScrollController
	subagentScrollToEndID            string
	subagentNeedsScroll              bool
	subagentPendingLayout            bool
	subagentLive                     map[string]protocol.SubagentLiveEventPage
	subagentLiveLoads                map[string]uint64
	subagentLiveLoading              map[string]bool
	subagentRequestGeneration        uint64
	subagentRosterGeneration         uint64
	subagentRosterLoading            bool
	subagentRosterRefreshPending     bool
	subagentRevealPending            bool
	subagentRevealOffset             int
	subagentDismissID                string
	subagentDismissName              string
	subagentDismissGeneration        uint64
	subagentDismissPending           bool
	subagentDismissError             string
	inlineActivityOpen               map[string]bool
	activityExpanded                 map[activityToolKey]bool
	activityCursor                   activityToolKey
	needsScroll                      bool
	scrollPendingLayout              bool
	transcriptVisible                bool
	transcriptPinnedOnHide           bool
	activeRun                        sessionclient.Run
	activeRunID                      string
	runPending                       bool
	terminalRunActive                bool
	terminalRunID                    string
	terminalSettledRunID             string
	agentFeedbackPending             bool
	reloadPending                    bool
	cwdPending                       bool
	prompt                           *promptAdmission
	activeBash                       sessionclient.BashExecution
	activeBashID                     string
	bashStarting                     bool
	bashAdmission                    *bashAdmission
	bashCollapsed                    map[string]bool
	transcriptAnnotationsExpanded    map[string]bool
	bashHistory                      bashHistoryController

	instructions        auth.OpenAICodexDeviceInstructions
	browserInstructions auth.AnthropicLoginInstructions
	remaining           time.Duration
	authCode            string
	authCodeInput       chan string
	loginCancel         context.CancelFunc
	loginGeneration     uint64
	operation           uint64
	bootstrapModel      string
	bootstrapThinking   string
	newSessionPending   bool
	bootstrapTarget     protocol.SessionInfo

	availableMu    sync.RWMutex
	available      map[string]bool
	terminalCWD    string
	terminalStatus *terminalStatusReporter
}

func (s *appState) InitState() {
	options := s.Widget().(app).Options
	s.themeName = options.ThemeName
	if s.themeName == "" {
		s.themeName = kittheme.SystemName
	}
	s.themeDefinition = options.ThemeDefinition
	s.diffWrapLines = options.DiffWrapLines
	s.ctx, s.cancel = context.WithCancel(options.Context)
	s.resetAttachmentContext()
	s.available = cloneProviders(options.AvailableProviders)
	s.terminalCWD = options.CWD
	s.terminalStatus = options.terminalStatus
	s.transcriptVisible = true
	s.liveAssistant = -1
	s.liveTools = make(map[string]int)
	s.liveContent = make(map[int]liveContentBlock)
	s.activityExpanded = make(map[activityToolKey]bool)
	s.inlineActivityOpen = make(map[string]bool)
	s.bashCollapsed = make(map[string]bool)
	s.transcriptAnnotationsExpanded = make(map[string]bool)
	s.toastCancels = make(map[uint64]context.CancelFunc)
	s.location = options.Location
	s.locationBase = options.Location
	s.sessionDrafts = make(map[string]string)
	s.sessionDraftAttachments = make(map[string][]stagedAttachment)
	s.sessionDraftAttachmentIDs = make(map[string][]string)
	s.subagentTranscripts = make(map[string]protocol.SubagentTranscript)
	s.subagentTranscriptErrors = make(map[string]string)
	s.subagentTranscriptLoads = make(map[string]uint64)
	s.subagentTranscriptLoading = make(map[string]bool)
	s.subagentDiagnosticToasts = make(map[subagentDiagnosticToastKey]struct{})
	s.subagentScrolls = make(map[string]*ui.ScrollController)
	s.subagentFocuses = make(map[string]*ui.FocusNode)
	s.subagentLive = make(map[string]protocol.SubagentLiveEventPage)
	s.subagentLiveLoads = make(map[string]uint64)
	s.subagentLiveLoading = make(map[string]bool)
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
	positioning := s.transcriptInitialLoading || s.needsScroll || s.transcriptHistoryRestore != 0
	if s.transcriptInitialLoading {
		keepTicking = s.settleInitialTranscript()
	}
	if !s.transcriptInitialLoading && !positioning {
		s.observeTranscriptScroll()
	} else {
		s.transcriptHistoryLastOffset = s.scroll.Metrics().ScrollOffset
		s.transcriptHistoryScrollInput = false
	}
	if !s.transcriptInitialLoading && s.needsScroll && s.transcriptHistoryRestore == 0 {
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
	if s.restoreTranscriptHistoryAnchor() {
		keepTicking = true
	}
	if !positioning && s.maybeLoadTranscriptHistory() {
		keepTicking = true
	}
	if s.subagentNeedsScroll {
		controller := s.subagentScrolls[s.subagentScrollToEndID]
		if s.subagentPendingLayout {
			if controller != nil && controller.Attached() {
				controller.ScrollToEnd()
			}
			s.subagentPendingLayout = false
			keepTicking = true
		} else {
			if controller != nil && controller.Attached() {
				controller.ScrollToEnd()
			}
			s.subagentNeedsScroll = false
		}
	}
	s.sessionRename.TickFrame()
	if s.sessionExplorer.TickFrame() {
		keepTicking = true
	}
	if s.workspaceFilePickerRevealPending {
		if s.workspaceFilePickerScroll.Attached() {
			s.workspaceFilePickerScroll.ScrollToOffset(s.workspaceFilePickerRevealOffset)
			s.workspaceFilePickerRevealPending = false
		} else {
			keepTicking = true
		}
	}
	if s.workspacePickerRevealPending {
		if s.workspacePickerScroll.Attached() {
			s.workspacePickerScroll.ScrollToOffset(s.workspacePickerRevealOffset)
			s.workspacePickerRevealPending = false
		} else {
			keepTicking = true
		}
	}
	if s.subagentRevealPending {
		if s.activityScroll.Attached() {
			s.activityScroll.ScrollToOffset(s.subagentRevealOffset)
			s.subagentRevealPending = false
		} else {
			keepTicking = true
		}
	}
	return keepTicking || (s.needsScroll && !s.transcriptInitialLoading) || s.transcriptHistoryRestore != 0 || s.workspaceFilePickerRevealPending || s.workspacePickerRevealPending || s.subagentRevealPending || s.providerRetry != nil
}

// Keep pagination disabled until measured layout confirms the recent tail at
// the bottom across consecutive frames, rather than relying on a fixed delay.
func (s *appState) settleInitialTranscript() bool {
	if !s.transcriptVisible || s.phase != phaseReady {
		return false
	}
	count := len(s.mainTranscriptPresentation().Items)
	if count == 0 {
		s.SetState(func() { s.transcriptInitialLoading = false; s.transcriptInitialPositioned = true })
		s.needsScroll, s.scrollPendingLayout = false, false
		return true
	}
	if !s.scroll.Attached() {
		return false
	}
	metrics := s.scroll.Metrics()
	if metrics.ViewportHeight <= 0 {
		return false
	}
	if !s.transcriptList.Attached() {
		return true
	}
	_, last, visible := s.transcriptList.VisibleRange()
	atEnd := metrics.ScrollOffset == metrics.MaxScrollOffset && visible && last == count
	if atEnd && s.transcriptInitialStable && metrics == s.transcriptInitialMetrics {
		s.SetState(func() { s.transcriptInitialLoading = false; s.transcriptInitialPositioned = true })
		s.needsScroll, s.scrollPendingLayout = false, false
		s.transcriptHistoryLastOffset = metrics.ScrollOffset
		return true
	}
	s.transcriptInitialStable = atEnd
	s.transcriptInitialMetrics = metrics
	s.scroll.ScrollToEnd()
	return true
}

func (s *appState) observeTranscriptScroll() {
	if !s.transcriptVisible || !s.scroll.Attached() {
		return
	}
	offset := s.scroll.Metrics().ScrollOffset
	if s.transcriptHistoryScrollInput && offset < s.transcriptHistoryLastOffset {
		s.transcriptHistoryUserScroll = true
	}
	s.transcriptHistoryLastOffset = offset
	s.transcriptHistoryScrollInput = false
}

func (s *appState) noteTranscriptHistoryScrollUp(ui.EventContext) {
	if !s.transcriptVisible || s.transcriptInitialLoading || s.transcriptHistoryRestore != 0 {
		return
	}
	if s.needsScroll {
		// A transcript-directed upward scroll takes ownership from a deferred
		// follow request, including one scheduled while restoring the Agent pane.
		s.needsScroll = false
		s.scrollPendingLayout = false
	}
	s.transcriptHistoryUserScroll = true
}

func (s *appState) maybeLoadTranscriptHistory() bool {
	if s.transcriptInitialLoading || !s.transcriptVisible || s.needsScroll || !s.transcriptHistoryUserScroll || s.transcriptHistoryRestore != 0 || s.transcriptHistoryLoading || !s.transcriptHistoryHasMore || s.transcriptHistoryError != "" || s.phase != phaseReady {
		return false
	}
	first, _, ok := s.transcriptList.VisibleRange()
	if !ok || first > 2 {
		return false
	}
	s.loadTranscriptHistory()
	return s.transcriptHistoryLoading
}

func (s *appState) loadTranscriptHistory() {
	pager, ok := s.bound.(sessionclient.TranscriptPager)
	if !ok || s.transcriptHistoryLoading || !s.transcriptHistoryHasMore || s.transcriptHistoryCursor == "" {
		return
	}
	sessionID := s.session.ID
	cursor := s.transcriptHistoryCursor
	bound := s.bound
	attachmentContext := s.attachmentCtx
	var generation uint64
	s.SetState(func() {
		s.transcriptHistoryGeneration++
		generation = s.transcriptHistoryGeneration
		s.transcriptHistoryLoading = true
		s.transcriptHistoryUserScroll = false
		s.transcriptHistoryError = ""
	})
	runtime := s.Context().Runtime()
	go func() {
		requestContext, cancel := context.WithTimeout(attachmentContext, 30*time.Second)
		defer cancel()
		page, err := pager.TranscriptPage(requestContext, cursor)
		var refreshed *protocol.SessionSnapshot
		if errors.Is(err, sessionclient.ErrTranscriptCursorUnavailable) {
			snapshot, snapshotErr := bound.Snapshot(requestContext)
			if snapshotErr == nil {
				refreshed = &snapshot
			}
			err = snapshotErr
		} else if err == nil {
			if page.SessionID != sessionID {
				err = fmt.Errorf("transcript page belongs to another session")
			} else {
				err = page.ValidateBefore(cursor)
			}
		}
		if attachmentContext.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if s.session.ID != sessionID || s.transcriptHistoryGeneration != generation || !s.transcriptHistoryLoading || s.transcriptHistoryCursor != cursor {
				return
			}
			if err == nil && refreshed == nil {
				s.captureTranscriptHistoryAnchor()
			}
			s.SetState(func() {
				s.transcriptHistoryLoading = false
				if err != nil {
					s.transcriptHistoryError = err.Error()
					return
				}
				if refreshed != nil {
					if refreshed.ActiveRunID != "" && refreshed.ActiveRunID == s.activeRunID {
						// The bounded snapshot omits this active turn, so refresh only
						// durable state and leave its streamed projection intact.
						s.messages = projectTranscript(refreshed.Messages)
						s.resetTranscriptHistoryFromSnapshot(*refreshed)
					} else {
						// The run settled while recovery was in flight. Reconcile the
						// whole snapshot so its now-durable turn is not also kept live.
						s.messages = nil
						s.transcriptHistoryInitialized = false
						s.applySnapshot(*refreshed)
					}
					return
				}
				if mergeErr := s.prependTranscriptHistory(page); mergeErr != nil {
					s.transcriptHistoryError = mergeErr.Error()
				}
			})
		})
	}()
}

func (s *appState) retryTranscriptHistory(ui.EventContext) {
	s.SetState(func() { s.transcriptHistoryError = "" })
	s.loadTranscriptHistory()
}

func (s *appState) captureTranscriptHistoryAnchor() {
	metrics := s.scroll.Metrics()
	s.transcriptHistoryAnchorEnd = metrics.ScrollOffset >= metrics.MaxScrollOffset
	s.transcriptHistoryAnchorID = ""
	s.transcriptHistoryAnchorInset = 0
	s.transcriptHistoryAnchorExpected = metrics.ScrollOffset
	s.transcriptHistoryAnchorInput = s.transcriptHistoryInputGeneration
	if s.transcriptHistoryAnchorEnd {
		return
	}
	first, _, visible := s.transcriptList.VisibleRange()
	if !visible {
		return
	}
	presentation := s.mainTranscriptPresentation()
	if first < 0 || first >= len(presentation.Items) {
		return
	}
	s.needsScroll = false
	s.scrollPendingLayout = false
	s.transcriptHistoryAnchorID = presentation.Items[first].ID
	if offset, measured := s.transcriptList.OffsetForIndex(first); measured {
		s.transcriptHistoryAnchorInset = max(0, metrics.ScrollOffset-1-offset)
	}
}

func (s *appState) mainTranscriptPresentation() transcriptPresentation {
	messages := make([]transcriptMessage, 0, len(s.messages)+len(s.liveMessages))
	messages = append(messages, s.messages...)
	messages = append(messages, s.liveMessages...)
	return presentTranscript(messages)
}

func (s *appState) prependTranscriptHistory(page protocol.TranscriptPage) error {
	older := projectTranscript(page.Messages)
	sequenceByID := make(map[string]int64, len(s.messages))
	idBySequence := make(map[int64]string, len(s.messages))
	for _, message := range s.messages {
		if message.ID != "" {
			sequenceByID[message.ID] = message.Sequence
		}
		if message.Sequence > 0 {
			idBySequence[message.Sequence] = message.ID
		}
	}
	merged := make([]transcriptMessage, 0, len(older)+len(s.messages))
	for _, message := range older {
		if sequence, duplicate := sequenceByID[message.ID]; duplicate {
			return fmt.Errorf("earlier transcript overlaps message %q at sequence %d", message.ID, sequence)
		}
		if id, duplicate := idBySequence[message.Sequence]; duplicate && id != message.ID {
			return fmt.Errorf("earlier transcript overlaps conflicting sequence %d", message.Sequence)
		}
		merged = append(merged, message)
	}
	s.messages = append(merged, s.messages...)
	s.transcriptHistoryCursor = page.PreviousMessageCursor
	s.transcriptHistoryHasMore = page.HasMoreMessages
	if s.transcriptHistoryAnchorEnd {
		s.requestTranscriptScroll()
		s.transcriptHistoryAnchorEnd = false
	} else if s.transcriptHistoryAnchorID != "" {
		s.transcriptHistoryRestore = 1
	}
	return nil
}

func (s *appState) restoreTranscriptHistoryAnchor() bool {
	if s.transcriptHistoryRestore == 0 {
		return false
	}
	presentation := s.mainTranscriptPresentation()
	index := -1
	for candidate := range presentation.Items {
		if presentation.Items[candidate].ID == s.transcriptHistoryAnchorID {
			index = candidate
			break
		}
	}
	if index < 0 || !s.transcriptList.Attached() {
		s.transcriptHistoryRestore = 0
		s.transcriptHistoryAnchorID = ""
		return false
	}
	if s.transcriptHistoryInputGeneration != s.transcriptHistoryAnchorInput ||
		(s.transcriptHistoryRestore != 2 && s.scroll.Metrics().ScrollOffset != s.transcriptHistoryAnchorExpected) {
		s.transcriptHistoryRestore = 0
		s.transcriptHistoryAnchorID = ""
		return false
	}
	if s.transcriptHistoryRestore == 1 {
		s.transcriptList.ScrollToIndex(index, ui.ScrollAlignStart)
		s.transcriptHistoryRestore = 2
		return true
	}
	if s.transcriptHistoryRestore == 2 {
		// ScrollToIndex may be corrected during measured sliver layout. Capture
		// that settled position before distinguishing later user movement.
		s.transcriptHistoryAnchorExpected = s.scroll.Metrics().ScrollOffset
		s.transcriptHistoryRestore = 3
		return true
	}
	offset, ok := s.transcriptList.OffsetForIndex(index)
	if !ok || !s.scroll.Attached() {
		return true
	}
	s.scroll.ScrollToOffset(max(0, 1+offset+s.transcriptHistoryAnchorInset))
	s.transcriptHistoryRestore = 0
	s.transcriptHistoryAnchorID = ""
	return false
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

func (s *appState) storeToast(input toastInput) toastShowResult {
	result := s.toasts.Show(input)
	for _, evicted := range result.Evicted {
		if cancel := s.toastCancels[evicted]; cancel != nil {
			cancel()
			delete(s.toastCancels, evicted)
		}
	}
	return result
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
		result = s.storeToast(input)
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

func equivalentActivitySource(items []transcriptDisplayItem, previous transcriptDisplayItem) (transcriptDisplayItem, bool) {
	if previous.TurnID == "" {
		return transcriptDisplayItem{}, false
	}
	previousCalls := make(map[string]bool)
	for _, call := range displayItemToolCalls(previous) {
		previousCalls[call.ID] = true
	}
	for _, item := range items {
		if item.Kind != transcriptDisplayTurnWork || item.TurnID != previous.TurnID {
			continue
		}
		if len(previousCalls) == 0 {
			return item, true
		}
		for _, call := range displayItemToolCalls(item) {
			if previousCalls[call.ID] {
				return item, true
			}
		}
	}
	return transcriptDisplayItem{}, false
}

func (s *appState) requestTranscriptScroll() {
	if s.transcriptHistoryRestore != 0 {
		return
	}
	s.needsScroll = true
	s.scrollPendingLayout = true
}

func (s *appState) followTranscriptIfPinned() {
	if scrollControllerPinnedToEnd(&s.scroll) {
		s.requestTranscriptScroll()
	}
}

func scrollControllerPinnedToEnd(controller *ui.ScrollController) bool {
	if controller == nil || !controller.Attached() {
		return true
	}
	metrics := controller.Metrics()
	return metrics.ScrollOffset >= metrics.MaxScrollOffset
}

func (s *appState) resetAttachmentContext() {
	if s.runWatchCancel != nil {
		s.runWatchCancel()
		s.runWatchCancel = nil
	}
	s.runWatchID = ""
	s.runWatchGeneration++
	if s.sessionWatchCancel != nil {
		s.sessionWatchCancel()
		s.sessionWatchCancel = nil
	}
	if s.subagentWatchCancel != nil {
		s.subagentWatchCancel()
		s.subagentWatchCancel = nil
	}
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
	s.toolFileNavigationGeneration++
	if s.toolFileNavigationCancel != nil {
		s.toolFileNavigationCancel()
		s.toolFileNavigationCancel = nil
	}
	s.stopVCSMonitoring()
	if s.sessionWatchCancel != nil {
		s.sessionWatchCancel()
	}
	if s.subagentWatchCancel != nil {
		s.subagentWatchCancel()
	}
	if s.loginCancel != nil {
		s.loginCancel()
	}
	if s.sessionSwitchCancel != nil {
		s.sessionSwitchCancel()
	}
	if s.sessionCreateCancel != nil {
		s.sessionCreateCancel()
	}
	if s.attachmentCancel != nil {
		s.attachmentCancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.flushDiffPreferenceWrites(2 * time.Second)
}

func (s *appState) activityPresentation(mainMessages []transcriptMessage, conversationID string) transcriptPresentation {
	if conversationID != "" {
		conversation := protocol.SubagentConversation{ID: conversationID}
		for _, candidate := range s.subagentConversations {
			if candidate.ID == conversationID {
				conversation = candidate
				break
			}
		}
		messages := subagentPaneMessages(conversation, s.subagentTranscripts[conversationID], s.subagentLive[conversationID])
		return presentTranscript(messages)
	}
	return presentTranscript(mainMessages)
}

func (s *appState) Build(ctx ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	s.reconcileSessionMention()
	if control, ok := ui.Depend[ThemeControl](ctx); ok {
		s.applyTheme = control.Apply
	} else {
		s.applyTheme = func(kittheme.Definition) {}
	}
	presentedMessages := make([]transcriptMessage, 0, len(s.messages)+len(s.liveMessages))
	presentedMessages = append(presentedMessages, s.messages...)
	presentedMessages = append(presentedMessages, s.liveMessages...)
	var attachments sessionclient.AttachmentSession
	if capable, ok := s.bound.(sessionclient.AttachmentSession); ok {
		attachments = capable
	}
	snapshot := shellSnapshot{
		Phase:                        s.phase,
		Error:                        s.errorText,
		Status:                       s.status,
		Composer:                     s.composer,
		ComposerAttachments:          append([]stagedAttachment(nil), s.composerAttachments...),
		ComposerAnnotations:          s.composerAnnotations(),
		DiffWrapLines:                s.diffWrapLines,
		ComposerCursorEndGeneration:  s.composerCursorEndGeneration,
		ComposerCursorOffset:         s.composerCursorOffset,
		ComposerCursorGeneration:     s.composerCursorGeneration,
		PaletteOpen:                  s.palette.Open,
		PaletteQuery:                 s.palette.Query,
		PaletteSelection:             s.palette.Selection,
		PaletteCommands:              s.palette.Contributions,
		ThemePicker:                  s.themePicker.Snapshot(),
		ConfigurationPicker:          s.configurationPicker.Snapshot(),
		SessionDetailsOpen:           s.sessionDetailsOpen,
		SessionRename:                s.sessionRename.Snapshot(),
		AnnotationPicker:             annotationPickerSnapshot{Open: s.annotationPicker.Open, Selection: s.annotationPicker.Selection, Annotations: append([]protocol.AnnotationSummary(nil), s.annotations...)},
		SessionExplorer:              s.sessionExplorer.Snapshot(),
		AuthReturnReady:              s.authReturnReady,
		AuthFilter:                   s.authFilter,
		AuthSelection:                s.authSelection,
		AuthProviderID:               s.authProviderID,
		AuthAPIKey:                   s.authAPIKey,
		AuthCode:                     s.authCode,
		AuthPending:                  s.authPending,
		Session:                      s.session,
		Messages:                     presentedMessages,
		Attachments:                  attachments,
		Running:                      s.hasActiveWork(),
		AgentRunning:                 s.runPending,
		TurnActivity:                 s.presentedTurnActivity(time.Now()),
		TurnThinking:                 s.turnThinking,
		FollowUps:                    s.followUps,
		PendingInteractions:          append([]protocol.InteractionRequest(nil), s.pendingInteractions...),
		ContextTokens:                s.contextTokens,
		ContextWindow:                s.contextWindow,
		SessionUsage:                 s.sessionUsage,
		Scroll:                       &s.scroll,
		TranscriptList:               &s.transcriptList,
		TranscriptHistoryInitialized: s.transcriptHistoryInitialized,
		TranscriptHistoryHasMore:     s.transcriptHistoryHasMore,
		TranscriptHistoryLoading:     s.transcriptHistoryLoading,
		TranscriptInitialLoading:     s.transcriptInitialLoading,
		TranscriptHistoryError:       s.transcriptHistoryError,
		ActivityScroll:               &s.activityScroll,
		ActivityList:                 &s.activityList,
		ActivityFocus:                &s.activityFocus,
		SubagentFocuses:              cloneFocusNodeMap(s.subagentFocuses),
		Workspace:                    s.workspace.Snapshot(),
		CurrentWorkspaceID:           s.workspaceID,
		PaneInput:                    s.paneInput,
		WorkspaceFilePicker:          s.workspaceFilePicker,
		WorkspaceFilePickerScroll:    &s.workspaceFilePickerScroll,
		WorkspacePickerOpen:          s.workspacePickerOpen,
		WorkspacePickerQuery:         s.workspacePickerQuery,
		WorkspacePickerSelection:     s.workspacePickerSelection,
		WorkspacePickerScroll:        &s.workspacePickerScroll,
		WorkspaceLayout:              &s.workspaceLayout,
		ActivitySourceID:             s.activitySourceID,
		ActivityConversationID:       s.activityConversationID,
		ActivitySelected:             s.activitySelected,
		SubagentsOpen:                s.subagentsOpen,
		SubagentFilter:               s.subagentFilter,
		SubagentDefinitions:          append([]protocol.SubagentDefinition(nil), s.subagentDefinitions...),
		SubagentDiagnostics:          append([]protocol.SubagentDiagnostic(nil), s.subagentDiagnostics...),
		SubagentConversations:        append([]protocol.SubagentConversation(nil), s.subagentConversations...),
		SubagentSelection:            s.subagentSelection,
		SubagentTranscripts:          cloneSubagentTranscripts(s.subagentTranscripts),
		SubagentTranscriptErrors:     cloneStringMap(s.subagentTranscriptErrors),
		SubagentScrolls:              cloneScrollControllerMap(s.subagentScrolls),
		SubagentLive:                 cloneSubagentLive(s.subagentLive),
		SubagentDismissID:            s.subagentDismissID,
		SubagentDismissName:          s.subagentDismissName,
		SubagentDismissPending:       s.subagentDismissPending,
		SubagentDismissError:         s.subagentDismissError,
		InlineActivityOpen:           s.inlineActivityOpen,
		ActivityExpanded:             s.activityExpanded,
		ActivityCursor:               s.activityCursor,
		BashRunning:                  s.activeBashID != "",
		BashStarting:                 s.bashStarting,
		BashCollapsed:                s.bashCollapsed,
		AnnotationsExpanded:          s.transcriptAnnotationsExpanded,
		BashHistory:                  s.bashHistory,
		FileMention:                  s.fileMention,
		SessionMention:               s.sessionMention,
		SessionMentions:              s.sessionMentions,
		IndexedFiles:                 s.indexedFiles,
		Instructions:                 s.instructions,
		BrowserInstructions:          s.browserInstructions,
		Remaining:                    s.remaining,
		Location:                     s.location,
		Toasts:                       s.toasts.Snapshot(),
	}
	callbacks := shellCallbacks{
		OpenActivityFile: func(_ ui.EventContext, target toolFileTarget) {
			target.SessionID = snapshot.Session.ID
			target.WorkspaceID = snapshot.CurrentWorkspaceID
			target.CWD = snapshot.Session.CWD
			s.openToolFile(target)
		},
		InputOwner:       s.inputOwner,
		PaneInputChanged: s.setPaneInputOwner,
		WorkspaceMouse:   &s.workspaceMouse,
		SetDiffWrapLines: s.setDiffWrapLines,
		ShowDiffWarning: func(message string) {
			s.showToast(toastInput{Title: "Diff target unchanged", Subtitle: message, Variant: toastWarning})
		},
		ShowDiffNotice: func(message string) {
			s.showToast(toastInput{Title: "Viewing committed target", Subtitle: message, Variant: toastInfo})
		},
		OpenAuth: func(ui.EventContext) {
			if s.phase == phaseAuthGate {
				s.enterAuthSelect(false)
			}
		},
		SelectProvider:            s.selectProvider,
		RetryTranscriptHistory:    s.retryTranscriptHistory,
		TranscriptHistoryScrollUp: s.noteTranscriptHistoryScrollUp,
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
		AuthCodeChanged: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.authCode = value })
		},
		SubmitAuthCode: s.submitAuthCode,
		OpenURL: func(ctx ui.EventContext, raw string) {
			if err := openExternalURL(raw); err != nil {
				s.showToast(toastInput{Title: "Could not open browser", Subtitle: err.Error(), Variant: toastError})
				ctx.Notify("Could not open browser", err.Error())
				return
			}
			s.showToast(toastInput{Title: "Opened browser", Variant: toastInfo})
		},
		OpenActivity: func(_ ui.EventContext, sourceID string) {
			s.SetState(func() {
				s.activityConversationID = ""
				presentation := s.activityPresentation(presentedMessages, "")
				opening := !activitySourceIsOpen(s.inlineActivityOpen, presentation, sourceID, s.runPending)
				for id := range s.inlineActivityOpen {
					s.inlineActivityOpen[id] = false
				}
				s.inlineActivityOpen[sourceID] = opening
				if !opening {
					s.activitySourceID = ""
					s.activityCursor = activityToolKey{}
					return
				}
				s.inlineActivityOpen[sourceID] = true
				s.activitySourceID = sourceID
				keys := activityToolKeys(presentation, sourceID)
				if len(keys) > 0 {
					s.activityCursor = keys[0]
				}
			})
		},
		ShowTranscript: func(ctx ui.EventContext) {
			s.cancelPendingSubagentToolOpen()
			if s.activityFocus.HasFocus() {
				ctx.FocusNext()
			}
			s.SetState(func() {
				s.clearSubagentActivityForConversationChange("")
				s.workspace.SelectAgent()
				s.syncWorkspaceSelection()
				s.activitySelected = false
				s.subagentPaneID = ""
			})
		},
		CloseActivity: func(ctx ui.EventContext) {
			if s.activityFocus.HasFocus() {
				ctx.FocusNext()
			}
			if s.subagentsOpen {
				s.closeSubagents()
				return
			}
			s.SetState(func() {
				s.activitySourceID = ""
				s.activityConversationID = ""
				s.syncWorkspaceSelection()
				s.inlineActivityOpen = make(map[string]bool)
				s.activityExpanded = make(map[activityToolKey]bool)
				s.activityCursor = activityToolKey{}
			})
		},
		CancelSubagentTask: func(_ ui.EventContext, taskID string, generation uint64) {
			s.cancelSubagentTask(taskID, generation)
		},
		DismissSubagent: func(_ ui.EventContext, conversationID string, generation uint64) {
			s.requestSubagentDismiss(conversationID, generation)
		},
		SelectSubagent: func(_ ui.EventContext, name string) {
			s.SetState(func() { s.subagentSelection = name })
		},
		MoveSubagentSelection: func(_ ui.EventContext, delta int) {
			s.SetState(func() {
				items := filteredSubagentRosterItems(subagentRosterItems(s.subagentDefinitions, s.subagentConversations), s.subagentFilter)
				if len(items) == 0 {
					s.subagentSelection = ""
					return
				}
				index := 0
				for itemIndex, item := range items {
					if item.Name == s.subagentSelection {
						index = itemIndex
						break
					}
				}
				index = max(0, min(len(items)-1, index+delta))
				s.subagentSelection = items[index].Name
				s.activityScroll.ScrollToOffset(subagentRosterOffset(items, index))
			})
		},
		SubagentFilterChanged: func(_ ui.EventContext, query string) {
			s.SetState(func() {
				s.subagentFilter = query
				items := filteredSubagentRosterItems(subagentRosterItems(s.subagentDefinitions, s.subagentConversations), query)
				if len(items) == 0 {
					s.subagentSelection = ""
				} else {
					s.subagentSelection = items[0].Name
				}
			})
		},
		OpenSubagentConversation: func(_ ui.EventContext, conversationID string) {
			s.cancelPendingSubagentToolOpen()
			if s.subagentWatchCancel == nil {
				s.openSubagents()
			}
			s.openSubagentConversation(conversationID)
		},
		SelectWorkspacePane: func(_ ui.EventContext, descriptor workspacePaneDescriptor) {
			if owner := s.inputOwner(); owner.trapsFocus() && owner != inputTabs {
				return
			}
			if descriptor.Kind == workspacePaneSubagentConversation {
				s.openSubagentConversation(descriptor.ResourceID)
				return
			}
			s.SetState(func() {
				if identity, err := workspacePaneIdentityFor(descriptor); err == nil {
					s.workspace.Select(identity)
					s.syncWorkspaceSelection()
				}
			})
		},
		CloseWorkspacePane: func(_ ui.EventContext, descriptor workspacePaneDescriptor) {
			if owner := s.inputOwner(); owner.trapsFocus() && owner != inputTabs {
				return
			}
			if descriptor.Kind == workspacePaneSubagentConversation {
				s.closeSubagentConversation(descriptor.ResourceID)
			} else {
				s.SetState(func() {
					if identity, err := workspacePaneIdentityFor(descriptor); err == nil {
						s.workspace.Close(identity)
						s.syncWorkspaceSelection()
					}
				})
			}
			if s.workspacePickerOpen {
				s.SetState(func() {
					s.workspacePickerSelection = min(s.workspacePickerSelection, len(s.workspace.Panes()))
					s.requestWorkspacePickerReveal(s.workspacePickerSelection)
				})
			}
		},
		OpenWorkspaceFilePicker:  func(ui.EventContext) { s.openWorkspaceFilePicker() },
		CloseWorkspaceFilePicker: func(ui.EventContext) { s.SetState(func() { s.closeWorkspaceFilePicker() }) },
		WorkspaceFilePickerQuery: func(_ ui.EventContext, query string) {
			s.SetState(func() {
				s.workspaceFilePicker.Query = query
				s.workspaceFilePicker.ensureSelection(s.indexedFiles)
				s.requestWorkspaceFilePickerReveal()
			})
		},
		MoveWorkspaceFilePicker: func(_ ui.EventContext, delta int) {
			s.SetState(func() {
				s.workspaceFilePicker.move(s.indexedFiles, delta)
				s.requestWorkspaceFilePickerReveal()
			})
		},
		ActivateWorkspaceFilePicker: func(_ ui.EventContext, row workspaceFilePickerRow) { s.activateWorkspaceFilePickerRow(row) },
		SelectWorkspaceFilePicker: func(_ ui.EventContext, row workspaceFilePickerRow) {
			if workspaceFilePickerRowSelectable(row) {
				s.SetState(func() { s.workspaceFilePicker.Selection = row.Key })
			}
		},
		RefreshWorkspaceFilePicker: func(ui.EventContext) { s.refreshWorkspaceFilePicker() },
		OpenWorkspacePicker:        func(ui.EventContext) { s.openWorkspacePicker() },
		CloseWorkspacePicker: func(ui.EventContext) {
			s.SetState(func() {
				s.workspacePickerOpen = false
				s.workspacePickerQuery = ""
				s.workspacePickerSelection = 0
				s.workspacePickerRevealPending = false
			})
		},
		WorkspacePickerQuery: func(_ ui.EventContext, query string) {
			s.SetState(func() {
				s.workspacePickerQuery = query
				s.workspacePickerSelection = 0
				s.requestWorkspacePickerReveal(0)
			})
		},
		WorkspacePickerSelection: func(_ ui.EventContext, selection int) {
			s.SetState(func() {
				s.workspacePickerSelection = max(0, selection)
				s.requestWorkspacePickerReveal(s.workspacePickerSelection)
			})
		},
		MoveWorkspaceFocus: func(ui.EventContext) {
			if s.inputOwner().trapsFocus() {
				return
			}
			s.SetState(func() { s.inputControl = nil; s.workspace.MoveFocus() })
		},
		FocusWorkspaceComposer: func(ui.EventContext) {
			if s.inputOwner().trapsFocus() {
				return
			}
			s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusComposer) })
		},
		FocusWorkspaceContent: func(ui.EventContext) {
			if s.inputOwner().trapsFocus() {
				return
			}
			s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusContent) })
		},
		MoveWorkspaceSelection: func(_ ui.EventContext, delta int) {
			if s.inputOwner().trapsFocus() {
				return
			}
			selectedConversationID := ""
			s.SetState(func() {
				if s.workspace.MoveSelection(delta) {
					syncPane, paneSelected := s.workspace.SelectedPane()
					if paneSelected && syncPane.Kind == workspacePaneSubagentConversation {
						selectedConversationID = syncPane.ResourceID
					}
					s.clearSubagentActivityForConversationChange(selectedConversationID)
					s.syncWorkspaceSelection()
				}
			})
			if selectedConversationID != "" {
				s.refreshSubagentTranscript(selectedConversationID)
			}
		},
		OpenSubagentActivity: func(_ ui.EventContext, conversationID, sourceID string) {
			s.subagentFocus(conversationID).RequestFocus()
			s.SetState(func() {
				presentation := s.activityPresentation(presentedMessages, conversationID)
				running := false
				for _, conversation := range s.subagentConversations {
					if conversation.ID == conversationID {
						running = conversation.State == "running"
						break
					}
				}
				opening := !activitySourceIsOpen(s.inlineActivityOpen, presentation, sourceID, running)
				for id := range s.inlineActivityOpen {
					s.inlineActivityOpen[id] = false
				}
				s.inlineActivityOpen[sourceID] = opening
				if !opening {
					s.activitySourceID = ""
					s.activityConversationID = ""
					s.activityCursor = activityToolKey{}
					return
				}
				s.inlineActivityOpen[sourceID] = true
				s.activitySourceID = sourceID
				s.activityConversationID = conversationID
				keys := activityToolKeys(presentation, sourceID)
				if len(keys) > 0 {
					s.activityCursor = keys[0]
				}
			})
		},
		OpenSubagentFromTool: func(_ ui.EventContext, agentName string) {
			s.activityFocus.RequestFocus()
			s.openSubagentFromTool(agentName)
		},
		CloseSubagentConversation: func(_ ui.EventContext, conversationID string) {
			s.closeSubagentConversation(conversationID)
		},
		ScrollActivity: func(_ ui.EventContext, pages int) {
			if s.activityScroll.Attached() {
				s.activityScroll.ScrollByPages(pages)
			}
		},
		SelectActivityTool: func(_ ui.EventContext, key activityToolKey) {
			s.SetState(func() { s.activityCursor = key })
		},
		ToggleBashOutput: func(_ ui.EventContext, executionID string) {
			s.SetState(func() { s.bashCollapsed[executionID] = !s.bashCollapsed[executionID] })
		},
		ToggleTranscriptAnnotations: func(_ ui.EventContext, messageID string) {
			s.SetState(func() { s.transcriptAnnotationsExpanded[messageID] = !s.transcriptAnnotationsExpanded[messageID] })
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
		SelectFileMention: func(ctx ui.EventContext, path string) {
			s.selectFileMention(ctx, path)
		},
		SelectSessionMention: s.selectSessionMention,
		CopyCode: func(ctx ui.EventContext) {
			if s.instructions.UserCode != "" {
				ctx.Copy(s.instructions.UserCode)
				s.showToast(toastInput{Title: "Device code copied", Variant: toastInfo})
			}
		},
		DismissToast: s.dismissToast,
		ComposerPasted: func(_ ui.EventContext, value string) {
			if s.phase != phaseReady {
				return
			}
			if paths, composer, ok := attachmentPathsForComposerChange(s.composer, value); ok {
				s.stageAttachments(paths, composer)
				return
			}
			metrics := s.scroll.Metrics()
			followTranscript := s.scroll.Attached() && metrics.ScrollOffset >= metrics.MaxScrollOffset
			s.SetState(func() {
				s.fileMention.Observe(s.composer, value, true)
				s.sessionMention.Observe(s.composer, value, true)
				if !s.sessionMention.Open {
					s.closeSessionMention()
				}
				s.composer = value
				s.composerDraftGeneration++
				if followTranscript {
					s.requestTranscriptScroll()
				}
			})
		},
		RespondInteraction: func(_ ui.EventContext, response protocol.InteractionResponse, complete func(error)) {
			interactionSession, ok := s.bound.(sessionclient.InteractionSession)
			if !ok {
				complete(fmt.Errorf("interaction session is unavailable"))
				return
			}
			runtime := s.Context().Runtime()
			go func() {
				requestContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := interactionSession.RespondInteraction(requestContext, response)
				runtime.Dispatch(func() {
					complete(err)
					if err != nil {
						s.showToast(toastInput{Title: "Could not submit response", Subtitle: err.Error(), Variant: toastError})
					}
				})
			}()
		},
		RemoveAttachment: func(_ ui.EventContext, index int) {
			s.removeComposerAttachment(index)
		},
		ActivateAnnotation: func(_ ui.EventContext, annotation protocol.AnnotationSummary) {
			s.activateAnnotation(annotation)
		},
		RemoveAnnotation: func(_ ui.EventContext, annotationID uint64) {
			s.removeAnnotation(annotationID)
		},
		CreateAnnotation: func(anchor protocol.AnnotationAnchor, body string, done func(error)) {
			s.createInlineAnnotation(anchor, body, done)
		},
		LoadAnnotation: func(annotationID uint64, done func(string, error)) func() {
			return s.loadInlineAnnotation(annotationID, done)
		},
		UpdateAnnotation: func(annotationID uint64, body string, done func(error)) {
			s.updateInlineAnnotation(annotationID, body, done)
		},
		OpenAnnotationPicker: func(ui.EventContext) {
			if !s.admitRootModal() {
				return
			}
			s.SetState(func() { s.annotationPicker.Begin() })
		},
		RestoreFollowUps: func(ctx ui.EventContext) {
			s.restoreFollowUps(ctx)
		},
		ComposerChanged: func(ctx ui.EventContext, value string) {
			if s.phase != phaseReady {
				return
			}
			// Some terminals deliver bracketed paste text as a normal bulk field
			// change. Retain attachment detection as a fallback instead of relying
			// exclusively on the paste event marker.
			if paths, composer, ok := attachmentPathsForComposerChange(s.composer, value); ok {
				s.stageAttachments(paths, composer)
				return
			}
			metrics := s.scroll.Metrics()
			followTranscript := s.scroll.Attached() && metrics.ScrollOffset >= metrics.MaxScrollOffset
			openedFileMention := false
			openedSessionMention := false
			s.SetState(func() {
				composer, intercepted := s.palette.HandleComposerChange(s.composer, value, s.hasActiveWork())
				if intercepted {
					return
				}
				openedFileMention = s.fileMention.Observe(s.composer, composer, false)
				openedSessionMention = s.sessionMention.Observe(s.composer, composer, false)
				if !s.sessionMention.Open {
					s.closeSessionMention()
				}
				if openedFileMention {
					s.closeSessionMention()
				}
				if openedSessionMention {
					s.fileMention.Close()
				}
				s.sessionMention.ensureSelection(s.sessionMentions.Entries)
				s.fileMention.ensureSelection(s.indexedFiles.Entries)
				s.composer = composer
				if followTranscript {
					s.requestTranscriptScroll()
				}
			})
			if openedFileMention {
				s.loadFileMentions(ctx.Runtime())
			}
			if openedSessionMention {
				s.loadSessionMentions(ctx.Runtime())
			}
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
		SelectTheme: func(_ ui.EventContext, index int) {
			if index == s.themePicker.Selection && s.themePicker.previewValid && !s.themePicker.Loading {
				s.commitTheme()
				return
			}
			s.selectTheme(index)
		},
		OpenSessionRename: func(ui.EventContext) {
			s.openCurrentSessionRename()
		},
		OpenModel: func(ui.EventContext) {
			s.openConfigurationPicker(configurationPickerModel)
		},
		OpenThinking: func(ui.EventContext) {
			s.openConfigurationPicker(configurationPickerThinking)
		},
		ConfigurationQuery: func(_ ui.EventContext, value string) {
			s.SetState(func() {
				if s.configurationPicker.EditingContext {
					s.configurationPicker.EditValue = value
					s.configurationPicker.Error = ""
				} else {
					s.configurationPicker.SetQuery(value)
				}
			})
		},
		SelectConfiguration: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.configurationPicker.Select(value) })
		},
		ApplyConfiguration: func(ui.EventContext) {
			s.applyConfigurationSelection()
		},
		SessionQueryChanged: func(_ ui.EventContext, value string) { s.SetState(func() { s.sessionExplorer.SetQuery(value) }) },
		ToggleSessionTree: func(_ ui.EventContext, sessionID string) {
			s.SetState(func() { s.sessionExplorer.ToggleExpanded(sessionID) })
		},
		SelectSession: func(_ ui.EventContext, sessionID string) {
			s.SetState(func() { s.sessionExplorer.Select(sessionID) })
		},
		SessionRenameChanged: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.sessionRename.SetText(value) })
		},
		SubmitCurrentSessionRename: func(_ ui.EventContext, value string) {
			s.renameCurrentSession(value)
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
			ctx.Quit()
		},
		Dismiss: s.dismiss,
	}
	files, _ := s.bound.(sessionclient.WorkspaceFilesSession)
	diffs, _ := s.bound.(sessionclient.DiffSession)
	return shellView{Snapshot: snapshot, Callbacks: callbacks, WorkspaceFiles: files, Diff: diffs}
}

func (s *appState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase {
		return ui.EventIgnored
	}
	if s.transcriptVisible && !s.transcriptInitialLoading {
		switch input := event.(type) {
		case ui.Mouse:
			switch input.Button {
			case ui.MouseWheelUp, ui.MouseWheelDown, ui.MouseLeftButton:
				s.transcriptHistoryScrollInput = true
			}
		case ui.Key:
			switch input.Keycode {
			case ui.KeyUp, vaxis.KeyPgUp, ui.KeyHome:
				s.transcriptHistoryScrollInput = true
			}
		}
	}
	if mouse, ok := event.(ui.Mouse); ok && s.transcriptVisible && s.transcriptHistoryRestore != 0 {
		switch mouse.Button {
		case ui.MouseWheelUp, ui.MouseWheelDown, ui.MouseLeftButton:
			s.transcriptHistoryInputGeneration++
		}
	}
	s.reconcileInputOwner()
	if s.inputOwner().permitsRoot() {
		ctx.Invoke(captureControlFocusIntent{Report: func(control *controlFocusState) { s.inputControl = control }})
	}
	// Ignore stale pointer targets during ownership transitions. Background
	// scrolling remains available for a dock, but no old control may activate.
	if mouse, ok := event.(ui.Mouse); ok && s.inputOwner() == inputInteraction && mouse.Button == ui.MouseLeftButton {
		s.SetState(func() {})
	}
	if mouse, ok := event.(ui.Mouse); ok && s.inputToken() != s.renderedInput && s.inputOwner().trapsFocus() {
		if s.inputOwner().modal() || mouse.Button == ui.MouseLeftButton {
			return ui.EventHandled
		}
	}
	// A pane-local modal is geometrically bounded by its pane, so its barrier
	// cannot cover shell chrome or the composer. The logical owner still makes
	// those regions inert while preserving pointer interaction inside the picker.
	if mouse, ok := event.(ui.Mouse); ok && s.inputOwner() == inputPane && !s.targetOwnsInput(ctx) {
		if mouse.EventType == vaxis.EventRelease {
			s.workspaceMouse.Release()
		}
		return ui.EventHandled
	}
	// A dock leaves background selection and scrolling available, not lower
	// editor/button activation. Passive pane focus anchors are selection surfaces.
	if mouse, ok := event.(ui.Mouse); ok && mouse.Button == ui.MouseLeftButton && s.inputOwner() == inputInteraction && !s.targetOwnsInput(ctx) {
		if control := captureInputControl(ctx); control != nil && !control.Widget().(controlFocusScope).Passive {
			return ui.EventHandled
		}
	}
	if _, starting := event.(vaxis.PasteStartEvent); starting {
		s.pasteOwner = s.inputToken()
		s.pasteControl = captureInputControl(ctx)
	}
	if key, ok := event.(ui.Key); ok && key.EventType != vaxis.EventPaste && key.MatchString("Ctrl+c") {
		return s.handleCtrlC(ctx, key)
	}
	if result, consumed := s.paste.Observe(ctx, event, func(ctx ui.EventContext, key ui.Key) ui.EventResult {
		if s.pasteOwner != s.inputToken() || s.pasteControl != captureInputControl(ctx) {
			return ui.EventHandled
		}
		return s.deliverPaste(ctx, key)
	}); consumed {
		return result
	}
	key, ok := event.(ui.Key)
	if !ok {
		return ui.EventIgnored
	}
	if key.EventType == vaxis.EventPaste {
		return s.deliverPaste(ctx, key)
	}
	result := s.handleKey(ctx, key)
	if result == ui.EventIgnored && !key.MatchString("Super+c") && (s.inputToken() != s.renderedInput || !s.targetOwnsInput(ctx)) {
		return ui.EventHandled
	}
	return result
}

func (s *appState) handleKey(ctx ui.EventContext, key ui.Key) ui.EventResult {
	owner := s.inputOwner()
	if key.EventType != vaxis.EventPaste && key.MatchString("Ctrl+c") {
		return s.handleCtrlC(ctx, key)
	}
	if key.EventType != vaxis.EventPaste && key.MatchString("Escape") && owner.modal() && owner != inputAuth && owner != inputPane {
		if key.EventType != ui.EventRelease {
			s.dismiss(ctx)
		}
		return ui.EventHandled
	}
	if key.EventType == vaxis.EventPaste && !s.acceptsPaste(owner) {
		return ui.EventHandled
	}
	if owner.trapsFocus() && key.EventType != vaxis.EventPaste && (key.MatchString("Ctrl+p") || key.MatchString("Ctrl+]") || key.MatchString("Ctrl+[") || (key.MatchString("Ctrl+o") && owner != inputConfiguration)) {
		return ui.EventHandled
	}
	if owner == inputSubagentDismiss {
		if key.EventType == ui.EventRelease {
			return ui.EventHandled
		}
		if key.EventType != vaxis.EventPaste && key.MatchString("Enter") {
			s.dismissSubagent(s.subagentDismissID, s.subagentDismissGeneration)
			return ui.EventHandled
		}
		if key.MatchString("Escape") || key.MatchString("Ctrl+c") {
			return ui.EventIgnored
		}
		return ui.EventHandled
	}
	if owner == inputAnnotations {
		if key.EventType == ui.EventRelease {
			return ui.EventHandled
		}
		switch {
		case key.MatchString("Up"), key.MatchString("k"):
			s.SetState(func() { s.annotationPicker.Move(-1, len(s.annotations)) })
		case key.MatchString("Down"), key.MatchString("j"):
			s.SetState(func() { s.annotationPicker.Move(1, len(s.annotations)) })
		case key.MatchString("Enter"):
			if s.annotationPicker.Selection < len(s.annotations) {
				s.activateAnnotation(s.annotations[s.annotationPicker.Selection])
				s.SetState(func() { s.annotationPicker.Close() })
			}
		case key.MatchString("r"):
			if s.annotationPicker.Selection < len(s.annotations) && s.annotations[s.annotationPicker.Selection].Stale {
				s.reanchorAnnotation(s.annotations[s.annotationPicker.Selection])
			}
		case key.MatchString("Delete"), key.MatchString("Backspace"):
			if s.annotationPicker.Selection < len(s.annotations) {
				s.removeAnnotation(s.annotations[s.annotationPicker.Selection].ID)
			}
		case key.MatchString("Escape"), key.MatchString("Ctrl+c"):
			return ui.EventIgnored
		}
		return ui.EventHandled
	}
	if owner == inputRename {
		if key.EventType == ui.EventRelease {
			return ui.EventHandled
		}
		if key.EventType != vaxis.EventPaste && key.MatchString("Enter") {
			s.renameCurrentSession(s.sessionRename.Text)
			return ui.EventHandled
		}
		if key.MatchString("Escape") || key.MatchString("Ctrl+c") {
			return ui.EventIgnored
		}
		if s.sessionRename.Pending {
			return ui.EventHandled
		}
		handled := false
		s.SetState(func() { handled = s.sessionRename.HandleEditorKey(key) })
		if handled {
			return ui.EventHandled
		}
		return ui.EventIgnored
	}
	if owner == inputTheme {
		if key.EventType == ui.EventRelease || key.EventType == vaxis.EventPaste {
			return ui.EventHandled
		}
		count := len(s.themePicker.Names)
		switch {
		case key.MatchString("Escape") || key.MatchString("Ctrl+c"):
			return ui.EventIgnored
		case key.MatchString("Up") && count > 0:
			s.selectTheme((s.themePicker.Selection - 1 + count) % count)
		case key.MatchString("Down") && count > 0:
			s.selectTheme((s.themePicker.Selection + 1) % count)
		case key.MatchString("Enter"):
			s.commitTheme()
		}
		return ui.EventHandled
	}
	if owner == inputConfiguration {
		if key.EventType != ui.EventRelease && key.EventType != vaxis.EventPaste && key.MatchString("Ctrl+o") {
			s.SetState(func() { s.configurationPicker.BeginContextEdit() })
			return ui.EventHandled
		}
		if s.configurationPicker.EditingContext && key.EventType != ui.EventRelease && key.EventType != vaxis.EventPaste && key.MatchString("Enter") {
			s.saveModelContextWindow()
			return ui.EventHandled
		}
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
	if owner.root() == inputSessions {
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
		s.SetState(func() {
			handled = s.sessionExplorer.HandleKey(key)
			if !handled {
				handled = s.sessionExplorer.HandleEditorKey(key)
			}
		})
		if handled {
			return ui.EventHandled
		}
	}
	if owner == inputSessionMention {
		var entry protocol.SessionInfo
		var selectEntry, handled bool
		s.SetState(func() { entry, selectEntry, handled = s.sessionMention.HandleKey(s.sessionMentions.Entries, key) })
		if handled {
			if selectEntry {
				s.selectSessionMention(ctx, entry.ID)
			}
			return ui.EventHandled
		}
	}
	if owner == inputFileMention {
		var entry protocol.FileIndexEntry
		var selectEntry, handled bool
		s.SetState(func() { entry, selectEntry, handled = s.fileMention.HandleKey(s.indexedFiles.Entries, key) })
		if handled {
			if selectEntry {
				s.selectFileMention(ctx, entry.Path)
			}
			return ui.EventHandled
		}
	}
	if owner == inputBashHistory {
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
	if owner != inputPalette {
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
		if err == nil {
			location = resolveSessionLocation(s.ctx, snapshot.Session.CWD, options.Location, options.ResolveLocation)
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
				s.locationBase = location
				s.vcsStatus = nil
				s.applySnapshot(snapshot)
				if running {
					s.status = "esc abort · ctrl+c detach"
				}
			})
			for _, warning := range snapshot.Warnings {
				s.showToast(toastInput{Title: "Configuration adjusted", Subtitle: warning, Variant: toastWarning})
			}
			s.startVCSMonitoring()
			s.watchAttachedSession(bound, operation)
			if running {
				s.watchSession(bound, operation, snapshot.ActiveRunID)
			}
			if activeBashID != "" {
				s.resumeBash(bound, operation, activeBashID)
			}
		})
	}()
}

func resolveSessionLocation(ctx context.Context, cwd, fallback string, resolve func(context.Context, string) string) string {
	if cwd == "" {
		return fallback
	}
	if resolve != nil {
		return resolve(ctx, cwd)
	}
	return cwd
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

func (s *appState) snapshotMetadataStale(snapshot protocol.SessionSnapshot) bool {
	return s.metadataStreamID != "" && (snapshot.EventStreamID != s.metadataStreamID || snapshot.EventCursor < s.metadataSequence)
}

func (s *appState) applySessionMetadataSnapshot(snapshot protocol.SessionSnapshot) {
	staleMetadata := s.snapshotMetadataStale(snapshot)
	if !staleMetadata {
		s.reconcileWorkspaceIdentity(snapshot.Workspace)
	}
	if snapshot.Session.ID != "" {
		name, cwd := snapshot.Session.Name, snapshot.Session.CWD
		if staleMetadata {
			name, cwd = s.session.Name, s.session.CWD
		}
		s.session = snapshot.Session
		s.session.Name, s.session.CWD = name, cwd
	}
	s.palette.SetContributions(promptPaletteCommands(snapshot.PromptCommands), s.hasActiveWork())
	s.contextTokens = snapshot.ContextTokens
	s.contextWindow = snapshot.ContextWindow
	s.sessionUsage = snapshot.Usage
	s.followUps = snapshot.FollowUps
	if !staleMetadata {
		s.annotations = append([]protocol.AnnotationSummary(nil), snapshot.Annotations...)
	}
	if s.liveSequence == 0 || (snapshot.EventStreamID == s.metadataStreamID && snapshot.EventCursor >= s.liveSequence) {
		s.pendingInteractions = append([]protocol.InteractionRequest(nil), snapshot.PendingInteractions...)
		s.reconcileInputOwner()
		s.agentFeedbackPending = len(s.pendingInteractions) > 0
	}
	s.applySubagentSnapshot(snapshot)
}

func (s *appState) applySubagentSnapshot(snapshot protocol.SessionSnapshot) {
	s.subagentDefinitions = append([]protocol.SubagentDefinition(nil), snapshot.SubagentDefinitions...)
	s.applySubagentDiagnostics(snapshot.Session.ID, snapshot.SubagentDiagnostics)
	s.subagentConversations = append([]protocol.SubagentConversation(nil), snapshot.SubagentConversations...)
}

func (s *appState) applySubagentDiagnostics(sessionID string, diagnostics []protocol.SubagentDiagnostic) {
	s.subagentDiagnostics = append([]protocol.SubagentDiagnostic(nil), diagnostics...)
	if sessionID == "" {
		sessionID = s.session.ID
	}
	if s.subagentDiagnosticToasts == nil {
		s.subagentDiagnosticToasts = make(map[subagentDiagnosticToastKey]struct{})
	}
	for _, diagnostic := range diagnostics {
		key := subagentDiagnosticToastKey{
			SessionID: sessionID, Severity: diagnostic.Severity, Code: diagnostic.Code, Message: diagnostic.Message,
			Kind: diagnostic.Source.Kind, Path: diagnostic.Source.Path, PluginID: diagnostic.Source.PluginID,
		}
		if _, shown := s.subagentDiagnosticToasts[key]; shown {
			continue
		}
		s.subagentDiagnosticToasts[key] = struct{}{}
		subtitle := diagnostic.Message
		if diagnostic.Source.Path != "" {
			subtitle += " " + glyphMiddleDot + " " + diagnostic.Source.Path
		}
		s.storeToast(toastInput{
			Title: "Subagent definition warning", Subtitle: subtitle,
			Variant: toastWarning, Persistent: true,
		})
	}
}

func (s *appState) applySessionMetadataBaseline(snapshot protocol.SessionSnapshot) {
	if snapshot.Session.ID == "" || (s.metadataStreamID != "" && snapshot.EventStreamID == s.metadataStreamID && snapshot.EventCursor < s.metadataSequence) {
		return
	}
	s.reconcileWorkspaceIdentity(snapshot.Workspace)
	s.session.Name = snapshot.Session.Name
	s.session.CWD = snapshot.Session.CWD
	s.metadataStreamID = snapshot.EventStreamID
	s.metadataSequence = snapshot.EventCursor
	s.sessionExplorer.ApplyExternalRename(snapshot.Session.ID, snapshot.Session.Name)
}

func (s *appState) applySnapshot(snapshot protocol.SessionSnapshot) {
	if s.liveSequence > 0 && snapshot.EventStreamID == s.liveStreamID && snapshot.EventCursor < s.liveSequence {
		return
	}
	staleMetadata := s.snapshotMetadataStale(snapshot)
	if !staleMetadata {
		s.reconcileWorkspaceIdentity(snapshot.Workspace)
	}
	var previousActivitySource transcriptDisplayItem
	if s.activitySourceID != "" && s.activityConversationID == "" {
		messages := make([]transcriptMessage, 0, len(s.messages)+len(s.liveMessages))
		messages = append(messages, s.messages...)
		messages = append(messages, s.liveMessages...)
		previousActivitySource, _ = transcriptActivitySource(presentTranscript(messages).Items, s.activitySourceID)
	}
	s.palette.SetContributions(promptPaletteCommands(snapshot.PromptCommands), s.hasActiveWork())
	s.applySubagentSnapshot(snapshot)
	if snapshot.Session.ID != "" {
		name, cwd := snapshot.Session.Name, snapshot.Session.CWD
		if staleMetadata {
			name, cwd = s.session.Name, s.session.CWD
		}
		s.session = snapshot.Session
		s.session.Name, s.session.CWD = name, cwd
	}
	s.followUps = snapshot.FollowUps
	if !staleMetadata {
		s.annotations = append([]protocol.AnnotationSummary(nil), snapshot.Annotations...)
	}
	s.pendingInteractions = append([]protocol.InteractionRequest(nil), snapshot.PendingInteractions...)
	s.reconcileInputOwner()
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
	preservedHistory := s.mergeSnapshotTranscript(projected)
	if !s.transcriptHistoryInitialized || !preservedHistory {
		s.resetTranscriptHistoryFromSnapshot(snapshot)
	}
	s.resetLiveRun()
	if s.activitySourceID != "" && s.activityConversationID == "" {
		presentation := presentTranscript(s.messages)
		source, ok := transcriptActivitySource(presentation.Items, s.activitySourceID)
		if !ok {
			previousSourceID := s.activitySourceID
			source, ok = equivalentActivitySource(presentation.Items, previousActivitySource)
			if ok {
				s.activitySourceID = source.ID
				if s.inlineActivityOpen[previousSourceID] {
					delete(s.inlineActivityOpen, previousSourceID)
					s.inlineActivityOpen[source.ID] = true
				}
			}
		}
		if ok {
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
				keys := activityToolKeys(presentation, s.activitySourceID)
				if len(keys) > 0 {
					s.activityCursor = keys[0]
				}
			}
		} else {
			s.activitySourceID = ""
			s.activityConversationID = ""
			s.syncWorkspaceSelection()
			s.inlineActivityOpen = make(map[string]bool)
			s.activityExpanded = make(map[activityToolKey]bool)
			s.activityCursor = activityToolKey{}
		}
	}
	s.followTranscriptIfPinned()
	s.contextTokens = snapshot.ContextTokens
	s.contextWindow = snapshot.ContextWindow
	s.sessionUsage = snapshot.Usage
	s.activeRunID = snapshot.ActiveRunID
	s.runPending = snapshot.ActiveRunID != ""
	s.providerRetry = cloneProviderRetry(snapshot.ProviderRetry)
	s.activeCompactionID = ""
	if snapshot.ActiveCompaction != nil && snapshot.ActiveCompaction.RunID == snapshot.ActiveRunID {
		s.activeCompactionID = snapshot.ActiveCompaction.ID
		s.setTurnThinking("")
		s.setTurnActivity("Compacting session…")
	}
	if snapshot.ActiveRunID != "" && snapshot.ActiveRunID != s.terminalSettledRunID {
		s.markTerminalRunStarted(snapshot.ActiveRunID)
	}
	if snapshot.ActiveBashExecutionID != "" || s.activeBash == nil {
		s.activeBashID = snapshot.ActiveBashExecutionID
	} else if execution, found := findBashExecution(s.messages, nil, s.activeBashID); found && execution.Status != protocol.BashExecutionRunning {
		s.activeBash = nil
		s.activeBashID = ""
	}
	s.agentFeedbackPending = len(s.pendingInteractions) > 0
	if s.runPending {
		if s.agentFeedbackPending {
			s.turnActivity = "Waiting for feedback…"
		} else if s.activeCompactionID != "" {
			s.turnActivity = "Compacting session…"
		} else {
			s.turnActivity = "Working…"
		}
	}
	if !s.runPending {
		s.activeRun = nil
		s.prompt = nil
		s.status = ""
	}
}

func (s *appState) resetTranscriptHistoryFromSnapshot(snapshot protocol.SessionSnapshot) {
	if !s.transcriptInitialPositioned {
		s.transcriptInitialLoading = len(s.mainTranscriptPresentation().Items) > 0
		s.transcriptInitialPositioned = !s.transcriptInitialLoading
		s.transcriptInitialStable = false
		s.transcriptHistoryUserScroll = false
	}

	_, paginationAvailable := s.bound.(sessionclient.TranscriptPager)
	s.transcriptHistoryInitialized = paginationAvailable
	s.transcriptHistoryCursor = snapshot.PreviousMessageCursor
	s.transcriptHistoryHasMore = paginationAvailable && snapshot.HasMoreMessages
	s.transcriptHistoryLoading = false
	s.transcriptHistoryError = ""
	s.transcriptHistoryGeneration++
	s.transcriptHistoryAnchorID = ""
	s.transcriptHistoryRestore = 0
}

func (s *appState) mergeSnapshotTranscript(projected []transcriptMessage) bool {
	if len(projected) == 0 || len(s.messages) == 0 || projected[0].Sequence <= 0 {
		s.messages = projected
		return false
	}
	existingByID := make(map[string]int64, len(s.messages))
	existingBySequence := make(map[int64]string, len(s.messages))
	for _, message := range s.messages {
		if message.ID != "" {
			existingByID[message.ID] = message.Sequence
		}
		if message.Sequence > 0 {
			existingBySequence[message.Sequence] = message.ID
		}
	}
	overlaps := false
	for _, message := range projected {
		if sequence, found := existingByID[message.ID]; found {
			if sequence != message.Sequence {
				s.messages = projected
				return false
			}
			overlaps = true
		}
		if id, found := existingBySequence[message.Sequence]; found && id != message.ID {
			s.messages = projected
			return false
		}
	}
	if !overlaps {
		s.messages = projected
		return false
	}
	firstSequence := projected[0].Sequence
	prefix := make([]transcriptMessage, 0, len(s.messages)+len(projected))
	for _, message := range s.messages {
		if message.Sequence > 0 && message.Sequence < firstSequence {
			prefix = append(prefix, message)
		}
	}
	s.messages = append(prefix, projected...)
	return true
}

func projectTranscriptContent(content []protocol.TranscriptContent) []protocol.TranscriptContent {
	projected := make([]protocol.TranscriptContent, 0, len(content))
	for _, block := range content {
		switch block.Kind {
		case protocol.TranscriptContentFile:
			block = protocol.TranscriptContent{Kind: protocol.TranscriptContentText, Text: "[file: " + block.Filename + "]"}
		}
		projected = append(projected, block)
	}
	return projected
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
				ID: message.ID, Sequence: message.Sequence, Role: "bash", Bash: &execution,
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
				if block.AttachmentID == "" {
					textParts = append(textParts, "[image]")
				}
			case protocol.TranscriptContentFile:
				textParts = append(textParts, "[file: "+block.Filename+"]")
			}
		}
		text := strings.Join(textParts, "\n")
		thinking := strings.Join(thinkingParts, "\n")
		if text == "" && message.ErrorMessage != "" {
			text = message.ErrorMessage
		}
		if strings.TrimSpace(text) == "" && strings.TrimSpace(thinking) == "" && message.ToolName == "" && len(calls) == 0 && len(message.Content) == 0 {
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
			ID: message.ID, Sequence: message.Sequence, TurnID: message.TurnID, Role: message.Role,
			Text: text, Thinking: thinking, Content: projectTranscriptContent(message.Content),
			ToolCallID: message.ToolCallID, ToolCalls: calls,
			ToolName: message.ToolName, ToolStatus: status,
			ToolContent: append([]protocol.TranscriptContent(nil), message.Content...),
			ToolDetails: append(json.RawMessage(nil), message.Details...),
			StopReason:  message.StopReason, ErrorMessage: message.ErrorMessage, IsError: message.IsError,
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
	s.liveStreamID = ""
	s.turnActivity = ""
	s.turnThinking = ""
	s.runStopping = false
	s.providerRetry = nil
	s.activeCompactionID = ""
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

func cloneProviderRetry(retry *protocol.ProviderRetry) *protocol.ProviderRetry {
	if retry == nil {
		return nil
	}
	copy := *retry
	return &copy
}

func providerRetryActivity(retry *protocol.ProviderRetry, now time.Time) string {
	if retry == nil {
		return ""
	}
	deadline, err := time.Parse(time.RFC3339Nano, retry.RetryAt)
	if err != nil {
		return ""
	}
	remaining := deadline.Sub(now)
	seconds := int64(0)
	if remaining > 0 {
		seconds = int64((remaining + time.Second - 1) / time.Second)
	}
	return fmt.Sprintf("Retry %d in %ds…", retry.Count, seconds)
}

func (s *appState) recordCompactionOutcome(id string) bool {
	if id == "" {
		return false
	}
	if s.compactionOutcomeIDs == nil {
		s.compactionOutcomeIDs = make(map[string]struct{})
	}
	if _, duplicate := s.compactionOutcomeIDs[id]; duplicate {
		return false
	}
	const limit = 16
	if len(s.compactionOutcomeOrder) == limit {
		delete(s.compactionOutcomeIDs, s.compactionOutcomeOrder[0])
		s.compactionOutcomeOrder = s.compactionOutcomeOrder[1:]
	}
	s.compactionOutcomeIDs[id] = struct{}{}
	s.compactionOutcomeOrder = append(s.compactionOutcomeOrder, id)
	return true
}

func (s *appState) presentedTurnActivity(now time.Time) string {
	if !s.runStopping {
		if activity := providerRetryActivity(s.providerRetry, now); activity != "" {
			return activity
		}
	}
	return s.turnActivity
}

type cwdToolDetails struct {
	CWD     string `json:"cwd"`
	Changed bool   `json:"changed"`
}

type subagentToolDetails struct {
	Warning string `json:"warning"`
}

func (s *appState) applyRunEvents(events []protocol.SessionEvent) string {
	transcriptChanged := false
	changedCWD := ""
	for _, event := range events {
		if event.Sequence <= s.liveSequence {
			continue
		}
		s.liveSequence = event.Sequence
		if event.StreamID != "" {
			s.liveStreamID = event.StreamID
		}
		s.applyAnnotationEvent(event)
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
						s.invalidateFileMentions()
						s.session.CWD = details.CWD
						s.locationBase = details.CWD
						s.location = details.CWD
						s.vcsStatus = nil
						changedCWD = details.CWD
					}
				}
				if event.ToolName == "subagent" && !event.IsError && !event.DetailsOmitted {
					var details subagentToolDetails
					if json.Unmarshal(event.Details, &details) == nil && strings.TrimSpace(details.Warning) != "" {
						s.eventToasts = append(s.eventToasts, toastInput{
							Title: "Subagent provider unavailable", Subtitle: details.Warning, Variant: toastWarning,
						})
					}
				}
				if event.IsError {
					s.liveMessages[index].ToolStatus = "Failed"
				} else {
					s.liveMessages[index].ToolStatus = "Completed"
				}
			}
		case protocol.SessionEventInteractionRequested:
			if event.Interaction != nil {
				found := false
				for _, pending := range s.pendingInteractions {
					if pending.ID == event.Interaction.ID {
						found = true
						break
					}
				}
				if !found {
					s.pendingInteractions = append(s.pendingInteractions, *event.Interaction)
				}
				s.agentFeedbackPending = true
				s.reconcileInputOwner()
				s.setTurnActivity("Waiting for feedback…")
			}
		case protocol.SessionEventInteractionResolved:
			for index := range s.pendingInteractions {
				if s.pendingInteractions[index].ID == event.InteractionID {
					s.pendingInteractions = append(s.pendingInteractions[:index], s.pendingInteractions[index+1:]...)
					break
				}
			}
			s.agentFeedbackPending = len(s.pendingInteractions) > 0
			if s.agentFeedbackPending {
				s.setTurnActivity("Waiting for feedback…")
			} else {
				s.setTurnActivity("Working…")
			}
		case protocol.SessionEventProviderRetryScheduled:
			s.providerRetry = cloneProviderRetry(event.ProviderRetry)
			s.setTurnThinking("")
		case protocol.SessionEventProviderRetryStarted:
			s.providerRetry = nil
			s.setTurnActivity("Working…")
		case protocol.SessionEventCompactionStarted:
			s.activeCompactionID = event.CompactionID
			s.setTurnThinking("")
			s.setTurnActivity("Compacting session…")
		case protocol.SessionEventCompactionCompleted:
			current := s.activeCompactionID == event.CompactionID
			stale := s.activeCompactionID != "" && !current
			if current {
				s.activeCompactionID = ""
				s.setTurnActivity("Working…")
			}
			if !stale && s.recordCompactionOutcome(event.CompactionID) {
				s.showToast(toastInput{Title: "Session compacted", Subtitle: "Session context was compacted.", Variant: toastInfo})
			}
		case protocol.SessionEventCompactionFailed:
			current := s.activeCompactionID == event.CompactionID
			stale := s.activeCompactionID != "" && !current
			if current {
				s.activeCompactionID = ""
				s.setTurnActivity("Working…")
			}
			if !stale && s.recordCompactionOutcome(event.CompactionID) {
				s.showToast(toastInput{Title: "Auto-compaction failed", Subtitle: event.ErrorMessage, Variant: toastError})
			}
		case protocol.SessionEventContextUpdated:
			s.contextTokens = event.ContextTokens
			s.contextWindow = event.ContextWindow
		case protocol.SessionEventUsageUpdated:
			if event.Usage != nil && !sessionUsageDecreased(s.sessionUsage, *event.Usage) {
				s.sessionUsage = *event.Usage
			}
		case protocol.SessionEventRunFinished:
			transcriptChanged = true
			if s.activityConversationID == "" {
				for id := range s.inlineActivityOpen {
					delete(s.inlineActivityOpen, id)
				}
				s.activitySourceID = ""
				s.activityCursor = activityToolKey{}
			}
			s.markTerminalRunSettled(event.RunID)
			for _, index := range s.liveTools {
				if index >= 0 && index < len(s.liveMessages) && s.liveMessages[index].ToolStatus == "Planned" {
					s.liveMessages[index].Pending = false
					s.liveMessages[index].ToolStatus = "Not run"
				}
			}
			s.runStopping = false
			s.providerRetry = nil
			s.activeCompactionID = ""
			s.setTurnThinking("")
			s.setTurnActivity("")
		}
	}
	if len(events) > 0 {
		if transcriptChanged {
			s.followTranscriptIfPinned()
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
	}
	// Resolve existing call identity across the whole turn before choosing an
	// assistant owner. Execution events do not carry the planning message ID.
	for index := len(s.liveMessages) - 1; index >= 0; index-- {
		message := &s.liveMessages[index]
		if message.Role != "assistant" || message.TurnID != event.TurnID {
			continue
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
	s.followTranscriptIfPinned()
}

func (s *appState) applySessionMetadataEvents(events []protocol.SessionEvent) (changedCWD string) {
	for _, event := range events {
		if !sessionMetadataEvent(event.Kind) || event.SessionID != s.session.ID {
			continue
		}
		if (s.metadataStreamID != "" && event.StreamID != s.metadataStreamID) ||
			(event.StreamID == s.metadataStreamID && event.Sequence <= s.metadataSequence) {
			continue
		}
		s.metadataStreamID = event.StreamID
		s.metadataSequence = event.Sequence
		s.applyAnnotationEvent(event)
		switch event.Kind {
		case protocol.SessionEventSessionRenamed:
			s.session.Name = event.SessionName
			s.sessionExplorer.ApplyExternalRename(event.SessionID, event.SessionName)
		case protocol.SessionEventSessionCWDChanged:
			if event.Workspace == nil {
				continue
			}
			s.invalidateFileMentions()
			s.session.CWD = event.Workspace.CWD
			s.location = event.Workspace.CWD
			s.locationBase = event.Workspace.CWD
			s.vcsStatus = nil
			s.reconcileWorkspaceIdentity(event.Workspace)
			changedCWD = event.Workspace.CWD
		}
	}
	return changedCWD
}

func sessionMetadataEvent(kind protocol.SessionEventKind) bool {
	switch kind {
	case protocol.SessionEventSessionRenamed, protocol.SessionEventSessionCWDChanged,
		protocol.SessionEventAnnotationCreated, protocol.SessionEventAnnotationUpdated,
		protocol.SessionEventAnnotationDeleted, protocol.SessionEventAnnotationSubmitted:
		return true
	default:
		return false
	}
}

func attachedRunLifecycle(events []protocol.SessionEvent) (startedRunID, finishedRunID string, status protocol.RunStatus) {
	for _, event := range events {
		switch event.Kind {
		case protocol.SessionEventRunStarted:
			startedRunID = event.RunID
		case protocol.SessionEventRunFinished:
			finishedRunID = event.RunID
			status = event.Status
		}
	}
	return startedRunID, finishedRunID, status
}

func shouldApplyAttachedSnapshot(runPending bool, activeRunID, snapshotRunID string) bool {
	if runPending && activeRunID == "" && snapshotRunID == "" {
		return false
	}
	return snapshotRunID == "" || snapshotRunID != activeRunID
}

func (s *appState) watchAttachedSession(bound sessionclient.Session, operation uint64) {
	watcher, ok := bound.(sessionclient.SessionEventWatcher)
	if !ok {
		return
	}
	if s.sessionWatchCancel != nil {
		s.sessionWatchCancel()
	}
	watchContext, cancel := context.WithCancel(s.attachmentCtx)
	s.sessionWatchCancel = cancel
	runtime := s.Context().Runtime()
	applySnapshot := func(snapshot protocol.SessionSnapshot, finishedRunID string, status protocol.RunStatus) {
		runtime.Dispatch(func() {
			if operation != s.operation || s.bound != bound {
				return
			}
			if !shouldApplyAttachedSnapshot(s.runPending, s.activeRunID, snapshot.ActiveRunID) {
				copy := snapshot
				s.SetState(func() {
					s.applySessionMetadataBaseline(snapshot)
					s.deferredSessionSnapshot = &copy
				})
				return
			}
			s.deferredSessionSnapshot = nil
			alreadySettled := finishedRunID != "" && finishedRunID == s.terminalSettledRunID
			nextRunID := snapshot.ActiveRunID
			s.SetState(func() {
				s.applySessionMetadataBaseline(snapshot)
				s.applySnapshot(snapshot)
				if nextRunID != "" {
					s.activeRun = nil
					s.prompt = nil
					s.status = "esc abort · ctrl+c detach"
				} else {
					s.activeRun = nil
					s.prompt = nil
					if finishedRunID != "" {
						s.markTerminalRunSettled(finishedRunID)
					}
				}
			})
			if nextRunID != "" && s.runWatchID != nextRunID {
				s.watchSession(bound, operation, nextRunID)
			} else if finishedRunID != "" && !alreadySettled {
				s.notifyTurnSettledOnce(finishedRunID, status)
			}
		})
	}
	reconcile := func(finishedRunID string, status protocol.RunStatus) bool {
		delay := 50 * time.Millisecond
		for watchContext.Err() == nil {
			snapshot, err := bound.Snapshot(watchContext)
			if err == nil {
				applySnapshot(snapshot, finishedRunID, status)
				return true
			}
			timer := time.NewTimer(delay)
			select {
			case <-watchContext.Done():
				timer.Stop()
				return false
			case <-timer.C:
				if delay < 2*time.Second {
					delay *= 2
				}
			}
		}
		return false
	}
	go func() {
		for watchContext.Err() == nil {
			baseline, stream, err := watcher.Watch(watchContext)
			if err == nil {
				applySnapshot(baseline, "", "")
				for updates := range stream.Updates() {
					hasMetadata := false
					for _, event := range updates {
						hasMetadata = hasMetadata || sessionMetadataEvent(event.Kind)
					}
					if hasMetadata {
						copy := append([]protocol.SessionEvent(nil), updates...)
						runtime.Dispatch(func() {
							if operation == s.operation && s.bound == bound {
								changedCWD := ""
								s.SetState(func() { changedCWD = s.applySessionMetadataEvents(copy) })
								if changedCWD != "" {
									s.refreshLocation(changedCWD)
									s.startVCSMonitoring()
									s.refreshFileIndex(runtime)
								}
							}
						})
					}
					startedRunID, finishedRunID, status := attachedRunLifecycle(updates)
					if startedRunID == "" && finishedRunID == "" {
						continue
					}
					if !reconcile(finishedRunID, status) {
						return
					}
				}
			}
			select {
			case <-watchContext.Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}()
}

func (s *appState) watchSession(bound sessionclient.Session, operation uint64, runID string) {
	if s.runWatchCancel != nil && s.runWatchID == runID {
		return
	}
	runtime := s.Context().Runtime()
	attachmentCtx := s.attachmentCtx
	if attachmentCtx == nil {
		attachmentCtx = s.ctx
	}
	if s.runWatchCancel != nil {
		s.runWatchCancel()
	}
	s.runWatchGeneration++
	watchGeneration := s.runWatchGeneration
	watchCtx, cancel := context.WithCancel(attachmentCtx)
	s.runWatchCancel = cancel
	s.runWatchID = runID
	attachmentCtx = watchCtx
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var stream sessionclient.EventStream
		var streamCancel context.CancelFunc
		var updates <-chan []protocol.SessionEvent
		streamFailures := 0
		retryAt := time.Time{}
		connect := func() {
			if streamCancel != nil {
				streamCancel()
			}
			streamContext, cancel := context.WithCancel(attachmentCtx)
			connected, err := bound.Stream(streamContext, runID)
			if err != nil {
				cancel()
				streamCancel = nil
				streamFailures++
				exponent := min(streamFailures-1, 4)
				retryAt = time.Now().Add(100 * time.Millisecond * time.Duration(1<<exponent))
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
						streamFailures++
						exponent := min(streamFailures-1, 4)
						retryAt = time.Now().Add(100 * time.Millisecond * time.Duration(1<<exponent))
						snapshot, snapshotErr := bound.Snapshot(attachmentCtx)
						if snapshotErr == nil && snapshot.ActiveRunID == runID {
							if !snapshot.EventReplayAvailable {
								runtime.Dispatch(func() {
									if operation == s.operation && watchGeneration == s.runWatchGeneration {
										s.SetState(func() { s.applySnapshot(snapshot) })
									}
								})
							}
							continue
						}
						runtime.Dispatch(func() {
							if operation == s.operation && watchGeneration == s.runWatchGeneration {
								s.SetState(func() { s.status = "Reconnecting activity… · esc abort · ctrl+c detach" })
							}
						})
					}
					continue
				}
				streamFailures = 0
				retryAt = time.Time{}
				batch := append([]protocol.SessionEvent(nil), events...)
				runtime.Dispatch(func() {
					if operation == s.operation && watchGeneration == s.runWatchGeneration {
						changedCWD := ""
						var eventToasts []toastInput
						s.SetState(func() {
							changedCWD = s.applyRunEvents(batch)
							eventToasts = append([]toastInput(nil), s.eventToasts...)
							s.eventToasts = nil
						})
						for _, toast := range eventToasts {
							s.showToast(toast)
						}
						if changedCWD != "" {
							s.showToast(cwdChangeToast(changedCWD))
							s.refreshLocation(changedCWD)
							s.startVCSMonitoring()
							s.refreshFileIndex(runtime)
						} else if vcsRefreshNeeded(batch) {
							s.refreshVCSStatus()
						}
					}
				})
			case <-ticker.C:
				// Run lookup is recovery after SSE closes, not a parallel polling loop.
				if updates != nil || time.Now().Before(retryAt) {
					continue
				}
				info, err := bound.Run(attachmentCtx, runID)
				if err != nil {
					streamFailures++
					exponent := min(streamFailures-1, 4)
					retryAt = time.Now().Add(100 * time.Millisecond * time.Duration(1<<exponent))
					if attachmentCtx.Err() == nil {
						runtime.Dispatch(func() {
							if operation == s.operation && watchGeneration == s.runWatchGeneration {
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
					if operation == s.operation && watchGeneration == s.runWatchGeneration {
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
							if operation == s.operation && watchGeneration == s.runWatchGeneration {
								s.SetState(func() { s.settleRunWithoutSnapshot(info, err) })
								s.notifyTurnSettledOnce(info.RunID, info.Status)
							}
						})
						return
					}
					runtime.Dispatch(func() {
						if operation == s.operation && watchGeneration == s.runWatchGeneration {
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
					if operation != s.operation || watchGeneration != s.runWatchGeneration {
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
							s.runWatchID = nextRunID
							s.status = "esc abort · ctrl+c detach"
						}
					})
					s.notifyTurnSettledOnce(info.RunID, info.Status)
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

func (s *appState) notifyTurnSettledOnce(runID string, status protocol.RunStatus) {
	if runID == "" {
		return
	}
	if s.notifiedRunIDs == nil {
		s.notifiedRunIDs = make(map[string]bool)
	}
	if s.notifiedRunIDs[runID] {
		return
	}
	s.notifiedRunIDs[runID] = true
	s.notifiedRunOrder = append(s.notifiedRunOrder, runID)
	if len(s.notifiedRunOrder) > 64 {
		oldest := s.notifiedRunOrder[0]
		s.notifiedRunOrder = s.notifiedRunOrder[1:]
		delete(s.notifiedRunIDs, oldest)
	}
	s.notifyTurnSettled(status)
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
	if provider.ID == anthropicOAuthOptionID {
		s.startAnthropicLogin(ctx)
		return
	}
	s.SetState(func() {
		s.phase = phaseAuthAPIKey
		s.authProviderID = provider.ProviderID
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

func (s *appState) startAnthropicLogin(_ ui.EventContext) {
	options := s.Widget().(app).Options
	if options.BrowserLogin == nil {
		s.SetState(func() { s.errorText = "Claude subscription login is unavailable" })
		return
	}
	s.cancelLogin()
	loginContext, cancel := context.WithCancel(s.ctx)
	s.loginCancel = cancel
	generation := s.loginGeneration
	runtime := s.Context().Runtime()
	manualCode := make(chan string, 1)
	s.SetState(func() {
		s.phase = phaseAuthBrowser
		s.errorText = ""
		s.status = "Starting Claude sign-in…"
		s.browserInstructions = auth.AnthropicLoginInstructions{}
		s.authCode = ""
		s.authCodeInput = manualCode
		s.authFilter = ""
		s.authProviderID = anthropicOAuthOptionID
		s.authPending = true
	})
	go func() {
		err := options.BrowserLogin.Login(loginContext, manualCode, func(instructions auth.AnthropicLoginInstructions) error {
			runtime.Dispatch(func() {
				if generation != s.loginGeneration {
					return
				}
				s.SetState(func() {
					s.browserInstructions = instructions
					s.status = "Waiting for browser approval…"
				})
			})
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
					s.authCodeInput = nil
					s.errorText = err.Error()
					s.status = ""
				})
				return
			}
			s.availableMu.Lock()
			s.available[auth.AnthropicProviderID] = true
			s.availableMu.Unlock()
			if s.authReturnReady {
				s.SetState(func() {
					s.phase = phaseReady
					s.authReturnReady = false
					s.authPending = false
					s.authCodeInput = nil
					s.status = "Connected to Claude"
				})
				return
			}
			s.SetState(func() {
				s.phase = phaseLoading
				s.authPending = false
				s.authCodeInput = nil
				s.status = "Connected to Claude"
			})
			s.startBootstrap(preferredStartupModel(options.DefaultModel, "anthropic/claude-sonnet-4-6"), options.DefaultThinking)
		})
	}()
}

func (s *appState) submitAuthCode(_ ui.EventContext, value string) {
	value = strings.TrimSpace(value)
	if value == "" || s.authCodeInput == nil {
		return
	}
	select {
	case s.authCodeInput <- value:
		s.SetState(func() { s.authCode = ""; s.status = "Completing Claude sign-in…" })
	default:
	}
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
	return s.runPending || s.reloadPending || s.cwdPending || s.compactPending || s.configurationPicker.Pending || s.sessionCreatePending || s.bashStarting || s.activeBashID != ""
}

func (s *appState) openPalette() {
	if !s.admitRootModal() {
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
	s.replacingPalette = true
	defer func() { s.replacingPalette = false }()
	s.inputGeneration++
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
	case paletteCommandName:
		s.openCurrentSessionRename()
		if args != "" {
			s.renameCurrentSession(args)
		}
	case paletteCommandNew:
		s.createNewSession()
	case paletteCommandQuit:
		ctx.Quit()
	case paletteCommandReload:
		s.reloadSession()
	case paletteCommandDebug:
		s.SetState(func() { s.sessionDetailsOpen = true })
	case paletteCommandDiff:
		s.openWorkingTreeDiff()
	case paletteCommandFork:
		s.forkCurrentSession(args)
	case paletteCommandFiles:
		s.openWorkspaceFilePicker()
	case paletteCommandSessions:
		s.openSessionExplorer()
	case paletteCommandSubagents:
		s.openSubagents()
	case paletteCommandTabs:
		s.openWorkspacePicker()
	case paletteCommandTheme:
		s.openThemePicker()
	case paletteCommandThinking:
		s.openConfigurationPicker(configurationPickerThinking)
	}
}

func (s *appState) openWorkingTreeDiff() {
	if _, ok := s.bound.(sessionclient.DiffSession); !ok {
		s.showToast(toastInput{Title: "Diff unavailable", Subtitle: "This session does not expose repository changes.", Variant: toastWarning})
		return
	}
	if s.workspaceID == "" {
		s.showToast(toastInput{Title: "Diff unavailable", Subtitle: "The session workspace is not ready.", Variant: toastWarning})
		return
	}
	var err error
	s.SetState(func() {
		_, _, err = s.workspace.Open(workingTreeDiffWorkspacePane(s.workspaceID))
		if err == nil {
			s.syncWorkspaceSelection()
		}
	})
	if err != nil {
		s.showToast(toastInput{Title: "Could not open diff", Subtitle: err.Error(), Variant: toastWarning})
	}
}

func (s *appState) openThemePicker() {
	if !s.admitRootModal() {
		return
	}
	service := s.Widget().(app).Options.ThemeService
	if service == nil {
		s.showToast(toastInput{Title: "Theme picker unavailable", Variant: toastWarning})
		return
	}
	s.themeGeneration++
	generation := s.themeGeneration
	s.SetState(func() {
		s.themePicker = themePickerController{Open: true, Loading: true, CommittedName: s.themeName,
			CommittedDefinition: s.themeDefinition, PreviewDefinition: s.themeDefinition, previewValid: true}
	})
	runtime := s.Context().Runtime()
	go func() {
		names, err := service.Discover()
		runtime.Dispatch(func() {
			if generation != s.themeGeneration || !s.themePicker.Open {
				return
			}
			s.SetState(func() {
				if err != nil {
					s.themePicker.Loading = false
					s.themePicker.Err = fmt.Errorf("discover themes: %w", err)
					return
				}
				s.themePicker.OpenNames(names, s.themeName, s.themeDefinition)
			})
		})
	}()
}

func (s *appState) selectTheme(index int) {
	service := s.Widget().(app).Options.ThemeService
	if service == nil || !s.themePicker.Open || s.themePicker.Pending || index < 0 || index >= len(s.themePicker.Names) {
		return
	}
	s.themeGeneration++
	generation := s.themeGeneration
	name := s.themePicker.Names[index]
	s.SetState(func() {
		s.themePicker.Selection = index
		s.themePicker.Loading = name != kittheme.SystemName
		s.themePicker.Err = nil
		s.themePicker.Diagnostics = nil
		s.themePicker.previewValid = name == kittheme.SystemName
		if name == kittheme.SystemName {
			s.themePicker.PreviewDefinition = kittheme.Definition{}
		}
	})
	if name == kittheme.SystemName {
		s.applyTheme(kittheme.Definition{})
		return
	}
	if s.themeLoadActive {
		return
	}
	s.themeLoadActive = true
	runtime := s.Context().Runtime()
	go func() {
		definition, diagnostics, err := service.Load(name)
		runtime.Dispatch(func() {
			s.themeLoadActive = false
			if generation != s.themeGeneration || !s.themePicker.Open || s.themePicker.Selection != index {
				if s.themePicker.Open && len(s.themePicker.Names) > 0 {
					s.selectTheme(s.themePicker.Selection)
				}
				return
			}
			s.SetState(func() {
				s.themePicker.Loading = false
				s.themePicker.Diagnostics = diagnostics
				s.themePicker.Err = err
				s.themePicker.previewValid = err == nil
				if err == nil {
					s.themePicker.PreviewDefinition = definition
				}
			})
			if err == nil {
				s.applyTheme(definition)
			}
		})
	}()
}

func (s *appState) commitTheme() {
	service := s.Widget().(app).Options.ThemeService
	if service == nil || !s.themePicker.Open || s.themePicker.Loading || s.themePicker.Pending || !s.themePicker.previewValid || len(s.themePicker.Names) == 0 {
		return
	}
	name := s.themePicker.Names[s.themePicker.Selection]
	definition := s.themePicker.PreviewDefinition
	s.themeGeneration++
	generation := s.themeGeneration
	s.SetState(func() { s.themePicker.Pending = true })
	runtime := s.Context().Runtime()
	go func() {
		err := service.Save(name)
		runtime.Dispatch(func() {
			if generation != s.themeGeneration || !s.themePicker.Open {
				return
			}
			rollback := s.themePicker.CommittedDefinition
			s.SetState(func() {
				if err != nil {
					s.themePicker.Pending = false
					s.themePicker.Err = fmt.Errorf("save theme %q: %w", name, err)
					s.themePicker.PreviewDefinition = rollback
					s.themePicker.previewValid = false
					return
				}
				s.themeName = name
				s.themeDefinition = definition
				s.themePicker.Close()
			})
			if err != nil {
				s.applyTheme(rollback)
			}
		})
	}()
}

func (s *appState) cancelThemePicker() {
	if !s.themePicker.Open || s.themePicker.Pending {
		return
	}
	s.themeGeneration++
	definition := s.themePicker.CommittedDefinition
	s.SetState(func() { s.themePicker.Close() })
	s.applyTheme(definition)
}

func (s *appState) openSubagents() {
	if !s.admitRootModal() {
		return
	}
	if s.phase != phaseReady || s.bound == nil {
		return
	}
	s.SetState(func() {
		s.subagentRequestGeneration++
		s.subagentRosterLoading = false
		s.subagentRosterRefreshPending = false
		s.subagentsOpen = true
		s.subagentFilter = ""
	})
	s.refreshSubagents()
	if s.subagentWatchCancel != nil {
		s.subagentWatchCancel()
	}
	watchContext, cancel := context.WithCancel(s.attachmentCtx)
	s.subagentWatchCancel = cancel
	runtime := s.Context().Runtime()
	bound := s.bound
	watchGeneration := s.subagentRequestGeneration
	if watcher, ok := bound.(sessionclient.SessionEventWatcher); ok {
		go func() {
			for watchContext.Err() == nil {
				_, stream, err := watcher.Watch(watchContext)
				if err == nil {
					for updates := range stream.Updates() {
						changed := false
						for _, event := range updates {
							changed = changed || event.Kind == protocol.SessionEventSubagentChanged
						}
						if !changed {
							continue
						}
						runtime.Dispatch(func() {
							if s.subagentWorkspaceActive() && s.bound == bound && s.subagentRequestGeneration == watchGeneration {
								s.refreshSubagents()
							}
						})
					}
				}
				select {
				case <-watchContext.Done():
					return
				case <-time.After(250 * time.Millisecond):
				}
			}
		}()
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchContext.Done():
				return
			case <-ticker.C:
				runtime.Dispatch(func() {
					if !s.subagentWorkspaceActive() || s.bound != bound || s.subagentRequestGeneration != watchGeneration {
						return
					}
					refreshRoster, conversationID := s.visibleSubagentRefreshTargets()
					if refreshRoster {
						s.refreshSubagents()
					}
					if conversationID != "" {
						s.refreshSubagentTranscript(conversationID)
					}
				})
			}
		}
	}()
}

func (s *appState) closeSubagents() {
	s.SetState(func() {
		s.subagentsOpen = false
		s.subagentFilter = ""
		s.subagentRevealPending = false
		s.subagentPendingAgent = ""
	})
	s.stopSubagentWatchIfIdle()
}

func (s *appState) visibleSubagentRefreshTargets() (bool, string) {
	return s.subagentsOpen && !s.subagentRosterLoading, s.subagentPaneID
}

func (s *appState) subagentWorkspaceActive() bool {
	if s.subagentsOpen {
		return true
	}
	for _, pane := range s.workspace.Panes() {
		if pane.Kind == workspacePaneSubagentConversation {
			return true
		}
	}
	return false
}

func (s *appState) stopSubagentWatchIfIdle() {
	if s.subagentWorkspaceActive() {
		return
	}
	if s.subagentWatchCancel != nil {
		s.subagentWatchCancel()
		s.subagentWatchCancel = nil
	}
	s.subagentRequestGeneration++
	s.subagentRosterLoading = false
	s.subagentRosterRefreshPending = false
}

func (s *appState) refreshSubagents() {
	if s.bound == nil {
		return
	}
	s.subagentRosterGeneration++
	if s.subagentRosterLoading {
		s.subagentRosterRefreshPending = true
		return
	}
	s.startSubagentRosterRequest(s.subagentRosterGeneration)
}

func (s *appState) startSubagentRosterRequest(rosterGeneration uint64) {
	bound := s.bound
	attachmentContext := s.attachmentCtx
	requestGeneration := s.subagentRequestGeneration
	sessionID := s.session.ID
	runtime := s.Context().Runtime()
	s.subagentRosterLoading = true
	go func() {
		requestContext, cancel := context.WithTimeout(attachmentContext, subagentReadTimeout)
		defer cancel()
		result, err := bound.Subagent(requestContext, protocol.SubagentOperationInput{Action: protocol.SubagentListAgents})
		if attachmentContext.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if s.bound != bound || s.session.ID != sessionID || s.subagentRequestGeneration != requestGeneration {
				return
			}
			s.subagentRosterLoading = false
			current := s.subagentRosterResponseCurrent(requestGeneration, rosterGeneration)
			refreshPending := s.subagentRosterRefreshPending
			s.subagentRosterRefreshPending = false
			if current {
				if err != nil {
					if s.subagentPendingAgent != "" {
						s.SetState(func() { s.cancelPendingSubagentToolOpen() })
						s.showToast(toastInput{Title: "Could not open subagent", Subtitle: err.Error(), Variant: toastError})
					}
				} else {
					s.acceptSubagentResult(result)
				}
			}
			if refreshPending && s.subagentWorkspaceActive() && s.bound == bound {
				s.startSubagentRosterRequest(s.subagentRosterGeneration)
			}
		})
	}()
}

func (s *appState) subagentRosterResponseCurrent(requestGeneration, rosterGeneration uint64) bool {
	return s.subagentRequestGeneration == requestGeneration && s.subagentRosterGeneration == rosterGeneration
}

func (s *appState) acceptSubagentResult(result protocol.SubagentOperationResult) {
	requestedAgent, conversationID := "", ""
	s.SetState(func() {
		s.applySubagentResult(result)
		requestedAgent = s.subagentPendingAgent
		if requestedAgent != "" {
			conversationID = subagentConversationIDForAgent(s.subagentConversations, requestedAgent)
			s.subagentPendingAgent = ""
		}
	})
	s.stopSubagentWatchIfIdle()
	if requestedAgent == "" {
		return
	}
	if conversationID == "" {
		s.showToast(toastInput{
			Title:    "Subagent conversation unavailable",
			Subtitle: fmt.Sprintf("No active conversation for %q.", requestedAgent), Variant: toastWarning,
		})
		return
	}
	s.openSubagentConversation(conversationID)
}

func (s *appState) cancelPendingSubagentToolOpen() {
	s.subagentPendingAgent = ""
}

func (s *appState) openSubagentFromTool(agentName string) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" || s.phase != phaseReady || s.bound == nil {
		return
	}
	if !s.subagentsOpen {
		s.openSubagents()
	}
	s.SetState(func() {
		s.subagentPendingAgent = agentName
		s.subagentSelection = agentName
	})
	s.refreshSubagents()
}

func (s *appState) applySubagentResult(result protocol.SubagentOperationResult) {
	previousItems := filteredSubagentRosterItems(subagentRosterItems(s.subagentDefinitions, s.subagentConversations), s.subagentFilter)
	previousSelected, previousOK := selectedSubagentRosterItem(previousItems, s.subagentSelection)
	previousIndex := -1
	previousOffset := 0
	if previousOK {
		previousIndex = subagentRosterSelectionIndex(previousItems, previousSelected.Name)
		previousOffset = subagentRosterOffset(previousItems, previousIndex)
	}

	s.subagentDefinitions = append([]protocol.SubagentDefinition(nil), result.Definitions...)
	s.applySubagentDiagnostics(s.session.ID, result.Diagnostics)
	s.subagentConversations = append([]protocol.SubagentConversation(nil), result.Conversations...)
	s.reconcileSubagentTabs()
	items := filteredSubagentRosterItems(subagentRosterItems(s.subagentDefinitions, s.subagentConversations), s.subagentFilter)
	selected, ok := selectedSubagentRosterItem(items, s.subagentSelection)
	if !ok {
		s.subagentSelection = ""
		s.subagentRevealPending = false
		s.subagentRevealOffset = 0
		return
	}
	s.subagentSelection = selected.Name
	index := subagentRosterSelectionIndex(items, selected.Name)
	offset := subagentRosterOffset(items, index)
	rosterVisible := s.subagentsOpen
	selectionMoved := !previousOK || previousSelected.Name != selected.Name || previousIndex != index || previousOffset != offset
	if rosterVisible && selectionMoved {
		s.subagentRevealOffset = offset
		s.subagentRevealPending = true
	} else if !rosterVisible {
		s.subagentRevealPending = false
	}
}

func (s *appState) reconcileSubagentTabs() {
	active := make(map[string]struct{}, len(s.subagentConversations))
	for _, conversation := range s.subagentConversations {
		active[conversation.ID] = struct{}{}
	}
	retained := make([]string, 0, len(s.subagentTranscriptOrder))
	for _, conversationID := range s.subagentTranscriptOrder {
		if _, exists := active[conversationID]; exists {
			retained = append(retained, conversationID)
			continue
		}
		delete(s.subagentTranscripts, conversationID)
		delete(s.subagentTranscriptErrors, conversationID)
		s.advanceSubagentTranscriptLoad(conversationID)
		s.subagentTranscriptLoading[conversationID] = false
		s.advanceSubagentLiveLoad(conversationID)
		s.subagentLiveLoading[conversationID] = false
		delete(s.subagentScrolls, conversationID)
		delete(s.subagentFocuses, conversationID)
		delete(s.subagentLive, conversationID)
		identity, err := workspacePaneIdentityFor(subagentWorkspacePane(conversationID))
		if err == nil {
			s.workspace.Close(identity)
		}
		if s.activityConversationID == conversationID {
			s.activitySourceID = ""
			s.activityConversationID = ""
			s.inlineActivityOpen = make(map[string]bool)
			s.activityCursor = activityToolKey{}
		}
		if s.subagentScrollToEndID == conversationID {
			s.subagentScrollToEndID = ""
			s.subagentNeedsScroll = false
			s.subagentPendingLayout = false
		}
	}
	s.subagentTranscriptOrder = retained
	s.syncWorkspaceSelection()
}

func (s *appState) cancelSubagentTask(taskID string, generation uint64) {
	if s.bound == nil {
		return
	}
	bound := s.bound
	attachmentContext := s.attachmentCtx
	requestGeneration := s.subagentRequestGeneration
	sessionID := s.session.ID
	runtime := s.Context().Runtime()
	go func() {
		_, err := bound.Subagent(attachmentContext, protocol.SubagentOperationInput{
			Action: protocol.SubagentCancel, TaskID: taskID, Generation: generation,
		})
		if attachmentContext.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if s.bound != bound || s.session.ID != sessionID || s.subagentRequestGeneration != requestGeneration {
				return
			}
			if err != nil {
				s.showToast(toastInput{Title: "Could not cancel subagent task", Subtitle: err.Error(), Variant: toastError})
				return
			}
			s.showToast(toastInput{Title: "Subagent task cancellation requested", Variant: toastInfo})
			s.refreshSubagents()
		})
	}()
}

func (s *appState) subagentFocus(conversationID string) *ui.FocusNode {
	if s.subagentFocuses == nil {
		s.subagentFocuses = make(map[string]*ui.FocusNode)
	}
	if s.subagentFocuses[conversationID] == nil {
		s.subagentFocuses[conversationID] = &ui.FocusNode{}
	}
	return s.subagentFocuses[conversationID]
}

func (s *appState) requestSubagentScrollToEnd(conversationID string) {
	s.subagentScrollToEndID = conversationID
	s.subagentNeedsScroll = true
	s.subagentPendingLayout = true
}

func (s *appState) clearSubagentActivityForConversationChange(conversationID string) {
	if s.activityConversationID == "" || s.activityConversationID == conversationID {
		return
	}
	s.activitySourceID = ""
	s.activityConversationID = ""
	s.inlineActivityOpen = make(map[string]bool)
	s.activityCursor = activityToolKey{}
}

func (s *appState) openSubagentConversation(conversationID string) {
	if s.bound == nil {
		return
	}
	var openErr error
	s.SetState(func() {
		_, newTab, err := s.workspace.Open(subagentWorkspacePane(conversationID))
		if err != nil {
			openErr = err
			return
		}
		s.subagentsOpen = false
		s.subagentFilter = ""
		s.subagentTranscriptOrder = ensureSubagentTab(s.subagentTranscriptOrder, conversationID)
		if s.subagentScrolls[conversationID] == nil {
			s.subagentScrolls[conversationID] = &ui.ScrollController{}
		}
		s.subagentFocus(conversationID)
		s.syncWorkspaceSelection()
		s.clearSubagentActivityForConversationChange(conversationID)
		if newTab {
			s.requestSubagentScrollToEnd(conversationID)
		}
		s.subagentRevealPending = false
	})
	if openErr != nil {
		s.showToast(toastInput{Title: "Could not open workspace tab", Subtitle: openErr.Error(), Variant: toastWarning})
		return
	}
	s.refreshSubagentTranscript(conversationID)
}

func (s *appState) refreshSubagentTranscript(conversationID string) {
	if s.bound == nil || conversationID == "" {
		return
	}
	bound := s.bound
	attachmentContext := s.attachmentCtx
	sessionID := s.session.ID
	runtime := s.Context().Runtime()
	if reader, ok := bound.(sessionclient.SubagentEventReader); ok && !s.subagentLiveLoading[conversationID] {
		s.subagentLiveLoading[conversationID] = true
		liveGeneration := s.advanceSubagentLiveLoad(conversationID)
		current := s.subagentLive[conversationID]
		go func() {
			requestContext, cancel := context.WithTimeout(attachmentContext, subagentReadTimeout)
			defer cancel()
			page, err := reader.SubagentEvents(requestContext, conversationID, current.StreamID, current.LastSequence)
			if attachmentContext.Err() != nil {
				return
			}
			runtime.Dispatch(func() {
				if s.bound != bound || s.session.ID != sessionID || !s.subagentLiveLoadCurrent(conversationID, liveGeneration) || !containsSubagentTab(s.subagentTranscriptOrder, conversationID) {
					return
				}
				s.subagentLiveLoading[conversationID] = false
				if err != nil {
					return
				}
				followOutput := s.subagentPaneID == conversationID && scrollControllerPinnedToEnd(s.subagentScrolls[conversationID])
				s.SetState(func() {
					if page.ResyncRequired {
						s.subagentLive[conversationID] = protocol.SubagentLiveEventPage{}
						return
					}
					merged := s.subagentLive[conversationID]
					if merged.StreamID != "" && merged.StreamID != page.StreamID {
						merged = protocol.SubagentLiveEventPage{}
					}
					merged.StreamID, merged.FirstSequence, merged.LastSequence = page.StreamID, page.FirstSequence, max(merged.LastSequence, page.LastSequence)
					for _, event := range page.Events {
						if event.Sequence > s.subagentLive[conversationID].LastSequence {
							merged.Events = append(merged.Events, event)
						}
					}
					if len(merged.Events) > 128 {
						merged.Events = append([]protocol.SubagentLiveEvent(nil), merged.Events[len(merged.Events)-128:]...)
					}
					s.subagentLive[conversationID] = merged
					if followOutput && len(page.Events) > 0 {
						s.requestSubagentScrollToEnd(conversationID)
					}
				})
			})
		}()
	}
	if s.subagentTranscriptLoading[conversationID] {
		return
	}
	s.subagentTranscriptLoading[conversationID] = true
	loadGeneration := s.advanceSubagentTranscriptLoad(conversationID)
	go func() {
		requestContext, cancel := context.WithTimeout(attachmentContext, subagentReadTimeout)
		defer cancel()
		transcript, err := bound.SubagentTranscript(requestContext, conversationID)
		if attachmentContext.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if s.bound != bound || s.session.ID != sessionID || !s.subagentTranscriptLoadCurrent(conversationID, loadGeneration) || !containsSubagentTab(s.subagentTranscriptOrder, conversationID) {
				return
			}
			s.subagentTranscriptLoading[conversationID] = false
			if err != nil {
				s.SetState(func() { s.subagentTranscriptErrors[conversationID] = err.Error() })
				return
			}
			if len(transcript.Messages) >= len(s.subagentTranscripts[conversationID].Messages) {
				followOutput := s.subagentPaneID == conversationID && scrollControllerPinnedToEnd(s.subagentScrolls[conversationID])
				s.SetState(func() {
					s.subagentTranscripts[conversationID] = transcript
					if followOutput {
						s.requestSubagentScrollToEnd(conversationID)
					}
					delete(s.subagentTranscriptErrors, conversationID)
				})
			}
		})
	}()
}

func (s *appState) closeSubagentConversation(conversationID string) {
	s.SetState(func() {
		identity, err := workspacePaneIdentityFor(subagentWorkspacePane(conversationID))
		if err == nil {
			s.workspace.Close(identity)
		}
		delete(s.subagentTranscripts, conversationID)
		delete(s.subagentTranscriptErrors, conversationID)
		s.advanceSubagentTranscriptLoad(conversationID)
		s.subagentTranscriptLoading[conversationID] = false
		s.advanceSubagentLiveLoad(conversationID)
		s.subagentLiveLoading[conversationID] = false
		delete(s.subagentScrolls, conversationID)
		delete(s.subagentFocuses, conversationID)
		delete(s.subagentLive, conversationID)
		for index, id := range s.subagentTranscriptOrder {
			if id == conversationID {
				s.subagentTranscriptOrder = append(s.subagentTranscriptOrder[:index], s.subagentTranscriptOrder[index+1:]...)
				break
			}
		}
		s.syncWorkspaceSelection()
		if s.activityConversationID == conversationID {
			s.activitySourceID = ""
			s.activityConversationID = ""
			s.inlineActivityOpen = make(map[string]bool)
			s.activityCursor = activityToolKey{}
		}
		if s.subagentScrollToEndID == conversationID {
			s.subagentScrollToEndID = ""
			s.subagentNeedsScroll = false
			s.subagentPendingLayout = false
		}
	})
	s.stopSubagentWatchIfIdle()
}

func (s *appState) openWorkspacePicker() {
	if !s.admitRootModal() {
		return
	}
	if !s.workspace.StripVisible() {
		s.showToast(toastInput{Title: "No workspace tabs", Subtitle: "Open a secondary surface first.", Variant: toastWarning})
		return
	}
	s.SetState(func() {
		s.workspacePickerOpen = true
		s.workspacePickerQuery = ""
		s.workspacePickerSelection = s.workspaceSelectedIndex()
		s.workspacePickerScroll = ui.ScrollController{}
		s.requestWorkspacePickerReveal(s.workspacePickerSelection)
	})
}

func (s *appState) requestWorkspacePickerReveal(selection int) {
	viewport := s.workspacePickerScroll.Metrics().ViewportHeight
	offset := max(0, selection-5)
	if viewport > 0 {
		current := s.workspacePickerScroll.Metrics().ScrollOffset
		offset = current
		if selection < current {
			offset = selection
		} else if selection >= current+viewport {
			offset = selection - viewport + 1
		}
	}
	s.workspacePickerRevealOffset = max(0, offset)
	s.workspacePickerRevealPending = true
}

func (s *appState) workspaceSelectedIndex() int {
	selected := s.workspace.SelectedIdentity()
	if selected == workspaceAgentIdentity {
		return 0
	}
	for index, pane := range s.workspace.Panes() {
		identity, err := workspacePaneIdentityFor(pane)
		if err == nil && identity == selected {
			return index + 1
		}
	}
	return 0
}

func (s *appState) syncTranscriptVisibility(visible bool) {
	if visible == s.transcriptVisible {
		return
	}
	if !visible {
		// The left click selecting a workspace tab is captured before this
		// transition. Keep an in-flight history anchor authoritative rather
		// than treating that tab click as transcript scroll input.
		if s.transcriptHistoryRestore != 0 {
			s.transcriptHistoryAnchorInput = s.transcriptHistoryInputGeneration
		}
		s.transcriptPinnedOnHide = scrollControllerPinnedToEnd(&s.scroll)
	} else if s.transcriptPinnedOnHide {
		s.requestTranscriptScroll()
	}
	s.transcriptVisible = visible
}

func (s *appState) syncWorkspaceSelection() {
	_, paneSelected := s.workspace.SelectedPane()
	s.syncTranscriptVisibility(!paneSelected)
	if !s.workspace.StripVisible() {
		s.workspacePickerOpen = false
		s.workspacePickerQuery = ""
		s.workspacePickerSelection = 0
		s.workspacePickerRevealPending = false
	}
	pane, selected := s.workspace.SelectedPane()
	if !selected || pane.Kind != workspacePaneSubagentConversation {
		s.subagentPaneID = ""
		s.activitySelected = false
		return
	}
	s.subagentPaneID = pane.ResourceID
	s.activitySelected = true
}

func (s *appState) advanceSubagentLiveLoad(conversationID string) uint64 {
	s.subagentLiveLoads[conversationID]++
	return s.subagentLiveLoads[conversationID]
}

func (s *appState) subagentLiveLoadCurrent(conversationID string, generation uint64) bool {
	return s.subagentLiveLoads[conversationID] == generation
}

func (s *appState) advanceSubagentTranscriptLoad(conversationID string) uint64 {
	s.subagentTranscriptLoads[conversationID]++
	return s.subagentTranscriptLoads[conversationID]
}

func (s *appState) subagentTranscriptLoadCurrent(conversationID string, generation uint64) bool {
	return s.subagentTranscriptLoads[conversationID] == generation
}

func ensureSubagentTab(ids []string, target string) []string {
	if containsSubagentTab(ids, target) {
		return ids
	}
	return append(ids, target)
}

func containsSubagentTab(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func cloneSubagentLive(input map[string]protocol.SubagentLiveEventPage) map[string]protocol.SubagentLiveEventPage {
	output := make(map[string]protocol.SubagentLiveEventPage, len(input))
	for id, page := range input {
		page.Events = append([]protocol.SubagentLiveEvent(nil), page.Events...)
		output[id] = page
	}
	return output
}

func cloneSubagentTranscripts(input map[string]protocol.SubagentTranscript) map[string]protocol.SubagentTranscript {
	output := make(map[string]protocol.SubagentTranscript, len(input))
	for id, transcript := range input {
		transcript.Messages = append([]protocol.TranscriptMessage(nil), transcript.Messages...)
		output[id] = transcript
	}
	return output
}

func cloneScrollControllerMap(input map[string]*ui.ScrollController) map[string]*ui.ScrollController {
	output := make(map[string]*ui.ScrollController, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneFocusNodeMap(input map[string]*ui.FocusNode) map[string]*ui.FocusNode {
	output := make(map[string]*ui.FocusNode, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneStringMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func (s *appState) requestSubagentDismiss(conversationID string, generation uint64) {
	for _, conversation := range s.subagentConversations {
		if conversation.ID == conversationID {
			s.SetState(func() {
				s.subagentDismissID = conversationID
				s.subagentDismissName = conversation.AgentName
				s.subagentDismissGeneration = generation
				s.subagentDismissError = ""
			})
			return
		}
	}
}

func (s *appState) dismissSubagent(conversationID string, generation uint64) {
	if s.bound == nil || s.subagentDismissPending {
		return
	}
	s.SetState(func() {
		s.subagentDismissPending = true
		s.subagentDismissError = ""
	})
	bound := s.bound
	attachmentContext := s.attachmentCtx
	sessionID := s.session.ID
	runtime := s.Context().Runtime()
	go func() {
		_, err := bound.Subagent(attachmentContext, protocol.SubagentOperationInput{
			Action: protocol.SubagentDismiss, ConversationID: conversationID, Generation: generation,
		})
		if attachmentContext.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if s.bound != bound || s.session.ID != sessionID || s.subagentDismissID != conversationID {
				return
			}
			if err != nil {
				s.SetState(func() {
					s.subagentDismissPending = false
					s.subagentDismissError = err.Error()
				})
				return
			}
			s.SetState(func() {
				s.subagentDismissID = ""
				s.subagentDismissName = ""
				s.subagentDismissGeneration = 0
				s.subagentDismissPending = false
				s.subagentDismissError = ""
				identity, identityErr := workspacePaneIdentityFor(subagentWorkspacePane(conversationID))
				if identityErr == nil {
					s.workspace.Close(identity)
				}
				delete(s.subagentTranscripts, conversationID)
				delete(s.subagentTranscriptErrors, conversationID)
				s.advanceSubagentTranscriptLoad(conversationID)
				s.subagentTranscriptLoading[conversationID] = false
				s.advanceSubagentLiveLoad(conversationID)
				s.subagentLiveLoading[conversationID] = false
				delete(s.subagentScrolls, conversationID)
				delete(s.subagentFocuses, conversationID)
				delete(s.subagentLive, conversationID)
				for index, id := range s.subagentTranscriptOrder {
					if id == conversationID {
						s.subagentTranscriptOrder = append(s.subagentTranscriptOrder[:index], s.subagentTranscriptOrder[index+1:]...)
						break
					}
				}
				s.syncWorkspaceSelection()
				if s.activityConversationID == conversationID {
					s.activitySourceID = ""
					s.activityConversationID = ""
					s.inlineActivityOpen = make(map[string]bool)
					s.activityCursor = activityToolKey{}
				}
				if s.subagentScrollToEndID == conversationID {
					s.subagentScrollToEndID = ""
					s.subagentNeedsScroll = false
					s.subagentPendingLayout = false
				}
			})
			s.showToast(toastInput{Title: "Subagent dismissed", Variant: toastInfo})
			s.stopSubagentWatchIfIdle()
			if s.subagentWorkspaceActive() {
				s.refreshSubagents()
			}
		})
	}()
}

func (s *appState) enqueueDiffPreferenceWrite(service DiffPreferenceService, enabled bool, report func(uint64, error)) {
	s.diffPreferenceMu.Lock()
	s.diffPreferenceDesired = enabled
	s.diffPreferenceGeneration++
	if s.diffPreferenceWriting {
		s.diffPreferenceMu.Unlock()
		return
	}
	s.diffPreferenceWriting = true
	s.diffPreferenceWrites.Add(1)
	s.diffPreferenceMu.Unlock()
	go func() {
		defer s.diffPreferenceWrites.Done()
		for {
			s.diffPreferenceMu.Lock()
			desired := s.diffPreferenceDesired
			generation := s.diffPreferenceGeneration
			s.diffPreferenceMu.Unlock()
			err := service.SetWrapLines(desired)
			s.diffPreferenceMu.Lock()
			if generation != s.diffPreferenceGeneration {
				s.diffPreferenceMu.Unlock()
				continue
			}
			s.diffPreferenceWriting = false
			s.diffPreferenceMu.Unlock()
			if err != nil && report != nil {
				report(generation, err)
			}
			return
		}
	}()
}

func (s *appState) flushDiffPreferenceWrites(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.diffPreferenceWrites.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (s *appState) setDiffWrapLines(enabled bool) {
	s.SetState(func() { s.diffWrapLines = enabled })
	service := s.Widget().(app).Options.DiffPreferenceService
	if service == nil {
		return
	}
	runtime := s.Context().Runtime()
	s.enqueueDiffPreferenceWrite(service, enabled, func(generation uint64, err error) {
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			s.diffPreferenceMu.Lock()
			current := generation == s.diffPreferenceGeneration
			s.diffPreferenceMu.Unlock()
			if current {
				s.showToast(toastInput{Title: "Could not save diff preference", Subtitle: err.Error(), Variant: toastWarning})
			}
		})
	})
}

func (s *appState) openConfigurationPicker(mode configurationPickerMode) {
	if !s.admitRootModal() {
		return
	}
	if s.phase != phaseReady || s.bound == nil || s.configurationPicker.Mode != configurationPickerClosed {
		return
	}
	busyTransition := s.reloadPending || s.cwdPending || s.compactPending || s.configurationPicker.Pending
	if busyTransition {
		s.showToast(toastInput{Title: "Session is busy", Subtitle: "Wait for the current session transition to finish.", Variant: toastWarning})
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

func (s *appState) saveModelContextWindow() {
	service := s.Widget().(app).Options.ModelOverrideService
	if service == nil {
		s.SetState(func() { s.configurationPicker.Error = "Context-window settings are unavailable" })
		return
	}
	value := 0
	if text := strings.TrimSpace(s.configurationPicker.EditValue); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed <= 0 {
			s.SetState(func() { s.configurationPicker.Error = "Enter a positive integer, or leave blank to clear" })
			return
		}
		value = parsed
	}
	selector := s.configurationPicker.EditModel
	if err := service.SetContextWindow(selector, value); err != nil {
		s.SetState(func() { s.configurationPicker.Error = err.Error() })
		return
	}
	s.SetState(func() {
		s.configurationPicker.EditingContext = false
		s.configurationPicker.EditModel = ""
		s.configurationPicker.EditValue = ""
	})
	s.openConfigurationPicker(configurationPickerModel)
}

func (s *appState) applyConfigurationSelection() {
	if s.phase != phaseReady || s.bound == nil {
		return
	}
	mode := s.configurationPicker.Mode
	busyTransition := s.reloadPending || s.cwdPending || s.compactPending || s.configurationPicker.Pending
	if busyTransition {
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
				s.SetState(func() { s.applyConfigurationResult(result.Session) })
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
			if snapshotErr != nil {
				s.showToast(toastInput{Title: "Configuration refresh failed", Subtitle: snapshotErr.Error(), Variant: toastError})
			}
		})
	}()
}

func (s *appState) applyConfigurationResult(session protocol.SessionInfo) {
	s.session = session
	s.configurationPicker.CurrentModel = session.Model
	s.configurationPicker.CurrentThinking = session.ThinkingLevel
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
					s.invalidateFileMentions()
					s.session = info
					s.locationBase = info.CWD
					s.location = info.CWD
					s.vcsStatus = nil
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
			s.startVCSMonitoring()
			if info.CWD != previousCWD {
				s.refreshFileIndex(runtime)
			}
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
				s.SetState(func() {
					s.locationBase = location
					s.location = formatVCSLocation(location, s.vcsStatus)
				})
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
			toast.Subtitle += " · session refresh failed: " + snapshotErr.Error()
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
		details = append([]string{"Session refresh failed: " + snapshotErr.Error()}, details...)
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

func (s *appState) openCurrentSessionRename() {
	if !s.admitRootModal() || s.session.ID == "" || s.sessionRename.Pending {
		return
	}

	s.SetState(func() { s.sessionRename.Begin(s.session) })
}

func (s *appState) renameCurrentSession(value string) {
	var generation uint64
	var sessionID, name string
	var started bool
	s.SetState(func() {
		s.sessionRename.SetText(value)
		generation, sessionID, name, started = s.sessionRename.BeginSave()
	})
	if !started {
		return
	}
	server := s.Widget().(app).Options.Server
	runtime := s.Context().Runtime()
	go func() {
		renameContext, cancel := context.WithTimeout(s.attachmentCtx, 5*time.Second)
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
			s.SetState(func() {
				if s.sessionRename.Resolve(generation, renamed, err) && err == nil && s.session.ID == sessionID {
					s.session.Name = renamed.Name
				}
			})
		})
	}()
}

func (s *appState) openSessionExplorer() {
	if !s.admitRootModal() {
		return
	}
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
					s.session.Name = renamed.Name
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

func (s *appState) createNewSession() {
	if s.phase != phaseReady || s.bound == nil {
		return
	}
	if s.sessionCreatePending {
		s.showToast(toastInput{Title: "New session unavailable", Subtitle: "Session creation is already in progress.", Variant: toastWarning})
		return
	}

	options := s.Widget().(app).Options
	input := protocol.CreateSessionInput{
		CWD:           s.session.CWD,
		Model:         options.DefaultModel,
		ThinkingLevel: options.DefaultThinking,
	}
	createContext, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	generation := s.sessionCreateGeneration + 1
	sourceOperation := s.operation
	sourceSessionID := s.session.ID
	s.SetState(func() {
		s.sessionCreateGeneration = generation
		s.sessionCreatePending = true
		s.sessionCreateCancel = cancel
	})
	runtime := s.Context().Runtime()
	go func() {
		bound, snapshot, location, err := createSessionForSwitch(
			createContext, options.Server, input, options.ResolveLocation,
		)
		cancel()
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if generation != s.sessionCreateGeneration {
				return
			}
			s.sessionCreateCancel = nil
			s.sessionCreatePending = false
			if s.operation != sourceOperation || s.session.ID != sourceSessionID {
				return
			}
			if err != nil {
				s.showToast(toastInput{Title: "New session failed", Subtitle: err.Error(), Variant: toastError})
				return
			}
			var operation uint64
			s.SetState(func() {
				s.installSession(bound, snapshot, location)
				operation = s.operation
			})
			s.startVCSMonitoring()
			s.watchAttachedSession(bound, operation)
		})
	}()
}

func (s *appState) forkCurrentSession(message string) {
	if s.phase != phaseReady || s.bound == nil || s.hasActiveWork() {
		return
	}
	options := s.Widget().(app).Options
	childID, err := identifier.New("session_")
	if err != nil {
		s.showToast(toastInput{Title: "Could not fork session", Subtitle: err.Error(), Variant: toastError})
		return
	}
	forkContext, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	generation := s.sessionCreateGeneration + 1
	sourceOperation := s.operation
	sourceSessionID := s.session.ID
	s.SetState(func() {
		s.sessionCreateGeneration = generation
		s.sessionCreatePending = true
		s.sessionCreateCancel = cancel
	})
	runtime := s.Context().Runtime()
	go func() {
		created, err := options.Server.ForkSession(forkContext, sourceSessionID, protocol.ForkSessionInput{ID: childID})
		var bound sessionclient.Session
		var snapshot protocol.SessionSnapshot
		var location string
		if err == nil {
			bound, snapshot, location, err = attachSessionForSwitch(forkContext, options.Server, created.ID, options.ResolveLocation)
		}
		cancel()
		if s.ctx.Err() != nil {
			return
		}
		runtime.Dispatch(func() {
			if generation != s.sessionCreateGeneration {
				return
			}
			s.sessionCreateCancel = nil
			s.sessionCreatePending = false
			if s.operation != sourceOperation || s.session.ID != sourceSessionID {
				return
			}
			if err != nil {
				s.showToast(toastInput{Title: "Fork failed", Subtitle: err.Error(), Variant: toastError})
				return
			}
			var operation uint64
			s.SetState(func() {
				s.installSession(bound, snapshot, location)
				operation = s.operation
			})
			s.startVCSMonitoring()
			s.watchAttachedSession(bound, operation)
			if prompt := strings.TrimSpace(message); prompt != "" {
				s.startPromptSubmission(prompt, func(ctx context.Context) (sessionclient.Run, error) {
					if structured, ok := bound.(sessionclient.StructuredPromptSession); ok {
						result, submitErr := structured.SubmitPromptInput(ctx, protocol.PromptInput{Text: prompt})
						return result.Run, submitErr
					}
					return bound.StartPrompt(ctx, prompt)
				})
			}
		})
	}()
}

func createSessionForSwitch(
	ctx context.Context,
	server sessionclient.Server,
	input protocol.CreateSessionInput,
	resolveLocation func(context.Context, string) string,
) (sessionclient.Session, protocol.SessionSnapshot, string, error) {
	created, err := server.CreateSession(ctx, input)
	if err != nil {
		return nil, protocol.SessionSnapshot{}, "", fmt.Errorf("create session: %w", err)
	}
	if created.ID == "" {
		return nil, protocol.SessionSnapshot{}, "", errors.New("create session returned an empty session id")
	}
	return attachSessionForSwitch(ctx, server, created.ID, resolveLocation)
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
			s.startVCSMonitoring()
			s.watchAttachedSession(bound, operation)
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
	if s.sessionDrafts == nil {
		s.sessionDrafts = make(map[string]string)
	}
	if s.sessionDraftAttachments == nil {
		s.sessionDraftAttachments = make(map[string][]stagedAttachment)
	}
	if s.sessionDraftAttachmentIDs == nil {
		s.sessionDraftAttachmentIDs = make(map[string][]string)
	}
	if s.session.ID != "" {
		s.sessionDrafts[s.session.ID] = s.composer
		s.sessionDraftAttachments[s.session.ID] = append([]stagedAttachment(nil), s.composerAttachments...)
		s.sessionDraftAttachmentIDs[s.session.ID] = append([]string(nil), s.composerAttachmentIDs...)
	}
	s.resetAttachmentContext()
	s.fileMention.Close()
	s.closeSessionMention()
	s.sessionMentions.Entries = nil
	s.sessionMentions.generation++
	s.operation++
	s.terminalSettledRunID = ""
	s.notifiedRunIDs = make(map[string]bool)
	s.notifiedRunOrder = nil
	s.deferredSessionSnapshot = nil
	s.session = snapshot.Session
	s.metadataStreamID = snapshot.EventStreamID
	s.metadataSequence = snapshot.EventCursor
	s.bound = bound
	s.location = location
	s.locationBase = location
	s.vcsStatus = nil
	s.composer = s.sessionDrafts[snapshot.Session.ID]
	s.composerAttachments = append([]stagedAttachment(nil), s.sessionDraftAttachments[snapshot.Session.ID]...)
	s.composerAttachmentIDs = append([]string(nil), s.sessionDraftAttachmentIDs[snapshot.Session.ID]...)
	s.annotations = append([]protocol.AnnotationSummary(nil), snapshot.Annotations...)
	s.composerCursorEndGeneration++
	s.messages = nil
	s.transcriptList = ui.SliverListController{}
	s.transcriptInitialLoading, s.transcriptInitialPositioned, s.transcriptInitialStable = false, false, false
	s.transcriptHistoryUserScroll = false
	s.transcriptHistoryLastOffset = 0
	s.transcriptHistoryInitialized = false
	s.transcriptHistoryCursor = ""
	s.transcriptHistoryHasMore = false
	s.transcriptHistoryLoading = false
	s.transcriptHistoryError = ""
	s.transcriptHistoryGeneration++
	s.transcriptHistoryAnchorID = ""
	s.transcriptHistoryAnchorInset = 0
	s.transcriptHistoryAnchorExpected = 0
	s.transcriptHistoryAnchorEnd = false
	s.transcriptHistoryRestore = 0
	s.configurationPicker = configurationPickerController{}
	s.sessionRename.Reset()
	s.annotationPicker = annotationPickerController{}
	s.cwdPending = false
	s.reloadPending = false
	s.compactPending = false
	s.compactOperationID = ""
	s.followUpMutationPending = false
	s.resetLiveRun()
	s.contextTokens = 0
	s.contextWindow = 0
	s.scroll = ui.ScrollController{}
	s.activityScroll = ui.ScrollController{}
	s.activityList = activityListController{}
	s.workspace.Reset()
	s.workspaceID = ""
	s.closeWorkspaceFilePicker()
	s.indexedFiles.reset()
	s.workspacePickerOpen = false
	s.workspacePickerQuery = ""
	s.workspacePickerSelection = 0
	s.workspacePickerScroll = ui.ScrollController{}
	s.workspacePickerRevealPending = false
	s.workspacePickerRevealOffset = 0
	s.activitySourceID = ""
	s.activityConversationID = ""
	s.activitySelected = false
	s.transcriptVisible = true
	s.transcriptPinnedOnHide = false
	s.subagentsOpen = false
	s.subagentFilter = ""
	s.subagentRequestGeneration++
	s.subagentRosterLoading = false
	s.subagentRosterRefreshPending = false
	s.subagentDefinitions = nil
	s.subagentDiagnostics = nil
	s.subagentConversations = nil
	s.subagentSelection = ""
	s.subagentPendingAgent = ""
	s.subagentPaneID = ""
	s.subagentTranscripts = make(map[string]protocol.SubagentTranscript)
	s.subagentTranscriptErrors = make(map[string]string)
	s.subagentTranscriptLoads = make(map[string]uint64)
	s.subagentTranscriptLoading = make(map[string]bool)
	s.subagentTranscriptOrder = nil
	s.subagentScrolls = make(map[string]*ui.ScrollController)
	s.subagentFocuses = make(map[string]*ui.FocusNode)
	s.subagentScrollToEndID = ""
	s.subagentNeedsScroll = false
	s.subagentPendingLayout = false
	s.subagentLive = make(map[string]protocol.SubagentLiveEventPage)
	s.subagentLiveLoads = make(map[string]uint64)
	s.subagentLiveLoading = make(map[string]bool)
	s.subagentRevealPending = false
	s.subagentRevealOffset = 0
	s.subagentDismissID = ""
	s.subagentDismissName = ""
	s.subagentDismissGeneration = 0
	s.subagentDismissPending = false
	s.subagentDismissError = ""
	s.inlineActivityOpen = make(map[string]bool)
	s.activityExpanded = make(map[activityToolKey]bool)
	s.activityCursor = activityToolKey{}
	s.activeRun = nil
	s.activeRunID = ""
	s.runPending = false
	s.prompt = nil
	s.activeBash = nil
	s.activeBashID = ""
	s.bashStarting = false
	s.bashAdmission = nil
	s.bashCollapsed = make(map[string]bool)
	s.transcriptAnnotationsExpanded = make(map[string]bool)
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
	text := strings.TrimSpace(value)
	if s.bound == nil {
		return
	}
	for _, item := range s.composerAttachments {
		if item.Uploading {
			s.SetState(func() { s.status = "Waiting for attachments to finish uploading…" })
			return
		}
		if item.Error != "" && !item.PreserveID {
			s.SetState(func() { s.status = "Remove failed attachments before sending" })
			return
		}
	}
	attachmentIDs := s.composerPromptAttachmentIDs()
	annotationIDs := s.annotationIDs()
	if text == "" && len(attachmentIDs) == 0 && len(annotationIDs) == 0 {
		if s.runPending {
			s.promoteFollowUps()
		}
		return
	}
	if command, excludeFromContext, ok := parseDirectBash(value); ok {
		s.startDirectBash(value, command, excludeFromContext)
		return
	}
	if s.runPending {
		s.queueFollowUp(text, s.Context().Runtime())
		return
	}
	bound := s.bound
	input := protocol.PromptInput{Text: text, AttachmentIDs: attachmentIDs, AnnotationIDs: annotationIDs}
	s.startPromptSubmission(text, func(ctx context.Context) (sessionclient.Run, error) {
		if structured, ok := bound.(sessionclient.StructuredPromptSession); ok {
			result, err := structured.SubmitPromptInput(ctx, input)
			if err != nil {
				return nil, err
			}
			if result.Queued {
				return nil, promptQueuedError{queue: result.Queue}
			}
			return result.Run, nil
		}
		if queueAware, ok := bound.(sessionclient.FollowUpSession); ok {
			result, err := queueAware.SubmitPrompt(ctx, text)
			if err != nil {
				return nil, err
			}
			if result.Queued {
				return nil, promptQueuedError{queue: result.Queue}
			}
			return result.Run, nil
		}
		return bound.StartPrompt(ctx, text)
	})
}

func (s *appState) queueFollowUp(text string, runtime ui.Runtime) {
	followUpSession, ok := s.bound.(sessionclient.FollowUpSession)
	if s.followUpMutationPending || !ok {
		return
	}
	bound, operation, submittedDraft := s.bound, s.operation, s.composer
	submittedGeneration := s.composerDraftGeneration
	submittedAttachments := s.composerPromptAttachmentIDs()
	submittedAnnotations := s.annotationIDs()
	submittedRows := append([]stagedAttachment(nil), s.composerAttachments...)
	ctx := s.ctx
	s.SetState(func() { s.followUpMutationPending = true })
	go func() {
		var result sessionclient.PromptSubmission
		var err error
		if structured, ok := bound.(sessionclient.StructuredPromptSession); ok {
			result, err = structured.SubmitPromptInput(ctx, protocol.PromptInput{Text: text, AttachmentIDs: submittedAttachments, AnnotationIDs: submittedAnnotations})
		} else {
			result, err = followUpSession.SubmitPrompt(ctx, text)
		}
		var snapshot protocol.SessionSnapshot
		var snapshotErr error
		if err == nil && !result.Queued {
			if result.Run == nil {
				err = errors.New("prompt submission started without a run")
			} else {
				snapshot, snapshotErr = bound.Snapshot(ctx)
			}
		}
		runtime.Dispatch(func() {
			if s.operation != operation || s.bound != bound {
				return
			}
			s.SetState(func() {
				s.followUpMutationPending = false
				if err != nil {
					return
				}
				if result.Queued {
					s.followUps = result.Queue
					if s.composer == submittedDraft && s.composerDraftGeneration == submittedGeneration {
						s.composer = ""
						s.composerAttachmentIDs = nil
						s.composerAttachments = nil
					}
					return
				}
				if snapshotErr == nil {
					s.applySnapshot(snapshot)
				} else {
					s.resetLiveRun()
					s.liveMessages = append(s.liveMessages, transcriptMessage{Role: "user", Text: text})
					s.liveHasUser = true
					s.runPending = true
					s.status = "esc abort · ctrl+c detach"
					s.markTerminalRunStarted(result.Run.ID())
				}
				s.activeRun = result.Run
				s.activeRunID = result.Run.ID()
				if s.composer == submittedDraft && s.composerDraftGeneration == submittedGeneration {
					s.composer = ""
					s.composerAttachmentIDs = nil
					s.composerAttachments = nil
				}
			})
			if err != nil {
				s.SetState(func() {
					if s.composer == submittedDraft && s.composerDraftGeneration == submittedGeneration {
						s.composerAttachments = submittedRows
					}
				})
				s.showToast(toastInput{Title: "Could not queue follow-up", Subtitle: err.Error(), Variant: toastError})
				return
			}
			if !result.Queued {
				s.showToast(toastInput{Title: "Prompt started", Subtitle: "The previous run finished before the message was queued.", Variant: toastInfo})
				s.watchSession(bound, operation, result.Run.ID())
			}
		})
	}()
}

func (s *appState) restoreFollowUps(_ ui.EventContext) {
	followUpSession, ok := s.bound.(sessionclient.FollowUpSession)
	if s.followUpMutationPending || !ok || s.followUps.Count == 0 {
		return
	}
	bound, operation := s.bound, s.operation
	ctx, runtime := s.ctx, s.Context().Runtime()
	s.SetState(func() { s.followUpMutationPending = true })
	go func() {
		result, err := followUpSession.RestoreFollowUps(ctx)
		var restoredAttachments map[string]stagedAttachment
		if err == nil {
			restoredAttachments = resolveRestoredAttachments(ctx, bound, result.Messages)
		}
		runtime.Dispatch(func() {
			if s.operation != operation || s.bound != bound {
				return
			}
			s.SetState(func() {
				s.followUpMutationPending = false
				if err != nil {
					return
				}
				s.restoreFollowUpDraft(result, restoredAttachments)
			})
			if err != nil {
				s.showToast(toastInput{Title: "Could not restore follow-ups", Subtitle: err.Error(), Variant: toastError})
			}
		})
	}()
}

func (s *appState) restoreFollowUpDraft(result protocol.RestoreFollowUpsResult, restoredAttachments map[string]stagedAttachment) {
	seen := make(map[string]bool)
	for _, id := range s.composerAttachmentIDs {
		seen[id] = true
	}
	for _, item := range s.composerAttachments {
		seen[item.Info.ID] = true
	}
	texts := make([]string, 0, len(result.Messages))
	for _, message := range result.Messages {
		if message.Text != "" {
			texts = append(texts, message.Text)
		}
		for _, id := range message.AttachmentIDs {
			if !seen[id] {
				s.composerAttachments = append(s.composerAttachments, restoredAttachments[id])
				seen[id] = true
			}
		}
	}
	if s.composer != "" {
		texts = append(texts, s.composer)
	}
	s.composer = strings.Join(texts, "\n\n")
	s.composerCursorEndGeneration++
	s.composerDraftGeneration++
	s.followUps = result.Queue
}

func (s *appState) promoteFollowUps() {
	followUpSession, ok := s.bound.(sessionclient.FollowUpSession)
	if s.followUpMutationPending || !ok || s.followUps.Count == 0 {
		return
	}
	bound, operation := s.bound, s.operation
	ctx, runtime := s.ctx, s.Context().Runtime()
	s.SetState(func() { s.followUpMutationPending = true })
	go func() {
		result, err := followUpSession.PromoteFollowUps(ctx)
		var snapshot protocol.SessionSnapshot
		var snapshotErr error
		if err != nil {
			snapshot, snapshotErr = bound.Snapshot(ctx)
		}
		runtime.Dispatch(func() {
			if s.operation != operation || s.bound != bound {
				return
			}
			s.SetState(func() {
				s.followUpMutationPending = false
				if err != nil {
					if snapshotErr == nil {
						s.followUps = snapshot.FollowUps
					}
					return
				}
				s.followUps = result.Queue
			})
			if err != nil {
				s.showToast(toastInput{Title: "Could not send follow-ups now", Subtitle: err.Error(), Variant: toastError})
				return
			}
			s.showToast(toastInput{Title: "Follow-ups sent", Subtitle: fmt.Sprintf("Promoted %d queued messages.", result.Promoted), Variant: toastInfo})
		})
	}()
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

type promptQueuedError struct{ queue protocol.FollowUpQueue }

func (err promptQueuedError) Error() string { return "prompt queued behind active work" }

func (s *appState) startPromptSubmission(display string, start func(context.Context) (sessionclient.Run, error)) {
	if s.runPending {
		s.SetState(func() { s.status = "Run in progress · esc abort · ctrl+c detach" })
		return
	}
	admission := &promptAdmission{}
	bound := s.bound
	submittedBaseAttachmentIDs := append([]string(nil), s.composerAttachmentIDs...)
	submittedRows := append([]stagedAttachment(nil), s.composerAttachments...)
	operation := s.operation
	runtime := s.Context().Runtime()
	s.SetState(func() {
		s.composer = ""
		s.composerAttachmentIDs = nil
		s.composerAttachments = nil
		s.status = "esc abort · ctrl+c detach"
		s.resetLiveRun()
		s.turnActivity = "Working…"
		s.liveMessages = append(s.liveMessages, transcriptMessage{Role: "user", Text: display, Content: attachmentTranscriptContent(display, submittedRows)})
		s.liveHasUser = true
		s.requestTranscriptScroll()
		s.runPending = true
		s.markTerminalRunStarted("")
		s.prompt = admission
	})
	go func() {
		run, err := start(s.ctx)
		if err != nil {
			var queued promptQueuedError
			if errors.As(err, &queued) {
				snapshot, snapshotErr := bound.Snapshot(s.ctx)
				runtime.Dispatch(func() {
					if operation != s.operation {
						return
					}
					s.SetState(func() {
						s.resetLiveRun()
						s.runPending = false
						s.prompt = nil
						s.followUps = queued.queue
						if snapshotErr == nil {
							s.deferredSessionSnapshot = nil
							s.applySnapshot(snapshot)
						}
					})
					if snapshotErr == nil && snapshot.ActiveRunID != "" {
						s.watchSession(bound, operation, snapshot.ActiveRunID)
					}
				})
				return
			}
			if s.ctx.Err() == nil {
				runtime.Dispatch(func() {
					if operation == s.operation {
						s.SetState(func() {
							if s.composer == "" {
								s.composer = display
								s.composerAttachmentIDs = submittedBaseAttachmentIDs
								s.composerAttachments = submittedRows
							}
						})
					}
				})
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
	if deferred := s.deferredSessionSnapshot; deferred != nil {
		if !s.mergeSnapshotTranscript(projectTranscript(deferred.Messages)) {
			s.resetTranscriptHistoryFromSnapshot(*deferred)
		}
		s.applySubagentSnapshot(*deferred)
		s.followUps = deferred.FollowUps
		s.contextTokens = deferred.ContextTokens
		s.contextWindow = deferred.ContextWindow
		s.sessionUsage = deferred.Usage
		s.followTranscriptIfPinned()
		s.deferredSessionSnapshot = nil
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
	if s.bound != nil {
		snapshot, snapshotErr = s.bound.Snapshot(s.ctx)
	}
	runtime.Dispatch(func() {
		if operation != s.operation {
			return
		}
		if (snapshotErr != nil || snapshot.Session.ID == "") && s.deferredSessionSnapshot != nil {
			snapshot = *s.deferredSessionSnapshot
			snapshotErr = nil
		}
		s.deferredSessionSnapshot = nil
		nextRunID := ""
		nextBashID := ""
		reconciledSubmission := false
		s.SetState(func() {
			s.markTerminalRunSettled(s.activeRunID)
			s.activeRun = nil
			s.activeRunID = ""
			s.runPending = false
			s.prompt = nil
			s.status = ""
			s.followTranscriptIfPinned()
			if runErr != nil {
				if snapshotErr == nil && snapshot.Session.ID != "" {
					reconciledSubmission = true
					s.applySnapshot(snapshot)
					nextRunID = snapshot.ActiveRunID
					if s.activeBash == nil {
						nextBashID = snapshot.ActiveBashExecutionID
					}
				} else {
					s.messages = append(s.messages, s.liveMessages...)
					s.resetLiveRun()
				}
				if snapshotErr != nil || snapshot.Session.ID == "" {
					s.messages = append(s.messages, transcriptMessage{Role: "error", Text: runErr.Error()})
				}
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
		if runErr != nil && reconciledSubmission {
			s.showToast(toastInput{Title: "Prompt submission reconciled", Subtitle: runErr.Error(), Variant: toastWarning})
		}
		if nextRunID != "" && s.bound != nil {
			s.watchSession(s.bound, s.operation, nextRunID)
		}
		if nextBashID != "" && s.bound != nil {
			s.resumeBash(s.bound, s.operation, nextBashID)
		}
	})
}

func (s *appState) dismiss(_ ui.EventContext) {
	owner := s.inputOwner()
	if owner == inputTheme {
		s.cancelThemePicker()
		return
	}
	if owner == inputSubagentDismiss {
		if !s.subagentDismissPending {
			s.SetState(func() {
				s.subagentDismissID = ""
				s.subagentDismissName = ""
				s.subagentDismissGeneration = 0
				s.subagentDismissError = ""
			})
		}
		return
	}
	if owner == inputAnnotations {
		s.SetState(func() { s.annotationPicker.Close() })
		return
	}
	if owner == inputRename {
		if !s.sessionRename.Pending {
			s.SetState(func() { s.sessionRename.Cancel() })
		}
		return
	}
	if owner == inputConfiguration {
		if s.configurationPicker.Pending {
			return
		}
		s.SetState(func() { s.configurationPicker.HandleKey(ui.Key{Keycode: vaxis.KeyEsc}) })
		return
	}
	if owner == inputSessionDetails {
		s.SetState(func() { s.sessionDetailsOpen = false })
		return
	}
	if owner.root() == inputSessions {
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
	if owner == inputSessionMention {
		s.SetState(func() { s.closeSessionMention() })
		return
	}
	if owner == inputFileMention {
		s.SetState(func() { s.fileMention.Close() })
		return
	}
	if owner == inputBashHistory {
		s.SetState(func() { s.bashHistory.Close() })
		return
	}
	if owner == inputPalette {
		s.SetState(func() { s.palette.Close() })
		return
	}
	switch owner {
	case inputFiles:
		s.SetState(func() { s.closeWorkspaceFilePicker() })
		return
	case inputTabs:
		s.SetState(func() {
			s.workspacePickerOpen = false
			s.workspacePickerQuery = ""
			s.workspacePickerRevealPending = false
		})
		return
	case inputSubagents:
		s.closeSubagents()
		return
	case inputInteraction:
		return // The dock's local dismiss action owns cancellation.
	case inputPane:
		return // The pane-local child owns dismissal after it is rendered.
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
	case phaseAuthWaiting, phaseAuthBrowser:
		s.cancelLogin()
		s.SetState(func() {
			s.phase = phaseAuthSelect
			s.errorText = ""
			s.status = ""
			s.instructions = auth.OpenAICodexDeviceInstructions{}
			s.browserInstructions = auth.AnthropicLoginInstructions{}
			s.authProviderID = ""
			s.authAPIKey = ""
			s.authCode = ""
			s.authCodeInput = nil
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
		if !s.runPending || s.runStopping {
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
