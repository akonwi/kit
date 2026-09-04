// Package tui implements Kit's native vaxis terminal client.
package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	err := ui.Run(app{Options: options})
	close(done)
	cancel()
	return err
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
	Role       string
	Text       string
	Thinking   string
	ToolName   string
	ToolStatus string
	IsError    bool
	Pending    bool
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

	phase          phase
	errorText      string
	status         string
	composer       string
	authFilter     string
	authSelection  int
	authProviderID string
	authAPIKey     string
	authPending    bool
	session        protocol.SessionInfo
	bound          sessionclient.Session
	messages       []transcriptMessage
	liveMessages   []transcriptMessage
	liveAssistant  int
	liveHasUser    bool
	liveTools      map[string]int
	liveContent    map[int]liveContentBlock
	liveSequence   int64
	turnActivity   string
	runStopping    bool
	contextTokens  int
	contextWindow  int
	scroll         ui.ScrollController
	needsScroll    bool
	activeRun      sessionclient.Run
	activeRunID    string
	runPending     bool
	prompt         *promptAdmission

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
	if !s.needsScroll {
		return false
	}
	if !s.scroll.Attached() {
		return true
	}
	s.scroll.ScrollToEnd()
	s.needsScroll = false
	return false
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
		Phase:          s.phase,
		Error:          s.errorText,
		Status:         s.status,
		Composer:       s.composer,
		AuthFilter:     s.authFilter,
		AuthSelection:  s.authSelection,
		AuthProviderID: s.authProviderID,
		AuthAPIKey:     s.authAPIKey,
		AuthPending:    s.authPending,
		Session:        s.session,
		Messages:       presentedMessages,
		Running:        s.runPending,
		TurnActivity:   s.turnActivity,
		ContextTokens:  s.contextTokens,
		ContextWindow:  s.contextWindow,
		Scroll:         &s.scroll,
		Instructions:   s.instructions,
		Remaining:      s.remaining,
		Location:       options.Location,
	}
	callbacks := shellCallbacks{
		OpenAuth: func(ui.EventContext) {
			s.SetState(func() {
				s.phase = phaseAuthSelect
				s.errorText = ""
				s.authFilter = ""
				s.authSelection = 0
			})
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
		CopyCode: func(ctx ui.EventContext) {
			if s.instructions.UserCode != "" {
				ctx.Copy(s.instructions.UserCode)
				s.SetState(func() { s.status = "Device code copied" })
			}
		},
		ComposerChanged: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.composer = value })
		},
		Submit: s.submit,
		Retry: func(ui.EventContext) {
			s.SetState(func() {
				s.phase = phaseLoading
				s.errorText = ""
				s.status = "Starting Kit…"
			})
			s.startBootstrap(options.DefaultModel, options.DefaultThinking)
		},
		Quit: func(ctx ui.EventContext) {
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
	s.messages = projectTranscript(snapshot.Messages)
	s.resetLiveRun()
	s.needsScroll = true
	s.contextTokens = snapshot.ContextTokens
	s.contextWindow = snapshot.ContextWindow
	s.activeRunID = snapshot.ActiveRunID
	s.runPending = snapshot.ActiveRunID != ""
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
		if strings.TrimSpace(message.Text) == "" && strings.TrimSpace(message.Thinking) == "" && message.ToolName == "" {
			continue
		}
		role := message.Role
		if message.IsError && role != "tool" {
			role = "error"
		}
		result = append(result, transcriptMessage{
			Role: role, Text: message.Text, Thinking: message.Thinking,
			ToolName: message.ToolName, IsError: message.IsError,
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
	s.runStopping = false
}

func (s *appState) setTurnActivity(activity string) {
	if s.runStopping && activity != "" {
		return
	}
	s.turnActivity = activity
}

func (s *appState) applyRunEvents(events []protocol.SessionEvent) {
	for _, event := range events {
		if event.Sequence <= s.liveSequence {
			continue
		}
		s.liveSequence = event.Sequence
		switch event.Kind {
		case protocol.SessionEventRunStarted:
			s.setTurnActivity("Working…")
		case protocol.SessionEventUserMessage:
			if s.turnActivity == "" {
				s.setTurnActivity("Working…")
			}
			if !s.liveHasUser {
				s.liveMessages = append(s.liveMessages, transcriptMessage{Role: "user", Text: event.Text})
				s.liveHasUser = true
			}
		case protocol.SessionEventAssistantStarted:
			s.setTurnActivity("Working…")
			s.liveAssistant = len(s.liveMessages)
			s.liveContent = make(map[int]liveContentBlock)
			if event.Thinking != "" {
				s.liveContent[-2] = liveContentBlock{kind: protocol.SessionEventThinkingDelta, text: event.Thinking}
			}
			if event.Text != "" {
				s.liveContent[-1] = liveContentBlock{kind: protocol.SessionEventAssistantTextDelta, text: event.Text}
			}
			s.liveMessages = append(s.liveMessages, transcriptMessage{
				Role: "assistant", Text: event.Text, Thinking: event.Thinking, Pending: true,
			})
		case protocol.SessionEventAssistantTextDelta, protocol.SessionEventThinkingDelta:
			index := s.ensureLiveAssistant()
			block, exists := s.liveContent[event.ContentIndex]
			if exists && block.kind != event.Kind {
				continue
			}
			block.kind = event.Kind
			block.text += event.Delta
			s.liveContent[event.ContentIndex] = block
			s.syncLiveAssistant(index)
			if event.Kind == protocol.SessionEventThinkingDelta {
				s.setTurnActivity(latestThinkingLine(s.liveMessages[index].Thinking))
			} else {
				s.setTurnActivity("Working…")
			}
		case protocol.SessionEventAssistantCompleted:
			index := s.ensureLiveAssistant()
			if event.Text != "" || event.Thinking != "" {
				s.liveMessages[index].Text = event.Text
				s.liveMessages[index].Thinking = event.Thinking
			}
			s.liveMessages[index].Pending = false
			if s.liveMessages[index].Text == "" && s.liveMessages[index].Thinking == "" {
				s.removeLiveMessage(index)
			}
			s.liveAssistant = -1
			s.liveContent = make(map[int]liveContentBlock)
			s.setTurnActivity("Working…")
		case protocol.SessionEventToolPlanned, protocol.SessionEventToolStarted:
			s.setTurnActivity("Working…")
			index, ok := s.liveTools[event.ToolCallID]
			if !ok {
				index = len(s.liveMessages)
				s.liveTools[event.ToolCallID] = index
				s.liveMessages = append(s.liveMessages, transcriptMessage{Role: "tool", ToolName: event.ToolName})
			}
			s.liveMessages[index].Pending = true
			if event.Kind == protocol.SessionEventToolPlanned {
				s.liveMessages[index].ToolStatus = "Preparing…"
			} else {
				s.liveMessages[index].ToolStatus = "Running…"
			}
		case protocol.SessionEventToolUpdated, protocol.SessionEventToolCompleted:
			s.setTurnActivity("Working…")
			index, ok := s.liveTools[event.ToolCallID]
			if !ok {
				index = len(s.liveMessages)
				s.liveTools[event.ToolCallID] = index
				s.liveMessages = append(s.liveMessages, transcriptMessage{Role: "tool", ToolName: event.ToolName})
			}
			s.liveMessages[index].Text = event.Text
			s.liveMessages[index].IsError = event.IsError
			if event.Kind == protocol.SessionEventToolCompleted {
				s.liveMessages[index].Pending = false
				if event.IsError {
					s.liveMessages[index].ToolStatus = "Failed"
				} else {
					s.liveMessages[index].ToolStatus = "Completed"
				}
			}
		case protocol.SessionEventRunFinished:
			s.runStopping = false
			s.setTurnActivity("")
		}
	}
	if len(events) > 0 {
		s.needsScroll = true
	}
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

func (s *appState) ensureLiveAssistant() int {
	if s.liveAssistant >= 0 && s.liveAssistant < len(s.liveMessages) {
		return s.liveAssistant
	}
	s.liveAssistant = len(s.liveMessages)
	s.liveContent = make(map[int]liveContentBlock)
	s.liveMessages = append(s.liveMessages, transcriptMessage{Role: "assistant", Pending: true})
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
	s.needsScroll = true
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

func (s *appState) submit(_ ui.EventContext, value string) {
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
		s.needsScroll = true
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
		s.SetState(func() {
			s.activeRun = nil
			s.activeRunID = ""
			s.runPending = false
			s.prompt = nil
			s.status = ""
			s.needsScroll = true
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
				return
			}
			s.messages = append(s.messages, transcriptMessage{Role: "assistant", Text: outcome.Text})
		})
		if nextRunID != "" && s.bound != nil {
			s.watchSession(s.bound, s.operation, nextRunID)
		}
	})
}

func (s *appState) dismiss(_ ui.EventContext) {
	switch s.phase {
	case phaseAuthSelect:
		s.SetState(func() {
			s.phase = phaseAuthGate
			s.errorText = ""
			s.status = "Sign-in cancelled"
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
			s.status = "Sign-in cancelled"
			s.instructions = auth.OpenAICodexDeviceInstructions{}
			s.authProviderID = ""
			s.authAPIKey = ""
			s.authPending = false
		})
	case phaseReady:
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
