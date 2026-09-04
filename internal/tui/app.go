// Package tui implements Kit's native vaxis terminal client.
package tui

import (
	"context"
	"errors"
	"fmt"
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
	Role string
	Text string
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
		Messages:       append([]transcriptMessage(nil), s.messages...),
		Running:        s.runPending,
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
					s.status = "Working… · esc abort · ctrl+c detach"
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
	s.needsScroll = true
	s.contextTokens = snapshot.ContextTokens
	s.contextWindow = snapshot.ContextWindow
	s.activeRunID = snapshot.ActiveRunID
	s.runPending = snapshot.ActiveRunID != ""
	if !s.runPending {
		s.activeRun = nil
		s.prompt = nil
		s.status = ""
	}
}

func projectTranscript(messages []protocol.TranscriptMessage) []transcriptMessage {
	result := make([]transcriptMessage, 0, len(messages))
	for _, message := range messages {
		text := message.Text
		role := message.Role
		if role == "tool" {
			if message.ToolName != "" {
				text = message.ToolName + "\n" + text
			}
			if message.IsError {
				role = "error"
			}
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		result = append(result, transcriptMessage{Role: role, Text: text})
	}
	return result
}

func (s *appState) watchSession(bound sessionclient.Session, operation uint64, runID string) {
	runtime := s.Context().Runtime()
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
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
					continue
				}
				snapshot, err := bound.Snapshot(s.ctx)
				if err != nil {
					if s.ctx.Err() == nil {
						runtime.Dispatch(func() {
							if operation == s.operation {
								s.SetState(func() { s.status = "Run finished · reconnecting transcript… · ctrl+c detach" })
							}
						})
					}
					continue
				}
				nextRunID := snapshot.ActiveRunID
				runtime.Dispatch(func() {
					if operation != s.operation {
						return
					}
					s.SetState(func() {
						s.applySnapshot(snapshot)
						if info.Status != protocol.RunStatusCompleted {
							message := info.ErrorMessage
							if message == "" {
								message = "Run " + string(info.Status)
							}
							s.messages = append(s.messages, transcriptMessage{Role: "error", Text: message})
						}
						if nextRunID != "" {
							s.status = "Working… · esc abort · ctrl+c detach"
						}
					})
				})
				if nextRunID == "" {
					return
				}
				runID = nextRunID
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
		s.status = "Working… · esc abort · ctrl+c detach"
		s.messages = append(s.messages, transcriptMessage{Role: "user", Text: text})
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
		ready := make(chan struct{}, 1)
		runtime.Dispatch(func() {
			s.SetState(func() {
				s.activeRun = run
				s.activeRunID = run.ID()
			})
			ready <- struct{}{}
		})
		select {
		case <-s.ctx.Done():
			if admission.abort.Load() {
				abort()
			}
			return
		case <-ready:
		}
		if admission.abort.Load() {
			abort()
		}
		outcome, err := run.Wait(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		s.finishRun(runtime, outcome, err)
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
		s.SetState(func() { s.status = "Stopping…" })
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
